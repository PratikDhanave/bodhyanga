// policy.go — core Policy interface and generic policy building blocks.
//
// Overview:
//   Defines the Decision enum, the explainable PolicyResult, the Policy
//   interface the orchestrator evaluates before dispatching to agents, and
//   two reusable policies: a deny-on-first-failure CompositePolicy and a
//   MaxContentLengthPolicy for bounding payload sizes.
//
// Key declarations:
//   - Decision / DecisionAllow, DecisionDeny: policy outcomes.
//   - PolicyResult: decision plus human-readable Reason and CheckedAt timestamp.
//   - Policy: Evaluate(ctx, msg) contract enforced at the orchestrator boundary.
//   - CompositePolicy / NewComposite: stack of policies; first deny short-circuits.
//   - MaxContentLengthPolicy: denies messages whose Content exceeds Max bytes.
//
// See doc.go for package-level documentation.

package governance
