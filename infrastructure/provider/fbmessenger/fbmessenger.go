// Package fbmessenger implements port.NotificationProvider using the Meta Graph API.
// Reference: https://developers.facebook.com/docs/messenger-platform/send-messages
package fbmessenger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

type Config struct {
	PageAccessToken string
	BaseURL         string // override for tests; defaults to "https://graph.facebook.com/v18.0"
	Client          *http.Client
	Timeout         time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.PageAccessToken == "" {
		return nil, fmt.Errorf("fbmessenger: PageAccessToken required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://graph.facebook.com/v18.0"
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

func (p *Provider) Send(ctx context.Context, n *domain.Notification) error {
	if n.Recipient.MessagingID == "" {
		return fmt.Errorf("fbmessenger: messaging_id empty")
	}
	payload, err := json.Marshal(map[string]any{
		"recipient": map[string]string{"id": n.Recipient.MessagingID},
		"message":   map[string]string{"text": n.Body},
	})
	if err != nil {
		return fmt.Errorf("fbmessenger: marshal payload: %w", err)
	}
	url := p.cfg.BaseURL + "/me/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("fbmessenger request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.PageAccessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: fbmessenger http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("fbmessenger http %d (terminal)", resp.StatusCode)
	}
}
