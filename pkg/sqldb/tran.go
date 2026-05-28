// Copyright 2025 The Bodh Authors
// Portions Copyright Ardan Labs, Bill Kennedy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Adapted from github.com/ardanlabs/service business/sdk/sqldb/tran.go.
// The original wraps sqlx.DB.Beginx; Bodh wraps pgxpool.Pool.Begin and
// returns pgx.Tx instead so the rest of the codebase keeps using
// pgx.Tx.QueryRow / pgx.Tx.Exec directly.

package sqldb

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Beginner is the seam that lets a transaction-aware function accept
// either a pool or a wrapped *pgxpool.Pool. Domain stores depend on
// this interface, not on *pgxpool.Pool, so tests can substitute a
// fake.
//
// This mirrors the upstream Beginner interface; we return pgx.Tx
// instead of a CommitRollbacker because pgx.Tx already exposes Commit
// and Rollback with the right ctx-aware signatures.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PoolBeginner adapts *pgxpool.Pool to Beginner. The wrapping is a
// no-op at runtime; the indirection exists so a store can hold a
// Beginner field instead of a *pgxpool.Pool and gain test seams.
type PoolBeginner struct {
	pool *pgxpool.Pool
}

// NewBeginner wraps a pool. Returns nil if pool is nil so callers
// (cmd/care defers Close on the result) don't have to handle the
// degenerate case.
func NewBeginner(pool *pgxpool.Pool) *PoolBeginner {
	if pool == nil {
		return nil
	}
	return &PoolBeginner{pool: pool}
}

// Begin starts a new transaction on the pool. The caller is
// responsible for Commit/Rollback. Use WithTx for the common case
// where the caller wants the package to handle the lifecycle.
func (b *PoolBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	if b == nil || b.pool == nil {
		return nil, errors.New("sqldb: nil beginner")
	}
	return b.pool.Begin(ctx)
}

// Pool exposes the underlying pgxpool.Pool for callers (e.g. the
// migrator) that need to acquire a single dedicated connection.
func (b *PoolBeginner) Pool() *pgxpool.Pool {
	if b == nil {
		return nil
	}
	return b.pool
}

// Close releases the underlying pool. Idempotent and nil-safe so
// cmd/care can defer it unconditionally.
func (b *PoolBeginner) Close() {
	if b == nil || b.pool == nil {
		return
	}
	b.pool.Close()
	b.pool = nil
}

// WithTx runs fn inside a transaction begun on b. On success the
// transaction commits and the result of fn is returned; on any
// non-nil return the transaction rolls back. ROLLBACK is also
// triggered on a panic so a bug in fn cannot leak an open
// transaction back to the pool.
//
// This is the seam pkg/persistence/postgres uses to compose a row
// mutation with an audit_events append: both writes happen inside fn,
// so a failed audit append rolls back the row mutation too. That is
// the HIPAA §164.312(b) "atomic mutation + audit" invariant the
// task description requires.
//
// Caller responsibility: fn must use the supplied pgx.Tx for every
// statement. Reaching back to the pool inside fn would open a second
// connection that bypasses this transaction's atomicity guarantees.
func WithTx(ctx context.Context, b Beginner, fn func(pgx.Tx) error) (err error) {
	if b == nil {
		return errors.New("sqldb: nil beginner")
	}
	tx, err := b.Begin(ctx)
	if err != nil {
		return fmt.Errorf("sqldb: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			// Best-effort rollback then re-panic. We never swallow a
			// panic — the caller's caller (likely the orchestrator)
			// has its own recover that audits the failure.
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			// Rollback's error is intentionally swallowed: the
			// original err is what the caller cares about, and
			// rollback after a real failure may itself fail because
			// the transaction is already aborted.
			_ = tx.Rollback(ctx)
			return
		}
		// Happy path: commit and surface any commit error.
		if cerr := tx.Commit(ctx); cerr != nil {
			err = fmt.Errorf("sqldb: commit: %w", cerr)
		}
	}()
	return fn(tx)
}

// SetTenant binds the Postgres session variable app.tenant_id for
// the duration of tx. Every domain store in Bodh that touches
// PHI-bearing tables calls this immediately after Begin so the RLS
// policies in db/migrations/0002_rls.sql can scope rows by tenant.
//
// This used to live as an inline SELECT set_config(...) call in
// pkg/persistence/postgres; lifting it here means future stores reuse
// the helper instead of re-implementing the bind.
//
// set_config(name, value, is_local=true) is equivalent to SET LOCAL
// but accepts a parameter, which lets us bind tenantID safely.
// is_local=true scopes the change to the surrounding transaction so
// the next pool-checkout sees a clean session — critical because
// pgxpool reuses connections across requests.
func SetTenant(ctx context.Context, tx pgx.Tx, tenantID string) error {
	if tenantID == "" {
		return errors.New("sqldb: tenantID required")
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return fmt.Errorf("sqldb: set tenant: %w", err)
	}
	return nil
}
