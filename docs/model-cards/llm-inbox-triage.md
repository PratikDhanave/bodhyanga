# Model Card — `LLMInboxTriage`

> **Card version:** 1.0.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodh maintainers

## Model details

- **Variant name:** `LLMInboxTriage`
- **Source:** [`agents/inbox_triage_llm.go`](../../agents/inbox_triage_llm.go)
- **Type:** Single-LLM-call classifier with rule-based fallback
- **License:** Apache-2.0 (Bodh) / provider terms

## Intended use

- **Primary intended use:** Classify inbound patient messages into `refill | prior_auth | urgent_clinical | general`. Extract the medication or procedure code when applicable. Conservative bias: prefer `urgent_clinical` when uncertain.
- **Out-of-scope uses:**
  - Sentiment analysis (not a clinical category)
  - Patient identity verification (separate concern)
  - Reply drafting (this variant only classifies)

## Factors

- **Classification quality:** Inherits underlying LLM's reading comprehension.
- **Conservative default:** Ambiguous cases route to nurse-task queue (the safer outcome).

## Metrics

Recommended: build a benchset of synthetic inbox messages with known intent labels; measure per-category precision/recall.

| Metric | Target |
|---|---|
| Precision on `urgent_clinical` | >95% (under-triage is the dangerous failure) |
| Recall on `urgent_clinical` | >90% (over-triage adds queue volume) |
| Precision on `refill` | >90% |
| Precision on `prior_auth` | >90% |
| Fallback rate | <5% |

## Evaluation data

Synthetic inbox fixtures only. Real-patient text never in CI.

## Training data

Inherits underlying LLM.

## Ethical considerations

- **Risk: false-negative on urgent clinical (under-triage).** Worst-case outcome: a true emergency lands in the general queue. Mitigations: conservative defaults; the rule-based fallback also prefers urgent_clinical; reviewer monitoring for misrouted urgent cases.
- **Risk: language/dialect bias.** Inbound messages in non-English or with dialectical variation may be mis-classified. Mitigations: expand the bench; provider selection matters (some LLMs handle multilingual better).
- **Risk: prompt injection from inbound messages.** Mitigations: structured-output JSON schema constrains the response; refusal-phrase check via `pkg/safety.IsRefusal`.

## Caveats and recommendations

- **Conservative bias is non-optional.** Don't tune toward fewer urgent_clinical false positives at the cost of false negatives.
- **Monitor reroute rate.** When clinicians frequently reroute messages out of the queue the triage chose, the model needs retuning.
- **Don't auto-act on classifications.** Even `refill_request` should hit the refill_manager, which has its own logic + HITL gate; the inbox_triage doesn't escalate beyond its lane.

## References

- [agents/inbox_triage_llm.go](../../agents/inbox_triage_llm.go)
- [docs/agents.md § inbox_triage](../agents.md#inbox_triage)
