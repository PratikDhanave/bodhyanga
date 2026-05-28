// auth_test.go — unit tests for the JWT + OPA Rego stack.
//
// Coverage:
//   - Token roundtrip (Generate + Parse).
//   - Expired token surfaces as ErrInvalidToken (-> 401 at middleware).
//   - Missing Bearer header surfaces as ErrMissingBearer.
//   - Authorize_ExplainRoleGate: clinician same-tenant allow,
//     compliance_officer allow, auditor cross-tenant allow, nurse deny,
//     clinician cross-tenant deny.
//   - Authorize_ReviewsGate: nurse list allow, nurse decide deny,
//     clinician decide allow.
//   - Authorize_AuditGate: auditor allow, clinician deny.
package auth
