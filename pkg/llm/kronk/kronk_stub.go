// kronk_stub.go — no-op shim used when the `kronk` build tag is NOT set.
//
// Bodhyanga's default build does not link kronk (which pulls llama.cpp + yzma +
// bucky and requires CGO). This stub keeps the Client type, the New
// constructor, and the Tracer / Option / WithGrammar surfaces compiling so
// callers can reference them unconditionally (cmd/care, cmd/demo, cmd/bench
// import the package but only build the real implementation when -tags=kronk
// is passed). All operations are no-ops or return a clear error.
//
// To get real local-llama.cpp inference: build with
//
//   go build -tags kronk ./cmd/care

//go:build !kronk

package kronk

import (
	"context"
	"errors"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

// ProviderName matches the real implementation so bench / log output is
// consistent across build tags.
const ProviderName = "kronk"

// DefaultModelSource mirrors the real default so callers see the same
// identifier in their config regardless of build tag.
const DefaultModelSource = "unsloth/Qwen3-0.6B-Q8_0"

// Tracer is the same interface the real implementation accepts. Kept here
// so callers do not need build tags around their wiring code.
type Tracer interface {
	RecordLLMCall(t observability.LLMTrace)
}

// Option is the functional-options shape from the real implementation.
// The stub accepts options but ignores them — the real implementation
// applies them in New.
type Option func(*Client)

// WithGrammar is a no-op in the default build. The real implementation
// stores the grammar string and passes it to kronk when GBNF support lands.
func WithGrammar(_ string) Option { return func(_ *Client) {} }

// Client is a stub in the default build. Methods return clear errors or
// zero values so a stray caller is loud, not silently wrong.
type Client struct{}

// New returns an error in the default build to signal the kronk SDK was
// not linked. Rebuild with `-tags=kronk` for the real provider.
//
// The error message is stable: the diagnostician factory matches on the
// "requires build tag" substring when deciding whether to surface a
// helpful "rebuild with -tags=kronk" hint to the operator.
func New(_ string, _ observability.Logger, _ Tracer, _ ...Option) (*Client, error) {
	return nil, errors.New("kronk provider requires build tag: rebuild with -tags=kronk")
}

// Provider returns "kronk" for parity with the real implementation.
func (c *Client) Provider() string { return ProviderName }

// Model returns an empty string in the stub — there is no loaded model.
func (c *Client) Model() string { return "" }

// Complete always errors in the stub: callers should never reach this in
// the default build because New returns an error first. If they do, fail
// loud so the misconfiguration is visible.
func (c *Client) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, errors.New("kronk: not built with -tags=kronk")
}

// Close is a no-op in the stub.
func (c *Client) Close() error { return nil }

// Compile-time assertion that *Client satisfies llm.Client even in the
// default build — keeps the stub honest if the interface drifts.
var _ llm.Client = (*Client)(nil)
