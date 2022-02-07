package main

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type GlobalService struct {
	name        string
	namespace   string
	ports       []v1.ServicePort
	headless    bool
	labels      map[string]string
	annotations map[string]string
	clusters    []string
}

const (
	kubeSeviceTopologyAwareHintsAnno    = "service.kubernetes.io/topology-aware-hints"
	kubeSeviceTopologyAwareHintsAnnoVal = "auto"
)

var (
	globalSvcLabels       = map[string]string{"global-svc": "true"}
	globalSvcAnnotations  = map[string]string{kubeSeviceTopologyAwareHintsAnno: kubeSeviceTopologyAwareHintsAnnoVal} // Kube annotation to enable topolgy aware routing
	globalSvcClustersAnno = "global-svc-clusters"
)

type GlobalServiceStore struct {
	client kubernetes.Interface // Local cluster kube client
	store  map[string]*GlobalService
}

func newGlobalServiceStore(client kubernetes.Interface) *GlobalServiceStore {
	return &GlobalServiceStore{
		client: client,
		store:  make(map[string]*GlobalService),
	}
}

// AddOrUpdateClusterServiceTarget will append a cluster to the GlobalService
// clusters list. In case there is no global service in the store, it creates
// the GlobalService.
func (gss *GlobalServiceStore) AddOrUpdateClusterServiceTarget(svc *v1.Service, cluster string) (*GlobalService, error) {
	gsvcName := generateGlobalServiceName(svc.Name, svc.Namespace)
	gsvc, ok := gss.store[gsvcName]
	// Add new service in the store if it doesn't exist
	if !ok {
		gsvc = &GlobalService{
			name:        svc.Name,
			namespace:   svc.Namespace,
			ports:       svc.Spec.Ports,
			headless:    isHeadless(svc),
			labels:      globalSvcLabels,
			annotations: globalSvcAnnotations,
			clusters:    []string{cluster},
		}
		gsvc.annotations[globalSvcClustersAnno] = fmt.Sprintf("%s", cluster)
		gss.store[gsvcName] = gsvc
		return gsvc, nil
	}
	// If service exists, check and update global service
	if gsvc.headless != isHeadless(svc) {
		return nil, fmt.Errorf("Mismatch between existing headless service and requested")
	}
	if _, found := inSlice(gsvc.clusters, cluster); !found {
		gsvc.clusters = append(gsvc.clusters, cluster)
	}
	gsvc.annotations[globalSvcClustersAnno] = strings.Join(gsvc.clusters, ",")
	return gsvc, nil
}

// DeleteClusterServiceTarget removes a cluster from the GlobalService's
// clusters list. If the list is empty it deletes the GlobalService. Returns a
// pointer to a GlobalService or nil if completely deleted
func (gss *GlobalServiceStore) DeleteClusterServiceTarget(name, namespace, cluster string) *GlobalService {
	gsvcName := generateGlobalServiceName(name, namespace)
	gsvc, ok := gss.store[gsvcName]
	if !ok {
		return nil
	}
	if i, found := inSlice(gsvc.clusters, cluster); found {
		gsvc.clusters = removeFromSlice(gsvc.clusters, i)
	}
	gss.store[gsvcName] = gsvc
	if len(gsvc.clusters) == 0 {
		delete(gss.store, gsvcName)
		return nil
	}
	gsvc.annotations[globalSvcClustersAnno] = strings.Join(gsvc.clusters, ",")
	return gsvc
}

func (gss *GlobalServiceStore) Get(name, namespace string) (*GlobalService, error) {
	gsvcName := generateGlobalServiceName(name, namespace)
	gsvc, ok := gss.store[gsvcName]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return gsvc, nil
}

func (gss *GlobalServiceStore) Len() int {
	return len(gss.store)
}
