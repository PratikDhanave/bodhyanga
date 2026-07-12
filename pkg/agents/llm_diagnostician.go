package agents

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/workflow"
)

// llmAssessment is the structured verdict the model must return. It is
// deliberately NOT DiagnosisResult: CaseID and Rounds are never trusted from
// the model — they are stamped from the workup (spec FR-010/FR-011).
type llmAssessment struct {
	Diagnosis  string   `json:"diagnosis"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence"`
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
func NewLLMDiagnostician(id string, a *agent.Agent, cfg DiagnosticianConfig, guard Guard) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, wu Workup) error {
		return guarded(guard, ctx, id, "workup", HashCaseID(wu.Query.Case.CaseID), func() error {
			assess, err := assessWithModel(ctx, a, wu)
			if err != nil {
				// Guard records this as agent_error; the case does NOT
				// advance on fabricated content (FR-011).
				return err
			}
			return routeDiagnosis(ctx, cfg, wu, assess.Diagnosis, assess.Confidence, assess.Evidence)
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[DiagnosisResult]())
			rb.SendsMessageType(reflect.TypeFor[DiagnosticQuery]())
			return rb, nil
		},
	})
}

func assessWithModel(ctx *workflow.Context, a *agent.Agent, wu Workup) (llmAssessment, error) {
	prompt, err := buildPrompt(wu)
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
		return llmAssessment{}, fmt.Errorf("llm diagnostician: model returned empty diagnosis")
	}
	if assess.Confidence < 0 || assess.Confidence > 1 {
		return llmAssessment{}, fmt.Errorf("llm diagnostician: confidence %v out of [0,1]", assess.Confidence)
	}
	return assess, nil
}

func buildPrompt(wu Workup) (string, error) {
	payload, err := json.Marshal(struct {
		Symptoms []string     `json:"symptoms"`
		History  []string     `json:"history"`
		Round    int          `json:"round"`
		Results  []TestResult `json:"test_results"`
	}{wu.Query.Case.Symptoms, wu.Query.Case.History, wu.Query.Round, wu.Results})
	if err != nil {
		return "", err
	}
	return "You are a diagnostic decision-support engine in a RESEARCH system " +
		"(not clinical care). Given the de-identified workup below, return your " +
		"single most likely diagnosis, a calibrated confidence in [0,1], and the " +
		"supporting evidence items.\n\nWorkup: " + string(payload), nil
}
