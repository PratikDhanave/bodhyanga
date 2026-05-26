package mcp

import (
	"errors"
	"net/http"
)

// AuthProvider attaches credentials to outbound MCP HTTP requests. Apply runs
// after the JSON-RPC body has been built but before the request is dispatched;
// implementations should mutate req in place (e.g. set Authorization headers,
// attach TLS material) and return any error fatal to the request.
type AuthProvider interface {
	Apply(req *http.Request) error
}

// NoAuth attaches no credentials. Useful for development MCP servers that
// run inside a trusted network boundary; production deployments should use
// BearerAuth or (once Proposal 6 lands) SPIFFEAuth.
type NoAuth struct{}

// Apply is a no-op for NoAuth.
func (NoAuth) Apply(_ *http.Request) error { return nil }

// BearerAuth attaches an "Authorization: Bearer <token>" header. The token
// is captured by value at construction time; rotate by constructing a new
// BearerAuth or wrap with a token-source-backed implementation in a future
// change.
type BearerAuth struct {
	Token string
}

// Apply implements AuthProvider.
func (b BearerAuth) Apply(req *http.Request) error {
	if b.Token == "" {
		return errors.New("mcp: BearerAuth token is empty")
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	return nil
}

// ErrNotImplemented is returned by stubs whose real implementation lives in a
// future PR. Today the only stub is SPIFFEAuth (Proposal 6).
var ErrNotImplemented = errors.New("mcp: not implemented in this PR")

// SPIFFEAuth is a placeholder for SPIFFE/SPIRE-issued SVID-based mTLS auth.
// Real implementation lives in Proposal 6 — it will fetch an X.509 SVID via
// the Workload API, present it as the client certificate, and validate the
// peer's SPIFFE ID against an allowlist. Today Apply returns
// ErrNotImplemented so callers fail loudly instead of silently degrading to
// unauthenticated calls.
type SPIFFEAuth struct{}

// Apply implements AuthProvider — currently a typed not-implemented stub.
func (SPIFFEAuth) Apply(_ *http.Request) error { return ErrNotImplemented }
