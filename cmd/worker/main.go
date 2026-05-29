// notification-worker: composition root for one channel's worker process.
//
// Selecting WORKER_CHANNEL=push_ios|push_android|sms|email at startup decides
// which queue this binary drains and which provider it talks to.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/notification-engine/cmd/worker/consumer"
	"github.com/example/notification-engine/infrastructure/postgres"
	"github.com/example/notification-engine/infrastructure/provider/apns"
	"github.com/example/notification-engine/infrastructure/provider/fbmessenger"
	"github.com/example/notification-engine/infrastructure/provider/fcm"
	"github.com/example/notification-engine/infrastructure/provider/line"
	"github.com/example/notification-engine/infrastructure/provider/mock"
	"github.com/example/notification-engine/infrastructure/provider/sendgrid"
	"github.com/example/notification-engine/infrastructure/provider/telegram"
	"github.com/example/notification-engine/infrastructure/provider/twilio"
	"github.com/example/notification-engine/infrastructure/provider/whatsapp"
	"github.com/example/notification-engine/infrastructure/rabbitmq"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/platform/config"
	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/service"
	"github.com/example/notification-engine/observability/logger"
	"github.com/example/notification-engine/observability/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const adminAddr = ":9090"

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

	channel, err := domain.ParseChannel(cfg.WorkerChannel)
	if err != nil {
		return fmt.Errorf("invalid WORKER_CHANNEL: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// --- outbound adapters ---
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
	if err := mq.Setup([]domain.Channel{channel}); err != nil {
		return fmt.Errorf("rabbitmq setup: %w", err)
	}

	notificationsRepo := postgres.NewNotificationRepository(pool)
	publisher := rabbitmq.NewPublisher(mq)
	m := metrics.NewPrometheusMetrics()

	prv, err := buildProvider(cfg, log)
	if err != nil {
		return err
	}

	// --- service ---
	process := &service.ProcessNotification{
		Notifications: notificationsRepo,
		Provider:      prv,
		Publisher:     publisher,
		Metrics:       m,
		Clock:         port.RealClock{},
		Log:           log,
		Cfg:           service.ProcessNotificationConfig{MaxRetries: cfg.MaxRetries},
	}

	// --- inbound adapter ---
	c := &consumer.Consumer{
		Channel:     channel,
		Concurrency: cfg.WorkerConcurrency,
		Conn:        mq,
		UseCase:     process,
		Log:         log,
	}

	admin := startAdminServer(log, channel)
	defer func() {
		shutdown, sc := context.WithTimeout(context.Background(), 5*time.Second)
		defer sc()
		_ = admin.Shutdown(shutdown)
	}()

	log.Info("worker starting",
		"channel", channel,
		"concurrency", cfg.WorkerConcurrency,
		"admin_addr", adminAddr,
	)
	return c.Run(ctx)
}

func buildProvider(cfg config.Config, log *slog.Logger) (port.NotificationProvider, error) {
	if cfg.ProviderMode == "mock" {
		return mock.New(log, 0), nil
	}
	if cfg.ProviderMode != "real" {
		return nil, fmt.Errorf("unsupported PROVIDER_MODE %q", cfg.ProviderMode)
	}
	switch domain.Channel(cfg.WorkerChannel) {
	case domain.ChannelPushIOS:
		signer, err := buildAPNSAuth(cfg)
		if err != nil {
			return nil, err
		}
		return apns.New(apns.Config{
			BundleID: cfg.APNSBundleID, BaseURL: cfg.APNSEndpoint, Auth: signer,
		})
	case domain.ChannelPushAndroid:
		ts, err := buildFCMTokenSource(cfg)
		if err != nil {
			return nil, err
		}
		return fcm.New(fcm.Config{
			ProjectID: cfg.FCMProjectID, TokenSource: ts,
		})
	case domain.ChannelSMS:
		return twilio.New(twilio.Config{
			AccountSID: cfg.TwilioAccountSID,
			AuthToken:  cfg.TwilioAuthToken,
			FromNumber: cfg.TwilioFromNumber,
		})
	case domain.ChannelEmail:
		return sendgrid.New(sendgrid.Config{
			APIKey:    cfg.SendGridAPIKey,
			FromEmail: cfg.SendGridFromEmail,
			FromName:  cfg.SendGridFromName,
		})
	case domain.ChannelTelegram:
		if cfg.TelegramBotToken == "" {
			return nil, fmt.Errorf("telegram: TELEGRAM_BOT_TOKEN required for PROVIDER_MODE=real")
		}
		return telegram.New(telegram.Config{BotToken: cfg.TelegramBotToken})
	case domain.ChannelWhatsApp:
		if cfg.WhatsAppPhoneNumberID == "" || cfg.WhatsAppAccessToken == "" {
			return nil, fmt.Errorf("whatsapp: WHATSAPP_PHONE_NUMBER_ID and WHATSAPP_ACCESS_TOKEN required for PROVIDER_MODE=real")
		}
		return whatsapp.New(whatsapp.Config{PhoneNumberID: cfg.WhatsAppPhoneNumberID, AccessToken: cfg.WhatsAppAccessToken})
	case domain.ChannelLine:
		if cfg.LineChannelAccessToken == "" {
			return nil, fmt.Errorf("line: LINE_CHANNEL_ACCESS_TOKEN required for PROVIDER_MODE=real")
		}
		return line.New(line.Config{ChannelAccessToken: cfg.LineChannelAccessToken})
	case domain.ChannelFacebookMessenger:
		if cfg.FBPageAccessToken == "" {
			return nil, fmt.Errorf("fbmessenger: FB_PAGE_ACCESS_TOKEN required for PROVIDER_MODE=real")
		}
		return fbmessenger.New(fbmessenger.Config{PageAccessToken: cfg.FBPageAccessToken})
	default:
		return nil, fmt.Errorf("no real provider for channel %q", cfg.WorkerChannel)
	}
}

// buildAPNSAuth constructs the JWT signer for APNs. Today this is a stub —
// it returns an Authenticator that would use cfg.APNS{KeyID,TeamID,AuthKey}
// to sign an ES256 JWT and refresh it every 20 minutes. Slot in a real
// implementation (e.g. github.com/golang-jwt/jwt/v5) here.
func buildAPNSAuth(cfg config.Config) (apns.Authenticator, error) {
	if cfg.APNSKeyID == "" || cfg.APNSTeamID == "" || cfg.APNSAuthKey == "" {
		return nil, errors.New("apns: APNS_KEY_ID/APNS_TEAM_ID/APNS_AUTH_KEY required for PROVIDER_MODE=real")
	}
	return stubAPNSAuth{}, nil
}

type stubAPNSAuth struct{}

func (stubAPNSAuth) Authorization(_ context.Context) (string, error) {
	return "", errors.New("apns: ES256 JWT signer not yet implemented; wire in a real signer")
}

// buildFCMTokenSource is the equivalent stub for FCM. A real implementation
// uses golang.org/x/oauth2/google.JWTConfigFromJSON to mint scoped tokens.
func buildFCMTokenSource(cfg config.Config) (fcm.TokenSource, error) {
	if cfg.FCMCredentials == "" {
		return nil, errors.New("fcm: FCM_CREDENTIALS_JSON required for PROVIDER_MODE=real")
	}
	return stubFCMTokenSource{}, nil
}

type stubFCMTokenSource struct{}

func (stubFCMTokenSource) Token(_ context.Context) (string, error) {
	return "", errors.New("fcm: oauth2 token source not yet implemented; wire in golang.org/x/oauth2/google")
}

func startAdminServer(log *slog.Logger, channel domain.Channel) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"channel": string(channel),
		})
	})
	srv := &http.Server{
		Addr:              adminAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("worker admin server error", "err", err)
		}
	}()
	return srv
}
