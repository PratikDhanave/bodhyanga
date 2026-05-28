// Copyright 2025 The Bodh Authors
// Portions Copyright Ardan Labs, Bill Kennedy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Adapted from github.com/ardanlabs/service business/sdk/sqldb/sqldb.go.
// The original wraps github.com/jmoiron/sqlx; Bodh prefers pgx/v5 directly
// because the rest of the persistence layer already depends on
// pgxpool.Pool and because pgx exposes the pgconn.PgError code path we
// use to recognise unique-violation / undefined-table failure modes
// without going through sqlx's reflection layer.

// Package sqldb provides the Bodh-wide Postgres primitives: connection
// pool open + ping, status check, the transactor interface that lets a
// caller compose multiple writes (row mutation + audit row append) under
// one COMMIT/ROLLBACK barrier, and the typed error sentinels every
// domain store maps DB-level failures into.
//
// Why this package exists
//
// pkg/persistence/postgres holds Bodh's first store (eval / patient /
// audit) and inlines its own pool open + tenant-scoped transaction
// helper. As we add more domain stores (cases, reviews, ...), every one
// of them needs the same primitives: open a pool, run a status check on
// boot, begin a transaction the caller can extend with multiple writes,
// surface unique-violation as a typed error.
//
// pkg/sqldb is that reusable seam. pkg/persistence/postgres adopts it
// in this PR (see pkg/persistence/postgres/store.go); future stores can
// depend on pkg/sqldb without re-implementing pool + tx boilerplate.
//
// What this package does NOT do
//
//   - It does NOT speak the RLS tenant contract. That lives in the
//     domain stores (every query SET LOCAL app.tenant_id). The pool
//     opened here is just plumbing; do not call pool.Exec from outside
//     a store unless you know your statement is tenant-safe.
//   - It does NOT run migrations. pkg/migrate is the migrator; this
//     package only opens the connection the migrator runs on.
//   - It does NOT log queries or attach OTel spans. The upstream
//     ardanlabs/service file does both via foundation/logger + a custom
//     otel.AddSpan; Bodh runs through pkg/observability instead and
//     attaches spans at the domain-store boundary, not here, to keep
//     this package small and dep-free.
package sqldb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres SQLSTATE codes we surface as typed sentinels. The full list
// lives at https://www.postgresql.org/docs/current/errcodes-appendix.html
// — we only name the codes domain stores actually branch on so the
// alias surface stays narrow.
const (
	// uniqueViolation maps to ErrDuplicatedEntry. Callers branch on
	// this when an upsert race causes a PK / UNIQUE collision.
	uniqueViolation = "23505"
	// undefinedTable maps to ErrUndefinedTable. The bootstrap path
	// (first run before migrations apply) is the only legitimate
	// caller — every other branch is a real bug.
	undefinedTable = "42P01"
)

// Sentinel errors. Stores wrap these with %w so callers can use
// errors.Is to branch.
var (
	// ErrDuplicatedEntry is returned when a write fails because a
	// row with the same key already exists. The orchestrator surfaces
	// this as HTTP 409 in cmd/care; tests assert on the typed value
	// instead of string-matching the pgx error.
	ErrDuplicatedEntry = errors.New("sqldb: duplicated entry")

	// ErrUndefinedTable is returned when a query references a table
	// that does not exist. This is almost always a pre-migration boot
	// race — Bodh runs migrations on startup, so callers see this
	// only inside tests that intentionally exercise a fresh DB.
	ErrUndefinedTable = errors.New("sqldb: undefined table")
)

