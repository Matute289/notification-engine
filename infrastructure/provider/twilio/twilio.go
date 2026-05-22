// Package twilio implements port.NotificationProvider for SMS via Twilio's
// REST API (Messages resource). Authentication is HTTP Basic with
// AccountSID + AuthToken. Errors in the 5xx range or HTTP 429 are reported
// as transient; everything else is terminal.
//
// References:
//   https://www.twilio.com/docs/sms/quickstart/go
package twilio

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
)

type Config struct {
	AccountSID  string
	AuthToken   string
	FromNumber  string // e.g. "+15551234567"
	BaseURL     string // override for tests
	Client      *http.Client
	Timeout     time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.AccountSID == "" || cfg.AuthToken == "" {
		return nil, fmt.Errorf("twilio: AccountSID and AuthToken required")
	}
	if cfg.FromNumber == "" {
		return nil, fmt.Errorf("twilio: FromNumber required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.twilio.com"
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
		return fmt.Errorf("twilio: phone empty")
	}
	form := url.Values{}
	form.Set("To", string(n.Recipient.Phone))
	form.Set("From", p.cfg.FromNumber)
	form.Set("Body", n.Body)

	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json",
		p.cfg.BaseURL, p.cfg.AccountSID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("twilio request: %w", err)
	}
	req.SetBasicAuth(p.cfg.AccountSID, p.cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: twilio http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("twilio http %d (terminal)", resp.StatusCode)
	}
}
