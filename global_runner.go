package main

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/utilitywarehouse/semaphore-service-mirror/kube"
	"github.com/utilitywarehouse/semaphore-service-mirror/log"
)

// GlobalRunner watches a cluster for global services and mirrors the found
// configuration under a local namespace
type GlobalRunner struct {
	ctx                        context.Context
	client                     kubernetes.Interface
	globalServiceStore         *GlobalServiceStore
	serviceQueue               *queue
	serviceWatcher             *kube.ServiceWatcher
	endpointSliceQueue         *queue
	endpointSliceWatcher       *kube.EndpointSliceWatcher
	mirrorEndpointSliceWatcher *kube.EndpointSliceWatcher
	name                       string
	namespace                  string
	labelselector              string
	sync                       bool
	syncMirrorLabels           map[string]string // Labels used to watch mirrore endpointslices and delete stale objects on startup
	initialised                bool              // Flag to turn on after the successful initialisation of the runner.
	local                      bool              // Flag to identify if the runner is running against a local or remote cluster
	routingStrategyLabel       labels.Selector   // Label to identify services that want to utilise topology hints
}

func newGlobalRunner(client, watchClient kubernetes.Interface, name, namespace, labelselector string, resyncPeriod time.Duration, gst *GlobalServiceStore, local bool, rsl labels.Selector, sync bool) *GlobalRunner {
	mirrorLabels := map[string]string{
		"mirrored-endpoint-slice":        "true",
		"mirror-endpointslice-sync-name": name,
	}
	runner := &GlobalRunner{
		ctx:                  context.Background(),
		client:               client,
		name:                 name,
		namespace:            namespace,
		globalServiceStore:   gst,
		initialised:          false,
		local:                local,
		routingStrategyLabel: rsl,
		sync:                 sync,
		syncMirrorLabels:     mirrorLabels,
	}
	runner.serviceQueue = newQueue(fmt.Sprintf("%s-gl-service", name), runner.reconcileGlobalService)
	runner.endpointSliceQueue = newQueue(fmt.Sprintf("%s-endpointslice", name), runner.reconcileEndpointSlice)

	// Create and initialize a service watcher
	serviceWatcher := kube.NewServiceWatcher(
		fmt.Sprintf("gl-%s-serviceWatcher", name),
		watchClient,
		resyncPeriod,
		runner.ServiceEventHandler,
		labelselector,
		metav1.NamespaceAll,
	)
	runner.serviceWatcher = serviceWatcher
	runner.serviceWatcher.Init()

	// Create and initialize an endpointslice watcher
	endpointSliceWatcher := kube.NewEndpointSliceWatcher(
		fmt.Sprintf("gl-%s-endpointSliceWatcher", name),
		watchClient,
		resyncPeriod,
		runner.EndpointSliceEventHandler,
		labelselector,
		metav1.NamespaceAll,
	)
	runner.endpointSliceWatcher = endpointSliceWatcher
	runner.endpointSliceWatcher.Init()

	// Create and initialize an endpointslice watcher for mirrored endpointslices
	mirrorEndpointSliceWatcher := kube.NewEndpointSliceWatcher(
		fmt.Sprintf("mirror-%s-endpointSliceWatcher", name),
		watchClient,
		resyncPeriod,
		nil,
		labels.Set(mirrorLabels).String(),
		namespace,
	)
	runner.mirrorEndpointSliceWatcher = mirrorEndpointSliceWatcher
	runner.mirrorEndpointSliceWatcher.Init()

	return runner
}

