package bench

import (
	"context"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

// Config parameterizes the harness Runner. Zero values get slice defaults.
type Config struct {
	Tau       float64
	MaxRounds int
}

// RunSuite runs every fixture through one rule-engine pipeline and scores the
// produced diagnosis against gold. The Runner has an allow-all boundary, the
// review gate ON, and an auto-approve Responder so the review resolves inline
// (no checkpoint store, no pause) and each benign case reaches a care plan. A
// case that cannot terminate is classified failure — never a fabricated match.
// Fixtures run sequentially (the Runner is single-threaded), which also makes
// results deterministic.
func RunSuite(ctx context.Context, fixtures []Fixture, cfg Config) ([]CaseResult, error) {
	if cfg.Tau == 0 {
		cfg.Tau = 0.85
	}
	if cfg.MaxRounds == 0 {
		cfg.MaxRounds = 3
	}
	rec := audit.NewInMemory()
	b := admit.New(admit.AllowAll(), rec, nil)
	runner, err := pipeline.NewRunner(pipeline.Config{
		Tau: cfg.Tau, MaxRounds: cfg.MaxRounds, Review: true, Boundary: b,
		EvalStore: eval.NewInMemoryStore(), EngineName: "rule",
	}, rec)
	if err != nil {
		return nil, err
	}
	runner.Responder = func(req *workflow.ExternalRequest) (*workflow.ExternalResponse, error) {
		return req.CreateResponse(agents.ReviewDecision{Approved: true, ReviewerID: "bench"})
	}

	results := make([]CaseResult, 0, len(fixtures))
	for _, f := range fixtures {
		r := CaseResult{ID: f.ID, Gold: f.Gold}
		out, err := runner.Run(ctx, f.Case, "")
		if err != nil || out.Plan == nil {
			r.Status = StatusFailure
			results = append(results, r)
			continue
		}
		r.Produced = out.Plan.Diagnosis
		r.Forced = out.Plan.Forced
		r.Status = Score(f.Gold, r.Produced)
		results = append(results, r)
	}
	return results, nil
}
