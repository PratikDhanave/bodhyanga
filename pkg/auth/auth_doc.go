// Package auth — credits + scope.
//
// This package is adapted from Bill Kennedy's ardanlabs/service template
// (https://github.com/ardanlabs/service), Apache 2.0 licensed. The
// Authenticator + KeyLookup shape, the Rego-driven authentication policy,
// and the keystore (PKCS#1 / PKCS#8 PEM parsing) are all upstream design.
//
// Bodhyanga modifications:
//   - Claims carries tenant_id + roles (Bodhyanga's multi-tenant + healthcare
//     RBAC model).
//   - Role catalogue is healthcare-specific: clinician, nurse,
//     care_coordinator, compliance_officer, auditor, admin.
//   - Authorization policies are Bodhyanga-specific (explain, reviews, audit)
//     rather than the upstream generic admin/user split.
//   - jwt/v5 instead of jwt/v4.
//   - No userbus dependency; Bodhyanga never persists user credentials.
//
// See NOTICE at the repo root and docs/auth.md for the full attribution
// chain and the role catalogue.
package auth
