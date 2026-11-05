package agents

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/embeddings"
	"github.com/PratikDhanave/bodhyanga/pkg/tasks"
)

// Test buildDiagnostician with Anthropic key set.
func TestBuildDiagnosticianAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	a, err := buildDiagnostician(ProviderLLMAnthropic, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

// Test buildDiagnostician with OpenAI key set.
func TestBuildDiagnosticianOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	a, err := buildDiagnostician(ProviderLLMOpenAI, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

// Test buildDiagnostician Ollama.
func TestBuildDiagnosticianOllama(t *testing.T) {
	a, err := buildDiagnostician(ProviderLLMOllama, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

// Test buildDiagnostician unknown.
func TestBuildDiagnosticianUnknown(t *testing.T) {
	_, err := buildDiagnostician("bogus", nil)
	if err == nil {
		t.Error("expected error")
	}
}

// Test NewDiagnosticianDebate with Anthropic.
func TestNewDiagnosticianDebateAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	a, err := NewDiagnosticianDebate(ProviderLLMAnthropic, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DebateDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewDiagnosticianDebate with unknown provider.
func TestNewDiagnosticianDebateUnknown(t *testing.T) {
	_, err := NewDiagnosticianDebate("bogus", nil, nil)
	if err == nil {
		t.Error("expected error")
	}
}

// Test NewDiagnosticianVision with Anthropic.
func TestNewDiagnosticianVisionAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	a, err := NewDiagnosticianVision(ProviderLLMAnthropic, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMDiagnosticianVision); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewDiagnosticianVision with unknown.
func TestNewDiagnosticianVisionUnknown(t *testing.T) {
	_, err := NewDiagnosticianVision("bogus", nil)
	if err == nil {
		t.Error("expected error")
	}
}

// Test NewDiagnosticianTools with Anthropic.
func TestNewDiagnosticianToolsAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	a, err := NewDiagnosticianTools(ProviderLLMAnthropic, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*ToolUsingDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewDiagnosticianTools with unknown.
func TestNewDiagnosticianToolsUnknown(t *testing.T) {
	_, err := NewDiagnosticianTools("bogus", nil, nil)
	if err == nil {
		t.Error("expected error")
	}
}

// Test NewDiagnosticianReflexion with Anthropic.
func TestNewDiagnosticianReflexionAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	a, err := NewDiagnosticianReflexion(ProviderLLMAnthropic, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*ReflexionDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewDiagnosticianReflexion with unknown.
func TestNewDiagnosticianReflexionUnknown(t *testing.T) {
	_, err := NewDiagnosticianReflexion("bogus", nil, 1)
	if err == nil {
		t.Error("expected error")
	}
}

// Test NewDiagnosticianCascade with Anthropic.
func TestNewDiagnosticianCascadeAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	a, err := NewDiagnosticianCascade(ProviderLLMAnthropic, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*CascadeDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewDiagnosticianCascade with OpenAI.
func TestNewDiagnosticianCascadeOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	a, err := NewDiagnosticianCascade(ProviderLLMOpenAI, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*CascadeDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewQuestioner with Anthropic.
func TestNewQuestionerAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	a, err := NewQuestioner(ProviderLLMAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMQuestioner); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewQuestioner missing key.
func TestNewQuestionerAnthropicMissingKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	_, err := NewQuestioner(ProviderLLMAnthropic)
	if err == nil {
		t.Error("expected error")
	}
}

// Test NewInboxTriage with Anthropic.
func TestNewInboxTriageAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	q := tasks.NewQueue()
	a, err := NewInboxTriage(ProviderLLMAnthropic, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMInboxTriage); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewInboxTriage missing key.
func TestNewInboxTriageAnthropicMissingKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	q := tasks.NewQueue()
	_, err := NewInboxTriage(ProviderLLMAnthropic, q)
	if err == nil {
		t.Error("expected error")
	}
}

// Test NewReasoningVerifier with Anthropic.
func TestNewReasoningVerifierAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	a, err := NewReasoningVerifier(ProviderLLMAnthropic, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMReasoningVerifier); !ok {
		t.Errorf("got %T", a)
	}
}

// Test NewReasoningVerifier missing key.
func TestNewReasoningVerifierAnthropicMissingKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	_, err := NewReasoningVerifier(ProviderLLMAnthropic, false)
	if err == nil {
		t.Error("expected error")
	}
}

// Test DefaultEmbedderForProvider with OpenAI key set.
func TestDefaultEmbedderForProviderOpenAIWithKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	e, err := DefaultEmbedderForProvider(ProviderLLMOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if e == nil {
		t.Error("should return embedder with key set")
	}
}

// Test DefaultEmbedderForProvider with Voyage key set.
func TestDefaultEmbedderForProviderAnthropicWithVoyage(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "test-key")
	e, err := DefaultEmbedderForProvider(ProviderLLMAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if e == nil {
		t.Error("should return embedder with voyage key set")
	}
}

// Test LoadDefaultKnowledge with a valid (but empty) knowledge file.
func TestLoadDefaultKnowledgeWithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.json")
	// Write a valid corpus JSON file.
	os.WriteFile(path, []byte(`{"documents":[]}`), 0644)
	t.Setenv("BODHYANGA_KNOWLEDGE_CORPUS", path)

	// Use a simple embedder.
	e := embeddings.NewOllama("", "nomic-embed-text")
	store, err := LoadDefaultKnowledge(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Error("should return non-nil store")
	}
}

// Test LoadDefaultKnowledge with an invalid file path.
func TestLoadDefaultKnowledgeInvalidPath(t *testing.T) {
	t.Setenv("BODHYANGA_KNOWLEDGE_CORPUS", "/nonexistent/path/corpus.json")
	e := embeddings.NewOllama("", "nomic-embed-text")
	_, err := LoadDefaultKnowledge(context.Background(), e)
	if err == nil {
		t.Error("expected error for invalid path")
	}
}
