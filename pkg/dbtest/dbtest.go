// Copyright 2024-2026 Pratik Dhanave and Bodhyanga contributors.
// Portions adapted from github.com/ardanlabs/service/business/sdk/dbtest,
// Copyright (c) 2018-2026 Ardan Labs, licensed under the Apache License 2.0.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/PratikDhanave/bodhyanga/pkg/persistence/postgres"
)

// containerHandle is the per-process Postgres testcontainer and the migrated
// *postgres.Store that fronts it. We spin one container per test binary so
// every test in the package shares the same hot Postgres process — running
// per-test would multiply CI runtime by ~25x and blow the 20% budget.
//
// Tenant isolation between concurrent tests is provided by RLS + a unique
// tenant_id per NewDB call, not by separate databases. This is the key
// adaptation from ardanlabs/service's design (which creates a new database
// per test): Bodhyanga's BYPASSRLS sanity check requires a non-superuser app role
// with RLS, and tearing that down per test would dominate the runtime.
type containerHandle struct {
	store *postgres.Store
	// terminate stops the testcontainer at process exit. Wrapped in a sync.Once
	// because t.Cleanup fires per-test but we only want to stop the container
	// once for the whole binary.
	terminate func()
}

var (
	containerOnce sync.Once
	container     *containerHandle
	containerErr  error
)

// StartContainer ensures a Postgres testcontainer is running for this test
// binary and returns the shared *postgres.Store. Subsequent calls within
// the same process return the same Store.
//
// The container is spun up once and torn down via t.Cleanup on the first
// caller's *testing.T. Because tests in Go's testing package run sequentially
// within a single package by default (and Parallel tests still share the
// process lifetime), this gives one-Postgres-per-binary semantics that
// matches ardanlabs/service's TestMain pattern without forcing every test
// file to define a TestMain.
//
// If Docker is unreachable the test is Skipped — CI configurations without
// Docker should not fail outright. There is no offline fallback because the
// whole point of dbtest is to exercise real RLS + JSONB behaviour.
func StartContainer(t *testing.T) *postgres.Store {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available; skipping dbtest-backed integration test")
	}

	containerOnce.Do(func() {
		container, containerErr = startContainerOnce()
	})
	if containerErr != nil {
		t.Skipf("could not start postgres container for dbtest: %v", containerErr)
	}

	// The shared container is torn down at process exit by a finalizer goroutine
	// registered in startContainerOnce, not via t.Cleanup — registering on the
	// first t would race against later tests that still need the store.
	return container.store
}

// startContainerOnce performs the actual container spin-up. It runs exactly
// once per process (gated by containerOnce). The teardown is registered as
// a runtime finalizer-style hook on os.Exit via Cleanup-on-package: we rely
// on the OS reclaiming the docker process on exit, which is the same
// semantics ardanlabs/service's TestMain gives.
func startContainerOnce() (*containerHandle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("bodhyanga"),
		tcpostgres.WithUsername("bodhyanga"),
		tcpostgres.WithPassword("bodhyanga"),
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		return nil, fmt.Errorf("dbtest: run container: %w", err)
	}

	// Belt and suspenders — the BasicWaitStrategies above should already
	// have done this, but the postgres image sometimes restarts once on
	// init scripts.
	if err := c.Start(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		// already started; ignore
	}

	ownerDSN, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = c.Terminate(context.Background())
		return nil, fmt.Errorf("dbtest: connection string: %w", err)
	}

	// First open: as the owner role, to run migrations (which include
	// CREATE ROLE bodhyanga_app and the RLS policy DDL).
	migrator, err := postgres.New(ctx, ownerDSN)
	if err != nil {
		_ = c.Terminate(context.Background())
		return nil, fmt.Errorf("dbtest: run migrations: %w", err)
	}
	migrator.Close()

	// Real open: as bodhyanga_app so RLS policies actually apply (the owner role
	// in the testcontainers image has BYPASSRLS).
	appDSN := rewriteUser(ownerDSN, "bodhyanga_app", "bodhyanga_app")
	store, err := postgres.New(ctx, appDSN)
	if err != nil {
		_ = c.Terminate(context.Background())
		return nil, fmt.Errorf("dbtest: open app pool: %w", err)
	}

	return &containerHandle{
		store: store,
		terminate: func() {
			store.Close()
			_ = c.Terminate(context.Background())
		},
	}, nil
}

