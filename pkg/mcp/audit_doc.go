// audit.go — PHI-free audit hook for MCP tool calls.
//
// Overview:
//   Every HTTPClient.CallTool produces exactly one CallEvent, which the
//   Recorder translates into an audit.Event and forwards to the wrapped
//   audit.Recorder. The translation discards tool arguments and result
//   content — the audit row records that the call happened, who called what,
//   how long it took, and what the outcome was, but never the arguments or
//   the response. Those almost always carry PHI; including them in the audit
//   log would defeat the minimum-necessary principle (HIPAA §164.502(b))
//   and contradict the existing audit field-selection rule documented in
//   docs/governance.md.
//
// Key declarations:
//   - CallEvent: the structured per-call summary (endpoint, tool, duration,
//     outcome, tenant/case/trace correlation).
//   - Recorder: wraps an audit.Recorder; Record emits one audit.Event per
//     CallEvent.
//   - OutcomeSuccess / OutcomeError / OutcomeTimeout: outcome string constants.
//   - sanitiseEndpoint: strips userinfo / query / fragment so the audit
//     surface carries the host+path identity only.
//
// Fail-open: any error from the underlying audit sink is returned to the
// caller, but the MCP HTTPClient logs and continues — audit failures are
// operational issues, never reasons to abort a clinical tool call.

package mcp
