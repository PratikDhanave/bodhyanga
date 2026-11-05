// Package pipeline assembles the MARA slice workflow graph
// (architecture.md §1): intake → test_planner → diagnostician → care_plan,
// with the diagnostician's conditional loop-back to the planner.
package pipeline

import (
	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/intake"
)

// Config parameterizes the graph. Zero values get slice defaults
// (τ=0.85, 3 rounds), matching architecture.md §6.
type Config struct {
	Tau        float64
	MaxRounds  int
	Deidentify func(agents.PatientCase) agents.PatientCase
	Lab        agents.Lab
	// Boundary is the admission boundary applied to EVERY hop (spec FR-001).
	// nil disables admission — step-1 behavior, acceptable only in tests.
	Boundary *admit.Boundary
	// Review inserts the physician-review gate between diagnostician and
	// care plan (spec US3). cmd/slice always enables it; tests opt in.
	Review bool
	// EvalStore receives one InteractionRecord per terminal case (spec US5).
	// nil disables eval recording.
	EvalStore eval.Store
	// EngineName labels the diagnostician in eval metadata (rule | llm-*).
	EngineName string
	// Diagnostician overrides the rule executor (step 4 binds the LLM agent
	// here). It must consume Workup and send DiagnosisResult/DiagnosticQuery,
	// and is responsible for honoring the same Boundary.
	Diagnostician *workflow.Executor
	// Translate prepends the front-door translation hop (Inbound → PatientCase)
	// before intake (specs/002). When false the graph is byte-identical to the
	// JSON-only slice, and the Runner is fed a PatientCase; when true the Runner
	// is fed an agents.Inbound via RunInbound.
	Translate bool
}

// New builds the workflow. The two edges out of the diagnostician route on
// message type: DiagnosisResult proceeds, DiagnosticQuery loops back.
func New(cfg Config) (*workflow.Workflow, error) {
	if cfg.Tau == 0 {
		cfg.Tau = 0.85
	}
	if cfg.MaxRounds == 0 {
		cfg.MaxRounds = 3
	}
	var guard agents.Guard
	if cfg.Boundary != nil {
		guard = cfg.Boundary.Guard
	}

	dx := cfg.Diagnostician
	if dx == nil {
		dx = agents.NewRuleDiagnostician("diagnostician", agents.DiagnosticianConfig{
			Tau:       cfg.Tau,
			MaxRounds: cfg.MaxRounds,
		}, guard)
	}

	intakeNode := agents.NewIntake("intake", cfg.Deidentify, guard).Bind()
	planner := agents.NewTestPlanner("test_planner", cfg.Lab, guard).Bind()
	diagnostician := dx.Bind()
	carePlan := agents.NewCarePlan("care_plan", guard).Bind()

	isDiagnosis := func(msg any) bool { _, ok := msg.(agents.DiagnosisResult); return ok }
	isQuery := func(msg any) bool { _, ok := msg.(agents.DiagnosticQuery); return ok }

	// Opt-in front door: a translation hop that turns an Inbound (HL7/FHIR/JSON)
	// into a PatientCase feeds intake. When disabled, intake is the start node
	// and the graph is byte-identical to the JSON-only slice (spec SC-006).
	var b *workflow.Builder
	if cfg.Translate {
		translator := agents.NewTranslator("translator", intake.Translate, guard).Bind()
		b = workflow.NewBuilder(translator).
			AddEdge(translator, intakeNode).
			AddEdge(intakeNode, planner)
	} else {
		b = workflow.NewBuilder(intakeNode).
			AddEdge(intakeNode, planner)
	}
	b = b.
		AddEdge(planner, diagnostician).
		AddDirectEdge(diagnostician, planner, false, isQuery).
		WithName("bodhyanga-maf MARA slice")

	if !cfg.Review {
		return b.
			AddDirectEdge(diagnostician, carePlan, false, isDiagnosis).
			WithOutputFrom(carePlan).
			Build()
	}

	gate := newReviewGate("review_gate", guard).Bind()
	port := newReviewPort().Bind()
	decision := newReviewDecision("review_decision", guard, cfg.Boundary).Bind()

	return b.
		AddDirectEdge(diagnostician, gate, false, isDiagnosis).
		AddEdge(gate, port).
		AddEdge(port, decision).
		AddDirectEdge(decision, carePlan, false, isDiagnosis).
		AddDirectEdge(decision, port, false, func(msg any) bool {
			_, ok := msg.(agents.ReviewRequest) // stale re-ask
			return ok
		}).
		WithOutputFrom(carePlan, decision).
		Build()
}
