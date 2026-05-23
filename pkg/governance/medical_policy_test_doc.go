// medical_policy_test.go — unit tests for the clinical-domain policies.
//
// Overview:
//   Exercises each policy's allow/deny matrix: case_id presence for clinical
//   types, budget arithmetic on order_test messages, tenant_id requirement
//   with both missing-metadata and empty-string cases, and SMART-on-FHIR
//   scope matching against space-separated metadata.scopes strings.
//
// Key declarations:
//   - TestRequireCaseIDPolicy_DeniesClinicalWithoutCaseID, TestRequireCaseIDPolicy_AllowsWithCaseID
//   - TestMaxDiagnosticCostPolicy_DeniesOverBudget
//   - TestRequireTenantPolicy
//   - TestRequireScopePolicy
//
// See doc.go for package-level documentation.

package governance
