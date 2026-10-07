// Package testdb gives each test package its own throwaway, migrated
// database. Tests skip when TEST_DATABASE_URL is not set.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/YoshrithMalhotra/bookly/internal/store"
	"github.com/YoshrithMalhotra/bookly/migrations"
)

// New creates a database, migrates it and drops it when the test ends.
// TEST_DATABASE_URL must point at a server where the user may CREATE DATABASE.
func New(t *testing.T) *store.Store {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 6)
	rand.Read(b)
	name := "bookly_test_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	s, err := store.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		c, err := pgx.Connect(context.Background(), admin)
		if err == nil {
			c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			c.Close(context.Background())
		}
	})
	if err := s.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return s
}
