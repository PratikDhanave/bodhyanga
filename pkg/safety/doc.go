// Package safety provides lightweight guardrails for LLM-backed agents:
// refusal-pattern detection, hallucination cross-checks against retrieved
// evidence, and a simple verdict envelope for telemetry.
//
// What this is, what this is not
//
//   - It is a fast, deterministic, in-process check applied AFTER the LLM
//     returns and BEFORE the agent emits its message.
//   - It is NOT a replacement for clinical review (the HITL gate) or for
//     model-level safety training.
//
// Three checks are shipped:
//
//   - IsRefusal — detects common refusal phrases the model emits when
//     uncertain or constrained (e.g. "I cannot provide a diagnosis…").
//     Used to short-circuit to the rule-based fallback.
//
//   - CheckHallucination — given a claim and a list of retrieved citations,
//     decides whether the claim is grounded. The shipped implementation is
//     a simple keyword/term overlap heuristic; production deployments swap
//     in a stricter NLI or LLM-as-judge.
//
//   - ContainsPHIPatterns — a defence-in-depth check over LLM OUTPUT for
//     residual PHI shapes (SSN, MRN-like numbers, US phone). Inputs are
//     redacted via pkg/phi before they ever reach an LLM.
package safety
