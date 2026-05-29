// Package whatsapp implements port.NotificationProvider using the Meta Cloud API.
// Reference: https://developers.facebook.com/docs/whatsapp/cloud-api/messages
package whatsapp

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
	PhoneNumberID string
	AccessToken   string
	BaseURL       string // override for tests; defaults to "https://graph.facebook.com/v18.0"
	Client        *http.Client
	Timeout       time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.PhoneNumberID == "" || cfg.AccessToken == "" {
		return nil, fmt.Errorf("whatsapp: PhoneNumberID and AccessToken required")
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
	if n.Recipient.Phone.Empty() {
		return fmt.Errorf("whatsapp: phone empty")
	}
	payload, err := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                string(n.Recipient.Phone),
		"type":              "text",
		"text":              map[string]string{"body": n.Body},
	})
	if err != nil {
		return fmt.Errorf("whatsapp: marshal payload: %w", err)
	}
	url := fmt.Sprintf("%s/%s/messages", p.cfg.BaseURL, p.cfg.PhoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("whatsapp request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.AccessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: whatsapp http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("whatsapp http %d (terminal)", resp.StatusCode)
	}
}
