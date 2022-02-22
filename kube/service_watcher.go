package kube

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/utilitywarehouse/semaphore-service-mirror/log"
	"github.com/utilitywarehouse/semaphore-service-mirror/metrics"
)

type ServiceEventHandler = func(eventType watch.EventType, old *v1.Service, new *v1.Service)

type ServiceWatcher struct {
	ctx           context.Context
	client        kubernetes.Interface
	resyncPeriod  time.Duration
	stopChannel   chan struct{}
	store         cache.Store
	controller    cache.Controller
	eventHandler  ServiceEventHandler
	labelSelector string
	name          string
	namespace     string
	runner        string // Name of the parent runner of the watcher. Used for metrics to distinguish series.
}

func NewServiceWatcher(name string, client kubernetes.Interface, resyncPeriod time.Duration, handler ServiceEventHandler, labelSelector, namespace, runner string) *ServiceWatcher {
	return &ServiceWatcher{
		ctx:           context.Background(),
		client:        client,
		resyncPeriod:  resyncPeriod,
		stopChannel:   make(chan struct{}),
		eventHandler:  handler,
		labelSelector: labelSelector,
		name:          name,
		namespace:     namespace,
		runner:        runner,
	}
}

func (sw *ServiceWatcher) Init() {
	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = sw.labelSelector
			l, err := sw.client.CoreV1().Services(sw.namespace).List(sw.ctx, options)
			if err != nil {
				log.Logger.Error("service list error", "watcher", sw.name, "err", err)
			}
			return l, err
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = sw.labelSelector
			w, err := sw.client.CoreV1().Services(sw.namespace).Watch(sw.ctx, options)
			if err != nil {
				log.Logger.Error("service watch error", "watcher", sw.name, "err", err)
			}
			return w, err
		},
	}
	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			sw.handleEvent(watch.Added, nil, obj.(*v1.Service))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			sw.handleEvent(watch.Modified, oldObj.(*v1.Service), newObj.(*v1.Service))
		},
		DeleteFunc: func(obj interface{}) {
			sw.handleEvent(watch.Deleted, obj.(*v1.Service), nil)
		},
	}
	sw.store, sw.controller = cache.NewInformer(listWatch, &v1.Service{}, sw.resyncPeriod, eventHandler)
}

func (sw *ServiceWatcher) handleEvent(eventType watch.EventType, oldObj, newObj *v1.Service) {
	metrics.IncKubeWatcherEvents(sw.name, "service", sw.runner, eventType)
	metrics.SetKubeWatcherObjects(sw.name, "service", sw.runner, float64(len(sw.store.List())))

	if sw.eventHandler != nil {
		sw.eventHandler(eventType, oldObj, newObj)
	}
}

func (sw *ServiceWatcher) Run() {
	log.Logger.Info("starting service watcher", "watcher", sw.name)
	// Running controller will block until writing on the stop channel.
	sw.controller.Run(sw.stopChannel)
	log.Logger.Info("stopped service watcher", "watcher", sw.name)
}

func (sw *ServiceWatcher) Stop() {
	log.Logger.Info("stopping service watcher", "watcher", sw.name)
	close(sw.stopChannel)
}

func (sw *ServiceWatcher) HasSynced() bool {
	return sw.controller.HasSynced()
}

func (sw *ServiceWatcher) Get(name, namespace string) (*v1.Service, error) {
	key := namespace + "/" + name

	obj, exists, err := sw.store.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("service"), key)
	}

	return obj.(*v1.Service), nil
}

func (sw *ServiceWatcher) List() ([]*v1.Service, error) {
	var svcs []*v1.Service
	for _, obj := range sw.store.List() {
		svc, ok := obj.(*v1.Service)
		if !ok {
			return nil, fmt.Errorf("unexpected object in store: %+v", obj)
		}
		svcs = append(svcs, svc)
	}
	return svcs, nil
}
