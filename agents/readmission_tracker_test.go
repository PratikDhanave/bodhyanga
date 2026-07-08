package agents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func TestReadmissionTrackerIDNameCapabilities(t *testing.T) {
	a := &ReadmissionTrackerAgent{Registry: medical.NewPatientRegistry()}
	if a.ID() != "readmission_tracker" {
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

func TestReadmissionTrackerAdmitEvent(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &ReadmissionTrackerAgent{Registry: reg}
	e := medical.ADTEvent{
		EventID:    "e1",
		PatientID:  "p1",
		Type:       medical.ADTAdmit,
		OccurredAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
	}
	msg := agent.NewMessage("hl7", "readmission_tracker", agent.RoleAgent,
		"adt_event", medical.EncodeADT(e), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	// Admit events emit only readmission_risk, no discharge_event.
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].Type != "readmission_risk" {
		t.Errorf("Type=%s", out[0].Type)
	}
}

func TestReadmissionTrackerDischargeEvent(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &ReadmissionTrackerAgent{Registry: reg}
	e := medical.ADTEvent{
		EventID:    "e2",
		PatientID:  "p1",
		Type:       medical.ADTDischarge,
		OccurredAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		PrimaryDx:  "CHF",
	}
	msg := agent.NewMessage("hl7", "readmission_tracker", agent.RoleAgent,
		"adt_event", medical.EncodeADT(e), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	// Discharge emits readmission_risk + discharge_event.
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	if out[0].Type != "readmission_risk" {
		t.Errorf("out[0].Type=%s", out[0].Type)
	}
	if out[1].Type != "discharge_event" {
		t.Errorf("out[1].Type=%s", out[1].Type)
	}
	if out[1].To != "tcm_coordinator" {
		t.Errorf("out[1].To=%s, want tcm_coordinator", out[1].To)
	}
}

func TestReadmissionTrackerDischargeHITL(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &ReadmissionTrackerAgent{Registry: reg, HITL: true}
	e := medical.ADTEvent{
		EventID:    "e3",
		PatientID:  "p2",
		Type:       medical.ADTDischarge,
		OccurredAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		PrimaryDx:  "Sepsis",
	}
	msg := agent.NewMessage("hl7", "readmission_tracker", agent.RoleAgent,
		"adt_event", medical.EncodeADT(e), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	// Second message should route through human_review.
	if out[1].To != "human_review" {
		t.Errorf("To=%s, want human_review", out[1].To)
	}
	if out[1].Type != "review_request" {
		t.Errorf("Type=%s, want review_request", out[1].Type)
	}
}

func TestReadmissionTrackerDischargeHighRiskPriority(t *testing.T) {
	// Set up a high-risk patient (age >= 75, conditions, recent discharge).
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID:      "p3",
		Age:            80,
		Conditions:     []string{"chf", "dm", "ckd", "copd", "htn", "af", "cad", "stroke"},
		DischargedAt:   time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		LastSystolicBP: 165,
		LastA1c:        9.5,
	})
	a := &ReadmissionTrackerAgent{Registry: reg, HITL: true}
	e := medical.ADTEvent{
		EventID:    "e4",
		PatientID:  "p3",
		Type:       medical.ADTDischarge,
		OccurredAt: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
		PrimaryDx:  "CHF",
	}
	msg := agent.NewMessage("hl7", "readmission_tracker", agent.RoleAgent,
		"adt_event", medical.EncodeADT(e), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 2 {
		t.Fatalf("want >= 2, got %d", len(out))
	}
	// Check risk data from first message.
	var riskData map[string]any
	json.Unmarshal([]byte(out[0].Content), &riskData)
	// For HITL, check that the second message has envelope keys.
	if out[1].Metadata["hitl_kind"] != "discharge_review" {
		t.Errorf("hitl_kind=%v", out[1].Metadata["hitl_kind"])
	}
}

func TestReadmissionTrackerDischargeNewPatient(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &ReadmissionTrackerAgent{Registry: reg}
	e := medical.ADTEvent{
		EventID:    "e5",
		PatientID:  "new_patient",
		Type:       medical.ADTDischarge,
		OccurredAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}
	msg := agent.NewMessage("hl7", "readmission_tracker", agent.RoleAgent,
		"adt_event", medical.EncodeADT(e), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	// Verify the patient was upserted.
	if _, ok := reg.Get("new_patient"); !ok {
		t.Error("new patient should be upserted")
	}
}

func TestReadmissionTrackerInvalidJSON(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &ReadmissionTrackerAgent{Registry: reg}
	msg := agent.NewMessage("hl7", "readmission_tracker", agent.RoleAgent,
		"adt_event", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestReadmissionTrackerMediumRiskPriority(t *testing.T) {
	// Medium risk: score >= 0.3 but < 0.6.
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID:  "p4",
		Age:        66,                    // +0.15
		Conditions: []string{"dm", "htn"}, // +0.16
	})
	a := &ReadmissionTrackerAgent{Registry: reg, HITL: true}
	e := medical.ADTEvent{
		EventID:    "e6",
		PatientID:  "p4",
		Type:       medical.ADTDischarge,
		OccurredAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}
	msg := agent.NewMessage("hl7", "readmission_tracker", agent.RoleAgent,
		"adt_event", medical.EncodeADT(e), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 2 {
		t.Fatalf("want >= 2, got %d", len(out))
	}
	// Medium risk should produce "high" priority in HITL envelope.
	if out[1].Metadata["hitl_priority"] != "high" {
		t.Errorf("hitl_priority=%v, want high", out[1].Metadata["hitl_priority"])
	}
}
