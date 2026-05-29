# Bodh governance policies — medical domain.
#
# These are the OPA Rego equivalents of the Go policy structs in
# medical_policy.go. Expressing them in Rego lets operators audit, test
# (opa test), and swap policies without recompiling Go.
#
# The three exported result rules are loaded by pkg/governance.OPAPolicy:
#   data.bodh.governance.medical.case_id_result
#   data.bodh.governance.medical.diagnostic_cost_result
#   data.bodh.governance.medical.tenant_result
#
# Input contract (set by messageToOPAInput in opa_policy.go):
#   input.id         — message UUID
#   input.from       — sender identity
#   input.to         — recipient identity (may be empty string)
#   input.role       — "user" | "system" | "agent" | "tool" | "observer" | "evaluator"
#   input.type       — message type string, e.g. "presentation", "order_test"
#   input.content    — payload string
#   input.created_at — RFC3339 UTC timestamp
#   input.metadata   — metadata map (may be null / empty object)
#
# All results are objects: {"allowed": bool, "reason": string}
#
# Reason strings are PHI-free public contract for the audit log — treat
# changes to Reason values as schema changes (mirrors the Go allow/deny
# helper docstrings in medical_policy.go). SIEM dashboards group by Reason;
# pkg/governance tests assert specific strings.

package bodh.governance.medical

import rego.v1

# clinical_types mirrors clinicalTypes in RequireCaseIDPolicy.Evaluate and
# clinicalMessageTypes in pkg/orchestration. Keep in sync. See governance.md
# § RequireCaseIDPolicy for the rationale for this shared set.
clinical_types := {
	"presentation",
	"case_state",
	"ask_question",
	"order_test",
	"diagnosis",
	"verify_reasoning",
}

# =============================================================================
# case_id_result — mirrors RequireCaseIDPolicy
#
# Denies clinical messages that are missing a non-empty string case_id in
# metadata. Non-clinical messages pass through. Regulatory anchor: HIPAA
# §164.312(b) — case_id is the audit-trail spine.
# =============================================================================

# Default: clinical message without case_id (covers the "metadata present but
# no case_id key" path not matched by any explicit rule below).
default case_id_result := {"allowed": false, "reason": "case_id required for clinical messages"}

# Non-clinical — skip the correlator check.
case_id_result := {"allowed": true, "reason": "non-clinical message"} if {
	not clinical_types[input.type]
}

# Clinical but metadata is null (absent entirely — wiring bug). Uses explicit
# null check rather than `not input.metadata` because an empty object {} is
# truthy in OPA but distinct from null. messageToOPAInput passes null (not {})
# when msg.Metadata is nil in Go, so this rule fires exactly when the caller
# omitted the metadata map.
case_id_result := {"allowed": false, "reason": "missing metadata with case_id"} if {
	clinical_types[input.type]
	input.metadata == null
}

# Clinical + metadata present + valid string case_id.
case_id_result := {"allowed": true, "reason": "case_id present"} if {
	clinical_types[input.type]
	is_string(input.metadata.case_id)
	count(input.metadata.case_id) > 0
}

# =============================================================================
# diagnostic_cost_result — mirrors MaxDiagnosticCostPolicy
#
# Denies order_test messages that would push spent_usd + test_cost_usd above
# max_cost_usd. All other message types pass through. Missing cost fields
# allow (same permissive behaviour as the Go policy — cost_guardian agent is
# the primary enforcement layer; this is defence-in-depth). Regulatory
# anchor: diagnostic budget ceiling; see governance.md § MaxDiagnosticCostPolicy.
# =============================================================================

# Default: any case not matched by an explicit rule (e.g. type is not
# "order_test" and no other rule fires). Mirrors the Go policy's allow-if-
# not-order-test path.
default diagnostic_cost_result := {"allowed": true, "reason": "not a test order"}

# Clinical test order without any metadata (null) — allow; RequireCaseIDPolicy
# upstream in the composite will catch the missing-metadata case.
diagnostic_cost_result := {"allowed": true, "reason": "no cost metadata"} if {
	input.type == "order_test"
	input.metadata == null
}

# Test order: no max_cost_usd ceiling set — allow (unconstrained).
# Use object.get so the absent-key case resolves to null, not undefined.
# `not is_number(undefined)` fails in OPA (undefined propagates through
# built-ins); `not is_number(null)` succeeds (null is not a number → false).
diagnostic_cost_result := {"allowed": true, "reason": "no max_cost_usd constraint"} if {
	input.type == "order_test"
	input.metadata != null
	not is_number(object.get(input.metadata, "max_cost_usd", null))
}

# Test order: no test_cost_usd in the order — allow (cost_guardian catches it).
diagnostic_cost_result := {"allowed": true, "reason": "no test_cost_usd in metadata"} if {
	input.type == "order_test"
	input.metadata != null
	is_number(object.get(input.metadata, "max_cost_usd", null))
	not is_number(object.get(input.metadata, "test_cost_usd", null))
}

# Test order: would exceed budget — deny.
diagnostic_cost_result := {"allowed": false, "reason": "test would exceed diagnostic budget"} if {
	input.type == "order_test"
	input.metadata != null
	max_cost := object.get(input.metadata, "max_cost_usd", null)
	test_cost := object.get(input.metadata, "test_cost_usd", null)
	is_number(max_cost)
	is_number(test_cost)
	spent := object.get(input.metadata, "spent_usd", 0)
	spent + test_cost > max_cost
}

# Test order: within budget — allow.
diagnostic_cost_result := {"allowed": true, "reason": "within diagnostic budget"} if {
	input.type == "order_test"
	input.metadata != null
	max_cost := object.get(input.metadata, "max_cost_usd", null)
	test_cost := object.get(input.metadata, "test_cost_usd", null)
	is_number(max_cost)
	is_number(test_cost)
	spent := object.get(input.metadata, "spent_usd", 0)
	spent + test_cost <= max_cost
}

# =============================================================================
# tenant_result — mirrors RequireTenantPolicy
#
# Denies any message missing a non-empty string tenant_id in metadata.
# Regulatory anchor: HIPAA §164.308(a)(4) information-access management;
# tenant_id is the spine that downstream PostgreSQL RLS policies rely on.
# =============================================================================

# Default: metadata present but tenant_id key absent or wrong type.
default tenant_result := {"allowed": false, "reason": "tenant_id required"}

# No metadata map at all (null) — distinct reason so dashboards distinguish
# wiring bugs ("forgot the map entirely") from auth/gateway misconfigurations.
# Uses == null for the same reason as case_id_result above.
tenant_result := {"allowed": false, "reason": "tenant_id required: no metadata"} if {
	input.metadata == null
}

# Metadata present, tenant_id is a non-empty string — allow.
tenant_result := {"allowed": true, "reason": "tenant_id present"} if {
	is_string(input.metadata.tenant_id)
	count(input.metadata.tenant_id) > 0
}
