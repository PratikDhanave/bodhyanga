package mcp

import (
	"net/url"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
)

// Outcome strings used in CallEvent.Outcome.
const (
	OutcomeSuccess = "success"
	OutcomeError   = "error"
	OutcomeTimeout = "timeout"
)

// CallEvent is the audit-friendly summary of one CallTool invocation. Field
// selection mirrors the rule used everywhere else in Bodhyanga's audit pipeline:
// enough to reconstruct who called what when, never enough to leak the tool
// arguments or response content.
//
// Specifically NOT in this struct: tool arguments, tool result content, raw
// JSON-RPC bodies, response headers. Those frequently carry PHI; surfacing
// them in audit defeats the minimum-necessary principle.
type CallEvent struct {
	Endpoint  string        // MCP server URL, sanitised: scheme://host/path only
	ToolName  string        // tool name only — never arguments
	Duration  time.Duration // wall-clock from request start to response decoded
	Outcome   string        // OutcomeSuccess | OutcomeError | OutcomeTimeout
	ErrorText string        // err.Error() ONLY — never tool args or results
	TenantID  string        // for audit correlation (RLS routing)
	CaseID    string        // optional clinical-case correlation
	TraceID   string        // optional distributed-trace correlation
}

// Recorder turns a CallEvent into an audit.Event and forwards to the
// underlying audit.Recorder. Returns nil cheaply when the wrapped recorder
// is nil so callers don't have to guard every call site.
type Recorder struct {
	// Sink is the audit.Recorder events are written to. nil disables
	// recording (handy for unit tests that don't care about audit).
	Sink audit.Recorder
}

// Record emits one audit event for the call. Fail-open: any error from the
// sink is returned to the caller, but never propagates to abort the MCP
// call — the HTTPClient logs and continues.
func (r *Recorder) Record(ev CallEvent) error {
	if r == nil || r.Sink == nil {
		return nil
	}
	return r.Sink.Record(audit.Event{
		Kind:     audit.KindMessage,
		AgentID:  "mcp_client",
		Type:     "mcp_call_tool",
		To:       ev.Endpoint,
		From:     ev.ToolName,
		TenantID: ev.TenantID,
		CaseID:   ev.CaseID,
		TraceID:  ev.TraceID,
		Decision: ev.Outcome,
		Reason:   formatDuration(ev.Duration),
		ErrorText: ev.ErrorText,
	})
}

// sanitiseEndpoint strips credentials and query parameters from a server URL
// so the audit log carries only the scheme://host/path. This is the value
// CallEvent.Endpoint should hold.
//
// Invalid URLs round-trip unchanged so we never crash a clinical flow on a
// malformed endpoint; the audit event simply records the raw string.
func sanitiseEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	// Strip userinfo and querystring entirely.
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// formatDuration renders a duration like "12ms" / "1.4s" — the audit Reason
// field is a short human-readable string, not a precise number.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := d.Round(time.Millisecond).String()
	// time.Duration.String() can return forms like "1.234s" which is fine.
	return strings.TrimSpace(s)
}
