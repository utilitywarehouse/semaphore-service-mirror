package main

import (
	"flag"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/utilitywarehouse/kube-service-mirror/kube"
	"github.com/utilitywarehouse/kube-service-mirror/log"
	"k8s.io/client-go/kubernetes"
)

var (
	flagKubeConfigPath       = flag.String("kube-config", "", "Path of a kube config file, if not provided the app will try to get in cluster config")
	flagTargetKubeConfigPath = flag.String("target-kube-config", "", "Path of the target cluster kube config file to mirrot services from")
	flagLogLevel             = flag.String("log-level", "info", "Log level, defaults to info")
	flagResyncPeriod         = flag.Duration("resync-period", 60*time.Minute, "Namespace watcher cache resync period")
	flagMirrorNamespace      = flag.String("mirror-ns", "", "The namespace to create dummy mirror services in")
	flagSvcPrefix            = flag.String("svc-prefix", "", "(required) A prefix to apply on all mirrored services names. Will also be used for initial service sync")
	flagLabelSelector        = flag.String("label-selector", "", "(required) Label of services and endpoints to watch and mirror")
	flagSvcSync              = flag.Bool("svc-sync", true, "sync services on startup")

	saToken = os.Getenv("SERVICE_ACCOUNT_TOKEN")
	apiURL  = os.Getenv("KUBE_API_SERVER")
	caURL   = os.Getenv("REMOTE_CLUSTER_CA_CERT_URL")

	bearerRe = regexp.MustCompile(`[A-Z|a-z0-9\-\._~\+\/]+=*`)
)

func usage() {
	flag.Usage()
}

func main() {
	flag.Parse()

	if *flagLabelSelector == "" {
		usage()
	}

	if *flagSvcPrefix == "" {
		usage()
	}

	if saToken != "" {
		saToken = strings.TrimSuffix(saToken, "\n")
		if !bearerRe.Match([]byte(saToken)) {
			log.Logger.Error(
				"The provided token does not match regex",
				"regex", bearerRe.String)
			os.Exit(1)
		}
	}

	// Create a label to help syncing on startup
	MirrorLabels["mirror-svc-prefix-sync"] = *flagSvcPrefix

	log.InitLogger("kube-service-mirror", *flagLogLevel)

	// Get a kube client to use with the watchers
	homeClient, err := kube.ClientFromConfig(*flagKubeConfigPath)
	if err != nil {
		log.Logger.Error(
			"cannot create kube client for homecluster",
			"err", err,
		)
		usage()
	}

	var remoteClient *kubernetes.Clientset
	if *flagTargetKubeConfigPath != "" {
		remoteClient, err = kube.ClientFromConfig(*flagTargetKubeConfigPath)
	} else {
		remoteClient, err = kube.Client(saToken, apiURL, caURL)
	}
	if err != nil {
		log.Logger.Error(
			"cannot create kube client for remotecluster",
			"err", err,
		)
		usage()
	}

	runner := NewRunner(
		homeClient,
		remoteClient,
		*flagMirrorNamespace,
		*flagSvcPrefix,
		*flagLabelSelector,
		// Resync will trigger an onUpdate event for everything that is
		// stored in cache.
		*flagResyncPeriod,
		*flagSvcSync,
	)
	go runner.Run()
	defer runner.Stop()

	sm := http.NewServeMux()
	sm.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if runner.Healthy() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	log.Logger.Error(
		"Listen and Serve",
		"err", http.ListenAndServe(":8080", sm),
	)
}
