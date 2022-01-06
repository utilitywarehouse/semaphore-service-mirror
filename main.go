package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/utilitywarehouse/semaphore-service-mirror/kube"
	"github.com/utilitywarehouse/semaphore-service-mirror/log"
	"k8s.io/client-go/kubernetes"

	"github.com/utilitywarehouse/semaphore-service-mirror/backoff"
	_ "github.com/utilitywarehouse/semaphore-service-mirror/metrics"
)

var (
	flagKubeConfigPath  = flag.String("kube-config", getEnv("SSM_KUBE_CONFIG", ""), "Path of a kube config file, if not provided the app will try to get in cluster config")
	flagLogLevel        = flag.String("log-level", getEnv("SSM_LOG_LEVEL", "info"), "Log level")
	flagMirrorNamespace = flag.String("mirror-ns", getEnv("SSM_MIRROR_NS", ""), "The namespace to create dummy mirror services in")
	flagLabelSelector   = flag.String("label-selector", getEnv("SSM_LABEL_SELECTOR", ""), "Label of services and endpoints to watch and mirror")
	flagSSMConfig       = flag.String("config", getEnv("SSM_CONFIG", ""), "(required)Path to the json config file")

	bearerRe = regexp.MustCompile(`[A-Z|a-z0-9\-\._~\+\/]+=*`)
)

func usage() {
	flag.Usage()
	os.Exit(1)
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

func main() {
	var err error
	flag.Parse()
	log.InitLogger("semaphore-service-mirror", *flagLogLevel)

	// Config file path cannot be empty
	if *flagSSMConfig == "" {
		usage()
	}
	fileContent, err := os.ReadFile(*flagSSMConfig)
	if err != nil {
		log.Logger.Error("Cannot read config file", "err", err)
		os.Exit(1)
	}
	config, err := parseConfig(
		fileContent,
		*flagLabelSelector,
		*flagMirrorNamespace,
	)
	if err != nil {
		log.Logger.Error("Cannot parse config", "err", err)
		os.Exit(1)
	}

	// Get a kube client to use with the watchers
	homeClient, err := kube.ClientFromConfig(*flagKubeConfigPath)
	if err != nil {
		log.Logger.Error(
			"cannot create kube client for homecluster",
			"err", err,
		)
		usage()
	}

	var runners []*Runner
	for _, remote := range config.RemoteClusters {
		r, err := makeRunner(homeClient, remote, config.Global)
		if err != nil {
			log.Logger.Error("Failed to create runner", "err", err)
			os.Exit(1)
		}
		runners = append(runners, r)
		go func() {
			backoff.Retry(r.Run, "start runner")
		}()
	}

	listenAndServe(runners)
	// Stop runners before finishing
	for _, r := range runners {
		r.Stop()
	}
}

func listenAndServe(runners []*Runner) {
	sm := http.NewServeMux()
	sm.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		// A meaningful health check would be to verify that all runners
		// have started or kick the app otherwise via a liveness probe.
		// Client errors should be monitored via metrics.
		for _, r := range runners {
			if !r.initialised {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	sm.Handle("/metrics", promhttp.Handler())
	log.Logger.Error(
		"Listen and Serve",
		"err", http.ListenAndServe(":8080", sm),
	)
}

func makeRunner(homeClient kubernetes.Interface, remote *remoteClusterConfig, global globalConfig) (*Runner, error) {
	data, err := os.ReadFile(remote.RemoteSATokenPath)
	if err != nil {
		return nil, fmt.Errorf("Cannot read file: %s: %v", remote.RemoteSATokenPath, err)
	}
	saToken := string(data)
	if saToken != "" {
		saToken = strings.TrimSpace(saToken)
		if !bearerRe.Match([]byte(saToken)) {
			return nil, fmt.Errorf("The provided token does not match regex: %s", bearerRe.String())
		}
	}
	var remoteClient *kubernetes.Clientset
	if remote.KubeConfigPath != "" {
		remoteClient, err = kube.ClientFromConfig(remote.KubeConfigPath)
	} else {
		remoteClient, err = kube.Client(saToken, remote.RemoteAPIURL, remote.RemoteCAURL)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot create kube client for remotecluster %v", err)
	}
	return NewRunner(
		homeClient,
		remoteClient,
		remote.Name,
		global.MirrorNamespace,
		remote.ServicePrefix,
		global.LabelSelector,
		// Resync will trigger an onUpdate event for everything that is
		// stored in cache.
		remote.ResyncPeriod.Duration,
		global.ServiceSync,
	), nil
}
