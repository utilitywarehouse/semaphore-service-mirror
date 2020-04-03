package main

import (
	"flag"
	"os"
	"sync"
	"time"

	"github.com/utilitywarehouse/kube-service-mirror/kube"
	"github.com/utilitywarehouse/kube-service-mirror/log"
)

var (
	flagKubeConfigPath       = flag.String("kube-config", "", "Path of a kube config file, if not provided the app will try to get in cluster config")
	flagTargetKubeConfigPath = flag.String("target-kube-config", "", "(required) Path of the target cluster kube config file to mirrot services from")
	flagLogLevel             = flag.String("log-level", "info", "log level, defaults to info")
	flagResyncPeriod         = flag.Duration("resync-period", 60*time.Minute, "Namespace watcher cache resync period")
	flagMirrorNamespace      = flag.String("mirror-ns", "", "the namespace to create dummy mirror services in")
	flagSvcPrefix            = flag.String("svc-prefix", "", "a prefix to apply on all mirrored services names")
)

func usage() {
	flag.Usage()
	os.Exit(1)
}

func main() {

	flag.Parse()

	if *flagTargetKubeConfigPath == "" {
		usage()
	}

	log.InitLogger("kube-service-mirror", *flagLogLevel)

	/// Get a kube client to use with the watchers
	kubeClient, err := kube.GetClient(*flagKubeConfigPath)
	if err != nil {
		log.Logger.Error(
			"cannot create kube client for homecluster",
			"err", err,
		)
		usage()
	}

	watchClient, err := kube.GetClient(*flagTargetKubeConfigPath)
	if err != nil {
		log.Logger.Error(
			"cannot create kube client for homecluster",
			"err", err,
		)
		usage()
	}

	runner := NewRunner(
		kubeClient,
		watchClient,
		*flagMirrorNamespace,
		*flagSvcPrefix,
		// Resync will trigger an onUpdate event for everything that is
		// stored in cache.
		*flagResyncPeriod,
	)
	runner.Run()

	// Hold forever
	wg := sync.WaitGroup{}
	wg.Add(1)
	wg.Wait()
}
