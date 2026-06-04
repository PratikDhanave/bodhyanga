package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

func TestRefillManagerIDNameCapabilities(t *testing.T) {
	a := &RefillManagerAgent{Tasks: tasks.NewQueue(), Registry: medical.NewPatientRegistry()}
	if a.ID() != "refill_manager" {
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

func TestRefillManagerAutoApprove(t *testing.T) {
	q := tasks.NewQueue()
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID:   "p1",
		Medications: []medical.MedicationEntry{{Name: "Metformin"}},
	})
	a := &RefillManagerAgent{Tasks: q, Registry: reg}

	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"medication": "metformin",
	})
	msg := agent.NewMessage("inbox_triage", "refill_manager", agent.RoleAgent,
		"refill_request", string(body), map[string]any{})

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
	var resp map[string]string
	json.Unmarshal([]byte(out[0].Content), &resp)
	if resp["status"] != "approved" {
		t.Errorf("status=%s, want approved", resp["status"])
	}
	if len(q.List()) != 0 {
		t.Error("approved request should not create a task")
	}
}

func TestRefillManagerReview(t *testing.T) {
	q := tasks.NewQueue()
	reg := medical.NewPatientRegistry()
	a := &RefillManagerAgent{Tasks: q, Registry: reg}

	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"medication": "oxycodone",
	})
	msg := agent.NewMessage("inbox_triage", "refill_manager", agent.RoleAgent,
		"refill_request", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	var resp map[string]string
	json.Unmarshal([]byte(out[0].Content), &resp)
	if resp["status"] != "review" {
		t.Errorf("status=%s, want review", resp["status"])
	}
	if len(q.List()) != 1 {
		t.Error("review request should create a task")
	}
}

func TestRefillManagerHITL(t *testing.T) {
	q := tasks.NewQueue()
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID:   "p1",
		Medications: []medical.MedicationEntry{{Name: "Metformin"}},
	})
	a := &RefillManagerAgent{Tasks: q, Registry: reg, HITL: true}

	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"medication": "metformin",
	})
	msg := agent.NewMessage("inbox_triage", "refill_manager", agent.RoleAgent,
		"refill_request", string(body), map[string]any{})

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
}

func TestRefillManagerHITLReviewStatus(t *testing.T) {
	q := tasks.NewQueue()
	reg := medical.NewPatientRegistry()
	a := &RefillManagerAgent{Tasks: q, Registry: reg, HITL: true}

	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"medication": "oxycodone",
	})
	msg := agent.NewMessage("inbox_triage", "refill_manager", agent.RoleAgent,
		"refill_request", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	// Review status should produce high priority in HITL.
	if out[0].Metadata["hitl_priority"] != "high" {
		t.Errorf("hitl_priority=%v, want high", out[0].Metadata["hitl_priority"])
	}
}

func TestRefillManagerInvalidJSON(t *testing.T) {
	a := &RefillManagerAgent{Tasks: tasks.NewQueue(), Registry: medical.NewPatientRegistry()}
	msg := agent.NewMessage("inbox_triage", "refill_manager", agent.RoleAgent,
		"refill_request", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}
