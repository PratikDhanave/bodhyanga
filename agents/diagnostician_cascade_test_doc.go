// diagnostician_cascade_test.go — tests for CascadeDiagnostician.
//
// Overview:
//   Uses pairs of llm.Mock clients (one per tier) wired into two
//   LLMDiagnosticians to verify the cascade's tier-selection policy. Each
//   case asserts both behaviour (which diagnosis surfaces) and accounting
//   (how many times each tier was actually called), so the cost-avoidance
//   guarantee — large tier is touched only when needed — is testable.
//
// Key declarations:
//   - newCascadeForTest: helper that wires two mocks into a fresh
//     CascadeDiagnostician at a given escalation threshold
//   - TestCascade_HighConfidenceSkipsLarge: small clears 0.6 → large.Calls
//     length must be zero
//   - TestCascade_LowConfidenceEscalates: small returns 0.3 → large is
//     invoked and its proposal is the emitted one
//   - TestCascade_SmallFallsBackEscalatesToLarge: small LLM errors → small
//     runs its rule-based fallback (confidence ≤ 0.45) → cascade detects the
//     sub-threshold confidence and escalates to large
//   - TestCascade_DefaultEscalateAtIs0_6: zero-value EscalateAt must behave
//     like 0.6, and equality-at-threshold must NOT escalate
//
// See doc.go for package-level documentation.

package agents
