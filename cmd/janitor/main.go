// notification-janitor: composition root for the background janitor that
// rescues notifications stuck in the in_flight state (e.g. because the worker
// crashed mid-send and the broker isn't redelivering).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/notification-engine/internal/adapter/outbound/postgres"
	"github.com/example/notification-engine/internal/adapter/outbound/rabbitmq"
	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/app/usecase"
	"github.com/example/notification-engine/internal/domain"
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

	pool, err := postgres.Connect(ctx, cfg.PostgresDSN)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()

	mq, err := rabbitmq.Dial(cfg.RabbitMQURL, log)
	if err != nil {
		return fmt.Errorf("rabbitmq: %w", err)
	}
	defer mq.Close()
	if err := mq.Setup(domain.AllChannels()); err != nil {
		return fmt.Errorf("rabbitmq setup: %w", err)
	}

	uc := &usecase.RescueStuckNotifications{
		Notifications: postgres.NewNotificationRepository(pool),
		Publisher:     rabbitmq.NewPublisher(mq),
		Clock:         port.RealClock{},
		Log:           log,
		Cfg: usecase.RescueStuckConfig{
			StuckThreshold: cfg.JanitorStuckThreshold,
			BatchSize:      cfg.JanitorBatchSize,
		},
	}

	log.Info("janitor starting",
		"interval", cfg.JanitorInterval,
		"threshold", cfg.JanitorStuckThreshold,
		"batch_size", cfg.JanitorBatchSize)

	tick := time.NewTicker(cfg.JanitorInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("janitor stopping")
			return nil
		case <-tick.C:
			res, err := uc.Execute(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Error("janitor pass failed", "err", err)
				continue
			}
			if res.Examined > 0 {
				log.Info("janitor pass",
					"examined", res.Examined, "rescued", res.Rescued, "errors", res.Errors)
			}
		}
	}
}
