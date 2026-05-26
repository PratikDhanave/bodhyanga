// kronk_real_test.go — tests for the real kronk-backed Client.
//
// Only compiled under -tags=kronk. The CGO-heavy kronk SDK runs only on
// linux and darwin; we t.Skip everywhere else so the file is still
// compilable on Windows runners (the kronk dep itself enforces the
// platform constraint, but the explicit Skip keeps the failure mode
// readable when someone tries to run tests on an unsupported OS).
//
// We never pull a real model in the test: a fakeBackend is injected via
// the package-level newClientWithBackend helper, exercising the message
// translation, response parsing, JSON-fence stripping, and trace-emission
// paths without touching llama.cpp.

//go:build kronk

package kronk

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ardanlabs/kronk"

	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/observability"
)

// skipUnsupportedOS guards tests that need the CGO backend even via a fake
// — the kronk import itself may pull a build-time constraint that limits
// the platforms we can compile on.
func skipUnsupportedOS(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("kronk requires CGO; skipping on %s", runtime.GOOS)
	}
}

// fakeBackend replaces the real kronk.Client in tests so we never load a
// model. It records the last ChatRequest seen and returns a canned
// ChatResponse (or error).
type fakeBackend struct {
	mu       sync.Mutex
	lastReq  kronk.ChatRequest
	resp     kronk.ChatResponse
	err      error
	closed   int
	closeErr error
}

func (f *fakeBackend) Chat(_ context.Context, req kronk.ChatRequest) (kronk.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReq = req
	if f.err != nil {
		return kronk.ChatResponse{}, f.err
	}
	return f.resp, nil
}

func (f *fakeBackend) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return f.closeErr
}

// recordingTracer captures every LLMTrace emitted so tests can assert on
// it. Substitutes for *observability.StdLogger in unit tests.
type recordingTracer struct {
	mu     sync.Mutex
	traces []observability.LLMTrace
}

func (r *recordingTracer) RecordLLMCall(t observability.LLMTrace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.traces = append(r.traces, t)
}

// newClientWithBackend builds a Client wired to a caller-supplied backend.
// Exported (within the test build) to keep the production New constructor
// from leaking a test seam.
func newClientWithBackend(modelSource string, logger observability.Logger, tracer Tracer, be kronkBackend) *Client {
	if tracer == nil {
		tracer = nopTracer{}
	}
	return &Client{
		modelSource: modelSource,
		logger:      logger,
		tracer:      tracer,
		backend:     be,
	}
}

func TestReal_CompleteHappyPath(t *testing.T) {
	skipUnsupportedOS(t)
	be := &fakeBackend{
		resp: kronk.ChatResponse{
			Content:          "the diagnosis is pneumonia",
			PromptTokens:     42,
			CompletionTokens: 13,
			StopReason:       "stop",
		},
	}
	tracer := &recordingTracer{}
	c := newClientWithBackend("unsloth/Qwen3-0.6B-Q8_0", nil, tracer, be)

	resp, err := c.Complete(context.Background(), llm.Request{
		System:    "be terse",
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "what is it?"}},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "the diagnosis is pneumonia" {
		t.Fatalf("content = %q", resp.Content)
	}
	if resp.InputTokens != 42 || resp.OutputTokens != 13 {
		t.Fatalf("token counts wrong: %+v", resp)
	}
	if resp.Provider != ProviderName {
		t.Fatalf("provider = %q", resp.Provider)
	}
	if resp.Model != "unsloth/Qwen3-0.6B-Q8_0" {
		t.Fatalf("model = %q", resp.Model)
	}
	if resp.StopReason != "stop" {
		t.Fatalf("stop = %q", resp.StopReason)
	}
	if len(tracer.traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(tracer.traces))
	}
	tr := tracer.traces[0]
	if tr.Provider != ProviderName || tr.Model != "unsloth/Qwen3-0.6B-Q8_0" {
		t.Fatalf("trace identity wrong: %+v", tr)
	}
	if tr.InputTokens != 42 || tr.OutputTokens != 13 {
		t.Fatalf("trace tokens wrong: %+v", tr)
	}
}

