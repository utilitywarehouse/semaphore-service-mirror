package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/utilitywarehouse/semaphore-service-mirror/log"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

var (
	testGlobalLabels               = map[string]string{"global-svc": "true"}
	testGlobalSvcLabel             = map[string]string{"mirror.semaphore.uw.io/global-service": "true"}
	testGlobalSvcLabelString       = "mirror.semaphore.uw.io/global-service=true"
	testGlobalRoutingStrategyLabel = "mirror.semaphore.uw.io/global-service-routing-strategy=local-first"
	testServiceSelector            = map[string]string{"selector": "x"}
)

func TestAddSingleRemoteGlobalService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.InitLogger("semaphore-service-mirror-test", "debug")
	fakeClient := fake.NewSimpleClientset()

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    testGlobalSvcLabel,
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  testServiceSelector,
			ClusterIP: "1.1.1.1",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)
	testGlobalStore := newGlobalServiceStore()

	selector, _ := labels.Parse(testGlobalRoutingStrategyLabel)
	testRunner := newGlobalRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		testGlobalSvcLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
		selector,
		false,
	)
	go testRunner.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunner.serviceWatcher.HasSynced)

	// Test create cluster ip service - should create 1 service with no
	// cluster ip specified, the same ports and nil selector
	testRunner.reconcileGlobalService("test-svc", "remote-ns")

	expectedSpec := TestSpec{
		Ports:     testPorts,
		ClusterIP: "",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{
		TestSvc{
			Name:      fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator),
			Namespace: "local-ns",
			Spec:      expectedSpec,
			Labels:    testGlobalLabels,
			Annotations: map[string]string{
				globalSvcClustersAnno: "test-runner",
			},
		},
	}
	assertExpectedGlobalServices(ctx, t, expectedSvcs, fakeClient)
}

func TestAddSingleRemoteGlobalHeadlessService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.InitLogger("semaphore-service-mirror-test", "debug")
	fakeClient := fake.NewSimpleClientset()

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    testGlobalSvcLabel,
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  testServiceSelector,
			ClusterIP: "None",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)
	testGlobalStore := newGlobalServiceStore()

	selector, _ := labels.Parse(testGlobalRoutingStrategyLabel)
	testRunner := newGlobalRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		testGlobalSvcLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
		selector,
		false,
	)
	go testRunner.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunner.serviceWatcher.HasSynced)

	// Test create headless service - should create 1 service with "None"
	// cluster ip, the same ports and nil selector
	testRunner.reconcileGlobalService("test-svc", "remote-ns")

	expectedSpec := TestSpec{
		Ports:     testPorts,
		ClusterIP: "None",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{
		TestSvc{
			Name:      fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator),
			Namespace: "local-ns",
			Spec:      expectedSpec,
			Labels:    testGlobalLabels,
			Annotations: map[string]string{
				globalSvcClustersAnno: "test-runner",
			},
		},
	}
	assertExpectedGlobalServices(ctx, t, expectedSvcs, fakeClient)
}

func TestModifySingleRemoteGlobalService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log.InitLogger("semaphore-service-mirror-test", "debug")
	existingPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	existingAnnotations := map[string]string{
		globalSvcClustersAnno:            "test-runner",
		kubeSeviceTopologyAwareHintsAnno: kubeSeviceTopologyAwareHintsAnnoVal,
	}
	existingSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator),
			Namespace:   "local-ns",
			Labels:      testGlobalLabels,
			Annotations: existingAnnotations,
		},
		Spec: v1.ServiceSpec{
			Ports:     existingPorts,
			Selector:  testServiceSelector,
			ClusterIP: "1.1.1.1",
		},
	}
	fakeClient := fake.NewSimpleClientset(existingSvc)
	existingGlobalStore := newGlobalServiceStore()
	existingGlobalStore.store[fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator)] = &GlobalService{
		name:        "test-svc",
		namespace:   "remote-ns",
		ports:       existingPorts,
		labels:      testGlobalLabels,
		annotations: existingAnnotations,
		clusters:    []string{"test-runner"},
	}

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 2}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    testGlobalSvcLabel,
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  testServiceSelector,
			ClusterIP: "1.1.1.1",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)

	selector, _ := labels.Parse(testGlobalRoutingStrategyLabel)
	testRunner := newGlobalRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		testGlobalSvcLabelString,
		60*time.Minute,
		existingGlobalStore,
		false,
		selector,
		false,
	)
	go testRunner.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunner.serviceWatcher.HasSynced)

	testRunner.reconcileGlobalService("test-svc", "remote-ns")
	// After reconciling we should see updated ports and drop the topology aware hints annotation
	expectedSpec := TestSpec{
		Ports:     testPorts,
		ClusterIP: "1.1.1.1",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{
		TestSvc{
			Name:      fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator),
			Namespace: "local-ns",
			Spec:      expectedSpec,
			Labels:    testGlobalLabels,
			Annotations: map[string]string{
				globalSvcClustersAnno: "test-runner",
			},
		},
	}
	assertExpectedGlobalServices(ctx, t, expectedSvcs, fakeClient)
}

