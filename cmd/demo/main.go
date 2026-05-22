package main

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// Demo: sequential diagnosis workflow inspired by Microsoft MAI-DxO and SD Bench.
//
// Flow (virtual physician panel):
//   user → intake → diagnostic_supervisor → questioner → supervisor
//        → test_planner → cost_guardian → supervisor → diagnostician
//        → reasoning_verifier → supervisor (final diagnosis + cost report)
//
// Platform: MARA multi-agent architecture (same as c2siorg/genie, domain = medicine).

func main() {
	logger := observability.NewStdLogger()
	env := &orchestration.SimpleEnvironment{
		Logger: logger,
		Clock:  observability.SystemClock{},
	}

	reg := registry.NewInMemory()
	bus := comm.NewInMemoryBus()
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 65536},
		governance.RequireCaseIDPolicy{},
		governance.MaxDiagnosticCostPolicy{},
		governance.ResearchDisclaimerPolicy{},
	)

	ctx := context.Background()

	agentList := []agent.Agent{
		&agents.IntakeAgent{},
		&agents.DiagnosticSupervisor{},
		&agents.QuestionerAgent{},
		&agents.TestPlannerAgent{},
		&agents.CostGuardianAgent{},
		&agents.DiagnosticianAgent{},
		&agents.ReasoningVerifierAgent{},
	}
	for _, a := range agentList {
		_ = reg.Register(ctx, a)
	}

	orch := orchestration.NewOrchestrator(reg, bus, policy, env)
	orch.Start(ctx)

	// Sample case: cough + fever → community-acquired pneumonia (NEJM-style complexity, demo logic).
	presentation := medical.PatientPresentation{
		ChiefComplaint: "Cough and fever",
		HPI:          "58-year-old presents with 5 days productive cough and fever to 38.9°C",
		Demographics: "58M",
		Vitals:       "T 38.9°C, HR 98, RR 22, SpO2 94% on room air",
	}

	meta := map[string]any{
		"case_id":         "demo-case-001",
		"trace_id":        "trace-demo-001",
		"max_cost_usd":    500.0,
		"gold_diagnosis":  "Community-acquired pneumonia",
	}

	start := agent.NewMessage(
		"user",
		"intake",
		agent.RoleUser,
		"presentation",
		medical.EncodePresentation(presentation),
		meta,
	)
	bus.Publish(ctx, start)

	time.Sleep(3 * time.Second)
}
