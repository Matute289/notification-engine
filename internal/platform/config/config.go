// Package config loads runtime settings from environment variables. Keeping
// this in platform/ (not domain or app) makes it explicit that configuration
// is infrastructure concern, not business policy.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/example/notification-engine/internal/domain"
)

// Config aggregates every runtime setting. Values are sourced from environment
// variables; see .env.example for the canonical list.
type Config struct {
	LogLevel     string        `env:"LOG_LEVEL" envDefault:"info"`
	HTTPAddr     string        `env:"HTTP_ADDR" envDefault:":8080"`
	PostgresDSN  string        `env:"POSTGRES_DSN,required"`
	RedisAddr    string        `env:"REDIS_ADDR,required"`
	RedisDB      int           `env:"REDIS_DB" envDefault:"0"`
	RabbitMQURL  string        `env:"RABBITMQ_URL,required"`
	ProviderMode string        `env:"PROVIDER_MODE" envDefault:"mock"`
	MaxRetries   int           `env:"MAX_RETRIES" envDefault:"5"`
	HMACSkew     time.Duration `env:"HMAC_SKEW" envDefault:"5m"`
	DedupeTTL    time.Duration `env:"DEDUPE_TTL" envDefault:"24h"`
	RateLimitWindow time.Duration `env:"RATELIMIT_WINDOW" envDefault:"1h"`

	// Per-app-key global API QPS cap. 0 disables.
	AppKeyRateLimit  int           `env:"APP_KEY_RATE_LIMIT" envDefault:"0"`
	AppKeyRateWindow time.Duration `env:"APP_KEY_RATE_WINDOW" envDefault:"1s"`

	// Janitor settings (used only by cmd/janitor).
	JanitorInterval       time.Duration `env:"JANITOR_INTERVAL" envDefault:"30s"`
	JanitorStuckThreshold time.Duration `env:"JANITOR_STUCK_THRESHOLD" envDefault:"5m"`
	JanitorBatchSize      int           `env:"JANITOR_BATCH_SIZE" envDefault:"100"`

	// Outbox relay (used only by cmd/outbox-relay).
	RelayInterval  time.Duration `env:"RELAY_INTERVAL" envDefault:"500ms"`
	RelayBatchSize int           `env:"RELAY_BATCH_SIZE" envDefault:"100"`

	// Real provider credentials (used when PROVIDER_MODE=real).
	APNSBundleID   string `env:"APNS_BUNDLE_ID"`
	APNSKeyID      string `env:"APNS_KEY_ID"`
	APNSTeamID     string `env:"APNS_TEAM_ID"`
	APNSAuthKey    string `env:"APNS_AUTH_KEY"`
	APNSEndpoint   string `env:"APNS_ENDPOINT"`
	FCMProjectID   string `env:"FCM_PROJECT_ID"`
	FCMCredentials string `env:"FCM_CREDENTIALS_JSON"` // path to a service-account JSON file
	TwilioAccountSID string `env:"TWILIO_ACCOUNT_SID"`
	TwilioAuthToken  string `env:"TWILIO_AUTH_TOKEN"`
	TwilioFromNumber string `env:"TWILIO_FROM_NUMBER"`
	SendGridAPIKey   string `env:"SENDGRID_API_KEY"`
	SendGridFromEmail string `env:"SENDGRID_FROM_EMAIL"`
	SendGridFromName  string `env:"SENDGRID_FROM_NAME"`

	RateLimit RateLimit
	AppClients map[string]string `env:"APP_CLIENTS,required" envSeparator:","`

	WorkerChannel     string `env:"WORKER_CHANNEL" envDefault:"push_ios"`
	WorkerConcurrency int    `env:"WORKER_CONCURRENCY" envDefault:"8"`
}

// RateLimit holds the per-channel hourly cap. Conversion to a typed map keeps
// downstream code free of channel-string juggling.
type RateLimit struct {
	PushPerHour  int `env:"RATELIMIT_PUSH_PER_HOUR" envDefault:"20"`
	SMSPerHour   int `env:"RATELIMIT_SMS_PER_HOUR" envDefault:"5"`
	EmailPerHour int `env:"RATELIMIT_EMAIL_PER_HOUR" envDefault:"10"`
}

// AsMap returns the per-channel limits keyed by the typed Channel.
func (r RateLimit) AsMap() map[domain.Channel]int {
	return map[domain.Channel]int{
		domain.ChannelPushIOS:     r.PushPerHour,
		domain.ChannelPushAndroid: r.PushPerHour,
		domain.ChannelSMS:         r.SMSPerHour,
		domain.ChannelEmail:       r.EmailPerHour,
	}
}

// Load reads environment variables into Config and returns an error if any
// required field is missing or malformed.
func Load() (Config, error) {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	return cfg, validate(cfg)
}

func validate(c Config) error {
	if len(c.AppClients) == 0 {
		return fmt.Errorf("config: APP_CLIENTS must define at least one key:secret pair")
	}
	switch strings.ToLower(c.ProviderMode) {
	case "mock", "real":
	default:
		return fmt.Errorf("config: PROVIDER_MODE must be 'mock' or 'real', got %q", c.ProviderMode)
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("config: MAX_RETRIES must be >= 0")
	}
	return nil
}
