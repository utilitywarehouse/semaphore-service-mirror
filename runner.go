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

type Runner struct {
	ctx                        context.Context
	client                     kubernetes.Interface
	serviceQueue               *queue
	serviceWatcher             *kube.ServiceWatcher
	mirrorServiceWatcher       *kube.ServiceWatcher
	endpointsQueue             *queue
	endpointsWatcher           *kube.EndpointsWatcher
	endpointSliceQueue         *queue
	endpointSliceWatcher       *kube.EndpointSliceWatcher
	mirrorEndpointsWatcher     *kube.EndpointsWatcher
	mirrorEndpointSliceWatcher *kube.EndpointSliceWatcher
	mirrorLabels               map[string]string
	globalServiceStore         *GlobalServiceStore
	globalServiceQueue         *queue
	name                       string
	namespace                  string
	prefix                     string
	labelselector              string
	sync                       bool
	initialised                bool // Flag to turn on after the successful initialisation of the runner.
}

func NewRunner(client, watchClient kubernetes.Interface, name, namespace, prefix, labelselector string, resyncPeriod time.Duration, sync bool, gst *GlobalServiceStore) *Runner {
	mirrorLabels := map[string]string{
		"mirrored-svc":           "true",
		"mirror-svc-prefix-sync": prefix,
	}
	runner := &Runner{
		ctx:                context.Background(),
		client:             client,
		name:               name,
		namespace:          namespace,
		prefix:             prefix,
		sync:               sync,
		mirrorLabels:       mirrorLabels,
		globalServiceStore: gst,
		initialised:        false,
	}
	runner.serviceQueue = newQueue(fmt.Sprintf("%s-service", name), runner.reconcileService)
	runner.globalServiceQueue = newQueue(fmt.Sprintf("%s-gl-service", name), runner.reconcileGlobalService)
	runner.endpointsQueue = newQueue(fmt.Sprintf("%s-endpoints", name), runner.reconcileEndpoints)
	runner.endpointSliceQueue = newQueue(fmt.Sprintf("%s-endpointslice", name), runner.reconcileEndpointSlice)

	// Create and initialize a service watcher
	serviceWatcher := kube.NewServiceWatcher(
		fmt.Sprintf("%s-serviceWatcher", name),
		watchClient,
		resyncPeriod,
		runner.ServiceEventHandler,
		labelselector,
		metav1.NamespaceAll,
	)
	runner.serviceWatcher = serviceWatcher
	runner.serviceWatcher.Init()

	// Create and initialize a service watcher for mirrored services
	mirrorServiceWatcher := kube.NewServiceWatcher(
		fmt.Sprintf("%s-mirrorServiceWatcher", name),
		client,
		resyncPeriod,
		nil,
		labels.Set(mirrorLabels).String(),
		namespace,
	)
	runner.mirrorServiceWatcher = mirrorServiceWatcher
	runner.mirrorServiceWatcher.Init()

	// Create and initialize an endpoints watcher
	endpointsWatcher := kube.NewEndpointsWatcher(
		fmt.Sprintf("%s-endpointsWatcher", name),
		watchClient,
		resyncPeriod,
		runner.EndpointsEventHandler,
		labelselector,
		metav1.NamespaceAll,
	)
	runner.endpointsWatcher = endpointsWatcher
	runner.endpointsWatcher.Init()

	// Create and initialize an endpoints watcher for mirrored endpoints
	mirrorEndpointsWatcher := kube.NewEndpointsWatcher(
		fmt.Sprintf("%s-mirrorEndpointsWatcher", name),
		client,
		resyncPeriod,
		nil,
		labels.Set(mirrorLabels).String(),
		namespace,
	)
	runner.mirrorEndpointsWatcher = mirrorEndpointsWatcher
	runner.mirrorEndpointsWatcher.Init()

	// Create and initialize an endpointslice watcher
	endpointSliceWatcher := kube.NewEndpointSliceWatcher(
		fmt.Sprintf("%s-endpointSliceWatcher", name),
		watchClient,
		resyncPeriod,
		runner.EndpointSliceEventHandler,
		labelselector,
		metav1.NamespaceAll,
	)
	runner.endpointSliceWatcher = endpointSliceWatcher
	runner.endpointSliceWatcher.Init()

	// Create and initialize an endpointSlice watcher for mirrored endpointSlices
	mirrorEndpointSliceWatcher := kube.NewEndpointSliceWatcher(
		fmt.Sprintf("%s-mirrorEndpointSliceWatcher", name),
		client,
		resyncPeriod,
		nil,
		labels.Set(mirrorLabels).String(),
		namespace,
	)
	runner.mirrorEndpointSliceWatcher = mirrorEndpointSliceWatcher
	runner.mirrorEndpointSliceWatcher.Init()

	return runner
}

