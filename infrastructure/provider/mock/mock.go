// Package mock is a port.NotificationProvider that simply logs each send.
// The local docker-compose stack uses it so a tester can exercise the full
// pipeline without real APNs/FCM/Twilio/SendGrid credentials.
package mock

import (
	"context"
	"log/slog"
	"math/rand/v2"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
)

type Provider struct {
	log         *slog.Logger
	failureRate float64
}

func New(log *slog.Logger, failureRate float64) *Provider {
	return &Provider{log: log, failureRate: failureRate}
}

var _ port.NotificationProvider = (*Provider)(nil)

func (p *Provider) Send(_ context.Context, n *domain.Notification) error {
	if p.failureRate > 0 && rand.Float64() < p.failureRate {
		p.log.Warn("mock provider: simulating transient failure",
			"id", n.ID, "channel", n.Channel, "event_id", n.EventID)
		return port.ErrTransient
	}
	p.log.Info("mock provider delivered notification",
		"id", n.ID, "channel", n.Channel, "event_id", n.EventID,
		"recipient", n.Recipient, "subject", n.Subject, "body", n.Body)
	return nil
}
