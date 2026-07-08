// Package tools provides deterministic, in-process clinical utilities that
// an LLM diagnostician may invoke mid-reasoning.
//
// # Why a separate package
//
// An LLM that proposes a working diagnosis benefits from access to a small set
// of deterministic helpers: a drug-interaction check, a clinical risk
// calculator (Wells, CURB-65, CHA2DS2-VASc, HAS-BLED), and a FHIR-style
// patient lookup. Each helper is a pure Go function — no external calls — so
// they are safe to invoke from inside an agent's reasoning loop without
// taking on new network dependencies or surprising latency.
//
// Each helper is exposed through the Tool interface so the LLM can call any
// of them via the same Request.Tools / Response.ToolCalls round-trip the
// pkg/llm package already supports (see pkg/llm/client.go). A Registry
// bundles a set of Tools and converts them to llm.Tool descriptors for
// inclusion in a Request.
//
// # Scope and limits
//
// The data shipped in this package is illustrative — small hard-coded tables
// of drug pairs and patient records that are obviously synthetic, tagged with
// metadata.source = "demo-synth" in their JSON outputs. They are not a
// substitute for a real clinical decision-support system, a curated drug
// interaction database (e.g. Lexicomp, Micromedex), or a production FHIR
// store. The risk calculators implement the standard scoring rules but the
// banding text is condensed for demo use. Consumers wanting real-world
// safety must replace these tools with vetted implementations.
//
// What this package does NOT do
//
//   - Persist tool results — every Invoke is stateless.
//   - Talk to a real EHR, drug DB, or FHIR server. Stub data only.
//   - Provide treatment plans. Outputs describe risk; downstream agents and
//     the supervisor decide what to do with them.
package tools
