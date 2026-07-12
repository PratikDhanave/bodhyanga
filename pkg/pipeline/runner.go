package pipeline

import (
	"context"
	"errors"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/microsoft/agent-framework-go/workflow/inproc"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/auditbridge"
)

// Runner owns one case's execution: it runs the workflow, feeds the audit
// bridge, and reduces the event stream to an Outcome. cmd/slice and the
// integration tests share it so the trail is identical in both.
type Runner struct {
	wf  *workflow.Workflow
	cfg Config
	rec audit.Recorder
}

// Outcome is what one run observably produced.
type Outcome struct {
	Plan     *agents.CarePlan
	Denied   bool
	Forced   []agents.ForcedProgressionEvent
	Failures []workflow.ExecutorFailedEvent
	Events   []workflow.Event
}

// NewRunner builds the workflow from cfg and pairs it with the audit sink.
func NewRunner(cfg Config, rec audit.Recorder) (*Runner, error) {
	wf, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &Runner{wf: wf, cfg: cfg, rec: rec}, nil
}

// CaseToken returns the opaque fingerprint the trail is keyed on — computed
// on the post-de-identification case ID so every hop agrees.
func (r *Runner) CaseToken(pc agents.PatientCase) string {
	if r.cfg.Deidentify != nil {
		pc = r.cfg.Deidentify(pc)
	}
	return agents.HashCaseID(pc.CaseID)
}

// Run executes one case to its terminal state (or denial), returning the
// reduced outcome. Stream-level errors are returned as-is; executor failures
// (including denials) land in the Outcome.
func (r *Runner) Run(ctx context.Context, pc agents.PatientCase) (*Outcome, error) {
	run, err := inproc.Default.RunStreaming(ctx, r.wf, pc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = run.Close(ctx) }()

	bridge := auditbridge.New(r.rec, r.CaseToken(pc))
	out := &Outcome{}
	for evt, err := range run.WatchStream(ctx) {
		if err != nil {
			return out, err
		}
		bridge.Handle(evt)
		out.Events = append(out.Events, evt)
		switch e := evt.(type) {
		case agents.ForcedProgressionEvent:
			out.Forced = append(out.Forced, e)
		case workflow.ExecutorFailedEvent:
			out.Failures = append(out.Failures, e)
			if errors.Is(e.Error, admit.ErrDenied) {
				out.Denied = true
			}
		case workflow.ErrorEvent:
			// A denial at the start executor surfaces as a workflow-level
			// error (input processing halts) — that is a governed outcome,
			// not a runner failure.
			if errors.Is(e.Error, admit.ErrDenied) {
				out.Denied = true
				continue
			}
			return out, e.Error
		case workflow.OutputEvent:
			if plan, ok := e.Output.(agents.CarePlan); ok {
				out.Plan = &plan
			}
		}
	}
	return out, nil
}
