package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	testFlagGlobalSvcLabelSelector = "flag-global-label"
	testFlagGlobalSvcTopologyLabel = "flag-global-topology-label"
	testFlagMirrorSvcLabelSelector = "flag-mirror-label"
	testFlagMirrorNamespace        = "flag-namespace"
)

func TestConfig(t *testing.T) {
	emptyConfig := []byte(`
{
  "global": {}
}
`)
	_, err := parseConfig(emptyConfig, testFlagGlobalSvcLabelSelector, testFlagGlobalSvcTopologyLabel, testFlagMirrorSvcLabelSelector, testFlagMirrorNamespace)
	assert.Equal(t, fmt.Errorf("Configuration is missing local cluster name"), err)

	globalConfigOnly := []byte(`
{
  "global": {
    "globalSvcLabelSelector": "globalLabel",
    "globalSvcTopologyLabel": "globalTopologyLabel",
    "mirrorSvcLabelSelector": "label",
    "mirrorNamespace": "sys-semaphore"
  },
  "localCluster":{
    "name": "local_cluster"
  }
}
`)
	_, err = parseConfig(globalConfigOnly, testFlagGlobalSvcLabelSelector, testFlagGlobalSvcTopologyLabel, testFlagMirrorSvcLabelSelector, testFlagMirrorNamespace)
	assert.Equal(t, fmt.Errorf("No remote cluster configuration defined"), err)

	emptyRemoteConfigName := []byte(`
{
  "localCluster":{
    "name": "local_cluster"
  },
  "remoteClusters": [
    {
      "name": ""
    }
  ]
}
`)
	_, err = parseConfig(emptyRemoteConfigName, testFlagGlobalSvcLabelSelector, testFlagGlobalSvcTopologyLabel, testFlagMirrorSvcLabelSelector, testFlagMirrorNamespace)
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
	_, err = parseConfig(insufficientRemoteKubeConfigPath, testFlagGlobalSvcLabelSelector, testFlagGlobalSvcTopologyLabel, testFlagMirrorSvcLabelSelector, testFlagMirrorNamespace)
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
	config, err := parseConfig(rawFullConfig, testFlagGlobalSvcLabelSelector, testFlagGlobalSvcTopologyLabel, testFlagMirrorSvcLabelSelector, testFlagMirrorNamespace)
	assert.Equal(t, nil, err)
	assert.Equal(t, testFlagGlobalSvcLabelSelector, config.Global.GlobalSvcLabelSelector)
	assert.Equal(t, testFlagGlobalSvcTopologyLabel, config.Global.GlobalSvcTopologyLabel)
	assert.Equal(t, testFlagMirrorSvcLabelSelector, config.Global.MirrorSvcLabelSelector)
	assert.Equal(t, testFlagMirrorNamespace, config.Global.MirrorNamespace)
	assert.Equal(t, true, config.Global.ServiceSync)
	assert.Equal(t, "local_cluster", config.LocalCluster.Name)
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
