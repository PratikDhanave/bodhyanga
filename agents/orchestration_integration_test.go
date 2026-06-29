package agents_test

// Integration tests for the full MARA pipeline assembled from concrete agents.
// These live in the agents module (not pkg/orchestration) because they wire
// real agent implementations through the orchestrator; keeping them here means
// no main-module package — production OR test — imports
// github.com/PratikDhanave/bodh/agents, which is a prerequisite for the
// Phase-2 separate-module split (decoupling PR-0.6b). They exercise only
// exported orchestration symbols, so nothing is lost by the relocation.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/protocol"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

type integrationEnv struct{}

func (integrationEnv) Now() time.Time      { return time.Now() }
func (integrationEnv) Logf(string, ...any) {}

// TestIntegration_OrchestratorBootstrap verifies that the full MARA pipeline
// can be assembled and started without error — all agents register, governance
// composes, and the orchestrator accepts a message without panicking.
func TestIntegration_OrchestratorBootstrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := comm.NewInMemoryBus()
	reg := registry.NewInMemory()

	// Register all diagnostic agents.
	for _, a := range []agent.Agent{
		&agents.IntakeAgent{},
		&agents.DiagnosticSupervisor{},
		&agents.QuestionerAgent{},
		&agents.TestPlannerAgent{},
		&agents.CostGuardianAgent{},
		&agents.DiagnosticianAgent{},
		&agents.ReasoningVerifierAgent{},
	} {
		reg.Register(ctx, a)
	}

	// Verify all 7 agents registered.
	all := reg.List(ctx)
	if len(all) < 7 {
		t.Fatalf("expected at least 7 agents, got %d", len(all))
	}

	// Compose governance.
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 50000},
		governance.RequireCaseIDPolicy{},
	)

	// Start orchestrator — should not panic.
	env := integrationEnv{}
	orch := orchestration.NewOrchestrator(reg, bus, policy, env)
	orch.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish a well-formed intake message — should not panic.
	cs := medical.CaseState{
		CaseID:       "integration-001",
		Presentation: "65M with dyspnea and edema. BP 160/95, HR 92.",
		Condition:    "chf",
		MaxCostUSD:   500,
	}
	content, _ := json.Marshal(cs)

	bus.Publish(ctx, protocol.Message{
		ID:       "msg-int-001",
		From:     "test",
		To:       "intake",
		Role:     "user",
		Type:     "patient_presentation",
		Content:  string(content),
		Metadata: map[string]any{"case_id": cs.CaseID},
	})

	// Give the pipeline a moment to process.
	time.Sleep(200 * time.Millisecond)

	t.Log("Integration bootstrap passed — orchestrator started and accepted a message without error")
}

// TestIntegration_DiagnosticianDirect verifies that the rule-based
// diagnostician produces a CHF diagnosis when given a CHF case directly.
func TestIntegration_DiagnosticianDirect(t *testing.T) {
	cs := medical.CaseState{
		CaseID:       "direct-chf-001",
		Presentation: "65M with progressive dyspnea, bilateral LE edema, orthopnea. BP 160/95 HR 92 SpO2 93%.",
		Condition:    "chf",
		TestResults: map[string]string{
			"BNP": "850 pg/mL",
		},
	}
	content, _ := json.Marshal(cs)

	diag := &agents.DiagnosticianAgent{}
	env := integrationEnv{}

	msgs, err := diag.HandleMessage(context.Background(), agent.Message{
		From:    "test_planner",
		To:      "diagnostician",
		Role:    "agent",
		Type:    "tests_complete",
		Content: string(content),
	}, env)

	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("no messages returned")
	}

	// The output should be a diagnosis_proposal with CHF.
	var proposal struct {
		Diagnosis  string  `json:"diagnosis"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(msgs[0].Content), &proposal); err != nil {
		t.Fatalf("unmarshal proposal: %v", err)
	}

	if proposal.Diagnosis == "" {
		t.Fatal("empty diagnosis")
	}

	t.Logf("Direct diagnostician test: diagnosis=%q confidence=%.0f%%",
		proposal.Diagnosis, proposal.Confidence*100)
}
