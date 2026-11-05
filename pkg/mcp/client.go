package mcp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
)

// DefaultCallTimeout is the per-call wall-clock cap applied when the caller
// passes no deadline via context. Long-running tool calls should be modelled
// as resources or as A2A tasks (Proposal 2), not as multi-minute MCP calls.
const DefaultCallTimeout = 30 * time.Second

// DefaultClientName / DefaultClientVersion identify Bodhyanga in the initialize
// handshake unless overridden via Option.
const (
	DefaultClientName    = "bodhyanga-mcp-client"
	DefaultClientVersion = "0.1.0"
)

// Client is the provider-agnostic MCP client surface. Implementations must
// be safe for concurrent use across goroutines.
type Client interface {
	// Initialize performs the MCP handshake with the server. Returns the
	// negotiated protocol version, server capabilities, and serverInfo.
	Initialize(ctx context.Context) (*InitializeResult, error)

	// ListTools returns the tools the server exposes. Idempotent.
	ListTools(ctx context.Context) ([]Tool, error)

	// CallTool invokes a tool by name. Arguments must match the tool's
	// declared InputSchema. The returned ToolResult.IsError flag indicates
	// a tool-reported error; transport / JSON-RPC errors come back as Go
	// errors. *mcp.Error indicates a JSON-RPC protocol error.
	CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error)

	// ListResources returns the resources the server exposes. Idempotent.
	ListResources(ctx context.Context) ([]Resource, error)

	// ReadResource fetches the contents at uri.
	ReadResource(ctx context.Context, uri string) (*ResourceContent, error)

	// Close releases any resources held by the client. Idempotent.
	Close() error
}

// HTTPClient is the default Client backed by JSON-RPC 2.0 over HTTP+SSE.
type HTTPClient struct {
	endpoint        string
	transport       *transport
	recorder        *Recorder
	timeout         time.Duration
	protocolVersion string
	clientName      string
	clientVersion   string
	tenantID        string
	caseID          string
	traceID         string
}

// NewHTTPClient constructs an HTTPClient for the given endpoint. Apply
// Options to customise auth, timeouts, audit recording, the underlying
// *http.Client, and correlation IDs.
func NewHTTPClient(endpoint string, opts ...Option) *HTTPClient {
	c := &HTTPClient{
		endpoint:        endpoint,
		timeout:         DefaultCallTimeout,
		protocolVersion: ProtocolVersion,
		clientName:      DefaultClientName,
		clientVersion:   DefaultClientVersion,
		transport: &transport{
			endpoint: endpoint,
			http:     &http.Client{Timeout: 0}, // we manage timeouts via context
			auth:     NoAuth{},
		},
		recorder: &Recorder{},
	}
	for _, opt := range opts {
		opt(c)
	}
	// Keep the transport endpoint in sync if Option mutated the client endpoint.
	c.transport.endpoint = c.endpoint
	return c
}

// Option mutates an HTTPClient at construction time.
type Option func(*HTTPClient)

// WithAuth sets the AuthProvider used for outbound requests.
func WithAuth(a AuthProvider) Option {
	return func(c *HTTPClient) {
		if a == nil {
			a = NoAuth{}
		}
		c.transport.auth = a
	}
}

// WithRecorder wires an audit.Recorder; every CallTool emits one CallEvent.
// nil disables audit recording (default).
func WithRecorder(r audit.Recorder) Option {
	return func(c *HTTPClient) {
		c.recorder = &Recorder{Sink: r}
	}
}

// WithTimeout sets the per-call wall-clock cap applied via context.WithTimeout
// when the caller doesn't already supply a deadline. Zero or negative
// resets to DefaultCallTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *HTTPClient) {
		if d <= 0 {
			d = DefaultCallTimeout
		}
		c.timeout = d
	}
}

// WithHTTPClient overrides the *http.Client used for transport. Useful for
// custom TLS, proxies, or test injection. The client's own Timeout field is
// honoured; setting it to 0 lets context deadlines do the work.
func WithHTTPClient(h *http.Client) Option {
	return func(c *HTTPClient) {
		if h != nil {
			c.transport.http = h
		}
	}
}

// WithTenantID sets the tenant correlation tag attached to every emitted
// CallEvent. Required for multi-tenant audit / RLS routing.
func WithTenantID(id string) Option {
	return func(c *HTTPClient) {
		c.tenantID = id
	}
}

// WithCaseID sets the clinical-case correlation tag attached to CallEvents.
// Optional; usually carried via context in practice.
func WithCaseID(id string) Option {
	return func(c *HTTPClient) {
		c.caseID = id
	}
}

// WithTraceID sets the distributed-trace correlation tag attached to
// CallEvents.
func WithTraceID(id string) Option {
	return func(c *HTTPClient) {
		c.traceID = id
	}
}

