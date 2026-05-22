package agents

import (
	"context"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// IntakeAgent receives patient presentation and seeds case state for sequential diagnosis.
type IntakeAgent struct{}

func (a *IntakeAgent) ID() string   { return "intake" }
func (a *IntakeAgent) Name() string { return "Clinical Intake" }
func (a *IntakeAgent) Capabilities() []string {
	return []string{"intake_presentation"}
}

func (a *IntakeAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	pres, err := medical.DecodePresentation(msg.Content)
	if err != nil {
		env.Logf("[intake] invalid presentation: %v", err)
		return nil, err
	}
	env.Logf("[intake] case=%v chief_complaint=%q", msg.Metadata["case_id"], pres.ChiefComplaint)

	state := medical.CaseState{
		CaseID:          metadataString(msg.Metadata, "case_id"),
		Presentation:    pres.ChiefComplaint + "; " + pres.HPI,
		Answers:         make(map[string]string),
		TestResults:     make(map[string]string),
		MaxCostUSD:      metadataFloat(msg.Metadata, "max_cost_usd", 500),
		GoldDiagnosis:   metadataString(msg.Metadata, "gold_diagnosis"),
		Condition:       pres.Condition,
		RPMObservations: pres.RPMObservations,
	}

	out := agent.NewMessage(
		a.ID(),
		"rpm_monitor",
		agent.RoleAgent,
		"case_state",
		medical.EncodeState(state),
		copyMeta(msg.Metadata),
	)
	return []agent.Message{out}, nil
}
