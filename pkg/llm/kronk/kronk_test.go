// kronk_test.go — default-build tests for the kronk provider.
//
// In the default build (no -tags=kronk) the stub is what's compiled. These
// tests verify the stub contract: New errors loudly, Client.Provider /
// Close are inert but safe, and the satisfied llm.Client surface keeps
// callers from compile-time-breaking when they wire the constructor.
//
// The real-backend behaviour (message translation, response parsing,
// JSON-fence stripping, trace emission) is exercised in kronk_real_test.go
// which is gated behind `//go:build kronk` so it can import a fake kronk
// backend without dragging the CGO/llama.cpp deps into the default test
// run.

//go:build !kronk

package kronk

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

func TestStub_NewReturnsBuildTagError(t *testing.T) {
	logger := observability.NewStdLogger()
	_, err := New("unsloth/Qwen3-0.6B-Q8_0", logger, logger)
	if err == nil {
		t.Fatal("expected error when kronk build tag is not set")
	}
	if !strings.Contains(err.Error(), "requires build tag") {
		t.Fatalf("error should mention build tag, got %q", err)
	}
}

func TestStub_NewReturnsBuildTagErrorOnEmptyModel(t *testing.T) {
	// The stub does not even default the model source — the error path
	// must trip on the missing build tag regardless of arguments.
	_, err := New("", nil, nil)
	if err == nil {
		t.Fatal("expected build-tag error even with empty model source")
	}
}

func TestStub_NewAcceptsOptionsWithoutPanic(t *testing.T) {
	// The stub WithGrammar must be safe to call; downstream code constructs
	// the option unconditionally to keep the kronk-tagged code from
	// branching every wire point.
	opt := WithGrammar("root ::= object")
	if opt == nil {
		t.Fatal("WithGrammar returned nil")
	}
	_, err := New("", nil, nil, opt)
	if err == nil {
		t.Fatal("expected build-tag error")
	}
}

func TestStub_ProviderAndModelOnZeroValueClient(t *testing.T) {
	var c Client
	if got := c.Provider(); got != ProviderName {
		t.Fatalf("Provider = %q, want %q", got, ProviderName)
	}
	if got := c.Model(); got != "" {
		t.Fatalf("Model = %q, want empty", got)
	}
}

func TestStub_CompleteErrors(t *testing.T) {
	var c Client
	_, err := c.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected stub Complete to error")
	}
}

func TestStub_CloseIsNoop(t *testing.T) {
	var c Client
	if err := c.Close(); err != nil {
		t.Fatalf("Close should be no-op, got %v", err)
	}
}

func TestStub_SatisfiesLLMClient(t *testing.T) {
	// Compile-time guarantee verified at package scope (var _ llm.Client).
	// This test exists so a future refactor that breaks the assertion
	// fails the test suite, not just the build.
	var _ llm.Client = (*Client)(nil)

	// And the error chain plays nicely with errors.Is for callers that
	// want to special-case the build-tag miss.
	_, err := New("any", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, llm.ErrNoMessages) {
		t.Fatalf("stub error should not be ErrNoMessages: %v", err)
	}
}

// DefaultModelSource is the same identifier across build tags so config
// values do not silently shift when callers flip the tag.
func TestStub_DefaultModelSourceConstant(t *testing.T) {
	if DefaultModelSource == "" {
		t.Fatal("DefaultModelSource is empty")
	}
	if !strings.Contains(DefaultModelSource, "Qwen") && !strings.Contains(DefaultModelSource, "Llama") {
		t.Fatalf("DefaultModelSource %q does not look like a kronk model id", DefaultModelSource)
	}
}
