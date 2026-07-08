// Copyright 2025 The Bodh Authors
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
	"io/fs"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/PratikDhanave/bodh/pkg/sqldb"
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

// TestEmbeddedFS_LoadsCanonicalSet checks that the bodh-canonical
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
		tcpostgres.WithDatabase("bodh"),
		tcpostgres.WithUsername("bodh"),
		tcpostgres.WithPassword("bodh"),
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

// ---------------------------------------------------------------------------
// Load – additional edge-case tests
// ---------------------------------------------------------------------------

// TestLoad_EmptyDirectory covers the case where the migration directory
// exists but contains no .sql files. Load should return an empty slice
// and nil error — this is a valid (if unusual) state.
func TestLoad_EmptyDirectory(t *testing.T) {
	empty := fstest.MapFS{}
	got, err := Load(empty)
	if err != nil {
		t.Fatalf("Load on empty dir: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 files, got %d", len(got))
	}
}

// TestLoad_SkipsNonSQLFilesAndDirectories checks that Load silently
// ignores directories and non-.sql files in the migration root.
func TestLoad_SkipsNonSQLFilesAndDirectories(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		"README.md":     &fstest.MapFile{Data: []byte("# docs")},
		"notes.txt":     &fstest.MapFile{Data: []byte("ignore me")},
		"subdir":        &fstest.MapFile{Mode: fs.ModeDir},
	}
	got, err := Load(mfs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 file, got %d", len(got))
	}
	if got[0].Name != "0001_init.sql" {
		t.Errorf("unexpected file: %s", got[0].Name)
	}
}

// TestLoad_DescriptionParsing verifies that the description field is
// extracted from the filename with underscores replaced by spaces.
func TestLoad_DescriptionParsing(t *testing.T) {
	mfs := fstest.MapFS{
		"0042_add_patient_records.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t();")},
	}
	got, err := Load(mfs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 file, got %d", len(got))
	}
	if got[0].Description != "add patient records" {
		t.Errorf("description=%q want %q", got[0].Description, "add patient records")
	}
	if got[0].Version != 42 {
		t.Errorf("version=%d want 42", got[0].Version)
	}
}

// TestLoad_ReadDirError covers the fs.ReadDir failure path (line 102-104).
func TestLoad_ReadDirError(t *testing.T) {
	_, err := Load(&brokenReadDirFS{})
	if err == nil {
		t.Fatal("expected error from ReadDir failure, got nil")
	}
	if !strings.Contains(err.Error(), "read dir") {
		t.Errorf("error should mention 'read dir', got: %v", err)
	}
}

// TestLoad_ReadFileError covers the fs.ReadFile failure path (line 124-126).
func TestLoad_ReadFileError(t *testing.T) {
	_, err := Load(&brokenReadFileFS{})
	if err == nil {
		t.Fatal("expected error from ReadFile failure, got nil")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("error should mention 'read', got: %v", err)
	}
}

// TestLoad_GappedVersions confirms that gaps in version numbers are
// accepted (e.g. 0001, 0003, 0005) — Load only rejects duplicates.
func TestLoad_GappedVersions(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		"0005_b.sql": &fstest.MapFile{Data: []byte("SELECT 5;")},
		"0010_c.sql": &fstest.MapFile{Data: []byte("SELECT 10;")},
	}
	got, err := Load(mfs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 files, got %d", len(got))
	}
	wantVersions := []int{1, 5, 10}
	for i, w := range wantVersions {
		if got[i].Version != w {
			t.Errorf("got[%d].Version=%d want %d", i, got[i].Version, w)
		}
	}
}

// ---------------------------------------------------------------------------
// deterministicID tests
// ---------------------------------------------------------------------------

// TestDeterministicID_Stable checks that the same inputs always
// produce the same output and different inputs produce different ones.
func TestDeterministicID_Stable(t *testing.T) {
	id1 := deterministicID(1, "0001_init.sql")
	id2 := deterministicID(1, "0001_init.sql")
	id3 := deterministicID(2, "0002_patients.sql")

	if id1 != id2 {
		t.Errorf("same inputs produced different IDs: %s vs %s", id1, id2)
	}
	if id1 == id3 {
		t.Errorf("different inputs produced same ID: %s", id1)
	}
	if !strings.HasPrefix(id1, "mig-") {
		t.Errorf("ID should start with 'mig-', got: %s", id1)
	}
}

