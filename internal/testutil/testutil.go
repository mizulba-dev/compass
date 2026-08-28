// Package testutil connects tests to the Postgres instance used for local
// development (docker-compose db service), isolating each test in its own
// schema so tests can run concurrently without clobbering each other's
// tables.
package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func testDSN() (dsn string, explicit bool) {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v, true
	}
	return "postgres://webmcp:webmcp@localhost:55432/webmcp?sslmode=disable", false
}

// NewDB opens a connection to the test database and creates a fresh, empty
// schema for the caller's exclusive use, dropped on test cleanup. Requires a
// reachable Postgres (see docker-compose.yml's db service). With the
// default DSN, tests skip with a clear message when none is available —
// that's just "no local db running yet". If TEST_DATABASE_URL is set
// explicitly, a connection failure fails the test instead: the caller named
// a specific target, so silently skipping would hide a real, expected-to-work
// setup being broken.
func NewDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	dsn, explicit := testDSN()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// A single physical connection keeps the per-session search_path (set
	// below) in effect for every query the test issues.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		if explicit {
			t.Fatalf("TEST_DATABASE_URL=%s is set but unreachable: %v", dsn, err)
		}
		t.Skipf("no reachable Postgres at %s (start it with `docker compose up -d db`): %v", dsn, err)
	}

	schemaName := fmt.Sprintf("test_%d_%d", os.Getpid(), rand.Int63())
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %q", schemaName)); err != nil {
		db.Close()
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("SET search_path TO %q", schemaName)); err != nil {
		db.Close()
		t.Fatalf("set search_path: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA %q CASCADE", schemaName))
		db.Close()
	})

	return db
}
