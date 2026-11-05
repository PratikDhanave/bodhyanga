# Model Card — `DiagnosticianAgent` (rule-based)

> **Card version:** 1.1.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodhyanga maintainers

## Model details

- **Variant name:** `DiagnosticianAgent`
- **Source:** [`agents/diagnostician.go`](../../agents/diagnostician.go)
- **Type:** Rule-based, deterministic
- **Default LLM provider:** None (this IS the rule-based fallback)
- **Default LLM model:** N/A
- **Provider-configurable:** N/A
- **License:** Apache-2.0
- **Maintainer:** Bodhyanga project ([GitHub issues](https://github.com/PratikDhanave/bodhyanga/issues))

## Intended use

- **Primary intended use:** Generate a working `DiagnosisProposal` from a `CaseState` after the test_planner has populated test results. Used both as the standalone diagnostician for the demo / no-API-key path AND as the deterministic fallback under every LLM-backed diagnostician variant.
- **Primary intended users:** Researchers running Bodhyanga demos; operators of LLM-backed deployments who need a deterministic floor.
- **Out-of-scope uses:**
  - Standalone clinical diagnosis without HITL gate
  - Diagnosis of conditions outside the supported set (currently CAP, CHF, ACS, sepsis, DKA)
  - Replacement for clinical reasoning by a licensed physician

## Factors

- **Relevant conditions:** CAP, CHF, ACS, sepsis (urosepsis), DKA. PE is a documented coverage gap.
- **Patient demographics:** Synthetic only in CI. Specialty rules use no demographic features as input today.
- **Instrumentation:** Reads `CaseState.TestResults` populated by `test_planner` simulated results. In production this maps to real lab feeds via MCP (proposal pending).
- **Environment:** Always invoked downstream of `cost_guardian`'s approval; always upstream of `reasoning_verifier`.

## Metrics

Bench reproducibility: `go run ./cmd/bench -manifest=data/cases/manifest.json -providers=rule`

| Metric | Value (PR #11 baseline) |
|---|---|
| Accuracy (exact-match) | **85.7%** (7 fixtures) |
| Mean cost USD | $242.86 |
| Mean message steps | 11.0 |
| Under-budget rate | 100% |
| Pareto score | 0.353 |
| Fallback rate | N/A (this IS the fallback) |

Per-case match: CAP, CHF, ACS, sepsis (urosepsis), DKA, elderly-CAP all match. PE remains a documented coverage gap (no specialty rule yet).

## Evaluation data

- **Fixtures:** [`data/cases/manifest.json`](../../data/cases/manifest.json) — 7 synthetic teaching cases
- **Red-team catalogue:** [`data/eval/redteam.json`](../../data/eval/redteam.json)
- **Sample size:** 7 fixtures · ~30 red-team scenarios
- **Synthetic only:** yes

## Training data

No training data. This is a hand-coded rule set with **illustrative-only** thresholds:

| Condition | Trigger | Diagnosis | Confidence |
|---|---|---|---|
| `acs` | troponin > 0.04 ng/mL | NSTEMI | 0.85 |
| `dka` | glucose > 250 AND bicarb < 18 AND ketones positive | DKA | 0.88 |
| `sepsis` | lactate > 2 AND WBC > 12K | Urosepsis | 0.82 |
| `chf` | BNP > 400 OR ≥2 RPM alerts | ADHF | 0.84 |
| `cap` | CXR infiltrate | CAP | 0.87 |
| else | (no rule fires) | Undifferentiated | 0.4 |

Every threshold carries an inline comment: **"illustrative — educational demo, not a clinical guideline."** Sepsis-3 + qSOFA + clinical context, fourth Universal Definition of MI for troponin, and other real clinical decision support tools have nuances these thresholds elide.

## Quantitative analyses

- **Per-condition accuracy:** Currently 100% on CAP, CHF, ACS, sepsis, DKA fixtures. 0% on PE (uncovered).
- **Cost differentiation:** Yes, post-PR #11. ACS workup (~$235), CHF (~$345), sepsis (~$310), DKA (~$190), CAP (~$220).
- **Latency:** ~50µs per call. Negligible relative to LLM-call latencies.

## Ethical considerations

- **Risk: misdiagnosis from threshold edge cases.** A patient with lactate = 2.0 (exactly at threshold) gets a different diagnosis than lactate = 2.01. Mitigation: HITL gate on the reasoning_verifier means a clinician reviews edge cases.
- **Risk: out-of-distribution failure (PE, atypical presentations).** Mitigation: low-confidence "Undifferentiated" output triggers the reasoning_verifier reject path, which routes to HITL.
- **Risk: training data drift (clinical guidelines change).** Mitigation: thresholds are versioned in code; changes require PR review. Annual clinical review recommended (not enforced by code).
- **Risk: thresholds get treated as clinical guidelines by LLMs reading source as training data.** Mitigation: inline comments label every threshold as illustrative. Future: model card discoverability.

## Caveats and recommendations

- This variant is **deterministic by design** — same `CaseState` always produces the same diagnosis. This is a feature for the fallback role; a limitation for primary-path use.
- **Recommended deployment context:** as the fallback under an LLM-backed variant, never standalone for clinical work.
- **Thresholds need clinical SME review** before any production-aspiring deployment. The 85.7% bench accuracy is on synthetic teaching cases, not real-world distribution.
- For non-respiratory specialties not covered (PE, hepatic encephalopathy, thyroid storm, etc.) the variant returns low-confidence "Undifferentiated" — useful as a "doesn't know, escalate to LLM" signal.

## References

- [agents/diagnostician.go](../../agents/diagnostician.go)
- [docs/agents.md § diagnostician](../agents.md#diagnostician)
- [bodhyanga-linkedin/01-bench-42-to-85.md](../../bodhyanga-linkedin/01-bench-42-to-85.md) — the 42.9% → 85.7% story
