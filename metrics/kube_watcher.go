package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/watch"
)

var (
	kubeWatcherObjects = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "semaphore_service_mirror_kube_watcher_objects",
		Help: "Number of objects watched, by watcher and kind",
	},
		[]string{"watcher", "kind", "runner"},
	)
	kubeWatcherEvents = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "semaphore_service_mirror_kube_watcher_events_total",
		Help: "Number of events handled, by watcher, kind and event_type",
	},
		[]string{"watcher", "kind", "event_type", "runner"},
	)
)

func init() {
	prometheus.MustRegister(
		kubeWatcherObjects,
		kubeWatcherEvents,
	)
}

func IncKubeWatcherEvents(watcher, kind, runner string, eventType watch.EventType) {
	kubeWatcherEvents.With(prometheus.Labels{
		"watcher":    watcher,
		"kind":       kind,
		"event_type": string(eventType),
		"runner":     runner,
	}).Inc()
}

func SetKubeWatcherObjects(watcher, kind, runner string, v float64) {
	kubeWatcherObjects.With(prometheus.Labels{
		"watcher": watcher,
		"kind":    kind,
		"runner":  runner,
	}).Set(v)
}
