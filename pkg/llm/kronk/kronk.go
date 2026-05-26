// kronk.go — Ardan Labs kronk (llama.cpp via Go) adapter for pkg/llm.Client.
//
// Build tag: this file is compiled ONLY when the `kronk` build tag is set
// (e.g. `go build -tags kronk ./cmd/care`). The default Bodh build does not
// link kronk / llama.cpp / yzma / bucky — see kronk_stub.go for the no-op
// shim used otherwise. Gating keeps go.sum minimal (and CGO out of the
// hot path) for users who do not want a native llama.cpp dep.
//
// PHI policy: kronk runs entirely in-process — no network egress, no API
// keys, no third-party log sinks. That is the whole point of this provider:
// a HIPAA-friendly "no PHI leaves the box" path for clinical reasoning.
// Even so, the adapter writes traces via observability.Logger /
// observability.LLMTrace, which the pkg/phi.LoggerWrapper redacts, so
// even local log files do not collect PHI fingerprints.
//
// Wiring example (only meaningful behind the kronk tag):
//
//	logger := observability.NewStdLogger()
//	client, err := kronk.New("unsloth/Qwen3-0.6B-Q8_0", logger, logger)
//	if err != nil { log.Fatal(err) }
//	defer client.Close()
//	resp, err := client.Complete(ctx, llm.Request{...})

//go:build kronk

package kronk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ardanlabs/kronk"

	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/observability"
)

// ProviderName is the short identifier returned by Client.Provider().
// It appears in LLM traces, bench output, and the `-diagnostician=llm-kronk`
// factory arm.
const ProviderName = "kronk"

// DefaultModelSource is used when New is given an empty modelSource. The
// Qwen3 0.6B Q8_0 quant is small enough to fit on a laptop and tracks the
// example at https://github.com/ardanlabs/kronk/blob/main/examples/chat/main.go .
const DefaultModelSource = "unsloth/Qwen3-0.6B-Q8_0"

// Tracer is the minimal sink kronk uses to record one observability.LLMTrace
// per Complete call. *observability.StdLogger satisfies it via its
// RecordLLMCall method; tests substitute a fake. Kept as an interface so we
// do not couple the kronk provider to a concrete logger type — and so the
// pkg/phi.LoggerWrapper can be inserted at the seam without touching this
// package.
type Tracer interface {
	RecordLLMCall(t observability.LLMTrace)
}

// nopTracer drops every trace. Used when the caller passes nil for the
// tracer argument — a kronk binary should never be wired without a tracer,
// but the package must remain safe to call.
type nopTracer struct{}

func (nopTracer) RecordLLMCall(_ observability.LLMTrace) {}

// kronkBackend is the subset of kronk.Client used by this adapter. The
// interface lets tests inject a fake without spinning up llama.cpp.
type kronkBackend interface {
	Chat(ctx context.Context, req kronk.ChatRequest) (kronk.ChatResponse, error)
	Close() error
}

// Client is the kronk-backed pkg/llm.Client implementation. Built via New.
// Methods are safe for concurrent use to the extent the underlying kronk
// client is — see the kronk docs for thread-safety guarantees on the
// specific backend you compile against.
type Client struct {
	modelSource string
	logger      observability.Logger
	tracer      Tracer
	backend     kronkBackend

	// grammarOption is an opt-in GBNF grammar applied on Complete when set.
	// The current adapter accepts and stores it but does not yet wire it
	// into the kronk request (the GBNF grammar track populates this later).
	// Leaving the field present keeps the public API stable across the
	// integration step.
	grammarOption string
}

// Option configures the Client at construction time. Each option is a
// closure that mutates the not-yet-returned Client. The functional-options
// pattern keeps the constructor signature short and lets the GBNF grammar
// track add WithGrammar without breaking existing callers.
type Option func(*Client)

// WithGrammar attaches a GBNF grammar string to every Complete call. The
// grammar track will populate this once kronk's grammar API stabilises; the
// adapter accepts and stores the option today so callers can write the
// wiring against a fixed API.
func WithGrammar(grammar string) Option {
	return func(c *Client) { c.grammarOption = grammar }
}

// New constructs a kronk-backed Client. modelSource follows kronk's model
// identifier convention (e.g. "unsloth/Qwen3-0.6B-Q8_0"); empty falls back
// to DefaultModelSource. logger receives high-level lifecycle messages
// (provider startup / shutdown / fallback events). tracer receives one
// observability.LLMTrace per Complete call — pass *observability.StdLogger
// to get a JSON-line trace for free.
//
// Returns an error when the kronk backend cannot load the model. The error
// is intentionally surfaced (not retried) so the caller can fall back to a
// rule-based agent — the fallback contract from AGENTS.md invariant #3.
func New(modelSource string, logger observability.Logger, tracer Tracer, opts ...Option) (*Client, error) {
	if modelSource == "" {
		modelSource = DefaultModelSource
	}
	if tracer == nil {
		tracer = nopTracer{}
	}

	// kronk.NewClient pulls the model into the local cache on first call
	// (network) and then runs offline thereafter. Any error here surfaces
	// to the caller; the diagnostician factory translates a startup error
	// into "fall back to the rule path" without crashing the orchestrator.
	backend, err := kronk.NewClient(kronk.Config{
		ModelSource: modelSource,
	})
	if err != nil {
		return nil, fmt.Errorf("kronk: load %q: %w", modelSource, err)
	}

	c := &Client{
		modelSource: modelSource,
		logger:      logger,
		tracer:      tracer,
		backend:     backend,
	}
	for _, opt := range opts {
		opt(c)
	}

	if c.logger != nil {
		c.logger.Printf("kronk: ready (model=%s)", modelSource)
	}
	return c, nil
}

