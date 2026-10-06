package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
)

type Business struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Timezone        string    `json:"timezone"`
	GoogleReviewURL string    `json:"google_review_url"`
	OwnerEmail      string    `json:"owner_email"`
	CreatedAt       time.Time `json:"created_at"`
}

type Service struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DurationMin int    `json:"duration_min"`
	Price       string `json:"price"` // decimal as text, e.g. "25.00"
	Active      bool   `json:"active"`
}

const businessCols = `id, name, slug, timezone, coalesce(google_review_url, ''), owner_email, created_at`

func scanBusiness(row pgx.Row) (Business, error) {
	var b Business
	err := row.Scan(&b.ID, &b.Name, &b.Slug, &b.Timezone, &b.GoogleReviewURL, &b.OwnerEmail, &b.CreatedAt)
	return b, mapErr(err)
}

func (s *Store) BusinessBySlug(ctx context.Context, slug string) (Business, error) {
	return scanBusiness(s.pool.QueryRow(ctx, `SELECT `+businessCols+` FROM businesses WHERE slug = $1`, slug))
}

func (s *Store) BusinessByID(ctx context.Context, id int64) (Business, error) {
	return scanBusiness(s.pool.QueryRow(ctx, `SELECT `+businessCols+` FROM businesses WHERE id = $1`, id))
}

// Credentials returns the business id and password hash for a login email.
func (s *Store) Credentials(ctx context.Context, email string) (int64, []byte, error) {
	var id int64
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT id, password_hash FROM businesses WHERE owner_email = $1`, email).Scan(&id, &hash)
	return id, []byte(hash), mapErr(err)
}

type NewBusiness struct {
	Name, Slug, Timezone, OwnerEmail string
	PasswordHash                     []byte
}

// CreateBusiness signs up a new business with Mon–Fri 09:00–17:00 hours
// so the booking page works straight away.
func (s *Store) CreateBusiness(ctx context.Context, nb NewBusiness) (Business, error) {
	var b Business
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		b, err = scanBusiness(tx.QueryRow(ctx, `
			INSERT INTO businesses (name, slug, timezone, owner_email, password_hash)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING `+businessCols,
			nb.Name, nb.Slug, nb.Timezone, nb.OwnerEmail, string(nb.PasswordHash)))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO opening_hours (business_id, weekday, opens_at, closes_at)
			SELECT $1, d, '09:00', '17:00' FROM generate_series(1, 5) d`, b.ID)
		return err
	})
	return b, mapErr(err)
}

func (s *Store) UpdateBusiness(ctx context.Context, id int64, name, timezone, reviewURL string) (Business, error) {
	return scanBusiness(s.pool.QueryRow(ctx, `
		UPDATE businesses SET name = $2, timezone = $3, google_review_url = nullif($4, '')
		WHERE id = $1 RETURNING `+businessCols, id, name, timezone, reviewURL))
}

// Services lists a business's services; activeOnly hides retired ones.
func (s *Store) Services(ctx context.Context, businessID int64, activeOnly bool) ([]Service, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, duration_min, price::text, active FROM services
		WHERE business_id = $1 AND (active OR NOT $2)
		ORDER BY active DESC, name, id`, businessID, activeOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Service, error) {
		var sv Service
		err := r.Scan(&sv.ID, &sv.Name, &sv.DurationMin, &sv.Price, &sv.Active)
		return sv, err
	})
}

func (s *Store) Service(ctx context.Context, businessID, id int64) (Service, error) {
	var sv Service
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, duration_min, price::text, active FROM services
		WHERE business_id = $1 AND id = $2`, businessID, id).
		Scan(&sv.ID, &sv.Name, &sv.DurationMin, &sv.Price, &sv.Active)
	return sv, mapErr(err)
}

func (s *Store) CreateService(ctx context.Context, businessID int64, sv Service) (Service, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO services (business_id, name, duration_min, price, active)
		VALUES ($1, $2, $3, $4::numeric, $5)
		RETURNING id, price::text`, businessID, sv.Name, sv.DurationMin, sv.Price, sv.Active).
		Scan(&sv.ID, &sv.Price)
	return sv, mapErr(err)
}

// UpdateService edits a service. Retire it with Active=false; services are
// never deleted because past appointments point at them.
func (s *Store) UpdateService(ctx context.Context, businessID int64, sv Service) (Service, error) {
	err := s.pool.QueryRow(ctx, `
		UPDATE services SET name = $3, duration_min = $4, price = $5::numeric, active = $6
		WHERE business_id = $1 AND id = $2
		RETURNING price::text`, businessID, sv.ID, sv.Name, sv.DurationMin, sv.Price, sv.Active).
		Scan(&sv.Price)
	return sv, mapErr(err)
}

func (s *Store) OpeningHours(ctx context.Context, businessID int64) ([]booking.OpeningHours, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT weekday,
		       extract(hour FROM opens_at)::int * 60 + extract(minute FROM opens_at)::int,
		       extract(hour FROM closes_at)::int * 60 + extract(minute FROM closes_at)::int
		FROM opening_hours WHERE business_id = $1 ORDER BY weekday`, businessID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (booking.OpeningHours, error) {
		var h booking.OpeningHours
		var wd int
		err := r.Scan(&wd, &h.Opens, &h.Closes)
		h.Weekday = time.Weekday(wd)
		return h, err
	})
}

// SetOpeningHours replaces the whole week. Days not listed are closed.
func (s *Store) SetOpeningHours(ctx context.Context, businessID int64, hours []booking.OpeningHours) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM opening_hours WHERE business_id = $1`, businessID); err != nil {
			return err
		}
		for _, h := range hours {
			if _, err := tx.Exec(ctx, `
				INSERT INTO opening_hours (business_id, weekday, opens_at, closes_at)
				VALUES ($1, $2, $3::time, $4::time)`,
				businessID, int(h.Weekday), booking.FormatClock(h.Opens), booking.FormatClock(h.Closes)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Sessions

const SessionTTL = 30 * 24 * time.Hour

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// NewToken returns 32 random bytes, URL-safe encoded.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// CreateSession logs a business owner in and returns the cookie value.
func (s *Store) CreateSession(ctx context.Context, businessID int64) (string, error) {
	token := NewToken()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, business_id, expires_at) VALUES ($1, $2, now() + make_interval(secs => $3))`,
		hashToken(token), businessID, SessionTTL.Seconds())
	return token, err
}

// SessionBusiness returns the business id for a valid session token.
func (s *Store) SessionBusiness(ctx context.Context, token string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT business_id FROM sessions WHERE token_hash = $1 AND expires_at > now()`,
		hashToken(token)).Scan(&id)
	return id, mapErr(err)
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hashToken(token))
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return tag.RowsAffected(), err
}
