package careapi

import (
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

// pendingReview extracts the parked ReviewRequest from a paused run's event
// stream. The Runner surfaces Outcome.Paused but not the request itself, so we
// scan Outcome.Events for the workflow.RequestInfoEvent and decode its portable
// payload — the same PortableValueAs[ReviewRequest] pattern the CLI Responder
// uses (cmd/slice/main.go). Returns nil when the run is not awaiting review.
//
// (Chosen over adding Outcome.PendingReview() to pkg/pipeline: this keeps the
// affordance careapi-local and edits no shared type — plan.md T-R3.)
func pendingReview(out *pipeline.Outcome) *agents.ReviewRequest {
	if out == nil {
		return nil
	}
	for _, evt := range out.Events {
		e, ok := evt.(workflow.RequestInfoEvent)
		if !ok {
			continue
		}
		if req, ok := workflow.PortableValueAs[agents.ReviewRequest](e.Request.Data); ok {
			return &req
		}
	}
	return nil
}
