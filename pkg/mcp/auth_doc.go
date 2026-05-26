// auth.go — pluggable authentication providers for outbound MCP requests.
//
// Overview:
//   AuthProvider is the seam at which the HTTPClient attaches credentials to
//   each request. The default is NoAuth; production deployments wire
//   BearerAuth (typically a service-account JWT). SPIFFEAuth is a typed
//   not-implemented stub reserved for Proposal 6 (zero-trust workload
//   identity via SPIFFE/SPIRE).
//
// Key declarations:
//   - AuthProvider: the single Apply(req) interface.
//   - NoAuth: no-op provider; intended for trusted-network dev only.
//   - BearerAuth: sets Authorization: Bearer <token>.
//   - SPIFFEAuth: returns ErrNotImplemented today; real impl is a future PR.
//   - ErrNotImplemented: sentinel returned by the SPIFFEAuth stub.
//
// Why an interface instead of inlining: every future identity pattern (RFC
// 8693 token exchange, K8s ServiceAccount projected tokens, mTLS variants)
// composes here without touching the transport.

package mcp
