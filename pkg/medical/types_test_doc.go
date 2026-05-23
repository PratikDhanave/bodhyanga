// types_test.go — round-trip tests for CaseState JSON encoding.
//
// Overview:
//   Verifies that EncodeState / DecodeState preserve key CaseState fields
//   (CaseID, totals, hypotheses) so the diagnosis state machine can be
//   safely transported across the agent bus.
//
// Key declarations:
//   - TestEncodeDecodeState: encode-then-decode round-trip assertion
//
// See doc.go for package-level documentation.

package medical
