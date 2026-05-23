// reasoning_verifier.go — MAI-DxO self-verification step before final diagnosis.
//
// Overview:
//   Validates a DiagnosisProposal's confidence and rationale, updates the
//   case state with the working diagnosis, and routes onward to the
//   diagnostic_supervisor as a diagnosis message (optionally HITL-gated for
//   FDA CDS-style clinician sign-off).
//
// Key declarations:
//   - ReasoningVerifierAgent: agent with HITL flag.
//   - ReasoningVerifierAgent.HandleMessage: verifies and forwards or gates.
//
// See doc.go for package-level documentation.

package agents
