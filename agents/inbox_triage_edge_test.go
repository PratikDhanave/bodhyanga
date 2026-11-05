package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/tasks"
)

func TestInboxTriageRefill(t *testing.T) {
	q := tasks.NewQueue()
	a := &InboxTriageAgent{Tasks: q}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Refill request",
		"body":       "Need metformin refill",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].To != "refill_manager" {
		t.Errorf("To=%s, want refill_manager", out[0].To)
	}
	if out[0].Type != "refill_request" {
		t.Errorf("Type=%s, want refill_request", out[0].Type)
	}
}

func TestInboxTriagePriorAuth(t *testing.T) {
	q := tasks.NewQueue()
	a := &InboxTriageAgent{Tasks: q}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Prior auth needed",
		"body":       "Need authorization for MRI",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].To != "prior_auth" {
		t.Errorf("To=%s, want prior_auth", out[0].To)
	}
}

func TestInboxTriageUrgent(t *testing.T) {
	q := tasks.NewQueue()
	a := &InboxTriageAgent{Tasks: q}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Urgent",
		"body":       "chest pain since morning",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].Type != "inbox_routed" {
		t.Errorf("Type=%s, want inbox_routed", out[0].Type)
	}
	// Should create an urgent task.
	tl := q.List()
	if len(tl) != 1 {
		t.Fatalf("want 1 task, got %d", len(tl))
	}
	if tl[0].Priority != "urgent" {
		t.Errorf("priority=%s, want urgent", tl[0].Priority)
	}
}

func TestInboxTriageGeneral(t *testing.T) {
	q := tasks.NewQueue()
	a := &InboxTriageAgent{Tasks: q}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Question",
		"body":       "When is my next appointment?",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].Type != "inbox_routed" {
		t.Errorf("Type=%s, want inbox_routed", out[0].Type)
	}
	tl := q.List()
	if len(tl) != 1 {
		t.Fatalf("want 1 task, got %d", len(tl))
	}
	if tl[0].Priority != "normal" {
		t.Errorf("priority=%s, want normal", tl[0].Priority)
	}
}

func TestInboxTriageInvalidJSON(t *testing.T) {
	q := tasks.NewQueue()
	a := &InboxTriageAgent{Tasks: q}
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}
