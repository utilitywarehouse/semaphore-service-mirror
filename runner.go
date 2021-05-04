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

var (
	MirrorLabels = map[string]string{"mirrored-svc": "true"}
)

const (
	SEPARATOR = "6d6972726f720a"
)

type Runner struct {
	ctx              context.Context
	client           kubernetes.Interface
	serviceQueue     *queue
	serviceWatcher   *kube.ServiceWatcher
	endpointsQueue   *queue
	endpointsWatcher *kube.EndpointsWatcher
	namespace        string
	prefix           string
	labelselector    string
	sync             bool
}

func NewRunner(client, watchClient kubernetes.Interface, namespace, prefix, labelselector string, resyncPeriod time.Duration, sync bool) *Runner {
	runner := &Runner{
		ctx:       context.Background(),
		client:    client,
		namespace: namespace,
		prefix:    prefix,
		sync:      sync,
	}
	runner.serviceQueue = newQueue("service", runner.reconcileService)
	runner.endpointsQueue = newQueue("endpoints", runner.reconcileEndpoints)

	// Create and initialize a service watcher
	serviceWatcher := kube.NewServiceWatcher(
		watchClient,
		resyncPeriod,
		runner.ServiceEventHandler,
		labelselector,
	)
	runner.serviceWatcher = serviceWatcher
	runner.serviceWatcher.Init()

	// Create and initialize an endpoints watcher
	endpointsWatcher := kube.NewEndpointsWatcher(
		watchClient,
		resyncPeriod,
		runner.EndpointsEventHandler,
		labelselector,
	)
	runner.endpointsWatcher = endpointsWatcher
	runner.endpointsWatcher.Init()

	return runner
}

func (r *Runner) Run() error {
	go r.serviceWatcher.Run()
	// wait for service watcher to sync before starting the endpoints to
	// avoid race between them. TODO: atm dummy and could run forever if
	// serviceis cache fails to sync
	stopCh := make(chan struct{})
	if ok := cache.WaitForNamedCacheSync("serviceWatcher", stopCh, r.serviceWatcher.HasSynced); !ok {
		return fmt.Errorf("failed to wait for service caches to sync")
	}

	// After services store syncs, perform a sync to delete stale mirrors
	if r.sync {
		log.Logger.Info("Syncing services")
		if err := r.ServiceSync(); err != nil {
			log.Logger.Warn(
				"Error syncing services, skipping..",
				"err", err,
			)
		}
	}
	go r.endpointsWatcher.Run()

	go r.serviceQueue.Run()
	go r.endpointsQueue.Run()

	return nil
}

func (r *Runner) Stop() {
	r.serviceQueue.Stop()
	r.serviceWatcher.Stop()
	r.endpointsQueue.Stop()
	r.endpointsWatcher.Stop()
}

func (r *Runner) generateMirrorName(name, namespace string) string {
	if r.prefix != "" {
		return fmt.Sprintf("%s-%s-%s-%s", r.prefix, name, SEPARATOR, namespace)
	}
	return fmt.Sprintf("%s-%s-%s", name, SEPARATOR, namespace)
}

func (r *Runner) reconcileService(name, namespace string) error {
	mirrorName := r.generateMirrorName(name, namespace)

	// Get the remote service
	log.Logger.Info("getting remote service", "namespace", namespace, "name", name)
	remoteSvc, err := r.getRemoteService(name, namespace)
	if err != nil {
		return fmt.Errorf("getting remote service: %v", err)
	}
	// If the remote service doesn't exist, clean up the local mirror service (if it
	// exists)
	if remoteSvc == nil {
		log.Logger.Info("remote service not found, deleting local service", "namespace", r.namespace, "name", mirrorName)
		if err := r.deleteService(mirrorName, r.namespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting service %s/%s: %v", r.namespace, mirrorName, err)
		}
		return nil
	}

	// If the mirror service doesn't exist, create it. Otherwise, update it.
	if mirrorSvc, err := r.getService(mirrorName, r.namespace); errors.IsNotFound(err) {
		log.Logger.Info("local service not found, creating service", "namespace", r.namespace, "name", mirrorName)
		if _, err := r.createService(mirrorName, r.namespace, MirrorLabels, remoteSvc.Spec.Ports, isHeadless(remoteSvc)); err != nil {
			return fmt.Errorf("creating service %s/%s: %v", r.namespace, mirrorName, err)
		}
	} else if err != nil {
		return fmt.Errorf("getting service %s/%s: %v", r.namespace, mirrorName, err)
	} else {
		log.Logger.Info("local service found, updating service", "namespace", r.namespace, "name", mirrorName)
		if _, err := r.updateService(mirrorSvc, remoteSvc.Spec.Ports); err != nil {
			return fmt.Errorf("updating service %s/%s: %v", r.namespace, mirrorName, err)
		}
	}

	return nil
}

