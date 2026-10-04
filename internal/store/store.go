// Package store contains all Postgres queries. Nothing outside this package
// should write SQL.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
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
