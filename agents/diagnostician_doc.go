// diagnostician.go — MAI-DxO "Provide Diagnosis" inference from accumulated evidence.
//
// Overview:
//   Reads the case state and proposes a working diagnosis (with confidence
//   and rationale) from rule-based inference over test results and RPM
//   alerts, then forwards to reasoning_verifier as a diagnosis_proposal.
//
// Key declarations:
//   - DiagnosticianAgent: stateless diagnosis inference agent.
//   - DiagnosticianAgent.HandleMessage: emits diagnosis_proposal to verifier.
//   - inferDiagnosis: rule-based picker — fires specialty rules for ACS
//     (troponin), DKA (glucose/bicarb/ketones), sepsis (lactate+WBC),
//     CHF (BNP or RPM alerts), and the legacy CAP rule. Confidence
//     thresholds and numeric cutoffs are textbook-illustrative only.
//   - isTroponinElevated / extractFirstNumber / extractWBC: result-string
//     parsers shared by the specialty rules.
//   - mergeStateMeta: stashes case_state into metadata for the verifier.
//
// See doc.go for package-level documentation.

package agents