func (r *Runner) Run() error {
	go r.serviceWatcher.Run()
	go r.mirrorServiceWatcher.Run()
	// At this point the runner should be considered initialised and live.
	r.initialised = true
	// wait for service watcher to sync before starting the endpoints to
	// avoid race between them. TODO: atm dummy and could run forever if
	// services cache fails to sync
	stopCh := make(chan struct{})
	if ok := cache.WaitForNamedCacheSync("serviceWatcher", stopCh, r.serviceWatcher.HasSynced); !ok {
		return fmt.Errorf("failed to wait for service caches to sync")
	}
	if ok := cache.WaitForNamedCacheSync("mirrorServiceWatcher", stopCh, r.mirrorServiceWatcher.HasSynced); !ok {
		return fmt.Errorf("failed to wait for mirror service caches to sync")
	}

	// After services store syncs, perform a sync to delete stale mirrors
	if r.sync {
		log.Logger.Info("Syncing services", "runner", r.name)
		if err := r.ServiceSync(); err != nil {
			log.Logger.Warn(
				"Error syncing services, skipping..",
				"err", err,
				"runner", r.name,
			)
		}
	}
	go r.endpointsWatcher.Run()
	go r.mirrorEndpointsWatcher.Run()
	go r.endpointSliceWatcher.Run()
	go r.mirrorEndpointSliceWatcher.Run()

	go r.serviceQueue.Run()
	go r.globalServiceQueue.Run()
	go r.endpointsQueue.Run()
	go r.endpointSliceQueue.Run()

	return nil
}

func (r *Runner) Stop() {
	r.serviceQueue.Stop()
	r.serviceWatcher.Stop()
	r.mirrorServiceWatcher.Stop()
	r.endpointsQueue.Stop()
	r.endpointsWatcher.Stop()
	r.mirrorEndpointsWatcher.Stop()
}

