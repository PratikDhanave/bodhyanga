// diagnostician_test.go — unit tests for the rule-based diagnostician.
//
// Overview:
//   Exercises every specialty branch added when SD-Bench surfaced the
//   "always-CAP" misclassification on the rule provider. Each row pairs a
//   synthetic CaseState (the kind test_planner's simulator emits) with the
//   expected working diagnosis and a confidence floor.
//
// Key declarations:
//   - TestInferDiagnosisSpecialtyRules: table-driven coverage of the
//     ACS, DKA, sepsis (Urosepsis), CHF (BNP and RPM-alert variants),
//     CAP, and Undifferentiated fallback paths.
//   - TestExtractFirstNumberHonorsMarker: pins down the marker-after
//     extraction contract that lets the DKA rule read bicarb from a
//     BMP string that also contains glucose.
//   - TestExtractWBC: small helper-level coverage for the CBC parser.
//
// See doc.go for package-level documentation.

package agents
