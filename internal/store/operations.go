package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
)

// Time off

type TimeOff struct {
	ID       int64     `json:"id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Reason   string    `json:"reason"`
}

// TimeOffFrom lists time off that hasn't ended by from.
func (s *Store) TimeOffFrom(ctx context.Context, businessID int64, from time.Time) ([]TimeOff, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, starts_at, ends_at, reason FROM time_off
		WHERE business_id = $1 AND ends_at > $2 ORDER BY starts_at`, businessID, from)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TimeOff, error) {
		var t TimeOff
		err := r.Scan(&t.ID, &t.StartsAt, &t.EndsAt, &t.Reason)
		return t, err
	})
}

// AddTimeOff blocks a period and returns it with the number of existing
// appointments inside it (the owner decides what to do with those).
func (s *Store) AddTimeOff(ctx context.Context, businessID int64, t TimeOff) (TimeOff, int, error) {
	var clashes int
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM businesses WHERE id = $1 FOR UPDATE`, businessID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO time_off (business_id, starts_at, ends_at, reason) VALUES ($1, $2, $3, $4)
			RETURNING id`, businessID, t.StartsAt, t.EndsAt, t.Reason).Scan(&t.ID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT count(*) FROM appointments
			WHERE business_id = $1 AND status IN ('booked', 'confirmed')
			  AND starts_at < $3 AND ends_at > $2`, businessID, t.StartsAt, t.EndsAt).Scan(&clashes)
	})
	return t, clashes, mapErr(err)
}

func (s *Store) DeleteTimeOff(ctx context.Context, businessID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM time_off WHERE business_id = $1 AND id = $2`, businessID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return booking.ErrNotFound
	}
	return mapErr(err)
}

// Business settings

func (s *Store) UpdateBusiness(ctx context.Context, id int64, name, timezone, reviewURL, currency string) (Business, error) {
	return scanBusiness(s.pool.QueryRow(ctx, `
		UPDATE businesses SET name = $2, timezone = $3, google_review_url = nullif($4, ''), currency = $5
		WHERE id = $1 RETURNING `+businessCols, id, name, timezone, reviewURL, currency))
}

// Email verification

const EmailVerificationTTL = 7 * 24 * time.Hour

// CreateEmailVerification returns a token proving the owner controls the
// business's current login email.
func (s *Store) CreateEmailVerification(ctx context.Context, businessID int64) (Business, string, error) {
	token := NewToken()
	var b Business
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		b, err = scanBusiness(tx.QueryRow(ctx, `SELECT `+businessCols+` FROM businesses WHERE id = $1`, businessID))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM email_verifications WHERE business_id = $1`, businessID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO email_verifications (token_hash, business_id, email, expires_at)
			VALUES ($1, $2, $3, now() + make_interval(secs => $4))`,
			hashToken(token), businessID, b.OwnerEmail, EmailVerificationTTL.Seconds())
		return err
	})
	return b, token, mapErr(err)
}

// VerifyEmail marks the email verified if the token is valid and the
// account still uses the email the link was sent to.
func (s *Store) VerifyEmail(ctx context.Context, token string) error {
	return mapErr(s.inTx(ctx, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `
			DELETE FROM email_verifications v
			USING businesses b
			WHERE v.token_hash = $1 AND v.expires_at > now()
			  AND b.id = v.business_id AND b.owner_email = v.email
			RETURNING b.id`, hashToken(token)).Scan(&id)
		if err == pgx.ErrNoRows {
			return &booking.ValidationError{Msg: "this verification link has expired or was already used"}
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE businesses SET email_verified_at = now() WHERE id = $1`, id)
		return err
	}))
}

// ChangeEmail sets a new login email, which then needs verifying.
func (s *Store) ChangeEmail(ctx context.Context, businessID int64, email string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE businesses SET owner_email = $2, email_verified_at = NULL WHERE id = $1`, businessID, email)
	return mapErr(err)
}

// Export

// AllAppointments returns every appointment of a business, oldest first.
func (s *Store) AllAppointments(ctx context.Context, businessID int64) ([]AppointmentView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+apptCols+` FROM appointments a JOIN services s ON s.id = a.service_id
		WHERE a.business_id = $1 ORDER BY a.starts_at, a.id`, businessID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AppointmentView, error) { return scanAppt(r) })
}
