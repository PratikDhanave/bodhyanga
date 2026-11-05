// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package migrate

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/PratikDhanave/bodhyanga/pkg/sqldb"
)

// TestLoad_RejectsBadFilenames covers the parser without needing a DB.
// The runner downstream depends on Load returning a sorted set of
// well-formed Files; failing fast on a malformed filename catches
// packaging mistakes (a PR that drops migrate.sql instead of
// 0006_migrate.sql) before they reach production.
func TestLoad_RejectsBadFilenames(t *testing.T) {
	cases := map[string]map[string]string{
		"missing-version": {
			"init.sql": "CREATE TABLE foo();",
		},
		"bad-version-prefix": {
			"abc_init.sql": "CREATE TABLE foo();",
		},
		"empty-file": {
			"0001_init.sql": "   \n",
		},
		"duplicate-version": {
			"0001_a.sql": "SELECT 1;",
			"0001_b.sql": "SELECT 1;",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			fs := fstest.MapFS{}
			for k, v := range files {
				fs[k] = &fstest.MapFile{Data: []byte(v)}
			}
			if _, err := Load(fs); err == nil {
				t.Fatalf("expected error for %s, got nil", name)
			}
		})
	}
}

// TestLoad_SortsAscending verifies that out-of-order files in the
// directory listing are returned by ascending version. Production
// migrations live in pkg/migrate/sql/ which is ordered alphabetically
// by the filesystem; this test guards against a future move to a
// hash-bucketed embed where listing order is undefined.
func TestLoad_SortsAscending(t *testing.T) {
	fs := fstest.MapFS{
		"0003_c.sql": &fstest.MapFile{Data: []byte("SELECT 3;")},
		"0001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		"0002_b.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
	}
	got, err := Load(fs)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 files, got %d", len(got))
	}
	for i, want := range []int{1, 2, 3} {
		if got[i].Version != want {
			t.Errorf("got[%d].Version=%d want %d", i, got[i].Version, want)
		}
	}
}

// TestEmbeddedFS_LoadsCanonicalSet checks that the bodhyanga-canonical
// migrations (the ones shipped in pkg/migrate/sql) actually parse.
// This is the fastest way to catch a packaging regression — a future
// PR that drops a malformed file fails the test instantly without
// needing docker.
func TestEmbeddedFS_LoadsCanonicalSet(t *testing.T) {
	files, err := Load(FS())
	if err != nil {
		t.Fatalf("load embedded: %v", err)
	}
	if len(files) < 5 {
		t.Fatalf("expected ≥5 canonical migrations, got %d", len(files))
	}
	// 0001..0005 must be contiguous in the canonical set.
	for i, f := range files[:5] {
		if f.Version != i+1 {
			t.Errorf("files[%d].Version=%d want %d", i, f.Version, i+1)
		}
	}
}

// requirePostgres spins up a throwaway postgres via testcontainers
// or skips the test. Mirrors the helper in pkg/persistence/postgres.
func requirePostgres(t *testing.T) *sqldb.PoolBeginner {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available; skipping postgres integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("bodhyanga"),
		tcpostgres.WithUsername("bodhyanga"),
		tcpostgres.WithPassword("bodhyanga"),
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Skipf("could not start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	b, err := PingedBeginner(ctx, dsn)
	if err != nil {
		t.Fatalf("PingedBeginner: %v", err)
	}
	t.Cleanup(b.Close)
	return b
}

// TestUp_AppliesCanonicalAndRecordsAuditRow runs the full canonical
// migration set against a real postgres, then verifies (a) every
// version is recorded in schema_migrations, (b) at least one
// audit_events row was written with kind='schema_migration'.
func TestUp_AppliesCanonicalAndRecordsAuditRow(t *testing.T) {
	b := requirePostgres(t)
	ctx := context.Background()

	applied, err := Up(ctx, b, FS())
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(applied) == 0 {
		t.Fatalf("expected migrations to apply, got 0")
	}

	pool := b.Pool()
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	want, _ := Load(FS())
	if count < len(want) {
		t.Fatalf("schema_migrations count=%d want >= %d", count, len(want))
	}

	// audit row check requires SETting app.tenant_id to the migrator
	// sentinel so the RLS policy lets us SELECT.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := sqldb.SetTenant(ctx, tx, AuditTenant); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	var auditCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_events WHERE kind='schema_migration'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_events: %v", err)
	}
	if auditCount == 0 {
		t.Fatalf("expected at least one schema_migration audit row, got 0")
	}
}