func (r *Runner) getRemoteService(name, namespace string) (*v1.Service, error) {
	svcs, err := r.serviceWatcher.List()
	if err != nil {
		return nil, err
	}
	for _, svc := range svcs {
		if svc.Namespace == namespace && svc.Name == name {
			return svc, nil
		}
	}

	return nil, nil
}

func (r *Runner) getService(name, namespace string) (*v1.Service, error) {
	return r.client.CoreV1().Services(namespace).Get(
		r.ctx,
		name,
		metav1.GetOptions{},
	)
}

func isHeadless(svc *v1.Service) bool {
	if svc.Spec.ClusterIP == "None" {
		return true
	}
	return false
}

func isInList(s string, l []string) bool {
	for _, el := range l {
		if el == s {
			return true
		}
	}
	return false
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
			r.generateMirrorName(svc.Name, svc.Namespace),
		)
	}

	options := metav1.ListOptions{
		LabelSelector: labels.Set(MirrorLabels).String(),
	}
	currSvcs, err := r.client.CoreV1().Services(r.namespace).List(r.ctx, options)
	if err != nil {
		return err
	}

	for _, svc := range currSvcs.Items {
		if !isInList(svc.Name, mirrorSvcList) {
			log.Logger.Info(
				"Deleting old service and related endpoint",
				"service", svc.Name,
			)
			// Deleting a service should also clear the related
			// endpoints
			if err := r.deleteService(svc.Name, r.namespace); err != nil {
				log.Logger.Error(
					"Error clearing service",
					"service", svc.Name,
					"err", err,
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
		log.Logger.Debug("service added", "namespace", new.Namespace, "name", new.Name)
		r.serviceQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("service modified", "namespace", new.Namespace, "name", new.Name)
		r.serviceQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("service deleted", "namespace", old.Namespace, "name", old.Name)
		r.serviceQueue.Add(old)
	default:
		log.Logger.Info(
			"Unknown service event received: %v",
			eventType,
		)
	}
}

func (r *Runner) reconcileEndpoints(name, namespace string) error {
	mirrorName := r.generateMirrorName(name, namespace)

	// Get the remote endpoints
	log.Logger.Info("getting remote endpoints", "namespace", namespace, "name", name)
	remoteEndpoints, err := r.getRemoteEndpoints(name, namespace)
	if err != nil {
		return fmt.Errorf("getting remote endpoints %s/%s: %v", namespace, name, err)
	}
	// If the endpoints doesn't exist, clean up the local mirror endpoints (if it
	// exists)
	if remoteEndpoints == nil {
		log.Logger.Info("remote endpoints not found, removing local endpoints", "namespace", namespace, "name", name)
		if err := r.deleteEndpoints(mirrorName, r.namespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting endpoints %s/%s: %v", r.namespace, mirrorName, err)
		}
		return nil
	}

	// If the mirror endpoints doesn't exist, create it. Otherwise, update it.
	log.Logger.Info("getting local endpoints", "namespace", r.namespace, "name", mirrorName)
	if _, err := r.getEndpoints(mirrorName, r.namespace); errors.IsNotFound(err) {
		log.Logger.Info("local endpoints not found, creating endpoints", "namespace", r.namespace, "name", mirrorName)
		if _, err := r.createEndpoints(mirrorName, r.namespace, MirrorLabels, remoteEndpoints.Subsets); err != nil {
			return fmt.Errorf("creating endpoints %s/%s: %v", r.namespace, mirrorName, err)

		}
	} else if err != nil {
		return fmt.Errorf("getting endpoints %s/%s: %v", r.namespace, mirrorName, err)
	} else {
		log.Logger.Info("local endpoints found, updating endpoints", "namespace", r.namespace, "name", mirrorName)
		if _, err := r.updateEndpoints(mirrorName, r.namespace, MirrorLabels, remoteEndpoints.Subsets); err != nil {
			return fmt.Errorf("updating endpoints %s/%s: %v", r.namespace, mirrorName, err)
		}
	}

	return nil
}

func (r *Runner) getRemoteEndpoints(name, namespace string) (*v1.Endpoints, error) {
	endpoints, err := r.endpointsWatcher.List()
	if err != nil {
		return nil, err
	}
	for _, e := range endpoints {
		if e.Namespace == namespace && e.Name == name {
			return e, nil
		}
	}

	return nil, nil
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
		log.Logger.Debug("endpoints added", "namespace", new.Namespace, "name", new.Name)
		r.endpointsQueue.Add(new)
	case watch.Modified:
		log.Logger.Debug("endpoints modified", "namespace", new.Namespace, "name", new.Name)
		r.endpointsQueue.Add(new)
	case watch.Deleted:
		log.Logger.Debug("endpoints deleted", "namespace", old.Namespace, "name", old.Name)
		r.endpointsQueue.Add(old)
	default:
		log.Logger.Info(
			"Unknown endpoints event received: %v",
			eventType,
		)
	}
}

func (r *Runner) Healthy() bool {
	if r.serviceWatcher.Healthy() && r.endpointsWatcher.Healthy() {
		return true
	}
	return false
}
