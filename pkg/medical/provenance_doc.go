// provenance.go — Provenance watermark for DiagnosisProposal and the small
// helpers that build it.
//
// Overview:
//   Provenance is the per-proposal signature the LLM diagnostician stamps
//   onto its output so downstream consumers can attribute the diagnosis to
//   a specific (provider, model, prompt, citations, time, agent, variant)
//   tuple without re-running the call. The block is `omitempty` on the
//   parent DiagnosisProposal so rule-based output stays compact.
//
//   HashPrompt(system, user) is the deterministic SHA-256 hex digest used as
//   the PromptHash field. MakeProvenance composes a Provenance value from
//   the LLM-call ingredients (provider, model, system+user prompts, RAG
//   citations, agent ID, variant) and timestamps it with the current UTC
//   wall clock.
//
// Key declarations:
//   - Provenance: the watermark struct
//   - HashPrompt(system, user): deterministic prompt digest
//   - MakeProvenance(...): build a Provenance from LLM-call ingredients
//
// See doc.go for package-level documentation.

package medical
