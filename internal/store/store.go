// Package store contains all Postgres queries. Nothing outside this package
// should write SQL.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
)

// Store holds the connection pool. Create one with New at startup and pass
// *Store into whatever needs the database.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a connection pool and checks that Postgres is reachable.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// pgxpool.New connects lazily, so ping now to fail fast on a bad URL,
	// wrong password, or a database that isn't running.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Close releases all connections in the pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping checks that the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// ConflictError is a unique-constraint violation on a user-facing field.
type ConflictError struct{ Field string }

func (e *ConflictError) Error() string { return e.Field + " is already taken" }

// mapErr turns Postgres constraint errors into errors the API understands.
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return booking.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23P01": // exclusion_violation: overlapping appointment
		return booking.ErrSlotTaken
	case "23505": // unique_violation
		switch pgErr.ConstraintName {
		case "businesses_slug_key":
			return &ConflictError{Field: "booking link"}
		case "businesses_owner_email_key":
			return &ConflictError{Field: "email"}
		}
	}
	return err
}

// inTx runs fn in a transaction, committing if it returns nil.
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op after Commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
