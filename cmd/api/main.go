// notification-api: composition root for the API service.
//
// All cross-package wiring lives here. Other packages — domain, app/usecase,
// adapter/* — never import from cmd/, so this is the only place a concrete
// adapter is bound to a port.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/notification-engine/internal/adapter/inbound/httpapi"
	obsadapter "github.com/example/notification-engine/internal/adapter/outbound/observability"
	"github.com/example/notification-engine/internal/adapter/outbound/postgres"
	"github.com/example/notification-engine/internal/adapter/outbound/rabbitmq"
	redisadapter "github.com/example/notification-engine/internal/adapter/outbound/redis"
	"github.com/example/notification-engine/internal/adapter/outbound/rendering"
	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/app/usecase"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/platform/auth"
	"github.com/example/notification-engine/internal/platform/config"
	"github.com/example/notification-engine/internal/platform/observability"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// --- outbound adapters ---
	pool, err := postgres.Connect(ctx, cfg.PostgresDSN)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()

	rdb, err := redisadapter.Connect(ctx, cfg.RedisAddr, cfg.RedisDB)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer rdb.Close()

	mq, err := rabbitmq.Dial(cfg.RabbitMQURL, log)
	if err != nil {
		return fmt.Errorf("rabbitmq: %w", err)
	}
	defer mq.Close()
	if err := mq.Setup(domain.AllChannels()); err != nil {
		return fmt.Errorf("rabbitmq setup: %w", err)
	}

	notificationsRepo := postgres.NewNotificationRepository(pool)
	templatesRepo := postgres.NewTemplateRepository(pool)
	usersRepo := postgres.NewUserRepository(pool)
	publisher := rabbitmq.NewPublisher(mq)
	limiter := redisadapter.NewRateLimiter(rdb)
	deduper := redisadapter.NewDeduper(rdb)
	renderer := rendering.New(templatesRepo)
	metrics := obsadapter.NewPrometheusMetrics()

	// --- use cases ---
	clock := port.RealClock{}
	submit := &usecase.SubmitNotification{
		Notifications: notificationsRepo,
		Users:         usersRepo,
		Templates:     templatesRepo,
		Renderer:      renderer,
		Publisher:     publisher,
		Limiter:       limiter,
		Deduper:       deduper,
		Metrics:       metrics,
		Clock:         clock,
		Log:           log,
		Cfg: usecase.SubmitNotificationConfig{
			DedupeTTL:       cfg.DedupeTTL,
			RateLimits:      cfg.RateLimit.AsMap(),
			RateLimitWindow: cfg.RateLimitWindow,
		},
	}
	get := &usecase.GetNotification{Notifications: notificationsRepo}
	createTemplate := &usecase.CreateTemplate{Templates: templatesRepo, Clock: clock}
	getTemplate := &usecase.GetTemplate{Templates: templatesRepo}
	updateSetting := &usecase.UpdateSetting{Users: usersRepo, Clock: clock}
	registerDevice := &usecase.RegisterDevice{Users: usersRepo, Clock: clock}

	// --- inbound adapter ---
	handler := &httpapi.Handler{
		Submit:         submit,
		Get:            get,
		CreateTemplate: createTemplate,
		GetTemplate:    getTemplate,
		UpdateSetting:  updateSetting,
		RegisterDevice: registerDevice,
	}
	verifier := auth.NewVerifier(cfg.AppClients, cfg.HMACSkew)

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(handler, verifier, limiter, log, httpapi.RouterConfig{
			AppKeyRateLimit:  cfg.AppKeyRateLimit,
			AppKeyRateWindow: cfg.AppKeyRateWindow,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down api")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	return srv.Shutdown(shutdownCtx)
}
