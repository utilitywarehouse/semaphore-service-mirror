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
)

type EndpointsEventHandler = func(eventType watch.EventType, old *v1.Endpoints, new *v1.Endpoints)

type EndpointsWatcher struct {
	ctx           context.Context
	client        kubernetes.Interface
	resyncPeriod  time.Duration
	stopChannel   chan struct{}
	store         cache.Store
	controller    cache.Controller
	eventHandler  EndpointsEventHandler
	labelSelector string
	ListHealthy   bool
	WatchHealthy  bool
}

func NewEndpointsWatcher(client kubernetes.Interface, resyncPeriod time.Duration, handler EndpointsEventHandler, labelSelector string) *EndpointsWatcher {
	return &EndpointsWatcher{
		ctx:           context.Background(),
		client:        client,
		resyncPeriod:  resyncPeriod,
		stopChannel:   make(chan struct{}),
		eventHandler:  handler,
		labelSelector: labelSelector,
	}
}

func (ew *EndpointsWatcher) Init() {
	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = ew.labelSelector
			l, err := ew.client.CoreV1().Endpoints(metav1.NamespaceAll).List(ew.ctx, options)
			if err != nil {
				log.Logger.Error("ew: list error", "err", err)
				ew.ListHealthy = false
			} else {
				ew.ListHealthy = true
			}
			return l, err
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = ew.labelSelector
			w, err := ew.client.CoreV1().Endpoints(metav1.NamespaceAll).Watch(ew.ctx, options)
			if err != nil {
				log.Logger.Error("ew: watch error", "err", err)
				ew.WatchHealthy = false
			} else {
				ew.WatchHealthy = true
			}
			return w, err
		},
	}
	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ew.eventHandler(watch.Added, nil, obj.(*v1.Endpoints))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			ew.eventHandler(watch.Modified, oldObj.(*v1.Endpoints), newObj.(*v1.Endpoints))
		},
		DeleteFunc: func(obj interface{}) {
			ew.eventHandler(watch.Deleted, obj.(*v1.Endpoints), nil)
		},
	}
	ew.store, ew.controller = cache.NewInformer(listWatch, &v1.Endpoints{}, ew.resyncPeriod, eventHandler)
}

func (ew *EndpointsWatcher) Run() {
	log.Logger.Info("starting endpoints watcher")
	// Running controller will block until writing on the stop channel.
	ew.controller.Run(ew.stopChannel)
	log.Logger.Info("stopped endpoints watcher")
}

func (ew *EndpointsWatcher) Stop() {
	log.Logger.Info("stopping endpoints watcher")
	close(ew.stopChannel)
}

func (ew *EndpointsWatcher) Get(name, namespace string) (*v1.Endpoints, error) {
	key := namespace + "/" + name

	obj, exists, err := ew.store.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("endpoints"), key)
	}

	return obj.(*v1.Endpoints), nil
}

func (ew *EndpointsWatcher) List() ([]*v1.Endpoints, error) {
	var endpoints []*v1.Endpoints
	for _, obj := range ew.store.List() {
		e, ok := obj.(*v1.Endpoints)
		if !ok {
			return nil, fmt.Errorf("unexpected object in store: %+v", obj)
		}
		endpoints = append(endpoints, e)
	}
	return endpoints, nil
}

func (ew *EndpointsWatcher) Healthy() bool {
	if ew.ListHealthy && ew.WatchHealthy {
		return true
	}
	return false
}
