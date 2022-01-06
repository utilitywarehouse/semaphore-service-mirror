package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/utilitywarehouse/semaphore-service-mirror/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

var testMirrorLabels = map[string]string{
	"mirrored-svc":           "true",
	"mirror-svc-prefix-sync": "prefix",
}

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

func assertExpectedServices(t *testing.T, ctx context.Context, expectedSvcs []TestSvc, fakeClient *fake.Clientset) {
	svcs, err := fakeClient.CoreV1().Services("").List(
		ctx,
		metav1.ListOptions{},
	)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.InitLogger("semaphore-service-mirror-test", "debug")
	fakeClient := fake.NewSimpleClientset()

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    map[string]string{"uw.systems/test": "true"},
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  map[string]string{"selector": "x"},
			ClusterIP: "1.1.1.1",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)

	testRunner := NewRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		"prefix",
		"uw.systems/test=true",
		60*time.Minute,
		true,
	)
	go testRunner.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunner.serviceWatcher.HasSynced)

	// Test create cluster ip service - should create 1 service with no
	// cluster ip specified, the same ports and nil selector
	testRunner.reconcileService("test-svc", "remote-ns")

	expectedSpec := TestSpec{
		Ports:     testPorts,
		ClusterIP: "",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{TestSvc{
		Name:      fmt.Sprintf("prefix-remote-ns-%s-test-svc", Separator),
		Namespace: "local-ns",
		Spec:      expectedSpec,
	}}
	assertExpectedServices(t, ctx, expectedSvcs, fakeClient)
}

func TestAddHeadlessService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.InitLogger("semaphore-service-mirror-test", "debug")
	fakeClient := fake.NewSimpleClientset()

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    map[string]string{"uw.systems/test": "true"},
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  map[string]string{"selector": "x"},
			ClusterIP: "None",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)

	testRunner := NewRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		"prefix",
		"uw.systems/test=true",
		60*time.Minute,
		true,
	)
	go testRunner.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunner.serviceWatcher.HasSynced)

	// Test create headless service - should create 1 service with "None"
	// cluster ip, the same ports and nil selector
	testRunner.reconcileService("test-svc", "remote-ns")

	expectedSpec := TestSpec{
		Ports:     testPorts,
		ClusterIP: "None",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{TestSvc{
		Name:      fmt.Sprintf("prefix-remote-ns-%s-test-svc", Separator),
		Namespace: "local-ns",
		Spec:      expectedSpec,
	}}
	assertExpectedServices(t, ctx, expectedSvcs, fakeClient)
}

func TestModifyService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.InitLogger("semaphore-service-mirror-test", "debug")

	existingPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	existingSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("prefix-remote-ns-%s-test-svc", Separator),
			Namespace: "local-ns",
		},
		Spec: v1.ServiceSpec{
			Ports:     existingPorts,
			ClusterIP: "None",
		},
	}
	fakeClient := fake.NewSimpleClientset(existingSvc)

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 2}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    map[string]string{"uw.systems/test": "true"},
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  map[string]string{"selector": "x"},
			ClusterIP: "None",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)

	testRunner := NewRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		"prefix",
		"uw.systems/test=true",
		60*time.Minute,
		true,
	)
	go testRunner.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunner.serviceWatcher.HasSynced)

	testRunner.reconcileService("test-svc", "remote-ns")

	expectedSpec := TestSpec{
		Ports:     testPorts,
		ClusterIP: "None",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{TestSvc{
		Name:      fmt.Sprintf("prefix-remote-ns-%s-test-svc", Separator),
		Namespace: "local-ns",
		Spec:      expectedSpec,
	}}
	assertExpectedServices(t, ctx, expectedSvcs, fakeClient)
}

func TestModifyServiceNoChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.InitLogger("semaphore-service-mirror-test", "debug")

	existingPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	existingSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("prefix-remote-ns-%s-test-svc", Separator),
			Namespace: "local-ns",
		},
		Spec: v1.ServiceSpec{
			Ports:     existingPorts,
			ClusterIP: "None",
		},
	}
	fakeClient := fake.NewSimpleClientset(existingSvc)

	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    map[string]string{"uw.systems/test": "true"},
		},
		Spec: v1.ServiceSpec{
			Ports:     existingPorts,
			Selector:  map[string]string{"selector": "x"},
			ClusterIP: "None",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)

	testRunner := NewRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		"prefix",
		"uw.systems/test=true",
		60*time.Minute,
		true,
	)
	go testRunner.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunner.serviceWatcher.HasSynced)

	testRunner.reconcileService("test-svc", "remote-ns")

	svcs, err := fakeClient.CoreV1().Services("").List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, 1, len(svcs.Items))
	assert.Equal(t, *existingSvc, svcs.Items[0])
}

func TestServiceSync(t *testing.T) {
	ctx := context.Background()

	log.InitLogger("semaphore-service-mirror-test", "debug")

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	// Service on the remote cluster
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    map[string]string{"uw.systems/test": "true"},
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  map[string]string{"test-app": "true"},
			ClusterIP: "1.1.1.1",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)

	// Create mirrored service
	mirroredSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("prefix-remote-ns-%s-test-svc", Separator),
			Namespace: "local-ns",
			Labels:    testMirrorLabels,
		},
		Spec: v1.ServiceSpec{
			Ports:    testPorts,
			Selector: nil,
		},
	}
	// Create stale service
	staleSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("prefix-old-svc-%s-remote-ns", Separator),
			Namespace: "local-ns",
			Labels:    testMirrorLabels,
		},
		Spec: v1.ServiceSpec{
			Ports:    testPorts,
			Selector: nil,
		},
	}
	// feed them to the fake client
	fakeClient := fake.NewSimpleClientset(mirroredSvc, staleSvc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testRunner := NewRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		"prefix",
		"uw.systems/test=true",
		60*time.Minute,
		true,
	)
	go testRunner.serviceWatcher.Run()
	go testRunner.mirrorServiceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunner.serviceWatcher.HasSynced)
	cache.WaitForNamedCacheSync("mirrorServiceWatcher", ctx.Done(), testRunner.mirrorServiceWatcher.HasSynced)

	// ServiceSync will trigger a sync. Verify that old service is deleted
	if err := testRunner.ServiceSync(); err != nil {
		t.Fatal(err)
	}
	svcs, err := fakeClient.CoreV1().Services("").List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1, len(svcs.Items))
	assert.Equal(
		t,
		fmt.Sprintf("prefix-remote-ns-%s-test-svc", Separator),
		svcs.Items[0].Name,
	)
}
