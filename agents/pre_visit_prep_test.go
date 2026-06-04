package agents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func TestPreVisitPrepIDNameCapabilities(t *testing.T) {
	a := &PreVisitPrepAgent{Registry: medical.NewPatientRegistry()}
	if a.ID() != "pre_visit_prep" {
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

func TestPreVisitPrepWithFullRecord(t *testing.T) {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID:      "p1",
		LastSystolicBP: 145,
		LastBPDate:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastA1c:        7.2,
		LastA1cDate:    time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		LastWeightLb:   180,
		Conditions:     []string{"htn", "dm"},
		Medications: []medical.MedicationEntry{
			{Name: "lisinopril"},
			{Name: "metformin"},
		},
	})
	a := &PreVisitPrepAgent{Registry: reg}
	body, _ := json.Marshal(map[string]string{
		"patient_id":   "p1",
		"encounter_id": "enc1",
	})
	msg := agent.NewMessage("scheduler", "pre_visit_prep", agent.RoleAgent,
		"pre_visit_request", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].Type != "pre_visit_ready" {
		t.Errorf("Type=%s", out[0].Type)
	}

	var sum preVisitSummary
	if err := json.Unmarshal([]byte(out[0].Content), &sum); err != nil {
		t.Fatal(err)
	}
	if sum.PatientID != "p1" {
		t.Errorf("patient_id=%s", sum.PatientID)
	}
	if sum.EncounterID != "enc1" {
		t.Errorf("encounter_id=%s", sum.EncounterID)
	}
	if len(sum.Highlights) < 4 {
		t.Errorf("expected at least 4 highlights (BP, A1c, weight, meds), got %d", len(sum.Highlights))
	}
	if len(sum.OpenGaps) < 2 {
		t.Errorf("expected at least 2 open gaps, got %d", len(sum.OpenGaps))
	}
}

func TestPreVisitPrepUnknownPatient(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &PreVisitPrepAgent{Registry: reg}
	body, _ := json.Marshal(map[string]string{"patient_id": "unknown"})
	msg := agent.NewMessage("scheduler", "pre_visit_prep", agent.RoleAgent,
		"pre_visit_request", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("unknown patient should return nil, got %d messages", len(out))
	}
}

func TestPreVisitPrepInvalidJSON(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &PreVisitPrepAgent{Registry: reg}
	msg := agent.NewMessage("scheduler", "pre_visit_prep", agent.RoleAgent,
		"pre_visit_request", "not json", map[string]any{})

	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestPreVisitPrepMinimalRecord(t *testing.T) {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{PatientID: "p2"})
	a := &PreVisitPrepAgent{Registry: reg}
	body, _ := json.Marshal(map[string]string{"patient_id": "p2"})
	msg := agent.NewMessage("scheduler", "pre_visit_prep", agent.RoleAgent,
		"pre_visit_request", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
}