func (r *Runner) reconcileService(name, namespace string) error {
	mirrorName := generateMirrorName(r.prefix, namespace, name)

	// Get the remote service
	log.Logger.Info("getting remote service", "namespace", namespace, "name", name, "runner", r.name)
	remoteSvc, err := r.getRemoteService(name, namespace)
	if errors.IsNotFound(err) {
		// If the remote service doesn't exist, clean up the local mirror service (if it
		// exists)
		log.Logger.Info("remote service not found, deleting local service", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
		if err := r.deleteService(mirrorName, r.namespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting service %s/%s: %v", r.namespace, mirrorName, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting remote service: %v", err)
	}

	// If the mirror service doesn't exist, create it. Otherwise, update it.
	mirrorSvc, err := r.getService(mirrorName, r.namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("local service not found, creating service", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
		if _, err := r.createService(mirrorName, r.namespace, r.mirrorLabels, remoteSvc.Spec.Ports, isHeadless(remoteSvc)); err != nil {
			return fmt.Errorf("creating service %s/%s: %v", r.namespace, mirrorName, err)
		}
	} else if err != nil {
		return fmt.Errorf("getting service %s/%s: %v", r.namespace, mirrorName, err)
	} else {
		log.Logger.Info("local service found, updating service", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
		if _, err := r.updateService(mirrorSvc, remoteSvc.Spec.Ports); err != nil {
			return fmt.Errorf("updating service %s/%s: %v", r.namespace, mirrorName, err)
		}
	}

	return nil
}

func (r *Runner) reconcileGlobalService(name, namespace string) error {
	globalSvcName := generateGlobalServiceName(name, namespace)
	// Get the remote service
	log.Logger.Info("getting remote service", "namespace", namespace, "name", name, "runner", r.name)
	remoteSvc, err := r.getRemoteService(name, namespace)
	if errors.IsNotFound(err) {
		// If the remote service doesn't exist delete the cluster for
		// the service in the globalServiceStore
		log.Logger.Debug("deleting from global store", "namespace", namespace, "name", name, "runner", r.name)
		gsvc := r.globalServiceStore.DeleteClusterServiceTarget(name, namespace, r.name)
		// If the returned global service is nil, then we should try to
		// delete the local service. If the service is already deleted
		// continue
		if gsvc == nil {
			log.Logger.Info("global service not found, deleting local service", "namespace", r.namespace, "name", globalSvcName, "runner", r.name)
			if err := r.deleteService(globalSvcName, r.namespace); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("deleting service %s/%s: %v", r.namespace, globalSvcName, err)
			}
		}
	} else if err != nil {
		return fmt.Errorf("getting remote service: %v", err)
	}
	// If the remote service wasn't deleted, try to add it to the store
	if remoteSvc != nil {
		_, err := r.globalServiceStore.AddOrUpdateClusterServiceTarget(remoteSvc, r.name)
		if err != nil {
			return fmt.Errorf("failed to create/update service: %v", err)
		}
	}
	gsvc, err := r.globalServiceStore.Get(name, namespace)
	if err != nil {
		return fmt.Errorf("finding global service in the store: %v", err)
	}
	log.Logger.Debug("global service found", "name", gsvc.name, "runner", r.name)
	// If the global service doesn't exist, create it. Otherwise, update it.
	globalSvc, err := r.getService(globalSvcName, r.namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("local service not found, creating service", "namespace", r.namespace, "name", gsvc.name, "runner", r.name)
		if _, err := r.createService(globalSvcName, r.namespace, gsvc.labels, remoteSvc.Spec.Ports, gsvc.headless); err != nil {
			return fmt.Errorf("creating service %s/%s: %v", r.namespace, globalSvcName, err)
		}
	} else if err != nil {
		return fmt.Errorf("getting service %s/%s: %v", r.namespace, globalSvcName, err)
	} else {
		log.Logger.Info("local service found, updating service", "namespace", r.namespace, "name", gsvc.name, "runner", r.name)
		if _, err := r.updateGlobalService(globalSvc, gsvc.ports, gsvc.labels); err != nil {
			return fmt.Errorf("updating service %s/%s: %v", r.namespace, globalSvcName, err)
		}
	}
	return nil
}

func (r *Runner) getRemoteService(name, namespace string) (*v1.Service, error) {
	return r.serviceWatcher.Get(name, namespace)
}

func (r *Runner) getService(name, namespace string) (*v1.Service, error) {
	return r.client.CoreV1().Services(namespace).Get(
		r.ctx,
		name,
		metav1.GetOptions{},
	)
}

func (r *Runner) ServiceSync() error {
	storeSvcs, err := r.serviceWatcher.List()
	if err != nil {
		return err
	}

	mirrorSvcList := []string{}
	for _, svc := range storeSvcs {
		mirrorSvcList = append(
			mirrorSvcList,
			generateMirrorName(r.prefix, svc.Namespace, svc.Name),
		)
	}

	currSvcs, err := r.mirrorServiceWatcher.List()
	if err != nil {
		return err
	}

	for _, svc := range currSvcs {
		_, inSlice := inSlice(mirrorSvcList, svc.Name)
		if !inSlice {
			log.Logger.Info(
				"Deleting old service and related endpoint",
				"service", svc.Name,
				"runner", r.name,
			)
			// Deleting a service should also clear the related
			// endpoints
			if err := r.deleteService(svc.Name, r.namespace); err != nil {
				log.Logger.Error(
					"Error clearing service",
					"service", svc.Name,
					"err", err,
					"runner", r.name,
				)
				return err
			}
		}
	}
	return nil
}

func (r *Runner) createService(name, namespace string, labels map[string]string, ports []v1.ServicePort, headless bool) (*v1.Service, error) {
	// Create clusterIP or headless type services. There is no reason to
	// create anything with an external ip.
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: v1.ServiceSpec{
			Ports:    ports,
			Selector: nil,
		},
	}
	if headless {
		svc.Spec.ClusterIP = "None"
	}
	return r.client.CoreV1().Services(r.namespace).Create(
		r.ctx,
		svc,
		metav1.CreateOptions{},
	)
}

func (r *Runner) updateService(service *v1.Service, ports []v1.ServicePort) (*v1.Service, error) {
	// only meaningful update on mirror service should be on ports, no need
	// to cater for headless as clusterIP field is immutable
	service.Spec.Ports = ports
	service.Spec.Selector = nil

	return r.client.CoreV1().Services(r.namespace).Update(
		r.ctx,
		service,
		metav1.UpdateOptions{},
	)
}

// updateGlobalService is UpdateService that will also update the global labels to reflect clusters
func (r *Runner) updateGlobalService(service *v1.Service, ports []v1.ServicePort, labels map[string]string) (*v1.Service, error) {
	for k, v := range labels {
		service.ObjectMeta.Labels[k] = v
	}

	return r.updateService(service, ports)
}

func (r *Runner) deleteService(name, namespace string) error {
	return r.client.CoreV1().Services(namespace).Delete(
		r.ctx,
		name,
		metav1.DeleteOptions{},
	)
}

func (r *Runner) ServiceEventHandler(eventType watch.EventType, old *v1.Service, new *v1.Service) {
	switch eventType {
	case watch.Added:
		log.Logger.Debug("service added", "namespace", new.Namespace, "name", new.Name, "runner", r.name)
		r.serviceQueue.Add(new)
		r.globalServiceQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("service modified", "namespace", new.Namespace, "name", new.Name, "runner", r.name)
		r.serviceQueue.Add(new)
		r.globalServiceQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("service deleted", "namespace", old.Namespace, "name", old.Name, "runner", r.name)
		r.serviceQueue.Add(old)
		r.globalServiceQueue.Add(old)
	default:
		log.Logger.Info("Unknown service event received: %v", eventType, "runner", r.name)
	}
}

func (r *Runner) reconcileEndpoints(name, namespace string) error {
	mirrorName := generateMirrorName(r.prefix, namespace, name)

	// Get the remote endpoints
	log.Logger.Info("getting remote endpoints", "namespace", namespace, "name", name, "runner", r.name)
	remoteEndpoints, err := r.getRemoteEndpoints(name, namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("remote endpoints not found, removing local endpoints", "namespace", namespace, "name", name, "runner", r.name)
		if err := r.deleteEndpoints(mirrorName, r.namespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting endpoints %s/%s: %v", r.namespace, mirrorName, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting remote endpoints %s/%s: %v", namespace, name, err)

	}

	// If the mirror endpoints doesn't exist, create it. Otherwise, update it.
	log.Logger.Info("getting local endpoints", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
	_, err = r.getEndpoints(mirrorName, r.namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("local endpoints not found, creating endpoints", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
		if _, err := r.createEndpoints(mirrorName, r.namespace, r.mirrorLabels, remoteEndpoints.Subsets); err != nil {
			return fmt.Errorf("creating endpoints %s/%s: %v", r.namespace, mirrorName, err)

		}
	} else if err != nil {
		return fmt.Errorf("getting endpoints %s/%s: %v", r.namespace, mirrorName, err)
	} else {
		log.Logger.Info("local endpoints found, updating endpoints", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
		if _, err := r.updateEndpoints(mirrorName, r.namespace, r.mirrorLabels, remoteEndpoints.Subsets); err != nil {
			return fmt.Errorf("updating endpoints %s/%s: %v", r.namespace, mirrorName, err)
		}
	}

	return nil
}

func (r *Runner) getRemoteEndpoints(name, namespace string) (*v1.Endpoints, error) {
	return r.endpointsWatcher.Get(name, namespace)
}

func (r *Runner) getEndpoints(name, namespace string) (*v1.Endpoints, error) {
	return r.client.CoreV1().Endpoints(namespace).Get(
		r.ctx,
		name,
		metav1.GetOptions{},
	)
}

func (r *Runner) createEndpoints(name, namespace string, labels map[string]string, subsets []v1.EndpointSubset) (*v1.Endpoints, error) {
	return r.client.CoreV1().Endpoints(namespace).Create(
		r.ctx,
		&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Subsets: subsets,
		},
		metav1.CreateOptions{},
	)
}

func (r *Runner) updateEndpoints(name, namespace string, labels map[string]string, subsets []v1.EndpointSubset) (*v1.Endpoints, error) {
	return r.client.CoreV1().Endpoints(namespace).Update(
		r.ctx,
		&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Subsets: subsets,
		},
		metav1.UpdateOptions{},
	)
}

func (r *Runner) deleteEndpoints(name, namespace string) error {
	return r.client.CoreV1().Endpoints(namespace).Delete(
		r.ctx,
		name,
		metav1.DeleteOptions{},
	)
}

func (r *Runner) EndpointsEventHandler(eventType watch.EventType, old *v1.Endpoints, new *v1.Endpoints) {
	switch eventType {
	case watch.Added:
		log.Logger.Debug("endpoints added", "namespace", new.Namespace, "name", new.Name, "runner", r.name)
		r.endpointsQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("endpoints modified", "namespace", new.Namespace, "name", new.Name, "runner", r.name)
		r.endpointsQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("endpoints deleted", "namespace", old.Namespace, "name", old.Name, "runner", r.name)
		r.endpointsQueue.Add(old)
	default:
		log.Logger.Info("Unknown endpoints event received: %v", eventType, "runner", r.name)
	}
}

func (r *Runner) getRemoteEndpointSlice(name, namespace string) (*discoveryv1.EndpointSlice, error) {
	return r.endpointSliceWatcher.Get(name, namespace)
}

func (r *Runner) getEndpointSlice(name, namespace string) (*discoveryv1.EndpointSlice, error) {
	return r.client.DiscoveryV1().EndpointSlices(namespace).Get(
		r.ctx,
		name,
		metav1.GetOptions{},
	)
}

func (r *Runner) createEndpointSlice(name, namespace, targetService string, at discoveryv1.AddressType, endpoints []discoveryv1.Endpoint, ports []discoveryv1.EndpointPort) (*discoveryv1.EndpointSlice, error) {
	labels := r.mirrorLabels
	labels["kubernetes.io/service-name"] = targetService
	return r.client.DiscoveryV1().EndpointSlices(namespace).Create(
		r.ctx,
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			AddressType: at,
			Endpoints:   endpoints,
			Ports:       ports,
		},
		metav1.CreateOptions{},
	)
}

func (r *Runner) updateEndpointSlice(name, namespace, targetService string, endpoints []discoveryv1.Endpoint, ports []discoveryv1.EndpointPort) (*discoveryv1.EndpointSlice, error) {
	labels := r.mirrorLabels
	labels["kubernetes.io/service-name"] = targetService
	// Usually no point updating address type.
	return r.client.DiscoveryV1().EndpointSlices(namespace).Update(
		r.ctx,
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Endpoints: endpoints,
			Ports:     ports,
		},
		metav1.UpdateOptions{},
	)
}