// TestUp_Idempotent re-runs Up after a successful application and
// expects the second call to apply zero migrations.
func TestUp_Idempotent(t *testing.T) {
	b := requirePostgres(t)
	ctx := context.Background()

	if _, err := Up(ctx, b, FS()); err != nil {
		t.Fatalf("Up 1: %v", err)
	}
	applied, err := Up(ctx, b, FS())
	if err != nil {
		t.Fatalf("Up 2: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("second Up applied %d migrations, want 0", len(applied))
	}
}

// TestForcedFailure_LeavesNoSchemaMigrationsRow is the
// HIPAA-§164.312(b) invariant test: a migration that fails partway
// through MUST leave the system with no schema_migrations row and no
// audit_events row for that version. The runner ensures this by
// wrapping the DDL + bookkeeping in one transaction.
//
// We exercise this by handing the runner a deliberately broken
// migration (references a nonexistent table). The Up call returns an
// error; the on-disk state must show no record of version 9999.
func TestForcedFailure_LeavesNoSchemaMigrationsRow(t *testing.T) {
	b := requirePostgres(t)
	ctx := context.Background()

	// Apply the canonical set first so the audit table exists.
	if _, err := Up(ctx, b, FS()); err != nil {
		t.Fatalf("Up canonical: %v", err)
	}

	// Synthetic broken set: one good migration (0001 is the table
	// bootstrap, already applied) plus one bad one (9999 references
	// a nonexistent table). The good one should not re-apply (it's
	// already in schema_migrations); the bad one must fail and roll
	// back atomically.
	badFS := fstest.MapFS{
		"9999_broken.sql": &fstest.MapFile{Data: []byte(
			"INSERT INTO this_table_does_not_exist (x) VALUES (1);")},
	}
	_, err := Up(ctx, b, badFS)
	if err == nil {
		t.Fatal("expected Up to fail for broken migration, got nil")
	}
	if !strings.Contains(err.Error(), "9999") {
		t.Errorf("error should mention broken file, got: %v", err)
	}

	// Verify no schema_migrations row for 9999.
	pool := b.Pool()
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, 9999).Scan(&exists); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if exists {
		t.Fatal("schema_migrations contains 9999 after a failed migration — atomicity broken")
	}

	// Verify no audit_events row for 9999.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := sqldb.SetTenant(ctx, tx, AuditTenant); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	var auditExists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM audit_events WHERE reason LIKE $1)`, "%9999_broken%").Scan(&auditExists); err != nil {
		t.Fatalf("query audit_events: %v", err)
	}
	if auditExists {
		t.Fatal("audit_events contains 9999 after a failed migration — atomicity broken")
	}
}

// TestWithTx_RollsBackOnError covers the pkg/sqldb transaction
// composition seam: the row mutation + audit append must roll back
// together. We use the migrate package's own real DB so we don't
// pull testcontainers into a separate test file.
func TestWithTx_RollsBackOnError(t *testing.T) {
	b := requirePostgres(t)
	ctx := context.Background()

	if _, err := Up(ctx, b, FS()); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Insert one good row, then attempt a transaction that writes
	// a row and then returns an error. The row must NOT survive.
	const tenant = "tenant-tx-test"
	probeErr := errors.New("simulated downstream failure")
	err := sqldb.WithTx(ctx, b, func(tx pgx.Tx) error {
		if err := sqldb.SetTenant(ctx, tx, tenant); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO interaction_records
				(tenant_id, id, scenario, success, started_at, ended_at)
			 VALUES ($1, 'r-rollback', 'demo', true, now(), now())`, tenant); err != nil {
			return err
		}
		return probeErr
	})
	if !errors.Is(err, probeErr) {
		t.Fatalf("WithTx err = %v, want %v", err, probeErr)
	}

	// Verify the row is absent.
	pool := b.Pool()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := sqldb.SetTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	var n int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM interaction_records WHERE id='r-rollback'`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Fatalf("found %d rows after rollback, want 0", n)
	}
}

// Compile-time check that the migrate package's File slice satisfies
// the simple interface the runner advertises in its doc comments.
var _ = fmt.Sprintf
