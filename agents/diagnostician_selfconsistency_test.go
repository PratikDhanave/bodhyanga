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

// newSelfConsistencyScript wraps scriptedClient (defined in
// diagnostician_debate_test.go) so the self-consistency tests can hand each
// sample a distinct canned response without depending on goroutine ordering.
// The agent under test never makes a "chair" call, so the scriptedClient's
// chair-vs-specialist branching is moot here and we just feed scripts in
// order.
func newSelfConsistencyScript(replies ...scriptedReply) *scriptedClient {
	return &scriptedClient{scripts: append([]scriptedReply(nil), replies...)}
}

func TestSelfConsistency_VotesAcrossSamples(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	// 3 CAP, 2 Bronchitis. Winner must be CAP with votes=3, samples=5.
	cap1 := `{"diagnosis":"Community-acquired pneumonia","confidence":0.82,"rationale":"RLL infiltrate plus leukocytosis."}`
	cap2 := `{"diagnosis":"community-acquired pneumonia","confidence":0.78,"rationale":"Cough, fever, infiltrate."}`
	cap3 := `{"diagnosis":"Community-acquired pneumonia","confidence":0.88,"rationale":"Productive cough plus CXR infiltrate."}`
	br1 := `{"diagnosis":"Bronchitis","confidence":0.55,"rationale":"Cough without consolidation."}`
	br2 := `{"diagnosis":"Bronchitis","confidence":0.6,"rationale":"Productive cough only."}`

	client := newSelfConsistencyScript(
		scriptedReply{resp: &llm.Response{Content: cap1}},
		scriptedReply{resp: &llm.Response{Content: br1}},
		scriptedReply{resp: &llm.Response{Content: cap2}},
		scriptedReply{resp: &llm.Response{Content: br2}},
		scriptedReply{resp: &llm.Response{Content: cap3}},
	)
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	d := &SelfConsistencyDiagnostician{Base: base, Samples: 5, MaxConcurrency: 1}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got := len(client.Calls); got != 5 {
		t.Fatalf("LLM calls=%d, want 5", got)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" || out[0].Type != "diagnosis_proposal" {
		t.Fatalf("unexpected output: %+v", out)
	}

	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Original (un-normalised) winning diagnosis from the highest-confidence
	// vote for CAP — cap3 at 0.88.
	if p.Diagnosis != "Community-acquired pneumonia" {
		t.Fatalf("Diagnosis=%q, want %q", p.Diagnosis, "Community-acquired pneumonia")
	}
	// Mean confidence of the 3 CAP votes: (0.82+0.78+0.88)/3 = 0.8266...
	if p.Confidence < 0.82 || p.Confidence > 0.83 {
		t.Fatalf("Confidence=%v, want ~0.827", p.Confidence)
	}
	// Sampling temperature must have been applied so the samples could
	// diverge — every call should see the configured temperature, not the
	// base's default 0.2.
	for i, c := range client.Calls {
		if c.Temperature < 0.5 {
			t.Errorf("call %d Temperature=%v, want >=0.5 (sampling-friendly)", i, c.Temperature)
		}
	}
}

func TestSelfConsistency_TiebreakByMeanConfidence(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	// 2 CAP @ 0.9, 2 Bronchitis @ 0.6. Equal vote counts; CAP wins on mean
	// confidence.
	cap1 := `{"diagnosis":"CAP","confidence":0.9,"rationale":"r"}`
	cap2 := `{"diagnosis":"CAP","confidence":0.9,"rationale":"r"}`
	br1 := `{"diagnosis":"Bronchitis","confidence":0.6,"rationale":"r"}`
	br2 := `{"diagnosis":"Bronchitis","confidence":0.6,"rationale":"r"}`

	client := newSelfConsistencyScript(
		scriptedReply{resp: &llm.Response{Content: cap1}},
		scriptedReply{resp: &llm.Response{Content: br1}},
		scriptedReply{resp: &llm.Response{Content: cap2}},
		scriptedReply{resp: &llm.Response{Content: br2}},
	)
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	d := &SelfConsistencyDiagnostician{Base: base, Samples: 4, MaxConcurrency: 1}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, nil)
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Diagnosis != "CAP" {
		t.Fatalf("Tie-break winner Diagnosis=%q, want %q", p.Diagnosis, "CAP")
	}
	if p.Confidence < 0.89 || p.Confidence > 0.91 {
		t.Fatalf("Confidence=%v, want ~0.9", p.Confidence)
	}
}

func TestSelfConsistency_FallsBackOnMajorityErrors(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	// 3 errors of 5 — only 2 successes < 5/2 -> fall back.
	good := `{"diagnosis":"CAP","confidence":0.85,"rationale":"r"}`
	client := newSelfConsistencyScript(
		scriptedReply{err: errors.New("network down")},
		scriptedReply{resp: &llm.Response{Content: good}},
		scriptedReply{err: errors.New("network down")},
		scriptedReply{resp: &llm.Response{Content: good}},
		scriptedReply{err: errors.New("network down")},
	)
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	d := &SelfConsistencyDiagnostician{Base: base, Samples: 5, MaxConcurrency: 1}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("fallback should still emit one proposal: %+v", out)
	}
	if !containsLog(env, "falling back to rule-based") {
		t.Fatalf("expected fallback log line, got %v", env.logs)
	}
	if got := len(client.Calls); got != 5 {
		t.Fatalf("LLM calls=%d, want 5 (all samples attempted)", got)
	}
}

