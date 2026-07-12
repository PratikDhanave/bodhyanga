package pipeline

import (
	"reflect"

	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// The physician-review gate (spec US3, contracts/admission-and-review.md):
//
//	diagnostician → review_gate → [RequestPort physician_review] → review_decision → care_plan
//
// review_gate turns a DiagnosisResult into a ReviewRequest and parks the
// diagnosis in workflow STATE (so it survives checkpoints); the port pauses
// the run until an external ReviewDecision arrives; review_decision validates
// the verdict, audits it, and either forwards the diagnosis to care_plan or
// terminates the case with a CaseRejected output.

const (
	pendingReviewKey   = "pending_review"
	pendingReviewScope = "ReviewState" // named scope: shared across executors
)

// newReviewPort declares the external port. MAF owns request-ID correlation:
// run.SendResponse only accepts responses created from the delivered request,
// and a resumed run republishes the SAME pending request.
func newReviewPort() workflow.RequestPort {
	return workflow.RequestPort{
		ID:       "physician_review",
		Request:  reflect.TypeFor[agents.ReviewRequest](),
		Response: reflect.TypeFor[agents.ReviewDecision](),
	}
}

// newReviewGate parks the diagnosis in state and asks for review.
func newReviewGate(id string, guard agents.Guard) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, d agents.DiagnosisResult) error {
		return guardedPipe(guard, ctx, id, admit.TypeDiagnosisResult, agents.HashCaseID(d.CaseID), func() error {
			if err := ctx.QueueStateUpdate(pendingReviewKey, pendingReviewScope, d); err != nil {
				return err
			}
			return ctx.SendMessage("", agents.ReviewRequest{
				CaseToken:  agents.HashCaseID(d.CaseID),
				Diagnosis:  d.Diagnosis,
				Confidence: d.Confidence,
				Forced:     d.Forced,
				Rounds:     d.Rounds,
			})
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[agents.ReviewRequest]())
			return rb, nil
		},
	})
}

// newReviewDecision validates and applies the physician's verdict.
func newReviewDecision(id string, guard agents.Guard, boundary *admit.Boundary) *workflow.Executor {
	return workflow.NewExecutor(id, func(ctx *workflow.Context, dec agents.ReviewDecision) error {
		// Read the parked diagnosis BEFORE the guard so a stale response can
		// be audited with the right token even when nothing is pending.
		var pending *agents.DiagnosisResult
		if raw, err := ctx.ReadState(pendingReviewKey, pendingReviewScope); err == nil && raw != nil {
			var d agents.DiagnosisResult
			var ok bool
			if d, ok = raw.(agents.DiagnosisResult); !ok {
				if pv, isPV := raw.(workflow.PortableValue); isPV {
					d, ok = workflow.PortableValueAs[agents.DiagnosisResult](pv)
				}
			}
			// CaseID=="" is the cleared-slot sentinel, not a pending review.
			if ok && d.CaseID != "" {
				pending = &d
			}
		}
		token := ""
		if pending != nil {
			token = agents.HashCaseID(pending.CaseID)
		}
		return guardedPipe(guard, ctx, id, admit.TypeReviewDecision, token, func() error {
			// Stale / invalid verdicts: no pending review, or anonymous
			// reviewer. Audited and discarded — never silently applied.
			if pending == nil || dec.ReviewerID == "" {
				if boundary != nil {
					boundary.RecordReviewDecision(token, dec.ReviewerID, "reject", "stale_response")
				}
				if pending != nil {
					// Re-ask: the case must not be lost to a bad response.
					return ctx.SendMessage("", agents.ReviewRequest{
						CaseToken:  token,
						Diagnosis:  pending.Diagnosis,
						Confidence: pending.Confidence,
						Forced:     pending.Forced,
						Rounds:     pending.Rounds,
					})
				}
				return nil
			}
			// Clear the pending slot (typed sentinel — the state store may
			// reject nil); the verdict lands exactly once.
			if err := ctx.QueueStateUpdate(pendingReviewKey, pendingReviewScope, agents.DiagnosisResult{}); err != nil {
				return err
			}
			if dec.Approved {
				if boundary != nil {
					boundary.RecordReviewDecision(token, dec.ReviewerID, "approve", dec.Note)
				}
				return ctx.SendMessage("", *pending)
			}
			if boundary != nil {
				boundary.RecordReviewDecision(token, dec.ReviewerID, "reject", dec.Note)
			}
			return ctx.YieldOutput(agents.CaseRejected{
				CaseID:     pending.CaseID,
				Diagnosis:  pending.Diagnosis,
				ReviewerID: dec.ReviewerID,
				Note:       dec.Note,
			})
		})
	}).Extend(&workflow.Executor{
		ConfigureProtocol: func(rb *workflow.ProtocolBuilder) (*workflow.ProtocolBuilder, error) {
			rb.SendsMessageType(reflect.TypeFor[agents.DiagnosisResult]())
			rb.SendsMessageType(reflect.TypeFor[agents.ReviewRequest]())
			rb.YieldsOutputType(reflect.TypeFor[agents.CaseRejected]())
			return rb, nil
		},
	})
}

// guardedPipe mirrors agents.guarded for pipeline-owned executors.
func guardedPipe(g agents.Guard, ctx *workflow.Context, agentID, msgType, caseToken string, fn func() error) error {
	if g == nil {
		return fn()
	}
	return g(ctx, agentID, msgType, caseToken, fn)
}
