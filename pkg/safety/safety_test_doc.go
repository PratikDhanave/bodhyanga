// safety_test.go — unit tests for the safety guardrail checks.
//
// Overview:
//   Validates that refusal-phrase detection, hallucination grounding
//   heuristics, and PHI-shape detection behave deterministically and produce
//   well-formed Verdicts. Each table-driven test isolates one check; no
//   external dependencies or shared state.
//
// Key declarations:
//   - TestIsRefusal_DetectsCommonPhrases: covers at least five refusal phrases.
//   - TestIsRefusal_NoFalsePositiveOnClinicalText: confirms clinical prose does not trip the detector.
//   - TestCheckHallucination_GroundedClaimPasses: significant claim terms found in citations -> OK:true.
//   - TestCheckHallucination_NoCitationsFails: empty citation slice -> OK:false with reason.
//   - TestCheckHallucination_EmptyClaimPasses: claim with no significant terms -> OK:true.
//   - TestCheckHallucinationThreshold_Respected: same pair passes at 0.3, fails at 0.9.
//   - TestContainsPHIPatterns_DetectsSSN_Phone_MRN: each of the three regexes fires on its shape.
//   - TestContainsPHIPatterns_CleanTextPasses: benign clinical prose -> OK:true.
//
// See doc.go for package-level documentation.

package safety
