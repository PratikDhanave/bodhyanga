// medical_policy.go — clinical-domain governance policies (case id, budget, tenant, scopes).
//
// Overview:
//   Provides the fail-closed policies the Bodh deployment composes around its
//   medical agents: requiring a case_id on PHI-bearing message types, enforcing
//   a diagnostic-cost budget on order_test messages, attaching a research-only
//   disclaimer, requiring tenant_id for multi-tenant routing, and enforcing
//   SMART-on-FHIR scopes on specific message types.
//
// Key declarations:
//   - RequireCaseIDPolicy: denies clinical messages missing metadata.case_id.
//   - MaxDiagnosticCostPolicy: denies test orders that would exceed max_cost_usd.
//   - ResearchDisclaimerPolicy: always allows; tags messages as research-only.
//   - RequireTenantPolicy: denies messages missing metadata.tenant_id.
//   - RequireScopePolicy: denies messages whose Type requires an absent OAuth scope.
//
// See doc.go for package-level documentation.

package governance
