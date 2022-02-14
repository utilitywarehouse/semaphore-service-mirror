package main

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	defaultWGDeviceMTU  = 1420
	defaultWGListenPort = 51820
)

// Duration is a helper to unmarshal time.Duration from json
// https://stackoverflow.com/questions/48050945/how-to-unmarshal-json-into-durations/54571600#54571600
type Duration struct {
	time.Duration
}

// MarshalJSON calls json Marshall on Duration
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON provides handling of time.Duration when unmarshalling
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		d.Duration = tmp
		return nil
	default:
		return fmt.Errorf("Invalid duration of type %v", value)
	}
}

// globalConfig will keep configuration that applies globally on the operator
type globalConfig struct {
	GlobalSvcLabelSelector        string `json:"globalSvcLabelSelector"`   // Label used to select global services to mirror
	GlobalSvcRoutingStrategyLabel string `json:"globalSvcRoutingStrategy"` // Label used to enable topology aware hints for global services
	MirrorSvcLabelSelector        string `json:"mirrorSvcLabelSelector"`   // Label used to select remote services to mirror
	MirrorNamespace               string `json:"mirrorNamespace"`          // Local namespace to mirror remote services
	ServiceSync                   bool   `json:"serviceSync"`              // sync services on startup
}

type localClusterConfig struct {
	Name           string   `json:"name"`
	KubeConfigPath string   `json:"kubeConfigPath"`
	Zones          []string `json:"zones"`
}

type remoteClusterConfig struct {
	Name              string   `json:"name"`
	KubeConfigPath    string   `json:"kubeConfigPath"`
	RemoteAPIURL      string   `json:"remoteAPIURL"`
	RemoteCAURL       string   `json:"remoteCAURL"`
	RemoteSATokenPath string   `json:"remoteSATokenPath"`
	ResyncPeriod      Duration `json:"resyncPeriod"`
	ServicePrefix     string   `json:"servicePrefix"` // How to prefix services mirrored from this cluster locally
}

// Config holds the application configuration
type Config struct {
	Global         globalConfig           `json:"global"`
	LocalCluster   localClusterConfig     `json:"localCluster"`
	RemoteClusters []*remoteClusterConfig `json:"remoteClusters"`
}

func parseConfig(rawConfig []byte, flagGlobalSvcLabelSelector, flagGlobalSvcRoutingStrategyLabel, flagMirrorSvcLabelSelector, flagMirrorNamespace string) (*Config, error) {
	conf := &Config{}
	if err := json.Unmarshal(rawConfig, conf); err != nil {
		return nil, fmt.Errorf("error unmarshalling config: %v", err)
	}
	// Override global config via flags/env vars and check
	if flagMirrorSvcLabelSelector != "" {
		conf.Global.MirrorSvcLabelSelector = flagMirrorSvcLabelSelector
	}
	if conf.Global.MirrorSvcLabelSelector == "" {
		return nil, fmt.Errorf("Label selector for service mirroring should be specified either via global json config, env vars or flag")
	}
	if flagGlobalSvcLabelSelector != "" {
		conf.Global.GlobalSvcLabelSelector = flagGlobalSvcLabelSelector
	}
	if conf.Global.GlobalSvcLabelSelector == "" {
		return nil, fmt.Errorf("Label selector for global services should be specified either via global json config, env vars or flag")
	}
	if flagGlobalSvcRoutingStrategyLabel != "" {
		conf.Global.GlobalSvcRoutingStrategyLabel = flagGlobalSvcRoutingStrategyLabel
	}
	if conf.Global.GlobalSvcRoutingStrategyLabel == "" {
		return nil, fmt.Errorf("Label to enable topology aware hints for global services should be specified either via global json config, env vars or flag")
	}
	if flagMirrorNamespace != "" {
		conf.Global.MirrorNamespace = flagMirrorNamespace
	}
	if conf.Global.MirrorNamespace == "" {
		return nil, fmt.Errorf("Local mirroring namespace should be specified either via global json config, env vars or flag")
	}
	if conf.LocalCluster.Name == "" {
		return nil, fmt.Errorf("Configuration is missing local cluster name")
	}
	// If local cluster zones are not set, default to a dummy value, so that kube-proxy does not complain
	if len(conf.LocalCluster.Zones) == 0 {
		conf.LocalCluster.Zones = []string{"local"}
	}

	// Check for mandatory remote config.
	if len(conf.RemoteClusters) < 1 {
		return nil, fmt.Errorf("No remote cluster configuration defined")
	}
	for _, r := range conf.RemoteClusters {
		if r.Name == "" {
			return nil, fmt.Errorf("Configuration is missing remote cluster name")
		}
		if (r.RemoteAPIURL == "" || r.RemoteCAURL == "" || r.RemoteSATokenPath == "") && r.KubeConfigPath == "" {
			return nil, fmt.Errorf("Insufficient configuration to create remote cluster client. Set kubeConfigPath or remoteAPIURL and remoteCAURL and remoteSATokenPath")
		}
		if r.ServicePrefix == "" {
			return nil, fmt.Errorf("Configuration is missing a service prefix for services mirrored from the remote")
		}
	}
	return conf, nil
}
