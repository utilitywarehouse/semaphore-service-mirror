package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestMatchSelector_MatchService(t *testing.T) {
	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels: map[string]string{
				"mirror.semaphore.uw.io/test":   "true",
				"mirror.semaphore.uw.io/random": "true",
			},
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  map[string]string{"selector": "x"},
			ClusterIP: "1.1.1.1",
		},
	}
	selector, err := labels.Parse("mirror.semaphore.uw.io/test=true")
	if err != nil {
		t.Fatal(err)
	}
	res := matchSelector(selector, testSvc)
	assert.Equal(t, true, res)
}

func TestMatchSelector_NoServiceMatch(t *testing.T) {
	testPorts := []v1.ServicePort{v1.ServicePort{Port: 1}}
	testSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "remote-ns",
			Labels: map[string]string{
				"mirror.semaphore.uw.io/random": "true",
			},
		},
		Spec: v1.ServiceSpec{
			Ports:     testPorts,
			Selector:  map[string]string{"selector": "x"},
			ClusterIP: "1.1.1.1",
		},
	}
	selector, err := labels.Parse("mirror.semaphore.uw.io/test=true")
	if err != nil {
		t.Fatal(err)
	}
	res := matchSelector(selector, testSvc)
	assert.Equal(t, false, res)
}
