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
	ERROR_SVC_NOT_EXISTS = errors.New("service does not exist in store")
)

type ServiceEventHandler = func(eventType watch.EventType, old *v1.Service, new *v1.Service)

type ServiceWatcher struct {
	client        kubernetes.Interface
	resyncPeriod  time.Duration
	stopChannel   chan struct{}
	store         cache.Store
	controller    cache.Controller
	eventHandler  ServiceEventHandler
	labelSelector string
}

func NewServiceWatcher(client kubernetes.Interface, resyncPeriod time.Duration, handler ServiceEventHandler, labelSelector string) *ServiceWatcher {
	return &ServiceWatcher{
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
			return sw.client.CoreV1().Services(metav1.NamespaceAll).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = sw.labelSelector
			return sw.client.CoreV1().Services(metav1.NamespaceAll).Watch(options)
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

func (sw *ServiceWatcher) Get(name string) (*v1.Service, error) {
	// TODO: Can't make GetByKey work
	for _, obj := range sw.store.List() {
		svc, ok := obj.(*v1.Service)
		if !ok {
			log.Logger.Error("cannot read svc object: %s", obj)
			continue
		}
		if svc.Name == name {
			return svc, nil
		}
	}
	return &v1.Service{}, ERROR_SVC_NOT_EXISTS

}
