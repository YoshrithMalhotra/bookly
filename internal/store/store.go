// Package store contains all Postgres queries. Nothing outside this package
// should write SQL.
package store

// TODO(week 1): pick a driver (github.com/jackc/pgx/v5 — you used it in the
// URL shortener) and add a Store struct holding the connection pool.
// Pass *Store into handlers instead of using a global DB variable.
