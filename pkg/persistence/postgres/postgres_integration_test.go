package postgres_test

// This file holds the round-trip integration tests for the postgres store.
// They use pkg/dbtest's shared testcontainers harness so the heavy
// container spin-up happens once per binary instead of once per test.
//
// Tests that need access to the postgres package's unexported symbols
// (the migrations runner, the internal adapter types, the pool) live in
// postgres_test.go, which is package postgres and uses its own minimal
// requirePostgres helper.

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/dbtest"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// TestEvalStoreRoundTrip exercises SaveInteraction + ListInteractions
// against a tenant-scoped DB. The eval roster is the same shape the
// orchestrator writes after every case (see PR #12).
func TestEvalStoreRoundTrip(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	rec := eval.InteractionRecord{
		ID:        "interaction_test_rt1",
		Scenario:  "synthetic_round_trip",
		Success:   true,
		Metrics:   map[string]float64{"accuracy": 0.95},
		Metadata:  map[string]any{"notes": "synthetic"},
		StartedAt: time.Now().UTC().Truncate(time.Second),
		EndedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := db.Store.SaveInteraction(ctx, db.TenantID, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := db.Store.ListInteractions(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "interaction_test_rt1" || !got[0].Success {
		t.Fatalf("unexpected records: %+v", got)
	}
	if got[0].Metrics["accuracy"] != 0.95 {
		t.Fatalf("metrics not preserved: %+v", got[0].Metrics)
	}
}

// TestPatientRegistryRoundTrip exercises Upsert + Get + FilterByCondition
// + Delete on patient_records.
func TestPatientRegistryRoundTrip(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	p := medical.PatientRecord{
		PatientID:  "patient_test_rt1",
		Age:        61,
		Sex:        "F",
		Conditions: []string{"E11.9", "I10"},
		LastA1c:    7.4,
	}
	if err := db.Store.UpsertPatient(ctx, db.TenantID, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, ok, err := db.Store.GetPatient(ctx, db.TenantID, "patient_test_rt1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Age != 61 || got.Sex != "F" {
		t.Fatalf("payload not preserved: %+v", got)
	}

	by, err := db.Store.FilterPatientsByCondition(ctx, db.TenantID, "E11.9")
	if err != nil || len(by) != 1 {
		t.Fatalf("filter: n=%d err=%v", len(by), err)
	}

	if err := db.Store.DeletePatient(ctx, db.TenantID, "patient_test_rt1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := db.Store.GetPatient(ctx, db.TenantID, "patient_test_rt1"); ok {
		t.Fatalf("delete did not remove row")
	}
}

// TestTenantIsolation is the BYPASSRLS sanity check + the cross-tenant
// invisibility check. Each tenant gets its OWN dbtest.DB so they truly are
// distinct RLS scopes. Mirrors the pre-refactor TestTenantIsolation in
// postgres_test.go.
func TestTenantIsolation(t *testing.T) {
	dbA := dbtest.NewDB(t)
	dbB := dbtest.NewDB(t)

	// The two synthetic tenants must be distinct — that's what RLS scopes by.
	if dbA.TenantID == dbB.TenantID {
		t.Fatalf("dbtest: two NewDB calls returned the same tenant: %s", dbA.TenantID)
	}

	// Sanity check that the app role is NOT bypassing RLS — if this fails,
	// every isolation assertion below is a false positive.
	dbtest.AssertNoBypassRLS(t, dbA)

	ctx := context.Background()
	const sharedID = "patient_test_shared"

	if err := dbA.Store.UpsertPatient(ctx, dbA.TenantID, medical.PatientRecord{
		PatientID: sharedID,
		Age:       30,
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := dbB.Store.UpsertPatient(ctx, dbB.TenantID, medical.PatientRecord{
		PatientID: sharedID,
		Age:       40,
	}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	a, err := dbA.Store.ListPatients(ctx, dbA.TenantID)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	b, err := dbB.Store.ListPatients(ctx, dbB.TenantID)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(a) != 1 || a[0].Age != 30 {
		t.Fatalf("tenant A leaked: %+v", a)
	}
	if len(b) != 1 || b[0].Age != 40 {
		t.Fatalf("tenant B leaked: %+v", b)
	}

	// Cross-tenant lookup: A asks for the same ID, must see its own row.
	aRec, ok, err := dbA.Store.GetPatient(ctx, dbA.TenantID, sharedID)
	if err != nil {
		t.Fatalf("cross get: %v", err)
	}
	if !ok || aRec.Age != 30 {
		t.Fatalf("tenant A saw the wrong row: ok=%v rec=%+v", ok, aRec)
	}

	// Eval records should also be tenant-scoped.
	_ = dbA.Store.SaveInteraction(ctx, dbA.TenantID, eval.InteractionRecord{
		ID: "interaction_test_iso", Success: true,
		StartedAt: time.Now(), EndedAt: time.Now(),
	})
	recsB, err := dbB.Store.ListInteractions(ctx, dbB.TenantID)
	if err != nil {
		t.Fatalf("list interactions B: %v", err)
	}
	if len(recsB) != 0 {
		t.Fatalf("tenant B saw tenant A interactions: %+v", recsB)
	}
}

// TestAuditAppendOnly exercises RecordAuditEvent + ListAuditEvents under a
// tenant scope. The append-only invariant at the GRANT level is enforced by
// the migration; this test just confirms the round-trip succeeds.
func TestAuditAppendOnly(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	ev := audit.Event{
		ID:        "event_test_append1",
		At:        time.Now().UTC(),
		Kind:      audit.KindMessage,
		AgentID:   "diagnostician",
		PatientID: "patient_test_append1",
		CaseID:    "case_test_append1",
	}
	if err := db.Store.RecordAuditEvent(ctx, db.TenantID, ev); err != nil {
		t.Fatalf("record: %v", err)
	}
	out, err := db.Store.ListAuditEvents(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 1 || out[0].ID != "event_test_append1" {
		t.Fatalf("event not persisted: %+v", out)
	}
}

// TestSeed_TableDriven exercises the dbtest table-driven helper end-to-end
// with the synthetic seed. Demonstrates the pattern documented in
// docs/testing.md.
func TestSeed_TableDriven(t *testing.T) {
	db := dbtest.NewDB(t)
	dbtest.SeedPatientsOnly(t, db)

	tbl := []dbtest.Table{
		{
			Name:    "diabetes_code",
			ExpResp: 1,
			ExcFunc: func(ctx context.Context) any {
				xs, _ := db.Store.FilterPatientsByCondition(ctx, db.TenantID, "E11.9")
				return len(xs)
			},
			CmpFunc: cmpInt,
		},
		{
			Name:    "copd_code",
			ExpResp: 1,
			ExcFunc: func(ctx context.Context) any {
				xs, _ := db.Store.FilterPatientsByCondition(ctx, db.TenantID, "J44.9")
				return len(xs)
			},
			CmpFunc: cmpInt,
		},
		{
			Name:    "unknown_code",
			ExpResp: 0,
			ExcFunc: func(ctx context.Context) any {
				xs, _ := db.Store.FilterPatientsByCondition(ctx, db.TenantID, "Z99.99")
				return len(xs)
			},
			CmpFunc: cmpInt,
		},
	}
	dbtest.Run(t, tbl, "TestSeed_TableDriven")
}

func cmpInt(got any, exp any) string {
	if got == exp {
		return ""
	}
	return "mismatch"
}
