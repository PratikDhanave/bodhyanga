package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/knowledge"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
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
	if mock.Calls[0].Grammar == nil {
		t.Fatalf("expected GBNF grammar to be sent to LLM (sampler-level guarantee)")
	}
	if mock.Calls[0].Grammar.Format != llm.GrammarGBNF {
		t.Fatalf("Grammar.Format=%q want %q", mock.Calls[0].Grammar.Format, llm.GrammarGBNF)
	}
	if !strings.Contains(mock.Calls[0].Grammar.Body, "root") {
		t.Fatalf("grammar body missing root rule:\n%s", mock.Calls[0].Grammar.Body)
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

func TestLLMDiagnosticianRAGRetrievesAndInjectsCitations(t *testing.T) {
	state, encoded := newCaseStateJSON(t)
	respJSON := `{"diagnosis":"Community-acquired pneumonia","confidence":0.91,"differential":["CAP"],"rationale":"RLL infiltrate plus leukocytosis."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	store := &fakeKnowledge{
		Resp: []knowledge.Citation{
			{
				DocumentID: "idsa-cap-2019",
				Source:     "IDSA 2019 §III",
				Title:      "Community-Acquired Pneumonia in Adults",
				Snippet:    "Infiltrate on chest imaging plus signs of infection supports CAP.",
				Score:      0.92,
			},
			{
				DocumentID: "uspstf-cough",
				Source:     "USPSTF 2024",
				Title:      "Cough Evaluation",
				Snippet:    "Productive cough with fever raises concern for bacterial pneumonia.",
				Score:      0.81,
			},
		},
	}
	d := &LLMDiagnostician{
		Client:    mock,
		Fallback:  &DiagnosticianAgent{},
		Knowledge: store,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": state.CaseID})
	out, err := d.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(store.SearchCalls) != 1 {
		t.Fatalf("expected 1 Search call, got %d", len(store.SearchCalls))
	}
	if store.SearchCalls[0].K != 4 {
		t.Fatalf("default RetrievalK should be 4, got %d", store.SearchCalls[0].K)
	}
	q := store.SearchCalls[0].Query
	for _, want := range []string{"Cough and fever", "cap", "chest_xray", "CAP"} {
		if !strings.Contains(q, want) {
			t.Errorf("retrieval query missing %q: %s", want, q)
		}
	}

	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	userContent := mock.Calls[0].Messages[0].Content
	if !strings.Contains(userContent, "Evidence:") {
		t.Fatalf("expected Evidence: block in user message, got:\n%s", userContent)
	}
	if !strings.Contains(userContent, "IDSA 2019 §III") {
		t.Fatalf("expected source label in user message, got:\n%s", userContent)
	}
	if !strings.Contains(mock.Calls[0].System, "Evidence") {
		t.Fatalf("expected RAG system prompt to mention Evidence")
	}

	var proposal medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &proposal); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if len(proposal.Citations) != 2 {
		t.Fatalf("expected 2 citations on proposal, got %d", len(proposal.Citations))
	}
	if proposal.Citations[0].Source != "IDSA 2019 §III" {
		t.Fatalf("citation[0].Source=%q", proposal.Citations[0].Source)
	}
	if proposal.Citations[0].Score == 0 {
		t.Fatalf("citation score not preserved")
	}
}

func TestLLMDiagnosticianRAGPreservesModelCitationsWhenSupplied(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	respJSON := `{"diagnosis":"CAP","confidence":0.9,"rationale":"r","citations":[{"source":"MODEL","title":"chose its own","snippet":"x"}]}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	store := &fakeKnowledge{
		Resp: []knowledge.Citation{{Source: "RETRIEVED", Title: "tossed", Snippet: "y", Score: 0.5}},
	}
	d := &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}, Knowledge: store}

	out, err := d.HandleMessage(context.Background(), agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"}), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Citations) != 1 || p.Citations[0].Source != "MODEL" {
		t.Fatalf("model citations should win, got %+v", p.Citations)
	}
}

func TestLLMDiagnosticianRAGSurvivesSearchError(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	respJSON := `{"diagnosis":"CAP","confidence":0.8,"rationale":"r"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	store := &fakeKnowledge{Err: errSearch}
	d := &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}, Knowledge: store}
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"}), env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected LLM still called on retrieval failure, got %d", len(mock.Calls))
	}
	if strings.Contains(mock.Calls[0].Messages[0].Content, "Evidence:") {
		t.Fatalf("no Evidence block when retrieval fails")
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Citations) != 0 {
		t.Fatalf("no citations expected when retrieval failed")
	}
	if !containsLog(env, "rag search failed") {
		t.Fatalf("expected rag failure log, got %v", env.logs)
	}
}

func TestLLMDiagnosticianWithoutKnowledgeKeepsExistingBehaviour(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	respJSON := `{"diagnosis":"CAP","confidence":0.9,"rationale":"r"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	d := &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}}

	out, err := d.HandleMessage(context.Background(), agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"}), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	if strings.Contains(mock.Calls[0].Messages[0].Content, "Evidence:") {
		t.Fatalf("no Evidence block expected without Knowledge")
	}
	if !strings.Contains(mock.Calls[0].System, "MAI-DxO") {
		t.Fatalf("expected base system prompt")
	}
	if strings.Contains(mock.Calls[0].System, "Evidence grounding") {
		t.Fatalf("RAG system prompt should not be used")
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Citations) != 0 {
		t.Fatalf("no citations on non-RAG proposal, got %d", len(p.Citations))
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
