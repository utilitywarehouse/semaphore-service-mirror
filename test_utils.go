package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestSvc is a struct to make expected types for assertions
type TestSvc struct {
	Name        string
	Namespace   string
	Spec        TestSpec
	Labels      map[string]string
	Annotations map[string]string
}

// TestSpec represents the test service spec
type TestSpec struct {
	Ports     []v1.ServicePort
	Selector  map[string]string
	ClusterIP string
}

func assertExpectedServices(ctx context.Context, t *testing.T, expectedSvcs []TestSvc, fakeClient *fake.Clientset) {
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

// assertExpectediGlobalServices will also checck global service labels and annotations
func assertExpectedGlobalServices(ctx context.Context, t *testing.T, expectedSvcs []TestSvc, fakeClient *fake.Clientset) {
	svcs, err := fakeClient.CoreV1().Services("").List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	for i, expected := range expectedSvcs {
		assert.Equal(t, expected.Labels["global-svc"], svcs.Items[i].Labels["global-svc"])
		assert.Equal(t, expected.Annotations[kubeSeviceTopologyAwareHintsAnno], svcs.Items[i].Annotations[kubeSeviceTopologyAwareHintsAnno])
		assert.Equal(t, expected.Annotations[globalSvcClustersAnno], svcs.Items[i].Annotations[globalSvcClustersAnno])
	}
	assertExpectedServices(ctx, t, expectedSvcs, fakeClient)
}
