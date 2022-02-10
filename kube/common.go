package kube

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GetService returns a client get request for a service under a namespace
func GetService(ctx context.Context, client kubernetes.Interface, name, namespace string) (*v1.Service, error) {
	return client.CoreV1().Services(namespace).Get(
		ctx,
		name,
		metav1.GetOptions{},
	)
}

// CreateService creates a clusterIP or headless type service.
func CreateService(ctx context.Context, client kubernetes.Interface, name, namespace string, labels, annotations map[string]string, ports []v1.ServicePort, headless bool) (*v1.Service, error) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Ports:    ports,
			Selector: nil,
		},
	}
	if headless {
		svc.Spec.ClusterIP = "None"
	}
	return client.CoreV1().Services(namespace).Create(
		ctx,
		svc,
		metav1.CreateOptions{},
	)
}

// UpdateService updates service ports. No need to cater for headless services
// as clusterIP field is immutable and will fail.
func UpdateService(ctx context.Context, client kubernetes.Interface, service *v1.Service, ports []v1.ServicePort) (*v1.Service, error) {
	service.Spec.Ports = ports
	service.Spec.Selector = nil

	return client.CoreV1().Services(service.Namespace).Update(
		ctx,
		service,
		metav1.UpdateOptions{},
	)
}

// DeleteService returns a client delete service request
func DeleteService(ctx context.Context, client kubernetes.Interface, name, namespace string) error {
	return client.CoreV1().Services(namespace).Delete(
		ctx,
		name,
		metav1.DeleteOptions{},
	)
}
