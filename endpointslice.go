package main

import (
	v1 "k8s.io/api/core/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
)

const (
	endpointSliceControllerName = "kube-service-mirror"
)

func endpointsToEndpointSlices(endpoints *v1.Endpoints) ([]*discoveryv1beta1.EndpointSlice, error) {
	// Setting the ownerRef to the Endpoints resource from which the
	// EndpointSlices were generated ties their lifecycle to the it, which means
	// we do not have to handle deletion as they will be removed once the owner
	// Endpoints is deleted.
	ownerRef := metav1.NewControllerRef(endpoints, schema.GroupVersionKind{Version: "v1", Kind: "Endpoints"})
	ess := make([]*discoveryv1beta1.EndpointSlice, len(endpoints.Subsets))
	for i, e := range endpoints.Subsets {
		ports := make([]discoveryv1beta1.EndpointPort, len(e.Ports))
		for j, p := range e.Ports {
			ports[j] = discoveryv1beta1.EndpointPort{
				Name:     pointer.StringPtr(p.Name),
				Protocol: v1ProtocolPtr(p.Protocol),
				Port:     pointer.Int32Ptr(p.Port),
				// TODO: AppProtocol in v1.EndpointPort is only available from
				// kubernetes 1.18 forward (https://github.com/kubernetes/kubernetes/pull/88503)
				// AppProtocol: p.AppProtocol,
			}
		}
		// TODO: handle maxEndpoints and generate multiple EndpointSlices if
		// needed
		eps := make([]discoveryv1beta1.Endpoint, len(e.Addresses)+len(e.NotReadyAddresses))
		for j, a := range e.Addresses {
			eps[j] = endpointAddressToEndpoint(a, true)
		}
		for j, a := range e.NotReadyAddresses {
			eps[len(e.Addresses)+j] = endpointAddressToEndpoint(a, false)
		}
		ess[i] = &discoveryv1beta1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					discoveryv1beta1.LabelServiceName: endpoints.Name,
					discoveryv1beta1.LabelManagedBy:   endpointSliceControllerName,
				},
				GenerateName:    getEndpointSlicePrefix(endpoints.Name),
				OwnerReferences: []metav1.OwnerReference{*ownerRef},
				Namespace:       endpoints.Namespace,
			},
			Ports: ports,
			// TODO: Currently, all addresses will be IPv4 but we should detect
			// the type and set this value appropriately.
			AddressType: discoveryv1beta1.AddressTypeIPv4,
			Endpoints:   eps,
		}
	}
	return ess, nil
}

func endpointAddressToEndpoint(address v1.EndpointAddress, ready bool) discoveryv1beta1.Endpoint {
	ep := discoveryv1beta1.Endpoint{
		Addresses:  []string{address.IP},
		Conditions: discoveryv1beta1.EndpointConditions{Ready: pointer.BoolPtr(ready)},
		TargetRef:  address.TargetRef,
		// Since this Endpoint points to another cluster, we cannot use Service
		// Topology features.
		Topology: nil,
	}
	// If the Hostname attribute is empty, use a nil pointer instead of a
	// pointer to an empty string since "" is not a valid hostname.
	if address.Hostname != "" {
		ep.Hostname = pointer.StringPtr(address.Hostname)
	}
	return ep
}

func getEndpointSlicePrefix(endpointsName string) string {
	prefix := endpointsName + "-"
	if len(apimachineryvalidation.NameIsDNSSubdomain(prefix, true)) != 0 {
		prefix = endpointsName
	}
	return prefix
}

func v1ProtocolPtr(protocol v1.Protocol) *v1.Protocol {
	return &protocol
}
