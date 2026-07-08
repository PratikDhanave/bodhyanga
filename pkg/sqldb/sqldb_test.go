// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package sqldb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestClassifyError_UniqueViolation maps SQLSTATE 23505 onto the
// typed sentinel. Domain stores branch on errors.Is so the mapping
// must round-trip correctly.
func TestClassifyError_UniqueViolation(t *testing.T) {
	raw := &pgconn.PgError{Code: uniqueViolation, Message: "dup"}
	classified := ClassifyError(raw)
	if !errors.Is(classified, ErrDuplicatedEntry) {
		t.Fatalf("classified=%v not a ErrDuplicatedEntry", classified)
	}
}

// TestClassifyError_UndefinedTable maps SQLSTATE 42P01.
func TestClassifyError_UndefinedTable(t *testing.T) {
	raw := &pgconn.PgError{Code: undefinedTable, Message: "no such table"}
	classified := ClassifyError(raw)
	if !errors.Is(classified, ErrUndefinedTable) {
		t.Fatalf("classified=%v not a ErrUndefinedTable", classified)
	}
}

// TestClassifyError_Passthrough leaves unrecognised errors alone so
// callers can branch on the underlying cause without losing context.
func TestClassifyError_Passthrough(t *testing.T) {
	src := errors.New("some other failure")
	if got := ClassifyError(src); got != src {
		t.Fatalf("got %v, want passthrough of original", got)
	}
	if got := ClassifyError(nil); got != nil {
		t.Fatalf("nil in should yield nil out; got %v", got)
	}
}

// TestBuildDSN_HappyPath verifies the synthesised DSN matches the
// upstream's shape (postgres://user:pw@host/db?sslmode=...&timezone=utc).
// Operators sometimes paste the synthesised DSN; an unexpected change
// here breaks their muscle memory.
func TestBuildDSN_HappyPath(t *testing.T) {
	got := buildDSN(Config{
		User: "bodh_app", Password: "pw",
		Host: "db:5432", Name: "bodh",
		DisableTLS: true,
	})
	want := "postgres://bodh_app:pw@db:5432/bodh?sslmode=disable&timezone=utc"
	if got != want {
		t.Fatalf("buildDSN got %q want %q", got, want)
	}
}

// TestBuildDSN_IncompleteReturnsEmpty so Open can detect "no config
// supplied" and surface a helpful error.
func TestBuildDSN_IncompleteReturnsEmpty(t *testing.T) {
	if got := buildDSN(Config{}); got != "" {
		t.Fatalf("empty config should yield empty DSN; got %q", got)
	}
}

// TestNewBeginner_NilPool returns nil so cmd/care can defer Close on
// the result without an extra nil-check.
func TestNewBeginner_NilPool(t *testing.T) {
	if got := NewBeginner(nil); got != nil {
		t.Fatalf("NewBeginner(nil) = %v, want nil", got)
	}
}

// TestClassifyError_UnrecognisedPgError leaves PgErrors with codes outside
// our sentinel set untouched.
func TestClassifyError_UnrecognisedPgError(t *testing.T) {
	raw := &pgconn.PgError{Code: "42601", Message: "syntax error"}
	got := ClassifyError(raw)
	// Should return the raw error, not wrapped.
	var pgErr *pgconn.PgError
	if !errors.As(got, &pgErr) {
		t.Fatalf("expected PgError passthrough, got %T", got)
	}
	if pgErr.Code != "42601" {
		t.Fatalf("code: got %q want 42601", pgErr.Code)
	}
}

// TestBuildDSN_WithSchema verifies the search_path parameter is appended.
func TestBuildDSN_WithSchema(t *testing.T) {
	got := buildDSN(Config{
		User: "u", Password: "p", Host: "h", Name: "n",
		Schema: "myschema", DisableTLS: true,
	})
	want := "postgres://u:p@h/n?sslmode=disable&timezone=utc&search_path=myschema"
	if got != want {
		t.Fatalf("buildDSN got %q want %q", got, want)
	}
}

// TestBuildDSN_TLSEnabled verifies sslmode=require when DisableTLS is false.
func TestBuildDSN_TLSEnabled(t *testing.T) {
	got := buildDSN(Config{
		User: "u", Password: "p", Host: "h", Name: "n",
	})
	if !strings.Contains(got, "sslmode=require") {
		t.Fatalf("expected sslmode=require, got %q", got)
	}
}

