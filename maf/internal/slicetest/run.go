// Package slicetest is the shared workflow-run harness for this repo's tests.
// It runs one case through a built pipeline and collects everything a test
// could want to assert on — including failures, which callers inspect rather
// than fataling on (admission tests EXPECT ExecutorFailedEvent).
package slicetest

import (
	"context"
	"testing"

	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/microsoft/agent-framework-go/workflow/inproc"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// Result collects one run's observable outcomes.
type Result struct {
	// Plan is non-nil when the workflow yielded a CarePlan output.
	Plan *agents.CarePlan
	// Forced holds every ForcedProgressionEvent observed.
	Forced []agents.ForcedProgressionEvent
	// Failures holds every ExecutorFailedEvent (e.g. admission denials).
	Failures []workflow.ExecutorFailedEvent
	// Events is the full ordered event stream for custom assertions.
	Events []workflow.Event
}

// Run executes one case and drains the event stream. Workflow-level stream
// errors fail the test (they indicate harness bugs, not case outcomes);
// executor failures and error events are collected for the caller.
func Run(t *testing.T, wf *workflow.Workflow, pc agents.PatientCase) Result {
	t.Helper()
	ctx := context.Background()
	run, err := inproc.Default.RunStreaming(ctx, wf, pc)
	if err != nil {
		t.Fatalf("slicetest: start run: %v", err)
	}
	defer func() { _ = run.Close(ctx) }()

	var res Result
	for evt, err := range run.WatchStream(ctx) {
		if err != nil {
			t.Fatalf("slicetest: stream: %v", err)
		}
		res.Events = append(res.Events, evt)
		switch e := evt.(type) {
		case agents.ForcedProgressionEvent:
			res.Forced = append(res.Forced, e)
		case workflow.ExecutorFailedEvent:
			res.Failures = append(res.Failures, e)
		case workflow.OutputEvent:
			if plan, ok := e.Output.(agents.CarePlan); ok {
				res.Plan = &plan
			} else {
				t.Fatalf("slicetest: unexpected output type %T", e.Output)
			}
		}
	}
	return res
}
