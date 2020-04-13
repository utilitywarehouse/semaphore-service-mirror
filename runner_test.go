package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/utilitywarehouse/kube-service-mirror/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
)

// To make expected types
type TestSpec struct {
	Ports     []v1.ServicePort
	Selector  map[string]string
	ClusterIP string
}

func TestAddService(t *testing.T) {

	log.InitLogger("kube-service-mirror-test", "debug")
	fakeClient := fake.NewSimpleClientset()

	testRunner := &Runner{
		client:    fakeClient,
		namespace: "local-ns",
	}

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    map[string]string{"test-svc": "true"},
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  map[string]string{"selector": "x"},
			ClusterIP: "1.1.1.1",
		},
	}

	// Test create cluster ip service - should create 1 service with no
	// cluster ip specified, the same ports and nil selector
	testRunner.ServiceEventHandler(watch.Added, &v1.Service{}, testSvc)

	svcs, err := fakeClient.CoreV1().Services("").List(
		metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	expected := TestSpec{
		Ports:     testPorts,
		ClusterIP: "",
		Selector:  nil,
	}
	assert.Equal(t, 1, len(svcs.Items))
	assert.Equal(t, "test-svc-remote-ns", svcs.Items[0].Name)
	assert.Equal(t, "local-ns", svcs.Items[0].Namespace)
	assert.Equal(t, expected.ClusterIP, svcs.Items[0].Spec.ClusterIP)
	assert.Equal(t, expected.Selector, svcs.Items[0].Spec.Selector)
	assert.Equal(t, expected.Ports, svcs.Items[0].Spec.Ports)

	// Test create headless service - should create 1 service with "None"
	// cluster ip, the same ports and nil selector
	fakeClient = fake.NewSimpleClientset() // reset client to clear svc
	testRunner = &Runner{
		client:    fakeClient,
		namespace: "local-ns",
	}
	testSvc.Spec.ClusterIP = "None" // headless service

	testRunner.ServiceEventHandler(watch.Added, &v1.Service{}, testSvc)

	svcs, err = fakeClient.CoreV1().Services("").List(
		metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	expected = TestSpec{
		Ports:     testPorts,
		ClusterIP: "None",
		Selector:  nil,
	}
	assert.Equal(t, 1, len(svcs.Items))
	assert.Equal(t, "test-svc-remote-ns", svcs.Items[0].Name)
	assert.Equal(t, "local-ns", svcs.Items[0].Namespace)
	assert.Equal(t, expected.ClusterIP, svcs.Items[0].Spec.ClusterIP)
	assert.Equal(t, expected.Selector, svcs.Items[0].Spec.Selector)
	assert.Equal(t, expected.Ports, svcs.Items[0].Spec.Ports)

	// Test add on existing triggers update
	updatePorts := []v1.ServicePort{v1.ServicePort{Port: 2}}
	testSvc.Spec.Ports = updatePorts

	testRunner.ServiceEventHandler(watch.Added, &v1.Service{}, testSvc)

	svcs, err = fakeClient.CoreV1().Services("").List(
		metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	expected = TestSpec{
		Ports:     updatePorts,
		ClusterIP: "None",
		Selector:  nil,
	}
	assert.Equal(t, 1, len(svcs.Items))
	assert.Equal(t, "test-svc-remote-ns", svcs.Items[0].Name)
	assert.Equal(t, "local-ns", svcs.Items[0].Namespace)
	assert.Equal(t, expected.ClusterIP, svcs.Items[0].Spec.ClusterIP)
	assert.Equal(t, expected.Selector, svcs.Items[0].Spec.Selector)
	assert.Equal(t, expected.Ports, svcs.Items[0].Spec.Ports)
}
