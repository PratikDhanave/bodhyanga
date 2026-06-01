package governance

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// BreakGlassEvent carries the fields the BreakGlassPolicy hands to its
// Recorder for every valid emergency-access event. The type lives in the
// governance package (rather than pkg/audit) to avoid an import cycle:
// pkg/audit already imports pkg/governance for PolicyResult.
//
// Callers that wire the real audit.FileStore / audit.InMemory implement
// BreakGlassRecorder with a thin adapter in pkg/orchestration or cmd/care —
// the adapter converts BreakGlassEvent into an audit.Event with
// Kind=audit.KindBreakGlass and calls audit.Recorder.Record.
type BreakGlassEvent struct {
	// MessageID is the protocol.Message.ID of the break-glass request.
	MessageID string
	// From / To / Type are the routing triple from the message envelope.
	From string
	To   string
	Type string
	// Clinician is the value of msg.Metadata["break_glass_clinician"].
	// Captured here so the Recorder can embed it in the audit reason without
	// re-parsing metadata.
	Clinician string
	// Reason is a pre-formatted PHI-free description of why access was granted.
	Reason string
	// Correlation handles copied from metadata (patient_id, case_id,
	// trace_id, tenant_id). All are optional; absent keys remain empty.
	PatientID string
	CaseID    string
	TraceID   string
	TenantID  string
	// At is the UTC timestamp at which the policy evaluated the request.
	At time.Time
}

// BreakGlassRecorder is the sink for emergency-access audit events.
//
// The interface is defined here rather than in pkg/audit to avoid an import
// cycle (pkg/audit → pkg/governance → pkg/audit). In practice the concrete
// implementation wraps audit.Recorder with an adapter that converts
// BreakGlassEvent to audit.Event{Kind: audit.KindBreakGlass}.
//
// Implementations must be safe for concurrent calls (the orchestrator
// dispatches on bus goroutines). Record must not block for arbitrary
// durations — the caller is on the message-dispatch hot path.
type BreakGlassRecorder interface {
	RecordBreakGlass(e BreakGlassEvent) error
}

// BreakGlassPolicy allows emergency override of normal access controls while
// generating a mandatory audit event.
//
// Clinical use case: an ED physician needs immediate access to records for a
// patient not in their panel — for example, a trauma case arriving without
// prior provider relationship. Normal scope and panel-membership policies
// would deny the request; BreakGlassPolicy creates an auditable exception
// path that satisfies both the care imperative and HIPAA §164.312(b) audit
// requirements.
//
// Regulatory anchor: HIPAA §164.308(a)(1)(ii)(D) requires "procedures to
// regularly review records of information system activity such as audit logs,
// access reports, and security incident tracking reports." Emergency access
// events are specifically called out in most covered-entity HIPAA policies as
// requiring post-hoc review within 24-72 hours. The mandatory audit event
// emitted here is that paper trail.
//
// The policy is intentionally pass-through for normal messages (those without
// break_glass=true in metadata). This allows BreakGlassPolicy to sit in the
// composite alongside all other policies without adding latency to the common
// path.
//
// Wiring: include BreakGlassPolicy in the CompositePolicy BEFORE scope/panel
// policies so a valid break-glass can override them. The CompositePolicy
// short-circuits on the first deny, so ordering matters — if a scope policy
// denies before BreakGlassPolicy evaluates, the override never fires.
//
// Example composite (ED deployment):
//
//	governance.NewComposite(
//	    governance.MaxContentLengthPolicy{Max: 65536},
//	    governance.RequireCaseIDPolicy{},
//	    governance.BreakGlassPolicy{Recorder: auditAdapter},
//	    governance.RequireScopePolicy{Required: scopeMap},
//	)
//
// Security note: BreakGlassPolicy trusts the caller to supply honest
// break_glass_reason and break_glass_clinician values. In production these
// fields MUST be injected by the authenticated API gateway (not accepted
// from the request body directly) so they reflect the verified identity of
// the logged-in clinician, not self-asserted values. The audit event is the
// detective control; the gateway injection is the preventive control.
type BreakGlassPolicy struct {
	// Recorder is the audit sink that receives the mandatory break-glass
	// event for every valid emergency-access request. It must not be nil
	// when break_glass=true messages are expected; Evaluate will propagate a
	// record error as a deny to keep the fail-closed posture — a missing
	// audit trail is treated the same as a missing authorisation.
	Recorder BreakGlassRecorder
}

// Evaluate applies the break-glass logic.
//
// Three outcomes:
//  1. msg.Metadata["break_glass"] != "true" — pass-through allow (normal path).
//  2. msg.Metadata["break_glass"] == "true" with valid reason+clinician — allow
//     AND emit a KindBreakGlass audit event unconditionally.
//  3. msg.Metadata["break_glass"] == "true" but reason or clinician missing — deny.
//
// The deny reason for case 3 is intentionally stable and descriptive so that
// downstream dashboards can group by it:
//
//	"break_glass requires break_glass_reason and break_glass_clinician metadata"
func (p BreakGlassPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	// Fast path: not a break-glass request. Let other policies decide.
	if msg.Metadata == nil || msg.Metadata["break_glass"] != "true" {
		return allow("normal access — no break_glass flag"), nil
	}

	// Validate required fields for the break-glass override.
	reason, _ := msg.Metadata["break_glass_reason"].(string)
	clinician, _ := msg.Metadata["break_glass_clinician"].(string)

	if reason == "" || clinician == "" {
		// Missing validation fields: deny. The reason string is the stable
		// public contract; tests and dashboards assert its exact value.
		return deny("break_glass requires break_glass_reason and break_glass_clinician metadata"), nil
	}

	// Valid break-glass: emit the mandatory audit event BEFORE returning the
	// allow result. Audit-before-allow means the access is recorded even if
	// the process crashes between RecordBreakGlass() and returning to the
	// orchestrator (at-least-once semantics on the audit side).
	//
	// Record errors are treated as deny to stay fail-closed. A break-glass
	// event that cannot be audited is operationally worse than one that is
	// blocked: the former creates untracked access that violates HIPAA
	// §164.312(b); the latter creates a logged failure that can be paged and
	// remediated.
	e := BreakGlassEvent{
		MessageID: msg.ID,
		From:      msg.From,
		To:        msg.To,
		Type:      msg.Type,
		Clinician: clinician,
		Reason:    "break_glass access granted: " + clinician,
		At:        time.Now().UTC(),
	}
	// Copy the four standard correlation handles (patient_id, case_id,
	// trace_id, tenant_id) from metadata so the break-glass row is
	// joinable in the same SIEM queries as other KindPolicyDecision rows.
	if v, ok := msg.Metadata["patient_id"].(string); ok {
		e.PatientID = v
	}
	if v, ok := msg.Metadata["case_id"].(string); ok {
		e.CaseID = v
	}
	if v, ok := msg.Metadata["trace_id"].(string); ok {
		e.TraceID = v
	}
	if v, ok := msg.Metadata["tenant_id"].(string); ok {
		e.TenantID = v
	}

	if err := p.Recorder.RecordBreakGlass(e); err != nil {
		// Audit failure → deny. See security note above.
		return PolicyResult{}, err
	}

	return allow("break_glass access granted"), nil
}