// ---------------------------------------------------------------------------
// Up – mock-based tests for error paths that don't need a real Postgres
// ---------------------------------------------------------------------------

// TestUp_LoadError confirms that a bad fs.FS causes Up to return an error
// immediately without attempting any DB operations.
func TestUp_LoadError(t *testing.T) {
	_, err := Up(context.Background(), &fakeBeginner{}, &brokenReadDirFS{})
	if err == nil {
		t.Fatal("expected error from Load failure, got nil")
	}
	if !strings.Contains(err.Error(), "read dir") {
		t.Errorf("error should mention 'read dir', got: %v", err)
	}
}

// TestUp_EmptyMigrationSet checks that Up returns (nil, nil) when the
// fs.FS is valid but contains no .sql files.
func TestUp_EmptyMigrationSet(t *testing.T) {
	applied, err := Up(context.Background(), &fakeBeginner{}, fstest.MapFS{})
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if applied != nil {
		t.Fatalf("want nil for empty set, got %v", applied)
	}
}

// TestUp_BeginBootstrapError covers the path where b.Begin fails during
// the bootstrap transaction (line 190-192).
func TestUp_BeginBootstrapError(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &fakeBeginner{beginErr: errors.New("connection refused")}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected begin error, got nil")
	}
	if !strings.Contains(err.Error(), "begin bootstrap") {
		t.Errorf("error should mention 'begin bootstrap', got: %v", err)
	}
}

// TestUp_BootstrapDDLError covers the path where the CREATE TABLE
// schema_migrations fails (line 193-196).
func TestUp_BootstrapDDLError(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &fakeBeginner{
		txFactory: func() *fakeTx {
			return &fakeTx{
				execErr: errors.New("DDL blocked"),
			}
		},
	}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected DDL error, got nil")
	}
	if !strings.Contains(err.Error(), "bootstrap schema_migrations") {
		t.Errorf("error should mention 'bootstrap schema_migrations', got: %v", err)
	}
}

// TestUp_QueryMigrationsError covers the path where SELECT version
// FROM schema_migrations fails (line 199-202).
func TestUp_QueryMigrationsError(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &fakeBeginner{
		txFactory: func() *fakeTx {
			return &fakeTx{
				queryErr: errors.New("query failed"),
			}
		},
	}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected query error, got nil")
	}
	if !strings.Contains(err.Error(), "read schema_migrations") {
		t.Errorf("error should mention 'read schema_migrations', got: %v", err)
	}
}

// TestUp_BootstrapCommitError covers the path where the bootstrap
// transaction commit fails (line 213-215).
func TestUp_BootstrapCommitError(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &fakeBeginner{
		txFactory: func() *fakeTx {
			return &fakeTx{
				commitErr: errors.New("disk full"),
				queryRows: &fakeRows{closed: true}, // empty result set
			}
		},
	}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected commit error, got nil")
	}
	if !strings.Contains(err.Error(), "commit bootstrap") {
		t.Errorf("error should mention 'commit bootstrap', got: %v", err)
	}
}

// TestUp_ScanVersionError covers the rows.Scan failure path during
// bootstrap (lines 205-209).
func TestUp_ScanVersionError(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &fakeBeginner{
		txFactory: func() *fakeTx {
			return &fakeTx{
				queryRows: &fakeRows{
					nextCount: 1,
					scanErr:   errors.New("scan failed"),
				},
			}
		},
	}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
	if !strings.Contains(err.Error(), "scan failed") {
		t.Errorf("error should mention 'scan failed', got: %v", err)
	}
}

// TestUp_ApplyOneBeginError covers the path where b.Begin fails when
// applying an individual migration (line 234-236).
func TestUp_ApplyOneBeginError(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &countingBeginner{
		failAfter: 1, // first Begin (bootstrap) succeeds, second (applyOne) fails
		failErr:   errors.New("connection lost"),
	}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected begin error in applyOne, got nil")
	}
	if !strings.Contains(err.Error(), "begin") {
		t.Errorf("error should mention 'begin', got: %v", err)
	}
}

