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

// LLMReasoningVerifier is a drop-in replacement for ReasoningVerifierAgent
// that uses an LLM to judge whether a DiagnosisProposal is grounded in the
// accompanying CaseState evidence and is not over-confident. On any failure
// (timeout, JSON parse, transport error) it delegates to a rule-based
// fallback so the pipeline always produces a verdict.
//
// The agent's ID is "reasoning_verifier" — same as the rule-based one — so
// the supervisor's routing (which targets that ID) does not have to change.
type LLMReasoningVerifier struct {
	// Client is the LLM provider. Required.
	Client llm.Client

	// Fallback runs when the LLM call fails or returns junk. Required.
	// Typically &ReasoningVerifierAgent{HITL: ...}.
	Fallback agent.Agent

	// HITL, when true, routes verified diagnoses through human_review for
	// clinician sign-off before the supervisor finalises (FDA CDS-style
	// oversight). Matches ReasoningVerifierAgent.HITL.
	HITL bool

	// Timeout caps a single LLM call. Defaults to 30s.
	Timeout time.Duration

	// MaxTokens caps the response length. Defaults to 512.
	MaxTokens int
}

func (a *LLMReasoningVerifier) ID() string   { return "reasoning_verifier" }
func (a *LLMReasoningVerifier) Name() string { return "Clinical Reasoning Verifier (LLM)" }
func (a *LLMReasoningVerifier) Capabilities() []string {
	return []string{"verify_reasoning", "safety_check", "llm_backed"}
}

// verifierOutput mirrors the JSON shape the LLM is asked to produce.
type verifierOutput struct {
	Verified bool   `json:"verified"`
	Reason   string `json:"reason"`
}

func (a *LLMReasoningVerifier) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var proposal medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(msg.Content), &proposal); err != nil {
		env.Logf("[reasoning_verifier/llm] invalid proposal: %v", err)
		return nil, err
	}

	inputState, _ := msg.Metadata["case_state"].(string)
	state, _ := medical.DecodeState(inputState)

	verdict, llmErr := a.callLLM(ctx, proposal, state, env)
	if llmErr != nil {
		env.Logf("[reasoning_verifier/llm] %s — falling back to rule-based", llmErr)
		return a.runFallback(ctx, msg, env)
	}

	if !verdict.Verified {
		env.Logf("[reasoning_verifier/llm] REJECT: %s", verdict.Reason)
		return nil, nil
	}

	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[reasoning_verifier/llm] VERIFIED diagnosis %q — %s", proposal.Diagnosis, verdict.Reason)

	stateJSON := medical.EncodeState(state)
	meta := copyMeta(msg.Metadata)
	var out agent.Message
	if a.HITL {
		out = agent.NewMessage(a.ID(), "human_review", agent.RoleAgent,
			"review_request", stateJSON, reviewEnvelope(meta, reviewOptions{
				Kind:       "diagnosis_review",
				NextTo:     "diagnostic_supervisor",
				NextType:   "diagnosis",
				Priority:   "high",
				Subject:    "Sign off diagnosis: " + proposal.Diagnosis,
				SLAMinutes: 4 * 60,
			}))
	} else {
		out = agent.NewMessage(a.ID(), "diagnostic_supervisor", agent.RoleAgent,
			"diagnosis", stateJSON, meta)
	}
	return []agent.Message{out}, nil
}

func (a *LLMReasoningVerifier) callLLM(ctx context.Context, proposal medical.DiagnosisProposal, state medical.CaseState, env agent.Environment) (verifierOutput, error) {
	if a.Client == nil {
		return verifierOutput{}, errors.New("llm client not configured")
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	maxTokens := a.MaxTokens
	if maxTokens == 0 {
		maxTokens = 512
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	proposalJSON, _ := json.MarshalIndent(proposal, "", "  ")
	stateJSON, _ := json.MarshalIndent(state, "", "  ")
	user := "DiagnosisProposal:\n" + string(proposalJSON) +
		"\n\nCaseState evidence:\n" + string(stateJSON) +
		"\n\nVerify whether the rationale is grounded in the supplied evidence and the confidence is not over-stated."

	req := llm.Request{
		System:      verifierSystemPrompt,
		CacheSystem: true,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: user,
		}},
		MaxTokens:   maxTokens,
		Temperature: 0.0,
		JSONSchema: &llm.JSONSchema{
			Name:        "verify_diagnosis_reasoning",
			Description: "Decide whether the diagnosis rationale is grounded in the CaseState and is not over-confident.",
			Schema:      json.RawMessage(verifierSchema),
		},
	}

	resp, err := a.Client.Complete(cctx, req)
	if err != nil {
		return verifierOutput{}, fmt.Errorf("llm call: %w", err)
	}

	var v verifierOutput
	if err := json.Unmarshal([]byte(resp.Content), &v); err != nil {
		return verifierOutput{}, fmt.Errorf("decode JSON: %w", err)
	}

	env.Logf("[reasoning_verifier/llm] provider=%s model=%s tokens in=%d out=%d cached=%d stop=%s",
		a.Client.Provider(), a.Client.Model(), resp.InputTokens, resp.OutputTokens, resp.CachedTokens, resp.StopReason)
	return v, nil
}

func (a *LLMReasoningVerifier) runFallback(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Fallback == nil {
		return nil, errors.New("no fallback configured")
	}
	return a.Fallback.HandleMessage(ctx, msg, env)
}

// verifierSystemPrompt is sent (cached) on every LLM verifier call.
const verifierSystemPrompt = `You are the Reasoning Verifier agent in a virtual physician panel modelled on Microsoft's MAI-DxO sequential diagnosis architecture.

Your role:
  - Read a DiagnosisProposal (diagnosis, confidence, rationale, differential).
  - Read the accompanying CaseState (presentation, history answers, test results, RPM alerts, hypotheses).
  - Decide whether the rationale is grounded ONLY in evidence already in the CaseState — flag any claim that invents labs, vitals, or history that is not present.
  - Decide whether the confidence is appropriate for the available evidence — flag clear over-confidence (e.g. >0.8 with thin evidence) or missing rationale.

Return a single JSON object with:
  - verified : true when the rationale is grounded AND the confidence is not over-stated; false otherwise.
  - reason   : a one-sentence justification of your decision.

Constraints:
  - This is a research / educational system. Do not propose a different diagnosis; only judge the one supplied.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`

// verifierSchema is the JSON Schema for the verifier output.
const verifierSchema = `{
  "type": "object",
  "properties": {
    "verified": {
      "type": "boolean",
      "description": "True when the rationale is grounded in the CaseState and the confidence is not over-stated."
    },
    "reason": {
      "type": "string",
      "description": "One-sentence justification of the verdict."
    }
  },
  "required": ["verified", "reason"],
  "additionalProperties": false
}`
