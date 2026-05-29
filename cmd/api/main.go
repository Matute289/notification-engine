// notification-api: composition root for the API service.
//
// All cross-package wiring lives here. Other packages — domain, service,
// infrastructure/* — never import from cmd/, so this is the only place a concrete
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

	httpapi "github.com/example/notification-engine/cmd/api/http"
	"github.com/example/notification-engine/cmd/api/http/handlers"
	mongoinfra "github.com/example/notification-engine/infrastructure/mongodb"
	"github.com/example/notification-engine/infrastructure/postgres"
	"github.com/example/notification-engine/infrastructure/rabbitmq"
	"github.com/example/notification-engine/infrastructure/rendering"
	redisinfra "github.com/example/notification-engine/infrastructure/redis"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/platform/auth"
	"github.com/example/notification-engine/internal/platform/config"
	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/service"
	"github.com/example/notification-engine/observability/logger"
	"github.com/example/notification-engine/observability/metrics"
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
	log := logger.NewLogger(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// --- outbound adapters ---
	pool, err := postgres.Connect(ctx, cfg.PostgresDSN)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()

	rdb, err := redisinfra.Connect(ctx, cfg.RedisAddr, cfg.RedisDB)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer rdb.Close()

	mongoClient, mongoDB, err := mongoinfra.Connect(ctx, cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		return fmt.Errorf("mongodb: %w", err)
	}
	defer func() { _ = mongoClient.Disconnect(context.Background()) }()

	mq, err := rabbitmq.Dial(cfg.RabbitMQURL, log)
	if err != nil {
		return fmt.Errorf("rabbitmq: %w", err)
	}
	defer mq.Close()
	if err := mq.Setup(domain.AllChannels()); err != nil {
		return fmt.Errorf("rabbitmq setup: %w", err)
	}

	notificationsRepo := postgres.NewNotificationRepository(pool)
	usersRepo := postgres.NewUserRepository(pool)
	mongoTemplatesRepo, err := mongoinfra.NewTemplateRepository(mongoDB)
	if err != nil {
		return fmt.Errorf("mongodb templates: %w", err)
	}
	// Shared circuit breaker: when Redis is unhealthy all three Redis-backed
	// components (rate limiter, deduper, template cache) fail open together.
	redisCB := redisinfra.NewCircuitBreaker(0, 0) // defaults: threshold=5, openTimeout=30s

	// Redis L2 cache in front of MongoDB; the renderer adds an in-process L1.
	templatesRepo := redisinfra.NewTemplateCache(mongoTemplatesRepo, rdb, cfg.TemplateCacheTTL, redisCB)
	publisher := rabbitmq.NewPublisher(mq)
	limiter := redisinfra.NewRateLimiter(rdb, redisCB)
	deduper := redisinfra.NewDeduper(rdb, redisCB)
	renderer := rendering.New(templatesRepo)
	m := metrics.NewPrometheusMetrics()

	// --- services ---
	clock := port.RealClock{}
	submit := &service.SubmitNotification{
		Notifications: notificationsRepo,
		Users:         usersRepo,
		Templates:     templatesRepo,
		Renderer:      renderer,
		Publisher:     publisher,
		Limiter:       limiter,
		Deduper:       deduper,
		Metrics:       m,
		Clock:         clock,
		Log:           log,
		Cfg: service.SubmitNotificationConfig{
			DedupeTTL:       cfg.DedupeTTL,
			RateLimits:      cfg.RateLimit.AsMap(),
			RateLimitWindow: cfg.RateLimitWindow,
		},
	}
	get := &service.GetNotification{Notifications: notificationsRepo}
	createTemplate := &service.CreateTemplate{Templates: templatesRepo, Clock: clock}
	getTemplate := &service.GetTemplate{Templates: templatesRepo}
	updateTemplate := &service.UpdateTemplate{Templates: templatesRepo, Clock: clock}
	deleteTemplate := &service.DeleteTemplate{Templates: templatesRepo}
	listTemplates := &service.ListTemplates{Templates: templatesRepo}
	updateSetting := &service.UpdateSetting{Users: usersRepo, Clock: clock}
	registerDevice := &service.RegisterDevice{Users: usersRepo, Clock: clock}
	deleteDevice := &service.DeleteDevice{Users: usersRepo}

	// --- auth ---
	clerkVerifier, err := auth.NewClerkVerifier(ctx, cfg.ClerkIssuer, cfg.ClerkAuthorizedParties)
	if err != nil {
		return fmt.Errorf("clerk: %w", err)
	}
	if clerkVerifier != nil {
		log.Info("clerk auth enabled", "issuer", cfg.ClerkIssuer)
	}
	var hmacVerifier *auth.Verifier
	if len(cfg.AppClients) > 0 {
		hmacVerifier = auth.NewVerifier(cfg.AppClients, cfg.HMACSkew)
		log.Info("hmac auth enabled", "clients", len(cfg.AppClients))
	}

	// --- inbound adapter ---
	h := &handlers.Handler{
		SubmitSvc:         submit,
		GetSvc:            get,
		CreateTemplateSvc: createTemplate,
		GetTemplateSvc:    getTemplate,
		UpdateTemplateSvc: updateTemplate,
		DeleteTemplateSvc: deleteTemplate,
		ListTemplatesSvc:  listTemplates,
		UpdateSettingSvc:  updateSetting,
		RegisterDeviceSvc: registerDevice,
		DeleteDeviceSvc:   deleteDevice,
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(h, clerkVerifier, hmacVerifier, limiter, log, httpapi.RouterConfig{
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
