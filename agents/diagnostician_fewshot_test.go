package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/knowledge"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// fewShotRespJSON is a valid DiagnosisProposal payload reused across tests so
// the LLM mock can return successfully and let the test focus on the prompt
// augmentation path.
const fewShotRespJSON = `{"diagnosis":"Community-acquired pneumonia","confidence":0.9,"differential":["CAP"],"rationale":"RLL infiltrate plus leukocytosis."}`

func TestFewShot_InjectsExamplesIntoPrompt(t *testing.T) {
	state, encoded := newCaseStateJSON(t)
	mock := &llm.Mock{Resp: &llm.Response{Content: fewShotRespJSON}}
	shots := &fakeKnowledge{
		Resp: []knowledge.Citation{
			{
				DocumentID: "fewshot-rev-cap-001",
				Source:     "hitl-decision/dr-alice/2026-05-23T09:05:00Z",
				Title:      "Diagnosis review: Community-acquired pneumonia",
				Snippet:    "Case: 58M productive cough fever 38.9C — Approved diagnosis: CAP",
				Score:      0.91,
			},
			{
				DocumentID: "fewshot-rev-cap-002",
				Source:     "hitl-decision/dr-bob/2026-05-23T13:03:00Z",
				Title:      "Diagnosis review: Community-acquired pneumonia (CURB-65 0)",
				Snippet:    "Case: 34F productive cough crackles LLL — Approved diagnosis: CAP",
				Score:      0.83,
			},
		},
	}
	d := &FewShotDiagnostician{
		Base:  &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Shots: shots,
		K:     2,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": state.CaseID})
	out, err := d.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" || out[0].Type != "diagnosis_proposal" {
		t.Fatalf("unexpected emission: %+v", out)
	}

	if len(shots.SearchCalls) != 1 {
		t.Fatalf("expected one shots Search call, got %d", len(shots.SearchCalls))
	}
	if shots.SearchCalls[0].K != 2 {
		t.Fatalf("shots Search K=%d, want 2", shots.SearchCalls[0].K)
	}
	// The retrieval query is built by buildRetrievalQuery from the case state.
	for _, want := range []string{"Cough and fever", "cap"} {
		if !strings.Contains(shots.SearchCalls[0].Query, want) {
			t.Errorf("shots query missing %q: %s", want, shots.SearchCalls[0].Query)
		}
	}

	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	userContent := mock.Calls[0].Messages[0].Content
	if !strings.HasPrefix(userContent, "Examples (past clinician-approved cases):") {
		t.Fatalf("user message should lead with the Examples header, got:\n%s", userContent)
	}
	for _, want := range []string{
		"Case: 58M productive cough fever 38.9C",
		"Case: 34F productive cough crackles LLL",
	} {
		if !strings.Contains(userContent, want) {
			t.Errorf("user message missing shot snippet %q:\n%s", want, userContent)
		}
	}
	// The original case-state prompt must survive the prepend.
	if !strings.Contains(userContent, "Case state:") {
		t.Fatalf("base case-state block clobbered by few-shot injection:\n%s", userContent)
	}

	var proposal medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if proposal.Diagnosis != "Community-acquired pneumonia" {
		t.Fatalf("Diagnosis=%q", proposal.Diagnosis)
	}
}

func TestFewShot_NoStoreSkipsInjection(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	mock := &llm.Mock{Resp: &llm.Response{Content: fewShotRespJSON}}
	d := &FewShotDiagnostician{
		Base:  &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Shots: nil,
		K:     3,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	if _, err := d.HandleMessage(context.Background(), msg, newFakeEnv()); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	user := mock.Calls[0].Messages[0].Content
	if strings.Contains(user, "Examples (past clinician-approved cases):") {
		t.Fatalf("no Examples block expected when Shots is nil, got:\n%s", user)
	}

	// Sanity: the base diagnostician prompt is still produced.
	if !strings.HasPrefix(user, "Case state:") {
		t.Fatalf("expected base user prompt to lead with 'Case state:', got:\n%s", user)
	}
}

func TestFewShot_LowScoreCitationsDropped(t *testing.T) {
	state, encoded := newCaseStateJSON(t)
	mock := &llm.Mock{Resp: &llm.Response{Content: fewShotRespJSON}}
	shots := &fakeKnowledge{
		Resp: []knowledge.Citation{
			{
				DocumentID: "fewshot-strong",
				Title:      "Diagnosis review: CAP",
				Snippet:    "STRONG_SHOT_SNIPPET",
				Score:      0.8,
			},
			{
				DocumentID: "fewshot-weak",
				Title:      "Diagnosis review: noise",
				Snippet:    "WEAK_SHOT_SNIPPET",
				Score:      0.1, // below the 0.3 floor
			},
		},
	}
	d := &FewShotDiagnostician{
		Base:  &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Shots: shots,
		K:     2,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": state.CaseID})
	if _, err := d.HandleMessage(context.Background(), msg, newFakeEnv()); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	user := mock.Calls[0].Messages[0].Content
	if !strings.Contains(user, "STRONG_SHOT_SNIPPET") {
		t.Fatalf("expected strong shot to be injected, got:\n%s", user)
	}
	if strings.Contains(user, "WEAK_SHOT_SNIPPET") {
		t.Fatalf("low-score shot should be dropped, got:\n%s", user)
	}
	// Only one example block should appear.
	if c := strings.Count(user, "--- example "); c != 1 {
		t.Fatalf("want exactly 1 example marker after low-score filter, got %d in:\n%s", c, user)
	}
}

func TestFewShot_FallsBackOnLLMError(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	mock := &llm.Mock{Err: errors.New("simulated network failure")}
	shots := &fakeKnowledge{Resp: []knowledge.Citation{{
		DocumentID: "fewshot-x",
		Title:      "shot",
		Snippet:    "Case: x — Approved diagnosis: y",
		Score:      0.7,
	}}}
	rule := &DiagnosticianAgent{}
	d := &FewShotDiagnostician{
		Base:  &LLMDiagnostician{Client: mock, Fallback: rule},
		Shots: shots,
		K:     3,
	}

	env := newFakeEnv()
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" || out[0].Type != "diagnosis_proposal" {
		t.Fatalf("rule-based fallback must still emit one proposal: %+v", out)
	}
	if !containsLog(env, "falling back") {
		t.Fatalf("expected fallback log line, got %v", env.logs)
	}
}