func TestReal_MessageTranslation(t *testing.T) {
	skipUnsupportedOS(t)
	be := &fakeBackend{resp: kronk.ChatResponse{Content: "ok"}}
	c := newClientWithBackend("m", nil, nil, be)

	_, err := c.Complete(context.Background(), llm.Request{
		System: "S",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "U1"},
			{Role: llm.RoleAssistant, Content: "A1"},
			{Role: llm.RoleUser, Content: "U2"},
		},
		MaxTokens:   128,
		Temperature: 0.3,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got := be.lastReq.Messages
	if len(got) != 4 {
		t.Fatalf("expected 4 messages (1 system + 3 turns), got %d", len(got))
	}
	if got[0].Role != "system" || got[0].Content != "S" {
		t.Fatalf("system msg wrong: %+v", got[0])
	}
	if got[1].Role != "user" || got[1].Content != "U1" {
		t.Fatalf("user1 wrong: %+v", got[1])
	}
	if got[2].Role != "assistant" || got[2].Content != "A1" {
		t.Fatalf("assistant wrong: %+v", got[2])
	}
	if got[3].Role != "user" || got[3].Content != "U2" {
		t.Fatalf("user2 wrong: %+v", got[3])
	}
	if be.lastReq.MaxTokens != 128 {
		t.Fatalf("max tokens = %d", be.lastReq.MaxTokens)
	}
}

func TestReal_NoMessagesError(t *testing.T) {
	skipUnsupportedOS(t)
	c := newClientWithBackend("m", nil, nil, &fakeBackend{})
	_, err := c.Complete(context.Background(), llm.Request{})
	if !errors.Is(err, llm.ErrNoMessages) {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

func TestReal_BackendErrorPropagates(t *testing.T) {
	skipUnsupportedOS(t)
	be := &fakeBackend{err: errors.New("oom")}
	tracer := &recordingTracer{}
	c := newClientWithBackend("m", nil, tracer, be)

	_, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "kronk: chat") {
		t.Fatalf("error not wrapped: %v", err)
	}
	if len(tracer.traces) != 1 {
		t.Fatalf("expected 1 trace even on error, got %d", len(tracer.traces))
	}
	if tracer.traces[0].Error == "" {
		t.Fatal("error trace missing Error field")
	}
}

func TestReal_JSONSchemaValidatesContent(t *testing.T) {
	skipUnsupportedOS(t)
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"valid object", `{"x":1}`, false},
		{"json fenced", "```json\n{\"x\":1}\n```", false},
		{"plain fenced", "```\n{\"x\":1}\n```", false},
		{"invalid", "not json", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			be := &fakeBackend{resp: kronk.ChatResponse{Content: tc.content}}
			c := newClientWithBackend("m", nil, nil, be)
			_, err := c.Complete(context.Background(), llm.Request{
				Messages:   []llm.Message{{Role: llm.RoleUser, Content: "x"}},
				JSONSchema: &llm.JSONSchema{Name: "x", Schema: []byte(`{}`)},
			})
			if tc.wantErr && err == nil {
				t.Fatalf("want err, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, llm.ErrInvalidJSON) {
				t.Fatalf("want ErrInvalidJSON, got %v", err)
			}
		})
	}
}

func TestReal_EmptyModelSourceUsesDefault(t *testing.T) {
	skipUnsupportedOS(t)
	// We can't drive New here without loading a real model, but we can
	// verify the default constant matches the example referenced in the
	// kronk repo.
	if DefaultModelSource != "unsloth/Qwen3-0.6B-Q8_0" {
		t.Fatalf("DefaultModelSource drifted: %q", DefaultModelSource)
	}
}

func TestReal_CloseReleasesBackend(t *testing.T) {
	skipUnsupportedOS(t)
	be := &fakeBackend{}
	c := newClientWithBackend("m", nil, nil, be)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if be.closed != 1 {
		t.Fatalf("backend Close called %d times, want 1", be.closed)
	}
	// Second Close is safe.
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if be.closed != 1 {
		t.Fatalf("backend Close called %d times on second client.Close, want still 1", be.closed)
	}
}

func TestReal_CompleteAfterCloseErrors(t *testing.T) {
	skipUnsupportedOS(t)
	be := &fakeBackend{}
	c := newClientWithBackend("m", nil, nil, be)
	_ = c.Close()
	_, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error after Close")
	}
}

func TestReal_WithGrammarRoundTrip(t *testing.T) {
	skipUnsupportedOS(t)
	c := &Client{}
	WithGrammar("root ::= object")(c)
	if c.grammarOption != "root ::= object" {
		t.Fatalf("grammar option lost: %q", c.grammarOption)
	}
}
