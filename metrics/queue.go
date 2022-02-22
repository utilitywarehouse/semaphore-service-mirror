package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/util/workqueue"
)

var (
	queueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "semaphore_service_mirror_queue_depth",
			Help: "Workqueue depth, by queue name",
		},
		[]string{"name"},
	)
	queueAdds = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semaphore_service_mirror_queue_adds_total",
			Help: "Workqueue adds, by queue name",
		},
		[]string{"name"},
	)
	queueLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "semaphore_service_mirror_queue_latency_duration_seconds",
			Help: "Workqueue latency, by queue name",
		},
		[]string{"name"},
	)
	queueWorkDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "semaphore_service_mirror_queue_work_duration_seconds",
			Help: "Workqueue work duration, by queue name",
		},
		[]string{"name"},
	)
	queueUnfinishedWork = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "semaphore_service_mirror_queue_unfinished_work_seconds",
			Help: "Unfinished work in seconds, by queue name",
		},
		[]string{"name"},
	)
	queueLongestRunningProcessor = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "semaphore_service_mirror_queue_longest_running_processor_seconds",
			Help: "Longest running processor, by queue name",
		},
		[]string{"name"},
	)
	queueRetries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semaphore_service_mirror_queue_retries_total",
			Help: "Workqueue retries, by queue name",
		},
		[]string{"name"},
	)
	queueRequeued = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "semaphore_service_mirror_queue_requeued_items",
			Help: "Items that have been requeued but not reconciled yet, by queue name",
		},
		[]string{"name"},
	)
)

func init() {
	prometheus.MustRegister(
		queueDepth,
		queueAdds,
		queueLatency,
		queueWorkDuration,
		queueUnfinishedWork,
		queueLongestRunningProcessor,
		queueRetries,
		queueRequeued,
	)
	workqueue.SetProvider(&workqueueProvider{})
}

// SetRequeued updates the number of requeued items
func SetRequeued(name string, val float64) {
	queueRequeued.With(prometheus.Labels{"name": name}).Set(val)
}

// workqueueProvider implements workqueue.MetricsProvider
type workqueueProvider struct{}

// NewDepthMetric returns a gauge which tracks the depth of a queue
func (p *workqueueProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	return queueDepth.With(prometheus.Labels{"name": name})
}

// NewAddsMetrics returns a counter which tracks the adds to a queue
func (p *workqueueProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	return queueAdds.With(prometheus.Labels{"name": name})
}

// NewLatencyMetric returns a histogram which tracks the latency of a queue
func (p *workqueueProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	return queueLatency.With(prometheus.Labels{"name": name})
}

// NewWorkDurationMetric returns a histogram which tracks the time a queue
// spends working
func (p *workqueueProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	return queueWorkDuration.With(prometheus.Labels{"name": name})
}

// NewUnfinishedWorkSecondsMetric returns a gauge which tracks the time spent
// doing work that hasn't finished yet for a queue
func (p *workqueueProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return queueUnfinishedWork.With(prometheus.Labels{"name": name})
}

// NewLongestRunningProcessorSecondsMetric returns a gauge which tracks the
// duration of the longest running processor for a queue
func (p *workqueueProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return queueLongestRunningProcessor.With(prometheus.Labels{"name": name})
}

// NewRetriesMetric returns a counter which tracks the number of retries for a
// queue
func (p *workqueueProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	return queueRetries.With(prometheus.Labels{"name": name})
}
