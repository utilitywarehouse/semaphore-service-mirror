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
	flagGlobalSvcLabelSelector = flag.String("global-svc-label-selector", getEnv("SSM_GLOBAL_SVC_LABEL_SELECTOR", ""), "Label of services and endpoints to watch and mirror")
	flagKubeConfigPath         = flag.String("kube-config", getEnv("SSM_KUBE_CONFIG", ""), "Path of a kube config file, if not provided the app will try to get in cluster config")
	flagLogLevel               = flag.String("log-level", getEnv("SSM_LOG_LEVEL", "info"), "Log level")
	flagMirrorNamespace        = flag.String("mirror-ns", getEnv("SSM_MIRROR_NS", ""), "The namespace to create dummy mirror services in")
	flagMirrorSvcLabelSelector = flag.String("mirror-svc-label-selector", getEnv("SSM_MIRROR_SVC_LABEL_SELECTOR", ""), "Label of services and endpoints to watch and mirror")
	flagSSMConfig              = flag.String("config", getEnv("SSM_CONFIG", ""), "(required)Path to the json config file")

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
		*flagGlobalSvcLabelSelector,
		*flagMirrorSvcLabelSelector,
		*flagMirrorNamespace,
	)
	if err != nil {
		log.Logger.Error("Cannot parse config", "err", err)
		os.Exit(1)
	}
	// set DefaultLocalEndpointZones value for topology aware routing
	setLocalEndpointZones(config.LocalCluster.Zones)

	// Get a kube client for the local cluster
	homeClient, err := kube.ClientFromConfig(*flagKubeConfigPath)
	if err != nil {
		log.Logger.Error(
			"cannot create kube client for local cluster",
			"err", err,
		)
		usage()
	}

	gst := newGlobalServiceStore(homeClient)
	gr := makeGlobalRunner(homeClient, homeClient, config.LocalCluster.Name, config.Global, gst, true)
	go func() { backoff.Retry(gr.Run, "start runner") }()
	runners := []Runner{gr}
	for _, remote := range config.RemoteClusters {
		remoteClient, err := makeRemoteKubeClientFromConfig(remote)
		if err != nil {
			log.Logger.Error("cannot create kube client for remotecluster", "err", err)
			os.Exit(1)
		}
		mr := makeMirrorRunner(homeClient, remoteClient, remote, config.Global)
		runners = append(runners, mr)
		go func() { backoff.Retry(mr.Run, "start mirror runner") }()
		gr := makeGlobalRunner(homeClient, remoteClient, remote.Name, config.Global, gst, false)
		runners = append(runners, gr)
		go func() { backoff.Retry(gr.Run, "start mirror runner") }()
	}

	listenAndServe(runners)
	// Stop runners before finishing
	for _, r := range runners {
		r.Stop()
	}
}

func listenAndServe(runners []Runner) {
	sm := http.NewServeMux()
	sm.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		// A meaningful health check would be to verify that all runners
		// have started or kick the app otherwise via a liveness probe.
		// Client errors should be monitored via metrics.
		for _, r := range runners {
			if !r.Initialised() {
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

func makeRemoteKubeClientFromConfig(remote *remoteClusterConfig) (*kubernetes.Clientset, error) {
	if remote.KubeConfigPath != "" {
		return kube.ClientFromConfig(remote.KubeConfigPath)
	}
	// If kubeconfig path is not set, try to use craft it from the rest of the config
	data, err := os.ReadFile(remote.RemoteSATokenPath)
	if err != nil {
		return nil, fmt.Errorf("Cannot read file: %s: %v", remote.RemoteSATokenPath, err)
	}
	saToken := string(data)
	if saToken != "" {
		saToken = strings.TrimSpace(saToken)
		if !bearerRe.MatchString(saToken) {
			return nil, fmt.Errorf("The provided token does not match regex: %s", bearerRe.String())
		}
	}
	return kube.Client(saToken, remote.RemoteAPIURL, remote.RemoteCAURL)
}

func makeMirrorRunner(homeClient, remoteClient *kubernetes.Clientset, remote *remoteClusterConfig, global globalConfig) *MirrorRunner {
	return NewMirrorRunner(
		homeClient,
		remoteClient,
		remote.Name,
		global.MirrorNamespace,
		remote.ServicePrefix,
		global.MirrorSvcLabelSelector,
		// Resync will trigger an onUpdate event for everything that is
		// stored in cache.
		remote.ResyncPeriod.Duration,
		global.ServiceSync,
	)
}

func makeGlobalRunner(homeClient, remoteClient *kubernetes.Clientset, name string, global globalConfig, gst *GlobalServiceStore, localCluster bool) *GlobalRunner {
	return NewGlobalRunner(
		homeClient,
		remoteClient,
		name,
		global.MirrorNamespace,
		global.GlobalSvcLabelSelector,
		// TODO: Need to specify resync period?
		0,
		gst,
		localCluster,
	)
}
