// Package auditbridge maps MAF workflow events onto bodhyanga's append-only audit
// trail (architecture.md §4). The bridge writes telemetry rows — hop
// executions, failures, checkpoints, finalization. Admission rows (allow /
// deny) are written synchronously by pkg/admit inside the hop itself;
// duplicates between the two writers are tolerated, missing denials are not.
// No event field ever carries message content or a raw case ID.
package auditbridge

import (
	"errors"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
)

// Bridge consumes one run's event stream for one case.
type Bridge struct {
	rec       audit.Recorder
	caseToken string
	// lastLabel remembers the payload label each executor was invoked with,
	// so the completion row can name the message type without touching the
	// payload again.
	lastLabel map[string]string
}

// New builds a bridge for one case. caseToken must be the opaque fingerprint.
func New(rec audit.Recorder, caseToken string) *Bridge {
	return &Bridge{rec: rec, caseToken: caseToken, lastLabel: map[string]string{}}
}

// Handle maps one workflow event to zero or more audit rows. Audit failures
// are swallowed (fail-open): this bridge is telemetry, the safety-critical
// rows are pkg/admit's.
func (b *Bridge) Handle(evt workflow.Event) {
	switch e := evt.(type) {
	case workflow.ExecutorInvokedEvent:
		if label, ok := admit.LabelFor(e.Message); ok {
			b.lastLabel[e.ExecutorID] = label
		}
	case workflow.ExecutorCompletedEvent:
		b.record(audit.Event{
			Kind: audit.KindMessage, AgentID: e.ExecutorID,
			Type: b.lastLabel[e.ExecutorID], CaseID: b.caseToken,
		})
	case workflow.ExecutorFailedEvent:
		if errors.Is(e.Error, admit.ErrDenied) {
			return // the deny row was already written synchronously by admit
		}
		b.record(audit.Event{
			Kind: audit.KindAgentError, AgentID: e.ExecutorID,
			CaseID: b.caseToken, ErrorText: admit.SafeErrorText(e.Error),
		})
	case workflow.SuperStepCompletedEvent:
		if e.CompletionInfo != nil && e.CompletionInfo.CheckpointInfo != nil {
			b.record(audit.Event{
				Kind: audit.KindMessage, Type: "checkpoint",
				MessageID: e.CompletionInfo.CheckpointInfo.CheckpointID,
				CaseID:    b.caseToken,
			})
		}
	case workflow.RequestInfoEvent:
		b.record(audit.Event{
			Kind: audit.KindMessage, AgentID: "physician_review",
			Type: "review_requested", CaseID: b.caseToken,
		})
	case workflow.OutputEvent:
		b.record(audit.Event{
			Kind: audit.KindMessage, Type: "case_finalized", CaseID: b.caseToken,
		})
	}
}

func (b *Bridge) record(e audit.Event) {
	if b.rec == nil {
		return
	}
	_ = b.rec.Record(e) // fail-open; admit logs sink failures loudly already
}
