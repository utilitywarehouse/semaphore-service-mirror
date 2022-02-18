package kube

import (
	"context"
	"fmt"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/utilitywarehouse/semaphore-service-mirror/log"
	"github.com/utilitywarehouse/semaphore-service-mirror/metrics"
)

type EndpointSliceEventHandler = func(eventType watch.EventType, old *discoveryv1.EndpointSlice, new *discoveryv1.EndpointSlice)

type EndpointSliceWatcher struct {
	ctx           context.Context
	client        kubernetes.Interface
	resyncPeriod  time.Duration
	stopChannel   chan struct{}
	store         cache.Store
	controller    cache.Controller
	eventHandler  EndpointSliceEventHandler
	labelSelector string
	name          string
	namespace     string
}

func NewEndpointSliceWatcher(name string, client kubernetes.Interface, resyncPeriod time.Duration, handler EndpointSliceEventHandler, labelSelector, namespace string) *EndpointSliceWatcher {
	return &EndpointSliceWatcher{
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

func (esw *EndpointSliceWatcher) Init() {
	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = esw.labelSelector
			l, err := esw.client.DiscoveryV1().EndpointSlices(esw.namespace).List(esw.ctx, options)
			if err != nil {
				log.Logger.Error("EndpointSlice list error", "watcher", esw.name, "err", err)
			} else {
			}
			return l, err
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = esw.labelSelector
			w, err := esw.client.DiscoveryV1().EndpointSlices(esw.namespace).Watch(esw.ctx, options)
			if err != nil {
				log.Logger.Error("EndpointSlice watch error", "watcher", esw.name, "err", err)
			} else {
			}
			return w, err
		},
	}
	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			esw.handleEvent(watch.Added, nil, obj.(*discoveryv1.EndpointSlice))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			esw.handleEvent(watch.Modified, oldObj.(*discoveryv1.EndpointSlice), newObj.(*discoveryv1.EndpointSlice))
		},
		DeleteFunc: func(obj interface{}) {
			esw.handleEvent(watch.Deleted, obj.(*discoveryv1.EndpointSlice), nil)
		},
	}
	esw.store, esw.controller = cache.NewInformer(listWatch, &discoveryv1.EndpointSlice{}, esw.resyncPeriod, eventHandler)
}

func (esw *EndpointSliceWatcher) handleEvent(eventType watch.EventType, oldObj, newObj *discoveryv1.EndpointSlice) {
	metrics.IncKubeWatcherEvents(esw.name, "endpointslice", eventType)
	metrics.SetKubeWatcherObjects(esw.name, "endpointslice", float64(len(esw.store.List())))

	if esw.eventHandler != nil {
		esw.eventHandler(eventType, oldObj, newObj)
	}
}

func (esw *EndpointSliceWatcher) Run() {
	log.Logger.Info("starting endpointslice watcher", "watcher", esw.name)
	// Running controller will block until writing on the stop channel.
	esw.controller.Run(esw.stopChannel)
	log.Logger.Info("stopped endpointslice watcher", "watcher", esw.name)
}

func (esw *EndpointSliceWatcher) Stop() {
	log.Logger.Info("stopping endpoints watcher", "watcher", esw.name)
	close(esw.stopChannel)
}

func (esw *EndpointSliceWatcher) HasSynced() bool {
	return esw.controller.HasSynced()
}

func (esw *EndpointSliceWatcher) Get(name, namespace string) (*discoveryv1.EndpointSlice, error) {
	key := namespace + "/" + name

	obj, exists, err := esw.store.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(discoveryv1.Resource("endpointslice"), key)
	}

	return obj.(*discoveryv1.EndpointSlice), nil
}

func (esw *EndpointSliceWatcher) List() ([]*discoveryv1.EndpointSlice, error) {
	var endpointslice []*discoveryv1.EndpointSlice
	for _, obj := range esw.store.List() {
		e, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			return nil, fmt.Errorf("unexpected object in store: %+v", obj)
		}
		endpointslice = append(endpointslice, e)
	}
	return endpointslice, nil
}