// TestUp_RecordInsertError covers the path where the INSERT INTO
// schema_migrations fails inside applyOne (line 253-255).
func TestUp_RecordInsertError(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &phasedBeginner{
		phases: []func() (pgx.Tx, error){
			// Phase 0 (bootstrap): succeed with empty rows.
			func() (pgx.Tx, error) {
				return &fakeTx{queryRows: &fakeRows{closed: true}}, nil
			},
			// Phase 1 (applyOne): succeed on first Exec (migration SQL),
			// fail on second Exec (schema_migrations INSERT).
			func() (pgx.Tx, error) {
				return &countingExecTx{
					failOnExec: 2,
					failErr:    errors.New("disk full"),
				}, nil
			},
		},
	}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected record insert error, got nil")
	}
	if !strings.Contains(err.Error(), "record") {
		t.Errorf("error should mention 'record', got: %v", err)
	}
}

// TestUp_AuditInsertError covers the path where the INSERT INTO
// audit_events fails for a non-version-1 migration (line 278-280).
func TestUp_AuditInsertError(t *testing.T) {
	mfs := fstest.MapFS{
		"0002_something.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &phasedBeginner{
		phases: []func() (pgx.Tx, error){
			// Phase 0 (bootstrap): succeed with empty rows.
			func() (pgx.Tx, error) {
				return &fakeTx{queryRows: &fakeRows{closed: true}}, nil
			},
			// Phase 1 (applyOne): succeed on first two Execs (migration
			// SQL and schema_migrations INSERT), fail on third (audit INSERT).
			func() (pgx.Tx, error) {
				return &countingExecTx{
					failOnExec: 3,
					failErr:    errors.New("audit table locked"),
				}, nil
			},
		},
	}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected audit insert error, got nil")
	}
	if !strings.Contains(err.Error(), "audit") {
		t.Errorf("error should mention 'audit', got: %v", err)
	}
}

