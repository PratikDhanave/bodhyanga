// careplan_test.go — tests for per-condition care plan construction and codec.
//
// Overview:
//   Validates that BuildCarePlan emits the expected first goal metric and
//   medication for each supported condition (HTN, DM, CHF, CKD, COPD) and
//   that the JSON round-trip via EncodeCarePlan / DecodeCarePlan preserves
//   key fields.
//
// Key declarations:
//   - TestBuildCarePlanPerCondition: table-driven per-condition checks
//   - TestCarePlanRoundTrip: JSON encode/decode preservation test
//
// See doc.go for package-level documentation.

package medical
