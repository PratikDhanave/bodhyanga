# Authorization policy for GET /explain/{case_id} — Bodh right-to-explanation
# endpoint (GDPR Article 22(3) + Article 15(1)(h)).
#
# Authored for Bodh. Adapted in shape from
# https://github.com/ardanlabs/service/blob/master/app/sdk/auth/rego/authorization.rego
# (Apache 2.0). Bodh-specific role catalogue + tenant scoping.
#
# Input contract (set by pkg/mid.Authorize):
#   input.Roles      — array of role strings from the JWT claims.
#   input.TenantID   — the JWT's tenant_id claim (the data subject's tenant).
#   input.ResourceTenantID — the tenant the resource being read belongs to.
#                            For /explain/{case_id} this is the binary's
#                            BODH_TENANT_ID (single-tenant binary today).
#
# Output:
#   data.bodh.explain.allow — true iff the caller may read this case's
#                             structured explanation.
#
# Policy:
#   * `clinician` or `compliance_officer` may read explanations for cases
#     in *their* tenant.
#   * `auditor` may read explanations across *all* tenants (read-only role
#     by deployment policy; write endpoints reject auditor separately).
#   * Every other role -> deny -> middleware returns 403.

package bodh.explain

import rego.v1

default allow := false

# Same-tenant read for clinician or compliance_officer.
allow if {
	some role in input.Roles
	role in {"clinician", "compliance_officer"}
	input.TenantID == input.ResourceTenantID
}

# Cross-tenant read for auditor.
allow if {
	some role in input.Roles
	role == "auditor"
}