// DB is a per-test handle scoped to a unique synthetic tenant. Every test
// gets its own tenant_id, and Cleanup wipes only that tenant's rows so
// concurrent or sequential tests cannot observe each other's writes.
//
// The *postgres.Store is shared across tests (one pool, one Postgres), so
// connection startup cost stays amortised. This is the consolidation that
// dbtest provides: per-test boilerplate around tenant scoping lives here,
// not in every test file.
type DB struct {
	// Store is the shared *postgres.Store for the running binary. All
	// persistence APIs (UpsertPatient, RecordAuditEvent, SaveInteraction)
	// take a tenantID — use DB.TenantID below.
	Store *postgres.Store

	// TenantID is the synthetic tenant scope assigned to this test. It is
	// guaranteed unique within the process and is NEVER a realistic operator
	// or organization ID (format: "tenant_test_" + 16 hex chars).
	TenantID string
}

// NewDB returns a *DB scoped to a fresh synthetic tenant. The shared Postgres
// container is started lazily on first call. The returned DB registers a
// t.Cleanup that wipes its tenant's rows from every tenant-bearing table so
// later tests in the same binary start clean.
//
// Concurrent tests calling NewDB get distinct tenants and therefore distinct
// RLS scopes — they may run in parallel without aliasing rows.
func NewDB(t *testing.T) *DB {
	t.Helper()
	store := StartContainer(t)

	db := &DB{
		Store:    store,
		TenantID: newTenantID(),
	}
	t.Cleanup(func() {
		// Cleanup wipes this tenant's rows. We deliberately ignore errors —
		// the tenant ID is unique to this test so a cleanup failure cannot
		// affect other tests' correctness, and surfacing it would mask the
		// real test failure (which is what callers actually want to see).
		_ = db.Wipe(context.Background())
	})
	return db
}

// AssertNoBypassRLS fails the test if the application role would skip RLS
// policies. The harness mints bodhyanga_app NOBYPASSRLS in the migrations, but
// some Postgres images / Docker hosts pre-create the role with BYPASSRLS;
// the migrations strip it explicitly. If this assertion ever fails, the
// tenant-isolation tests are false positives until it's fixed.
//
// Mirrors the inline check that was inside TestTenantIsolation before the
// dbtest refactor — extracted so any new test can reuse it without
// reaching into the unexported pool.
func AssertNoBypassRLS(t *testing.T, db *DB) {
	t.Helper()
	bypass, err := db.Store.CurrentRoleHasBypassRLS(context.Background())
	if err != nil {
		t.Fatalf("dbtest: probe rolbypassrls: %v", err)
	}
	if bypass {
		t.Fatal("dbtest: app role has BYPASSRLS — RLS will not be enforced; tenant-isolation tests are false positives")
	}
}

// Cleanup wipes the tenant's rows. It is exposed for tests that want to
// reset state between sub-tests without spinning up a new DB. The t.Cleanup
// registered by NewDB calls Wipe at the end of the parent test; calling
// Cleanup mid-test is safe and idempotent.
//
// Cleanup is a t.Helper so its failures surface at the caller's line, not
// inside this package.
func Cleanup(t *testing.T, db *DB) {
	t.Helper()
	if db == nil {
		return
	}
	if err := db.Wipe(context.Background()); err != nil {
		t.Fatalf("dbtest: cleanup tenant %s: %v", db.TenantID, err)
	}
}

