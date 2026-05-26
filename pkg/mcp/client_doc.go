// client.go — the Client interface and the HTTPClient implementation.
//
// Overview:
//   Client is the surface every Bodh agent depends on; HTTPClient is the
//   default implementation backed by JSON-RPC 2.0 over HTTP+SSE.
//   Construction goes through NewHTTPClient + functional options
//   (WithAuth, WithRecorder, WithTimeout, WithHTTPClient, WithTenantID,
//   WithCaseID, WithTraceID, WithProtocolVersion, WithClientInfo).
//
// Key declarations:
//   - Client: the five-method MCP surface.
//   - HTTPClient: the JSON-RPC over HTTP+SSE implementation.
//   - NewHTTPClient, Option, With*: construction and configuration.
//   - DefaultCallTimeout, DefaultClientName/Version: tunables.
//   - classifyOutcome: maps an error + context state to OutcomeTimeout /
//     OutcomeError for the audit hook.
//
// Behavioural rules:
//   - One attempt per call. Higher-level retry policy is the calling agent's
//     responsibility — see docs/operations.md § LLM reliability for the
//     pattern Bodh uses.
//   - Context-aware throughout. Caller-supplied deadlines win; only when the
//     incoming ctx has no deadline does WithTimeout fire.
//   - PHI-free audit: arguments and results never enter the audit pipeline.
//   - Typed errors: JSON-RPC failures come back as *mcp.Error; transport
//     failures are wrapped Go errors. Use errors.As to disambiguate.

package mcp
