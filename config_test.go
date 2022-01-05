package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	testFlagLabelSelector   = "flag-label"
	testFlagMirrorNamespace = "flag-namespace"
)

func TestConfig(t *testing.T) {
	emptyConfig := []byte(`
{
  "global": {}
}
`)
	_, err := parseConfig(emptyConfig, testFlagLabelSelector, testFlagMirrorNamespace)
	assert.Equal(t, fmt.Errorf("No remote cluster configuration defined"), err)

	globalConfigOnly := []byte(`
{
  "global": {
    "labelSelector": "label",
    "mirrorNamespace": "sys-semaphore"
  }
}
`)
	_, err = parseConfig(globalConfigOnly, testFlagLabelSelector, testFlagMirrorNamespace)
	assert.Equal(t, fmt.Errorf("No remote cluster configuration defined"), err)

	emptyRemoteConfigName := []byte(`
{
  "remoteClusters": [
    {
      "name": ""
    }
  ]
}
`)
	_, err = parseConfig(emptyRemoteConfigName, testFlagLabelSelector, testFlagMirrorNamespace)
	assert.Equal(t, fmt.Errorf("Configuration is missing remote cluster name"), err)
	insufficientRemoteKubeConfigPath := []byte(`
{
  "localCluster": {
    "name": "local_cluster",
    "kubeConfigPath": "/path/to/kube/config"
  },
  "remoteClusters": [
    {
      "name": "remote_cluster_1",
      "remoteCAURL": "remote_ca_url",
      "remoteAPIURL": "remote_api_url"
    }
  ]
}
`)
	_, err = parseConfig(insufficientRemoteKubeConfigPath, testFlagLabelSelector, testFlagMirrorNamespace)
	assert.Equal(t, fmt.Errorf("Insufficient configuration to create remote cluster client. Set kubeConfigPath or remoteAPIURL and remoteCAURL and remoteSATokenPath"), err)

	rawFullConfig := []byte(`
{
  "global": {
    "serviceSync": true
  },
  "localCluster": {
    "name": "local_cluster",
    "kubeConfigPath": "/path/to/kube/config"
  },
  "remoteClusters": [
    {
      "name": "remote_cluster_1",
      "remoteCAURL": "remote_ca_url",
      "remoteAPIURL": "remote_api_url",
      "remoteSATokenPath": "/path/to/token",
      "resyncPeriod": "10s",
      "servicePrefix": "cluster-1"
    },
    {
      "name": "remote_cluster_2",
      "kubeConfigPath": "/path/to/kube/config",
      "servicePrefix": "cluster-2"
    }
  ]
}
`)
	config, err := parseConfig(rawFullConfig, testFlagLabelSelector, testFlagMirrorNamespace)
	assert.Equal(t, nil, err)
	assert.Equal(t, testFlagLabelSelector, config.Global.LabelSelector)
	assert.Equal(t, testFlagMirrorNamespace, config.Global.MirrorNamespace)
	assert.Equal(t, true, config.Global.ServiceSync)
	assert.Equal(t, "/path/to/kube/config", config.LocalCluster.KubeConfigPath)
	assert.Equal(t, 2, len(config.RemoteClusters))
	assert.Equal(t, "remote_ca_url", config.RemoteClusters[0].RemoteCAURL)
	assert.Equal(t, "remote_api_url", config.RemoteClusters[0].RemoteAPIURL)
	assert.Equal(t, "/path/to/token", config.RemoteClusters[0].RemoteSATokenPath)
	assert.Equal(t, "", config.RemoteClusters[0].KubeConfigPath)
	assert.Equal(t, Duration{10 * time.Second}, config.RemoteClusters[0].ResyncPeriod)
	assert.Equal(t, "cluster-1", config.RemoteClusters[0].ServicePrefix)
	assert.Equal(t, "remote_cluster_2", config.RemoteClusters[1].Name)
	assert.Equal(t, "", config.RemoteClusters[1].RemoteCAURL)
	assert.Equal(t, "", config.RemoteClusters[1].RemoteAPIURL)
	assert.Equal(t, "", config.RemoteClusters[1].RemoteSATokenPath)
	assert.Equal(t, "/path/to/kube/config", config.RemoteClusters[1].KubeConfigPath)
	assert.Equal(t, Duration{0}, config.RemoteClusters[1].ResyncPeriod)
	assert.Equal(t, "cluster-2", config.RemoteClusters[1].ServicePrefix)

}
