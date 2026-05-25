// Package middleware contains the chi-compatible middleware used by the API:
// request id, panic recovery, structured access logs, HMAC auth, and metrics.
package middleware

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/example/notification-engine/internal/platform/auth"
	"github.com/example/notification-engine/internal/port"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

type ctxKey string

const (
	ctxRequestID ctxKey = "request_id"
	ctxAppKey    ctxKey = "app_key"
	ctxIdentity  ctxKey = "identity"
)

// Identity represents the authenticated caller, regardless of mechanism.
type Identity struct {
	// Subject is the Clerk user ID (for bearer tokens) or the HMAC app key (for services).
	Subject string
	// Kind is "user" (Clerk JWT) or "service" (HMAC).
	Kind string
}

// IdentityFromContext retrieves the authenticated Identity from the request context.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(ctxIdentity).(Identity)
	return v, ok
}

func withIdentity(ctx context.Context, id Identity) context.Context {
	ctx = context.WithValue(ctx, ctxIdentity, id)
	// Populate ctxAppKey with Subject so AppKeyRateLimit and AccessLog work
	// without modification regardless of auth mechanism.
	return context.WithValue(ctx, ctxAppKey, id.Subject)
}

// RequestID assigns a UUID to every request and stamps it on the response.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxRequestID).(string)
	return v
}
func AppKeyFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxAppKey).(string)
	return v
}

// Recoverer logs the panic with stack and returns 500.
func Recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						"request_id", RequestIDFromContext(r.Context()),
						"panic", rec,
						"stack", string(debug.Stack()))
					http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// AccessLog emits one structured line per request after the response is sent.
// httpHist is an optional histogram; nil disables HTTP metrics.
func AccessLog(log *slog.Logger, httpHist *prometheus.HistogramVec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			dur := time.Since(start)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = r.URL.Path
			}
			status := strconv.Itoa(ww.Status())
			if httpHist != nil {
				httpHist.WithLabelValues(r.Method, route, status).Observe(dur.Seconds())
			}
			log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", dur.Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
				"app_key", AppKeyFromContext(r.Context()))
		})
	}
}

// Authenticate verifies every request using whichever credential is present:
//   - Authorization: Bearer <jwt>  →  Clerk JWT verification (when clerk != nil)
//   - X-App-Key + HMAC headers     →  HMAC-SHA256 verification (when hmacVer != nil)
//
// Passing nil for either disables that mechanism. At least one must be non-nil.
// Unauthenticated routes (e.g. /healthz, /metrics) must be mounted outside the
// protected sub-router.
func Authenticate(clerk *auth.ClerkVerifier, hmacVer *auth.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Clerk Bearer path — no body needed.
			if clerk != nil {
				if bearer := extractBearer(r); bearer != "" {
					claims, err := clerk.Verify(r.Context(), bearer)
					if err != nil {
						writeErr(w, http.StatusUnauthorized, "unauthorized")
						return
					}
					ctx := withIdentity(r.Context(), Identity{Subject: claims.Subject, Kind: "user"})
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// HMAC path — reads and replaces body so downstream handlers see it.
			if hmacVer != nil {
				body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
				if err != nil {
					writeErr(w, http.StatusBadRequest, "invalid body")
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))

				key, err := hmacVer.Verify(
					r.Header.Get(auth.HeaderAppKey),
					r.Header.Get(auth.HeaderTimestamp),
					r.Header.Get(auth.HeaderSignature),
					r.Method, r.URL.Path, body,
				)
				if err != nil {
					writeErr(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				ctx := withIdentity(r.Context(), Identity{Subject: key, Kind: "service"})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			writeErr(w, http.StatusUnauthorized, "unauthorized")
		})
	}
}

// extractBearer returns the token from an "Authorization: Bearer <token>" header,
// or empty string if absent or malformed.
func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":"`+msg+`"}`)
}

// AppKeyRateLimit caps the global request rate for one (authenticated) app
// key. It expects to run after HMACAuth so the AppKey is in the context.
// Configuration: limit per window. limit<=0 disables the middleware.
func AppKeyRateLimit(rl port.RateLimiter, limit int, window time.Duration) func(http.Handler) http.Handler {
	if limit <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := AppKeyFromContext(r.Context())
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			ok, err := rl.Allow(r.Context(), fmt.Sprintf("notif:rl:appkey:%s", key), limit, window)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "ratelimit_error")
				return
			}
			if !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
				writeErr(w, http.StatusTooManyRequests, "app_rate_limited")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