// Run starts the watchers and queues of the runner
func (gr *GlobalRunner) Run() error {
	go gr.serviceWatcher.Run()
	// At this point the runner should be considered initialised and live.
	gr.initialised = true
	stopCh := make(chan struct{})
	if ok := cache.WaitForNamedCacheSync("serviceWatcher", stopCh, gr.serviceWatcher.HasSynced); !ok {
		return fmt.Errorf("failed to wait for service caches to sync")
	}

	go gr.endpointSliceWatcher.Run()
	go gr.mirrorEndpointSliceWatcher.Run()
	// We need to wait fot endpoinslices watchers to sync before we sync
	if ok := cache.WaitForNamedCacheSync(fmt.Sprintf("gl-%s-endpointSliceWatcher", gr.name), stopCh, gr.endpointSliceWatcher.HasSynced); !ok {
		return fmt.Errorf("failed to wait for endpintslices caches to sync")
	}
	if ok := cache.WaitForNamedCacheSync(fmt.Sprintf("mirror-%s-endpointSliceWatcher", gr.name), stopCh, gr.mirrorEndpointSliceWatcher.HasSynced); !ok {
		return fmt.Errorf("failed to wait for mirror endpintslices caches to sync")
	}
	// After endpointslice store syncs, perform a sync to delete stale mirrors
	if gr.sync {
		log.Logger.Info("Syncing endpointslices", "runner", gr.name)
		if err := gr.EndpointSliceSync(); err != nil {
			log.Logger.Warn(
				"Error syncing endpointslices, skipping..",
				"err", err,
				"runner", gr.name,
			)
		}
	}

	go gr.serviceQueue.Run()
	go gr.endpointSliceQueue.Run()

	return nil
}

// Stop stops watchers and runners
func (gr *GlobalRunner) Stop() {
	gr.serviceQueue.Stop()
	gr.serviceWatcher.Stop()
	gr.endpointSliceQueue.Stop()
	gr.endpointSliceWatcher.Stop()
}

// Initialised returns true when the runner is successfully initialised
func (gr *GlobalRunner) Initialised() bool {
	return gr.initialised
}

func (gr *GlobalRunner) reconcileGlobalService(name, namespace string) error {
	globalSvcName := generateGlobalServiceName(name, namespace)
	// Get the remote service
	log.Logger.Info("getting remote service", "namespace", namespace, "name", name, "runner", gr.name)
	remoteSvc, err := gr.getRemoteService(name, namespace)
	if errors.IsNotFound(err) {
		// If the remote service doesn't exist delete the cluster for
		// the service in the globalServiceStore
		log.Logger.Debug("deleting from global store", "namespace", namespace, "name", name, "runner", gr.name)
		gsvc := gr.globalServiceStore.DeleteClusterServiceTarget(name, namespace, gr.name)
		// If the returned global service is nil, then we should try to
		// delete the local service. If the service is already deleted
		// continue
		if gsvc == nil {
			log.Logger.Info("global service not found, deleting local service", "namespace", gr.namespace, "name", globalSvcName, "runner", gr.name)
			if err := kube.DeleteService(gr.ctx, gr.client, globalSvcName, gr.namespace); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("deleting service %s/%s: %v", gr.namespace, globalSvcName, err)
			}
		}
	} else if err != nil {
		return fmt.Errorf("getting remote service: %v", err)
	}
	// If the remote service wasn't deleted, try to add it to the store
	if remoteSvc != nil {
		setServiceTopologyHints := matchSelector(gr.routingStrategyLabel, remoteSvc)
		_, err := gr.globalServiceStore.AddOrUpdateClusterServiceTarget(remoteSvc, gr.name, setServiceTopologyHints)
		if err != nil {
			return fmt.Errorf("failed to create/update service: %v", err)
		}
	}
	gsvc, err := gr.globalServiceStore.Get(name, namespace)
	if err != nil {
		return fmt.Errorf("finding global service in the store: %v", err)
	}
	log.Logger.Debug("global service found", "name", gsvc.name, "runner", gr.name)
	// If the global service doesn't exist, create it. Otherwise, update it.
	globalSvc, err := kube.GetService(gr.ctx, gr.client, globalSvcName, gr.namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("local service not found, creating service", "namespace", gr.namespace, "name", gsvc.name, "runner", gr.name)
		if _, err := kube.CreateService(gr.ctx, gr.client, globalSvcName, gr.namespace, gsvc.labels, gsvc.annotations, remoteSvc.Spec.Ports, gsvc.headless); err != nil {
			return fmt.Errorf("creating service %s/%s: %v", gr.namespace, globalSvcName, err)
		}
	} else if err != nil {
		return fmt.Errorf("getting service %s/%s: %v", gr.namespace, globalSvcName, err)
	} else {
		log.Logger.Info("local service found, updating service", "namespace", gr.namespace, "name", gsvc.name, "runner", gr.name)
		if _, err := gr.updateGlobalService(globalSvc, gsvc.ports, gsvc.annotations); err != nil {
			return fmt.Errorf("updating service %s/%s: %v", gr.namespace, globalSvcName, err)
		}
	}
	return nil
}

