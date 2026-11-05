package agents

import (
	"context"
	"encoding/json"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// ReasoningVerifierAgent implements MAI-DxO self-verification before final diagnosis.
//
// When HITL is true, the verified diagnosis is routed through human_review so a
// clinician signs off before the supervisor finalizes (FDA CDS-style oversight).
type ReasoningVerifierAgent struct {
	HITL bool
}

func (a *ReasoningVerifierAgent) ID() string   { return "reasoning_verifier" }
func (a *ReasoningVerifierAgent) Name() string { return "Clinical Reasoning Verifier" }
func (a *ReasoningVerifierAgent) Capabilities() []string {
	return []string{"verify_reasoning", "safety_check"}
}

func (a *ReasoningVerifierAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var proposal medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(msg.Content), &proposal); err != nil {
		env.Logf("[reasoning_verifier] invalid proposal: %v", err)
		return nil, err
	}

	inputState, _ := msg.Metadata["case_state"].(string)
	state, _ := medical.DecodeState(inputState)

	// Demo verification rules (production: LLM rubric + clinician review).
	verified := proposal.Confidence >= 0.5 && proposal.Rationale != ""
	if !verified {
		env.Logf("[reasoning_verifier] REJECT: insufficient confidence or rationale")
		return nil, nil
	}

	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[reasoning_verifier] VERIFIED diagnosis %q", proposal.Diagnosis)

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
