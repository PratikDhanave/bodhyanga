package agents

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

func TestTCMCoordinatorIDNameCapabilities(t *testing.T) {
	a := &TCMCoordinatorAgent{Tasks: tasks.NewQueue()}
	if a.ID() != "tcm_coordinator" {
		t.Errorf("ID=%q", a.ID())
	}
	if a.Name() == "" {
		t.Error("Name must be non-empty")
	}
	caps := a.Capabilities()
	if len(caps) == 0 {
		t.Error("Capabilities must be non-empty")
	}
}

func TestTCMCoordinatorNormalDischarge(t *testing.T) {
	q := tasks.NewQueue()
	a := &TCMCoordinatorAgent{Tasks: q}
	d := medical.DischargeSummary{
		CaseID:       "c1",
		PatientID:    "p1",
		DischargedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		PrimaryDx:    "CAP",
	}
	msg := agent.NewMessage("readmission_tracker", "tcm_coordinator", agent.RoleAgent,
		"discharge_event", medical.EncodeDischarge(d), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].To != "engagement" {
		t.Errorf("To=%s, want engagement", out[0].To)
	}
	if out[0].Type != "tcm_tasks_created" {
		t.Errorf("Type=%s", out[0].Type)
	}
	// Default follow-up days for non-high-risk: 2, 7, 30 = 3 tasks.
	tl := q.List()
	if len(tl) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tl))
	}
}

func TestTCMCoordinatorHighRiskDischarge(t *testing.T) {
	q := tasks.NewQueue()
	a := &TCMCoordinatorAgent{Tasks: q}
	d := medical.DischargeSummary{
		CaseID:       "c1",
		PatientID:    "p1",
		DischargedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		PrimaryDx:    "CHF",
		HighRisk:     true,
	}
	msg := agent.NewMessage("readmission_tracker", "tcm_coordinator", agent.RoleAgent,
		"discharge_event", medical.EncodeDischarge(d), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	// High-risk follow-up days: 1, 3, 7, 14, 30 = 5 tasks.
	tl := q.List()
	if len(tl) != 5 {
		t.Errorf("expected 5 tasks for high-risk, got %d", len(tl))
	}
}

func TestTCMCoordinatorCustomFollowUpDays(t *testing.T) {
	q := tasks.NewQueue()
	a := &TCMCoordinatorAgent{Tasks: q}
	d := medical.DischargeSummary{
		CaseID:       "c1",
		PatientID:    "p1",
		DischargedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		PrimaryDx:    "CAP",
		FollowUpDays: []int{1, 5},
	}
	msg := agent.NewMessage("readmission_tracker", "tcm_coordinator", agent.RoleAgent,
		"discharge_event", medical.EncodeDischarge(d), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	tl := q.List()
	if len(tl) != 2 {
		t.Errorf("expected 2 tasks for custom days, got %d", len(tl))
	}
}

func TestTCMCoordinatorWithRegistryAndConflicts(t *testing.T) {
	q := tasks.NewQueue()
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID: "p1",
		Medications: []medical.MedicationEntry{
			{Name: "lisinopril", Class: "ace_inhibitor"},
		},
	})
	a := &TCMCoordinatorAgent{Tasks: q, Registry: reg}
	d := medical.DischargeSummary{
		CaseID:       "c1",
		PatientID:    "p1",
		DischargedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		PrimaryDx:    "CAP",
		HomeMeds:     []medical.MedicationEntry{{Name: "lisinopril", Class: "ace_inhibitor"}},
		NewMeds:      []medical.MedicationEntry{{Name: "enalapril", Class: "ace_inhibitor"}},
	}
	msg := agent.NewMessage("readmission_tracker", "tcm_coordinator", agent.RoleAgent,
		"discharge_event", medical.EncodeDischarge(d), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	// Should create med_reconciliation task + 3 follow-up tasks.
	tl := q.List()
	if len(tl) < 4 {
		t.Errorf("expected >= 4 tasks (1 reconciliation + 3 follow-ups), got %d", len(tl))
	}
}

func TestTCMCoordinatorHITL(t *testing.T) {
	q := tasks.NewQueue()
	a := &TCMCoordinatorAgent{Tasks: q, HITL: true}
	d := medical.DischargeSummary{
		CaseID:       "c1",
		PatientID:    "p1",
		DischargedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		PrimaryDx:    "CAP",
	}
	msg := agent.NewMessage("readmission_tracker", "tcm_coordinator", agent.RoleAgent,
		"discharge_event", medical.EncodeDischarge(d), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].To != "human_review" {
		t.Errorf("To=%s, want human_review", out[0].To)
	}
	if out[0].Type != "review_request" {
		t.Errorf("Type=%s", out[0].Type)
	}
}

func TestTCMCoordinatorInvalidJSON(t *testing.T) {
	a := &TCMCoordinatorAgent{Tasks: tasks.NewQueue()}
	msg := agent.NewMessage("readmission_tracker", "tcm_coordinator", agent.RoleAgent,
		"discharge_event", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestTCMCoordinatorEmptyCaseID(t *testing.T) {
	q := tasks.NewQueue()
	a := &TCMCoordinatorAgent{Tasks: q}
	d := medical.DischargeSummary{
		PatientID:    "p1",
		DischargedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		PrimaryDx:    "CAP",
	}
	msg := agent.NewMessage("readmission_tracker", "tcm_coordinator", agent.RoleAgent,
		"discharge_event", medical.EncodeDischarge(d), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	// No case_id in meta.
	if _, ok := out[0].Metadata["case_id"]; ok {
		t.Error("empty CaseID should not be set in metadata")
	}
}
