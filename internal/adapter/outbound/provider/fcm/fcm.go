// Package fcm implements port.NotificationProvider against Firebase Cloud
// Messaging (HTTP v1 API).
//
// Skeleton only: request shape and endpoint match the public docs but token
// acquisition (Google service-account OAuth2) is delegated to a TokenSource
// so callers wire it up themselves (typically golang.org/x/oauth2/google).
//
// References:
//   https://firebase.google.com/docs/cloud-messaging/send-message
package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
)

// TokenSource returns a fresh Google access token. Implementations should
// cache + refresh as appropriate.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type Config struct {
	ProjectID   string // Firebase project id; baked into the endpoint URL
	BaseURL     string // override for tests
	TokenSource TokenSource
	Client      *http.Client
	Timeout     time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("fcm: ProjectID required")
	}
	if cfg.TokenSource == nil {
		return nil, fmt.Errorf("fcm: TokenSource required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://fcm.googleapis.com"
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

// FCM v1 wire format.
type sendRequest struct {
	Message message `json:"message"`
}
type message struct {
	Token        string       `json:"token"`
	Notification *fcmNotif    `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
}
type fcmNotif struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

func (p *Provider) Send(ctx context.Context, n *domain.Notification) error {
	if n.Recipient.DeviceToken.Empty() {
		return fmt.Errorf("fcm: device token empty")
	}
	body, err := json.Marshal(sendRequest{Message: message{
		Token:        string(n.Recipient.DeviceToken),
		Notification: &fcmNotif{Title: n.Subject, Body: n.Body},
		Data:         n.Variables,
	}})
	if err != nil {
		return fmt.Errorf("fcm marshal: %w", err)
	}

	url := fmt.Sprintf("%s/v1/projects/%s/messages:send", p.cfg.BaseURL, p.cfg.ProjectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("fcm request: %w", err)
	}
	tok, err := p.cfg.TokenSource.Token(ctx)
	if err != nil {
		return fmt.Errorf("fcm auth: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: fcm http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("fcm http %d (terminal)", resp.StatusCode)
	}
}
