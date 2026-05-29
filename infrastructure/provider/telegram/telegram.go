// Package telegram implements port.NotificationProvider for the Telegram Bot API.
// Reference: https://core.telegram.org/bots/api#sendmessage
package telegram

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
	BotToken string
	BaseURL  string // override for tests; defaults to "https://api.telegram.org"
	Client   *http.Client
	Timeout  time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("telegram: BotToken required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.telegram.org"
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
		return fmt.Errorf("telegram: messaging_id empty")
	}
	payload, err := json.Marshal(map[string]any{
		"chat_id": n.Recipient.MessagingID,
		"text":    n.Body,
	})
	if err != nil {
		return fmt.Errorf("telegram: marshal payload: %w", err)
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", p.cfg.BaseURL, p.cfg.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: telegram http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("telegram http %d (terminal)", resp.StatusCode)
	}
}
