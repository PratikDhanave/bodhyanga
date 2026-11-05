package agents

import (
	"os"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/tasks"
)

func TestNewDiagnosticianRule(t *testing.T) {
	a, err := NewDiagnostician("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DiagnosticianAgent); !ok {
		t.Errorf("empty provider should return DiagnosticianAgent, got %T", a)
	}

	a, err = NewDiagnostician(ProviderRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DiagnosticianAgent); !ok {
		t.Errorf("rule provider should return DiagnosticianAgent, got %T", a)
	}
}

func TestNewDiagnosticianUnknown(t *testing.T) {
	_, err := NewDiagnostician("bogus")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestNewDiagnosticianAnthropicMissingKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	_, err := NewDiagnostician(ProviderLLMAnthropic)
	if err == nil {
		t.Error("expected error when ANTHROPIC_API_KEY not set")
	}
}

func TestNewDiagnosticianOpenAIMissingKey(t *testing.T) {
	os.Unsetenv("OPENAI_API_KEY")
	_, err := NewDiagnostician(ProviderLLMOpenAI)
	if err == nil {
		t.Error("expected error when OPENAI_API_KEY not set")
	}
}

func TestNewDiagnosticianOllama(t *testing.T) {
	a, err := NewDiagnostician(ProviderLLMOllama)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMDiagnostician); !ok {
		t.Errorf("ollama should return LLMDiagnostician, got %T", a)
	}
}

func TestNewDiagnosticianWithKnowledge(t *testing.T) {
	a, err := NewDiagnosticianWithKnowledge(ProviderRule, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DiagnosticianAgent); !ok {
		t.Error("rule+knowledge should return DiagnosticianAgent")
	}
}

func TestNewDiagnosticianDebateRule(t *testing.T) {
	a, err := NewDiagnosticianDebate("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DiagnosticianAgent); !ok {
		t.Errorf("empty provider debate should return DiagnosticianAgent, got %T", a)
	}
}

func TestNewDiagnosticianDebateOllama(t *testing.T) {
	a, err := NewDiagnosticianDebate(ProviderLLMOllama, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DebateDiagnostician); !ok {
		t.Errorf("ollama debate should return DebateDiagnostician, got %T", a)
	}
}

func TestNewDiagnosticianDebateWithSpecialists(t *testing.T) {
	specs := []Specialist{{Name: "test", Description: "test specialist"}}
	a, err := NewDiagnosticianDebate(ProviderLLMOllama, nil, specs)
	if err != nil {
		t.Fatal(err)
	}
	debate, ok := a.(*DebateDiagnostician)
	if !ok {
		t.Fatalf("got %T", a)
	}
	if len(debate.Specialists) != 1 {
		t.Errorf("specialists=%d, want 1", len(debate.Specialists))
	}
}

func TestNewDiagnosticianVisionRule(t *testing.T) {
	a, err := NewDiagnosticianVision("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DiagnosticianAgent); !ok {
		t.Errorf("rule vision should return DiagnosticianAgent, got %T", a)
	}
}

func TestNewDiagnosticianVisionOllama(t *testing.T) {
	a, err := NewDiagnosticianVision(ProviderLLMOllama, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMDiagnosticianVision); !ok {
		t.Errorf("ollama vision should return LLMDiagnosticianVision, got %T", a)
	}
}

func TestNewDiagnosticianToolsRule(t *testing.T) {
	a, err := NewDiagnosticianTools("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DiagnosticianAgent); !ok {
		t.Errorf("rule tools should return DiagnosticianAgent, got %T", a)
	}
}

func TestNewDiagnosticianToolsOllama(t *testing.T) {
	a, err := NewDiagnosticianTools(ProviderLLMOllama, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*ToolUsingDiagnostician); !ok {
		t.Errorf("ollama tools should return ToolUsingDiagnostician, got %T", a)
	}
}

func TestNewDiagnosticianReflexionRule(t *testing.T) {
	a, err := NewDiagnosticianReflexion("", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*DiagnosticianAgent); !ok {
		t.Errorf("rule reflexion should return DiagnosticianAgent, got %T", a)
	}
}

