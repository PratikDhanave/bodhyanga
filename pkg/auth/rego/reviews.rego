# Authorization policy for /api/reviews — Bodhyanga HITL gate (21 USC §360j(o)(1)
# CDS carve-out criterion 4 "independent review" surface).
#
# Authored for Bodhyanga. Adapted in shape from
# https://github.com/ardanlabs/service/blob/master/app/sdk/auth/rego/authorization.rego
# (Apache 2.0).
#
# This policy enforces the AGENTS.md invariant: HITL gate is mandatory for
# clinical actions, and only clinicians or compliance officers may approve
# or reject a clinically consequential queued review. Nurses (and the
# care_coordinator role for refill triage flows) may list pending reviews
# to triage their work but may not decide them.
#
# Input contract:
#   input.Roles    — array of role strings from JWT claims.
#   input.Method   — HTTP method ("GET", "POST", "PUT", "DELETE").
#   input.Path     — request URL path (e.g. "/api/reviews",
#                    "/api/reviews/{id}", "/api/reviews/{id}/decide").
#   input.TenantID — JWT's tenant_id claim.
#   input.ResourceTenantID — binary's BODHYANGA_TENANT_ID.
#
# Policy summary:
#   GET  /api/reviews            -> clinician | compliance_officer | nurse
#                                   | care_coordinator (same tenant)
#   GET  /api/reviews/{id}       -> same as list
#   POST /api/reviews/{id}/decide -> clinician | compliance_officer ONLY,
#                                    same tenant. NEVER nurse.

package bodhyanga.reviews

import rego.v1

default allow := false

# Read-only list / get — nurses & coordinators may triage their queue.
allow if {
	input.Method == "GET"
	some role in input.Roles
	role in {"clinician", "compliance_officer", "nurse", "care_coordinator"}
	input.TenantID == input.ResourceTenantID
}

# Decide (approve / reject / modify) — clinician or compliance_officer only.
allow if {
	input.Method == "POST"
	some role in input.Roles
	role in {"clinician", "compliance_officer"}
	input.TenantID == input.ResourceTenantID
}
