// Copyright 2025 The Bodh Authors
// Portions Copyright Ardan Labs, Bill Kennedy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Adapted from github.com/ardanlabs/service business/sdk/migrate/migrate.go.
// The upstream delegates to ardanlabs/darwin; Bodh implements the
// numeric-prefix migration runner inline because (a) we want to stay
// free of the darwin dep, (b) we need to wire a per-migration
// audit_events row that darwin doesn't model, and (c) the runner
// already exists in pkg/persistence/postgres/migrations.go — this
// package generalises it.

// Package migrate is the Bodh schema migration runner. It reads SQL
// files from an embed.FS, applies any whose NNNN version exceeds the
// highest recorded in schema_migrations, and writes an audit_events
// row per applied migration so the schema change itself is auditable.
//
// Why a per-migration audit_events row
//
// HIPAA §164.312(b) requires "hardware, software, and procedural
// mechanisms that record and examine activity in information systems
// that contain or use electronic protected health information." The
// schema itself is part of that information system; a deployment
// that DDL'd a new column onto patient_records must leave a trail
// for the forensic reconstruction §164.404 contemplates. Writing the
// audit_events row inside the same transaction as the migration
// statements means a failed migration leaves NO schema_migrations
// row AND NO audit_events row — the rollback is atomic. See the
// failure-rollback test in migrate_test.go.
//
// File-naming contract
//
//   - Files live at the root of the embed.FS (no "migrations/"
//     prefix is required; pass the fs.FS rooted where you want).
//   - Each file is named NNNN_description.sql where NNNN is a
//     zero-padded integer. The integer is the version; gaps are OK
//     but discouraged; duplicates are rejected at load time.
//   - The description is free-form and is recorded in
//     audit_events.reason.
//
// # Up vs the previous in-package migrator
//
// pkg/persistence/postgres has its own migration runner today
// (migrations.go). Up is the more general version: it accepts any
// fs.FS so callers (cmd/care, cmd/bodhadmin, tests) can pass either
// the Bodh-canonical migrations or a test-local one. The existing
// runner stays in place to keep pkg/persistence/postgres compiling
// while consumers move over.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/PratikDhanave/bodh/pkg/sqldb"
)

// File is one parsed migration. Exported for tests that want to
// assert on the loaded set without re-implementing the parser.
type File struct {
	// Version is the numeric NNNN prefix. Two migrations may not
	// share a version (Load fails fast if they do).
	Version int

	// Name is the full filename — e.g. "0001_init.sql".
	Name string

	// Description is the underscore-separated portion between the
	// version prefix and the .sql suffix, with underscores converted
	// to spaces. Recorded verbatim in audit_events.reason so an
	// investigator can grep for "patients" without first looking up
	// what 0002 is.
	Description string

	// SQL is the file body. Applied as a single Exec inside a
	// transaction — multi-statement is fine (pgx batches them).
	SQL string
}

