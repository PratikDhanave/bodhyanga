// diagnostician_cascade.go — cost-aware small→big cascade diagnostician.
//
// Overview:
//   CascadeDiagnostician composes two fully-wired LLMDiagnosticians under the
//   shared "diagnostician" agent ID. Calls are routed to a cheap SmallTier
//   first; if its DiagnosisProposal's confidence is at least EscalateAt
//   (default 0.6) the small-tier output is forwarded unchanged. Otherwise the
//   same input message is replayed against the more capable LargeTier and that
//   emission is returned instead. Both tiers reuse the base LLMDiagnostician's
//   prompt, RAG store, JSON schema and rule-based fallback — the cascade owns
//   only the tier-selection policy.
//
// Fallback-detection heuristic:
//   A small-tier LLM failure (timeout, transport error, malformed JSON,
//   below-MinConfidence answer) triggers the wrapped LLMDiagnostician's
//   rule-based fallback, which always emits a confidence at-or-below 0.45
//   (see DiagnosticianAgent.inferDiagnosis). Those proposals therefore land
//   below the 0.6 default EscalateAt and naturally trigger an escalation —
//   the cascade never needs to introspect log lines or sniff for a fallback
//   marker; a low confidence is the signal, regardless of source.
//
// Key declarations:
//   - CascadeDiagnostician: struct with SmallTier, LargeTier, EscalateAt,
//     Timeout
//   - defaultCascadeEscalateAt: the 0.6 threshold used when EscalateAt is 0
//   - CascadeDiagnostician.HandleMessage: small-then-big routing entry point
//   - extractProposalConfidence: decodes the first diagnosis_proposal in an
//     emitted message slice and returns its Confidence
//   - tierIdentity: provider/model strings for logging
//
// See doc.go for package-level documentation.

package agents