func TestAddGlobalServiceMultipleClusters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.InitLogger("semaphore-service-mirror-test", "debug")
	fakeClient := fake.NewSimpleClientset()

	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	// Create a service with the same name and namespace in 2 clusters (A and B)
	testSvcA := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    testGlobalSvcLabel,
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  testServiceSelector,
			ClusterIP: "1.1.1.1",
		},
	}
	testSvcB := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels:    testGlobalSvcLabel,
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  testServiceSelector,
			ClusterIP: "2.2.2.2",
		},
	}
	fakeWatchClientA := fake.NewSimpleClientset(testSvcA)
	fakeWatchClientB := fake.NewSimpleClientset(testSvcB)
	testGlobalStore := newGlobalServiceStore()

	selector, _ := labels.Parse("mirror.semaphore.uw.io/test=true")
	testRunnerA := newGlobalRunner(
		fakeClient,
		fakeWatchClientA,
		"runnerA",
		"local-ns",
		testGlobalSvcLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
		selector,
		false,
	)
	testRunnerB := newGlobalRunner(
		fakeClient,
		fakeWatchClientB,
		"runnerB",
		"local-ns",
		testGlobalSvcLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
		selector,
		false,
	)

	go testRunnerA.serviceWatcher.Run()
	go testRunnerB.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunnerA.serviceWatcher.HasSynced)
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunnerB.serviceWatcher.HasSynced)

	expectedSpec := TestSpec{
		Ports:     testPorts,
		ClusterIP: "",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{TestSvc{
		Name:      fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator),
		Namespace: "local-ns",
		Spec:      expectedSpec,
		Labels:    testGlobalLabels,
		Annotations: map[string]string{
			globalSvcClustersAnno: "runnerA",
		},
	}}

	testRunnerA.reconcileGlobalService("test-svc", "remote-ns")
	assertExpectedGlobalServices(ctx, t, expectedSvcs, fakeClient)

	// Reconciling the service from cluster B should only edit the respective label
	testRunnerB.reconcileGlobalService("test-svc", "remote-ns")
	expectedSvcs[0].Annotations[globalSvcClustersAnno] = "runnerA,runnerB"
	assertExpectedGlobalServices(ctx, t, expectedSvcs, fakeClient)
}

