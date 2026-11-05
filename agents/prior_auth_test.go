package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/tasks"
)

func TestPriorAuthIDNameCapabilities(t *testing.T) {
	a := &PriorAuthAgent{Tasks: tasks.NewQueue()}
	if a.ID() != "prior_auth" {
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

func TestPriorAuthLowRiskAutoApproves(t *testing.T) {
	q := tasks.NewQueue()
	a := &PriorAuthAgent{Tasks: q}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"code":       "DEXA",
		"payer":      "united",
	})
	msg := agent.NewMessage("inbox_triage", "prior_auth", agent.RoleAgent,
		"prior_auth_request", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].Type != "prior_auth_status" {
		t.Errorf("Type=%s", out[0].Type)
	}

	var resp map[string]string
	json.Unmarshal([]byte(out[0].Content), &resp)
	if resp["status"] != "approved" {
		t.Errorf("status=%s, want approved", resp["status"])
	}
	// No task should be created for approved requests.
	if len(q.List()) != 0 {
		t.Error("approved request should not create a task")
	}
}

func TestPriorAuthHighRiskSubmitted(t *testing.T) {
	q := tasks.NewQueue()
	a := &PriorAuthAgent{Tasks: q}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"code":       "MRI",
		"payer":      "aetna",
	})
	msg := agent.NewMessage("inbox_triage", "prior_auth", agent.RoleAgent,
		"prior_auth_request", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	var resp map[string]string
	json.Unmarshal([]byte(out[0].Content), &resp)
	if resp["status"] != "submitted" {
		t.Errorf("status=%s, want submitted", resp["status"])
	}
	// A task should be created for submitted requests.
	if len(q.List()) != 1 {
		t.Errorf("submitted request should create 1 task, got %d", len(q.List()))
	}
}

func TestPriorAuthCTAutoApproves(t *testing.T) {
	q := tasks.NewQueue()
	a := &PriorAuthAgent{Tasks: q}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"code":       "CT",
	})
	msg := agent.NewMessage("inbox_triage", "prior_auth", agent.RoleAgent,
		"prior_auth_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]string
	json.Unmarshal([]byte(out[0].Content), &resp)
	if resp["status"] != "approved" {
		t.Errorf("CT should be approved, got %s", resp["status"])
	}
}

func TestPriorAuthHITLRoutesThroughReview(t *testing.T) {
	q := tasks.NewQueue()
	a := &PriorAuthAgent{Tasks: q, HITL: true}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"code":       "DEXA",
	})
	msg := agent.NewMessage("inbox_triage", "prior_auth", agent.RoleAgent,
		"prior_auth_request", string(body), map[string]any{})
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

func TestPriorAuthInvalidJSON(t *testing.T) {
	a := &PriorAuthAgent{Tasks: tasks.NewQueue()}
	msg := agent.NewMessage("inbox_triage", "prior_auth", agent.RoleAgent,
		"prior_auth_request", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}
