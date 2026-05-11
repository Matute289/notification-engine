package port

import (
	"context"
	"errors"

	"github.com/example/notification-engine/internal/domain"
)

// ErrTransient marks a provider failure that the use case should retry. Any
// other error is treated as terminal (4xx-class) and dead-lettered immediately.
var ErrTransient = errors.New("transient provider error")

// NotificationProvider is the outbound port to a third-party delivery service
// (APNs, FCM, Twilio, SendGrid). Implementations must be goroutine-safe.
type NotificationProvider interface {
	Send(ctx context.Context, n *domain.Notification) error
}
