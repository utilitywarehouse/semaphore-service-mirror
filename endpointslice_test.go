package main

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/pointer"
)

const (
	randStringCharset = "abcdefghijklmnopqrstuvwxyz0123456789"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = randStringCharset[rand.Int63()%int64(len(randStringCharset))]
	}
	return string(b)
}

func TestGetEndpointSlicePrefix(t *testing.T) {
	en := randString(10)
	if v := getEndpointSlicePrefix(en); v != en+"-" {
		t.Errorf("getEndpointSlicePrefix: expected %s, got %s", en+"-", v)
	}
	en = randString(validation.DNS1123SubdomainMaxLength)
	if v := getEndpointSlicePrefix(en); v != en+"-" {
		t.Errorf("getEndpointSlicePrefix: expected %s, got %s", en+"-", v)
	}
	en = randString(validation.DNS1123SubdomainMaxLength + 1)
	if v := getEndpointSlicePrefix(en); v != en {
		t.Errorf("getEndpointSlicePrefix: expected %s, got %s", en, v)
	}
}

func TestEndpointsToEndpointSlices(t *testing.T) {
	testCases := []struct {
		in  *v1.Endpoints
		out []*discoveryv1beta1.EndpointSlice
	}{
		{
			&v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Subsets: []v1.EndpointSubset{
					{
						Addresses: []v1.EndpointAddress{
							{
								IP:        "1.0.0.1",
								Hostname:  "foo1.bar.baz",
								NodeName:  pointer.StringPtr(""),
								TargetRef: nil,
							},
						},
						NotReadyAddresses: []v1.EndpointAddress{
							{
								IP:        "1.0.0.2",
								Hostname:  "foo2.bar.baz",
								NodeName:  pointer.StringPtr(""),
								TargetRef: nil,
							},
						},
						Ports: []v1.EndpointPort{
							{
								Name:     "foo",
								Port:     12345,
								Protocol: v1.ProtocolTCP,
							},
						},
					},
				},
			},
			[]*discoveryv1beta1.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "foo-",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         "v1",
								Kind:               "Endpoints",
								Name:               "foo",
								BlockOwnerDeletion: pointer.BoolPtr(true),
								Controller:         pointer.BoolPtr(true),
							},
						},
						Labels: map[string]string{
							discoveryv1beta1.LabelServiceName: "foo",
							discoveryv1beta1.LabelManagedBy:   endpointSliceControllerName,
						},
					},
					AddressType: discoveryv1beta1.AddressTypeIPv4,
					Endpoints: []discoveryv1beta1.Endpoint{
						{
							Addresses:  []string{"1.0.0.1"},
							Conditions: discoveryv1beta1.EndpointConditions{Ready: pointer.BoolPtr(true)},
							Hostname:   pointer.StringPtr("foo1.bar.baz"),
							TargetRef:  nil,
							Topology:   nil,
						},
						{
							Addresses:  []string{"1.0.0.2"},
							Conditions: discoveryv1beta1.EndpointConditions{Ready: pointer.BoolPtr(false)},
							Hostname:   pointer.StringPtr("foo2.bar.baz"),
							TargetRef:  nil,
							Topology:   nil,
						},
					},
					Ports: []discoveryv1beta1.EndpointPort{
						{
							Name:     pointer.StringPtr("foo"),
							Port:     pointer.Int32Ptr(12345),
							Protocol: v1ProtocolPtr(v1.ProtocolTCP),
						},
					},
				},
			},
		},
	}

	for i, tc := range testCases {
		o, err := endpointsToEndpointSlices(tc.in)
		if err != nil {
			t.Errorf("endpointsToEndpointSlices: test case #%d returned an error: %v", i, err)
		}
		if diff := cmp.Diff(tc.out, o); diff != "" {
			t.Errorf("endpointsToEndpointSlices: test case #%d did not return expected output:\n%s", i, diff)
		}
	}
}

func TestEndpointAdressToEndpoint(t *testing.T) {
	testCases := []struct {
		in    v1.EndpointAddress
		ready bool
		out   discoveryv1beta1.Endpoint
	}{
		{
			v1.EndpointAddress{
				IP:        "1.2.3.4",
				Hostname:  "",
				NodeName:  pointer.StringPtr(""),
				TargetRef: nil,
			},
			true,
			discoveryv1beta1.Endpoint{
				Addresses:  []string{"1.2.3.4"},
				Conditions: discoveryv1beta1.EndpointConditions{Ready: pointer.BoolPtr(true)},
				Hostname:   nil,
				TargetRef:  nil,
				Topology:   nil,
			},
		},
		{
			v1.EndpointAddress{
				IP:        "1.2.3.4",
				Hostname:  "foo.bar.baz",
				NodeName:  pointer.StringPtr(""),
				TargetRef: nil,
			},
			false,
			discoveryv1beta1.Endpoint{
				Addresses:  []string{"1.2.3.4"},
				Conditions: discoveryv1beta1.EndpointConditions{Ready: pointer.BoolPtr(false)},
				Hostname:   pointer.StringPtr("foo.bar.baz"),
				TargetRef:  nil,
				Topology:   nil,
			},
		},
	}

	for i, tc := range testCases {
		o := endpointAddressToEndpoint(tc.in, tc.ready)
		if diff := cmp.Diff(tc.out, o); diff != "" {
			t.Errorf("endpointAdressToEndpoint: test case #%d did not return expected output:\n%s", i, diff)
		}
	}
}
