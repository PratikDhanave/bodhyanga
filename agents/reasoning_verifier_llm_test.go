package agents

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func newVerifierInputs(t *testing.T) (medical.DiagnosisProposal, agent.Message) {
	t.Helper()
	state := medical.CaseState{
		CaseID:       "test-case-verifier",
		Presentation: "Cough and fever",
		Condition:    "cap",
		TestResults: map[string]string{
			"chest_xray":           "RLL infiltrate",
			"complete_blood_count": "WBC 14.2",
		},
	}
	proposal := medical.DiagnosisProposal{
		Diagnosis:  "Community-acquired pneumonia",
		Confidence: 0.91,
		Rationale:  "RLL infiltrate plus leukocytosis on a febrile patient.",
	}
	content, _ := json.Marshal(proposal)
	msg := agent.NewMessage("diagnostician", "reasoning_verifier", agent.RoleAgent,
		"diagnosis_proposal", string(content),
		map[string]any{"case_state": medical.EncodeState(state)})
	return proposal, msg
}

func TestLLMReasoningVerifierSuccessEmitsExpectedMessage(t *testing.T) {
	_, msg := newVerifierInputs(t)
	respJSON := `{"verified":true,"reason":"Imaging plus leukocytosis support the diagnosis."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON, InputTokens: 90, OutputTokens: 20}}
	v := &LLMReasoningVerifier{Client: mock, Fallback: &ReasoningVerifierAgent{}}

	env := newFakeEnv()
	out, err := v.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if out[0].To != "diagnostic_supervisor" {
		t.Fatalf("To=%s, want diagnostic_supervisor", out[0].To)
	}
	if out[0].Type != "diagnosis" {
		t.Fatalf("Type=%s, want diagnosis", out[0].Type)
	}
	got, err := medical.DecodeState(out[0].Content)
	if err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if got.WorkingDiagnosis != "Community-acquired pneumonia" {
		t.Fatalf("WorkingDiagnosis=%q", got.WorkingDiagnosis)
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
	if mock.Calls[0].Grammar == nil {
		t.Fatalf("expected GBNF enum grammar over verifier verdicts")
	}
	if mock.Calls[0].Grammar.Format != llm.GrammarGBNF {
		t.Fatalf("Grammar.Format=%q want gbnf", mock.Calls[0].Grammar.Format)
	}
	for _, want := range []string{"approve", "reject", "defer"} {
		if !containsSubstring(mock.Calls[0].Grammar.Body, want) {
			t.Errorf("grammar body missing verdict %q:\n%s", want, mock.Calls[0].Grammar.Body)
		}
	}
}

func TestLLMReasoningVerifierHITLRoutesToHumanReview(t *testing.T) {
	_, msg := newVerifierInputs(t)
	respJSON := `{"verified":true,"reason":"Grounded in evidence."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	v := &LLMReasoningVerifier{Client: mock, Fallback: &ReasoningVerifierAgent{HITL: true}, HITL: true}

	out, err := v.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "human_review" || out[0].Type != "review_request" {
		t.Fatalf("HITL should route to human_review/review_request, got %+v", out)
	}
}

func TestLLMReasoningVerifierRejectEmitsNothing(t *testing.T) {
	_, msg := newVerifierInputs(t)
	respJSON := `{"verified":false,"reason":"Confidence is overstated for the evidence."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	v := &LLMReasoningVerifier{Client: mock, Fallback: &ReasoningVerifierAgent{}}

	out, err := v.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("verified=false should emit nothing, got %+v", out)
	}
}

func TestLLMReasoningVerifierFallsBackOnLLMError(t *testing.T) {
	_, msg := newVerifierInputs(t)
	mock := &llm.Mock{Err: errors.New("network down")}
	v := &LLMReasoningVerifier{Client: mock, Fallback: &ReasoningVerifierAgent{}}

	env := newFakeEnv()
	out, err := v.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "diagnostic_supervisor" || out[0].Type != "diagnosis" {
		t.Fatalf("fallback should emit diagnosis to supervisor: %+v", out)
	}
	if !containsLog(env, "falling back") {
		t.Fatalf("expected fallback log line, got %v", env.logs)
	}
}

func TestLLMReasoningVerifierFallsBackOnBadJSON(t *testing.T) {
	_, msg := newVerifierInputs(t)
	mock := &llm.Mock{Resp: &llm.Response{Content: "this is not json"}}
	v := &LLMReasoningVerifier{Client: mock, Fallback: &ReasoningVerifierAgent{}}

	out, err := v.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "diagnostic_supervisor" || out[0].Type != "diagnosis" {
		t.Fatalf("bad JSON should trigger fallback emission: %+v", out)
	}
}
