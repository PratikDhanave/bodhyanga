package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

func TestLLMInboxTriageRefill(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"refill","medication":"metformin"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    q,
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Refill needed",
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
}

func TestLLMInboxTriageRefillNoMedication(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"refill"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    q,
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Refill needed",
		"body":       "Need refill",
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
	var resp map[string]string
	json.Unmarshal([]byte(out[0].Content), &resp)
	if resp["medication"] != "unknown" {
		t.Errorf("medication=%s, want unknown", resp["medication"])
	}
}

func TestLLMInboxTriagePriorAuth(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"prior_auth","procedure_code":"MRI"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    q,
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Authorization needed",
		"body":       "Need MRI authorization",
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

func TestLLMInboxTriagePriorAuthNoCode(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"prior_auth"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    q,
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Auth request",
		"body":       "Need auth",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]string
	json.Unmarshal([]byte(out[0].Content), &resp)
	if resp["code"] != "unknown" {
		t.Errorf("code=%s, want unknown", resp["code"])
	}
}

func TestLLMInboxTriageUrgent(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"urgent_clinical"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    q,
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Emergency",
		"body":       "Chest pain",
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
}

func TestLLMInboxTriageGeneral(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"general"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    q,
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Question",
		"body":       "How do I get my records?",
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
}

func TestLLMInboxTriageUnknownCategoryFallback(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"bogus"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    q,
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Question",
		"body":       "Something",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Error("fallback should produce output")
	}
}

func TestLLMInboxTriageUrgentNilTasks(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"urgent_clinical"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    nil, // nil task queue
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Emergency",
		"body":       "Chest pain",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Error("nil task queue should fall back")
	}
}

func TestLLMInboxTriageGeneralNilTasks(t *testing.T) {
	q := tasks.NewQueue()
	respJSON := `{"category":"general"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMInboxTriage{
		Client:   mock,
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    nil, // nil task queue
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "Question",
		"body":       "How are you?",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Error("nil task queue should fall back")
	}
}
