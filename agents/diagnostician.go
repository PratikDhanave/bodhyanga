package agents

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// DiagnosticianAgent implements MAI-DxO "Provide Diagnosis" from accumulated evidence.
type DiagnosticianAgent struct{}

func (a *DiagnosticianAgent) ID() string   { return "diagnostician" }
func (a *DiagnosticianAgent) Name() string { return "Diagnostician" }
func (a *DiagnosticianAgent) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis"}
}

func (a *DiagnosticianAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	proposal := inferDiagnosis(state)
	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[diagnostician] diagnosis=%q confidence=%.0f%%", proposal.Diagnosis, proposal.Confidence*100)
	env.Logf("[diagnostician] rationale: %s", proposal.Rationale)

	content, _ := json.Marshal(proposal)
	stateContent := medical.EncodeState(state)

	// Verify reasoning before finalizing (MAI-DxO self-check).
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

func inferDiagnosis(state medical.CaseState) medical.DiagnosisProposal {
	if state.Condition == "chf" && len(state.RPMAlerts) >= 2 {
		return medical.DiagnosisProposal{
			Diagnosis:    "Acute decompensated heart failure",
			Confidence:   0.84,
			Differential: state.Hypotheses,
			Rationale:    "Dyspnea with weight gain and RPM alerts (elevated BP, weight, low SpO2)",
		}
	}

	xray, hasInfiltrate := state.TestResults["chest_xray"]
	cbc, hasLeukocytosis := state.TestResults["complete_blood_count"]

	if hasInfiltrate && hasLeukocytosis &&
		strings.Contains(xray, "infiltrate") && strings.Contains(cbc, "WBC") {
		return medical.DiagnosisProposal{
			Diagnosis:    "Community-acquired pneumonia",
			Confidence:   0.87,
			Differential: state.Hypotheses,
			Rationale:    "Fever, productive cough, leukocytosis, and RLL infiltrate on chest X-ray",
		}
	}
	return medical.DiagnosisProposal{
		Diagnosis:  "Undifferentiated respiratory infection — further workup needed",
		Confidence: 0.45,
		Rationale:  "Insufficient objective findings for definitive diagnosis",
	}
}

func mergeStateMeta(meta map[string]any, stateJSON string) map[string]any {
	m := copyMeta(meta)
	m["case_state"] = stateJSON
	return m
}
