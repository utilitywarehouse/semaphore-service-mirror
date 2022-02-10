package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/utilitywarehouse/semaphore-service-mirror/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

var (
	testGlobalLabels            = map[string]string{"global-svc": "true"}
	testGlobalMirrorLabel       = map[string]string{"mirror.semaphore.uw.io/global-service": "true"}
	testGlobalMirrorLabelString = "mirror.semaphore.uw.io/global-service=true"
	testServiceSelector         = map[string]string{"selector": "x"}
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
			Labels:    testGlobalMirrorLabel,
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  testServiceSelector,
			ClusterIP: "1.1.1.1",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)
	testGlobalStore := newGlobalServiceStore()

	testRunner := newGlobalRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		testGlobalMirrorLabelString,
		60*time.Minute,
		testGlobalStore,
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
	// Test will appear alphabetically sorted in the client response
	expectedSvcs := []TestSvc{
		TestSvc{
			Name:      fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator),
			Namespace: "local-ns",
			Spec:      expectedSpec,
			Labels:    testGlobalLabels,
			Annotations: map[string]string{
				globalSvcClustersAnno:            "test-runner",
				kubeSeviceTopologyAwareHintsAnno: kubeSeviceTopologyAwareHintsAnnoVal,
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
			Labels:    testGlobalMirrorLabel,
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  testServiceSelector,
			ClusterIP: "None",
		},
	}
	fakeWatchClient := fake.NewSimpleClientset(testSvc)
	testGlobalStore := newGlobalServiceStore()

	testRunner := newGlobalRunner(
		fakeClient,
		fakeWatchClient,
		"test-runner",
		"local-ns",
		testGlobalMirrorLabelString,
		60*time.Minute,
		testGlobalStore,
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
				globalSvcClustersAnno:            "test-runner",
				kubeSeviceTopologyAwareHintsAnno: kubeSeviceTopologyAwareHintsAnnoVal,
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
			Labels:    testGlobalMirrorLabel,
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
			Labels:    testGlobalMirrorLabel,
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

	testRunnerA := newGlobalRunner(
		fakeClient,
		fakeWatchClientA,
		"runnerA",
		"local-ns",
		testGlobalMirrorLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
	)
	testRunnerB := newGlobalRunner(
		fakeClient,
		fakeWatchClientB,
		"runnerB",
		"local-ns",
		testGlobalMirrorLabelString,
		60*time.Minute,
		testGlobalStore,
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
			globalSvcClustersAnno:            "runnerA",
			kubeSeviceTopologyAwareHintsAnno: kubeSeviceTopologyAwareHintsAnnoVal,
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
	labels := globalSvcLabels
	annotations := globalSvcAnnotations
	annotations[globalSvcClustersAnno] = "runnerA,runnerB"
	testGlobalStore.store[fmt.Sprintf("gl-remote-ns-%s-test-svc", Separator)] = &GlobalService{
		name:        "test-svc",
		namespace:   "remote-ns",
		ports:       existingPorts,
		labels:      labels,
		annotations: annotations,
		clusters:    []string{"runnerA", "runnerB"},
	}

	// Remote fake clients won't have any services as we are deleting
	fakeWatchClientA := fake.NewSimpleClientset()
	fakeWatchClientB := fake.NewSimpleClientset()

	testRunnerA := newGlobalRunner(
		fakeClient,
		fakeWatchClientA,
		"runnerA",
		"local-ns",
		testGlobalMirrorLabelString,
		60*time.Minute,
		testGlobalStore,
		false,
	)
	testRunnerB := newGlobalRunner(
		fakeClient,
		fakeWatchClientB,
		"runnerB",
		"local-ns",
		testGlobalMirrorLabelString,
		60*time.Minute,
		testGlobalStore,
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
