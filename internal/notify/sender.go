// Package notify sends messages to customers.
package notify

import (
	"context"
	"log/slog"
)

// Sender delivers one message to a phone number. The worker only depends on
// this interface, so the fake and the real WhatsApp sender are swappable.
type Sender interface {
	Send(ctx context.Context, phone, body string) error
}

// FakeSender logs instead of sending. Use it until week 4.
type FakeSender struct{}

func (FakeSender) Send(ctx context.Context, phone, body string) error {
	slog.Info("fake send", "phone", phone, "body", body)
	return nil
}

// TODO(week 4): TwilioSender implementing Sender via the WhatsApp sandbox.
