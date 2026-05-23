package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// LLMDiagnostician is a drop-in replacement for DiagnosticianAgent that asks
// an LLM for a structured diagnosis. On any failure (timeout, JSON parse,
// transport error, low confidence) it delegates to a rule-based fallback so
// the pipeline always produces a diagnosis_proposal.
//
// The agent's ID is "diagnostician" — same as the rule-based one — so the
// supervisor's routing (which targets that ID) does not have to change.
type LLMDiagnostician struct {
	// Client is the LLM provider. Required.
	Client llm.Client

	// Fallback runs when the LLM call fails or returns junk. Required.
	// Typically &DiagnosticianAgent{}.
	Fallback agent.Agent

	// Timeout caps a single LLM call. Defaults to 30s.
	Timeout time.Duration

	// MaxTokens caps the response length. Defaults to 512.
	MaxTokens int

	// MinConfidence below which the LLM's answer is treated as untrusted
	// and the fallback fires. Defaults to 0.3.
	MinConfidence float64
}

func (a *LLMDiagnostician) ID() string   { return "diagnostician" }
func (a *LLMDiagnostician) Name() string { return "Diagnostician (LLM)" }
func (a *LLMDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis", "llm_backed"}
}

func (a *LLMDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	proposal, llmErr := a.callLLM(ctx, state, env)
	if llmErr != nil {
		env.Logf("[diagnostician/llm] %s — falling back to rule-based", llmErr)
		return a.runFallback(ctx, msg, env)
	}

	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[diagnostician/llm] provider=%s model=%s diagnosis=%q confidence=%.0f%%",
		a.Client.Provider(), a.Client.Model(), proposal.Diagnosis, proposal.Confidence*100)
	env.Logf("[diagnostician/llm] rationale: %s", proposal.Rationale)

	content, _ := json.Marshal(proposal)
	stateContent := medical.EncodeState(state)
	verify := agent.NewMessage(
		a.ID(),
		"reasoning_verifier",
		agent.RoleAgent,
		"diagnosis_proposal",
		string(content),
		mergeStateMeta(msg.Metadata, stateContent),
	)
	return []agent.Message{verify}, nil
}

func (a *LLMDiagnostician) callLLM(ctx context.Context, state medical.CaseState, env agent.Environment) (medical.DiagnosisProposal, error) {
	if a.Client == nil {
		return medical.DiagnosisProposal{}, errors.New("llm client not configured")
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	maxTokens := a.MaxTokens
	if maxTokens == 0 {
		maxTokens = 512
	}
	minConfidence := a.MinConfidence
	if minConfidence == 0 {
		minConfidence = 0.3
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stateJSON, _ := json.MarshalIndent(state, "", "  ")
	req := llm.Request{
		System:      diagnosticianSystemPrompt,
		CacheSystem: true,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: "Case state:\n" + string(stateJSON) + "\n\nReturn one DiagnosisProposal.",
		}},
		MaxTokens:   maxTokens,
		Temperature: 0.2,
		JSONSchema: &llm.JSONSchema{
			Name:        "emit_diagnosis",
			Description: "Emit a single working diagnosis with confidence, differential, and rationale.",
			Schema:      json.RawMessage(diagnosisSchema),
		},
	}

	resp, err := a.Client.Complete(cctx, req)
	if err != nil {
		return medical.DiagnosisProposal{}, fmt.Errorf("llm call: %w", err)
	}

	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(resp.Content), &p); err != nil {
		return medical.DiagnosisProposal{}, fmt.Errorf("decode JSON: %w", err)
	}
	if p.Diagnosis == "" {
		return medical.DiagnosisProposal{}, errors.New("empty diagnosis in LLM output")
	}
	if p.Confidence < minConfidence {
		return medical.DiagnosisProposal{}, fmt.Errorf("confidence %.2f below threshold %.2f", p.Confidence, minConfidence)
	}

	env.Logf("[diagnostician/llm] tokens in=%d out=%d cached=%d stop=%s",
		resp.InputTokens, resp.OutputTokens, resp.CachedTokens, resp.StopReason)
	return p, nil
}

func (a *LLMDiagnostician) runFallback(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Fallback == nil {
		return nil, errors.New("no fallback configured")
	}
	return a.Fallback.HandleMessage(ctx, msg, env)
}

// diagnosticianSystemPrompt is sent (cached) on every LLM diagnostician call.
const diagnosticianSystemPrompt = `You are the Diagnostician agent in a virtual physician panel modelled on Microsoft's MAI-DxO sequential diagnosis architecture.

Your role:
  - Read the structured CaseState (presentation, vitals, history answers, ordered tests with results, hypotheses, RPM observations).
  - Propose the single most likely working diagnosis.
  - Provide a 1-3 sentence clinical rationale grounded only in evidence already in the CaseState — do not invent labs, vitals, or history that is not present.
  - Provide a confidence score between 0.0 and 1.0.
  - List up to five differential diagnoses, ordered by likelihood.

Constraints:
  - This is a research / educational system. Do not provide treatment plans.
  - If the evidence is insufficient for a confident diagnosis, return a working label such as "Undifferentiated <complaint> — further workup needed" with confidence below 0.5.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`

// diagnosisSchema is the JSON Schema for DiagnosisProposal.
const diagnosisSchema = `{
  "type": "object",
  "properties": {
    "diagnosis": {
      "type": "string",
      "description": "The single most likely working diagnosis."
    },
    "confidence": {
      "type": "number",
      "minimum": 0,
      "maximum": 1,
      "description": "Confidence in the working diagnosis from 0.0 to 1.0."
    },
    "differential": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Up to five differential diagnoses ordered by likelihood."
    },
    "rationale": {
      "type": "string",
      "description": "1-3 sentence clinical rationale grounded in the supplied CaseState."
    }
  },
  "required": ["diagnosis", "confidence", "rationale"],
  "additionalProperties": false
}`
