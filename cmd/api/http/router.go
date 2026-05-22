package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/notification-engine/cmd/api/http/handlers"
	mw "github.com/example/notification-engine/middleware"
	"github.com/example/notification-engine/internal/platform/auth"
	"github.com/example/notification-engine/internal/port"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RouterConfig groups optional knobs that the composition root passes in.
// AppKeyRateLimit and AppKeyRateWindow gate per-app-key global QPS.
type RouterConfig struct {
	AppKeyRateLimit  int
	AppKeyRateWindow time.Duration
}

// NewRouter wires middleware and the handler onto a chi router. Health and
// metrics live outside the HMAC-protected sub-router so probes don't sign
// their requests.
func NewRouter(h *handlers.Handler, verifier *auth.Verifier, limiter port.RateLimiter, log *slog.Logger, cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	httpHist := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Latency of HTTP requests handled by the API server.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	r.Use(mw.RequestID)
	r.Use(mw.Recoverer(log))
	r.Use(mw.AccessLog(log, httpHist))

	r.Get("/healthz", health)
	r.Get("/readyz", health)
	r.Handle("/metrics", promhttp.Handler())

	r.Group(func(r chi.Router) {
		r.Use(mw.HMACAuth(verifier))
		r.Use(mw.AppKeyRateLimit(limiter, cfg.AppKeyRateLimit, cfg.AppKeyRateWindow))
		r.Route("/v1", func(r chi.Router) {
			r.Post("/notifications", h.SubmitNotification)
			r.Get("/notifications/{id}", h.GetNotification)
			r.Post("/templates", h.CreateTemplate)
			r.Get("/templates/{id}", h.GetTemplate)
			r.Put("/users/{id}/settings", h.UpdateSetting)
			r.Post("/users/{id}/devices", h.RegisterDevice)
		})
	})
	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
