package main

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/utilitywarehouse/kube-service-mirror/kube"
	"github.com/utilitywarehouse/kube-service-mirror/log"
)

var (
	MirrorLabels = map[string]string{"mirrored-svc": "true"}
)

const (
	SEPARATOR = "6d6972726f720a"
)

type Runner struct {
	client           kubernetes.Interface
	serviceWatcher   *kube.ServiceWatcher
	endpointsWatcher *kube.EndpointsWatcher
	namespace        string
	prefix           string
	labelselector    string
	sync             bool
}

func NewRunner(client, watchClient kubernetes.Interface, namespace, prefix, labelselector string, resyncPeriod time.Duration, sync bool) *Runner {
	runner := &Runner{
		client:    client,
		namespace: namespace,
		prefix:    prefix,
		sync:      sync,
	}

	// Create and initialize a service wathcer
	serviceWatcher := kube.NewServiceWatcher(
		watchClient,
		resyncPeriod,
		runner.ServiceEventHandler,
		labelselector,
	)
	runner.serviceWatcher = serviceWatcher
	runner.serviceWatcher.Init()

	// Create and initialize a service wathcer
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
	return nil
}

func (r *Runner) Stop() {
	r.serviceWatcher.Stop()
	r.endpointsWatcher.Stop()
}

func (r *Runner) generateMirrorName(name, namespace string) string {
	if r.prefix != "" {
		return fmt.Sprintf("%s-%s-%s-%s", r.prefix, name, SEPARATOR, namespace)
	}
	return fmt.Sprintf("%s-%s-%s", name, SEPARATOR, namespace)
}

func (r *Runner) getService(name, namespace string) (*v1.Service, error) {
	return r.client.CoreV1().Services(namespace).Get(
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
	currSvcs, err := r.client.CoreV1().Services(r.namespace).List(options)
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
			err := r.client.CoreV1().Services(r.namespace).Delete(
				svc.Name,
				&metav1.DeleteOptions{},
			)
			if err != nil {
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
	return r.client.CoreV1().Services(r.namespace).Create(svc)
}

func (r *Runner) updateService(service *v1.Service, ports []v1.ServicePort) (*v1.Service, error) {
	// only meaningful update on mirror service should be on ports, no need
	// to cater for headless as clusterIP field is immutable
	service.Spec.Ports = ports
	service.Spec.Selector = nil

	return r.client.CoreV1().Services(r.namespace).Update(service)
}

func (r *Runner) onServiceAdd(new *v1.Service) {
	name := r.generateMirrorName(new.Name, new.Namespace)

	svc, err := r.getService(name, r.namespace)
	if err != nil {
		log.Logger.Info(
			"cannot get service will try to create",
			"service", name,
		)

		_, err := r.createService(name, r.namespace, MirrorLabels, new.Spec.Ports, isHeadless(new))
		if err != nil {
			log.Logger.Error(
				"failed to create mirror service",
				"service", new.Name,
				"err", err,
			)
		}
	} else {
		log.Logger.Info(
			"service already there will try updating",
			"service", name,
		)
		_, err := r.updateService(svc, new.Spec.Ports)
		if err != nil {
			log.Logger.Error(
				"failed to update existing mirror service on add",
				"service", new.Name,
				"err", err,
			)
		}
	}
	return
}

func (r *Runner) onServiceModify(new *v1.Service) {
	name := r.generateMirrorName(new.Name, new.Namespace)
	svc, err := r.getService(name, r.namespace)
	if err != nil {
		log.Logger.Error(
			"cannot get service to update",
			"service", name,
		)
		return
	}
	_, err = r.updateService(svc, new.Spec.Ports)
	if err != nil {
		log.Logger.Error(
			"failed to update mirror service",
			"service", new.Name,
			"err", err,
		)
	}
	return
}

func (r *Runner) onServiceDelete(old *v1.Service) {
	name := r.generateMirrorName(old.Name, old.Namespace)
	err := r.client.CoreV1().Services(r.namespace).Delete(
		name,
		&metav1.DeleteOptions{},
	)
	if err != nil {
		log.Logger.Error(
			"failed to delete mirror service",
			"service", old.Name,
			"err", err,
		)
	}
	return
}
func (r *Runner) ServiceEventHandler(eventType watch.EventType, old *v1.Service, new *v1.Service) {
	switch eventType {
	case watch.Added:
		r.onServiceAdd(new)
	case watch.Modified:
		r.onServiceModify(new)
	case watch.Deleted:
		r.onServiceDelete(old)
	default:
		log.Logger.Info(
			"Unknown service event received: %v",
			eventType,
		)
	}
}

func (r *Runner) getEndpoints(name, namespace string) (*v1.Endpoints, error) {
	return r.client.CoreV1().Endpoints(namespace).Get(
		name,
		metav1.GetOptions{},
	)
}

func (r *Runner) createEndpoints(name, namespace string, labels map[string]string, subsets []v1.EndpointSubset) (*v1.Endpoints, error) {
	return r.client.CoreV1().Endpoints(namespace).Create(
		&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Subsets: subsets,
		})
}

func (r *Runner) updateEndpoints(name, namespace string, labels map[string]string, subsets []v1.EndpointSubset) (*v1.Endpoints, error) {
	return r.client.CoreV1().Endpoints(namespace).Update(
		&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Subsets: subsets,
		})
}

func (r *Runner) onEndpointsAdd(new *v1.Endpoints) {
	name := r.generateMirrorName(new.Name, new.Namespace)
	_, err := r.getEndpoints(name, r.namespace)
	if err != nil {
		log.Logger.Info(
			"cannot get endpoints will try to create",
			"endpoints", name,
		)
		_, err = r.createEndpoints(name, r.namespace, MirrorLabels, new.Subsets)
		if err != nil {
			log.Logger.Error(
				"failed to create mirror endpoints",
				"endpoints", new.Name,
				"err", err,
			)
		}
	} else {
		log.Logger.Info(
			"endpoints found will try to update",
			"endpoints", name,
		)
		_, err = r.updateEndpoints(name, r.namespace, MirrorLabels, new.Subsets)
		if err != nil {
			log.Logger.Error(
				"failed to update mirror endpoints",
				"endpoints", new.Name,
				"err", err,
			)
		}
	}
}

func (r *Runner) onEndpointsModify(new *v1.Endpoints) {
	name := r.generateMirrorName(new.Name, new.Namespace)
	_, err := r.updateEndpoints(name, r.namespace, MirrorLabels, new.Subsets)
	if err != nil {
		log.Logger.Error(
			"failed to update mirror endpoints",
			"endpoints", new.Name,
			"err", err,
		)
	}
	return
}

func (r *Runner) onEndpointsDelete(old *v1.Endpoints) {
	name := r.generateMirrorName(old.Name, old.Namespace)
	err := r.client.CoreV1().Endpoints(r.namespace).Delete(
		name,
		&metav1.DeleteOptions{},
	)
	if err != nil {
		log.Logger.Error(
			"failed to delete mirror endpoints",
			"endpoints", old.Name,
			"err", err,
		)
	}
	return
}

func (r *Runner) EndpointsEventHandler(eventType watch.EventType, old *v1.Endpoints, new *v1.Endpoints) {
	switch eventType {
	case watch.Added:
		r.onEndpointsAdd(new)
	case watch.Modified:
		r.onEndpointsModify(new)
	case watch.Deleted:
		r.onEndpointsDelete(old)
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
