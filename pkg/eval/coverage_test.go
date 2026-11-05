package eval

import (
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// ---------------------------------------------------------------------------
// InMemoryStore
// ---------------------------------------------------------------------------

func TestInMemoryStore_SaveAndList(t *testing.T) {
	store := NewInMemoryStore()
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	rec := InteractionRecord{
		ID:       "rec-1",
		Scenario: "test",
		Success:  true,
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 record, got %d", len(list))
	}
	if list[0].ID != "rec-1" {
		t.Errorf("expected rec-1, got %q", list[0].ID)
	}
}

func TestInMemoryStore_ListReturnsACopy(t *testing.T) {
	store := NewInMemoryStore()
	store.Save(InteractionRecord{ID: "r1"})

	list1 := store.List()
	list1[0].ID = "modified"

	list2 := store.List()
	if list2[0].ID == "modified" {
		t.Error("List should return a copy, not reference to internal slice")
	}
}

// ---------------------------------------------------------------------------
// diagnosisMatch / boolToFloat
// ---------------------------------------------------------------------------

func TestDiagnosisMatch_EmptyGold(t *testing.T) {
	if diagnosisMatch("something", "") {
		t.Error("expected false when gold is empty")
	}
}

func TestDiagnosisMatch_CaseInsensitive(t *testing.T) {
	if !diagnosisMatch("  Asthma  ", "asthma") {
		t.Error("expected match regardless of case and whitespace")
	}
}

func TestBoolToFloat_True(t *testing.T) {
	if boolToFloat(true) != 1 {
		t.Error("expected 1 for true")
	}
}

func TestBoolToFloat_False(t *testing.T) {
	if boolToFloat(false) != 0 {
		t.Error("expected 0 for false")
	}
}

// ---------------------------------------------------------------------------
// ToInteractionRecord
// ---------------------------------------------------------------------------

func TestToInteractionRecord(t *testing.T) {
	cm := CaseMetrics{
		DiagnosisMatch:  true,
		Accuracy:        1.0,
		CostUSD:         50.0,
		CostUtilization: 0.5,
		MessageSteps:    3,
		RPMAlertCount:   1,
		UnderBudget:     true,
	}
	rec := cm.ToInteractionRecord("case-001", "demo-scenario")
	if rec.ID != "case-001" {
		t.Errorf("expected case-001, got %q", rec.ID)
	}
	if rec.Scenario != "demo-scenario" {
		t.Errorf("expected demo-scenario, got %q", rec.Scenario)
	}
	if !rec.Success {
		t.Error("expected Success=true when both DiagnosisMatch and UnderBudget")
	}
	if rec.Metrics["accuracy"] != 1.0 {
		t.Errorf("expected accuracy 1.0, got %f", rec.Metrics["accuracy"])
	}
}

func TestToInteractionRecord_FailedBudget(t *testing.T) {
	cm := CaseMetrics{
		DiagnosisMatch: true,
		UnderBudget:    false,
	}
	rec := cm.ToInteractionRecord("case-002", "test")
	if rec.Success {
		t.Error("expected Success=false when UnderBudget=false")
	}
}

// ---------------------------------------------------------------------------
// ScoreCase with zero max cost
// ---------------------------------------------------------------------------

func TestScoreCase_ZeroMaxCost(t *testing.T) {
	state := medical.CaseState{
		WorkingDiagnosis: "asthma",
		GoldDiagnosis:    "asthma",
		TotalCostUSD:     10,
		MaxCostUSD:       0,
	}
	cm := ScoreCase(state, 5)
	if cm.CostUtilization != 0 {
		t.Errorf("expected 0 utilization for zero max, got %f", cm.CostUtilization)
	}
}
