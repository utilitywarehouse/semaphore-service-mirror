package main

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/utilitywarehouse/semaphore-service-mirror/kube"
	"github.com/utilitywarehouse/semaphore-service-mirror/log"
)

// MirrorRunner watches a remote cluster and mirrors services and endpoints locally
type MirrorRunner struct {
	ctx                    context.Context
	client                 kubernetes.Interface
	serviceQueue           *queue
	serviceWatcher         *kube.ServiceWatcher
	mirrorServiceWatcher   *kube.ServiceWatcher
	endpointsQueue         *queue
	endpointsWatcher       *kube.EndpointsWatcher
	mirrorEndpointsWatcher *kube.EndpointsWatcher
	mirrorLabels           map[string]string
	name                   string
	namespace              string
	prefix                 string
	labelselector          string
	sync                   bool
	initialised            bool // Flag to turn on after the successful initialisation of the runner.
}

func NewMirrorRunner(client, watchClient kubernetes.Interface, name, namespace, prefix, labelselector string, resyncPeriod time.Duration, sync bool) *MirrorRunner {
	mirrorLabels := map[string]string{
		"mirrored-svc":           "true",
		"mirror-svc-prefix-sync": prefix,
	}
	runner := &MirrorRunner{
		ctx:          context.Background(),
		client:       client,
		name:         name,
		namespace:    namespace,
		prefix:       prefix,
		sync:         sync,
		mirrorLabels: mirrorLabels,
		initialised:  false,
	}
	runner.serviceQueue = newQueue(fmt.Sprintf("%s-service", name), runner.reconcileService)
	runner.endpointsQueue = newQueue(fmt.Sprintf("%s-endpoints", name), runner.reconcileEndpoints)

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

	return runner
}

func (mr *MirrorRunner) Run() error {
	go mr.serviceWatcher.Run()
	go mr.mirrorServiceWatcher.Run()
	// At this point the runner should be considered initialised and live.
	mr.initialised = true
	// wait for service watcher to sync before starting the endpoints to
	// avoid race between them. TODO: atm dummy and could run forever if
	// services cache fails to sync
	stopCh := make(chan struct{})
	if ok := cache.WaitForNamedCacheSync("serviceWatcher", stopCh, mr.serviceWatcher.HasSynced); !ok {
		return fmt.Errorf("failed to wait for service caches to sync")
	}
	if ok := cache.WaitForNamedCacheSync("mirrorServiceWatcher", stopCh, mr.mirrorServiceWatcher.HasSynced); !ok {
		return fmt.Errorf("failed to wait for mirror service caches to sync")
	}

	// After services store syncs, perform a sync to delete stale mirrors
	if mr.sync {
		log.Logger.Info("Syncing services", "runner", mr.name)
		if err := mr.ServiceSync(); err != nil {
			log.Logger.Warn(
				"Error syncing services, skipping..",
				"err", err,
				"runner", mr.name,
			)
		}
	}
	go mr.endpointsWatcher.Run()
	go mr.mirrorEndpointsWatcher.Run()

	go mr.serviceQueue.Run()
	go mr.endpointsQueue.Run()

	return nil
}

func (mr *MirrorRunner) Stop() {
	mr.serviceQueue.Stop()
	mr.serviceWatcher.Stop()
	mr.mirrorServiceWatcher.Stop()
	mr.endpointsQueue.Stop()
	mr.endpointsWatcher.Stop()
	mr.mirrorEndpointsWatcher.Stop()
}

func (mr *MirrorRunner) Initialised() bool {
	return mr.initialised
}

func (mr *MirrorRunner) reconcileService(name, namespace string) error {
	mirrorName := generateMirrorName(mr.prefix, namespace, name)

	// Get the remote service
	log.Logger.Info("getting remote service", "namespace", namespace, "name", name, "runner", mr.name)
	remoteSvc, err := mr.getRemoteService(name, namespace)
	if errors.IsNotFound(err) {
		// If the remote service doesn't exist, clean up the local mirror service (if it
		// exists)
		log.Logger.Info("remote service not found, deleting local service", "namespace", mr.namespace, "name", mirrorName, "runner", mr.name)
		if err := kube.DeleteService(mr.ctx, mr.client, mirrorName, mr.namespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting service %s/%s: %v", mr.namespace, mirrorName, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting remote service: %v", err)
	}

	// If the mirror service doesn't exist, create it. Otherwise, update it.
	mirrorSvc, err := kube.GetService(mr.ctx, mr.client, mirrorName, mr.namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("local service not found, creating service", "namespace", mr.namespace, "name", mirrorName, "runner", mr.name)
		if _, err := kube.CreateService(mr.ctx, mr.client, mirrorName, mr.namespace, mr.mirrorLabels, map[string]string{}, remoteSvc.Spec.Ports, isHeadless(remoteSvc)); err != nil {
			return fmt.Errorf("creating service %s/%s: %v", mr.namespace, mirrorName, err)
		}
	} else if err != nil {
		return fmt.Errorf("getting service %s/%s: %v", mr.namespace, mirrorName, err)
	} else {
		log.Logger.Info("local service found, updating service", "namespace", mr.namespace, "name", mirrorName, "runner", mr.name)
		if _, err := kube.UpdateService(mr.ctx, mr.client, mirrorSvc, remoteSvc.Spec.Ports); err != nil {
			return fmt.Errorf("updating service %s/%s: %v", mr.namespace, mirrorName, err)
		}
	}

	return nil
}

func (mr *MirrorRunner) getRemoteService(name, namespace string) (*v1.Service, error) {
	return mr.serviceWatcher.Get(name, namespace)
}

