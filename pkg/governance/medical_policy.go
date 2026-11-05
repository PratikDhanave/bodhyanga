package governance

import (
	"context"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// RequireCaseIDPolicy denies clinical messages missing case_id in metadata.
//
// Regulatory anchor: HIPAA §164.312(b) audit controls require correlation
// strong enough that an investigator can reconstruct who-did-what-when. The
// case_id is the spine that ties every audit event for a single sequential-
// diagnosis flow together — without it, the audit log becomes a soup of
// uncorrelated rows. See docs/governance.md § RequireCaseIDPolicy and
// docs/architecture.md § Audit pipeline.
//
// Failure mode prevented: a malformed clinical message reaching the agent
// pipeline without a correlator, producing audit events that cannot be
// joined into a case trace. The downstream consequences are forensic
// blindness on incident review and broken cohort analytics.
//
// Example deny audit record (illustrative):
//
//	{kind: "policy_decision", decision: "deny",
//	 reason: "case_id required for clinical messages",
//	 type: "presentation", from: "user", to: "intake"}
//
// Aligns with MARA governance: fail closed before agents handle PHI-bearing
// payloads (Bodhyanga design rule 4).
type RequireCaseIDPolicy struct{}

// Evaluate enforces the case_id correlator on clinical message types.
//
// Non-clinical types (system broadcasts, observer telemetry, evaluator
// rubrics) pass through with an "allow non-clinical" reason; they don't
// carry PHI and aren't required to participate in the case-correlated
// audit trail.
func (RequireCaseIDPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	// Source of truth for "what counts as clinical". The orchestrator
	// (pkg/orchestration.clinicalMessageTypes) mirrors this set to decide
	// which policy *allows* are worth recording — they share the literal
	// intentionally, with a code comment in orchestrator.go noting the
	// mirror. If you add a new clinical type, update BOTH.
	clinicalTypes := map[string]bool{
		"presentation": true, "case_state": true, "ask_question": true,
		"order_test": true, "diagnosis": true, "verify_reasoning": true,
	}
	if !clinicalTypes[msg.Type] {
		// Non-clinical: skip the correlator check. This is what lets
		// system/observer/evaluator traffic flow without a case_id.
		return allow("non-clinical message"), nil
	}

	// From here on: clinical message. case_id is mandatory.
	if msg.Metadata == nil {
		// Distinct deny reason from the next case so dashboards can
		// distinguish "caller forgot the metadata map entirely" (often
		// a wiring bug) from "metadata present but no case_id key".
		return deny("missing metadata with case_id"), nil
	}
	if _, ok := msg.Metadata["case_id"].(string); !ok {
		// Type-assert to string explicitly — a non-string case_id (e.g.
		// numeric or nil) should fail, not silently coerce.
		return deny("case_id required for clinical messages"), nil
	}
	return allow("case_id present"), nil
}

// MaxDiagnosticCostPolicy denies test orders that would exceed the per-case
// USD budget declared in metadata.
//
// Failure mode prevented: a runaway agent (LLM-backed or rule-based)
// ordering an unbounded series of tests and bankrupting a virtual budget
// or, in a real deployment, charging an insurer / patient for unauthorised
// workup. The cost_guardian agent already enforces this at the agent
// layer; this policy is defence-in-depth, and the chokepoint for any
// future MCP-driven tool that can directly order a test bypassing the
// supervisor — see docs/governance.md § MaxDiagnosticCostPolicy.
//
// Regulatory anchor: SD-Bench cost ceiling (project convention) and, in
// production, a precondition for any clinically-consequential test order
// that needs prior auth or insurer pre-approval (see pkg/tasks and the
// prior_auth agent).
//
// Example deny audit record:
//
//	{kind: "policy_decision", decision: "deny",
//	 reason: "test would exceed diagnostic budget",
//	 type: "order_test"}
//
// Cost: O(1) — three map lookups and one float comparison.
type MaxDiagnosticCostPolicy struct{}

// Evaluate gates only on order_test messages. Other message types pass
// through cheaply; the cost ceiling only matters at the point a test is
// being ordered.
func (MaxDiagnosticCostPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	if msg.Type != "order_test" {
		// Out of scope for this policy. Returning allow keeps the
		// composite chain moving without spending cycles on metadata
		// lookups that wouldn't matter here.
		return allow("not a test order"), nil
	}
	if msg.Metadata == nil {
		// No metadata at all. We allow rather than deny because the
		// RequireCaseIDPolicy will catch missing-metadata for clinical
		// types upstream in the composite. Doubling up on the deny
		// would muddy the reason in the audit log.
		return allow("no cost metadata"), nil
	}

	// max_cost_usd is the case-level ceiling, typically set on the
	// presentation message and carried forward. Absent => no budget
	// constraint, so allow. Production deployments should ensure this
	// is always populated by the intake agent.
	maxCost, ok := msg.Metadata["max_cost_usd"].(float64)
	if !ok {
		return allow("no max_cost_usd constraint"), nil
	}
	// test_cost_usd is the marginal cost of this specific test order.
	// Missing here means the test_planner didn't price its proposal —
	// we allow rather than deny so a misconfigured planner doesn't
	// silently break the whole flow; the cost_guardian agent will
	// catch it. (Defence-in-depth, not defence-in-strictness.)
	testCost, ok := msg.Metadata["test_cost_usd"].(float64)
	if !ok {
		return allow("no test_cost_usd in metadata"), nil
	}
	// spent_usd is the running case total. Absent => treat as zero
	// (the first test order in a case). The ignored ok is deliberate:
	// a missing key here means "nothing spent yet" which is valid.
	spent, _ := msg.Metadata["spent_usd"].(float64)
	if spent+testCost > maxCost {
		// Deny: this order would tip the case over budget. Note the
		// Reason mentions "budget" only — no actual USD figures land
		// in the audit log (the dollar amounts would not technically
		// be PHI, but echoing budget internals into the audit pipeline
		// expands the on-disk surface unnecessarily).
		return deny("test would exceed diagnostic budget"), nil
	}
	return allow("within diagnostic budget"), nil
}

// ResearchDisclaimerPolicy is a non-blocking intent marker.
//
// It always allows. Its sole purpose is to stamp every message's audit
// allow event with a Reason that documents deployment intent. Production
// deployments REPLACE this policy with a real ProductionAttestationPolicy
// requiring a signed assertion that the deployment context is approved
// (BAA in place, clinical validation completed, regulatory clearance
// where applicable). See docs/governance.md § ResearchDisclaimerPolicy
// and the disclaimer at the top of README.md.
//
// Why ship a no-op policy at all? Because the Reason string lands in the
// audit log: anyone forensically reviewing a Bodhyanga case trace can confirm
// from the audit pipeline alone that the system was running in research
// mode at the time. The presence/absence of this Reason in production
// audit dumps is itself a compliance check.
type ResearchDisclaimerPolicy struct{}

// Evaluate always allows. The Reason is the load-bearing contribution.
func (ResearchDisclaimerPolicy) Evaluate(_ context.Context, _ protocol.Message) (PolicyResult, error) {
	return allow("research demo — not for clinical use"), nil
}

// RequireTenantPolicy denies any message missing tenant_id in metadata.
//
// Failure mode prevented: a message reaching an agent without a tenant
// correlator, then being written to Postgres without app.tenant_id set —
// at best a foreign-key error, at worst a cross-tenant data leak if a
// future code path bypasses the withTenant helper. With this policy in
// the composite, no agent ever receives a tenant-less message, and the
// RLS layer downstream can rely on tenant_id being present in every
// audit/eval/patient write.
//
// Pair with: pkg/persistence/postgres which calls
// set_config('app.tenant_id', ..., true) per transaction; the RLS
// policy is USING (tenant_id = current_setting('app.tenant_id', true)).
// Operators enabling multi-tenancy compose this into their CompositePolicy
// so every hop carries a tenant correlator that downstream stores can
// enforce (e.g. PostgreSQL row-level security).
//
// Regulatory anchor: HIPAA §164.308(a)(4) information-access management
// — tenant isolation is the architectural primitive that lets one Bodhyanga
// deployment serve multiple covered entities under separate BAAs.
//
// Example deny audit record:
//
//	{kind: "policy_decision", decision: "deny",
//	 reason: "tenant_id required", type: "presentation"}
type RequireTenantPolicy struct{}

// Evaluate denies on missing metadata or missing/empty tenant_id.
//
// Note that the two deny reasons differ: "no metadata" vs. "tenant_id
// required". Dashboards can distinguish "caller forgot the map entirely"
// (likely a wiring bug) from "metadata present but tenant_id missing"
// (likely an auth/gateway misconfiguration).
func (RequireTenantPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	if msg.Metadata == nil {
		return deny("tenant_id required: no metadata"), nil
	}
	// Both unset (!ok) and empty-string ("") are treated as missing. The
	// empty-string check matters because gateways sometimes inject an
	// empty header value when no tenant is bound to a token; we treat
	// that as "absent" rather than "the empty-tenant tenant".
	if v, ok := msg.Metadata["tenant_id"].(string); !ok || v == "" {
		return deny("tenant_id required"), nil
	}
	return allow("tenant_id present"), nil
}

// RequireScopePolicy denies a message if the SMART-on-FHIR scope expected for
// its Type is missing from metadata.scopes (space-separated string, as in OAuth
// 2.0 token introspection). Required maps message Type -> required scope.
//
// Regulatory anchor: SMART-on-FHIR scope semantics (HL7 specification) and,
// indirectly, HIPAA §164.308(a)(4) access-management — the scope assertion
// is how a gateway proves the caller is authorised for the FHIR operation
// the message implies.
//
// Failure mode prevented: a caller with a token scoped to read-only
// access sending a write-type message (e.g. cdm_request with a
// patient-roster read scope). Without this policy the orchestrator
// would happily route the message to the cdm_planner agent, which has
// no reason to re-check authorisation.
//
// Wiring: the API gateway is expected to validate the bearer token,
// extract scopes from the introspection response, and inject them into
// msg.Metadata["scopes"] BEFORE the message reaches the bus. This
// policy is the chokepoint that confirms the injection happened and
// the scopes match what the operation needs.
//
// Example:
//
//	RequireScopePolicy{Required: map[string]string{
//	    "cdm_request":      "user/CarePlan.write",
//	    "discharge_event":  "user/Encounter.read",
//	}}
//
// Example deny audit record:
//
//	{kind: "policy_decision", decision: "deny",
//	 reason: "required scope missing: user/CarePlan.write",
//	 type: "cdm_request"}
type RequireScopePolicy struct {
	// Required is the per-Type scope requirement table. Types absent
	// from the map are allowed without scope checks — keep the table
	// explicit so operators can audit which operations are gated.
	Required map[string]string
}

// Evaluate runs the per-type scope check.
//
// Cost: O(n_scopes) on a typical token (≤ ~10 scopes). The linear scan
// is faster than building a set for inputs this small and avoids any
// per-message allocation in the allow path.
func (p RequireScopePolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	required, ok := p.Required[msg.Type]
	if !ok {
		// Type is not gated by this policy. Allow.
		return allow("no scope required for type"), nil
	}
	if msg.Metadata == nil {
		// Required scope, but no metadata at all — deny. The Reason
		// includes the required scope so operators can immediately see
		// what was expected without cross-referencing the policy config.
		return deny("scopes missing for " + required), nil
	}
	// scopes is a space-separated string per OAuth 2.0 token-introspection
	// convention. The "_" intentionally ignores the ok flag: a missing
	// "scopes" key is treated the same as an empty value, which the
	// strings.Fields loop below will handle (zero iterations → deny).
	scopes, _ := msg.Metadata["scopes"].(string)
	for _, s := range strings.Fields(scopes) {
		if s == required {
			// Match found — allow. The Reason embeds the matched scope
			// so the audit log records exactly which scope was honoured
			// (helpful for revocation impact analysis).
			return allow("scope " + required + " present"), nil
		}
	}
	// No matching scope after exhausting the list — deny.
	return deny("required scope missing: " + required), nil
}

// allow constructs an allow PolicyResult with the given Reason.
//
// IMPORTANT: the Reason string is part of the audit pipeline's public
// contract. It flows into audit.Event.Reason (see
// pkg/audit.FromPolicyDecision) where SIEM dashboards group by it and
// pkg/governance tests assert specific values. Treat Reason changes as
// schema changes — bump tests and dashboards together.
//
// CheckedAt is set to UTC for cross-cluster consistency; no audit query
// should ever need to do timezone math.
func allow(reason string) PolicyResult {
	return PolicyResult{
		Decision:  DecisionAllow,
		Reason:    reason,
		CheckedAt: time.Now().UTC(),
	}
}

// deny constructs a deny PolicyResult with the given Reason.
//
// Same audit-pipeline contract as allow(): the Reason is observable in
// every produced audit.Event for a denied message and is the single most
// useful piece of forensic information for "why didn't this message
// reach an agent?" investigations. Keep Reasons:
//   - specific (avoid generic "denied")
//   - actionable (the operator should know what to fix)
//   - PHI-free (never echo msg.Content or patient identifiers)
//
// See docs/governance.md § Audit events for the field-selection rationale.
func deny(reason string) PolicyResult {
	return PolicyResult{
		Decision:  DecisionDeny,
		Reason:    reason,
		CheckedAt: time.Now().UTC(),
	}
}
