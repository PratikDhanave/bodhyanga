// redteam_test.go — tests for the red-team loader and the per-category
// safety scorer.
//
// Overview:
//   The loader test reads data/eval/redteam.json (a JSON array of >=12
//   adversarial fixtures) and verifies each has attack metadata. The scorer
//   tests stub a DiagnosisProposal for each attack category and assert
//   that ScoreRedTeamOutcome returns safe / unsafe / uncertain per the
//   documented heuristics.
//
// Key declarations:
//   - TestLoadRedTeamSuite_ReadsFixture
//   - TestLoadRedTeamSuite_MissingFile
//   - TestScoreRedTeamOutcome_PromptInjectionRefusedIsSafe
//   - TestScoreRedTeamOutcome_PromptInjectionEchoIsUnsafe
//   - TestScoreRedTeamOutcome_JailbreakIgnoredIsSafe
//   - TestScoreRedTeamOutcome_HallucinationFabricationIsUnsafe
//   - TestScoreRedTeamOutcome_HallucinationLowConfidenceIsSafe
//   - TestScoreRedTeamOutcome_PHIInRationaleIsUnsafe
//   - TestScoreRedTeamOutcome_PHIRedactedIsSafe
//   - TestScoreRedTeamOutcome_LowConfidenceForMissingInfoIsSafe
//   - TestScoreRedTeamOutcome_OverconfidentMissingInfoIsUnsafe
//   - TestScoreRedTeamOutcome_ContradictoryAcknowledgedIsSafe
//   - TestScoreRedTeamOutcome_DangerousSolicitationRefusedIsSafe
//   - TestScoreRedTeamOutcome_DangerousSolicitationDoseNamedIsUnsafe
//   - TestScoreRedTeamOutcome_OutOfDomainDisclaimedIsSafe
//   - TestScoreRedTeamOutcome_DistractionMatchesGoldIsSafe
//   - TestScoreRedTeamOutcome_DistractionMissesGoldIsUnsafe
//   - TestScoreRedTeamOutcome_UnknownCategoryIsUncertain
//
// See doc.go for package-level documentation.

package eval
