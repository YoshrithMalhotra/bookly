package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/YoshrithMalhotra/bookly/internal/billing"
	"github.com/YoshrithMalhotra/bookly/internal/booking"
)

// Billing

func (s *Store) SetStripeCustomer(ctx context.Context, businessID int64, customerID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE businesses SET stripe_customer_id = $2 WHERE id = $1`, businessID, customerID)
	return mapErr(err)
}

// ApplySubscription stores the latest state of a Stripe subscription. It
// matches the business by Stripe customer, falling back to the business id
// from metadata. An old subscription's events never overwrite a newer one
// unless they make it live again.
func (s *Store) ApplySubscription(ctx context.Context, sub billing.Subscription) (bool, error) {
	var periodEnd *time.Time
	if !sub.CurrentPeriodEnd.IsZero() {
		periodEnd = &sub.CurrentPeriodEnd
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE businesses
		SET stripe_customer_id = $1, stripe_subscription_id = $2,
		    subscription_status = $3, current_period_end = $4
		WHERE (stripe_customer_id = $1 OR (stripe_customer_id IS NULL AND id = $5))
		  AND (stripe_subscription_id IS NULL OR stripe_subscription_id = $2
		       OR $3 IN ('active', 'trialing', 'past_due'))`,
		sub.CustomerID, sub.ID, sub.Status, periodEnd, sub.BusinessID)
	return tag.RowsAffected() > 0, mapErr(err)
}

// Password resets

const PasswordResetTTL = time.Hour

// CreatePasswordReset returns a single-use token for the owner of email,
// or booking.ErrNotFound if there is no such account.
func (s *Store) CreatePasswordReset(ctx context.Context, email string) (Business, string, error) {
	var b Business
	token := NewToken()
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		b, err = scanBusiness(tx.QueryRow(ctx, `SELECT `+businessCols+` FROM businesses WHERE owner_email = $1`, email))
		if err != nil {
			return err
		}
		// One live link at a time.
		if _, err := tx.Exec(ctx, `DELETE FROM password_resets WHERE business_id = $1`, b.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO password_resets (token_hash, business_id, expires_at)
			VALUES ($1, $2, now() + make_interval(secs => $3))`,
			hashToken(token), b.ID, PasswordResetTTL.Seconds())
		return err
	})
	return b, token, mapErr(err)
}

// ResetPassword uses a reset token to set a new password hash and logs out
// every existing session. It returns the business id.
func (s *Store) ResetPassword(ctx context.Context, token string, hash []byte) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE password_resets SET used_at = now()
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
			RETURNING business_id`, hashToken(token)).Scan(&id)
		if err == pgx.ErrNoRows {
			return &booking.ValidationError{Msg: "this reset link has expired or was already used; please request a new one"}
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE businesses SET password_hash = $2 WHERE id = $1`, id, string(hash)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE business_id = $1`, id)
		return err
	})
	return id, mapErr(err)
}

// PasswordHash returns the stored hash for a business.
func (s *Store) PasswordHash(ctx context.Context, businessID int64) ([]byte, error) {
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM businesses WHERE id = $1`, businessID).Scan(&hash)
	return []byte(hash), mapErr(err)
}

// ChangePassword sets a new hash and logs out every other session.
func (s *Store) ChangePassword(ctx context.Context, businessID int64, hash []byte, keepSession string) error {
	return mapErr(s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE businesses SET password_hash = $2 WHERE id = $1`, businessID, string(hash)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM sessions WHERE business_id = $1 AND token_hash <> $2`, businessID, hashToken(keepSession))
		return err
	}))
}

// DeleteBusiness removes the business and, by cascade, all its services,
// appointments, messages and sessions.
func (s *Store) DeleteBusiness(ctx context.Context, businessID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, businessID)
	return mapErr(err)
}
