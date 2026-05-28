// Package mid hosts the standard Bodh HTTP middleware chain.
//
// Adapted from Bill Kennedy / Ardan Labs' Ultimate Service template
// (https://github.com/ardanlabs/service), licensed Apache 2.0. See NOTICE
// and LICENSE-APACHE at the repository root.
//
// Standard chain (outer → inner):
//
//	mid.Panics()        // recover + count
//	mid.Logger(log)     // PHI-redacted request/response log lines
//	mid.Metrics()       // process-local request + error counters
//	mid.Otel()          // request_id propagation (+ OTel span behind build tag)
//	mid.Errors(log)     // translate handler error → wire envelope
//	handler
//
// Why this exact order:
//   - Panics is outermost so it can catch a panic raised by any other
//     middleware OR the handler.
//   - Logger sits next so every request — including ones whose handler
//     panics — appears in the log. The duration recorded is wall-clock
//     including everything below.
//   - Metrics counts requests + errors. Sits above Errors so the counter
//     sees the raw return value, not the wrapped envelope.
//   - Otel stamps the request_id onto context BEFORE Errors so the
//     envelope can pick it up.
//   - Errors is innermost so it sees the raw handler return and is the
//     last layer before the wire envelope is constructed.
//
// PHI invariants (AGENTS.md):
//   - Logger and Errors both run through pkg/phi.LoggerWrapper. The raw
//     observability.Logger passed in is wrapped exactly once per
//     middleware (NewRedactor() is idempotent so re-wrapping is safe).
//   - The Errors middleware NEVER echoes err.Error() into the wire
//     envelope when the error is untyped. Untyped errors are assumed to
//     carry PHI and replaced with a generic "internal error" message.
//   - InternalOnlyLog short-circuits the wire message to a generic
//     "internal error" string. The full panic / stacktrace lives in the
//     log only.
package mid
