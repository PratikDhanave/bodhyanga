package agents

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func newCaseStateJSON(t *testing.T) (medical.CaseState, string) {
	t.Helper()
	state := medical.CaseState{
		CaseID:       "test-case-1",
		Presentation: "Cough and fever",
		Condition:    "cap",
		TestResults: map[string]string{
			"chest_xray":           "RLL infiltrate",
			"complete_blood_count": "WBC 14.2",
		},
		Hypotheses:   []string{"CAP", "Bronchitis"},
		TotalCostUSD: 165,
		MaxCostUSD:   500,
	}
	return state, medical.EncodeState(state)
}

func TestLLMDiagnosticianSuccessEmitsProposalToVerifier(t *testing.T) {
	state, encoded := newCaseStateJSON(t)
	respJSON := `{"diagnosis":"Community-acquired pneumonia","confidence":0.91,"differential":["CAP","Bronchitis"],"rationale":"RLL infiltrate plus leukocytosis."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON, InputTokens: 100, OutputTokens: 30}}
	rule := &DiagnosticianAgent{}
	d := &LLMDiagnostician{Client: mock, Fallback: rule}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": state.CaseID})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if out[0].To != "reasoning_verifier" {
		t.Fatalf("To=%s, want reasoning_verifier", out[0].To)
	}
	if out[0].Type != "diagnosis_proposal" {
		t.Fatalf("Type=%s, want diagnosis_proposal", out[0].Type)
	}
	var proposal medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &proposal); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if proposal.Diagnosis != "Community-acquired pneumonia" {
		t.Fatalf("Diagnosis=%q", proposal.Diagnosis)
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

func TestLLMDiagnosticianFallsBackOnLLMError(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	mock := &llm.Mock{Err: errors.New("network down")}
	rule := &DiagnosticianAgent{}
	d := &LLMDiagnostician{Client: mock, Fallback: rule}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("fallback should still emit one proposal: %+v", out)
	}
	if !containsLog(env, "falling back") {
		t.Fatalf("expected fallback log line, got %v", env.logs)
	}
}

func TestLLMDiagnosticianFallsBackOnBadJSON(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	mock := &llm.Mock{Resp: &llm.Response{Content: "this is not json"}}
	rule := &DiagnosticianAgent{}
	d := &LLMDiagnostician{Client: mock, Fallback: rule}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	out, err := d.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("bad JSON should trigger fallback emission: %+v", out)
	}
}

func TestLLMDiagnosticianFallsBackOnLowConfidence(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	respJSON := `{"diagnosis":"Maybe pneumonia","confidence":0.1,"rationale":"Unsure."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	rule := &DiagnosticianAgent{}
	d := &LLMDiagnostician{Client: mock, Fallback: rule, MinConfidence: 0.3}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	out, err := d.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected fallback to emit one message, got %d", len(out))
	}
}

func containsLog(env *fakeEnv, want string) bool {
	for _, l := range env.logs {
		if containsSubstring(l, want) {
			return true
		}
	}
	return false
}

func containsSubstring(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
