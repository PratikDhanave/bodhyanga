package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// scriptedLLM is a stateful llm.Client that returns a queued sequence of
// responses/errors. Each Complete call pops the next entry; running off the
// end is a test bug and panics with a helpful message. Used by the reflexion
// tests where the first call drafts a proposal and subsequent calls are
// critiques.
type scriptedLLM struct {
	mu      sync.Mutex
	step    int
	entries []scriptedStep

	Calls []llm.Request
}

type scriptedStep struct {
	Resp *llm.Response
	Err  error
}

func newScriptedLLM(steps ...scriptedStep) *scriptedLLM {
	return &scriptedLLM{entries: steps}
}

func (s *scriptedLLM) Provider() string { return "mock" }
func (s *scriptedLLM) Model() string    { return "mock-model" }

func (s *scriptedLLM) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls = append(s.Calls, req)
	if s.step >= len(s.entries) {
		panic("scriptedLLM: ran out of canned responses (test bug)")
	}
	e := s.entries[s.step]
	s.step++
	if e.Err != nil {
		return nil, e.Err
	}
	return e.Resp, nil
}

func reflexionCaseState(t *testing.T) (medical.CaseState, string) {
	t.Helper()
	state := medical.CaseState{
		CaseID:       "reflexion-1",
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

const (
	initialDiagnosis = "Community-acquired pneumonia"
	revisedDiagnosis = "Aspiration pneumonia"
	secondRevision   = "Healthcare-associated pneumonia"
)

func draftResponse() *llm.Response {
	return &llm.Response{Content: `{"diagnosis":"` + initialDiagnosis + `","confidence":0.91,"differential":["CAP","Bronchitis"],"rationale":"RLL infiltrate plus leukocytosis."}`}
}

func critiqueVerified() *llm.Response {
	return &llm.Response{Content: `{"verified":true,"reason":"rationale grounded in CXR + WBC","verification_qa":[{"q":"What supports CAP?","a":"RLL infiltrate plus WBC 14.2"}]}`}
}

func critiqueRejectWithRevision(diag string) *llm.Response {
	body := `{"verified":false,"reason":"missed aspiration risk","verification_qa":[{"q":"alternatives?","a":"aspiration pneumonia"}],"revised_proposal":{"diagnosis":"` + diag + `","confidence":0.78,"differential":["Aspiration pneumonia","CAP"],"rationale":"RLL infiltrate plus aspiration history (revised)."}}`
	return &llm.Response{Content: body}
}

func critiqueRejectNoRevision() *llm.Response {
	return &llm.Response{Content: `{"verified":false,"reason":"thin evidence","revised_proposal":null}`}
}

func runReflexion(t *testing.T, d *ReflexionDiagnostician) ([]agent.Message, *fakeEnv) {
	t.Helper()
	_, encoded := reflexionCaseState(t)
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "reflexion-1"})
	env := newFakeEnv()
	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	return out, env
}

func decodeProposal(t *testing.T, m agent.Message) medical.DiagnosisProposal {
	t.Helper()
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(m.Content), &p); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	return p
}

func TestReflexion_VerifiedFirstRoundEmitsOriginal(t *testing.T) {
	client := newScriptedLLM(
		scriptedStep{Resp: draftResponse()},
		scriptedStep{Resp: critiqueVerified()},
	)
	d := &ReflexionDiagnostician{
		Base: &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
	}

	out, _ := runReflexion(t, d)
	if len(out) != 1 || out[0].To != "reasoning_verifier" || out[0].Type != "diagnosis_proposal" {
		t.Fatalf("expected one diagnosis_proposal to reasoning_verifier, got %+v", out)
	}
	p := decodeProposal(t, out[0])
	if p.Diagnosis != initialDiagnosis {
		t.Fatalf("diagnosis=%q, want original %q", p.Diagnosis, initialDiagnosis)
	}
	if len(client.Calls) != 2 {
		t.Fatalf("want 2 LLM calls (draft + 1 critique), got %d", len(client.Calls))
	}
	// Critique call: assert CacheSystem=true and verifier system prompt.
	if !client.Calls[1].CacheSystem {
		t.Errorf("critique call should have CacheSystem=true")
	}
	if !strings.Contains(client.Calls[1].System, "clinical reasoning verifier") {
		t.Errorf("critique system prompt missing verifier framing: %q", client.Calls[1].System)
	}
	if client.Calls[1].JSONSchema == nil || client.Calls[1].JSONSchema.Name != "critique_diagnosis" {
		t.Errorf("critique call should request critique_diagnosis schema")
	}
}

func TestReflexion_UnverifiedRoundAdoptsRevised(t *testing.T) {
	client := newScriptedLLM(
		scriptedStep{Resp: draftResponse()},
		scriptedStep{Resp: critiqueRejectWithRevision(revisedDiagnosis)},
	)
	d := &ReflexionDiagnostician{
		Base: &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
	}

	out, env := runReflexion(t, d)
	p := decodeProposal(t, out[0])
	if p.Diagnosis != revisedDiagnosis {
		t.Fatalf("diagnosis=%q, want revised %q", p.Diagnosis, revisedDiagnosis)
	}
	if !containsLog(env, "REVISED") {
		t.Errorf("expected REVISED log line, got %v", env.logs)
	}
}

