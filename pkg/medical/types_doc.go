// types.go — core diagnostic case state and intake/proposal types.
//
// Overview:
//   Defines the SD Bench / MAI-DxO style sequential diagnosis state machine:
//   the running CaseState, the diagnostic test catalog with illustrative
//   costs, the patient intake presentation, and the final diagnosis proposal.
//   Provides JSON codecs so these structures can move through the bus.
//
// Key declarations:
//   - CaseState: running diagnosis context (questions, tests, hypotheses, costs)
//   - DiagnosticTest, TestCatalog: virtual investigations with USD costs
//   - PatientPresentation: initial case intake payload
//   - DiagnosisProposal: final differential + rationale emitted by the panel
//   - EncodeState / DecodeState / EncodePresentation / DecodePresentation: JSON helpers
//
// See doc.go for package-level documentation.

package medical
