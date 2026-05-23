// adt_test.go — tests for readmission risk scoring and ADT event round-trip.
//
// Overview:
//   Exercises ScoreReadmissionRisk with a low-risk young patient and a
//   high-risk elderly patient (multi-condition, uncontrolled, recently
//   discharged) to confirm banding and the 1.0 score clip. Also verifies
//   ADTEvent JSON round-trip integrity.
//
// Key declarations:
//   - TestScoreReadmissionRisk: low- and high-risk banding plus score clipping
//   - TestADTRoundTrip: EncodeADT / DecodeADT preservation test
//
// See doc.go for package-level documentation.

package medical
