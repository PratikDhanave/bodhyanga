// main.go — CLI that exports the HITL review queue as fine-tuning data.
//
// Overview:
//   Every clinician decision on the HITL queue (pkg/hitl + medical.ReviewRequest)
//   is labelled training data. Approved reviews are positive examples,
//   approved_with_modifications are preference signals, and rejected reviews
//   are hard negatives. This binary reads a JSON dump of []medical.ReviewRequest
//   (today: a static file; long-term: a DB export) and writes two parallel
//   JSONL files — sft.jsonl (supervised) and dpo.jsonl (preference) — into
//   an output directory.
//
// Flow:
//   1. Resolve input path (-in or $HOME/.bodh/hitl-queue.json).
//   2. Decode []medical.ReviewRequest (bare array or {"reviews":[...]} wrapper).
//   3. Filter to decided reviews (drop pending / expired).
//   4. For SFT: emit one chat-shaped line per approved / modified / (optionally)
//      rejected review.
//   5. For DPO: emit one {prompt, chosen, rejected} per approved_with_modifications
//      (modification > original) and per (approved, rejected) pair sharing a case_id.
//   6. Print a summary; exit 0 even when zero decisions were found.
//
// Key declarations:
//   - DiagnosticianSystemPrompt: mirrors agents/diagnostician_llm.go so fine-tuned
//     models behave like the LLM diagnostician under the same prompt.
//   - run: testable entry point returning (exitCode, error).
//   - buildSFT / buildDPO: the two transforms; both are pure functions over
//     []medical.ReviewRequest so tests can drive them directly.
//   - caseStateContent: prefer r.Meta["case_state"] (what the upstream agent
//     gave the LLM), fall back to r.Detail, last resort r.Payload.
//
// See README's "Fine-tuning from HITL decisions" section in docs/ai-features.md.

package main
