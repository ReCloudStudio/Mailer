// Package notify delivers mail notifications to chat platforms.
package notify

import (
	"context"

	"github.com/recloud/mailer/internal/mail"
)

// Notifier delivers a single mail notification to a destination.
type Notifier interface {
	// Name identifies the notifier in logs.
	Name() string
	// Send delivers the message. It should be safe to call concurrently.
	Send(ctx context.Context, msg mail.Message) error
}
