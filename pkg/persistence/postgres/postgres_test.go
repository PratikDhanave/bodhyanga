package postgres

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// requirePostgres spins up a throwaway Postgres via testcontainers. If Docker
// isn't reachable the test is skipped — CI without Docker shouldn't fail,
// and there is no offline fallback because the whole point is to exercise
// real RLS + JSONB behaviour.
func requirePostgres(t *testing.T) *Store {
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
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	// Belt and suspenders — the BasicWaitStrategies above should already
	// have done this, but the postgres image sometimes restarts once on
	// init scripts.
	if err := container.Start(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		// already started; ignore
	}
	_ = wait.ForLog("database system is ready to accept connections")

	ownerDSN, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	// First open: as the owner role, to run migrations (which include
	// CREATE ROLE bodhyanga_app and the RLS policy DDL).
	migrator, err := New(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("migrator New: %v", err)
	}
	migrator.Close()

	// Real open: as bodhyanga_app so RLS policies actually apply (the owner role
	// in the testcontainers image has BYPASSRLS).
	appDSN := rewriteUser(t, ownerDSN, "bodhyanga_app", "bodhyanga_app")
	store, err := New(ctx, appDSN)
	if err != nil {
		t.Fatalf("app New: %v", err)
	}
	t.Cleanup(store.Close)

	return store
}

// rewriteUser swaps the user:password segment of a postgres URL.
func rewriteUser(t *testing.T, dsn, user, pass string) string {
	t.Helper()
	u, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.User = user
	u.Password = pass
	// Rebuild as URL.
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		user, pass, u.Host, u.Port, u.Database)
}

func TestEvalStoreRoundTrip(t *testing.T) {
	s := requirePostgres(t)
	ctx := context.Background()
	const tenant = "tenant-a"

	rec := eval.InteractionRecord{
		ID:        "r1",
		Scenario:  "demo",
		Success:   true,
		Metrics:   map[string]float64{"accuracy": 0.95},
		Metadata:  map[string]any{"notes": "hello"},
		StartedAt: time.Now().UTC().Truncate(time.Second),
		EndedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SaveInteraction(ctx, tenant, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.ListInteractions(ctx, tenant)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "r1" || !got[0].Success {
		t.Fatalf("unexpected records: %+v", got)
	}
	if got[0].Metrics["accuracy"] != 0.95 {
		t.Fatalf("metrics not preserved: %+v", got[0].Metrics)
	}
}

func TestPatientRegistryRoundTrip(t *testing.T) {
	s := requirePostgres(t)
	ctx := context.Background()
	const tenant = "tenant-a"

	p := medical.PatientRecord{
		PatientID:  "pt-001",
		Age:        61,
		Sex:        "F",
		Conditions: []string{"E11.9", "I10"},
		LastA1c:    7.4,
	}
	if err := s.UpsertPatient(ctx, tenant, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, ok, err := s.GetPatient(ctx, tenant, "pt-001")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Age != 61 || got.Sex != "F" {
		t.Fatalf("payload not preserved: %+v", got)
	}

	by, err := s.FilterPatientsByCondition(ctx, tenant, "E11.9")
	if err != nil || len(by) != 1 {
		t.Fatalf("filter: n=%d err=%v", len(by), err)
	}

	// Hard-delete (GDPR Article 17).
	if err := s.DeletePatient(ctx, tenant, "pt-001"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := s.GetPatient(ctx, tenant, "pt-001"); ok {
		t.Fatalf("delete did not remove row")
	}
}

func TestTenantIsolation(t *testing.T) {
	s := requirePostgres(t)
	ctx := context.Background()

	if err := s.UpsertPatient(ctx, "tenant-a", medical.PatientRecord{
		PatientID: "shared-id",
		Age:       30,
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := s.UpsertPatient(ctx, "tenant-b", medical.PatientRecord{
		PatientID: "shared-id",
		Age:       40,
	}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	// Sanity check that the app connection is NOT bypassing RLS — if this
	// fails, RLS isolation below is a false-positive.
	conn, _ := s.pool.Acquire(ctx)
	var bypass bool
	_ = conn.QueryRow(ctx, `SELECT rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&bypass)
	conn.Release()
	if bypass {
		t.Fatal("app role has BYPASSRLS — RLS will not be enforced")
	}

	a, err := s.ListPatients(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	b, err := s.ListPatients(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(a) != 1 || a[0].Age != 30 {
		t.Fatalf("tenant A leaked: %+v", a)
	}
	if len(b) != 1 || b[0].Age != 40 {
		t.Fatalf("tenant B leaked: %+v", b)
	}

	// Cross-tenant lookup: tenant-a asks for tenant-b's row by same ID and
	// must see its own row (or none) — never tenant-b's.
	aRec, ok, err := s.GetPatient(ctx, "tenant-a", "shared-id")
	if err != nil {
		t.Fatalf("cross get: %v", err)
	}
	if !ok || aRec.Age != 30 {
		t.Fatalf("tenant-a saw the wrong row: ok=%v rec=%+v", ok, aRec)
	}

	// Eval records should also be tenant-scoped.
	_ = s.SaveInteraction(ctx, "tenant-a", eval.InteractionRecord{
		ID: "x", Success: true,
		StartedAt: time.Now(), EndedAt: time.Now(),
	})
	recsB, err := s.ListInteractions(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("list interactions B: %v", err)
	}
	if len(recsB) != 0 {
		t.Fatalf("tenant-b saw tenant-a interactions: %+v", recsB)
	}
}

func TestAuditAppendOnly(t *testing.T) {
	s := requirePostgres(t)
	ctx := context.Background()
	const tenant = "tenant-a"

	ev := audit.Event{
		ID:        "ev-1",
		At:        time.Now().UTC(),
		Kind:      audit.KindMessage,
		AgentID:   "diagnostician",
		PatientID: "pt-1",
		CaseID:    "c-1",
	}
	if err := s.RecordAuditEvent(ctx, tenant, ev); err != nil {
		t.Fatalf("record: %v", err)
	}
	out, err := s.ListAuditEvents(ctx, tenant)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 1 || out[0].ID != "ev-1" {
		t.Fatalf("event not persisted: %+v", out)
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	s := requirePostgres(t)
	ctx := context.Background()

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	// Run migrations again — they should no-op.
	if err := runMigrations(ctx, conn.Conn()); err != nil {
		t.Fatalf("re-run migrations: %v", err)
	}

	// Verify schema_migrations has exactly the embedded version count.
	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != len(migs) {
		t.Fatalf("schema_migrations rows = %d, want %d", count, len(migs))
	}
}

func TestEvalAdapterImplementsInterface(t *testing.T) {
	// Compile-time check that the adapters implement the documented contracts.
	var _ eval.Store = (*evalAdapter)(nil)
	var _ audit.Recorder = (*auditAdapter)(nil)
	// PatientRegistryAdapter does not need to satisfy an interface
	// (medical.PatientRegistry is a struct, not an interface) but must
	// expose the same method set.
	_ = (&PatientRegistryAdapter{}).List
	_ = (&PatientRegistryAdapter{}).Get
	_ = (&PatientRegistryAdapter{}).Upsert
	_ = (&PatientRegistryAdapter{}).FilterByCondition
}

// silence unused-import warnings when the test is skipped.
var _ = pgx.ErrNoRows
