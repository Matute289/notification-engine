// Package line implements port.NotificationProvider using the LINE Messaging API.
// Reference: https://developers.line.biz/en/reference/messaging-api/#send-push-message
package line

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
	ChannelAccessToken string
	BaseURL            string // override for tests; defaults to "https://api.line.me"
	Client             *http.Client
	Timeout            time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.ChannelAccessToken == "" {
		return nil, fmt.Errorf("line: ChannelAccessToken required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.line.me"
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
		return fmt.Errorf("line: messaging_id empty")
	}
	payload, err := json.Marshal(map[string]any{
		"to":       n.Recipient.MessagingID,
		"messages": []map[string]string{{"type": "text", "text": n.Body}},
	})
	if err != nil {
		return fmt.Errorf("line: marshal payload: %w", err)
	}
	url := p.cfg.BaseURL + "/v2/bot/message/push"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("line request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.ChannelAccessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: line http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("line http %d (terminal)", resp.StatusCode)
	}
}
