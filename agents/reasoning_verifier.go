package agents

import (
	"context"
	"encoding/json"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// ReasoningVerifierAgent implements MAI-DxO self-verification before final diagnosis.
type ReasoningVerifierAgent struct{}

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

	stateJSON, _ := msg.Metadata["case_state"].(string)
	state, _ := medical.DecodeState(stateJSON)

	// Demo verification rules (production: LLM rubric + clinician review).
	verified := proposal.Confidence >= 0.5 && proposal.Rationale != ""
	if !verified {
		env.Logf("[reasoning_verifier] REJECT: insufficient confidence or rationale")
		return nil, nil
	}

	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[reasoning_verifier] VERIFIED diagnosis %q", proposal.Diagnosis)

	out := agent.NewMessage(
		a.ID(),
		"diagnostic_supervisor",
		agent.RoleAgent,
		"diagnosis",
		medical.EncodeState(state),
		copyMeta(msg.Metadata),
	)
	return []agent.Message{out}, nil
}
