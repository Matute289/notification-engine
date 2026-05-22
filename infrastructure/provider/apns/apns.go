// Package apns implements port.NotificationProvider against Apple Push
// Notification service (APNs) over HTTP/2.
//
// This is a working skeleton: the request shape, headers, and error mapping
// match Apple's documentation. Authentication is by JWT (token-based,
// recommended over certificates) signed with ES256 over a key id + team id +
// the auth key downloaded from the developer console. The signing piece is
// stubbed via the Authenticator interface so callers can inject a real
// signer (or a fake in tests) without dragging an ES256 dependency into this
// package.
//
// References:
//   https://developer.apple.com/documentation/usernotifications/sending-notification-requests-to-apns
package apns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
)

// Endpoint is APNs production. Sandbox endpoint should be wired via Config.
const Endpoint = "https://api.push.apple.com"

// Authenticator builds the `Bearer <jwt>` value Apple expects in the
// authorization header. Production code wires this to an ES256 signer that
// rotates the JWT every <=20 minutes.
type Authenticator interface {
	Authorization(ctx context.Context) (string, error)
}

// Config holds the per-app values needed for the request.
type Config struct {
	BundleID string         // X-Apns-Topic
	BaseURL  string         // override for the sandbox endpoint
	Auth     Authenticator
	Client   *http.Client   // optional; defaults to a configured HTTP/2 client
	Timeout  time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.BundleID == "" {
		return nil, fmt.Errorf("apns: BundleID required")
	}
	if cfg.Auth == nil {
		return nil, fmt.Errorf("apns: Authenticator required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = Endpoint
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: cfg.Timeout}
	}
	return &Provider{cfg: cfg, client: c}, nil
}

var _ port.NotificationProvider = (*Provider)(nil)

// payload is the JSON Apple expects: an "aps" envelope plus arbitrary
// custom keys at the top level.
type payload struct {
	APS apsBody `json:"aps"`
}

type apsBody struct {
	Alert apsAlert `json:"alert"`
	Badge int      `json:"badge,omitempty"`
}

type apsAlert struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

func (p *Provider) Send(ctx context.Context, n *domain.Notification) error {
	if n.Recipient.DeviceToken.Empty() {
		return fmt.Errorf("apns: device token empty")
	}
	body, err := json.Marshal(payload{
		APS: apsBody{
			Alert: apsAlert{Title: n.Subject, Body: n.Body},
		},
	})
	if err != nil {
		return fmt.Errorf("apns marshal: %w", err)
	}

	url := fmt.Sprintf("%s/3/device/%s", p.cfg.BaseURL, n.Recipient.DeviceToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("apns request: %w", err)
	}
	auth, err := p.cfg.Auth.Authorization(ctx)
	if err != nil {
		return fmt.Errorf("apns auth: %w", err)
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apns-topic", p.cfg.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-id", n.ID.String())

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: apns http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("apns http %d (terminal)", resp.StatusCode)
	}
}
