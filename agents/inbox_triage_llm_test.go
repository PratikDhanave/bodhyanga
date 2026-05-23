package agents

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

func newInboxMessageJSON(t *testing.T, subject, body string) string {
	t.Helper()
	b, err := json.Marshal(inboxMessage{PatientID: "p-42", Subject: subject, Body: body})
	if err != nil {
		t.Fatalf("marshal inbox message: %v", err)
	}
	return string(b)
}

func TestLLMInboxTriageSuccessEmitsExpectedMessage(t *testing.T) {
	content := newInboxMessageJSON(t, "Need refill", "Please refill my metformin")
	respJSON := `{"category":"refill","medication":"metformin","procedure_code":""}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON, InputTokens: 60, OutputTokens: 12}}
	q := tasks.NewQueue()
	a := &LLMInboxTriage{Client: mock, Fallback: &InboxTriageAgent{Tasks: q}, Tasks: q}

	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent, "inbox_message", content, map[string]any{})
	env := newFakeEnv()

	out, err := a.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if out[0].To != "refill_manager" {
		t.Fatalf("To=%s, want refill_manager", out[0].To)
	}
	if out[0].Type != "refill_request" {
		t.Fatalf("Type=%s, want refill_request", out[0].Type)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(out[0].Content), &body); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if body["patient_id"] != "p-42" || body["medication"] != "metformin" {
		t.Fatalf("refill body=%+v", body)
	}
	if got, _ := out[0].Metadata["inbox_category"].(string); got != "refill" {
		t.Fatalf("metadata inbox_category=%q, want refill", got)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	if mock.Calls[0].JSONSchema == nil {
		t.Fatalf("expected JSON schema to be sent to LLM")
	}
	if !mock.Calls[0].CacheSystem {
		t.Fatalf("expected CacheSystem=true on system prompt")
	}
}

func TestLLMInboxTriageFallsBackOnLLMError(t *testing.T) {
	content := newInboxMessageJSON(t, "Refill request", "metformin please")
	mock := &llm.Mock{Err: errors.New("network down")}
	q := tasks.NewQueue()
	a := &LLMInboxTriage{Client: mock, Fallback: &InboxTriageAgent{Tasks: q}, Tasks: q}

	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent, "inbox_message", content, map[string]any{})
	env := newFakeEnv()

	out, err := a.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "refill_manager" || out[0].Type != "refill_request" {
		t.Fatalf("fallback should still route refill: %+v", out)
	}
	if !containsLog(env, "falling back") {
		t.Fatalf("expected fallback log line, got %v", env.logs)
	}
}

func TestLLMInboxTriageFallsBackOnBadJSON(t *testing.T) {
	content := newInboxMessageJSON(t, "Refill request", "metformin please")
	mock := &llm.Mock{Resp: &llm.Response{Content: "this is not json"}}
	q := tasks.NewQueue()
	a := &LLMInboxTriage{Client: mock, Fallback: &InboxTriageAgent{Tasks: q}, Tasks: q}

	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent, "inbox_message", content, map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "refill_manager" || out[0].Type != "refill_request" {
		t.Fatalf("bad JSON should trigger fallback refill emission: %+v", out)
	}
}