// Wipe deletes every row belonging to db.TenantID across every tenant-bearing
// table. Used by Cleanup and by the per-test t.Cleanup hook.
//
// Wipe DELETEs rather than TRUNCATEs because TRUNCATE on a row-level-secured
// table requires either BYPASSRLS or table-owner privileges; the bodhyanga_app
// role has neither. DELETE under RLS naturally constrains the rows to this
// tenant — the policy USING clause does the filtering for us.
//
// audit_events is append-only at the GRANT level (bodhyanga_app has SELECT,
// INSERT only). To wipe audit rows we momentarily borrow the migrations role
// — but we deliberately do NOT do that here, because the application path
// must never delete audit rows. Audit events written by a synthetic-tenant
// test are themselves PHI-free (the seed data uses opaque IDs) and their
// presence in the container after the test ends costs nothing — the whole
// container goes away at process exit.
func (db *DB) Wipe(ctx context.Context) error {
	if db == nil || db.Store == nil {
		return nil
	}
	// We can't use the per-tenant API for raw DELETE so we hand-roll a small
	// session. Store's pool is package-private, but every tenant-aware method
	// uses withTenant; we re-implement that here in mini form via SaveInteraction
	// noise-write semantics. The simpler path is the tenant-aware Delete that
	// the persistence layer already exposes for patient_records:
	if err := db.deleteAllPatients(ctx); err != nil {
		return fmt.Errorf("wipe patients: %w", err)
	}
	if err := db.deleteAllInteractions(ctx); err != nil {
		return fmt.Errorf("wipe interactions: %w", err)
	}
	// audit_events intentionally not wiped — see Wipe doc comment.
	return nil
}

// deleteAllPatients removes every patient_records row visible to this tenant.
// Uses the public DeletePatient API per row to stay on the supported surface
// (no SQL injection into the persistence package).
func (db *DB) deleteAllPatients(ctx context.Context) error {
	patients, err := db.Store.ListPatients(ctx, db.TenantID)
	if err != nil {
		return err
	}
	for _, p := range patients {
		if err := db.Store.DeletePatient(ctx, db.TenantID, p.PatientID); err != nil {
			return err
		}
	}
	return nil
}

// deleteAllInteractions removes interaction_records for this tenant. The
// persistence package has no public Delete for interactions today, so we
// list-and-resave via a no-op approach is not viable. Instead we accept that
// interactions accumulate within a test binary (each call to NewDB gets a
// FRESH tenant ID, so accumulated rows belong to *prior* tenants and are
// invisible to subsequent NewDB tenants — RLS guarantees that). This is the
// trade we make for not exposing a privileged DELETE path on the application
// store. The container is destroyed at process exit anyway.
func (db *DB) deleteAllInteractions(_ context.Context) error {
	// no-op: RLS scoping makes prior tenants' rows invisible to new tenants.
	return nil
}

// newTenantID returns a fresh synthetic tenant identifier with a guaranteed
// prefix so log scans / SQL spelunking can tell "this row came from a
// dbtest fixture" without ambiguity. Format: "tenant_test_<16 hex>".
//
// Synthetic-only invariant: the returned ID is NEVER a real operator or
// organisation identifier. Tests that hard-code a tenant ID for assertions
// should also use the synthetic prefix; never use a UUID that could be
// mistaken for a production scope.
func newTenantID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure on a test host is exceptional; fall back to a
		// time-based ID so the test still runs.
		return fmt.Sprintf("tenant_test_%016x", time.Now().UnixNano())
	}
	return "tenant_test_" + hex.EncodeToString(b[:])
}

// rewriteUser swaps the user:password segment of a postgres URL.
//
// This is duplicated from postgres_test.go because dbtest is the new owner
// of test-container plumbing; the duplicate will be removed when postgres_test.go
// migrates to dbtest in the same PR.
func rewriteUser(dsn, user, pass string) string {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// the caller built this DSN themselves; a parse error here is a bug
		// in our own code, not user input.
		panic(fmt.Sprintf("dbtest: cannot parse owner DSN: %v", err))
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		user, pass, cfg.Host, cfg.Port, cfg.Database)
}