// Provider returns "kronk" so bench output and LLM traces can attribute
// requests to this provider.
func (c *Client) Provider() string { return ProviderName }

// Model echoes the model identifier the client was constructed with. The
// llm.Response also carries this on every call.
func (c *Client) Model() string { return c.modelSource }

// Close releases the underlying kronk backend (frees the llama.cpp context
// and memory). Always defer Close after a successful New. Safe to call
// multiple times — the second call is a no-op.
func (c *Client) Close() error {
	if c == nil || c.backend == nil {
		return nil
	}
	be := c.backend
	c.backend = nil
	return be.Close()
}

// Complete runs one chat completion. The request is translated to kronk's
// native ChatRequest shape; the response is parsed back into llm.Response
// with token accounting and a stop reason. JSON-schema responses are
// validated client-side (kronk does not yet enforce JSON Schema natively;
// the grammar track will close that gap). PHI never leaves the box — the
// kronk backend is local.
func (c *Client) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if c == nil || c.backend == nil {
		return nil, errors.New("kronk: client is closed")
	}
	if len(req.Messages) == 0 {
		return nil, llm.ErrNoMessages
	}

	kreq := c.buildRequest(req)

	start := time.Now()
	kresp, err := c.backend.Chat(ctx, kreq)
	dur := time.Since(start)

	// Record one trace per call. Even on error, we record so the fallback
	// rate is observable in aggregate. PHI does not appear here: the trace
	// only carries provider/model/token counts/stop reason — no prompt,
	// no response, no case ID (the caller adds CaseID before recording
	// when it has one).
	trace := observability.LLMTrace{
		StartedAt:    start,
		DurationMs:   dur.Milliseconds(),
		Provider:     ProviderName,
		Model:        c.modelSource,
		InputTokens:  kresp.PromptTokens,
		OutputTokens: kresp.CompletionTokens,
		Kind:         "completion",
	}
	if err != nil {
		trace.Error = err.Error()
		c.tracer.RecordLLMCall(trace)
		return nil, fmt.Errorf("kronk: chat: %w", err)
	}

	content := kresp.Content
	if req.JSONSchema != nil {
		if content == "" {
			c.tracer.RecordLLMCall(trace)
			return nil, llm.ErrInvalidJSON
		}
		// Some quantised local models wrap JSON in code fences; strip them
		// before validation so the caller does not have to.
		content = stripJSONFences(content)
		var tmp any
		if jerr := json.Unmarshal([]byte(content), &tmp); jerr != nil {
			trace.Error = "invalid json"
			c.tracer.RecordLLMCall(trace)
			return nil, llm.ErrInvalidJSON
		}
	}

	stop := kresp.StopReason
	if stop == "" {
		stop = "stop"
	}
	trace.StopReason = stop
	c.tracer.RecordLLMCall(trace)

	return &llm.Response{
		Content:      content,
		InputTokens:  kresp.PromptTokens,
		OutputTokens: kresp.CompletionTokens,
		CachedTokens: 0, // kronk runs locally; no provider-reported cache.
		Model:        c.modelSource,
		Provider:     ProviderName,
		StopReason:   stop,
	}, nil
}

// buildRequest translates a Bodh llm.Request into kronk's ChatRequest. The
// system prompt becomes a leading system message; the rest of Messages are
// passed through in order. Images are NOT yet supported by this adapter —
// vision-capable kronk models will land in a follow-up.
func (c *Client) buildRequest(req llm.Request) kronk.ChatRequest {
	msgs := make([]kronk.ChatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, kronk.ChatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, kronk.ChatMessage{Role: string(m.Role), Content: m.Content})
	}
	kreq := kronk.ChatRequest{
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	// The grammar track populates GrammarOption later; once kronk's API
	// settles we will pass it through here. For now, callers can already
	// supply WithGrammar at construction; the field round-trips intact.
	if c.grammarOption != "" {
		kreq.Grammar = c.grammarOption
	}
	return kreq
}

// stripJSONFences removes leading "```json" / "```" fences and trailing
// "```" some quantised local models like to emit. Idempotent. Returns the
// original string if no fences are found.
func stripJSONFences(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return s
	}
	// Drop the opening fence (and optional language tag).
	if idx := strings.Index(t, "\n"); idx >= 0 {
		t = t[idx+1:]
	}
	t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	return strings.TrimSpace(t)
}

// Compile-time assertion that *Client implements llm.Client. If kronk's
// shape ever drifts away from the Bodh interface, this line is the first
// thing that fails to build under -tags=kronk.
var _ llm.Client = (*Client)(nil)