func TestSelfConsistency_AggregatesDifferentials(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	// Two winning CAP samples carry differentials; one losing Bronchitis
	// sample carries a third differential. All three (deduped, frequency-
	// ordered) should appear in the final union.
	cap1 := `{"diagnosis":"CAP","confidence":0.85,"differential":["Bronchitis","Heart failure"],"rationale":"r"}`
	cap2 := `{"diagnosis":"CAP","confidence":0.8,"differential":["Bronchitis","Pulmonary embolism"],"rationale":"r"}`
	br1 := `{"diagnosis":"Bronchitis","confidence":0.55,"differential":["Asthma exacerbation"],"rationale":"r"}`

	client := newSelfConsistencyScript(
		scriptedReply{resp: &llm.Response{Content: cap1}},
		scriptedReply{resp: &llm.Response{Content: br1}},
		scriptedReply{resp: &llm.Response{Content: cap2}},
	)
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	d := &SelfConsistencyDiagnostician{Base: base, Samples: 3, MaxConcurrency: 1}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, nil)
	out, err := d.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Diagnosis != "CAP" {
		t.Fatalf("Diagnosis=%q, want CAP", p.Diagnosis)
	}
	got := map[string]int{}
	for _, d := range p.Differential {
		got[d]++
	}
	for _, want := range []string{"Bronchitis", "Heart failure", "Pulmonary embolism", "Asthma exacerbation"} {
		if got[want] == 0 {
			t.Errorf("differential missing %q (got %v)", want, p.Differential)
		}
		if got[want] > 1 {
			t.Errorf("differential %q duplicated %d times", want, got[want])
		}
	}
	// Most frequent (Bronchitis appears in 2 samples) should come first.
	if len(p.Differential) == 0 || p.Differential[0] != "Bronchitis" {
		t.Errorf("frequency-ordered differential should start with Bronchitis, got %v", p.Differential)
	}
}

func TestSelfConsistency_PreservesCitations(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	// Three CAP samples but only the highest-confidence one carries citations.
	// The vote winner inside the CAP bucket is that highest-confidence vote,
	// so its citations must propagate.
	cap1 := `{"diagnosis":"CAP","confidence":0.7,"rationale":"r"}`
	cap2 := `{"diagnosis":"CAP","confidence":0.95,"rationale":"strongest","citations":[{"source":"IDSA","title":"CAP guideline","snippet":"infiltrate"}]}`
	cap3 := `{"diagnosis":"CAP","confidence":0.75,"rationale":"r"}`

	client := newSelfConsistencyScript(
		scriptedReply{resp: &llm.Response{Content: cap1}},
		scriptedReply{resp: &llm.Response{Content: cap2}},
		scriptedReply{resp: &llm.Response{Content: cap3}},
	)
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	d := &SelfConsistencyDiagnostician{Base: base, Samples: 3, MaxConcurrency: 1}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, nil)
	out, err := d.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(out[0].Content), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Citations) != 1 {
		t.Fatalf("expected 1 citation propagated from winning vote, got %d (%+v)", len(p.Citations), p.Citations)
	}
	if p.Citations[0].Source != "IDSA" {
		t.Fatalf("citation Source=%q, want IDSA", p.Citations[0].Source)
	}
	if p.Rationale != "strongest" {
		t.Fatalf("Rationale=%q, want the highest-confidence vote's rationale", p.Rationale)
	}
}

func TestSelfConsistency_DefaultSamplesIs5(t *testing.T) {
	_, encoded := newCaseStateJSON(t)

	good := `{"diagnosis":"CAP","confidence":0.85,"rationale":"r"}`
	// Feed 5 identical replies — if Samples defaults to 5 we expect exactly
	// 5 LLM calls.
	client := newSelfConsistencyScript(
		scriptedReply{resp: &llm.Response{Content: good}},
		scriptedReply{resp: &llm.Response{Content: good}},
		scriptedReply{resp: &llm.Response{Content: good}},
		scriptedReply{resp: &llm.Response{Content: good}},
		scriptedReply{resp: &llm.Response{Content: good}},
	)
	base := &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}}
	// Samples deliberately left as zero — exercises the default.
	d := &SelfConsistencyDiagnostician{Base: base, MaxConcurrency: 1}

	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, nil)
	if _, err := d.HandleMessage(context.Background(), msg, newFakeEnv()); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got := len(client.Calls); got != DefaultSelfConsistencySamples {
		t.Fatalf("default sample count = %d, want %d", got, DefaultSelfConsistencySamples)
	}
	if DefaultSelfConsistencySamples != 5 {
		t.Fatalf("DefaultSelfConsistencySamples = %d, want 5", DefaultSelfConsistencySamples)
	}
}
