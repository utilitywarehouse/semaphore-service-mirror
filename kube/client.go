package kube

import (
	"errors"
	"fmt"
	"io"

	"crypto/tls"
	"encoding/pem"
	"io/ioutil"
	"net/http"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	// in case of local kube config
	// _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"github.com/utilitywarehouse/kube-service-mirror/log"
)

type CertMan struct {
	apiURL string
}

func (cm *CertMan) certificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	resp, err := http.Get(cm.apiURL)
	defer func() {
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}()
	if err != nil {
		log.Logger.Error(
			"error getting remote CA",
			"err", err)
		return nil, err
	}

	var cert tls.Certificate
	body, err := ioutil.ReadAll(resp.Body)

	// Discard "rest" - expecting a single block only
	block, _ := pem.Decode(body)
	if block == nil {
		log.Logger.Error("failed to parse certificate PEM")
		return nil, errors.New("failed to parse certificate PEM")
	}
	if block.Type == "CERTIFICATE" {
		cert.Certificate = append(cert.Certificate, block.Bytes)
	} else {
		log.Logger.Error(
			"PEM block is not Type CERTIFICATE",
			"type", block.Type)
		return nil, errors.New("failed to parse certificate PEM")
	}
	if len(cert.Certificate) == 0 {
		log.Logger.Error(
			"No certificates found",
			"url", cm.apiURL)
		return nil, errors.New("No certificates found")
	}
	return &cert, nil
}

// Client returns a Kubernetes client (clientset) from the kubeconfig path
// or from the in-cluster service account environment.
func Client(token, apiURL, caURL string) (*kubernetes.Clientset, error) {
	cm := &CertMan{apiURL}
	conf := &rest.Config{
		Host:        apiURL,
		Transport:   &http.Transport{TLSClientConfig: &tls.Config{GetCertificate: cm.certificate}},
		BearerToken: token,
	}
	return kubernetes.NewForConfig(conf)
}

// InClusterClient returns a Kubernetes client (clientset) from the in-cluster
// service account environment.
func InClusterClient() (*kubernetes.Clientset, error) {
	conf, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes client config: %v", err)
	}
	return kubernetes.NewForConfig(conf)
}
