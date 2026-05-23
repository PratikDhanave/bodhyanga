// cost_guardian.go — diagnostic spend validator (MAI-DxO cost-value trade-off).
//
// Overview:
//   Inspects the running diagnostic cost on case state and either approves
//   (tests_complete) or denies (tests_revise) the workup back to the
//   supervisor, enforcing the per-case MaxCostUSD budget.
//
// Key declarations:
//   - CostGuardianAgent: stateless agent for budget checks.
//   - CostGuardianAgent.HandleMessage: emits tests_complete or tests_revise.
//
// See doc.go for package-level documentation.

package agents
