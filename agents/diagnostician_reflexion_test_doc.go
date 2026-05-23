// diagnostician_reflexion_test.go — tests for ReflexionDiagnostician.
//
// Overview:
//   Uses a small scripted llm.Client (scriptedLLM) that pops a queued
//   sequence of responses so a single test can stage a draft call followed
//   by one or more critique calls. The shared draftResponse() returns the
//   initial proposal; critiqueVerified / critiqueRejectWithRevision /
//   critiqueRejectNoRevision return the three critique outcomes the loop
//   has to handle. Tests cover the verified-first-round path, single-round
//   adoption of a revision, best-effort tolerance of critique errors,
//   fallback when the initial draft fails, two-round convergence (both
//   "revise twice" and "revise then verify"), and the default-MaxRounds=1
//   guarantee.
//
// Key declarations:
//   - scriptedLLM / scriptedStep: queued multi-call llm.Client
//   - draftResponse / critiqueVerified / critiqueRejectWithRevision /
//     critiqueRejectNoRevision: canned response builders
//   - TestReflexion_VerifiedFirstRoundEmitsOriginal
//   - TestReflexion_UnverifiedRoundAdoptsRevised
//   - TestReflexion_CritiqueErrorKeepsOriginal
//   - TestReflexion_InitialFailsFallsBack
//   - TestReflexion_MultipleRoundsConverge
//   - TestReflexion_MultipleRoundsConverge_SecondVerifies
//   - TestReflexion_RejectWithoutRevisionKeepsOriginal
//   - TestReflexion_DefaultMaxRoundsIs1
//
// See doc.go for package-level documentation.

package agents
