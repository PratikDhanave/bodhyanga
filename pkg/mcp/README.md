# `pkg/mcp` — Bodh's Model Context Protocol client

> JSON-RPC 2.0 over HTTP+SSE client for the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/). Implements **Proposal 1** of [docs/agent-interop.md](../../docs/agent-interop.md).

Bodh's existing `pkg/tools` package holds internal Go tool helpers that the LLM-backed `diagnostician_tools` agent calls directly. That works for the demo, but every external lab / imaging / FHIR / pharmacy integration needs a fresh Go adapter. MCP is the path of least resistance — any vendor-published MCP server becomes available with zero adapter code.

This package ships the client side of that wire format. The companion server façade (Proposal 8) and the refactor of `diagnostician_tools` to use this client are deferred to follow-up PRs.

## Why MCP

See **[Proposal 1 in docs/agent-interop.md](../../docs/agent-interop.md#proposal-1-mcp-client-in-bodh-high-impact-2-weeks)** for the full impact analysis. Headline:

- Hospital integrations are the #1 friction in clinical AI deployment.
- MCP is rapidly becoming the de-facto agent-to-tool standard (Anthropic, Google, Microsoft, growing community catalogue).
- Bodh's orchestrator + governance composer is already conceptually an MCP gateway for internal traffic; adding an MCP client lets the same policies apply to external tool egress.

## Minimal usage

```go
import (
    "context"
    "github.com/PratikDhanave/bodh/pkg/mcp"
)

c := mcp.NewHTTPClient("https://lab-mcp.hospital.example/v1")
if _, err := c.Initialize(ctx); err != nil { return err }
res, err := c.CallTool(ctx, "lab_query", map[string]any{"loinc": "26464-8"})
```

`res.Content` is the list of `ContentItem` chunks the server returned. `res.IsError` is the tool-reported error flag; transport / JSON-RPC failures come back as Go errors instead.

## Auth providers

```go
// Bearer token (typical service-account JWT).
c := mcp.NewHTTPClient(endpoint, mcp.WithAuth(mcp.BearerAuth{Token: jwt}))

// No auth — only for trusted-network development.
c := mcp.NewHTTPClient(endpoint, mcp.WithAuth(mcp.NoAuth{}))

// SPIFFE workload identity — stub for now, returns mcp.ErrNotImplemented.
// Real implementation lands with Proposal 6.
c := mcp.NewHTTPClient(endpoint, mcp.WithAuth(mcp.SPIFFEAuth{}))
```

Implement your own provider by satisfying the `AuthProvider` interface:

```go
type AuthProvider interface {
    Apply(req *http.Request) error
}
```

## Audit integration (PHI-free)

Every `CallTool` emits one `CallEvent` to the configured `audit.Recorder`:

```go
import "github.com/PratikDhanave/bodh/pkg/audit"

rec := audit.NewInMemory()
c := mcp.NewHTTPClient(
    endpoint,
    mcp.WithRecorder(rec),
    mcp.WithTenantID("hospital-a"),
    mcp.WithCaseID("case-001"),
    mcp.WithTraceID(traceID),
)
```

What the audit event carries:

| Field | Source |
|---|---|
| `kind` | `KindMessage` |
| `agent_id` | `"mcp_client"` (fixed) |
| `type` | `"mcp_call_tool"` |
| `from` | tool name only — **never arguments** |
| `to` | sanitised endpoint (scheme://host/path; no userinfo, no query) |
| `decision` | `success` / `error` / `timeout` |
| `reason` | human-readable duration (`"42ms"`, `"1.4s"`) |
| `tenant_id`, `case_id`, `trace_id` | correlation tags from `With*` options |
| `error` | `err.Error()` only — **never** tool arguments or results |

This matches the field-selection rule documented in [docs/governance.md](../../docs/governance.md): enough to reconstruct who called what when, never enough to leak PHI if the audit sink is compromised.

## Timeouts and context

`DefaultCallTimeout` is 30 seconds per call. Override with `WithTimeout(d)`. Caller-supplied context deadlines always win — if `ctx` has a deadline, the client-level timeout is ignored.

```go
c := mcp.NewHTTPClient(endpoint, mcp.WithTimeout(5*time.Second))

// Or per-call:
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
res, err := c.CallTool(ctx, "tool_x", args)
```

Long-running tool calls should be modelled as resources (`ListResources` / `ReadResource`) or as A2A tasks ([Proposal 2](../../docs/agent-interop.md#proposal-2-a2a-server-endpoints-for-bodh-agents-high-impact-2-3-weeks)) — not as multi-minute MCP calls.

## Error handling

```go
res, err := c.CallTool(ctx, "lab_query", args)
if err != nil {
    var rpcErr *mcp.Error
    switch {
    case errors.As(err, &rpcErr):
        // JSON-RPC protocol error from the server (-32602 invalid params, etc.)
    case errors.Is(err, context.DeadlineExceeded):
        // Timeout — caller-supplied deadline or the WithTimeout default fired.
    default:
        // Transport / decode / HTTP-status failure.
    }
}
if res.IsError {
    // The tool ran but reported a non-fatal error inside its result.
}
```

## Transport details

The client speaks JSON-RPC 2.0 over HTTP POST. Servers may respond two ways:

1. **`application/json`** — the simple request/response case. The whole body is one `Response` envelope.
2. **`text/event-stream`** — SSE-framed sequence of payloads for streaming tool calls. The transport assembles partial `ToolResult` chunks into a final merged result; this is transparent to the caller.

Servers signal the mode via `Content-Type` on the response. Both modes are supported by the same code path.

## What's NOT in this PR

These items are deferred to follow-up PRs:

- **`MCPPolicyEnforcer`** wrapper that runs `governance.Policy` before each `CallTool`. The orchestrator's policy composer becomes Bodh's MCP gateway when this lands.
- **Refactor of `agents/diagnostician_tools.go`** to use this client in place of the in-process `pkg/tools.Registry`.
- **stdio transport** for local-process MCP servers. HTTP+SSE is the right primary transport; stdio is a follow-up.
- **MCP server façade** exposing Bodh's `pkg/tools` (Proposal 8).
- **Real SPIFFE/SPIRE integration** behind `SPIFFEAuth` (Proposal 6).
- **Retry / backoff.** One attempt per call. Higher-level retry policy is the calling agent's responsibility — see `docs/operations.md § LLM reliability` for the pattern Bodh uses elsewhere.

## File map

| File | What it holds |
|---|---|
| `doc.go` | Package overview + scope statement |
| `types.go` | JSON-RPC envelopes + MCP wire types (`Tool`, `Resource`, `ToolResult`, `ContentItem`, `Initialize*`) |
| `client.go` | `Client` interface + `HTTPClient` + `Option` constructors |
| `transport.go` | `transport` — JSON-RPC over HTTP POST with SSE response support |
| `auth.go` | `AuthProvider` + `NoAuth`, `BearerAuth`, `SPIFFEAuth` stub |
| `audit.go` | `CallEvent` + `Recorder` — PHI-free audit hook |
| `client_test.go` | `httptest.Server`-based coverage of every method, auth path, error case |
| `*_doc.go` | Per-source-file documentation companions per Bodh convention |

## Running the tests

```bash
go test ./pkg/mcp/...
```

16 tests cover initialize, list/call tools, list/read resources, SSE streaming, timeout, audit emission (success + error, with PHI-leak assertions), auth headers, and three flavours of misbehaving server.
