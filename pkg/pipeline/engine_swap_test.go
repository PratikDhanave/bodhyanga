package pipeline_test

import (
	"context"
	"encoding/json"
	"iter"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/auditbridge"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

// fakeLLM builds a MAF agent whose "model" replies with a fixed body —
// exercising the full agent plumbing (middleware, structured output) with
// zero network (constitution II / spec FR-012).
func fakeLLM(b *admit.Boundary, body string) *agent.Agent {
	run := func(ctx context.Context, msgs []*message.Message, opts ...agent.Option) iter.Seq2[*agent.ResponseUpdate, error] {
		return func(yield func(*agent.ResponseUpdate, error) bool) {
			yield(&agent.ResponseUpdate{
				Contents: []message.Content{&message.TextContent{Text: body}},
			}, nil)
		}
	}
	cfg := agent.Config{ID: "diagnostician-llm"}
	if b != nil {
		cfg.Middlewares = []agent.Middleware{b.Middleware("diagnostician", admit.TypeWorkup)}
	}
	return agent.New(agent.ProviderConfig{
		ProviderName: "fake-llm",
		Run:          run,
		Format:       func(any) (agent.ResponseFormat, error) { return agent.ResponseFormat{}, nil },
		Unmarshal: func(_ agent.ResponseFormat, data []byte, v any) error {
			return json.Unmarshal(data, v)
		},
	}, cfg)
}

func runEngine(t *testing.T, dxBody string) (*pipeline.Outcome, *audit.InMemory, error) {
	t.Helper()
	rec := audit.NewInMemory()
	b := admit.New(admit.AllowAll(), rec, nil)
	dxCfg := agents.DiagnosticianConfig{Tau: 0.85, MaxRounds: 3}
	cfg := pipeline.Config{Boundary: b}
	if dxBody != "" {
		cfg.Diagnostician = agents.NewLLMDiagnostician("diagnostician",
			fakeLLM(b, dxBody), dxCfg, b.Guard, nil)
	}
	r, err := pipeline.NewRunner(cfg, rec)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), agents.PatientCase{
		CaseID: "case-swap", Symptoms: []string{"chest pain"},
	}, "")
	return out, rec, err
}

const confidentACS = `{"diagnosis":"acute coronary syndrome","confidence":0.93,"evidence":["troponin=elevated"]}`

// TestEngineSwapIsConfigurationOnly runs the SAME case through the rule
// engine and a fake LLM engine and asserts structurally identical outcomes
// and trail shapes — the graph never changed (SC-004).
func TestEngineSwapIsConfigurationOnly(t *testing.T) {
	ruleOut, ruleRec, err := runEngine(t, "")
	if err != nil {
		t.Fatal(err)
	}
	llmOut, llmRec, err := runEngine(t, confidentACS)
	if err != nil {
		t.Fatal(err)
	}

	for name, out := range map[string]*pipeline.Outcome{"rule": ruleOut, "llm": llmOut} {
		if out.Plan == nil {
			t.Fatalf("%s engine produced no care plan", name)
		}
		if out.Plan.Diagnosis != "acute coronary syndrome" {
			t.Errorf("%s diagnosis = %q", name, out.Plan.Diagnosis)
		}
	}
	// Same trail SHAPE: every hop admission the rule engine incurred exists
	// for the LLM engine too (the LLM run resolves in round 1, so the
	// loop-back hops differ in count but not in kind).
	for _, evs := range [][]audit.Event{ruleRec.List(), llmRec.List()} {
		if err := auditbridge.CheckHopCompleteness(evs); err != nil {
			t.Errorf("invariant 1: %v", err)
		}
		if err := auditbridge.CheckOrderedSubsequence(evs, []auditbridge.Row{
			{Kind: audit.KindPolicyDecision, Decision: "allow", AgentID: "diagnostician", Type: admit.TypeWorkup},
			{Kind: audit.KindPolicyDecision, Decision: "allow", AgentID: "care_plan", Type: admit.TypeDiagnosisResult},
		}); err != nil {
			t.Errorf("trail shape: %v", err)
		}
	}
	// The provider call itself was admission-guarded (middleware row exists
	// only on the LLM path — defense in depth).
	llmAdmissions := 0
	for _, e := range llmRec.List() {
		if e.Kind == audit.KindPolicyDecision && e.AgentID == "diagnostician" {
			llmAdmissions++
		}
	}
	if llmAdmissions < 2 { // hop guard + provider middleware
		t.Errorf("LLM path admissions = %d, want ≥2 (hop + provider run)", llmAdmissions)
	}
}

// TestMalformedModelOutputNeverBecomesADiagnosis (FR-011).
func TestMalformedModelOutputNeverBecomesADiagnosis(t *testing.T) {
	cases := map[string]string{
		"not json":       `the patient probably has a cold`,
		"empty diag":     `{"diagnosis":"","confidence":0.9}`,
		"bad confidence": `{"diagnosis":"acs","confidence":7.5}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			out, rec, err := runEngine(t, body)
			if err == nil && out != nil && out.Plan != nil {
				t.Fatal("malformed model output produced a care plan")
			}
			hasAgentError := false
			for _, e := range rec.List() {
				if e.Kind == audit.KindAgentError && e.AgentID == "diagnostician" {
					hasAgentError = true
					if len(e.ErrorText) == 0 {
						t.Error("agent_error without error text")
					}
				}
			}
			if !hasAgentError {
				t.Error("model failure not audited as agent_error")
			}
		})
	}
}
