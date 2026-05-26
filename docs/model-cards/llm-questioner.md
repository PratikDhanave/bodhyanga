# Model Card — `LLMQuestioner`

> **Card version:** 1.0.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodh maintainers

## Model details

- **Variant name:** `LLMQuestioner`
- **Source:** [`agents/questioner_llm.go`](../../agents/questioner_llm.go)
- **Type:** Single-LLM-call agent with rule-based fallback
- **Default LLM provider:** parent factory (same env vars as `LLMDiagnostician`)
- **License:** Apache-2.0 (Bodh) / provider terms

## Intended use

- **Primary intended use:** Generate 3-5 context-relevant history questions from a `CaseState`, with simulated patient answers and 3 starting hypotheses. Used in the diagnostic flow after `intake` to expand the case state with structured history.
- **Out-of-scope uses:**
  - Real patient interviewing (the simulated answers are synthetic)
  - Open-ended chat with patients (this variant is constrained to structured outputs)

## Factors

- **Question quality:** Depends heavily on the underlying LLM's clinical reasoning. Quality of the rule-based fallback's four fixed questions is the floor.
- **Hypothesis generation:** Same — LLM-generated when available, condition-driven canned otherwise.

## Metrics

Hard to measure standalone; the questioner's value shows up in downstream diagnostician accuracy. Recommended: ablation bench where the questioner is the only variable.

## Evaluation data

Same as `LLMDiagnostician`.

## Training data

Inherits underlying LLM.

## Ethical considerations

- **Risk: leading questions.** Mitigations: prompt explicitly asks for neutral history questions; reasoning_verifier reviews the case state downstream.
- **Risk: simulated answers in a real-patient flow.** Mitigations: production deployments must replace `simulatedAnswer()` with real patient input (planned via SMART on FHIR or HL7 v2).

## Caveats and recommendations

- The simulated-answer pattern is **demo-only**. Production replaces it with actual patient input.
- For real-patient flows, the questioner becomes more like a conversation orchestrator — out of scope for this variant today.

## References

- [agents/questioner_llm.go](../../agents/questioner_llm.go)
