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

// newCascadeForTest wires a CascadeDiagnostician backed by two distinct
// llm.Mock clients so each tier records its own Calls and can be inspected
// independently after HandleMessage.
func newCascadeForTest(small, large *llm.Mock, escalateAt float64) *CascadeDiagnostician {
	return &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{
			Client:   small,
			Fallback: &DiagnosticianAgent{},
		},
		LargeTier: &LLMDiagnostician{
			Client:   large,
			Fallback: &DiagnosticianAgent{},
		},
		EscalateAt: escalateAt,
	}
}

func TestCascade_HighConfidenceSkipsLarge(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	smallResp := `{"diagnosis":"Community-acquired pneumonia","confidence":0.8,"differential":["CAP"],"rationale":"clear infiltrate"}`
	small := &llm.Mock{
		Resp:         &llm.Response{Content: smallResp},
		ProviderName: "mock",
		ModelName:    "small-cheap",
	}
	large := &llm.Mock{
		Resp:         &llm.Response{Content: `{"diagnosis":"should-not-emit","confidence":0.99,"rationale":"x"}`},
		ProviderName: "mock",
		ModelName:    "large-expensive",
	}
	cascade := newCascadeForTest(small, large, 0.6)

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := cascade.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(small.Calls) != 1 {
		t.Fatalf("small tier should be called exactly once, got %d", len(small.Calls))
	}
	if len(large.Calls) != 0 {
		t.Fatalf("large tier should NOT be called when small clears threshold, got %d", len(large.Calls))
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("expected one diagnosis_proposal to reasoning_verifier, got %+v", out)
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if p.Diagnosis != "Community-acquired pneumonia" {
		t.Fatalf("expected small-tier proposal forwarded, got %q", p.Diagnosis)
	}
}

func TestCascade_LowConfidenceEscalates(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	smallResp := `{"diagnosis":"Maybe bronchitis","confidence":0.3,"rationale":"unsure"}`
	largeResp := `{"diagnosis":"Community-acquired pneumonia","confidence":0.85,"differential":["CAP"],"rationale":"strong infiltrate evidence"}`
	// Use MinConfidence=0.0 on both tiers via the cascade constructor so the
	// small tier's 0.3 proposal is not rejected internally — we want the
	// cascade itself to make the escalation decision.
	small := &llm.Mock{Resp: &llm.Response{Content: smallResp}}
	large := &llm.Mock{Resp: &llm.Response{Content: largeResp}}
	cascade := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: small, Fallback: &DiagnosticianAgent{}, MinConfidence: 0.01},
		LargeTier: &LLMDiagnostician{Client: large, Fallback: &DiagnosticianAgent{}, MinConfidence: 0.01},
		EscalateAt: 0.6,
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := cascade.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(small.Calls) != 1 {
		t.Fatalf("small tier should be called once, got %d", len(small.Calls))
	}
	if len(large.Calls) != 1 {
		t.Fatalf("large tier should be called once after escalation, got %d", len(large.Calls))
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 message out, got %d", len(out))
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if p.Diagnosis != "Community-acquired pneumonia" {
		t.Fatalf("expected large-tier proposal forwarded, got %q", p.Diagnosis)
	}
	if p.Confidence != 0.85 {
		t.Fatalf("expected large-tier confidence 0.85, got %v", p.Confidence)
	}
	if !containsLog(env, "escalating to large") {
		t.Fatalf("expected escalation log line, got %v", env.logs)
	}
}

func TestCascade_SmallFallsBackEscalatesToLarge(t *testing.T) {
	// Heuristic under test: when the small tier's LLM errors, its wrapped
	// LLMDiagnostician runs the rule-based fallback, which on an
	// information-poor CaseState lands in the undifferentiated branch with
	// confidence 0.45. The cascade treats that sub-threshold confidence as
	// "low" and escalates to the large tier — exactly what we want, because
	// the rule-based output already lost the case to the cheap LLM.
	//
	// We deliberately build a sparse CaseState (no chest_xray, no CBC, no
	// RPM alerts) so DiagnosticianAgent.inferDiagnosis takes the 0.45
	// fallthrough branch rather than the 0.87 CAP branch.
	state := medical.CaseState{
		CaseID:       "cascade-fallback-1",
		Presentation: "Vague malaise",
	}
	encoded := medical.EncodeState(state)
	small := &llm.Mock{Err: errors.New("small-tier network down")}
	largeResp := `{"diagnosis":"Community-acquired pneumonia","confidence":0.88,"rationale":"high-quality LLM diagnosis"}`
	large := &llm.Mock{Resp: &llm.Response{Content: largeResp}}
	cascade := newCascadeForTest(small, large, 0.6)

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := cascade.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(small.Calls) != 1 {
		t.Fatalf("small tier LLM should still be invoked once, got %d", len(small.Calls))
	}
	if len(large.Calls) != 1 {
		t.Fatalf("large tier should be invoked once after small fallback, got %d", len(large.Calls))
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 message out, got %d", len(out))
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if p.Diagnosis != "Community-acquired pneumonia" || p.Confidence != 0.88 {
		t.Fatalf("expected large-tier proposal forwarded, got %+v", p)
	}
	if !containsLog(env, "escalating to large") {
		t.Fatalf("expected escalation log line on rule-based fallback, got %v", env.logs)
	}
}

func TestCascade_DefaultEscalateAtIs0_6(t *testing.T) {
	_, encoded := newCaseStateJSON(t)
	// Just below 0.6 — must escalate.
	smallResp := `{"diagnosis":"Probable CAP","confidence":0.59,"rationale":"borderline"}`
	largeResp := `{"diagnosis":"Community-acquired pneumonia","confidence":0.9,"rationale":"clear"}`
	small := &llm.Mock{Resp: &llm.Response{Content: smallResp}}
	large := &llm.Mock{Resp: &llm.Response{Content: largeResp}}
	// EscalateAt left at zero — should use defaultCascadeEscalateAt = 0.6.
	cascade := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: small, Fallback: &DiagnosticianAgent{}, MinConfidence: 0.01},
		LargeTier: &LLMDiagnostician{Client: large, Fallback: &DiagnosticianAgent{}, MinConfidence: 0.01},
	}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	out, err := cascade.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(large.Calls) != 1 {
		t.Fatalf("0.59 must trigger escalation under the 0.6 default, got large calls=%d", len(large.Calls))
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if p.Confidence != 0.9 {
		t.Fatalf("expected large-tier proposal forwarded, got %+v", p)
	}

	// And exactly-at-threshold (0.60) should NOT escalate.
	small2 := &llm.Mock{Resp: &llm.Response{Content: `{"diagnosis":"CAP","confidence":0.60,"rationale":"at-threshold"}`}}
	large2 := &llm.Mock{Resp: &llm.Response{Content: `{"diagnosis":"never","confidence":0.99,"rationale":"x"}`}}
	cascade2 := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: small2, Fallback: &DiagnosticianAgent{}, MinConfidence: 0.01},
		LargeTier: &LLMDiagnostician{Client: large2, Fallback: &DiagnosticianAgent{}, MinConfidence: 0.01},
	}
	if _, err := cascade2.HandleMessage(context.Background(), msg, newFakeEnv()); err != nil {
		t.Fatalf("HandleMessage at-threshold: %v", err)
	}
	if len(large2.Calls) != 0 {
		t.Fatalf("confidence == EscalateAt should NOT escalate, got large calls=%d", len(large2.Calls))
	}
}
