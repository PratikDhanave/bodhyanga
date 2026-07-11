package gateway

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// Pipeline is one agent's admission control: governance + audit, plus optional
// extra middlewares. A per-agent runtime builds one Pipeline and calls Admit
// for every inbound message before dispatching to the agent's HandleMessage.
//
// Construction is plain field assignment so callers wire only what they need;
// the zero value (no policy, no recorder, no middlewares) admits everything
// and is the in-process / test default. Nil Policy disables governance; nil
// Recorder disables audit.
type Pipeline struct {
	// AgentID is recorded as the handling agent in KindMessage events and
	// appears in logs. Required for a meaningful audit trail.
	AgentID string

	// Policy is the governance stack (typically a *governance.CompositePolicy).
	// Nil disables governance — every message is admitted. Mirrors the
	// orchestrator's nil-policy semantics.
	Policy governance.Policy

	// Recorder is the audit sink. Nil disables audit (the message still flows;
	// audit is fail-open, exactly as in the orchestrator).
	Recorder audit.Recorder

	// ClinicalTypes selects which message types get a KindPolicyDecision event
	// on the ALLOW path. Nil ⇒ no allow-audit (deny is always audited). Use
	// DefaultClinicalTypes() to match the monolith.
	ClinicalTypes map[string]bool

	// Extra holds defence-in-depth gates that run on the allow path, after
	// governance, before the agent. Empty by default.
	Extra []Middleware

	// Log is an optional structured logger (e.g. env.Logf). Nil silences
	// gateway diagnostics. It must never be handed msg.Content (PHI).
	Log func(format string, args ...any)

	// now lets tests stub the clock for synthesized deny timestamps. Nil ⇒
	// time.Now().UTC().
	now func() time.Time
}

// Admit runs governance + extra middlewares + audit for one inbound message
// and reports whether the agent should handle it. It reproduces Steps 1–2 of
// pkg/orchestration.Orchestrator.Start exactly. A false return means the
// message was blocked (and, where applicable, audited); the caller must NOT
// invoke the agent.
func (p *Pipeline) Admit(ctx context.Context, msg protocol.Message) bool {
	// === Step 1: Governance ===
	if p.Policy != nil {
		res, err := p.Policy.Evaluate(ctx, msg)
		if err != nil {
			// Fail closed. The orchestrator logs and drops without an audit
			// event here (a known, deliberate gap); we match that behaviour.
			p.logf("gateway[%s]: policy error for msg %s: %v", p.AgentID, msg.ID, err)
			return false
		}
		if res.Decision == governance.DecisionDeny {
			// Deny is always audited — every deny is operator signal.
			p.logf("gateway[%s]: message %s denied by policy: %s", p.AgentID, msg.ID, res.Reason)
			p.record(audit.FromPolicyDecision(msg, res))
			return false
		}
		// Allow path: audit only clinical types (avoid bookkeeping noise).
		if p.ClinicalTypes[msg.Type] {
			p.record(audit.FromPolicyDecision(msg, res))
		}
	}

	// === Step 2: Extra middlewares (allow path only) ===
	for _, m := range p.Extra {
		admit, reason, err := m.Check(ctx, msg)
		if err != nil {
			p.logf("gateway[%s]: middleware %s errored on msg %s: %v (fail-closed)",
				p.AgentID, m.Name(), msg.ID, err)
			p.recordBlock(msg, m.Name()+": evaluation error")
			return false
		}
		if !admit {
			p.logf("gateway[%s]: middleware %s blocked msg %s: %s",
				p.AgentID, m.Name(), msg.ID, reason)
			p.recordBlock(msg, m.Name()+": "+reason)
			return false
		}
	}

	// === Step 3: Record the invocation, then admit ===
	// KindMessage is recorded BEFORE the agent runs so a panicking handler
	// still leaves an "invoked AgentID with msg" trail.
	p.record(audit.FromMessage(p.AgentID, msg))
	return true
}

// RecordAgentError audits a KindAgentError event for a message whose agent
// handler returned a non-nil error. It is the post-admission counterpart to
// Admit: Admit covers Steps 1–2 (governance + KindMessage), and a per-agent
// runtime calls this after HandleMessage fails, mirroring
// orchestrator.recordAgentError exactly.
//
// The event carries err.Error() in ErrorText and NEVER msg.Content — the same
// PHI-safety contract Admit honours. Audit stays fail-open: a nil Recorder or a
// sink error is swallowed, never surfaced to the caller.
func (p *Pipeline) RecordAgentError(msg protocol.Message, err error) {
	e := audit.FromMessage(p.AgentID, msg)
	e.Kind = audit.KindAgentError
	e.ErrorText = err.Error()
	p.record(e)
}

// recordBlock audits an extra-middleware block as a policy_decision deny so the
// trail is uniform with governance denies.
func (p *Pipeline) recordBlock(msg protocol.Message, reason string) {
	p.record(audit.FromPolicyDecision(msg, governance.PolicyResult{
		Decision:  governance.DecisionDeny,
		Reason:    reason,
		CheckedAt: p.clock(),
	}))
}

// record forwards an event to the recorder, swallowing sink errors (audit is
// fail-open: never block message flow on audit downtime). Never logs
// msg.Content — only routing/kind metadata already on the Event.
func (p *Pipeline) record(e audit.Event) {
	if p.Recorder == nil {
		return
	}
	if err := p.Recorder.Record(e); err != nil {
		p.logf("gateway[%s]: audit record failed (msg=%s kind=%s): %v",
			p.AgentID, e.MessageID, e.Kind, err)
	}
}

func (p *Pipeline) logf(format string, args ...any) {
	if p.Log != nil {
		p.Log(format, args...)
	}
}

func (p *Pipeline) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now().UTC()
}
