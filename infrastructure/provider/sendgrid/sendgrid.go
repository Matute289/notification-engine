// Package sendgrid implements port.NotificationProvider for email via the
// SendGrid v3 mail/send API. Authentication is a bearer API key.
//
// References:
//   https://docs.sendgrid.com/api-reference/mail-send/mail-send
package sendgrid

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

type Config struct {
	APIKey     string
	FromEmail  string
	FromName   string
	BaseURL    string // override for tests
	Client     *http.Client
	Timeout    time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("sendgrid: APIKey required")
	}
	if cfg.FromEmail == "" {
		return nil, fmt.Errorf("sendgrid: FromEmail required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.sendgrid.com"
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

// v3 wire format (subset).
type sendRequest struct {
	Personalizations []personalization `json:"personalizations"`
	From             contact           `json:"from"`
	Subject          string            `json:"subject"`
	Content          []content         `json:"content"`
}
type personalization struct {
	To []contact `json:"to"`
}
type contact struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}
type content struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (p *Provider) Send(ctx context.Context, n *domain.Notification) error {
	if n.Recipient.Email.Empty() {
		return fmt.Errorf("sendgrid: recipient email empty")
	}
	body, err := json.Marshal(sendRequest{
		Personalizations: []personalization{{
			To: []contact{{Email: string(n.Recipient.Email)}},
		}},
		From:    contact{Email: p.cfg.FromEmail, Name: p.cfg.FromName},
		Subject: n.Subject,
		Content: []content{{Type: "text/html", Value: n.Body}},
	})
	if err != nil {
		return fmt.Errorf("sendgrid marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/v3/mail/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sendgrid request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusAccepted:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: sendgrid http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("sendgrid http %d (terminal)", resp.StatusCode)
	}
}
