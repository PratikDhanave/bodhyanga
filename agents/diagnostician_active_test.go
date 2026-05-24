package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// newActiveLearningCaseStateJSON returns the canonical CAP fixture used by
// the active-learning tests. The rule-based DiagnosticianAgent emits
// "Community-acquired pneumonia" (confidence 0.87) on this CaseState because
// both the chest x-ray ("RLL infiltrate") and CBC ("WBC 14.2") trip the rule
// branch in inferDiagnosis.
func newActiveLearningCaseStateJSON(t *testing.T) (medical.CaseState, string) {
	t.Helper()
	return newCaseStateJSON(t)
}

func TestActiveLearning_AgreementEmitsOnlyLLM(t *testing.T) {
	_, encoded := newActiveLearningCaseStateJSON(t)

	// LLM returns exactly what the rule-based agent emits for this fixture
	// (CAP), so token-Jaccard == 1.0 and the agent must emit only the LLM
	// proposal toward reasoning_verifier — no human_review traffic.
	respJSON := `{"diagnosis":"Community-acquired pneumonia","confidence":0.9,"rationale":"RLL infiltrate plus leukocytosis."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	d := &ActiveLearningDiagnostician{
		LLM:  &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Rule: &DiagnosticianAgent{},
		HITL: true, // even with HITL on, agreement must NOT route
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output (LLM only), got %d: %+v", len(out), out)
	}
	if out[0].To != "reasoning_verifier" {
		t.Fatalf("output To=%q, want reasoning_verifier", out[0].To)
	}
	if out[0].Type != "diagnosis_proposal" {
		t.Fatalf("output Type=%q, want diagnosis_proposal", out[0].Type)
	}
	if !containsLog(env, "AGREE") {
		t.Fatalf("expected AGREE log line, got %v", env.logs)
	}
}

func TestActiveLearning_DisagreementWithHITLEmitsBothMessages(t *testing.T) {
	_, encoded := newActiveLearningCaseStateJSON(t)

	// Rule-based path infers CAP for this fixture; LLM dissents with PE.
	respJSON := `{"diagnosis":"Acute pulmonary embolism","confidence":0.78,"rationale":"Sudden dyspnea, tachycardia, hypoxia."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	d := &ActiveLearningDiagnostician{
		LLM:  &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Rule: &DiagnosticianAgent{},
		HITL: true,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", encoded, map[string]any{"case_id": "x", "patient_id": "p1"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 outputs (LLM + review), got %d: %+v", len(out), out)
	}

	// First message: the LLM proposal toward reasoning_verifier.
	if out[0].To != "reasoning_verifier" || out[0].Type != "diagnosis_proposal" {
		t.Fatalf("out[0] should be LLM->verifier, got To=%q Type=%q", out[0].To, out[0].Type)
	}
	var llmP medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &llmP); err != nil {
		t.Fatalf("decode LLM proposal: %v", err)
	}
	if llmP.Diagnosis != "Acute pulmonary embolism" {
		t.Fatalf("LLM proposal Diagnosis=%q, want Acute pulmonary embolism", llmP.Diagnosis)
	}

	// Second message: the HITL review request.
	rev := out[1]
	if rev.To != "human_review" {
		t.Fatalf("review To=%q, want human_review", rev.To)
	}
	if rev.Type != "review_request" {
		t.Fatalf("review Type=%q, want review_request", rev.Type)
	}
	if got := metadataString(rev.Metadata, "hitl_kind"); got != activeLearningReviewKind {
		t.Fatalf("hitl_kind=%q, want %q", got, activeLearningReviewKind)
	}
	if got := metadataString(rev.Metadata, "hitl_next_to"); got != "engagement" {
		t.Fatalf("hitl_next_to=%q, want engagement", got)
	}
	if got := metadataString(rev.Metadata, "hitl_next_type"); got != activeLearningNextType {
		t.Fatalf("hitl_next_type=%q, want %q", got, activeLearningNextType)
	}
	if got := metadataString(rev.Metadata, "hitl_priority"); got != "normal" {
		t.Fatalf("hitl_priority=%q, want normal", got)
	}
	subj := metadataString(rev.Metadata, "hitl_subject")
	if !strings.Contains(subj, "Acute pulmonary embolism") || !strings.Contains(subj, "Community-acquired pneumonia") {
		t.Fatalf("hitl_subject=%q, want it to name both diagnoses", subj)
	}
	if got := metadataFloat(rev.Metadata, "hitl_sla_minutes", -1); got != float64(activeLearningSLAMinutes) {
		t.Fatalf("hitl_sla_minutes=%v, want %d", got, activeLearningSLAMinutes)
	}
	if got := metadataString(rev.Metadata, "active_learning_detail"); !strings.Contains(got, "Acute pulmonary embolism") || !strings.Contains(got, "Community-acquired pneumonia") {
		t.Fatalf("active_learning_detail missing one of the proposals: %q", got)
	}
	// Patient/case correlation must survive into the review.
	if got := metadataString(rev.Metadata, "case_id"); got != "x" {
		t.Fatalf("review case_id=%q, want x", got)
	}
	if got := metadataString(rev.Metadata, "patient_id"); got != "p1" {
		t.Fatalf("review patient_id=%q, want p1", got)
	}

	// Payload is JSON of {llm, rule, case_state, jaccard, agreement_threshold}.
	var payload struct {
		LLM       medical.DiagnosisProposal `json:"llm"`
		Rule      medical.DiagnosisProposal `json:"rule"`
		CaseState medical.CaseState         `json:"case_state"`
		Jaccard   float64                   `json:"jaccard"`
	}
	if err := json.Unmarshal([]byte(rev.Content), &payload); err != nil {
		t.Fatalf("decode review payload: %v", err)
	}
	if payload.LLM.Diagnosis != "Acute pulmonary embolism" {
		t.Fatalf("payload.LLM.Diagnosis=%q", payload.LLM.Diagnosis)
	}
	if payload.Rule.Diagnosis != "Community-acquired pneumonia" {
		t.Fatalf("payload.Rule.Diagnosis=%q", payload.Rule.Diagnosis)
	}
	if payload.CaseState.CaseID != "test-case-1" {
		t.Fatalf("payload.CaseState.CaseID=%q", payload.CaseState.CaseID)
	}
	if payload.Jaccard >= 0.6 {
		t.Fatalf("payload.Jaccard=%v, want <0.6 (disagreement)", payload.Jaccard)
	}
}