func (r *Runner) deleteEndpointSlice(name, namespace string) error {
	return r.client.DiscoveryV1().EndpointSlices(namespace).Delete(
		r.ctx,
		name,
		metav1.DeleteOptions{},
	)
}
func (r *Runner) reconcileEndpointSlice(name, namespace string) error {
	// Just prefix the name with gl, since we will be using the mirrors for global services
	mirrorName := fmt.Sprintf("gl-%s", name)
	// Get the remote endpointslice
	log.Logger.Info("getting remote endpointslice", "namespace", namespace, "name", name, "runner", r.name)
	remoteEndpointSlice, err := r.getRemoteEndpointSlice(name, namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("remote endpointslice not found, removing local mirror", "namespace", namespace, "name", name, "runner", r.name)
		if err := r.deleteEndpointSlice(mirrorName, r.namespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting endpointslice %s/%s: %v", r.namespace, mirrorName, err)
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
	log.Logger.Info("getting local endpointslice", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
	_, err = r.getEndpointSlice(mirrorName, r.namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("local endpointslice not found, creating", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
		if _, err := r.createEndpointSlice(mirrorName, r.namespace, targetGlobalService, remoteEndpointSlice.AddressType, remoteEndpointSlice.Endpoints, remoteEndpointSlice.Ports); err != nil {
			return fmt.Errorf("creating endpointslice %s/%s: %v", r.namespace, mirrorName, err)

		}
	} else if err != nil {
		return fmt.Errorf("getting endpointslice %s/%s: %v", r.namespace, mirrorName, err)
	} else {
		log.Logger.Info("local endpointslice found, updating", "namespace", r.namespace, "name", mirrorName, "runner", r.name)
		if _, err := r.updateEndpointSlice(mirrorName, r.namespace, targetGlobalService, remoteEndpointSlice.Endpoints, remoteEndpointSlice.Ports); err != nil {
			return fmt.Errorf("updating endpointslice %s/%s: %v", r.namespace, mirrorName, err)
		}
	}
	return nil
}

func (r *Runner) EndpointSliceEventHandler(eventType watch.EventType, old *discoveryv1.EndpointSlice, new *discoveryv1.EndpointSlice) {
	switch eventType {
	case watch.Added:
		log.Logger.Debug("endpoints added", "namespace", new.Namespace, "name", new.Name, "runner", r.name)
		r.endpointSliceQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("endpoints modified", "namespace", new.Namespace, "name", new.Name, "runner", r.name)
		r.endpointSliceQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("endpoints deleted", "namespace", old.Namespace, "name", old.Name, "runner", r.name)
		r.endpointSliceQueue.Add(old)
	default:
		log.Logger.Info("Unknown endpoints event received: %v", eventType, "runner", r.name)
	}
}
