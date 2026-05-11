// notification-outbox-relay: drains pending notification_outbox rows and
// publishes them to RabbitMQ. Multiple replicas can run safely — Claim uses
// SELECT ... FOR UPDATE SKIP LOCKED so the work partitions itself.
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

	repo := postgres.NewNotificationRepository(pool)
	uc := &usecase.RelayOutbox{
		Outbox:    repo,
		Publisher: rabbitmq.NewPublisher(mq),
		Log:       log,
		Cfg:       usecase.RelayConfig{BatchSize: cfg.RelayBatchSize},
	}

	log.Info("outbox-relay starting",
		"interval", cfg.RelayInterval, "batch_size", cfg.RelayBatchSize)

	tick := time.NewTicker(cfg.RelayInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("outbox-relay stopping")
			return nil
		case <-tick.C:
			res, err := uc.Execute(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Error("relay pass failed", "err", err)
				continue
			}
			if res.Examined > 0 {
				log.Info("relay pass",
					"examined", res.Examined, "published", res.Published, "failed", res.Failed)
			}
		}
	}
}
