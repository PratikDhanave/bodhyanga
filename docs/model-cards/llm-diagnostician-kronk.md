# Model Card — `LLMDiagnostician (kronk / in-process llama.cpp)`

> **Card version:** 0.1.0 · **Last reviewed:** 2026-05-26 · **Author:** Bodhyanga maintainers
>
> Template per Mitchell et al. (2018). Bodhyanga's agent-variant model cards.

## Model details

- **Variant name:** `LLMDiagnostician` (provider `kronk`)
- **Source:** [`pkg/llm/kronk/kronk.go`](../../pkg/llm/kronk/kronk.go) + [`agents/diagnostician_llm.go`](../../agents/diagnostician_llm.go)
- **Type:** Single-LLM-call agent (in-process llama.cpp inference) with deterministic rule-based fallback
- **Default LLM provider:** Ardan Labs [kronk](https://github.com/ardanlabs/kronk) (wraps llama.cpp via yzma + bucky)
- **Default LLM model:** `unsloth/Qwen3-0.6B-Q8_0` (override via `BODHYANGA_KRONK_MODEL`)
- **Provider-configurable:** yes (any kronk-compatible model source identifier; see variant matrix below)
- **Build tag:** `kronk` — the default Bodhyanga binary does NOT link llama.cpp / CGO. Rebuild with `go build -tags kronk ./cmd/care` (or `./cmd/demo`, `./cmd/bench`) to enable.
- **License:** Apache-2.0 (Bodhyanga) / kronk license / model-specific weights license (Qwen, Llama, Phi all carry their own — verify before deploying)
- **Maintainer:** Bodhyanga project ([GitHub issues](https://github.com/PratikDhanave/bodhyanga/issues))

## Intended use

- **Primary intended use:** Generate a `DiagnosisProposal` from a `CaseState` using a *local* LLM call — same JSON shape as the rule-based diagnostician and the other LLM variants, but with no network egress. The model runs in the same Go process via CGO; the bytes never leave the box.
- **Primary intended users:**
  - **Operators in network-restricted environments** (air-gapped hospital LANs, on-prem deployments where the BAA chain does not yet cover a cloud LLM vendor).
  - **Researchers** who want reproducible local inference, fixed weights, and no per-call cost.
  - **Privacy-first reviewers** for whom "no PHI leaves the perimeter" is a compliance precondition.
- **Out-of-scope uses:**
  - Standalone clinical diagnosis without the HITL gate (see AGENTS.md invariant #1).
  - Deployment without the deterministic rule-based fallback wired in (see AGENTS.md invariant #3).
  - Running the kronk binary on a host where the model-weights license has not been reviewed.
  - Treating local inference as a substitute for a model card or AIIA — it is not. PHI never leaves the box, but bias, hallucination, and overconfidence still apply.

## Factors

- **Relevant conditions:** Any condition the chosen small-model checkpoint covers. The default Qwen3-0.6B Q8_0 is a tiny model — accuracy on Bodhyanga's bench is expected to lag the cloud variants. Larger local models (Qwen3-32B, Llama-3-70B) close the gap at the cost of RAM / VRAM and host startup latency.
- **Patient demographics:** Inherits the biases of the underlying open-weight checkpoint. Equity audit pipeline is shared with the other LLM variants ([ROADMAP Phase 7](../../ROADMAP.md)).
- **Instrumentation:** Same `CaseState` inputs as every other diagnostician variant. RAG is not yet wired for kronk (no first-class embedding adapter); the variant runs without retrieved citations until that follow-up lands.
- **Environment:** Always invoked downstream of `cost_guardian`, upstream of `reasoning_verifier`. No prompt caching today (the kronk SDK does not surface a cache hit metric). The host must have enough RAM for the chosen quant; OOM at startup is the most common failure and is surfaced as a New() error, which the factory translates into "fall back to the rule path."

## Metrics

Bench reproducibility: `go build -tags kronk ./cmd/bench && ./bench -manifest=data/cases/manifest.json -providers=llm-kronk`.

Kronk results are **deployment-dependent** — model weights, quantisation level, host hardware, thread budget, and context window all move the numbers. Run the bench in your environment and update this card.

| Metric | Expected range (Qwen3-0.6B-Q8_0 on a laptop) |
|---|---|
| Accuracy (exact-match) | 35-55% on Bodhyanga's 7-fixture bench — substantially below Sonnet/GPT-4o |
| Mean cost USD | $0 marginal (no per-call billing) + amortised hardware cost |
| Mean message steps | 11.0 (shared with the other variants — `test_planner` runs the same workups) |
| Latency p50 | 3-10s per LLM call (tiny model on CPU); 0.5-2s on M-series GPU |
| Latency p99 | 30s+ if the host is loaded; client uses the same 30s default timeout as the other variants |
| Fallback rate | High — expect 20-50% on the tiny default model; alert at >70% |
| Prompt cache hit rate | N/A (kronk does not surface a cache metric) |

Variant accuracy improves rapidly with model size: a Qwen3-7B or Llama-3-8B Q5 quant typically lands close to the cloud variants on Bodhyanga's bench, at the cost of larger RAM / VRAM / startup latency.

## Evaluation data

- **Fixtures:** [`data/cases/manifest.json`](../../data/cases/manifest.json)
- **Red-team catalogue:** [`data/eval/redteam.json`](../../data/eval/redteam.json)
- **Knowledge corpus:** [`data/knowledge/corpus.json`](../../data/knowledge/corpus.json) — currently unused by this variant (no kronk embedding adapter yet)
- **Sample size:** 7 fixtures × 6 red-team categories. Synthetic only — opaque case IDs, no real PHI.

## Training data

Bodhyanga does not train custom models. This variant composes with a third-party open-weight checkpoint that the operator loads via the `BODHYANGA_KRONK_MODEL` env var. Before deploying:

- Review the chosen checkpoint's model card on Hugging Face (license, training data, known limitations).
- Verify the quantisation level was produced by a trusted source (e.g. unsloth, TheBloke, official model org).
- Run the red-team catalogue against the loaded weights — small quants degrade on adversarial prompts faster than full-precision originals.

Suggested checkpoint families (verify license + suitability before use):

| Family | Suggested sizes | License |
|---|---|---|
| Qwen / Qwen2 / Qwen3 | 0.5B – 32B | Apache-2.0 (Qwen2+); Qwen1 has a custom license |
| Meta Llama 3 / 3.1 / 3.2 | 1B – 70B | Llama 3 Community License (commercial-OK with attribution) |
| Mistral / Mixtral | 7B / 8x7B | Apache-2.0 (open-weights releases) |
| Microsoft Phi-3 / Phi-3.5 | 3.8B – 14B | MIT |

## Quantitative analyses

Run side-by-side with the other variants once you have linked kronk:

```bash
go build -tags kronk ./cmd/bench
./bench -providers=rule,llm-anthropic,llm-openai,llm-ollama,llm-kronk \
        -manifest=data/cases/manifest.json
```

Compare per-provider accuracy, fallback rate, and Pareto score. The interesting question for kronk: at what model size does the local variant become non-inferior to Ollama on a clinical bench while staying inside the same memory budget?

## Ethical considerations

- **Risk: hallucinated diagnosis from a small local model.** Mitigations: deterministic rule-based fallback (always wired); `MinConfidence` threshold; `reasoning_verifier` review; HITL gate before any clinically consequential message is acted on.
- **Risk: training data bias inherited from the open-weight checkpoint.** Mitigations: red-team catalogue; equity audit pipeline (planned, shared with the cloud variants).
- **Risk: prompt injection from clinical narrative.** Mitigations: `pkg/safety.IsRefusal` post-check; structured-output JSON schema constrains response shape; reasoning_verifier reviews the rationale.
- **Risk: model-weights license violation.** Mitigation: model card check at deploy time; weights pinned in deployment manifest.
- **Risk: host OOM / kernel kill during inference.** Mitigation: kronk reports OOM as a startup error; factory falls back to the rule path; operator alerts on fallback rate.

### What this variant *does not* introduce

- **No new PHI surface.** kronk runs in the same process; the case narrative never touches a network socket and never leaves the host. This is the headline win.
- **No new audit-log surface.** The trace recorded via `observability.LLMTrace` carries provider / model / token counts / stop reason — no prompt, no response, no narrative.
- **No new dependency on a vendor BAA.** The kronk variant does not require a cloud BAA: there is no cloud counterparty. (Local inference still inherits HIPAA Technical Safeguards — disk encryption, access controls, audit logs — those obligations remain.)

## Caveats and recommendations

- **The fallback is the contract.** This variant must never be deployed without its deterministic fallback. Small local models trip the fallback more often than cloud models — that is *expected* and the design is robust to it.
- **`MinConfidence` threshold (default 0.6) routes low-confidence local LLM responses to the fallback.** Tune per deployment; small models tend to be over-confident on out-of-distribution cases.
- **Pin the model weights.** A kronk-backed Bodhyanga deployment is reproducible only if the operator records the exact model source identifier, quantisation level, and checkpoint sha256. Treat this like a container image digest.
- **Monitor host memory.** llama.cpp can be aggressive about page-cache use. Set kernel cgroup limits; alert on host swap.
- **No PHI leaves the box** — but disk encryption, access controls, and audit logs still apply. HIPAA Technical Safeguards are not waived by local inference.
- **No first-class RAG today.** The diagnostician runs without retrieved citations on this provider until a kronk-compatible embedding adapter lands.
- **Plan for cold-start latency.** Loading a Q8 quant on cold boot can take seconds to tens of seconds. The factory surfaces the startup error to the operator; production deployments should pre-warm.

## Deployment property: local-only

This variant is the *only* Bodhyanga LLM diagnostician variant for which the following property holds:

> The PHI content of any `CaseState` passed to this client never traverses a network socket and never leaves the OS process that hosts the orchestrator.

Compliance implications:

- **HIPAA Privacy Rule § 164.502 (minimum necessary).** Satisfied trivially — no third-party disclosure occurs.
- **HIPAA § 164.314 (BAA chain).** No vendor BAA required for the LLM call itself. Hosting infrastructure (cloud VM, on-prem hypervisor) still requires its own BAA / risk analysis.
- **GDPR Article 28 (processors).** Cloud LLM vendor is not in scope as a processor.
- **42 CFR Part 2 (SUD records).** Operators handling substance-use records under Part 2 may prefer this variant because no third-party API is engaged.

## Fallback contract (mandatory)

Every kronk-backed agent is wired as:

```
                  ┌─ kronk.Client.Complete succeeds + confidence ≥ MinConfidence ─► LLM proposal
LLMDiagnostician ─┤
                  └─ kronk fails / refuses / low confidence ─────────────────────► DiagnosticianAgent (rule path)
```

The fallback is the contract. The case always finalizes — even when:

- The kronk build tag is not set (stub returns an error; factory translates to "rule path").
- The model weights are not on disk (load fails; factory translates to "rule path").
- The model OOMs the host (Complete returns an error; LLMDiagnostician falls back).
- The model returns refusal text or invalid JSON (safety check trips; LLMDiagnostician falls back).
- The model is overconfident on an out-of-distribution case (`reasoning_verifier` rejects; HITL queue takes over).

## References

- [pkg/llm/kronk/kronk.go](../../pkg/llm/kronk/kronk.go)
- [pkg/llm/kronk/kronk_doc.go](../../pkg/llm/kronk/kronk_doc.go)
- [agents/diagnostician_factory.go](../../agents/diagnostician_factory.go) — provider matrix
- [agents/diagnostician_llm.go](../../agents/diagnostician_llm.go) — the wrapping LLMDiagnostician
- [docs/model-cards/llm-diagnostician.md](./llm-diagnostician.md) — cloud-variant baseline
- [docs/compliance.md](../compliance.md) — BAA chain analysis
- [docs/ai-features.md § kronk](../ai-features.md) — adapter overview
- Mitchell, Wu, Zaldivar, et al. (2018) [*Model Cards for Model Reporting*](https://arxiv.org/abs/1810.03677)
- Ardan Labs kronk: <https://github.com/ardanlabs/kronk>
- Reference example: <https://github.com/ardanlabs/kronk/blob/main/examples/chat/main.go>
