package postgres_test

// coverage_boost_test.go targets functions at 0% coverage and low-coverage
// branches identified by go tool cover -func. All tests use the shared
// dbtest.NewDB harness so they share the per-binary Postgres container.

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/dbtest"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// ---------------------------------------------------------------------------
// Ping (was 0%)
// ---------------------------------------------------------------------------

func TestPing(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	if err := db.Store.Ping(ctx); err != nil {
		t.Fatalf("Ping on live store: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SetDefaultTenant / DefaultTenant (were 0%)
// ---------------------------------------------------------------------------

func TestDefaultTenant(t *testing.T) {
	db := dbtest.NewDB(t)

	// Initially empty.
	if got := db.Store.DefaultTenant(); got != "" {
		t.Fatalf("expected empty default tenant, got %q", got)
	}

	db.Store.SetDefaultTenant("tenant-set-test")
	if got := db.Store.DefaultTenant(); got != "tenant-set-test" {
		t.Fatalf("expected tenant-set-test, got %q", got)
	}

	// Clear it.
	db.Store.SetDefaultTenant("")
	if got := db.Store.DefaultTenant(); got != "" {
		t.Fatalf("expected empty after clear, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// EvalStore adapter (EvalStore, evalAdapter.Save, evalAdapter.List — all 0%)
// ---------------------------------------------------------------------------

func TestEvalStoreAdapter(t *testing.T) {
	db := dbtest.NewDB(t)

	adapter := db.Store.EvalStore(db.TenantID)

	rec := eval.InteractionRecord{
		ID:        "adapter_eval_1",
		Scenario:  "adapter_test",
		Success:   true,
		Metrics:   map[string]float64{"latency_ms": 42},
		Metadata:  map[string]any{"source": "coverage_boost"},
		StartedAt: time.Now().UTC().Truncate(time.Second),
		EndedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := adapter.Save(rec); err != nil {
		t.Fatalf("adapter Save: %v", err)
	}

	got := adapter.List()
	if len(got) != 1 {
		t.Fatalf("adapter List: expected 1 record, got %d", len(got))
	}
	if got[0].ID != "adapter_eval_1" {
		t.Fatalf("adapter List: expected ID adapter_eval_1, got %s", got[0].ID)
	}
	if got[0].Metrics["latency_ms"] != 42 {
		t.Fatalf("adapter List: metrics not preserved: %+v", got[0].Metrics)
	}
}

// ---------------------------------------------------------------------------
// PatientRegistry adapter (PatientRegistry, Upsert, Get, List,
// FilterByCondition — all 0%)
// ---------------------------------------------------------------------------

func TestPatientRegistryAdapter(t *testing.T) {
	db := dbtest.NewDB(t)

	reg := db.Store.PatientRegistry(db.TenantID)

	p := medical.PatientRecord{
		PatientID:  "adapter_pt_1",
		Age:        55,
		Sex:        "M",
		Conditions: []string{"I10", "E11.9"},
		LastA1c:    6.8,
	}
	reg.Upsert(p)

	// Get
	got, ok := reg.Get("adapter_pt_1")
	if !ok {
		t.Fatal("adapter Get: expected ok=true")
	}
	if got.Age != 55 || got.Sex != "M" {
		t.Fatalf("adapter Get: unexpected record: %+v", got)
	}

	// List
	all := reg.List()
	if len(all) != 1 {
		t.Fatalf("adapter List: expected 1 patient, got %d", len(all))
	}

	// FilterByCondition
	filtered := reg.FilterByCondition("I10")
	if len(filtered) != 1 {
		t.Fatalf("adapter FilterByCondition: expected 1, got %d", len(filtered))
	}

	// Filter with non-matching code returns empty.
	none := reg.FilterByCondition("Z99.99")
	if len(none) != 0 {
		t.Fatalf("adapter FilterByCondition: expected 0, got %d", len(none))
	}

	// Get for non-existent patient returns false.
	_, notOK := reg.Get("nonexistent")
	if notOK {
		t.Fatal("adapter Get: expected ok=false for nonexistent patient")
	}
}

// ---------------------------------------------------------------------------
// AuditRecorder adapter (AuditRecorder, auditAdapter.Record,
// auditAdapter.List — all 0%)
// ---------------------------------------------------------------------------

func TestAuditRecorderAdapter(t *testing.T) {
	db := dbtest.NewDB(t)

	recorder := db.Store.AuditRecorder(db.TenantID)

	ev := audit.Event{
		ID:        "adapter_ev_1",
		At:        time.Now().UTC(),
		Kind:      audit.KindAgentError,
		AgentID:   "test-agent",
		PatientID: "adapter_pt_err",
		CaseID:    "adapter_case_err",
		ErrorText: "synthetic error for coverage",
	}
	if err := recorder.Record(ev); err != nil {
		t.Fatalf("adapter Record: %v", err)
	}

	out := recorder.List()
	if len(out) != 1 {
		t.Fatalf("adapter List: expected 1 event, got %d", len(out))
	}
	if out[0].ID != "adapter_ev_1" {
		t.Fatalf("adapter List: expected ID adapter_ev_1, got %s", out[0].ID)
	}
	if out[0].Kind != audit.KindAgentError {
		t.Fatalf("adapter List: expected kind agent_error, got %s", out[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// RecordAuditEvent — missing ID error path (was 69.2%)
// ---------------------------------------------------------------------------

func TestRecordAuditEvent_MissingID(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	ev := audit.Event{
		// ID deliberately empty to hit the error branch.
		At:      time.Now().UTC(),
		Kind:    audit.KindMessage,
		AgentID: "diag",
	}
	err := db.Store.RecordAuditEvent(ctx, db.TenantID, ev)
	if err == nil {
		t.Fatal("expected error for missing audit event ID")
	}
}

// TestRecordAuditEvent_AutoTimestamp covers the branch where e.At is zero
// and gets backfilled to time.Now().
func TestRecordAuditEvent_AutoTimestamp(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	ev := audit.Event{
		ID:      "auto_ts_ev_1",
		Kind:    audit.KindMessage,
		AgentID: "diagnostician",
		// At deliberately zero — the store should backfill.
	}
	if err := db.Store.RecordAuditEvent(ctx, db.TenantID, ev); err != nil {
		t.Fatalf("record with zero At: %v", err)
	}

	out, err := db.Store.ListAuditEvents(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out))
	}
	if out[0].At.IsZero() {
		t.Fatal("At was not backfilled")
	}
}

// ---------------------------------------------------------------------------
// ListAuditEvents — multiple events, richly populated (improves 58.3%)
// ---------------------------------------------------------------------------

func TestListAuditEvents_MultipleEvents(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	events := []audit.Event{
		{
			ID:        "multi_ev_1",
			At:        time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
			Kind:      audit.KindMessage,
			AgentID:   "intake",
			MessageID: "msg_m1",
			From:      "patient",
			To:        "intake",
			Type:      "chief_complaint",
			PatientID: "pt_multi_1",
			CaseID:    "case_multi_1",
			TraceID:   "trace_m1",
		},
		{
			ID:        "multi_ev_2",
			At:        time.Date(2026, 6, 1, 10, 1, 0, 0, time.UTC),
			Kind:      audit.KindPolicyDecision,
			AgentID:   "orchestrator",
			MessageID: "msg_m2",
			CaseID:    "case_multi_1",
			Decision:  "allow",
			Reason:    "policy passed",
		},
		{
			ID:         "multi_ev_3",
			At:         time.Date(2026, 6, 1, 10, 2, 0, 0, time.UTC),
			Kind:       audit.KindReviewDecision,
			AgentID:    "hitl-gate",
			MessageID:  "msg_m3",
			CaseID:     "case_multi_1",
			Decision:   "approve",
			Reason:     "reviewer approved",
			ReviewerID: "dr-smith",
		},
		{
			ID:        "multi_ev_4",
			At:        time.Date(2026, 6, 1, 10, 3, 0, 0, time.UTC),
			Kind:      audit.KindAgentError,
			AgentID:   "diagnostician",
			CaseID:    "case_multi_1",
			ErrorText: "synthetic LLM timeout",
		},
	}

	for _, ev := range events {
		if err := db.Store.RecordAuditEvent(ctx, db.TenantID, ev); err != nil {
			t.Fatalf("record %s: %v", ev.ID, err)
		}
	}

	out, err := db.Store.ListAuditEvents(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("expected 4 events, got %d", len(out))
	}

	// Events should be ordered by At ASC.
	for i := 1; i < len(out); i++ {
		if out[i].At.Before(out[i-1].At) {
			t.Fatalf("events not in chronological order at index %d", i)
		}
	}

	// Verify field round-trip on the richest event (review decision).
	var review audit.Event
	for _, e := range out {
		if e.ID == "multi_ev_3" {
			review = e
			break
		}
	}
	if review.Decision != "approve" || review.ReviewerID != "dr-smith" || review.Reason != "reviewer approved" {
		t.Fatalf("review event fields not preserved: %+v", review)
	}

	// Verify error event fields.
	var errEv audit.Event
	for _, e := range out {
		if e.ID == "multi_ev_4" {
			errEv = e
			break
		}
	}
	if errEv.ErrorText != "synthetic LLM timeout" {
		t.Fatalf("error text not preserved: %+v", errEv)
	}
}

// ---------------------------------------------------------------------------
// withTenant — empty tenant error path (was 66.7%)
// ---------------------------------------------------------------------------

func TestWithTenant_EmptyTenantID(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	// Any operation with an empty tenant should fail.
	err := db.Store.SaveInteraction(ctx, "", eval.InteractionRecord{
		ID:        "empty_tenant_1",
		Scenario:  "should_fail",
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for empty tenantID")
	}
}

// ---------------------------------------------------------------------------
// UpsertPatient with DischargedAt set (covers the non-zero branch)
// ---------------------------------------------------------------------------

func TestUpsertPatient_WithDischargedAt(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	discharged := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	p := medical.PatientRecord{
		PatientID:    "pt_discharged_1",
		Age:          70,
		Sex:          "M",
		Conditions:   []string{"I50.9"},
		RPMEnrolled:  true,
		DischargedAt: discharged,
	}
	if err := db.Store.UpsertPatient(ctx, db.TenantID, p); err != nil {
		t.Fatalf("upsert with DischargedAt: %v", err)
	}

	got, ok, err := db.Store.GetPatient(ctx, db.TenantID, "pt_discharged_1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.DischargedAt.IsZero() {
		t.Fatal("DischargedAt was not preserved")
	}
	if !got.RPMEnrolled {
		t.Fatal("RPMEnrolled was not preserved")
	}
}

// ---------------------------------------------------------------------------
// UpsertPatient with nil Conditions (covers the nil-guard branch)
// ---------------------------------------------------------------------------

func TestUpsertPatient_NilConditions(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	p := medical.PatientRecord{
		PatientID:  "pt_nil_cond_1",
		Age:        45,
		Sex:        "F",
		Conditions: nil, // explicitly nil
	}
	if err := db.Store.UpsertPatient(ctx, db.TenantID, p); err != nil {
		t.Fatalf("upsert with nil conditions: %v", err)
	}

	got, ok, err := db.Store.GetPatient(ctx, db.TenantID, "pt_nil_cond_1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Age != 45 {
		t.Fatalf("unexpected age: %d", got.Age)
	}
}

// ---------------------------------------------------------------------------
// SaveInteraction upsert (ON CONFLICT UPDATE branch)
// ---------------------------------------------------------------------------

func TestSaveInteraction_Upsert(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	rec := eval.InteractionRecord{
		ID:        "upsert_eval_1",
		Scenario:  "first_write",
		Success:   false,
		Metrics:   map[string]float64{"accuracy": 0.5},
		StartedAt: time.Now().UTC().Truncate(time.Second),
		EndedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := db.Store.SaveInteraction(ctx, db.TenantID, rec); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Update the same record.
	rec.Scenario = "updated"
	rec.Success = true
	rec.Metrics["accuracy"] = 0.99
	if err := db.Store.SaveInteraction(ctx, db.TenantID, rec); err != nil {
		t.Fatalf("upsert save: %v", err)
	}

	got, err := db.Store.ListInteractions(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 record after upsert, got %d", len(got))
	}
	if got[0].Scenario != "updated" || !got[0].Success {
		t.Fatalf("upsert did not update: %+v", got[0])
	}
}

// ---------------------------------------------------------------------------
// SaveInteraction with nil Metrics/Metadata (covers emptyMap/emptyAnyMap)
// ---------------------------------------------------------------------------

func TestSaveInteraction_NilMaps(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	rec := eval.InteractionRecord{
		ID:        "nil_maps_1",
		Scenario:  "nil_maps",
		Success:   true,
		Metrics:   nil,
		Metadata:  nil,
		StartedAt: time.Now().UTC().Truncate(time.Second),
		EndedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := db.Store.SaveInteraction(ctx, db.TenantID, rec); err != nil {
		t.Fatalf("save with nil maps: %v", err)
	}

	got, err := db.Store.ListInteractions(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 record, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// GetPatient for non-existent patient (covers the ErrNoRows path in
// GetPatient directly, not just via the adapter)
// ---------------------------------------------------------------------------

func TestGetPatient_NotFound(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	_, ok, err := db.Store.GetPatient(ctx, db.TenantID, "nonexistent_pt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for nonexistent patient")
	}
}

// ---------------------------------------------------------------------------
// ListPatients / FilterPatientsByCondition on empty tenant
// ---------------------------------------------------------------------------

func TestListPatients_Empty(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	out, err := db.Store.ListPatients(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 patients, got %d", len(out))
	}
}

func TestFilterPatientsByCondition_Empty(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	out, err := db.Store.FilterPatientsByCondition(ctx, db.TenantID, "E11.9")
	if err != nil {
		t.Fatalf("filter empty: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 patients, got %d", len(out))
	}
}

// ---------------------------------------------------------------------------
// ListInteractions on empty tenant
// ---------------------------------------------------------------------------

func TestListInteractions_Empty(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	out, err := db.Store.ListInteractions(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 records, got %d", len(out))
	}
}

// ---------------------------------------------------------------------------
// ListAuditEvents on empty tenant
// ---------------------------------------------------------------------------

func TestListAuditEvents_Empty(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	out, err := db.Store.ListAuditEvents(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 events, got %d", len(out))
	}
}

// ---------------------------------------------------------------------------
// UpsertPatient update path (ON CONFLICT DO UPDATE)
// ---------------------------------------------------------------------------

func TestUpsertPatient_Update(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	p := medical.PatientRecord{
		PatientID:  "pt_upsert_upd_1",
		Age:        40,
		Sex:        "F",
		Conditions: []string{"J44.9"},
	}
	if err := db.Store.UpsertPatient(ctx, db.TenantID, p); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Update the same patient.
	p.Age = 41
	p.Conditions = []string{"J44.9", "I10"}
	if err := db.Store.UpsertPatient(ctx, db.TenantID, p); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, ok, err := db.Store.GetPatient(ctx, db.TenantID, "pt_upsert_upd_1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Age != 41 {
		t.Fatalf("expected age 41 after update, got %d", got.Age)
	}
	if len(got.Conditions) != 2 {
		t.Fatalf("expected 2 conditions after update, got %d", len(got.Conditions))
	}
}

// ---------------------------------------------------------------------------
// CurrentRoleHasBypassRLS — directly exercise the public method
// ---------------------------------------------------------------------------

func TestCurrentRoleHasBypassRLS(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	bypass, err := db.Store.CurrentRoleHasBypassRLS(ctx)
	if err != nil {
		t.Fatalf("CurrentRoleHasBypassRLS: %v", err)
	}
	// The app role bodh_app should NOT have bypass.
	if bypass {
		t.Fatal("expected bypass=false for bodh_app")
	}
}

// ---------------------------------------------------------------------------
// DeletePatient on non-existent patient (idempotent — no error)
// ---------------------------------------------------------------------------

func TestDeletePatient_NonExistent(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	// Should succeed silently (no row to delete).
	if err := db.Store.DeletePatient(ctx, db.TenantID, "ghost_patient"); err != nil {
		t.Fatalf("delete non-existent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Multiple patients — List returns all, Filter returns subset
// ---------------------------------------------------------------------------

func TestListAndFilter_MultiplePatients(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	patients := []medical.PatientRecord{
		{PatientID: "multi_pt_1", Age: 50, Conditions: []string{"E11.9"}},
		{PatientID: "multi_pt_2", Age: 60, Conditions: []string{"I10"}},
		{PatientID: "multi_pt_3", Age: 70, Conditions: []string{"E11.9", "I10"}},
	}
	for _, p := range patients {
		if err := db.Store.UpsertPatient(ctx, db.TenantID, p); err != nil {
			t.Fatalf("upsert %s: %v", p.PatientID, err)
		}
	}

	all, err := db.Store.ListPatients(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 patients, got %d", len(all))
	}

	diabetic, err := db.Store.FilterPatientsByCondition(ctx, db.TenantID, "E11.9")
	if err != nil {
		t.Fatalf("filter E11.9: %v", err)
	}
	if len(diabetic) != 2 {
		t.Fatalf("expected 2 diabetic patients, got %d", len(diabetic))
	}

	hypertensive, err := db.Store.FilterPatientsByCondition(ctx, db.TenantID, "I10")
	if err != nil {
		t.Fatalf("filter I10: %v", err)
	}
	if len(hypertensive) != 2 {
		t.Fatalf("expected 2 hypertensive patients, got %d", len(hypertensive))
	}
}

// ---------------------------------------------------------------------------
// Audit event tenant isolation
// ---------------------------------------------------------------------------

func TestAuditEvent_TenantIsolation(t *testing.T) {
	dbA := dbtest.NewDB(t)
	dbB := dbtest.NewDB(t)
	ctx := context.Background()

	ev := audit.Event{
		ID:      "iso_ev_1",
		At:      time.Now().UTC(),
		Kind:    audit.KindMessage,
		AgentID: "intake",
	}
	if err := dbA.Store.RecordAuditEvent(ctx, dbA.TenantID, ev); err != nil {
		t.Fatalf("record in tenant A: %v", err)
	}

	outB, err := dbB.Store.ListAuditEvents(ctx, dbB.TenantID)
	if err != nil {
		t.Fatalf("list tenant B: %v", err)
	}
	if len(outB) != 0 {
		t.Fatalf("tenant B should not see tenant A audit events, got %d", len(outB))
	}
}

// ---------------------------------------------------------------------------
// withTenant — operations with empty tenant on other surfaces
// ---------------------------------------------------------------------------

func TestEmptyTenantID_Patient(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	err := db.Store.UpsertPatient(ctx, "", medical.PatientRecord{PatientID: "nope"})
	if err == nil {
		t.Fatal("expected error for empty tenantID on UpsertPatient")
	}
}

func TestEmptyTenantID_Audit(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	err := db.Store.RecordAuditEvent(ctx, "", audit.Event{ID: "nope", Kind: audit.KindMessage})
	if err == nil {
		t.Fatal("expected error for empty tenantID on RecordAuditEvent")
	}
}

func TestEmptyTenantID_ListPatients(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	_, err := db.Store.ListPatients(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty tenantID on ListPatients")
	}
}

func TestEmptyTenantID_GetPatient(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	_, _, err := db.Store.GetPatient(ctx, "", "any")
	if err == nil {
		t.Fatal("expected error for empty tenantID on GetPatient")
	}
}

func TestEmptyTenantID_FilterPatients(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	_, err := db.Store.FilterPatientsByCondition(ctx, "", "E11.9")
	if err == nil {
		t.Fatal("expected error for empty tenantID on FilterPatientsByCondition")
	}
}

func TestEmptyTenantID_DeletePatient(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	err := db.Store.DeletePatient(ctx, "", "any")
	if err == nil {
		t.Fatal("expected error for empty tenantID on DeletePatient")
	}
}

func TestEmptyTenantID_ListInteractions(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	_, err := db.Store.ListInteractions(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty tenantID on ListInteractions")
	}
}

func TestEmptyTenantID_ListAuditEvents(t *testing.T) {
	db := dbtest.NewDB(t)
	ctx := context.Background()

	_, err := db.Store.ListAuditEvents(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty tenantID on ListAuditEvents")
	}
}
