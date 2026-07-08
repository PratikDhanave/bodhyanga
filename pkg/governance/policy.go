package governance

import (
	"context"
	"errors"
	"time"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// Decision describes the outcome of a policy check.
//
// A binary decision is intentionally simple for a reference implementation.
// Production platforms often extend this to include:
//   - "allow with modifications" (e.g., redact sensitive data)
//   - "allow but require approval" (human-in-the-loop)
//   - structured actions (log, alert, throttle, reroute)
//
// The string value travels into audit.Event.Decision verbatim (see
// pkg/audit.FromPolicyDecision) and therefore forms part of the on-disk
// public contract — renaming the constants is a schema change for any
// downstream SIEM / compliance dashboard reading the audit log.
type Decision string

const (
	// DecisionAllow lets the message proceed to the agent's HandleMessage.
	// The orchestrator only records allow events for clinical message types
	// (see clinicalMessageTypes in pkg/orchestration) to keep the audit log
	// signal-rich — see docs/governance.md § Audit events.
	DecisionAllow Decision = "allow"

	// DecisionDeny blocks the message. The orchestrator records the deny,
	// logs via env.Logf, and short-circuits before the agent ever sees the
	// payload — the fail-closed posture mandated by Bodh design rule 4
	// (governance runs before HandleMessage) and aligned with HIPAA
	// §164.312(b) audit-control expectations.
	DecisionDeny Decision = "deny"
)

// PolicyResult captures the decision plus optional explanation.
//
// Governance must be explainable. The Reason field provides a human-readable
// rationale suitable for logs and audits.
//
// The struct is intentionally flat (no nested decisions, no scores): policy
// composition is handled by CompositePolicy, not by post-hoc merging of
// PolicyResult values. Keeping the type tiny also keeps Evaluate hot-path
// cost low — the orchestrator runs Evaluate once per message hop.
type PolicyResult struct {
	// Decision is the binary verdict. The orchestrator branches on this in
	// pkg/orchestration.Orchestrator.Start; any value other than
	// DecisionAllow is treated as deny (fail closed).
	Decision Decision

	// Reason is a short PHI-free string that ends up in audit.Event.Reason
	// and in operational logs via env.Logf. It is therefore part of the
	// public contract — tests in pkg/governance assert specific Reason
	// strings, and downstream dashboards group by Reason. See
	// docs/governance.md § Adding a policy for the conventions.
	Reason string

	// CheckedAt is the UTC timestamp the policy made its decision. Helpers
	// allow() and deny() (in medical_policy.go) set this via
	// time.Now().UTC(); CompositePolicy backfills it on the deny path if
	// a child returned a zero value.
	CheckedAt time.Time

	// CheckedByID is reserved for future per-policy attribution (e.g.
	// "MaxContentLengthPolicy", "Part2ConsentPolicy"). It is unset by the
	// shipped policies today; tests should not depend on its value.
	CheckedByID string
}

// Policy is the interface for governance policies applied to messages.
//
// Policies are designed to be:
//   - composable (many small policies are easier to manage than one huge policy)
//   - deterministic where possible (predictability helps debugging and testing)
//   - fast (they run in the critical path before every agent invocation)
//
// Contract:
//   - Evaluate runs synchronously, BEFORE the agent's HandleMessage. The
//     orchestrator (pkg/orchestration.Orchestrator.Start) is the only caller
//     of Evaluate in the platform — see docs/architecture.md
//     § "Orchestrator: the only place where governance runs".
//   - A non-nil error is treated as a deny by the orchestrator; the safer
//     posture is to fail closed than to admit a malformed message silently.
//   - The shipped policies are stateless and side-effect-free. Planned
//     stateful policies (RateLimit, BreakGlass, Part2Consent) will read
//     pre-bound state from ctx — see docs/governance.md § Stateful policies.
//   - Budget: aim for sub-100µs per policy. Composite.Evaluate is
//     O(n_policies) per message; n is small today but grows with each
//     defence-in-depth addition.
type Policy interface {
	Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error)
}

// CompositePolicy runs multiple policies and denies if any denies.
//
// This pattern lets you build a policy stack such as:
//   - input size limits
//   - sender allowlists
//   - tool authorization checks
//   - content filters
//
// CompositePolicy is the project's safety boundary: cmd/care/main.go (and
// cmd/demo / cmd/caredemo) build one of these at startup, hand it to
// NewOrchestrator, and never reassign it. Adding a new policy is a
// single-line edit at that callsite — no orchestrator change required
// (Bodh design rule 3, registry/bus/governance separation).
type CompositePolicy struct {
	// policies is the ordered list evaluated by Evaluate. Order matters
	// because the first deny short-circuits the rest (see Evaluate).
	// Convention: put the cheapest and most-discriminating checks first
	// (e.g. MaxContentLengthPolicy before scope checks) so deny paths
	// terminate as cheaply as possible.
	policies []Policy
}