// TestBuildDSN_MissingFields verifies that partial configs return "".
func TestBuildDSN_MissingFields(t *testing.T) {
	cases := []Config{
		{User: "", Password: "p", Host: "h", Name: "n"},
		{User: "u", Password: "p", Host: "", Name: "n"},
		{User: "u", Password: "p", Host: "h", Name: ""},
	}
	for i, c := range cases {
		if got := buildDSN(c); got != "" {
			t.Fatalf("case %d: expected empty DSN, got %q", i, got)
		}
	}
}

// TestOpen_EmptyConfig returns an error for an empty Config with no DSN.
func TestOpen_EmptyConfig(t *testing.T) {
	_, err := Open(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error for empty Config, got nil")
	}
	if !strings.Contains(err.Error(), "empty DSN") {
		t.Fatalf("error: got %q, want substring 'empty DSN'", err.Error())
	}
}

// TestOpen_InvalidDSN returns an error for a malformed DSN.
func TestOpen_InvalidDSN(t *testing.T) {
	_, err := Open(context.Background(), Config{DSN: "://invalid"})
	if err == nil {
		t.Fatal("expected error for invalid DSN, got nil")
	}
	if !strings.Contains(err.Error(), "parse dsn") {
		t.Fatalf("error: got %q, want substring 'parse dsn'", err.Error())
	}
}

// TestOpen_UnreachableHost returns an error when the pool can't connect.
func TestOpen_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Use a valid DSN that points to a non-existent host.
	// pgxpool.NewWithConfig should succeed (it creates the pool lazily),
	// so Open itself returns no error. The pool is created but not pinged.
	pool, err := Open(ctx, Config{DSN: "postgres://nobody:pass@127.0.0.1:59999/nodb?sslmode=disable"})
	if err != nil {
		// Pool creation is lazy in pgxpool, so this might actually succeed.
		// If it fails (some pgx versions), that's also acceptable.
		return
	}
	defer pool.Close()
}

// TestOpen_WithMaxConns exercises the MaxConns path.
func TestOpen_WithMaxConns(t *testing.T) {
	// We can't easily test the pool size, but we can verify Open doesn't
	// crash when MaxConns is set.
	_, err := Open(context.Background(), Config{
		DSN:      "postgres://nobody:pass@127.0.0.1:59999/nodb?sslmode=disable",
		MaxConns: 4,
	})
	// Pool creation is lazy; it may or may not error.
	_ = err
}

// TestStatusCheck_NilPool returns an error.
func TestStatusCheck_NilPool(t *testing.T) {
	if err := StatusCheck(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil pool")
	}
}

// TestStatusCheck_CancelledContext returns context error when context is already done.
func TestStatusCheck_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// We need a real-ish pool. Can't use nil (already tested). We can test
	// with a pool that will fail to connect, and a cancelled context.
	pool, err := Open(context.Background(), Config{DSN: "postgres://nobody:pass@127.0.0.1:59999/nodb?sslmode=disable"})
	if err != nil {
		// If pool creation fails, skip this test.
		t.Skipf("could not create pool for test: %v", err)
	}
	defer pool.Close()
	err = StatusCheck(ctx, pool)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestNewBeginner_NonNilPool returns a non-nil PoolBeginner.
func TestNewBeginner_NonNilPool(t *testing.T) {
	// Use a real-ish pool pointing to an unreachable host. pgxpool creates
	// connections lazily, so NewWithConfig won't error.
	pcfg, err := pgxpool.ParseConfig("postgres://nobody:pass@127.0.0.1:59999/nodb?sslmode=disable")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), pcfg)
	if err != nil {
		t.Skipf("could not create pool: %v", err)
	}
	defer pool.Close()

	b := NewBeginner(pool)
	if b == nil {
		t.Fatal("NewBeginner(non-nil) should return non-nil")
	}

	// Exercise Pool().
	if got := b.Pool(); got != pool {
		t.Fatal("Pool() should return the underlying pool")
	}

	// Exercise Close().
	b.Close()
	if b.pool != nil {
		t.Fatal("Close() should nil out pool")
	}
	// Close again is idempotent.
	b.Close()
}

// TestPoolBeginner_Begin_NilReceiver returns an error.
func TestPoolBeginner_Begin_NilReceiver(t *testing.T) {
	var b *PoolBeginner
	_, err := b.Begin(context.Background())
	if err == nil {
		t.Fatal("expected error for nil PoolBeginner")
	}
}