// Load reads every "*.sql" file from f and returns them sorted by
// Version ascending. Returns an error on duplicates, on a malformed
// filename, or on an empty file (an empty migration is almost always
// a packaging bug; better to fail loud at boot than confuse a future
// developer who diffs schema_migrations against the file list).
func Load(f fs.FS) ([]File, error) {
	entries, err := fs.ReadDir(f, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: read dir: %w", err)
	}
	out := make([]File, 0, len(entries))
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(e.Name(), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migrate: %s: filename must be NNNN_description.sql", e.Name())
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("migrate: %s: bad version prefix: %w", e.Name(), err)
		}
		if prev, ok := seen[v]; ok {
			return nil, fmt.Errorf("migrate: duplicate version %d in %s and %s", v, prev, e.Name())
		}
		seen[v] = e.Name()
		body, err := fs.ReadFile(f, e.Name())
		if err != nil {
			return nil, fmt.Errorf("migrate: read %s: %w", e.Name(), err)
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			return nil, fmt.Errorf("migrate: %s is empty", e.Name())
		}
		desc := strings.TrimSuffix(parts[1], ".sql")
		desc = strings.ReplaceAll(desc, "_", " ")
		out = append(out, File{
			Version:     v,
			Name:        e.Name(),
			Description: desc,
			SQL:         string(body),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// AuditTenant is the tenant_id Bodh stamps on migration audit_events
// rows. Migrations are operator actions, not patient actions; we use a
// reserved sentinel rather than co-opting a real tenant ID so the
// audit log can be filtered (kind='migration' OR tenant_id='_bodh_system')
// without leaking schema-change records into a real tenant's view.
const AuditTenant = "_bodh_system"

// auditKind labels the migration audit rows. Matches the discipline
// in pkg/audit (string kinds, grep-able). Not exported through pkg/audit
// because the audit package intentionally enumerates only the kinds the
// orchestrator emits; schema-change is an operations-layer event, not
// an agent-loop one.
const auditKind = "schema_migration"

// Up applies any pending migrations from src against pool. Returns
// the list of migrations actually applied; an empty slice when the
// schema was already up to date.
//
// Semantics
//
//   - Bootstraps schema_migrations on first run.
//   - Each migration runs in its own transaction. The transaction
//     applies the migration's SQL, inserts the schema_migrations row,
//     and inserts the audit_events row. ALL THREE succeed or ALL
//     THREE roll back — that is the §164.312(b) invariant tested by
//     TestForcedFailure_LeavesNoSchemaMigrationsRow.
//   - audit_events is the same table the production audit log uses;
//     the BYPASSRLS owner role that runs migrations is responsible
//     for writing under AuditTenant. RLS policies in
//     db/migrations/0002_rls.sql let the migrator INSERT regardless
//     of tenant when it has BYPASSRLS, which is the production
//     migration role's posture.
//   - Idempotent. Re-running is a no-op once every version is
//     recorded.
func Up(ctx context.Context, b sqldb.Beginner, src fs.FS) ([]File, error) {
	files, err := Load(src)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Bootstrap + read applied set on its own transaction. The audit
	// table is created here too — once that first migration lands,
	// every subsequent run can write its audit row.
	tx, err := b.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: begin bootstrap: %w", err)
	}
	if _, err := tx.Exec(ctx, schemaMigrationsDDL); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("migrate: bootstrap schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := tx.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("migrate: read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			_ = tx.Rollback(ctx)
			return nil, err
		}
		applied[v] = true
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("migrate: commit bootstrap: %w", err)
	}

	out := make([]File, 0, len(files))
	for _, m := range files {
		if applied[m.Version] {
			continue
		}
		if err := applyOne(ctx, b, m); err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, nil
}

// applyOne runs one migration inside a transaction. All three writes
// (DDL, schema_migrations, audit_events) succeed or none do.
func applyOne(ctx context.Context, b sqldb.Beginner, m File) error {
	tx, err := b.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrate: begin %s: %w", m.Name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("migrate: apply %s: %w", m.Name, sqldb.ClassifyError(err))
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		INSERT INTO schema_migrations (version, applied_at)
		VALUES ($1, $2)
		ON CONFLICT (version) DO NOTHING
	`, m.Version, now); err != nil {
		return fmt.Errorf("migrate: record %s: %w", m.Name, err)
	}

	// audit_events row. The ID is deterministic — a sha256 of the
	// migration version + filename — so re-running the same
	// migration against a fresh DB produces the same audit ID. That
	// is useful for replay tooling and for cross-environment
	// reconciliation, and matches the existing audit.Event practice
	// of letting the caller supply a stable ID.
	auditID := deterministicID(m.Version, m.Name)
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events
			(tenant_id, id, at, kind, agent_id, reason)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, id) DO NOTHING
	`, AuditTenant, auditID, now, auditKind, "bodh-migrator", m.Name+": "+m.Description); err != nil {
		// If audit_events does not yet exist (we're applying 0001
		// which creates it), the INSERT can't run. We tolerate this
		// for the very first migration only — every subsequent
		// migration is expected to have audit_events available.
		// Detect via the typed sentinel from sqldb.ClassifyError.
		if errors.Is(sqldb.ClassifyError(err), sqldb.ErrUndefinedTable) && m.Version == 1 {
			// Fall through to commit; the audit row for 0001 is
			// inserted by 0001 itself (see pkg/migrate/sql/0001_init.sql).
		} else {
			return fmt.Errorf("migrate: audit %s: %w", m.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrate: commit %s: %w", m.Name, err)
	}
	committed = true
	return nil
}

// deterministicID derives a stable audit ID from the migration
// identity. Not a security primitive — just a way to make replays
// deterministic. The "mig-" prefix lets investigators grep audit IDs
// and group schema-change events.
func deterministicID(version int, name string) string {
	h := sha256.New()
	fmt.Fprintf(h, "bodh-migrate-v1|%d|%s", version, name)
	sum := h.Sum(nil)
	return "mig-" + hex.EncodeToString(sum[:8])
}

// PingedBeginner is a small convenience: open + StatusCheck +
// NewBeginner in one call, used by cmd/care and cmd/bodhadmin.
// Callers who want finer control wire the three calls themselves.
func PingedBeginner(ctx context.Context, dsn string) (*sqldb.PoolBeginner, error) {
	pool, err := sqldb.Open(ctx, sqldb.Config{DSN: dsn})
	if err != nil {
		return nil, err
	}
	if err := sqldb.StatusCheck(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return sqldb.NewBeginner(pool), nil
}

// schemaMigrationsDDL bootstraps the bookkeeping table. Idempotent.
// Kept inline (rather than in a 0000_bootstrap.sql) because Up needs
// this table to exist before it can even read the applied-set.
const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// guard against the linter complaining when pgx is only used for its
// types in callers of this package.
var _ pgx.Tx = (pgx.Tx)(nil)