// NewComposite constructs a composite policy.
//
// Variadic so callers can write the policy stack inline and read it as a
// declarative manifest of the deployment's safety posture. See
// docs/governance.md § Composition: fail fast for the worked example.
func NewComposite(policies ...Policy) *CompositePolicy {
	return &CompositePolicy{policies: policies}
}

// Evaluate runs all policies in order.
//
// Deny-on-first-failure ("fail fast") is the default because it:
//   - keeps the hot path short — any deny terminates the loop early
//   - yields a single clear Reason for the audit log (less ambiguity for
//     forensic review than a merged multi-reason string)
//   - matches how human reviewers read the policy stack: the first thing
//     that's wrong is usually the thing worth fixing
//
// If you want "collect all reasons", implement an alternative composite —
// CompositePolicy is intentionally not extensible via flags so the
// behaviour stays predictable in audit.
//
// An error from any child is propagated unchanged. The orchestrator then
// treats that error as a deny (logs and short-circuits) — the alternative,
// silent admission on policy malfunction, is the worst safety outcome.
func (c *CompositePolicy) Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error) {
	// Capture "now" once for the whole evaluation. Using a single timestamp
	// keeps the deny-path audit timestamps consistent even when the loop
	// runs across multiple wall-clock instants (negligible today, but
	// matters for ordering invariants once stateful policies land).
	now := time.Now().UTC()

	for _, p := range c.policies {
		// Run the child policy. Per the Policy contract (see interface
		// docstring) this is synchronous, side-effect-free for shipped
		// policies, and must complete inside the per-hop budget.
		res, err := p.Evaluate(ctx, msg)
		if err != nil {
			// Propagate verbatim. The orchestrator's policy-error path
			// logs and denies; do not wrap or annotate here because the
			// caller already has full context (msg.ID, agent_id).
			return PolicyResult{}, err
		}

		if res.Decision == DecisionDeny {
			// Backfill CheckedAt if the child forgot to set it. The audit
			// record needs a non-zero timestamp; without this guard a
			// poorly-written policy could produce events with At=zero
			// that break time-ordered queries against the audit log.
			if res.CheckedAt.IsZero() {
				res.CheckedAt = now
			}
			// Short-circuit: do not evaluate remaining policies. The
			// first deny wins.
			return res, nil
		}
	}

	// All children allowed. Synthesise a single allow result; the Reason
	// "all policies allow" is observable in audit.Event.Reason for the
	// clinical-types allow path and tests in pkg/governance assert it.
	return PolicyResult{
		Decision:  DecisionAllow,
		Reason:    "all policies allow",
		CheckedAt: now,
	}, nil
}

// MaxContentLengthPolicy denies messages whose content exceeds a limit.
//
// Why it exists:
//   - protects the system from runaway payload sizes (DoS / prompt-bomb cap)
//   - bounds prompt cost for LLM-backed downstream agents — a 10 MB
//     "clinical narrative" would otherwise drain a budget on a single call
//   - is the canonical first policy in any deployment's stack; ship it
//     before any per-tenant scoping or PHI logic so even unauthenticated
//     callers hit it cheaply
//
// Cost: O(1) — len(string) in Go is a constant-time field read.
type MaxContentLengthPolicy struct {
	// Max is the inclusive byte cap on msg.Content. cmd/care/main.go uses
	// 65536 (64 KiB) — design-decided as ample for any clinical narrative
	// while small enough that a malicious caller cannot exhaust memory in
	// a tight loop. Increase only with a corresponding budget review.
	Max int
}

// Evaluate applies the content length rule.
//
// Note: We check msg.Content length only. A production policy may consider the
// serialized size of the entire message (including Metadata) and apply limits
// per-role or per-message type. Metadata is currently bounded by application
// convention (small string/number values), not by this policy.
func (p MaxContentLengthPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	// Guard against misconfiguration. A zero/negative Max is almost
	// certainly a wiring bug (e.g. forgetting the field literal). Returning
	// an error rather than silently allowing every message is the
	// fail-closed posture: the orchestrator treats the error as a deny.
	if p.Max <= 0 {
		return PolicyResult{}, errors.New("invalid max content length")
	}
	if len(msg.Content) > p.Max {
		// Deny path. Reason is deliberately short and PHI-free —
		// "content too long" is descriptive without echoing any of the
		// over-long content into the audit log.
		return PolicyResult{
			Decision:  DecisionDeny,
			Reason:    "content too long",
			CheckedAt: time.Now().UTC(),
		}, nil
	}
	return PolicyResult{
		Decision:  DecisionAllow,
		Reason:    "within content length limit",
		CheckedAt: time.Now().UTC(),
	}, nil
}
