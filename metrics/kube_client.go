package metrics

import (
	"context"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/metrics"
)

var (
	kubeClientRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "semaphore_service_mirror_kube_http_request_total",
		Help: "Total number of HTTP requests to the Kubernetes API by host, code and method",
	},
		[]string{"host", "code", "method"},
	)
	kubeClientRequestsDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "semaphore_service_mirror_kube_http_request_duration_seconds",
		Help: "Histogram of latencies for HTTP requests to the Kubernetes API by host and method",
	},
		[]string{"host", "method"},
	)
)

func init() {
	(&kubeClientRequestAdapter{}).Register()
}

// kubeClientRequestAdapter implements metrics interfaces provided by client-go
type kubeClientRequestAdapter struct{}

// Register registers the adapter
func (a *kubeClientRequestAdapter) Register() {
	metrics.Register(
		metrics.RegisterOpts{
			RequestLatency: a,
			RequestResult:  a,
		},
	)
	prometheus.MustRegister(
		kubeClientRequests,
		kubeClientRequestsDuration,
	)

}

// Increment implements metrics.ResultMetric
func (a kubeClientRequestAdapter) Increment(ctx context.Context, code string, method string, host string) {
	kubeClientRequests.With(prometheus.Labels{
		"code":   code,
		"method": method,
		"host":   host,
	}).Inc()
}

// Observe implements metrics.LatencyMetric
func (a kubeClientRequestAdapter) Observe(ctx context.Context, method string, u url.URL, latency time.Duration) {
	kubeClientRequestsDuration.With(prometheus.Labels{
		"host":   u.Host,
		"method": method,
	}).Observe(latency.Seconds())
}
