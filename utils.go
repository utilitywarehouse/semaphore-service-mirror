package main

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

const (
	// Separator is inserted between the namespace and name in the mirror
	// name to prevent clashes
	Separator = "73736d"
)

var (
	// DefaultLocalEndpointZones holds the configured availability zones for
	// the local cluster
	DefaultLocalEndpointZones []discoveryv1.ForZone
)

// generateMirrorName generates a name for mirrored objects based on the name
// and namespace of the remote object: <prefix>-<namespace>-73736d-<name>
func generateMirrorName(prefix, namespace, name string) string {
	return fmt.Sprintf("%s-%s-%s-%s", prefix, namespace, Separator, name)
}

// generateGlobalServiceName generates a name for mirrored objects based on the
// name and namespace of the remote object: gl-<namespace>-73736d-<name>
func generateGlobalServiceName(name, namespace string) string {
	return fmt.Sprintf("gl-%s-%s-%s", namespace, Separator, name)
}

// generateGlobalEndpointSliceName just prefixes the name with `gl-`, and relies
// on kubernetes suffixes for endpointslices to not collide.
func generateGlobalEndpointSliceName(name string) string {
	return fmt.Sprintf("gl-%s", name)
}

func generateEndpointSliceLabels(targetService string) map[string]string {
	labels := map[string]string{}
	labels["kubernetes.io/service-name"] = targetService
	labels["endpointslice.kubernetes.io/managed-by"] = "semaphore-service-mirror"
	return labels
}

func setLocalEndpointZones(zones []string) {
	for _, z := range zones {
		DefaultLocalEndpointZones = append(DefaultLocalEndpointZones, discoveryv1.ForZone{Name: z})
	}
}

func inSlice(slice []string, val string) (int, bool) {
	for i, item := range slice {
		if item == val {
			return i, true
		}
	}
	return -1, false
}

func removeFromSlice(slice []string, i int) []string {
	slice[len(slice)-1], slice[i] = slice[i], slice[len(slice)-1]
	return slice[:len(slice)-1]
}

func isHeadless(svc *v1.Service) bool {
	if svc.Spec.ClusterIP == "None" {
		return true
	}
	return false
}
