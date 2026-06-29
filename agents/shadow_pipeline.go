package agents

// shadow_pipeline.go hosts BuildShadowPipeline, the convenience constructor
// that wires a full intake→diagnosis orchestrator for shadow-mode evaluation.
//
// Why it lives in the agents package and not in pkg/shadow: it is the ONLY
// piece of shadow tooling that needs concrete agent implementations. Keeping
// it here lets pkg/shadow (the ConcordanceLedger + ShadowRunner mechanics)
// stay agent-free, which is a prerequisite for the agent-decoupling module
// split (decoupling PR-0.6): no main-module package may import
// github.com/PratikDhanave/bodh/agents, or the future bodh/agents → bodh
// module graph would be cyclic. The dependency direction is now one-way:
// agents → pkg/{orchestration,comm,governance,registry,observability}, never
// the reverse.

import (
	"context"
	"fmt"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// BuildShadowPipeline constructs a full orchestrator + bus + agent pipeline
// suitable for shadow evaluation. It returns the started orchestrator and the
// bus. The provider parameter selects the diagnostician backend (e.g. "rule",
// "llm-anthropic").
//
// This is a convenience for cmd/shadow and tests; production deployments wire
// their own orchestrator. The pipeline mirrors the cmd/care sequential-
// diagnosis core (intake → rpm_monitor → supervisor → questioner →
// test_planner → cost_guardian → diagnostician → reasoning_verifier).
func BuildShadowPipeline(ctx context.Context, provider string) (*orchestration.Orchestrator, comm.Bus, error) {
	logger := observability.NewStdLogger()
	env := &orchestration.SimpleEnvironment{Logger: logger, Clock: observability.SystemClock{}}
	reg := registry.NewInMemory()
	bus := comm.NewInMemoryBus()
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 65536},
		governance.RequireCaseIDPolicy{},
		governance.MaxDiagnosticCostPolicy{},
		governance.ResearchDisclaimerPolicy{},
	)

	diag, err := NewDiagnostician(provider)
	if err != nil {
		return nil, nil, fmt.Errorf("build diagnostician (%s): %w", provider, err)
	}

	for _, a := range []agent.Agent{
		&IntakeAgent{},
		&RPMMonitorAgent{},
		&DiagnosticSupervisor{},
		&QuestionerAgent{},
		&TestPlannerAgent{},
		&CostGuardianAgent{},
		diag,
		&ReasoningVerifierAgent{},
	} {
		_ = reg.Register(ctx, a)
	}

	orch := orchestration.NewOrchestrator(reg, bus, policy, env)
	orch.Start(ctx)

	return orch, bus, nil
}
