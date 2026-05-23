package agents

import (
	"errors"
	"fmt"
	"os"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

// Provider names accepted by NewDiagnostician and the -diagnostician CLI flag.
const (
	ProviderRule         = "rule"
	ProviderLLMAnthropic = "llm-anthropic"
	ProviderLLMOpenAI    = "llm-openai"
	ProviderLLMOllama    = "llm-ollama"
)

// Default model identifiers per provider. Override with BODH_ANTHROPIC_MODEL,
// BODH_OPENAI_MODEL, or BODH_OLLAMA_MODEL env vars. The Ollama host defaults
// to llm.OllamaDefaultHost; override with OLLAMA_HOST.
const (
	DefaultAnthropicModel = "claude-sonnet-4-6"
	DefaultOpenAIModel    = "gpt-4o"
	DefaultOllamaModel    = "llama3.2"
)

// NewDiagnostician builds the diagnostician agent for the given provider.
// Empty provider or "rule" returns the deterministic rule-based agent. LLM
// providers read their API key from ANTHROPIC_API_KEY / OPENAI_API_KEY and
// fall back to the rule-based agent on any per-call failure.
func NewDiagnostician(provider string) (agent.Agent, error) {
	switch provider {
	case "", ProviderRule:
		return &DiagnosticianAgent{}, nil

	case ProviderLLMAnthropic:
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, errors.New("ANTHROPIC_API_KEY env var not set")
		}
		model := envDefault("BODH_ANTHROPIC_MODEL", DefaultAnthropicModel)
		return &LLMDiagnostician{
			Client:   llm.NewAnthropic(key, model),
			Fallback: &DiagnosticianAgent{},
		}, nil

	case ProviderLLMOpenAI:
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, errors.New("OPENAI_API_KEY env var not set")
		}
		model := envDefault("BODH_OPENAI_MODEL", DefaultOpenAIModel)
		return &LLMDiagnostician{
			Client:   llm.NewOpenAI(key, model),
			Fallback: &DiagnosticianAgent{},
		}, nil

	case ProviderLLMOllama:
		host := os.Getenv("OLLAMA_HOST")
		model := envDefault("BODH_OLLAMA_MODEL", DefaultOllamaModel)
		return &LLMDiagnostician{
			Client:   llm.NewOllama(host, model),
			Fallback: &DiagnosticianAgent{},
		}, nil

	default:
		return nil, fmt.Errorf("unknown diagnostician provider %q (expected rule|llm-anthropic|llm-openai|llm-ollama)", provider)
	}
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