// WithProtocolVersion overrides the MCP protocol revision sent in the
// initialize handshake. Defaults to ProtocolVersion.
func WithProtocolVersion(v string) Option {
	return func(c *HTTPClient) {
		if v != "" {
			c.protocolVersion = v
		}
	}
}

// WithClientInfo overrides the client name / version reported in the
// initialize handshake.
func WithClientInfo(name, version string) Option {
	return func(c *HTTPClient) {
		if name != "" {
			c.clientName = name
		}
		if version != "" {
			c.clientVersion = version
		}
	}
}

// Initialize implements Client.
func (c *HTTPClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()

	params := InitializeParams{
		ProtocolVersion: c.protocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      ClientInfo{Name: c.clientName, Version: c.clientVersion},
	}
	resp, err := c.transport.call(ctx, MethodInitialize, params)
	if err != nil {
		return nil, err
	}
	var out InitializeResult
	if err := decodeResult(resp, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListTools implements Client.
func (c *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()

	resp, err := c.transport.call(ctx, MethodToolsList, struct{}{})
	if err != nil {
		return nil, err
	}
	var out listToolsResult
	if err := decodeResult(resp, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool implements Client.
//
// One audit event is emitted per invocation, regardless of success or
// failure. The event records only the endpoint, tool name, duration, and
// outcome — never arguments or response content (see audit.go).
func (c *HTTPClient) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	if name == "" {
		return nil, errors.New("mcp: CallTool: tool name is empty")
	}
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()

	start := time.Now()
	resp, err := c.transport.call(ctx, MethodToolsCall, callToolParams{Name: name, Arguments: args})
	elapsed := time.Since(start)

	if err != nil {
		c.recordCall(name, elapsed, classifyOutcome(ctx, err), err.Error())
		return nil, err
	}

	var out ToolResult
	if decodeErr := decodeResult(resp, &out); decodeErr != nil {
		c.recordCall(name, elapsed, OutcomeError, decodeErr.Error())
		return nil, decodeErr
	}

	outcome := OutcomeSuccess
	var errText string
	if out.IsError {
		outcome = OutcomeError
		errText = "tool reported isError"
	}
	c.recordCall(name, elapsed, outcome, errText)
	return &out, nil
}

// ListResources implements Client.
func (c *HTTPClient) ListResources(ctx context.Context) ([]Resource, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()

	resp, err := c.transport.call(ctx, MethodResourcesList, struct{}{})
	if err != nil {
		return nil, err
	}
	var out listResourcesResult
	if err := decodeResult(resp, &out); err != nil {
		return nil, err
	}
	return out.Resources, nil
}

// ReadResource implements Client.
func (c *HTTPClient) ReadResource(ctx context.Context, uri string) (*ResourceContent, error) {
	if uri == "" {
		return nil, errors.New("mcp: ReadResource: uri is empty")
	}
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()

	resp, err := c.transport.call(ctx, MethodResourcesRead, readResourceParams{URI: uri})
	if err != nil {
		return nil, err
	}
	var out ResourceContent
	if err := decodeResult(resp, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Close implements Client. The HTTP transport has no long-lived state today,
// so Close is a no-op — kept on the interface for future SSE-pinned or mTLS
// session-cache implementations.
func (c *HTTPClient) Close() error { return nil }

// withDeadline applies the configured per-call timeout iff the incoming
// context has no deadline of its own. This honours upstream context
// cancellation while still capping the per-call latency.
func (c *HTTPClient) withDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		// Caller-supplied deadline wins.
		return context.WithCancel(ctx)
	}
	if c.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}

// recordCall emits one CallEvent. Audit failures are logged at the call
// site (callers can wrap a logging Recorder if they want to see them); we
// never let a missing audit sink abort a clinical call.
func (c *HTTPClient) recordCall(toolName string, elapsed time.Duration, outcome, errText string) {
	if c.recorder == nil {
		return
	}
	_ = c.recorder.Record(CallEvent{
		Endpoint:  sanitiseEndpoint(c.endpoint),
		ToolName:  toolName,
		Duration:  elapsed,
		Outcome:   outcome,
		ErrorText: errText,
		TenantID:  c.tenantID,
		CaseID:    c.caseID,
		TraceID:   c.traceID,
	})
}

// classifyOutcome decides between OutcomeTimeout and OutcomeError given the
// transport error and the originating context. context.DeadlineExceeded /
// context.Canceled are the timeout signals; everything else is a generic
// error.
func classifyOutcome(ctx context.Context, err error) string {
	if err == nil {
		return OutcomeSuccess
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return OutcomeTimeout
	}
	if ctxErr := ctx.Err(); ctxErr != nil && (errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(ctxErr, context.Canceled)) {
		return OutcomeTimeout
	}
	return OutcomeError
}

// Compile-time assertion that HTTPClient implements Client.
var _ Client = (*HTTPClient)(nil)