// TestUp_AuditInsertUndefinedTable_Version1 covers the special-case
// fallthrough where audit_events INSERT fails with ErrUndefinedTable
// on version 1 (line 275-277). The runner tolerates this because 0001
// creates the audit table and inserts its own row.
func TestUp_AuditInsertUndefinedTable_Version1(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	// Create a pgconn.PgError with the undefinedTable code.
	undefinedErr := &pgconn.PgError{
		Code:    "42P01",
		Message: "relation \"audit_events\" does not exist",
	}
	fb := &phasedBeginner{
		phases: []func() (pgx.Tx, error){
			// Phase 0 (bootstrap): succeed with empty rows.
			func() (pgx.Tx, error) {
				return &fakeTx{queryRows: &fakeRows{closed: true}}, nil
			},
			// Phase 1 (applyOne for version 1): succeed on first two
			// Execs, fail on third with undefinedTable error. Since
			// version == 1, the runner should tolerate this and commit.
			func() (pgx.Tx, error) {
				return &countingExecTx{
					failOnExec: 3,
					failErr:    undefinedErr,
				}, nil
			},
		},
	}
	applied, err := Up(context.Background(), fb, mfs)
	if err != nil {
		t.Fatalf("expected success for version-1 undefined table, got: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("want 1 applied, got %d", len(applied))
	}
}

// TestUp_CommitError covers the path where tx.Commit fails inside
// applyOne (line 283-285).
func TestUp_CommitError(t *testing.T) {
	mfs := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	fb := &phasedBeginner{
		phases: []func() (pgx.Tx, error){
			// Phase 0 (bootstrap): succeed.
			func() (pgx.Tx, error) {
				return &fakeTx{queryRows: &fakeRows{closed: true}}, nil
			},
			// Phase 1 (applyOne): all Execs succeed, Commit fails.
			func() (pgx.Tx, error) {
				return &countingExecTx{
					commitErr: errors.New("serialization failure"),
				}, nil
			},
		},
	}
	_, err := Up(context.Background(), fb, mfs)
	if err == nil {
		t.Fatal("expected commit error, got nil")
	}
	if !strings.Contains(err.Error(), "commit") {
		t.Errorf("error should mention 'commit', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Integration tests (require Postgres via testcontainers)
// ---------------------------------------------------------------------------

// TestUp_PartialApply verifies that when one migration out of several
// fails, the successfully-applied ones remain recorded and the failed
// one leaves no trace.
func TestUp_PartialApply(t *testing.T) {
	b := requirePostgres(t)
	ctx := context.Background()

	// Apply canonical set first to bootstrap audit_events.
	if _, err := Up(ctx, b, FS()); err != nil {
		t.Fatalf("Up canonical: %v", err)
	}

	// Two synthetic migrations: one good, one bad.
	mfs := fstest.MapFS{
		"8001_good.sql": &fstest.MapFile{Data: []byte(
			"CREATE TABLE test_partial_good (id int);")},
		"8002_bad.sql": &fstest.MapFile{Data: []byte(
			"INSERT INTO this_table_does_not_exist_999 (x) VALUES (1);")},
	}
	applied, err := Up(ctx, b, mfs)
	if err == nil {
		t.Fatal("expected error for bad migration, got nil")
	}

	// 8001 should have been applied before 8002 failed.
	if len(applied) != 1 {
		t.Fatalf("want 1 applied migration, got %d", len(applied))
	}
	if applied[0].Version != 8001 {
		t.Errorf("applied[0].Version=%d want 8001", applied[0].Version)
	}

	// Verify 8001 is in schema_migrations but 8002 is not.
	pool := b.Pool()
	var exists8001, exists8002 bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=8001)`).Scan(&exists8001); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists8001 {
		t.Fatal("8001 should be in schema_migrations")
	}
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=8002)`).Scan(&exists8002); err != nil {
		t.Fatalf("query: %v", err)
	}
	if exists8002 {
		t.Fatal("8002 should NOT be in schema_migrations")
	}
}

// TestUp_SkipsAlreadyApplied confirms that a migration whose version is
// already in schema_migrations is skipped.
func TestUp_SkipsAlreadyApplied(t *testing.T) {
	b := requirePostgres(t)
	ctx := context.Background()

	// Apply canonical set.
	if _, err := Up(ctx, b, FS()); err != nil {
		t.Fatalf("Up canonical: %v", err)
	}

	// New set that re-uses version 1 (already applied) plus a new one.
	mfs := fstest.MapFS{
		"0001_reused.sql": &fstest.MapFile{Data: []byte("SELECT 'should be skipped';")},
		"7777_new.sql":    &fstest.MapFile{Data: []byte("CREATE TABLE test_skip_7777 (id int);")},
	}
	applied, err := Up(ctx, b, mfs)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	// Only 7777 should be applied; 0001 should be skipped.
	if len(applied) != 1 {
		t.Fatalf("want 1 applied, got %d", len(applied))
	}
	if applied[0].Version != 7777 {
		t.Errorf("applied[0].Version=%d want 7777", applied[0].Version)
	}
}

// TestUp_SQLSyntaxError ensures that a migration with invalid SQL fails
// and the runner reports the offending filename.
func TestUp_SQLSyntaxError(t *testing.T) {
	b := requirePostgres(t)
	ctx := context.Background()

	// Bootstrap.
	if _, err := Up(ctx, b, FS()); err != nil {
		t.Fatalf("Up canonical: %v", err)
	}

	mfs := fstest.MapFS{
		"6666_syntax.sql": &fstest.MapFile{Data: []byte(
			"THIS IS NOT VALID SQL AT ALL;")},
	}
	_, err := Up(ctx, b, mfs)
	if err == nil {
		t.Fatal("expected SQL syntax error, got nil")
	}
	if !strings.Contains(err.Error(), "6666_syntax") {
		t.Errorf("error should reference the migration file, got: %v", err)
	}
}

// TestUp_AuditRowContent verifies that the audit_events rows written
// by the migrator contain the expected kind, agent_id, and reason.
func TestUp_AuditRowContent(t *testing.T) {
	b := requirePostgres(t)
	ctx := context.Background()

	if _, err := Up(ctx, b, FS()); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Add a synthetic migration to check its audit row specifically.
	mfs := fstest.MapFS{
		"5555_audit_check.sql": &fstest.MapFile{Data: []byte(
			"CREATE TABLE test_audit_check (id int);")},
	}
	applied, err := Up(ctx, b, mfs)
	if err != nil {
		t.Fatalf("Up synthetic: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("want 1, got %d", len(applied))
	}

	pool := b.Pool()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := sqldb.SetTenant(ctx, tx, AuditTenant); err != nil {
		t.Fatalf("set tenant: %v", err)
	}

	var kind, agentID, reason string
	expectedID := deterministicID(5555, "5555_audit_check.sql")
	err = tx.QueryRow(ctx,
		`SELECT kind, agent_id, reason FROM audit_events WHERE id=$1 AND tenant_id=$2`,
		expectedID, AuditTenant).Scan(&kind, &agentID, &reason)
	if err != nil {
		t.Fatalf("query audit row: %v", err)
	}
	if kind != "schema_migration" {
		t.Errorf("kind=%q want 'schema_migration'", kind)
	}
	if agentID != "bodh-migrator" {
		t.Errorf("agent_id=%q want 'bodh-migrator'", agentID)
	}
	if !strings.Contains(reason, "audit check") {
		t.Errorf("reason=%q should contain 'audit check'", reason)
	}
}

// ---------------------------------------------------------------------------
// PingedBeginner tests
// ---------------------------------------------------------------------------

// TestPingedBeginner_BadDSN covers the Open error path (line 306-308).
func TestPingedBeginner_BadDSN(t *testing.T) {
	_, err := PingedBeginner(context.Background(), "postgres://invalid:0/nonexistent?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for bad DSN, got nil")
	}
}

// TestPingedBeginner_Success (integration) validates the happy path.
func TestPingedBeginner_Success(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("bodh_pb"),
		tcpostgres.WithUsername("bodh"),
		tcpostgres.WithPassword("bodh"),
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Skipf("could not start postgres: %v", err)
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
	if b == nil {
		t.Fatal("expected non-nil PoolBeginner")
	}
	b.Close()
}

// ---------------------------------------------------------------------------
// FS() tests
// ---------------------------------------------------------------------------

// TestFS_ReturnsNonNil validates that the embedded FS helper returns a
// usable fs.FS. The panic path (FS() line 33) is unreachable in normal
// builds since the embed directive guarantees the "sql" subdirectory
// exists at compile time.
func TestFS_ReturnsNonNil(t *testing.T) {
	f := FS()
	if f == nil {
		t.Fatal("FS() returned nil")
	}
	// Should be able to read at least one file from it.
	entries, err := fs.ReadDir(f, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("FS() returned empty directory")
	}
}

// ---------------------------------------------------------------------------
// Fake / mock types for non-DB tests
// ---------------------------------------------------------------------------

// brokenReadDirFS is an fs.FS whose ReadDir always fails.
type brokenReadDirFS struct{}

func (brokenReadDirFS) Open(name string) (fs.File, error) {
	return nil, errors.New("broken: open")
}

// brokenReadFileFS is an fs.FS that lists a valid .sql file but fails
// when that file is read.
type brokenReadFileFS struct{}

func (brokenReadFileFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &fakeDirFile{entries: []fs.DirEntry{
			fakeDirEntry{name: "0001_init.sql"},
		}}, nil
	}
	return nil, errors.New("broken: read file")
}

// fakeDirFile implements fs.ReadDirFile for brokenReadFileFS.
type fakeDirFile struct {
	entries []fs.DirEntry
	read    bool
}

func (f *fakeDirFile) Stat() (fs.FileInfo, error) {
	return fakeFileInfo{name: ".", isDir: true}, nil
}
func (f *fakeDirFile) Read([]byte) (int, error) { return 0, errors.New("not a regular file") }
func (f *fakeDirFile) Close() error             { return nil }
func (f *fakeDirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.read {
		return nil, nil
	}
	f.read = true
	return f.entries, nil
}

// fakeDirEntry implements fs.DirEntry.
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (e fakeDirEntry) Name() string               { return e.name }
func (e fakeDirEntry) IsDir() bool                { return e.isDir }
func (e fakeDirEntry) Type() fs.FileMode          { return 0 }
func (e fakeDirEntry) Info() (fs.FileInfo, error) { return fakeFileInfo{name: e.name}, nil }

// fakeFileInfo implements fs.FileInfo.
type fakeFileInfo struct {
	name  string
	isDir bool
}

func (i fakeFileInfo) Name() string       { return i.name }
func (i fakeFileInfo) Size() int64        { return 0 }
func (i fakeFileInfo) Mode() fs.FileMode  { return 0 }
func (i fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (i fakeFileInfo) IsDir() bool        { return i.isDir }
func (i fakeFileInfo) Sys() any           { return nil }

// fakeBeginner implements sqldb.Beginner for testing error paths that
// don't need a real Postgres connection.
type fakeBeginner struct {
	beginErr      error
	beginErrAfter int // fail Begin after this many successful calls
	txFactory     func() *fakeTx
	callCount     int
}

func (fb *fakeBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	fb.callCount++
	if fb.beginErrAfter > 0 && fb.callCount > fb.beginErrAfter {
		return nil, fb.beginErr
	}
	if fb.beginErr != nil && fb.beginErrAfter == 0 {
		return nil, fb.beginErr
	}
	if fb.txFactory != nil {
		tx := fb.txFactory()
		if tx == nil {
			return nil, errors.New("fake: begin refused")
		}
		return tx, nil
	}
	return &fakeTx{}, nil
}

// fakeTx implements pgx.Tx with configurable failures.
type fakeTx struct {
	pgx.Tx    // embed to satisfy the interface; unused methods panic
	execErr   error
	execCount int
	queryErr  error
	queryRows *fakeRows
	commitErr error
}

func (tx *fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.execCount++
	if tx.execErr != nil {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (tx *fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.queryErr != nil {
		return nil, tx.queryErr
	}
	if tx.queryRows != nil {
		return tx.queryRows, nil
	}
	return &fakeRows{closed: true}, nil
}

func (tx *fakeTx) Commit(ctx context.Context) error {
	return tx.commitErr
}

func (tx *fakeTx) Rollback(ctx context.Context) error {
	return nil
}

// fakeRows implements pgx.Rows with configurable Next/Scan behavior.
type fakeRows struct {
	pgx.Rows  // embed; unused methods panic
	nextCount int
	scanErr   error
	callCount int
	closed    bool
}

func (r *fakeRows) Next() bool {
	if r.closed {
		return false
	}
	r.callCount++
	return r.callCount <= r.nextCount
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	// Populate a single int scan target with 0.
	if len(dest) > 0 {
		if p, ok := dest[0].(*int); ok {
			*p = 0
		}
	}
	return nil
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }

// countingBeginner is a Beginner that returns a working fakeTx for
// the first `failAfter` calls and then returns failErr. This lets
// tests exercise code paths where the bootstrap transaction succeeds
// but a subsequent Begin (e.g. inside applyOne) fails.
type countingBeginner struct {
	failAfter int
	failErr   error
	calls     int
}

func (cb *countingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	cb.calls++
	if cb.calls > cb.failAfter {
		return nil, cb.failErr
	}
	return &fakeTx{
		queryRows: &fakeRows{closed: true},
	}, nil
}

// phasedBeginner returns a different tx (or error) on each successive
// Begin call, driven by an ordered list of factory functions. This
// lets tests script the bootstrap phase separately from the applyOne
// phase.
type phasedBeginner struct {
	phases []func() (pgx.Tx, error)
	calls  int
}

func (pb *phasedBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	idx := pb.calls
	pb.calls++
	if idx < len(pb.phases) {
		return pb.phases[idx]()
	}
	return nil, errors.New("phasedBeginner: no more phases")
}

// countingExecTx is a pgx.Tx fake that counts Exec calls and fails
// on a specific one. This lets tests target the schema_migrations
// INSERT (2nd Exec) or the audit_events INSERT (3rd Exec) independently.
type countingExecTx struct {
	pgx.Tx
	failOnExec int // 0 means never fail on exec
	failErr    error
	commitErr  error
	execCalls  int
}

func (tx *countingExecTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	if tx.failOnExec > 0 && tx.execCalls == tx.failOnExec {
		return pgconn.CommandTag{}, tx.failErr
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (tx *countingExecTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return &fakeRows{closed: true}, nil
}

func (tx *countingExecTx) Commit(ctx context.Context) error {
	return tx.commitErr
}

func (tx *countingExecTx) Rollback(ctx context.Context) error {
	return nil
}

// Compile-time check that the migrate package's File slice satisfies
// the simple interface the runner advertises in its doc comments.
var _ = fmt.Sprintf
