package kube

import (
	"errors"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/utilitywarehouse/kube-service-mirror/log"
)

var (
	ERROR_ENDPOINTS_NOT_EXIST = errors.New("endpoints does not exist in store")
)

type EndpointsEventHandler = func(eventType watch.EventType, old *v1.Endpoints, new *v1.Endpoints)

type EndpointsWatcher struct {
	client       kubernetes.Interface
	resyncPeriod time.Duration
	stopChannel  chan struct{}
	store        cache.Store
	controller   cache.Controller
	eventHandler EndpointsEventHandler
}

func NewEndpointsWatcher(client kubernetes.Interface, resyncPeriod time.Duration, handler EndpointsEventHandler) *EndpointsWatcher {
	return &EndpointsWatcher{
		client:       client,
		resyncPeriod: resyncPeriod,
		stopChannel:  make(chan struct{}),
		eventHandler: handler,
	}
}

func (ew *EndpointsWatcher) Init() {
	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			// "" will get endpoints from all namespaces
			return ew.client.CoreV1().Endpoints("").List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			// "" will get endpoints from all namespaces
			return ew.client.CoreV1().Endpoints("").Watch(options)
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

func (ew *EndpointsWatcher) Start() {
	log.Logger.Info("starting endpoints watcher")
	// Running controller will block until writing on the stop channel.
	ew.controller.Run(ew.stopChannel)
	log.Logger.Info("stopped endpoints watcher")
}

func (ew *EndpointsWatcher) Stop() {
	log.Logger.Info("stopping endpoints watcher")
	close(ew.stopChannel)
}

func (ew *EndpointsWatcher) Get(name string) (*v1.Endpoints, error) {
	// TODO: Can't make GetByKey work
	for _, obj := range ew.store.List() {
		endpoints, ok := obj.(*v1.Endpoints)
		if !ok {
			log.Logger.Error("cannot read endpoints object: %s", obj)
			continue
		}
		if endpoints.Name == name {
			return endpoints, nil
		}
	}
	return &v1.Endpoints{}, ERROR_ENDPOINTS_NOT_EXIST
}
