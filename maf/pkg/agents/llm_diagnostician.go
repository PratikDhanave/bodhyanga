package agents

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/workflow"
)

// llmAssessment is the structured verdict the model must return. It is
// deliberately NOT DiagnosisResult: CaseID and Rounds are never trusted from
// the model — they are stamped from the workup (spec FR-010/FR-011).
type llmAssessment struct {
	Diagnosis  string     `json:"diagnosis"`
	Confidence float64    `json:"confidence"`
	Evidence   []string   `json:"evidence"`
	Citations  []Citation `json:"citations"`
}

// NewLLMDiagnostician hosts a MAF agent (Anthropic, OpenAI/Ollama, or a test
// fake) behind the SAME graph node protocol as the rule diagnostician:
// consumes Workup, emits DiagnosisResult or loops back with DiagnosticQuery.
// Swapping engines is a binding change, never a graph change (spec US4).
//
// The de-identified workup content DOES go to the model — that is the
// clinical function; PHI was stripped at intake. The guard admits the hop,
// and the agent's own admission middleware (if configured) additionally
// guards the provider call, keyed by the case token placed on the context.
// The optional retriever grounds the model in a knowledge corpus (specs/003).
// It is strictly additive: retrieval runs INSIDE the admitted hop, before the
// model call, and fails open — any retrieval error is logged PHI-free and
// swallowed, so the model still runs (ungrounded) and the case still
// finalizes. A nil retriever means RAG is disabled.
func NewLLMDiagnostician(id string, a *agent.Agent, cfg DiagnosticianConfig, guard Guard, retriever *Retriever) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, wu Workup) error {
		return guarded(guard, ctx, id, "workup", HashCaseID(wu.Query.Case.CaseID), func() error {
			// Fail-open retrieval: an error never blocks the case, mirroring
			// the audit fail-open invariant (FR-004/FR-005).
			retrieved, err := retriever.Retrieve(ctx, wu)
			if err != nil {
				slog.Warn("rag retrieval failed; proceeding ungrounded",
					"case", HashCaseID(wu.Query.Case.CaseID))
				retrieved = nil
			}
			assess, err := assessWithModel(ctx, a, wu, retrieved)
			if err != nil {
				// Guard records this as agent_error; the case does NOT
				// advance on fabricated content (FR-011).
				return err
			}
			return routeDiagnosis(ctx, cfg, wu, assess.Diagnosis, assess.Confidence,
				assess.Evidence, reconcileCitations(assess.Citations, retrieved))
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[DiagnosisResult]())
			rb.SendsMessageType(reflect.TypeFor[DiagnosticQuery]())
			return rb, nil
		},
	})
}

func assessWithModel(ctx *workflow.Context, a *agent.Agent, wu Workup, evidence []Citation) (llmAssessment, error) {
	prompt, err := buildPrompt(wu, evidence)
	if err != nil {
		return llmAssessment{}, err
	}
	runCtx := WithCaseToken(ctx, HashCaseID(wu.Query.Case.CaseID))

	var assess llmAssessment
	if _, err := a.RunText(runCtx, prompt, agent.WithStructuredOutput(&assess)).Collect(); err != nil {
		return llmAssessment{}, fmt.Errorf("llm diagnostician: %w", err)
	}
	// Post-validation: a malformed or out-of-range verdict never becomes a
	// diagnosis (FR-011).
	if strings.TrimSpace(assess.Diagnosis) == "" {
		// Static, PHI-free message → audit-safe (kept verbatim in the trail).
		return llmAssessment{}, auditSafeErr("llm diagnostician: model returned empty diagnosis")
	}
	if assess.Confidence < 0 || assess.Confidence > 1 {
		// Confidence is decision-support data, not PHI → audit-safe.
		return llmAssessment{}, auditSafeErr(fmt.Sprintf("llm diagnostician: confidence %v out of [0,1]", assess.Confidence))
	}
	return assess, nil
}

func buildPrompt(wu Workup, evidence []Citation) (string, error) {
	payload, err := json.Marshal(struct {
		Symptoms []string     `json:"symptoms"`
		History  []string     `json:"history"`
		Round    int          `json:"round"`
		Results  []TestResult `json:"test_results"`
	}{wu.Query.Case.Symptoms, wu.Query.Case.History, wu.Query.Round, wu.Results})
	if err != nil {
		return "", err
	}
	system := "You are a diagnostic decision-support engine in a RESEARCH system " +
		"(not clinical care). Given the de-identified workup below, return your " +
		"single most likely diagnosis, a calibrated confidence in [0,1], and the " +
		"supporting evidence items."
	if block := evidenceBlock(evidence); block != "" {
		// When evidence is supplied, instruct the model to ground in and cite
		// only from it (spec US1); the block is prepended before the workup.
		system += " Ground your reasoning in the supplied Evidence and cite only " +
			"from it; if the evidence does not support a confident answer, say so."
		return system + "\n\n" + block + "\nWorkup: " + string(payload), nil
	}
	return system + "\n\nWorkup: " + string(payload), nil
}

// evidenceBlock renders retrieved citations as an "Evidence:" block, mirroring
// bodhyanga's agents/diagnostician_llm.go format. Empty citations => empty string
// (no block, no behavior change when RAG is off).
func evidenceBlock(cites []Citation) string {
	if len(cites) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Evidence:\n")
	for _, c := range cites {
		fmt.Fprintf(&b, "- [%s] %s — %s\n", c.Source, c.Title, c.Snippet)
	}
	return b.String()
}

// reconcileCitations decides which provenance rides the DiagnosisResult: the
// model's own citations when it returned any, otherwise the retrieved set.
// This mirrors bodhyanga's "keep model subset else retrieved" rule — no hard
// membership check (plan non-goal).
func reconcileCitations(model, retrieved []Citation) []Citation {
	if len(model) > 0 {
		return model
	}
	return retrieved
}
