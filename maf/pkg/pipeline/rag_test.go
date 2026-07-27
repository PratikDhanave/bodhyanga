package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/embeddings"
	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

const ragCorpus = "../agents/testdata/corpus.json"

// capturingLLM is a fake model that records the prompt it received, so tests
// can assert the Evidence block was (or was not) injected.
func capturingLLM(b *admit.Boundary, body string, gotPrompt *string) *agent.Agent {
	run := func(ctx context.Context, msgs []*message.Message, opts ...agent.Option) iter.Seq2[*agent.ResponseUpdate, error] {
		var sb strings.Builder
		for _, m := range msgs {
			for _, c := range m.Contents {
				if tc, ok := c.(*message.TextContent); ok {
					sb.WriteString(tc.Text)
				}
			}
		}
		*gotPrompt = sb.String()
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

// runRAG runs the chest-pain case through the pipeline with an LLM engine and,
// optionally, a retriever. It returns the outcome, the recovered terminal
// DiagnosisResult, and the prompt the model saw.
func runRAG(t *testing.T, body string, r *agents.Retriever) (*pipeline.Outcome, agents.DiagnosisResult, string) {
	t.Helper()
	rec := audit.NewInMemory()
	b := admit.New(admit.AllowAll(), rec, nil)
	var prompt string
	cfg := pipeline.Config{
		Boundary: b,
		Diagnostician: agents.NewLLMDiagnostician("diagnostician",
			capturingLLM(b, body, &prompt), agents.DiagnosticianConfig{Tau: 0.85, MaxRounds: 3}, b.Guard, r),
	}
	runner, err := pipeline.NewRunner(cfg, rec)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runner.Run(context.Background(), agents.PatientCase{
		CaseID: "case-rag", Symptoms: []string{"acute chest pain", "diaphoresis"},
		History: []string{"hypertension"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	return out, terminalDiagnosis(out), prompt
}

// terminalDiagnosis recovers the DiagnosisResult from the event stream the same
// way the Runner's eval recorder does (runner.go).
func terminalDiagnosis(out *pipeline.Outcome) agents.DiagnosisResult {
	var d agents.DiagnosisResult
	for _, evt := range out.Events {
		if inv, ok := evt.(workflow.ExecutorInvokedEvent); ok {
			if m, ok := inv.Message.(agents.DiagnosisResult); ok {
				d = m
			}
		}
	}
	return d
}

func ragRetriever(t *testing.T) *agents.Retriever {
	t.Helper()
	r, err := agents.LoadRetriever(ragCorpus, &embeddings.Mock{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

const confidentACSNoCite = `{"diagnosis":"acute coronary syndrome","confidence":0.93,"evidence":["troponin elevated"]}`

// T008: with a corpus, the model prompt carries an Evidence block and the
// diagnosis carries retrieved citations (SC-001, FR-002, FR-003).
func TestRAG_GroundsPromptAndSurfacesCitations(t *testing.T) {
	_, dx, prompt := runRAG(t, confidentACSNoCite, ragRetriever(t))

	if !strings.Contains(prompt, "Evidence:") {
		t.Errorf("prompt missing Evidence block:\n%s", prompt)
	}
	if len(dx.Citations) == 0 {
		t.Fatal("diagnosis carried no citations with a matching corpus")
	}
	if dx.Citations[0].Source != "demo-synth" {
		t.Errorf("citation source = %q, want demo-synth", dx.Citations[0].Source)
	}
}

// T008: no corpus (nil retriever) → no Evidence block, zero citations, output
// otherwise identical (SC-001).
func TestRAG_DisabledLeavesNodeUnchanged(t *testing.T) {
	out, dx, prompt := runRAG(t, confidentACSNoCite, nil)

	if strings.Contains(prompt, "Evidence:") {
		t.Errorf("RAG-off prompt should carry no Evidence block:\n%s", prompt)
	}
	if len(dx.Citations) != 0 {
		t.Errorf("RAG-off diagnosis carried %d citations, want 0", len(dx.Citations))
	}
	if out.Plan == nil || out.Plan.Diagnosis != "acute coronary syndrome" {
		t.Errorf("RAG-off case did not finalize as expected: %+v", out.Plan)
	}
}

// T008: when the model returns its own citations, those are kept over the
// retrieved set (FR-003 reconciliation).
func TestRAG_ModelCitationsWin(t *testing.T) {
	body := `{"diagnosis":"acute coronary syndrome","confidence":0.93,` +
		`"evidence":["troponin"],"citations":[{"source":"model-cite","title":"M","snippet":"s","score":0.5}]}`
	_, dx, _ := runRAG(t, body, ragRetriever(t))

	if len(dx.Citations) != 1 || dx.Citations[0].Source != "model-cite" {
		t.Errorf("want the model's own citation kept, got %+v", dx.Citations)
	}
}

// T015: an erroring embedder degrades safely — the diagnosis is still produced
// with zero citations and no case failure (SC-003, FR-004/FR-005).
func TestRAG_EmbedderErrorFailsOpen(t *testing.T) {
	r, err := agents.LoadRetriever(ragCorpus, &embeddings.Mock{Err: errors.New("embedder down")}, 4)
	if err == nil {
		// Index at load itself embeds; a permanently-erroring mock fails there.
		// Build with a good mock, then poison at query time instead.
		mock := &embeddings.Mock{}
		r, err = agents.LoadRetriever(ragCorpus, mock, 4)
		if err != nil {
			t.Fatal(err)
		}
		mock.Err = errors.New("embedder down")
	}
	out, dx, _ := runRAG(t, confidentACSNoCite, r)

	if out.Plan == nil || out.Plan.Diagnosis != "acute coronary syndrome" {
		t.Fatalf("retrieval failure broke the case: %+v", out)
	}
	if len(dx.Citations) != 0 {
		t.Errorf("degraded run should carry zero citations, got %d", len(dx.Citations))
	}
}
