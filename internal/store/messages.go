package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
)

// DueMessage is a claimed message plus everything needed to send it.
type DueMessage struct {
	booking.DueMessage
	ID            int64
	Attempts      int
	CustomerName  string
	CustomerPhone string
	ServiceName   string
	BusinessName  string
	Timezone      string
	CancelToken   string
}

// Outcome is what happened to a due message; ProcessNextDue saves it.
type Outcome struct {
	Status    string    // "sent", "cancelled", "failed" or "pending" (retry/defer)
	SendAt    time.Time // next attempt when Status is pending
	Attempts  int
	LastError string
}

// ProcessNextDue claims one due message with FOR UPDATE SKIP LOCKED, so
// any number of workers can run side by side without sending a message
// twice, calls handle, and saves the outcome. The row stays locked while
// handle runs; if the process dies before commit the lock is released and
// the message is retried (at-least-once delivery). It reports false when
// nothing is due.
func (s *Store) ProcessNextDue(ctx context.Context, now time.Time, handle func(context.Context, time.Time, DueMessage) Outcome) (bool, error) {
	found := false
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var m DueMessage
		var reviewURL *string
		err := tx.QueryRow(ctx, `
			SELECT m.id, m.type, m.attempts,
			       a.status, a.whatsapp_opt_in, a.starts_at, a.ends_at,
			       a.customer_name, a.customer_phone, coalesce(a.cancel_token, ''),
			       s.name, b.name, b.timezone, b.google_review_url
			FROM scheduled_messages m
			JOIN appointments a ON a.id = m.appointment_id
			JOIN services s ON s.id = a.service_id
			JOIN businesses b ON b.id = a.business_id
			WHERE m.status = 'pending' AND m.send_at <= $1
			ORDER BY m.send_at
			LIMIT 1
			FOR UPDATE OF m SKIP LOCKED`, now).Scan(
			&m.ID, &m.Type, &m.Attempts,
			&m.Status, &m.OptIn, &m.StartsAt, &m.EndsAt,
			&m.CustomerName, &m.CustomerPhone, &m.CancelToken,
			&m.ServiceName, &m.BusinessName, &m.Timezone, &reviewURL)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if reviewURL != nil {
			m.ReviewURL = *reviewURL
		}

		out := handle(ctx, now, m)
		var lastErr *string
		if out.LastError != "" {
			lastErr = &out.LastError
		}
		_, err = tx.Exec(ctx, `
			UPDATE scheduled_messages
			SET status = $2::text, attempts = $3, last_error = $4,
			    send_at = CASE WHEN $2::text = 'pending' THEN $5::timestamptz ELSE send_at END,
			    sent_at = CASE WHEN $2::text = 'sent' THEN $6::timestamptz END
			WHERE id = $1`, m.ID, out.Status, out.Attempts, lastErr, out.SendAt, now)
		return err
	})
	return found, err
}

// MessageView is a scheduled message as shown to the owner.
type MessageView struct {
	Type      string     `json:"type"`
	Status    string     `json:"status"`
	SendAt    time.Time  `json:"send_at"`
	SentAt    *time.Time `json:"sent_at"`
	Attempts  int        `json:"attempts"`
	LastError *string    `json:"last_error"`
}

// Messages lists an appointment's scheduled messages, scoped by business.
func (s *Store) Messages(ctx context.Context, businessID, appointmentID int64) ([]MessageView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.type, m.status, m.send_at, m.sent_at, m.attempts, m.last_error
		FROM scheduled_messages m JOIN appointments a ON a.id = m.appointment_id
		WHERE a.business_id = $1 AND a.id = $2
		ORDER BY m.send_at`, businessID, appointmentID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MessageView, error) {
		var m MessageView
		err := r.Scan(&m.Type, &m.Status, &m.SendAt, &m.SentAt, &m.Attempts, &m.LastError)
		return m, err
	})
}
