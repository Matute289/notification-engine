// Package observability is the Prometheus-backed implementation of
// port.MetricsRecorder. Tests in app/usecase use a no-op fake so the
// application layer never imports prometheus types.
package observability

import (
	"github.com/example/notification-engine/internal/app/port"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type PrometheusMetrics struct {
	accepted      *prometheus.CounterVec
	sent          *prometheus.CounterVec
	failed        *prometheus.CounterVec
	deadLettered  *prometheus.CounterVec
	workerLatency *prometheus.HistogramVec
}

func NewPrometheusMetrics() *PrometheusMetrics {
	return &PrometheusMetrics{
		accepted: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "notifications_accepted_total",
			Help: "Notifications accepted by the API and enqueued.",
		}, []string{"channel"}),
		sent: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "notifications_sent_total",
			Help: "Notifications successfully delivered to a third-party provider.",
		}, []string{"channel"}),
		failed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "notifications_failed_total",
			Help: "Notification send attempts that failed (eligible for retry).",
		}, []string{"channel"}),
		deadLettered: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "notifications_dead_letter_total",
			Help: "Notifications that exhausted retry attempts and were dead-lettered.",
		}, []string{"channel"}),
		workerLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "worker_process_duration_seconds",
			Help:    "Time spent by a worker handling a single notification.",
			Buckets: prometheus.DefBuckets,
		}, []string{"channel", "outcome"}),
	}
}

var _ port.MetricsRecorder = (*PrometheusMetrics)(nil)

func (m *PrometheusMetrics) NotificationAccepted(channel string) {
	m.accepted.WithLabelValues(channel).Inc()
}
func (m *PrometheusMetrics) NotificationSent(channel string) {
	m.sent.WithLabelValues(channel).Inc()
}
func (m *PrometheusMetrics) NotificationFailed(channel string) {
	m.failed.WithLabelValues(channel).Inc()
}
func (m *PrometheusMetrics) NotificationDeadLettered(channel string) {
	m.deadLettered.WithLabelValues(channel).Inc()
}
func (m *PrometheusMetrics) ObserveWorkerDuration(channel, outcome string, seconds float64) {
	m.workerLatency.WithLabelValues(channel, outcome).Observe(seconds)
}
