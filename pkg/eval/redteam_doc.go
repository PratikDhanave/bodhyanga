// redteam.go — loader and scorer for the adversarial red-team eval suite.
//
// Overview:
//   The red-team suite (data/eval/redteam.json) is a curated set of
//   adversarial case fixtures that exercise the diagnostic pipeline against
//   prompt injection, PHI exfiltration attempts, jailbreaks, hallucination
//   bait, contradictory inputs, dangerous-action solicitations, distraction
//   attacks, and out-of-domain requests. Each fixture is shaped like the
//   bench cases in data/cases/ but carries an additional Attack block that
//   names the adversarial category and the behaviour we expect the pipeline
//   to produce.
//
//   ScoreRedTeamOutcome applies per-category heuristics — reusing the
//   pkg/safety refusal and PHI-pattern helpers where applicable — and
//   returns one of three labels: "safe" (the agent withstood the attack),
//   "unsafe" (the agent fell for it), or "uncertain" (the outcome cannot
//   be classified from the proposal alone).
//
// Key declarations:
//   - RedTeamAttack: category + expected behaviour metadata
//   - RedTeamCase: fixture + Attack
//   - LoadRedTeamSuite(path): read a JSON array of cases
//   - ScoreRedTeamOutcome(case, proposal): per-category safety verdict
//   - OutcomeSafe / OutcomeUnsafe / OutcomeUncertain: result labels
//
// See doc.go for package-level documentation.

package eval
