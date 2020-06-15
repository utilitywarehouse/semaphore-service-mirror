package kube

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/utilitywarehouse/kube-service-mirror/log"
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
	ListHealthy   bool
	WatchHealthy  bool
}

func NewServiceWatcher(client kubernetes.Interface, resyncPeriod time.Duration, handler ServiceEventHandler, labelSelector string) *ServiceWatcher {
	return &ServiceWatcher{
		ctx:           context.Background(),
		client:        client,
		resyncPeriod:  resyncPeriod,
		stopChannel:   make(chan struct{}),
		eventHandler:  handler,
		labelSelector: labelSelector,
	}
}

func (sw *ServiceWatcher) Init() {
	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = sw.labelSelector
			l, err := sw.client.CoreV1().Services(metav1.NamespaceAll).List(sw.ctx, options)
			if err != nil {
				log.Logger.Error("sw: list error", "err", err)
				sw.ListHealthy = false
			} else {
				sw.ListHealthy = true
			}
			return l, err
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = sw.labelSelector
			w, err := sw.client.CoreV1().Services(metav1.NamespaceAll).Watch(sw.ctx, options)
			if err != nil {
				log.Logger.Error("sw: watch error", "err", err)
				sw.WatchHealthy = false
			} else {
				sw.WatchHealthy = true
			}
			return w, err
		},
	}
	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			sw.eventHandler(watch.Added, nil, obj.(*v1.Service))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			sw.eventHandler(watch.Modified, oldObj.(*v1.Service), newObj.(*v1.Service))
		},
		DeleteFunc: func(obj interface{}) {
			sw.eventHandler(watch.Deleted, obj.(*v1.Service), nil)
		},
	}
	sw.store, sw.controller = cache.NewInformer(listWatch, &v1.Service{}, sw.resyncPeriod, eventHandler)
}

func (sw *ServiceWatcher) Run() {
	log.Logger.Info("starting service watcher")
	// Running controller will block until writing on the stop channel.
	sw.controller.Run(sw.stopChannel)
	log.Logger.Info("stopped service watcher")
}

func (sw *ServiceWatcher) Stop() {
	log.Logger.Info("stopping service watcher")
	close(sw.stopChannel)
}

func (sw *ServiceWatcher) HasSynced() bool {
	return sw.controller.HasSynced()
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

func (sw *ServiceWatcher) Healthy() bool {
	if sw.ListHealthy && sw.WatchHealthy {
		return true
	}
	return false
}
