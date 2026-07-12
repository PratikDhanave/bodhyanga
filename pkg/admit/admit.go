// Package admit is THE admission boundary (architecture.md §3): every hop —
// pure-Go executor or LLM agent run — passes through Boundary before any
// agent logic executes. Fail-closed on governance (policy error ≡ deny),
// fail-open on audit (a failed audit write never blocks care flow, but deny
// records are written synchronously with the denial so no observer can miss
// them). Envelopes and audit events never carry message content (PHI).
package admit

import (
	"context"
	"errors"
	"log/slog"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/governance"
)

// ErrDenied is returned by guarded hops when governance blocks the action.
// It surfaces as an ExecutorFailedEvent and halts the case's forward path.
var ErrDenied = errors.New("admit: denied by governance")

// Boundary is the one admission implementation (spec FR-001).
type Boundary struct {
	policy   governance.Policy
	recorder audit.Recorder
	log      *slog.Logger
}

// New wires the boundary. All three dependencies are required; audit and
// logging degrade gracefully at call time, governance never does.
func New(pol governance.Policy, rec audit.Recorder, log *slog.Logger) *Boundary {
	if log == nil {
		log = slog.Default()
	}
	return &Boundary{policy: pol, recorder: rec, log: log}
}

// Admit evaluates one hop. It returns nil only when governance explicitly
// allows; any error, non-allow decision, or unlabeled message type is a
// denial. The deny audit record is written synchronously BEFORE returning.
func (b *Boundary) Admit(ctx context.Context, agentID, msgType, caseToken string) error {
	env, err := buildEnvelope(agentID, msgType, caseToken)
	if err != nil {
		b.record(audit.Event{
			Kind: audit.KindPolicyDecision, AgentID: agentID, Type: msgType,
			CaseID: caseToken, Decision: string(governance.DecisionDeny),
			Reason: err.Error(),
		})
		return ErrDenied
	}

	res, err := b.policy.Evaluate(ctx, env)
	if err != nil || res.Decision != governance.DecisionAllow {
		reason := res.Reason
		if err != nil {
			// Policy errors are denials (fail-closed). The error class — not
			// the error text, which could echo envelope internals — goes in
			// the reason.
			reason = "policy_error"
		}
		b.record(audit.Event{
			Kind: audit.KindPolicyDecision, AgentID: agentID, MessageID: env.ID,
			To: agentID, Type: msgType, CaseID: caseToken,
			Decision: string(governance.DecisionDeny), Reason: reason,
		})
		return ErrDenied
	}

	b.record(audit.Event{
		Kind: audit.KindPolicyDecision, AgentID: agentID, MessageID: env.ID,
		To: agentID, Type: msgType, CaseID: caseToken,
		Decision: string(governance.DecisionAllow), Reason: res.Reason,
	})
	return nil
}

// RecordAgentError audits a handler failure after an admitted hop. ErrorText
// carries err.Error() only — never payload (bodh audit discipline).
func (b *Boundary) RecordAgentError(agentID, caseToken string, err error) {
	b.record(audit.Event{
		Kind: audit.KindAgentError, AgentID: agentID, CaseID: caseToken,
		ErrorText: err.Error(),
	})
}

// RecordReviewDecision audits a physician's verdict (or a stale-response
// rejection). This is the 21 USC §360j(o) anchor row — ReviewerID names the
// licensed reviewer; reason "stale_response" marks audited discards.
func (b *Boundary) RecordReviewDecision(caseToken, reviewerID, decision, reason string) {
	b.record(audit.Event{
		Kind: audit.KindReviewDecision, AgentID: "physician_review",
		CaseID: caseToken, Decision: decision, Reason: reason,
		ReviewerID: reviewerID,
	})
}

// Guard wraps one hop's body: admit, run, audit failure. The signature is
// deliberately generic-free so pkg/agents can accept it without importing
// this package (see agents.Guard).
func (b *Boundary) Guard(ctx context.Context, agentID, msgType, caseToken string, run func() error) error {
	if err := b.Admit(ctx, agentID, msgType, caseToken); err != nil {
		return err
	}
	if err := run(); err != nil {
		b.RecordAgentError(agentID, caseToken, err)
		return err
	}
	return nil
}

// record is the fail-open audit seam: write errors are loud but never alter
// control flow (the deny/allow decision has already been made).
func (b *Boundary) record(e audit.Event) {
	if b.recorder == nil {
		return
	}
	if err := b.recorder.Record(e); err != nil {
		b.log.Error("admit: audit write failed (fail-open, care flow continues)",
			"error", err, "kind", string(e.Kind), "agent", e.AgentID, "case", e.CaseID)
	}
}
