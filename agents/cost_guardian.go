package agents

import (
	"context"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// CostGuardianAgent validates diagnostic spend against budget (MAI-DxO cost-value trade-off).
type CostGuardianAgent struct{}

func (a *CostGuardianAgent) ID() string   { return "cost_guardian" }
func (a *CostGuardianAgent) Name() string { return "Diagnostic Cost Guardian" }
func (a *CostGuardianAgent) Capabilities() []string {
	return []string{"cost_check", "pareto_optimization"}
}

func (a *CostGuardianAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	if state.TotalCostUSD > state.MaxCostUSD {
		env.Logf("[cost_guardian] DENY: cost $%.2f exceeds budget $%.2f — requesting revision",
			state.TotalCostUSD, state.MaxCostUSD)
		out := agent.NewMessage(a.ID(), "diagnostic_supervisor", agent.RoleAgent,
			"tests_revise", medical.EncodeState(state), copyMeta(msg.Metadata))
		return []agent.Message{out}, nil
	}

	env.Logf("[cost_guardian] APPROVE: $%.2f / $%.2f budget (%.0f%% utilized)",
		state.TotalCostUSD, state.MaxCostUSD, 100*state.TotalCostUSD/state.MaxCostUSD)

	out := agent.NewMessage(
		a.ID(),
		"diagnostic_supervisor",
		agent.RoleAgent,
		"tests_complete",
		medical.EncodeState(state),
		copyMeta(msg.Metadata),
	)
	return []agent.Message{out}, nil
}
