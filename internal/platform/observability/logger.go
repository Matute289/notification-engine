// Package observability holds cross-cutting infrastructure: the structured
// logger and (eventually) tracing init. The Prometheus MetricsRecorder
// implementation lives in adapter/outbound/observability so it can satisfy
// the application port.
package observability

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger returns a JSON slog.Logger configured at the requested level.
// Unrecognised levels fall back to info.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
