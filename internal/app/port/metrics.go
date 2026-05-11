package port

// MetricsRecorder is the abstraction the application uses to emit metrics.
// Adapters can satisfy it with Prometheus, statsd, OpenTelemetry, or a no-op
// implementation in tests. Keeping the surface narrow (only the events the
// app actually emits) prevents over-coupling to any one library.
type MetricsRecorder interface {
	NotificationAccepted(channel string)
	NotificationSent(channel string)
	NotificationFailed(channel string)
	NotificationDeadLettered(channel string)
	ObserveWorkerDuration(channel, outcome string, seconds float64)
}
