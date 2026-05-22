package agents

import (
	"context"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// TestPlannerAgent implements MAI-DxO "Request Test" with cost-aware ordering.
type TestPlannerAgent struct{}

func (a *TestPlannerAgent) ID() string   { return "test_planner" }
func (a *TestPlannerAgent) Name() string { return "Diagnostic Test Planner" }
func (a *TestPlannerAgent) Capabilities() []string {
	return []string{"order_test", "interpret_results"}
}

func (a *TestPlannerAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}
	if state.TestResults == nil {
		state.TestResults = make(map[string]string)
	}

	// Cost-effective workup for respiratory presentation (SD Bench style).
	planned := []string{"complete_blood_count", "chest_xray", "basic_metabolic_panel"}
	for _, name := range planned {
		cost := medical.TestCatalog[name]
		env.Logf("[test_planner] ordering %s ($%.0f)", name, cost)
		test := medical.DiagnosticTest{Name: name, CostUSD: cost, Ordered: true}
		state.TestsOrdered = append(state.TestsOrdered, test)
		state.TotalCostUSD += cost
		state.TestResults[name] = simulatedTestResult(name)
	}

	env.Logf("[test_planner] ordered %d tests; running total $%.2f", len(planned), state.TotalCostUSD)

	// Route through cost guardian before proceeding (MAI-DxO cost check).
	meta := copyMeta(msg.Metadata)
	meta["spent_usd"] = state.TotalCostUSD
	meta["test_cost_usd"] = 0.0

	out := agent.NewMessage(
		a.ID(),
		"cost_guardian",
		agent.RoleAgent,
		"case_state",
		medical.EncodeState(state),
		meta,
	)
	return []agent.Message{out}, nil
}

func simulatedTestResult(name string) string {
	switch name {
	case "complete_blood_count":
		return "WBC 14.2 K, neutrophil predominance"
	case "chest_xray":
		return "Right lower lobe infiltrate consistent with consolidation"
	case "basic_metabolic_panel":
		return "Within normal limits"
	default:
		return "Pending"
	}
}
