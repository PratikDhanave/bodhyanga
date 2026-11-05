# Bodhyanga Model Cards

Per Mitchell et al. (2018), [*Model Cards for Model Reporting*](https://arxiv.org/abs/1810.03677), every machine-learning model that ships to production should be accompanied by a Model Card documenting:

- Model details (provider, version, type)
- Intended use (and out-of-scope uses)
- Factors that affect performance
- Metrics
- Evaluation data
- Training data (or its analogue when using vendor LLMs)
- Quantitative analyses
- Ethical considerations
- Caveats and recommendations

Bodhyanga doesn't train custom models — every "model" in Bodhyanga is either a vendor LLM (Anthropic, OpenAI, Ollama) accessed through `pkg/llm.Client`, or a deterministic rule-based agent. Both have model-card-shaped documentation needs.

This directory holds one card per **agent variant**, not per underlying LLM. The agent variant is the integration shape (rule-based, single-LLM call, debate, reflexion, cascade, vision, tool-using) — the LLM provider is a configurable parameter of each variant.

## Cards in this directory

| File | Agent variant | Source |
|---|---|---|
| [rule-diagnostician.md](rule-diagnostician.md) | `DiagnosticianAgent` (rule-based) | [agents/diagnostician.go](../../agents/diagnostician.go) |
| [llm-diagnostician.md](llm-diagnostician.md) | `LLMDiagnostician` (single-call) | [agents/diagnostician_llm.go](../../agents/diagnostician_llm.go) |
| [llm-diagnostician-vision.md](llm-diagnostician-vision.md) | `LLMDiagnosticianVision` (image-attached) | [agents/diagnostician_vision.go](../../agents/diagnostician_vision.go) |
| [debate-diagnostician.md](debate-diagnostician.md) | `DebateDiagnostician` (multi-expert) | [agents/diagnostician_debate.go](../../agents/diagnostician_debate.go) |
| [reflexion-diagnostician.md](reflexion-diagnostician.md) | `ReflexionDiagnostician` (self-critique loop) | [agents/diagnostician_reflexion.go](../../agents/diagnostician_reflexion.go) |
| [cascade-diagnostician.md](cascade-diagnostician.md) | `CascadeDiagnostician` (small→large escalation) | [agents/diagnostician_cascade.go](../../agents/diagnostician_cascade.go) |
| [llm-questioner.md](llm-questioner.md) | `LLMQuestioner` | [agents/questioner_llm.go](../../agents/questioner_llm.go) |
| [llm-reasoning-verifier.md](llm-reasoning-verifier.md) | `LLMReasoningVerifier` | [agents/reasoning_verifier_llm.go](../../agents/reasoning_verifier_llm.go) |
| [llm-inbox-triage.md](llm-inbox-triage.md) | `LLMInboxTriage` | [agents/inbox_triage_llm.go](../../agents/inbox_triage_llm.go) |

## Maintenance

When a new LLM-backed agent variant ships:

1. Copy the [TEMPLATE.md](TEMPLATE.md) file in this directory.
2. Fill every section honestly. **Unknown** is an acceptable answer; **omitted** is not.
3. Link the card from the table above.
4. Reference the card from [docs/agents.md](../agents.md) in the agent's row.

When an existing variant's behavior changes meaningfully (provider swap, prompt rev, threshold change, new fallback trigger):

1. Update the relevant card.
2. Bump the version in the card's header.
3. Note the change in [ROADMAP.md](../../ROADMAP.md) decision log.

## What model cards are NOT

- A substitute for the bench (`cmd/bench`) — model cards describe; the bench measures.
- A substitute for the audit log — model cards are static; the audit log records the actual behavior at runtime.
- A substitute for the AIIA — Model cards are about the model; AI Impact Assessments are about the *use* of the model in a specific deployment context.

## Disclaimer

These model cards describe agent variants for research purposes. Bodhyanga is not a medical device and not approved for clinical use. Production deployments must conduct their own validation, including clinical-team review of every threshold, prompt, and fallback decision documented here.
