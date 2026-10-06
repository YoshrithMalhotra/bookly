package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
)

// AppointmentView is an appointment with the details people see on screen.
type AppointmentView struct {
	ID            int64          `json:"id"`
	ServiceID     int64          `json:"service_id"`
	ServiceName   string         `json:"service_name"`
	CustomerName  string         `json:"customer_name"`
	CustomerPhone string         `json:"customer_phone"`
	StartsAt      time.Time      `json:"starts_at"`
	EndsAt        time.Time      `json:"ends_at"`
	Status        booking.Status `json:"status"`
	WhatsAppOptIn bool           `json:"whatsapp_opt_in"`
}

const apptCols = `a.id, a.service_id, s.name, a.customer_name, a.customer_phone,
	a.starts_at, a.ends_at, a.status, a.whatsapp_opt_in`

func scanAppt(row pgx.Row) (AppointmentView, error) {
	var a AppointmentView
	err := row.Scan(&a.ID, &a.ServiceID, &a.ServiceName, &a.CustomerName, &a.CustomerPhone,
		&a.StartsAt, &a.EndsAt, &a.Status, &a.WhatsAppOptIn)
	return a, err
}

// BusyIntervals returns non-cancelled appointments overlapping [from, to).
func (s *Store) BusyIntervals(ctx context.Context, businessID int64, from, to time.Time) ([]booking.Interval, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT starts_at, ends_at FROM appointments
		WHERE business_id = $1 AND status <> 'cancelled'
		  AND starts_at < $3 AND ends_at > $2`, businessID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (booking.Interval, error) {
		var i booking.Interval
		err := r.Scan(&i.Start, &i.End)
		return i, err
	})
}

// CreateAppointment inserts the appointment and its scheduled messages in
// one transaction and returns its id and the customer's manage token. An
// overlapping booking fails with booking.ErrSlotTaken and leaves nothing behind.
func (s *Store) CreateAppointment(ctx context.Context, businessID int64, req booking.Request, endsAt time.Time, msgs []booking.PlannedMessage) (int64, string, error) {
	token := NewToken()
	var id int64
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO appointments (business_id, service_id, customer_name, customer_phone,
			                          starts_at, ends_at, whatsapp_opt_in, cancel_token)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			businessID, req.ServiceID, req.CustomerName, req.CustomerPhone,
			req.StartsAt, endsAt, req.WhatsAppOptIn, token).Scan(&id)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO scheduled_messages (appointment_id, type, send_at) VALUES ($1, $2, $3)`,
				id, string(m.Type), m.SendAt); err != nil {
				return err
			}
		}
		return nil
	})
	return id, token, mapErr(err)
}

// AppointmentsBetween lists a business's appointments starting in [from, to).
func (s *Store) AppointmentsBetween(ctx context.Context, businessID int64, from, to time.Time) ([]AppointmentView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+apptCols+` FROM appointments a JOIN services s ON s.id = a.service_id
		WHERE a.business_id = $1 AND a.starts_at >= $2 AND a.starts_at < $3
		ORDER BY a.starts_at, a.id`, businessID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AppointmentView, error) { return scanAppt(r) })
}

// AppointmentByToken is the customer's view of their booking.
func (s *Store) AppointmentByToken(ctx context.Context, token string) (AppointmentView, Business, error) {
	var businessID int64
	err := s.pool.QueryRow(ctx, `SELECT business_id FROM appointments WHERE cancel_token = $1`, token).Scan(&businessID)
	if err != nil {
		return AppointmentView{}, Business{}, mapErr(err)
	}
	a, err := scanAppt(s.pool.QueryRow(ctx, `
		SELECT `+apptCols+` FROM appointments a JOIN services s ON s.id = a.service_id
		WHERE a.cancel_token = $1`, token))
	if err != nil {
		return a, Business{}, mapErr(err)
	}
	b, err := s.BusinessByID(ctx, businessID)
	return a, b, err
}

