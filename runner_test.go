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
type TestSvc struct {
	Name      string
	Namespace string
	Spec      TestSpec
}

type TestSpec struct {
	Ports     []v1.ServicePort
	Selector  map[string]string
	ClusterIP string
}

func assertExpectedServices(t *testing.T, expectedSvcs []TestSvc, fakeClient *fake.Clientset) {

	svcs, err := fakeClient.CoreV1().Services("").List(
		metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, len(expectedSvcs), len(svcs.Items))
	for i, expected := range expectedSvcs {
		assert.Equal(t, expected.Name, svcs.Items[i].Name)
		assert.Equal(t, expected.Namespace, svcs.Items[i].Namespace)
		assert.Equal(t, expected.Spec.ClusterIP, svcs.Items[i].Spec.ClusterIP)
		assert.Equal(t, expected.Spec.Selector, svcs.Items[i].Spec.Selector)
		assert.Equal(t, expected.Spec.Ports, svcs.Items[i].Spec.Ports)
	}
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

	expectedSpec := TestSpec{
		Ports:     testPorts,
		ClusterIP: "",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{TestSvc{
		Name:      "test-svc-remote-ns",
		Namespace: "local-ns",
		Spec:      expectedSpec,
	}}
	assertExpectedServices(t, expectedSvcs, fakeClient)

	// Test create headless service - should create 1 service with "None"
	// cluster ip, the same ports and nil selector
	fakeClient = fake.NewSimpleClientset() // reset client to clear svc
	testRunner = &Runner{
		client:    fakeClient,
		namespace: "local-ns",
	}
	testSvc.Spec.ClusterIP = "None" // headless service

	testRunner.ServiceEventHandler(watch.Added, &v1.Service{}, testSvc)

	expectedSpec = TestSpec{
		Ports:     testPorts,
		ClusterIP: "None",
		Selector:  nil,
	}
	expectedSvcs = []TestSvc{TestSvc{
		Name:      "test-svc-remote-ns",
		Namespace: "local-ns",
		Spec:      expectedSpec,
	}}
	assertExpectedServices(t, expectedSvcs, fakeClient)

	// Test add on existing triggers update
	updatePorts := []v1.ServicePort{v1.ServicePort{Port: 2}}
	testSvc.Spec.Ports = updatePorts

	testRunner.ServiceEventHandler(watch.Added, &v1.Service{}, testSvc)

	expectedSpec = TestSpec{
		Ports:     updatePorts,
		ClusterIP: "None",
		Selector:  nil,
	}
	expectedSvcs = []TestSvc{TestSvc{
		Name:      "test-svc-remote-ns",
		Namespace: "local-ns",
		Spec:      expectedSpec,
	}}
	assertExpectedServices(t, expectedSvcs, fakeClient)
}

func TestModifyService(t *testing.T) {

	log.InitLogger("kube-service-mirror-test", "debug")

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	// Service on the remote cluster
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

	// Create mirrored service and feed it to the fake client
	mirroredSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc-remote-ns",
			Namespace: "local-ns",
			Labels:    CommonLabels,
		},
		Spec: v1.ServiceSpec{
			Ports:    testPorts,
			Selector: nil,
		},
	}
	fakeClient := fake.NewSimpleClientset(mirroredSvc)

	testRunner := &Runner{
		client:    fakeClient,
		namespace: "local-ns",
	}

	// Test update with the same service won't change the mirrored
	testRunner.ServiceEventHandler(watch.Modified, &v1.Service{}, testSvc)

	svcs, err := fakeClient.CoreV1().Services("").List(
		metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, 1, len(svcs.Items))
	assert.Equal(t, *mirroredSvc, svcs.Items[0])

	// Test update ports
	updatePorts := []v1.ServicePort{v1.ServicePort{Port: 2}}
	testSvc.Spec.Ports = updatePorts

	testRunner.ServiceEventHandler(watch.Modified, &v1.Service{}, testSvc)

	expectedSpec := TestSpec{
		Ports:     updatePorts,
		ClusterIP: "",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{TestSvc{
		Name:      "test-svc-remote-ns",
		Namespace: "local-ns",
		Spec:      expectedSpec,
	}}
	assertExpectedServices(t, expectedSvcs, fakeClient)

	// Test update to headless
	testSvc.Spec.ClusterIP = "None"

	testRunner.ServiceEventHandler(watch.Modified, &v1.Service{}, testSvc)

	svcs, err = fakeClient.CoreV1().Services("").List(
		metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	expectedSpec = TestSpec{
		Ports:     updatePorts,
		ClusterIP: "None",
		Selector:  nil,
	}
	expectedSvcs = []TestSvc{TestSvc{
		Name:      "test-svc-remote-ns",
		Namespace: "local-ns",
		Spec:      expectedSpec,
	}}
	assertExpectedServices(t, expectedSvcs, fakeClient)
}
