// Package worker sends due reminders and review requests.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
	"github.com/YoshrithMalhotra/bookly/internal/notify"
	"github.com/YoshrithMalhotra/bookly/internal/store"
)

// BatchSize caps how many messages one tick handles before checking the clock again.
const BatchSize = 100

type Worker struct {
	Store     *store.Store
	Sender    notify.Sender
	PublicURL string
	Now       func() time.Time
}

// Run processes due messages every interval until ctx is cancelled. A
// message being sent when ctx is cancelled finishes first, so a shutdown
// never loses or duplicates a message.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastCleanup := time.Time{}
	for {
		n, err := w.SendDue(ctx)
		if err != nil {
			slog.Error("send due", "error", err)
		} else if n > 0 {
			slog.Info("processed messages", "count", n)
		}
		if time.Since(lastCleanup) > time.Hour {
			if n, err := w.Store.DeleteExpiredSessions(ctx); err != nil {
				slog.Error("delete expired sessions", "error", err)
			} else if n > 0 {
				slog.Info("deleted expired sessions", "count", n)
			}
			lastCleanup = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SendDue handles up to BatchSize due messages and returns how many it processed.
func (w *Worker) SendDue(ctx context.Context) (int, error) {
	n := 0
	for n < BatchSize && ctx.Err() == nil {
		// Each message gets its own transaction; detach from ctx so a
		// shutdown signal doesn't abort a send halfway through.
		found, err := w.Store.ProcessNextDue(context.WithoutCancel(ctx), w.now(), w.handle)
		if err != nil {
			return n, err
		}
		if !found {
			break
		}
		n++
	}
	return n, nil
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Worker) handle(ctx context.Context, now time.Time, m store.DueMessage) store.Outcome {
	log := slog.With("message_id", m.ID, "type", m.Type)
	switch booking.Decide(m.DueMessage, now) {
	case booking.ActionCancel:
		log.Info("message skipped")
		return store.Outcome{Status: "cancelled", Attempts: m.Attempts}
	case booking.ActionDefer:
		return store.Outcome{Status: "pending", Attempts: m.Attempts, SendAt: now.Add(booking.ReviewRecheck)}
	}

	loc, err := time.LoadLocation(m.Timezone)
	if err != nil {
		loc = time.UTC
	}
	content := booking.MessageContent{
		CustomerName: m.CustomerName,
		BusinessName: m.BusinessName,
		ServiceName:  m.ServiceName,
		StartsAt:     m.StartsAt,
		Location:     loc,
		ManageURL:    w.PublicURL + "/c/" + m.CancelToken,
		ReviewURL:    m.ReviewURL,
	}
	msg := notify.Message{
		To:   m.CustomerPhone,
		Kind: string(m.Type),
		Body: content.Body(m.Type),
		Vars: content.Vars(m.Type),
	}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// attempts counts failed tries, so fail-fail-success ends with 2.
	attempts := m.Attempts + 1
	if err := w.Sender.Send(sendCtx, msg); err != nil {
		if notify.IsPermanent(err) || attempts >= booking.MaxAttempts {
			log.Error("message failed", "attempts", attempts, "error", err)
			return store.Outcome{Status: "failed", Attempts: attempts, LastError: err.Error()}
		}
		next := now.Add(booking.RetryDelay(attempts))
		log.Warn("send failed, will retry", "attempts", attempts, "retry_at", next, "error", err)
		return store.Outcome{Status: "pending", Attempts: attempts, SendAt: next, LastError: err.Error()}
	}
	log.Info("message sent")
	return store.Outcome{Status: "sent", Attempts: m.Attempts}
}
