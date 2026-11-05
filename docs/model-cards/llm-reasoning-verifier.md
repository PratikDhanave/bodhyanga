# Model Card — `LLMReasoningVerifier`

> **Card version:** 1.0.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodhyanga maintainers

## Model details

- **Variant name:** `LLMReasoningVerifier`
- **Source:** [`agents/reasoning_verifier_llm.go`](../../agents/reasoning_verifier_llm.go)
- **Type:** Single-LLM-call verifier with rule-based fallback + HITL routing
- **License:** Apache-2.0 (Bodhyanga) / provider terms

## Intended use

- **Primary intended use:** Judge whether a `DiagnosisProposal`'s rationale is grounded in the supplied `CaseState` and whether the confidence is appropriately calibrated. Routes accepted diagnoses to `diagnostic_supervisor`; routes the verified diagnosis through `human_review` when `HITL: true`.
- **Out-of-scope uses:**
  - Standalone clinical-quality assurance (this variant is one signal, not a substitute)
  - Use without the HITL gate on production diagnostic flows

## Factors

- **Verification quality:** Same underlying LLM as the diagnostician variant typically. Independent models reduce correlated errors but add cost.
- **Confidence floor:** Rule-based fallback rejects below 0.5 + non-empty rationale. LLM variant uses richer judgment.

## Metrics

Hard to measure standalone; the verifier's value shows up as catch rate on bad diagnoses. Recommended: bench fixtures with intentionally-wrong rationales to measure rejection rate.

## Evaluation data

Same as `LLMDiagnostician`. Red-team catalogue exercises rationale-vs-evidence mismatches.

## Training data

Inherits underlying LLM.

## Ethical considerations

- **Risk: verifier rubber-stamps the diagnostician.** If both use the same underlying LLM, correlated errors are likely. Mitigations: use a DIFFERENT model tier or provider for the verifier (e.g. diagnostician on Sonnet, verifier on Opus). HITL gate is the ultimate backstop.
- **Risk: false rejection of borderline diagnoses.** Mitigation: reject path emits a low-confidence "Undifferentiated" diagnosis that lets the supervisor finalize (the `demo_uri.json` edge case where this fails is a tracked follow-up).

## Caveats and recommendations

- **Use a different model than the diagnostician.** Independent models reduce correlated errors.
- **Always wire HITL when this variant runs.** The verifier is the last automated gate before the diagnosis becomes the platform's "decision."
- **Monitor rejection rate.** Tells you whether the verifier and diagnostician are calibrated correctly relative to each other.

## References

- [agents/reasoning_verifier_llm.go](../../agents/reasoning_verifier_llm.go)
- [docs/agents.md § reasoning_verifier](../agents.md#reasoning_verifier)
