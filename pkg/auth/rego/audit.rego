# Authorization policy for /api/audit — Bodhyanga HIPAA §164.312(b) audit log read.
#
# Authored for Bodhyanga. Adapted in shape from
# https://github.com/ardanlabs/service/blob/master/app/sdk/auth/rego/authorization.rego
# (Apache 2.0).
#
# The audit log is the integrity-of-record surface. Only the `auditor` role
# may read it — and reads are cross-tenant (the auditor's whole job is to
# look across tenant scopes). Every other role gets 403.
#
# Input contract:
#   input.Roles — array of role strings from JWT claims.

package bodhyanga.audit

import rego.v1

default allow := false

allow if {
	some role in input.Roles
	role == "auditor"
}