func (gr *GlobalRunner) getRemoteService(name, namespace string) (*v1.Service, error) {
	return gr.serviceWatcher.Get(name, namespace)
}

// updateGlobalService is UpdateService that will also update the annotations to reflect clusters
func (gr *GlobalRunner) updateGlobalService(service *v1.Service, ports []v1.ServicePort, annotations map[string]string) (*v1.Service, error) {
	service.ObjectMeta.Annotations = annotations
	return kube.UpdateService(gr.ctx, gr.client, service, ports)
}

// ServiceEventHandler adds Service resource events to the respective queue
func (gr *GlobalRunner) ServiceEventHandler(eventType watch.EventType, old *v1.Service, new *v1.Service) {
	switch eventType {
	case watch.Added:
		log.Logger.Debug("service added", "namespace", new.Namespace, "name", new.Name, "runner", gr.name)
		gr.serviceQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("service modified", "namespace", new.Namespace, "name", new.Name, "runner", gr.name)
		gr.serviceQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("service deleted", "namespace", old.Namespace, "name", old.Name, "runner", gr.name)
		gr.serviceQueue.Add(old)
	default:
		log.Logger.Info("Unknown service event received: %v", eventType, "runner", gr.name)
	}
}

func (gr *GlobalRunner) getRemoteEndpointSlice(name, namespace string) (*discoveryv1.EndpointSlice, error) {
	return gr.endpointSliceWatcher.Get(name, namespace)
}

// EndpointSliceSync checks for stale mirrors (endpointslices) under the local
// namespace and deletes them
func (gr *GlobalRunner) EndpointSliceSync() error {
	storeEnpointSlices, err := gr.endpointSliceWatcher.List()
	if err != nil {
		return err
	}

	mirrorEndpointSliceList := []string{}
	for _, es := range storeEnpointSlices {
		mirrorEndpointSliceList = append(
			mirrorEndpointSliceList,
			generateGlobalEndpointSliceName(es.Name),
		)
	}

	currEndpointSlices, err := gr.mirrorEndpointSliceWatcher.List()
	if err != nil {
		return err
	}

	for _, es := range currEndpointSlices {
		_, inSlice := inSlice(mirrorEndpointSliceList, es.Name)
		if !inSlice {
			log.Logger.Info(
				"Deleting old endpointslice",
				"service", es.Name,
				"runner", gr.name,
			)
			if err := gr.deleteEndpointSlice(es.Name, es.Namespace); err != nil {
				log.Logger.Error(
					"Error clearing endpointslice",
					"endpointslice", es.Name,
					"err", err,
					"runner", gr.name,
				)
				return err
			}
		}
	}
	return nil
}

func (gr *GlobalRunner) getEndpointSlice(name, namespace string) (*discoveryv1.EndpointSlice, error) {
	return gr.client.DiscoveryV1().EndpointSlices(namespace).Get(
		gr.ctx,
		name,
		metav1.GetOptions{},
	)
}

// kube-proxy needs all Endpoints to have hints in order to allow topology aware routing.
func (gr *GlobalRunner) ensureEndpointSliceZones(endpoints []discoveryv1.Endpoint) []discoveryv1.Endpoint {
	var es []discoveryv1.Endpoint
	// For endpoints in remote clusters use a dummy zone and hint that will never be picker by kube-proxy
	if !gr.local {
		zone := "remote"
		for _, e := range endpoints {
			e.Zone = &zone
			e.Hints = &discoveryv1.EndpointHints{
				ForZones: []discoveryv1.ForZone{
					discoveryv1.ForZone{Name: "remote"}},
			}
			es = append(es, e)
		}
		return es
	}
	// For local endpoints allow all zones as set in config
	for _, e := range endpoints {
		e.Hints = &discoveryv1.EndpointHints{
			ForZones: DefaultLocalEndpointZones,
		}
		es = append(es, e)
	}
	return es
}

func (gr *GlobalRunner) createEndpointSlice(name, namespace, targetService string, at discoveryv1.AddressType, endpoints []discoveryv1.Endpoint, ports []discoveryv1.EndpointPort) (*discoveryv1.EndpointSlice, error) {
	return gr.client.DiscoveryV1().EndpointSlices(namespace).Create(
		gr.ctx,
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    generateEndpointSliceLabels(gr.syncMirrorLabels, targetService),
			},
			AddressType: at,
			Endpoints:   gr.ensureEndpointSliceZones(endpoints),
			Ports:       ports,
		},
		metav1.CreateOptions{},
	)
}