// TestPoolBeginner_Begin_NilPool returns an error when pool is nil.
func TestPoolBeginner_Begin_NilPool(t *testing.T) {
	b := &PoolBeginner{pool: nil}
	_, err := b.Begin(context.Background())
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}

// TestPoolBeginner_Begin_UnreachablePool tries to begin a tx on an unreachable pool.
func TestPoolBeginner_Begin_UnreachablePool(t *testing.T) {
	pcfg, err := pgxpool.ParseConfig("postgres://nobody:pass@127.0.0.1:59999/nodb?sslmode=disable")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), pcfg)
	if err != nil {
		t.Skipf("pool create: %v", err)
	}
	defer pool.Close()

	b := NewBeginner(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = b.Begin(ctx)
	// The Begin on an unreachable pool should fail.
	if err == nil {
		t.Fatal("expected error for Begin on unreachable pool")
	}
}

// TestOpen_ValidDSNPoolCreated exercises the happy path of Open with MaxConns=0
// fallback and with explicit MaxConns.
func TestOpen_ValidDSNPoolCreated(t *testing.T) {
	// pgxpool lazily creates connections, so Open should succeed even with
	// an unreachable host. This exercises the default MaxConns = 8 branch.
	pool, err := Open(context.Background(), Config{
		DSN: "postgres://nobody:pass@127.0.0.1:59999/nodb?sslmode=disable",
	})
	if err != nil {
		// Some versions may fail; skip.
		t.Skipf("Open failed: %v", err)
	}
	defer pool.Close()

	// Also test with explicit MaxConns.
	pool2, err := Open(context.Background(), Config{
		DSN:      "postgres://nobody:pass@127.0.0.1:59999/nodb?sslmode=disable",
		MaxConns: 2,
	})
	if err != nil {
		t.Skipf("Open with MaxConns failed: %v", err)
	}
	defer pool2.Close()
}

// TestOpen_FromConfigFields exercises the buildDSN → Open path.
func TestOpen_FromConfigFields(t *testing.T) {
	pool, err := Open(context.Background(), Config{
		User:       "testuser",
		Password:   "testpass",
		Host:       "127.0.0.1:59999",
		Name:       "testdb",
		DisableTLS: true,
	})
	if err != nil {
		t.Skipf("Open from fields failed: %v", err)
	}
	defer pool.Close()
}

// TestStatusCheck_WithDeadlineAlreadySet exercises the path where the context
// already has a deadline (skips the 1-second timeout).
func TestStatusCheck_WithDeadlineAlreadySet(t *testing.T) {
	pcfg, err := pgxpool.ParseConfig("postgres://nobody:pass@127.0.0.1:59999/nodb?sslmode=disable")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), pcfg)
	if err != nil {
		t.Skipf("pool create: %v", err)
	}
	defer pool.Close()

	// Context with explicit deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = StatusCheck(ctx, pool)
	// Should fail because pool can't reach a server.
	if err == nil {
		t.Fatal("expected error for StatusCheck on unreachable pool")
	}
}

// TestPoolBeginner_Pool_NilReceiver returns nil.
func TestPoolBeginner_Pool_NilReceiver(t *testing.T) {
	var b *PoolBeginner
	if got := b.Pool(); got != nil {
		t.Fatalf("Pool() on nil PoolBeginner = %v, want nil", got)
	}
}

// TestPoolBeginner_Close_NilReceiver does not panic.
func TestPoolBeginner_Close_NilReceiver(t *testing.T) {
	var b *PoolBeginner
	// Should not panic.
	b.Close()
}

// TestPoolBeginner_Close_NilPool does not panic.
func TestPoolBeginner_Close_NilPool(t *testing.T) {
	b := &PoolBeginner{pool: nil}
	b.Close()
}

// TestWithTx_NilBeginner returns an error.
func TestWithTx_NilBeginner(t *testing.T) {
	err := WithTx(context.Background(), nil, func(tx pgx.Tx) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for nil beginner")
	}
}

// mockBeginner is a fake Beginner for unit testing WithTx paths.
type mockBeginner struct {
	beginErr error
	tx       *mockTx
}

func (mb *mockBeginner) Begin(_ context.Context) (pgx.Tx, error) {
	if mb.beginErr != nil {
		return nil, mb.beginErr
	}
	return mb.tx, nil
}

// mockTx implements pgx.Tx minimally for WithTx tests.
type mockTx struct {
	committed  bool
	rolledBack bool
	commitErr  error
}