func TestActiveLearning_DisagreementWithoutHITLLogsOnly(t *testing.T) {
	_, encoded := newActiveLearningCaseStateJSON(t)

	respJSON := `{"diagnosis":"Acute pulmonary embolism","confidence":0.78,"rationale":"PE features."}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	d := &ActiveLearningDiagnostician{
		LLM:  &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Rule: &DiagnosticianAgent{},
		HITL: false,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("HITL off: want 1 output (LLM only), got %d: %+v", len(out), out)
	}
	if out[0].To != "reasoning_verifier" {
		t.Fatalf("LLM proposal To=%q, want reasoning_verifier", out[0].To)
	}
	if !containsLog(env, "NOT routed") {
		t.Fatalf("expected 'NOT routed' log line, got %v", env.logs)
	}
}

func TestActiveLearning_LLMFailureFallsBack(t *testing.T) {
	_, encoded := newActiveLearningCaseStateJSON(t)

	// LLM errors → LLMDiagnostician.runFallback emits the rule-based
	// proposal. The rule comparator also emits the rule-based proposal.
	// Both sides therefore agree, no review is routed, and exactly one
	// message lands on reasoning_verifier.
	mock := &llm.Mock{Err: errors.New("network down")}
	d := &ActiveLearningDiagnostician{
		LLM:  &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Rule: &DiagnosticianAgent{},
		HITL: true,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output after LLM fallback agreement, got %d: %+v", len(out), out)
	}
	if out[0].To != "reasoning_verifier" {
		t.Fatalf("To=%q, want reasoning_verifier", out[0].To)
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Diagnosis != "Community-acquired pneumonia" {
		t.Fatalf("Diagnosis=%q, want Community-acquired pneumonia (rule fallback)", p.Diagnosis)
	}
	if !containsLog(env, "falling back to rule-based") {
		t.Fatalf("expected LLM fallback log line, got %v", env.logs)
	}
	if !containsLog(env, "AGREE") {
		t.Fatalf("expected AGREE log after fallback, got %v", env.logs)
	}
}

func TestActiveLearning_AgreementThresholdRespected(t *testing.T) {
	_, encoded := newActiveLearningCaseStateJSON(t)

	// Rule emits "Community-acquired pneumonia". LLM returns the same
	// phrase modulo a hyphen. tokenJaccard treats the hyphen as a token
	// boundary so the two strings produce IDENTICAL token sets (jaccard=1.0)
	// even at threshold 0.99 — that case must AGREE.
	respJSONIdentical := `{"diagnosis":"Community acquired pneumonia","confidence":0.9,"rationale":"r"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSONIdentical}}
	d := &ActiveLearningDiagnostician{
		LLM:                &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Rule:               &DiagnosticianAgent{},
		HITL:               true,
		AgreementThreshold: 0.99,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("punctuation-only diff should still agree at 0.99; got %d outputs", len(out))
	}

	// Now mutate one token so the Jaccard drops below 0.99 but is well
	// above the 0.6 default — the threshold MUST be what gates routing,
	// not the default. Rule still emits CAP; LLM swaps "acquired" for
	// "associated".
	respJSONClose := `{"diagnosis":"Community associated pneumonia","confidence":0.9,"rationale":"r"}`
	mock2 := &llm.Mock{Resp: &llm.Response{Content: respJSONClose}}
	d2 := &ActiveLearningDiagnostician{
		LLM:                &LLMDiagnostician{Client: mock2, Fallback: &DiagnosticianAgent{}},
		Rule:               &DiagnosticianAgent{},
		HITL:               true,
		AgreementThreshold: 0.99,
	}
	out2, err := d2.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage (close-but-not-equal): %v", err)
	}
	if len(out2) != 2 {
		t.Fatalf("threshold=0.99 with one swapped token should route to review; got %d outputs", len(out2))
	}
	if out2[1].To != "human_review" {
		t.Fatalf("out2[1].To=%q, want human_review", out2[1].To)
	}
}

func TestTokenJaccard_BasicCases(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want float64 // exact for trivial cases
	}{
		{"both empty", "", "", 1.0},
		{"only a empty", "", "pneumonia", 0.0},
		{"only b empty", "pneumonia", "", 0.0},
		{"identical", "Community-acquired pneumonia", "Community-acquired pneumonia", 1.0},
		{"case insensitive", "PNEUMONIA", "pneumonia", 1.0},
		{"hyphen equals space", "Community-acquired pneumonia", "community acquired pneumonia", 1.0},
		{"disjoint", "pulmonary embolism", "myocardial infarction", 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tokenJaccard(c.a, c.b)
			if got != c.want {
				t.Fatalf("tokenJaccard(%q,%q)=%v, want %v", c.a, c.b, got, c.want)
			}
		})
	}

	// Partial-overlap sanity check: 2-of-3 tokens shared → 2/4 = 0.5.
	got := tokenJaccard("community acquired pneumonia", "hospital acquired pneumonia")
	if got < 0.49 || got > 0.51 {
		t.Fatalf("partial overlap: tokenJaccard=%v, want ~0.5", got)
	}
}
