// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/auth/rules.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga:
//   - Different rego module set (authentication + explain + reviews + audit).
//   - Module string constants exported so middleware in pkg/mid can pick a
//     module per-route.

package auth

import _ "embed"

// Module names — the embedded Rego source for each policy. Exported because
// pkg/mid.Authorize selects which module to evaluate per route.
var (
	//go:embed rego/authentication.rego
	regoAuthentication string

	// ModuleExplain is the Rego module for GET /explain/{case_id}.
	// The exposed rule is "data.bodhyanga.explain.allow".
	//
	//go:embed rego/explain.rego
	ModuleExplain string

	// ModuleReviews is the Rego module for /api/reviews + /api/reviews/{id}
	// + /api/reviews/{id}/decide. The exposed rule is
	// "data.bodhyanga.reviews.allow"; the policy switches on input.Method.
	//
	//go:embed rego/reviews.rego
	ModuleReviews string

	// ModuleAudit is the Rego module for GET /api/audit (auditor only).
	// The exposed rule is "data.bodhyanga.audit.allow".
	//
	//go:embed rego/audit.rego
	ModuleAudit string
)

// Rule queries — paired with the modules above. Constants keep call sites
// in pkg/mid free of string-typo risk.
const (
	QueryExplainAllow = "data.bodhyanga.explain.allow"
	QueryReviewsAllow = "data.bodhyanga.reviews.allow"
	QueryAuditAllow   = "data.bodhyanga.audit.allow"
)

// Role catalogue — the canonical set of role strings that appear in
// Claims.Roles and in input.Roles for the Rego policies. The Rego
// policies hard-code the same strings; if a role is renamed here it
// must be renamed in the Rego too.
const (
	RoleClinician         = "clinician"
	RoleNurse             = "nurse"
	RoleCareCoordinator   = "care_coordinator"
	RoleComplianceOfficer = "compliance_officer"
	RoleAuditor           = "auditor"
	RoleAdmin             = "admin"
)
