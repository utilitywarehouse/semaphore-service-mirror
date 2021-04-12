package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/utilitywarehouse/semaphore-service-mirror/kube"
	"github.com/utilitywarehouse/semaphore-service-mirror/log"
	"k8s.io/client-go/kubernetes"
)

var (
	flagKubeConfigPath       = flag.String("kube-config", getEnv("SSM_KUBE_CONFIG", ""), "Path of a kube config file, if not provided the app will try to get in cluster config")
	flagTargetKubeConfigPath = flag.String("target-kube-config", getEnv("SSM_TARGET_KUBE_CONFIG", ""), "Path of the target cluster kube config file to mirrot services from")
	flagLogLevel             = flag.String("log-level", getEnv("SSM_LOG_LEVEL", "info"), "Log level")
	flagResyncPeriod         = flag.Duration("resync-period", 60*time.Minute, "Namespace watcher cache resync period")
	flagMirrorNamespace      = flag.String("mirror-ns", getEnv("SSM_MIRROR_NS", ""), "The namespace to create dummy mirror services in")
	flagSvcPrefix            = flag.String("svc-prefix", getEnv("SSM_SVC_PREFIX", ""), "(required) A prefix to apply on all mirrored services names. Will also be used for initial service sync")
	flagLabelSelector        = flag.String("label-selector", getEnv("SSM_LABEL_SELECTOR", ""), "(required) Label of services and endpoints to watch and mirror")
	flagSvcSync              = flag.Bool("svc-sync", true, "Sync services on startup")
	flagRemoteAPIURL         = flag.String("remote-api-url", getEnv("SSM_REMOTE_API_URL", ""), "Remote Kubernetes API server URL")
	flagRemoteCAURL          = flag.String("remote-ca-url", getEnv("SSM_REMOTE_CA_URL", ""), "Remote Kubernetes CA certificate URL")
	flagRemoteSATokenPath    = flag.String("remote-sa-token-path", "", "Remote Kubernetes cluster token path")

	saToken = os.Getenv("SSM_REMOTE_SERVICE_ACCOUNT_TOKEN")

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
	if os.Getenv("SSM_RESYNC_PERIOD") != "" {
		*flagResyncPeriod, err = time.ParseDuration(os.Getenv("SSM_RESYNC_PERIOD"))
		if err != nil {
			fmt.Printf("SSM_RESYNC_PERIOD must be a duration (https://golang.org/pkg/time/#Duration)")
			os.Exit(1)
		}
	}
	if os.Getenv("SSM_SVC_SYNC") != "" {
		*flagSvcSync, err = strconv.ParseBool(os.Getenv("SSM_SVC_SYNC"))
		if err != nil {
			fmt.Printf("SSM_SVC_SYNC must be a boolean: 'true' | 'false'")
			os.Exit(1)
		}
	}

	flag.Parse()

	if *flagLabelSelector == "" {
		usage()
	}

	if *flagSvcPrefix == "" {
		usage()
	}

	if *flagRemoteSATokenPath != "" {
		data, err := ioutil.ReadFile(*flagRemoteSATokenPath)
		if err != nil {
			fmt.Printf("Cannot read file: %s", *flagRemoteSATokenPath)
			os.Exit(1)
		}
		saToken = string(data)
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

	log.InitLogger("semaphore-service-mirror", *flagLogLevel)

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
		remoteClient, err = kube.Client(saToken, *flagRemoteAPIURL, *flagRemoteCAURL)
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