func (gr *GlobalRunner) updateEndpointSlice(name, namespace, targetService string, at discoveryv1.AddressType, endpoints []discoveryv1.Endpoint, ports []discoveryv1.EndpointPort) (*discoveryv1.EndpointSlice, error) {
	return gr.client.DiscoveryV1().EndpointSlices(namespace).Update(
		gr.ctx,
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    generateEndpointSliceLabels(gr.syncMirrorLabels, targetService),
			},
			AddressType: at,
			Endpoints:   gr.ensureEndpointSliceZones(endpoints),
			Ports:       ports,
		},
		metav1.UpdateOptions{},
	)
}

func (gr *GlobalRunner) deleteEndpointSlice(name, namespace string) error {
	return gr.client.DiscoveryV1().EndpointSlices(namespace).Delete(
		gr.ctx,
		name,
		metav1.DeleteOptions{},
	)
}

func (gr *GlobalRunner) reconcileEndpointSlice(name, namespace string) error {
	mirrorName := generateGlobalEndpointSliceName(name)
	// Get the remote endpointslice
	log.Logger.Info("getting remote endpointslice", "namespace", namespace, "name", name, "runner", gr.name)
	remoteEndpointSlice, err := gr.getRemoteEndpointSlice(name, namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("remote endpointslice not found, removing local mirror", "namespace", namespace, "name", name, "runner", gr.name)
		if err := gr.deleteEndpointSlice(mirrorName, gr.namespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting endpointslice %s/%s: %v", gr.namespace, mirrorName, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting remote endpointslice %s/%s: %v", namespace, name, err)

	}
	// Determine the local service to target
	targetSvc, ok := remoteEndpointSlice.Labels["kubernetes.io/service-name"]
	if !ok {
		return fmt.Errorf("remote endpointslice is missing kubernetes.io/service-name label")
	}
	targetGlobalService := generateGlobalServiceName(targetSvc, namespace)
	// If the mirror endpointslice doesn't exist, create it. Otherwise, update it.
	log.Logger.Info("getting local endpointslice", "namespace", gr.namespace, "name", mirrorName, "runner", gr.name)
	_, err = gr.getEndpointSlice(mirrorName, gr.namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("local endpointslice not found, creating", "namespace", gr.namespace, "name", mirrorName, "runner", gr.name)
		if _, err := gr.createEndpointSlice(mirrorName, gr.namespace, targetGlobalService, remoteEndpointSlice.AddressType, remoteEndpointSlice.Endpoints, remoteEndpointSlice.Ports); err != nil {
			return fmt.Errorf("creating endpointslice %s/%s: %v", gr.namespace, mirrorName, err)

		}
	} else if err != nil {
		return fmt.Errorf("getting endpointslice %s/%s: %v", gr.namespace, mirrorName, err)
	} else {
		log.Logger.Info("local endpointslice found, updating", "namespace", gr.namespace, "name", mirrorName, "runner", gr.name)
		if _, err := gr.updateEndpointSlice(mirrorName, gr.namespace, targetGlobalService, remoteEndpointSlice.AddressType, remoteEndpointSlice.Endpoints, remoteEndpointSlice.Ports); err != nil {
			return fmt.Errorf("updating endpointslice %s/%s: %v", gr.namespace, mirrorName, err)
		}
	}
	return nil
}

// EndpointSliceEventHandler adds EndpointSlice resource events to the respective queue
func (gr *GlobalRunner) EndpointSliceEventHandler(eventType watch.EventType, old *discoveryv1.EndpointSlice, new *discoveryv1.EndpointSlice) {
	switch eventType {
	case watch.Added:
		log.Logger.Debug("endpoints added", "namespace", new.Namespace, "name", new.Name, "runner", gr.name)
		gr.endpointSliceQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("endpoints modified", "namespace", new.Namespace, "name", new.Name, "runner", gr.name)
		gr.endpointSliceQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("endpoints deleted", "namespace", old.Namespace, "name", old.Name, "runner", gr.name)
		gr.endpointSliceQueue.Add(old)
	default:
		log.Logger.Info("Unknown endpoints event received: %v", eventType, "runner", gr.name)
	}
}