func TestNewDiagnosticianReflexionOllama(t *testing.T) {
	a, err := NewDiagnosticianReflexion(ProviderLLMOllama, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	reflex, ok := a.(*ReflexionDiagnostician)
	if !ok {
		t.Fatalf("ollama reflexion should return ReflexionDiagnostician, got %T", a)
	}
	if reflex.MaxRounds != 1 {
		t.Errorf("default max rounds=%d, want 1", reflex.MaxRounds)
	}
}

func TestNewDiagnosticianReflexionCustomRounds(t *testing.T) {
	a, err := NewDiagnosticianReflexion(ProviderLLMOllama, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	reflex := a.(*ReflexionDiagnostician)
	if reflex.MaxRounds != 3 {
		t.Errorf("max rounds=%d, want 3", reflex.MaxRounds)
	}
}

func TestDrugInteractionTool(t *testing.T) {
	tool := DrugInteractionTool()
	if tool.Name() == "" {
		t.Error("DrugInteractionTool must have a name")
	}
}

func TestRiskCalculatorTool(t *testing.T) {
	tool := RiskCalculatorTool()
	if tool.Name() == "" {
		t.Error("RiskCalculatorTool must have a name")
	}
}

func TestFHIRQueryTool(t *testing.T) {
	tool := FHIRQueryTool()
	if tool.Name() == "" {
		t.Error("FHIRQueryTool must have a name")
	}
}

func TestEnvDefault(t *testing.T) {
	// Test fallback.
	os.Unsetenv("TEST_ENV_DEFAULT_KEY_XYZ")
	got := envDefault("TEST_ENV_DEFAULT_KEY_XYZ", "fallback")
	if got != "fallback" {
		t.Errorf("got %q, want fallback", got)
	}
	// Test env set.
	os.Setenv("TEST_ENV_DEFAULT_KEY_XYZ", "override")
	defer os.Unsetenv("TEST_ENV_DEFAULT_KEY_XYZ")
	got = envDefault("TEST_ENV_DEFAULT_KEY_XYZ", "fallback")
	if got != "override" {
		t.Errorf("got %q, want override", got)
	}
}

func TestCascadeDefaultModels(t *testing.T) {
	tests := []struct {
		provider string
		wantErr  bool
	}{
		{ProviderLLMAnthropic, false},
		{ProviderLLMOpenAI, false},
		{ProviderLLMOllama, false},
		{"bogus", true},
	}
	for _, tc := range tests {
		s, l, err := cascadeDefaultModels(tc.provider)
		if tc.wantErr {
			if err == nil {
				t.Errorf("cascadeDefaultModels(%q): want error", tc.provider)
			}
		} else {
			if err != nil {
				t.Errorf("cascadeDefaultModels(%q): %v", tc.provider, err)
			}
			if s == "" || l == "" {
				t.Errorf("cascadeDefaultModels(%q): empty models s=%q l=%q", tc.provider, s, l)
			}
		}
	}
}

func TestNewDiagnosticianCascadeRule(t *testing.T) {
	_, err := NewDiagnosticianCascade("", "", "", nil)
	if err == nil {
		t.Error("cascade with empty provider should error")
	}
	_, err = NewDiagnosticianCascade(ProviderRule, "", "", nil)
	if err == nil {
		t.Error("cascade with rule provider should error")
	}
}

func TestNewDiagnosticianCascadeOllama(t *testing.T) {
	a, err := NewDiagnosticianCascade(ProviderLLMOllama, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*CascadeDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

func TestNewDiagnosticianCascadeCustomModels(t *testing.T) {
	a, err := NewDiagnosticianCascade(ProviderLLMOllama, "custom-small", "custom-large", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*CascadeDiagnostician); !ok {
		t.Errorf("got %T", a)
	}
}

func TestNewDiagnosticianCascadeUnknownProvider(t *testing.T) {
	_, err := NewDiagnosticianCascade("bogus", "", "", nil)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestNewQuestionerRule(t *testing.T) {
	a, err := NewQuestioner("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*QuestionerAgent); !ok {
		t.Errorf("got %T", a)
	}
}

func TestNewQuestionerOllama(t *testing.T) {
	a, err := NewQuestioner(ProviderLLMOllama)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMQuestioner); !ok {
		t.Errorf("got %T", a)
	}
}

func TestNewQuestionerUnknown(t *testing.T) {
	_, err := NewQuestioner("bogus")
	if err == nil {
		t.Error("expected error")
	}
}

func TestNewInboxTriageRule(t *testing.T) {
	q := tasks.NewQueue()
	a, err := NewInboxTriage("", q)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*InboxTriageAgent); !ok {
		t.Errorf("got %T", a)
	}
}

func TestNewInboxTriageOllama(t *testing.T) {
	q := tasks.NewQueue()
	a, err := NewInboxTriage(ProviderLLMOllama, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMInboxTriage); !ok {
		t.Errorf("got %T", a)
	}
}

func TestNewInboxTriageUnknown(t *testing.T) {
	q := tasks.NewQueue()
	_, err := NewInboxTriage("bogus", q)
	if err == nil {
		t.Error("expected error")
	}
}

func TestNewReasoningVerifierRule(t *testing.T) {
	a, err := NewReasoningVerifier("", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*ReasoningVerifierAgent); !ok {
		t.Errorf("got %T", a)
	}
}

func TestNewReasoningVerifierOllama(t *testing.T) {
	a, err := NewReasoningVerifier(ProviderLLMOllama, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(*LLMReasoningVerifier); !ok {
		t.Errorf("got %T", a)
	}
}

func TestNewReasoningVerifierUnknown(t *testing.T) {
	_, err := NewReasoningVerifier("bogus", false)
	if err == nil {
		t.Error("expected error")
	}
}

func TestLLMClientForProviderModel(t *testing.T) {
	// Ollama should always work (no API key needed).
	c, err := llmClientForProviderModel(ProviderLLMOllama, "llama3.2")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Error("client should not be nil")
	}

	// Custom model override.
	c2, err := llmClientForProviderModel(ProviderLLMOllama, "")
	if err != nil {
		t.Fatal(err)
	}
	if c2 == nil {
		t.Error("client should not be nil")
	}

	// Unknown provider.
	_, err = llmClientForProviderModel("bogus", "")
	if err == nil {
		t.Error("expected error for unknown provider")
	}

	// Anthropic missing key.
	os.Unsetenv("ANTHROPIC_API_KEY")
	_, err = llmClientForProviderModel(ProviderLLMAnthropic, "")
	if err == nil {
		t.Error("expected error when ANTHROPIC_API_KEY missing")
	}

	// OpenAI missing key.
	os.Unsetenv("OPENAI_API_KEY")
	_, err = llmClientForProviderModel(ProviderLLMOpenAI, "")
	if err == nil {
		t.Error("expected error when OPENAI_API_KEY missing")
	}
}

func TestLLMClientForProvider(t *testing.T) {
	c, err := llmClientForProvider(ProviderLLMOllama)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Error("client should not be nil")
	}
}

func TestDefaultEmbedderForProvider(t *testing.T) {
	tests := []struct {
		provider string
		wantNil  bool
		wantErr  bool
	}{
		{"", true, false},
		{ProviderRule, true, false},
		{ProviderLLMKronk, true, false},
		{"bogus", false, true},
	}
	for _, tc := range tests {
		e, err := DefaultEmbedderForProvider(tc.provider)
		if tc.wantErr {
			if err == nil {
				t.Errorf("DefaultEmbedderForProvider(%q): want error", tc.provider)
			}
		} else {
			if err != nil {
				t.Errorf("DefaultEmbedderForProvider(%q): %v", tc.provider, err)
			}
			if tc.wantNil && e != nil {
				t.Errorf("DefaultEmbedderForProvider(%q): want nil", tc.provider)
			}
		}
	}

	// Ollama embedder (no API key needed).
	e, err := DefaultEmbedderForProvider(ProviderLLMOllama)
	if err != nil {
		t.Fatal(err)
	}
	if e == nil {
		t.Error("ollama embedder should not be nil")
	}

	// OpenAI without key returns nil.
	os.Unsetenv("OPENAI_API_KEY")
	e, err = DefaultEmbedderForProvider(ProviderLLMOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if e != nil {
		t.Error("openai without key should return nil")
	}

	// Anthropic without VOYAGE_API_KEY returns nil.
	os.Unsetenv("VOYAGE_API_KEY")
	e, err = DefaultEmbedderForProvider(ProviderLLMAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	if e != nil {
		t.Error("anthropic without voyage key should return nil")
	}
}

func TestLoadDefaultKnowledgeNoPath(t *testing.T) {
	os.Unsetenv("BODHYANGA_KNOWLEDGE_CORPUS")
	store, err := LoadDefaultKnowledge(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if store != nil {
		t.Error("no path should return nil store")
	}
}

func TestLoadDefaultKnowledgeNilEmbedder(t *testing.T) {
	t.Setenv("BODHYANGA_KNOWLEDGE_CORPUS", "/some/path")
	_, err := LoadDefaultKnowledge(nil, nil)
	if err == nil {
		t.Error("nil embedder should error")
	}
}

func TestNewDiagnosticianKronk(t *testing.T) {
	// Kronk requires build tag, so this should error in the default build.
	_, err := NewDiagnostician(ProviderLLMKronk)
	// Either it succeeds (tag present) or errors (tag absent).
	// We just test that it doesn't panic.
	_ = err
}

func TestLLMClientForProviderModelKronk(t *testing.T) {
	_, err := llmClientForProviderModel(ProviderLLMKronk, "")
	// Either succeeds or errors depending on build tag.
	_ = err
}

func TestLLMClientForProviderModelAnthropicCustomModel(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")
	c, err := llmClientForProviderModel(ProviderLLMAnthropic, "claude-custom")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Error("client should not be nil")
	}
}

func TestLLMClientForProviderModelOpenAICustomModel(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "test-key")
	defer os.Unsetenv("OPENAI_API_KEY")
	c, err := llmClientForProviderModel(ProviderLLMOpenAI, "gpt-custom")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Error("client should not be nil")
	}
}
