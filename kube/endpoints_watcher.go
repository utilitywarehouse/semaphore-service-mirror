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
	name          string
	namespace     string
}

func NewEndpointsWatcher(name string, client kubernetes.Interface, resyncPeriod time.Duration, handler EndpointsEventHandler, labelSelector, namespace string) *EndpointsWatcher {
	return &EndpointsWatcher{
		ctx:           context.Background(),
		client:        client,
		resyncPeriod:  resyncPeriod,
		stopChannel:   make(chan struct{}),
		eventHandler:  handler,
		labelSelector: labelSelector,
		name:          name,
		namespace:     namespace,
	}
}

func (ew *EndpointsWatcher) Init() {
	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = ew.labelSelector
			l, err := ew.client.CoreV1().Endpoints(ew.namespace).List(ew.ctx, options)
			if err != nil {
				log.Logger.Error("endpoints list error", "watcher", ew.name, "err", err)
			}
			return l, err
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = ew.labelSelector
			w, err := ew.client.CoreV1().Endpoints(ew.namespace).Watch(ew.ctx, options)
			if err != nil {
				log.Logger.Error("endpoints watch error", "watcher", ew.name, "err", err)
			}
			return w, err
		},
	}
	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ew.handleEvent(watch.Added, nil, obj.(*v1.Endpoints))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			ew.handleEvent(watch.Modified, oldObj.(*v1.Endpoints), newObj.(*v1.Endpoints))
		},
		DeleteFunc: func(obj interface{}) {
			ew.handleEvent(watch.Deleted, obj.(*v1.Endpoints), nil)
		},
	}
	ew.store, ew.controller = cache.NewInformer(listWatch, &v1.Endpoints{}, ew.resyncPeriod, eventHandler)
}

func (ew *EndpointsWatcher) handleEvent(eventType watch.EventType, oldObj, newObj *v1.Endpoints) {
	metrics.IncKubeWatcherEvents(ew.name, "endpoints", eventType)
	metrics.SetKubeWatcherObjects(ew.name, "endpoints", float64(len(ew.store.List())))

	if ew.eventHandler != nil {
		ew.eventHandler(eventType, oldObj, newObj)
	}
}

func (ew *EndpointsWatcher) Run() {
	log.Logger.Info("starting endpoints watcher", "watcher", ew.name)
	// Running controller will block until writing on the stop channel.
	ew.controller.Run(ew.stopChannel)
	log.Logger.Info("stopped endpoints watcher", "watcher", ew.name)
}

func (ew *EndpointsWatcher) Stop() {
	log.Logger.Info("stopping endpoints watcher", "watcher", ew.name)
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