func TestDeleteGlobalServiceMultipleClusters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.InitLogger("semaphore-service-mirror-test", "debug")

	existingPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	existingSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator),
			Namespace:   "local-ns",
			Labels:      globalSvcLabels,
			Annotations: globalSvcAnnotations,
		},
		Spec: v1.ServiceSpec{
			Ports:     existingPorts,
			ClusterIP: "",
		},
	}
	existingSvc.Annotations[globalSvcClustersAnno] = "runnerA,runnerB"

	fakeClient := fake.NewSimpleClientset(existingSvc)
	testGlobalStore := newGlobalServiceStore()
	// Add the existing service into global store from both clusters
	testLabels := globalSvcLabels
	annotations := globalSvcAnnotations
	annotations[globalSvcClustersAnno] = "runnerA,runnerB"
	testGlobalStore.store[fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator)] = &GlobalService{
		name:        "test-svc",
		namespace:   "remote-ns",
		ports:       existingPorts,
		labels:      testLabels,
		annotations: annotations,
		clusters:    []string{"runnerA", "runnerB"},
	}

	// Remote fake clients won't have any services as we are deleting
	fakeWatchClientA := fake.NewSimpleClientset()
	fakeWatchClientB := fake.NewSimpleClientset()

	selector, _ := labels.Parse("mirror.semaphore.uw.io/test=true")
	testRunnerA := newGlobalRunner(
		fakeClient,
		fakeWatchClientA,
		"runnerA",
		"local-ns",
		testGlobalSvcLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
		selector,
		false,
	)
	testRunnerB := newGlobalRunner(
		fakeClient,
		fakeWatchClientB,
		"runnerB",
		"local-ns",
		testGlobalSvcLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
		selector,
		false,
	)

	go testRunnerA.serviceWatcher.Run()
	go testRunnerB.serviceWatcher.Run()
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunnerA.serviceWatcher.HasSynced)
	cache.WaitForNamedCacheSync("serviceWatcher", ctx.Done(), testRunnerB.serviceWatcher.HasSynced)

	expectedSpec := TestSpec{
		Ports:     existingPorts,
		ClusterIP: "",
		Selector:  nil,
	}
	expectedSvcs := []TestSvc{TestSvc{
		Name:      fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator),
		Namespace: "local-ns",
		Spec:      expectedSpec,
		Labels:    testGlobalLabels,
		Annotations: map[string]string{
			globalSvcClustersAnno:            "runnerA,runnerB",
			kubeSeviceTopologyAwareHintsAnno: kubeSeviceTopologyAwareHintsAnnoVal,
		},
	}}
	assertExpectedGlobalServices(ctx, t, expectedSvcs, fakeClient)
	// Deleting the service from cluster A should only edit the respective label
	testRunnerA.reconcileGlobalService("test-svc", "remote-ns")
	expectedSvcs[0].Annotations[globalSvcClustersAnno] = "runnerB"
	assertExpectedGlobalServices(ctx, t, expectedSvcs, fakeClient)

	// Deleting the service from cluster B should delete the global service
	testRunnerB.reconcileGlobalService("test-svc", "remote-ns")
	assertExpectedServices(ctx, t, []TestSvc{}, fakeClient)
}

func TestEndpointSliceSync(t *testing.T) {
	log.InitLogger("semaphore-service-mirror-test", "debug")
	testMirrorLabels := map[string]string{
		"mirrored-endpoint-slice":        "true",
		"mirror-endpointslice-sync-name": "test-runner",
	}
	// EndpointSlice on the remote cluster
	testEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-slice",
			Namespace: "remote-ns",
			Labels:    testGlobalSvcLabel,
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testEndpointSlice)

	// Create mirrored endpointslice
	mirroredEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateGlobalEndpointSliceName("test-slice"),
			Namespace: "local-ns",
			Labels:    generateEndpointSliceLabels(testMirrorLabels, "test-svc"),
		},
	}
	// Create stale endpointslice
	staleEndpointSlice := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateGlobalEndpointSliceName("old-slice"),
			Namespace: "local-ns",
			Labels:    generateEndpointSliceLabels(testMirrorLabels, "test-svc"),
		},
	}
	// feed them to the fake client
	fakeClient := fake.NewSimpleClientset(mirroredEndpointSlice, staleEndpointSlice)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testGlobalStore := newGlobalServiceStore()
	selector, _ := labels.Parse(testGlobalRoutingStrategyLabel)
	testRunner := newGlobalRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		testGlobalSvcLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
		selector,
		true,
	)
	go testRunner.endpointSliceWatcher.Run()
	go testRunner.mirrorEndpointSliceWatcher.Run()
	cache.WaitForNamedCacheSync(fmt.Sprintf("gl-%s-endpointSliceWatcher", testRunner.name), ctx.Done(), testRunner.endpointSliceWatcher.HasSynced)
	cache.WaitForNamedCacheSync(fmt.Sprintf("mirror-%s-endpointSliceWatcher", testRunner.name), ctx.Done(), testRunner.mirrorEndpointSliceWatcher.HasSynced)

	// EndpointSliceSync will trigger a sync. Verify that old endpointslice is deleted
	if err := testRunner.EndpointSliceSync(); err != nil {
		t.Fatal(err)
	}
	endpointslices, err := fakeClient.DiscoveryV1().EndpointSlices("").List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1, len(endpointslices.Items))
	assert.Equal(
		t,
		generateGlobalEndpointSliceName("test-slice"),
		endpointslices.Items[0].Name,
	)
}
