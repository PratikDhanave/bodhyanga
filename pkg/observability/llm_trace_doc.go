// llm_trace.go — structured per-call telemetry for LLM and embedding requests.
//
// Overview:
//   Defines LLMTrace, a flat record describing one model invocation, and the
//   StdLogger.RecordLLMCall method that marshals it to a single JSON line on
//   the logger. When BODH_OTEL_ENABLED=1 is set and SetLLMOTelHook has wired
//   in an exporter, the same trace is forwarded to that hook for OTLP export.
//   The default build links no OTel SDK; tracing is opt-in.
//
// Key declarations:
//   - LLMTrace: flat struct of timing, identity, token counts, stop reason, kind.
//   - (*StdLogger).RecordLLMCall: emits "[llm-trace] {...json...}" and dispatches to the OTel hook when enabled.
//   - SetLLMOTelHook: installs (or clears) the OTel forwarding callback.
//
// See doc.go for package-level documentation.

package observability