func (mr *MirrorRunner) ServiceSync() error {
	storeSvcs, err := mr.serviceWatcher.List()
	if err != nil {
		return err
	}

	mirrorSvcList := []string{}
	for _, svc := range storeSvcs {
		mirrorSvcList = append(
			mirrorSvcList,
			generateMirrorName(mr.prefix, svc.Namespace, svc.Name),
		)
	}

	currSvcs, err := mr.mirrorServiceWatcher.List()
	if err != nil {
		return err
	}

	for _, svc := range currSvcs {
		_, inSlice := inSlice(mirrorSvcList, svc.Name)
		if !inSlice {
			log.Logger.Info(
				"Deleting old service and related endpoint",
				"service", svc.Name,
				"runner", mr.name,
			)
			// Deleting a service should also clear the related
			// endpoints
			if err := kube.DeleteService(mr.ctx, mr.client, svc.Name, mr.namespace); err != nil {
				log.Logger.Error(
					"Error clearing service",
					"service", svc.Name,
					"err", err,
					"runner", mr.name,
				)
				return err
			}
		}
	}
	return nil
}

func (mr *MirrorRunner) ServiceEventHandler(eventType watch.EventType, old *v1.Service, new *v1.Service) {
	switch eventType {
	case watch.Added:
		log.Logger.Debug("service added", "namespace", new.Namespace, "name", new.Name, "runner", mr.name)
		mr.serviceQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("service modified", "namespace", new.Namespace, "name", new.Name, "runner", mr.name)
		mr.serviceQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("service deleted", "namespace", old.Namespace, "name", old.Name, "runner", mr.name)
		mr.serviceQueue.Add(old)
	default:
		log.Logger.Info("Unknown service event received: %v", eventType, "runner", mr.name)
	}
}

func (mr *MirrorRunner) reconcileEndpoints(name, namespace string) error {
	mirrorName := generateMirrorName(mr.prefix, namespace, name)

	// Get the remote endpoints
	log.Logger.Info("getting remote endpoints", "namespace", namespace, "name", name, "runner", mr.name)
	remoteEndpoints, err := mr.getRemoteEndpoints(name, namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("remote endpoints not found, removing local endpoints", "namespace", namespace, "name", name, "runner", mr.name)
		if err := mr.deleteEndpoints(mirrorName, mr.namespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting endpoints %s/%s: %v", mr.namespace, mirrorName, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting remote endpoints %s/%s: %v", namespace, name, err)
	}

	// If the mirror endpoints doesn't exist, create it. Otherwise, update it.
	log.Logger.Info("getting local endpoints", "namespace", mr.namespace, "name", mirrorName, "runner", mr.name)
	_, err = mr.getEndpoints(mirrorName, mr.namespace)
	if errors.IsNotFound(err) {
		log.Logger.Info("local endpoints not found, creating endpoints", "namespace", mr.namespace, "name", mirrorName, "runner", mr.name)
		if _, err := mr.createEndpoints(mirrorName, mr.namespace, mr.mirrorLabels, remoteEndpoints.Subsets); err != nil {
			return fmt.Errorf("creating endpoints %s/%s: %v", mr.namespace, mirrorName, err)
		}
	} else if err != nil {
		return fmt.Errorf("getting endpoints %s/%s: %v", mr.namespace, mirrorName, err)
	} else {
		log.Logger.Info("local endpoints found, updating endpoints", "namespace", mr.namespace, "name", mirrorName, "runner", mr.name)
		if _, err := mr.updateEndpoints(mirrorName, mr.namespace, mr.mirrorLabels, remoteEndpoints.Subsets); err != nil {
			return fmt.Errorf("updating endpoints %s/%s: %v", mr.namespace, mirrorName, err)
		}
	}
	return nil
}

func (mr *MirrorRunner) getRemoteEndpoints(name, namespace string) (*v1.Endpoints, error) {
	return mr.endpointsWatcher.Get(name, namespace)
}

func (mr *MirrorRunner) getEndpoints(name, namespace string) (*v1.Endpoints, error) {
	return mr.client.CoreV1().Endpoints(namespace).Get(
		mr.ctx,
		name,
		metav1.GetOptions{},
	)
}

func (mr *MirrorRunner) createEndpoints(name, namespace string, labels map[string]string, subsets []v1.EndpointSubset) (*v1.Endpoints, error) {
	return mr.client.CoreV1().Endpoints(namespace).Create(
		mr.ctx,
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

func (mr *MirrorRunner) updateEndpoints(name, namespace string, labels map[string]string, subsets []v1.EndpointSubset) (*v1.Endpoints, error) {
	return mr.client.CoreV1().Endpoints(namespace).Update(
		mr.ctx,
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

func (mr *MirrorRunner) deleteEndpoints(name, namespace string) error {
	return mr.client.CoreV1().Endpoints(namespace).Delete(
		mr.ctx,
		name,
		metav1.DeleteOptions{},
	)
}

func (mr *MirrorRunner) EndpointsEventHandler(eventType watch.EventType, old *v1.Endpoints, new *v1.Endpoints) {
	switch eventType {
	case watch.Added:
		log.Logger.Debug("endpoints added", "namespace", new.Namespace, "name", new.Name, "runner", mr.name)
		mr.endpointsQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("endpoints modified", "namespace", new.Namespace, "name", new.Name, "runner", mr.name)
		mr.endpointsQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("endpoints deleted", "namespace", old.Namespace, "name", old.Name, "runner", mr.name)
		mr.endpointsQueue.Add(old)
	default:
		log.Logger.Info("Unknown endpoints event received: %v", eventType, "runner", mr.name)
	}
}