func TestReflexion_CritiqueErrorKeepsOriginal(t *testing.T) {
	client := newScriptedLLM(
		scriptedStep{Resp: draftResponse()},
		scriptedStep{Err: errors.New("critique transport boom")},
	)
	d := &ReflexionDiagnostician{
		Base: &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
	}

	out, env := runReflexion(t, d)
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("expected best-effort emission to verifier, got %+v", out)
	}
	p := decodeProposal(t, out[0])
	if p.Diagnosis != initialDiagnosis {
		t.Fatalf("diagnosis=%q, want original %q (critique errored)", p.Diagnosis, initialDiagnosis)
	}
	if !containsLog(env, "critique failed") {
		t.Errorf("expected critique failed log, got %v", env.logs)
	}
}

func TestReflexion_InitialFailsFallsBack(t *testing.T) {
	client := newScriptedLLM(
		scriptedStep{Err: errors.New("network down")},
	)
	d := &ReflexionDiagnostician{
		Base: &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
	}

	out, env := runReflexion(t, d)
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("rule fallback should emit one proposal, got %+v", out)
	}
	if !containsLog(env, "falling back to rule-based") {
		t.Errorf("expected rule-fallback log, got %v", env.logs)
	}
	// The rule fallback diagnoses CAP from the (synthetic) state.
	p := decodeProposal(t, out[0])
	if p.Diagnosis == "" {
		t.Fatalf("fallback emitted empty diagnosis")
	}
}

func TestReflexion_MultipleRoundsConverge(t *testing.T) {
	client := newScriptedLLM(
		scriptedStep{Resp: draftResponse()},
		scriptedStep{Resp: critiqueRejectWithRevision(revisedDiagnosis)},  // round 1: revise
		scriptedStep{Resp: critiqueRejectWithRevision(secondRevision)},    // round 2: revise again
	)
	d := &ReflexionDiagnostician{
		Base:      &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
		MaxRounds: 2,
	}

	out, _ := runReflexion(t, d)
	p := decodeProposal(t, out[0])
	if p.Diagnosis != secondRevision {
		t.Fatalf("diagnosis=%q, want second revision %q", p.Diagnosis, secondRevision)
	}
	if len(client.Calls) != 3 {
		t.Fatalf("want 3 LLM calls (draft + 2 critiques), got %d", len(client.Calls))
	}
}

func TestReflexion_MultipleRoundsConverge_SecondVerifies(t *testing.T) {
	// Extra coverage: round 1 revises, round 2 verifies → emit round-1 revision.
	client := newScriptedLLM(
		scriptedStep{Resp: draftResponse()},
		scriptedStep{Resp: critiqueRejectWithRevision(revisedDiagnosis)},
		scriptedStep{Resp: critiqueVerified()},
	)
	d := &ReflexionDiagnostician{
		Base:      &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
		MaxRounds: 2,
	}

	out, _ := runReflexion(t, d)
	p := decodeProposal(t, out[0])
	if p.Diagnosis != revisedDiagnosis {
		t.Fatalf("diagnosis=%q, want revised %q (second round verified it)", p.Diagnosis, revisedDiagnosis)
	}
	if len(client.Calls) != 3 {
		t.Fatalf("want 3 LLM calls, got %d", len(client.Calls))
	}
}

func TestReflexion_RejectWithoutRevisionKeepsOriginal(t *testing.T) {
	// Defensive: verified=false but revised_proposal=null → keep original, stop.
	client := newScriptedLLM(
		scriptedStep{Resp: draftResponse()},
		scriptedStep{Resp: critiqueRejectNoRevision()},
	)
	d := &ReflexionDiagnostician{
		Base:      &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
		MaxRounds: 3,
	}

	out, env := runReflexion(t, d)
	p := decodeProposal(t, out[0])
	if p.Diagnosis != initialDiagnosis {
		t.Fatalf("diagnosis=%q, want original (no revision supplied)", p.Diagnosis)
	}
	if len(client.Calls) != 2 {
		t.Fatalf("want exactly 2 calls (no further rounds), got %d", len(client.Calls))
	}
	if !containsLog(env, "no revision supplied") {
		t.Errorf("expected no-revision log, got %v", env.logs)
	}
}

func TestReflexion_DefaultMaxRoundsIs1(t *testing.T) {
	// MaxRounds left zero → exactly one critique call. We script three critiques;
	// only the first should be consumed.
	client := newScriptedLLM(
		scriptedStep{Resp: draftResponse()},
		scriptedStep{Resp: critiqueRejectWithRevision(revisedDiagnosis)},
	)
	d := &ReflexionDiagnostician{
		Base: &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
	}

	out, _ := runReflexion(t, d)
	if len(client.Calls) != 2 {
		t.Fatalf("default MaxRounds=1 → want 2 calls (draft + 1 critique), got %d", len(client.Calls))
	}
	p := decodeProposal(t, out[0])
	// One revision round happened and was the last.
	if p.Diagnosis != revisedDiagnosis {
		t.Fatalf("diagnosis=%q, want revised %q", p.Diagnosis, revisedDiagnosis)
	}
}