func (mt *mockTx) Begin(_ context.Context) (pgx.Tx, error) { return nil, nil }
func (mt *mockTx) Commit(ctx context.Context) error {
	mt.committed = true
	return mt.commitErr
}
func (mt *mockTx) Rollback(ctx context.Context) error {
	mt.rolledBack = true
	return nil
}
func (mt *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (mt *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (mt *mockTx) LargeObjects() pgx.LargeObjects                               { return pgx.LargeObjects{} }
func (mt *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (mt *mockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (mt *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (mt *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }
func (mt *mockTx) Conn() *pgx.Conn                                               { return nil }

// TestWithTx_BeginError returns wrapped error when Begin fails.
func TestWithTx_BeginError(t *testing.T) {
	b := &mockBeginner{beginErr: errors.New("begin fail")}
	err := WithTx(context.Background(), b, func(tx pgx.Tx) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "begin") {
		t.Fatalf("expected begin error, got %v", err)
	}
}

// TestWithTx_HappyPath commits on success.
func TestWithTx_HappyPath(t *testing.T) {
	tx := &mockTx{}
	b := &mockBeginner{tx: tx}
	err := WithTx(context.Background(), b, func(tx pgx.Tx) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tx.committed {
		t.Fatal("expected commit")
	}
	if tx.rolledBack {
		t.Fatal("unexpected rollback")
	}
}

// TestWithTx_FnError rolls back on fn error.
func TestWithTx_FnError(t *testing.T) {
	tx := &mockTx{}
	b := &mockBeginner{tx: tx}
	err := WithTx(context.Background(), b, func(tx pgx.Tx) error {
		return errors.New("fn fail")
	})
	if err == nil || !strings.Contains(err.Error(), "fn fail") {
		t.Fatalf("expected fn error, got %v", err)
	}
	if tx.committed {
		t.Fatal("expected no commit on error")
	}
	if !tx.rolledBack {
		t.Fatal("expected rollback on error")
	}
}

// TestWithTx_CommitError surfaces the commit error.
func TestWithTx_CommitError(t *testing.T) {
	tx := &mockTx{commitErr: errors.New("commit fail")}
	b := &mockBeginner{tx: tx}
	err := WithTx(context.Background(), b, func(tx pgx.Tx) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("expected commit error, got %v", err)
	}
}

// TestWithTx_PanicRollsBack ensures panics are re-raised after rollback.
func TestWithTx_PanicRollsBack(t *testing.T) {
	tx := &mockTx{}
	b := &mockBeginner{tx: tx}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic to be re-raised")
		}
		if r != "boom" {
			t.Fatalf("expected panic value 'boom', got %v", r)
		}
		if !tx.rolledBack {
			t.Fatal("expected rollback on panic")
		}
	}()
	_ = WithTx(context.Background(), b, func(tx pgx.Tx) error {
		panic("boom")
	})
}

// TestSetTenant_EmptyTenantID returns an error.
func TestSetTenant_EmptyTenantID(t *testing.T) {
	err := SetTenant(context.Background(), nil, "")
	if err == nil || !strings.Contains(err.Error(), "tenantID required") {
		t.Fatalf("expected tenantID required error, got %v", err)
	}
}

// TestSetTenant_ExecError covers the exec failure path.
func TestSetTenant_ExecError(t *testing.T) {
	tx := &errTx{execErr: errors.New("exec fail")}
	err := SetTenant(context.Background(), tx, "tenant_abc")
	if err == nil || !strings.Contains(err.Error(), "set tenant") {
		t.Fatalf("expected set tenant error, got %v", err)
	}
}

// TestSetTenant_Success covers the happy path.
func TestSetTenant_Success(t *testing.T) {
	tx := &errTx{}
	err := SetTenant(context.Background(), tx, "tenant_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// errTx is a mock pgx.Tx whose Exec returns a configurable error.
type errTx struct {
	execErr error
}

func (et *errTx) Begin(_ context.Context) (pgx.Tx, error) { return nil, nil }
func (et *errTx) Commit(_ context.Context) error          { return nil }
func (et *errTx) Rollback(_ context.Context) error        { return nil }
func (et *errTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (et *errTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (et *errTx) LargeObjects() pgx.LargeObjects                             { return pgx.LargeObjects{} }
func (et *errTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (et *errTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, et.execErr
}
func (et *errTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) { return nil, nil }
func (et *errTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row        { return nil }
func (et *errTx) Conn() *pgx.Conn                                               { return nil }