// CancelByToken lets a customer cancel their own upcoming booking.
func (s *Store) CancelByToken(ctx context.Context, token string, now time.Time) error {
	return mapErr(s.inTx(ctx, func(tx pgx.Tx) error {
		var id int64
		var status booking.Status
		var startsAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT id, status, starts_at FROM appointments WHERE cancel_token = $1 FOR UPDATE`, token).
			Scan(&id, &status, &startsAt)
		if err != nil {
			return err
		}
		if status != booking.StatusBooked && status != booking.StatusConfirmed {
			return &booking.ValidationError{Msg: "this booking can no longer be cancelled"}
		}
		if !startsAt.After(now) {
			return &booking.ValidationError{Msg: "this appointment has already started"}
		}
		return setStatus(ctx, tx, id, booking.StatusCancelled)
	}))
}

// SetStatus is the owner changing an appointment's status. The business id
// scopes the query so owners can only touch their own appointments.
func (s *Store) SetStatus(ctx context.Context, businessID, id int64, to booking.Status, now time.Time) (AppointmentView, error) {
	var a AppointmentView
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var from booking.Status
		var startsAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT status, starts_at FROM appointments WHERE id = $1 AND business_id = $2 FOR UPDATE`,
			id, businessID).Scan(&from, &startsAt)
		if err != nil {
			return err
		}
		if !booking.CanTransition(from, to) {
			return &booking.ValidationError{Msg: "can't change a " + string(from) + " appointment to " + string(to)}
		}
		if (to == booking.StatusDone || to == booking.StatusNoShow) && startsAt.After(now) {
			return &booking.ValidationError{Msg: "this appointment hasn't started yet"}
		}
		if err := setStatus(ctx, tx, id, to); err != nil {
			return err
		}
		a, err = scanAppt(tx.QueryRow(ctx, `
			SELECT `+apptCols+` FROM appointments a JOIN services s ON s.id = a.service_id WHERE a.id = $1`, id))
		return err
	})
	return a, mapErr(err)
}

// setStatus updates the appointment and keeps its pending messages in step.
func setStatus(ctx context.Context, tx pgx.Tx, id int64, to booking.Status) error {
	if _, err := tx.Exec(ctx, `UPDATE appointments SET status = $2 WHERE id = $1`, id, to); err != nil {
		return err
	}
	switch to {
	case booking.StatusCancelled, booking.StatusNoShow:
		_, err := tx.Exec(ctx, `
			UPDATE scheduled_messages SET status = 'cancelled'
			WHERE appointment_id = $1 AND status = 'pending'`, id)
		return err
	case booking.StatusDone:
		// Changed from no-show back to done: revive the review request.
		_, err := tx.Exec(ctx, `
			UPDATE scheduled_messages m
			SET status = 'pending', attempts = 0, last_error = NULL,
			    send_at = greatest(now(), a.ends_at + make_interval(secs => $2))
			FROM appointments a
			WHERE a.id = m.appointment_id AND m.appointment_id = $1
			  AND m.type = 'review' AND m.status = 'cancelled' AND m.sent_at IS NULL`,
			id, booking.ReviewDelay.Seconds())
		return err
	}
	return nil
}

// Stats are counts of past appointments by status since a given time.
type Stats struct {
	Done      int `json:"done"`
	NoShow    int `json:"no_show"`
	Cancelled int `json:"cancelled"`
	Unmarked  int `json:"unmarked"`
	Reminders int `json:"reminders_sent"`
	Reviews   int `json:"reviews_sent"`
}

func (s *Store) Stats(ctx context.Context, businessID int64, since, now time.Time) (Stats, error) {
	var st Stats
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'done'),
		       count(*) FILTER (WHERE status = 'no_show'),
		       count(*) FILTER (WHERE status = 'cancelled'),
		       count(*) FILTER (WHERE status IN ('booked', 'confirmed'))
		FROM appointments
		WHERE business_id = $1 AND starts_at >= $2 AND starts_at < $3`, businessID, since, now).
		Scan(&st.Done, &st.NoShow, &st.Cancelled, &st.Unmarked)
	if err != nil {
		return st, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE m.type = 'reminder'), count(*) FILTER (WHERE m.type = 'review')
		FROM scheduled_messages m JOIN appointments a ON a.id = m.appointment_id
		WHERE a.business_id = $1 AND m.status = 'sent' AND m.sent_at >= $2`, businessID, since).
		Scan(&st.Reminders, &st.Reviews)
	return st, err
}
