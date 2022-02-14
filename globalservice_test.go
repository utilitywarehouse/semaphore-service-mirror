package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type testService struct {
	cluster   string
	name      string
	namespace string
	clusterIP string
	ports     []int32
}

func createTestService(name, namespace, clusterIP string, ports []int32) *v1.Service {
	svcPorts := []v1.ServicePort{}
	for _, port := range ports {
		svcPorts = append(svcPorts, v1.ServicePort{Port: port})
	}
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Ports:     svcPorts,
			Selector:  map[string]string{"selector": "x"},
			ClusterIP: clusterIP,
		},
	}
}

func createTestStore(t *testing.T, services []testService, topologyAware bool) *GlobalServiceStore {
	store := newGlobalServiceStore()
	for _, s := range services {
		svc := createTestService(s.name, s.namespace, s.clusterIP, s.ports)
		_, err := store.AddOrUpdateClusterServiceTarget(svc, s.cluster, topologyAware)
		assert.Equal(t, nil, err)
	}
	return store
}

func TestAddOrUpdateClusterServiceTarget_AddSingleServiceTarget(t *testing.T) {
	store := createTestStore(t, []testService{
		testService{cluster: "cluster", name: "name", namespace: "namespace", clusterIP: "1.1.1.1", ports: []int32{80}},
	}, false)
	assert.Equal(t, 1, store.Len())
	gsvc, err := store.Get("name", "namespace")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, []string{"cluster"}, gsvc.clusters)
}

func TestAddOrUpdateClusterServiceTarget_AddMultipleServiceTargets(t *testing.T) {
	store := createTestStore(t, []testService{
		testService{cluster: "a", name: "name", namespace: "namespace", clusterIP: "1.1.1.1", ports: []int32{80}},
		testService{cluster: "b", name: "name", namespace: "namespace", clusterIP: "2.2.2.2", ports: []int32{80}},
		testService{cluster: "c", name: "name", namespace: "namespace", clusterIP: "3.3.3.3", ports: []int32{80}},
	}, false)
	assert.Equal(t, 1, store.Len())
	gsvc, err := store.Get("name", "namespace")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, []string{"a", "b", "c"}, gsvc.clusters)
}

func TestAddOrUpdateClusterServiceTarget_AddMultipleServices(t *testing.T) {
	store := createTestStore(t, []testService{
		testService{cluster: "a", name: "a", namespace: "a", clusterIP: "1.1.1.1", ports: []int32{80}},
		testService{cluster: "b", name: "b", namespace: "b", clusterIP: "2.2.2.2", ports: []int32{80}},
	}, false)
	assert.Equal(t, 2, store.Len())
}

func TestAddOrUpdateClusterServiceTarget_HeadlessMisMatch(t *testing.T) {
	store := newGlobalServiceStore()
	svcA := createTestService("name", "namespace", "1.1.1.1", []int32{80})
	clusterA := "a"
	_, err := store.AddOrUpdateClusterServiceTarget(svcA, clusterA, false)
	assert.Equal(t, nil, err)
	svcB := createTestService("name", "namespace", "None", []int32{80})
	clusterB := "b"
	_, err = store.AddOrUpdateClusterServiceTarget(svcB, clusterB, false)
	assert.Equal(t, fmt.Errorf("Mismatch between existing headless service and requested"), err)
}

func TestAddOrUpdateClusterServiceTarget_UpdateFungiblePorts(t *testing.T) {
	store := createTestStore(t, []testService{
		testService{cluster: "a", name: "name", namespace: "namespace", clusterIP: "1.1.1.1", ports: []int32{80}},
	}, false)
	svcB := createTestService("name", "namespace", "2.2.2.2", []int32{8080})
	clusterB := "b"
	_, err := store.AddOrUpdateClusterServiceTarget(svcB, clusterB, false)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1, store.Len())
	gsvc, err := store.Get("name", "namespace")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, []string{"a", "b"}, gsvc.clusters)
	assert.Equal(t, 1, len(gsvc.ports))
	assert.Equal(t, int32(8080), gsvc.ports[0].Port)
}

func TestAddOrUpdateClusterServiceTarget_UpdateFungibleTopologyAnnotations(t *testing.T) {
	store := createTestStore(t, []testService{
		testService{cluster: "a", name: "name", namespace: "namespace", clusterIP: "1.1.1.1", ports: []int32{80}},
	}, true)
	gsvc, err := store.Get("name", "namespace")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, len(gsvc.annotations))
	assert.Equal(t, kubeSeviceTopologyAwareHintsAnnoVal, gsvc.annotations[kubeSeviceTopologyAwareHintsAnno])

	// Add a service with topolofy aware flag set to false
	svcB := createTestService("name", "namespace", "2.2.2.2", []int32{8080})
	clusterB := "b"
	_, err = store.AddOrUpdateClusterServiceTarget(svcB, clusterB, false)
	if err != nil {
		t.Fatal(err)
	}
	// This should keep a single service in the store, but delete the
	// topology annotation
	assert.Equal(t, 1, store.Len())
	gsvc, err = store.Get("name", "namespace")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, []string{"a", "b"}, gsvc.clusters)
	assert.Equal(t, 1, len(gsvc.annotations))
}

func TestDeleteClusterServiceTarget_DeleteServiceLastTarget(t *testing.T) {
	store := createTestStore(t, []testService{
		testService{cluster: "cluster", name: "name", namespace: "namespace", clusterIP: "1.1.1.1", ports: []int32{80}},
	}, false)
	assert.Equal(t, 1, store.Len())
	svc := createTestService("name", "namespace", "1.1.1.1", []int32{80})
	store.DeleteClusterServiceTarget(svc.Name, svc.Namespace, "cluster")
	assert.Equal(t, 0, store.Len())
}

func TestDeleteClusterServiceTarget_DeleteServiceTarget(t *testing.T) {
	store := createTestStore(t, []testService{
		testService{cluster: "a", name: "name", namespace: "namespace", clusterIP: "1.1.1.1", ports: []int32{80}},
		testService{cluster: "b", name: "name", namespace: "namespace", clusterIP: "2.2.2.2", ports: []int32{80}},
	}, false)
	assert.Equal(t, 1, store.Len())
	gsvc, err := store.Get("name", "namespace")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, []string{"a", "b"}, gsvc.clusters)
	svcA := createTestService("name", "namespace", "1.1.1.1", []int32{80})
	store.DeleteClusterServiceTarget(svcA.Name, svcA.Namespace, "a")
	assert.Equal(t, 1, store.Len())
	assert.Equal(t, []string{"b"}, gsvc.clusters)
}

func TestDeleteClusterServiceTarget_NotPresent(t *testing.T) {
	store := createTestStore(t, []testService{
		testService{cluster: "cluster", name: "name", namespace: "namespace", clusterIP: "1.1.1.1", ports: []int32{80}},
	}, false)
	assert.Equal(t, 1, store.Len())
	svcB := createTestService("b", "b", "2.2.2.2", []int32{80})
	clusterB := "b"
	store.DeleteClusterServiceTarget(svcB.Name, svcB.Namespace, clusterB)
	assert.Equal(t, 1, store.Len())
}