// Config is the operator-facing knob set for opening a pool. Mirrors
// the surface of ardanlabs/service Config; we keep the same field names
// so an operator who knows the upstream template recognises this one.
type Config struct {
	// User / Password / Host / Name / Schema build the DSN when DSN
	// is empty. When DSN is non-empty the parsed values take over and
	// these fields are ignored.
	User     string
	Password string
	Host     string
	Name     string
	Schema   string

	// DSN is the raw pgx connection URL. When set it short-circuits
	// the User/Password/Host/Name/Schema fields. cmd/care prefers
	// DSN-from-env (BODH_DB_DSN) because operators paste full URLs.
	DSN string

	// MaxConns caps the pool size. Defaults to 8 (matches the
	// previous hard-coded value in pkg/persistence/postgres).
	MaxConns int32

	// DisableTLS removes sslmode from the DSN. Default false. Tests
	// flip this on for testcontainers postgres which doesn't have a
	// cert.
	DisableTLS bool
}

// Open returns a pgxpool.Pool ready for use. The pool is configured
// but NOT pinged; call StatusCheck before treating it as live so a
// caller can decide whether to fail boot or retry.
//
// Open is the only function in this package that constructs a pool;
// the rest of Bodh receives the *pgxpool.Pool and uses it directly.
// We do NOT wrap the pool in a Bodh type because pgxpool.Pool already
// exposes the right surface (Begin, Acquire, Query, Exec) and wrapping
// it would force every domain store to either reach through the
// wrapper or break the pgx.Tx contract.
func Open(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	dsn := cfg.DSN
	if dsn == "" {
		dsn = buildDSN(cfg)
	}
	if dsn == "" {
		return nil, errors.New("sqldb: empty DSN and incomplete Config")
	}
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("sqldb: parse dsn: %w", err)
	}
	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	} else if pcfg.MaxConns == 0 {
		pcfg.MaxConns = 8
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("sqldb: open pool: %w", err)
	}
	return pool, nil
}

// StatusCheck pings the pool. Mirrors the upstream behaviour: if the
// caller didn't pass a deadline we set a 1-second one so a busted DB
// fails fast on boot rather than blocking startup forever. On retry
// we back off linearly (100ms × attempt) until the context's deadline
// fires.
//
// Unlike the upstream we additionally execute "SELECT TRUE" once the
// ping succeeds — this catches the case where pgxpool returned a
// pooled-but-broken connection (the pool's health check sometimes
// hands out a connection whose underlying TCP is half-closed).
func StatusCheck(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("sqldb: nil pool")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Second)
		defer cancel()
	}

	for attempts := 1; ; attempts++ {
		if err := pool.Ping(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempts) * 100 * time.Millisecond):
		}
	}

	// Round-trip "SELECT TRUE" — pool.Ping is a connection-level probe;
	// this also exercises the wire.
	var tmp bool
	if err := pool.QueryRow(ctx, `SELECT TRUE`).Scan(&tmp); err != nil {
		return fmt.Errorf("sqldb: status check query: %w", err)
	}
	return nil
}

// ClassifyError maps a pgx/pg error to one of the typed sentinels.
// Returns the original error untouched when no match is found.
//
// Domain stores call this once at their statement boundary so the
// surface returned to handlers is the small Err* set defined above
// rather than the raw *pgconn.PgError. Keeping the mapping in one
// place is the reason this package exists.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case uniqueViolation:
		return fmt.Errorf("%w: %s", ErrDuplicatedEntry, pgErr.Message)
	case undefinedTable:
		return fmt.Errorf("%w: %s", ErrUndefinedTable, pgErr.Message)
	}
	return err
}

// buildDSN constructs a postgres URL from Config fields. The order
// (sslmode → timezone → search_path) matches the upstream so an
// operator comparing pasted DSNs sees the same shape.
func buildDSN(cfg Config) string {
	if cfg.User == "" || cfg.Host == "" || cfg.Name == "" {
		return ""
	}
	ssl := "require"
	if cfg.DisableTLS {
		ssl = "disable"
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s&timezone=utc",
		cfg.User, cfg.Password, cfg.Host, cfg.Name, ssl)
	if cfg.Schema != "" {
		dsn += "&search_path=" + cfg.Schema
	}
	return dsn
}
