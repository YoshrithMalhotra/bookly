// Package notify sends messages to customers.
package notify

import (
	"context"
	"errors"
	"log/slog"
)

// Message is one outgoing message. Body is the free-form text; Template and
// Vars are used instead when the sender has an approved template for Kind.
type Message struct {
	To   string // E.164, e.g. +447700900123
	Kind string // "reminder" or "review"
	Body string
	Vars []string
}

// Sender delivers one message. The worker only depends on this interface,
// so the fake and the real WhatsApp sender are swappable.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// PermanentError means retrying will not help (bad number, rejected
// template...). The worker marks the message failed straight away.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

func IsPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}

// FakeSender logs instead of sending. Used when Twilio isn't configured.
type FakeSender struct{}

func (FakeSender) Send(ctx context.Context, msg Message) error {
	slog.Info("fake send", "to", msg.To, "kind", msg.Kind, "body", msg.Body)
	return nil
}
