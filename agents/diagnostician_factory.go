package agents

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/embeddings"
	"github.com/PratikDhanave/bodh/pkg/knowledge"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/tasks"
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

// Default embedding model identifiers used by DefaultEmbedderForProvider and
// LoadDefaultKnowledge. Override with BODH_OPENAI_EMBED_MODEL /
// BODH_OLLAMA_EMBED_MODEL / BODH_VOYAGE_EMBED_MODEL.
const (
	DefaultOpenAIEmbedModel = "text-embedding-3-small"
	DefaultOllamaEmbedModel = "nomic-embed-text"
	DefaultVoyageEmbedModel = "voyage-3"
)

// NewDiagnostician builds the diagnostician agent for the given provider.
// Empty provider or "rule" returns the deterministic rule-based agent. LLM
// providers read their API key from ANTHROPIC_API_KEY / OPENAI_API_KEY and
// fall back to the rule-based agent on any per-call failure.
func NewDiagnostician(provider string) (agent.Agent, error) {
	return buildDiagnostician(provider, nil)
}

// NewDiagnosticianWithKnowledge builds the same diagnostician as
// NewDiagnostician but additionally wires the supplied knowledge.Store onto
// the LLM path so the agent runs Retrieval-Augmented Generation. The rule
// provider ignores the store. A nil store behaves identically to
// NewDiagnostician.
func NewDiagnosticianWithKnowledge(provider string, store knowledge.Store) (agent.Agent, error) {
	return buildDiagnostician(provider, store)
}

// NewDiagnosticianDebate builds the multi-expert debate diagnostician for
// the given provider. The rule provider does not support debate and returns
// the rule-based agent. The supplied store, when non-nil, enables RAG on
// top of every specialist call and the chair synthesis. A nil/empty
// specialists slice uses DefaultSpecialists().
func NewDiagnosticianDebate(provider string, store knowledge.Store, specialists []Specialist) (agent.Agent, error) {
	if provider == "" || provider == ProviderRule {
		return &DiagnosticianAgent{}, nil
	}
	base, err := buildDiagnostician(provider, store)
	if err != nil {
		return nil, err
	}
	llmBase, ok := base.(*LLMDiagnostician)
	if !ok {
		return base, nil
	}
	if len(specialists) == 0 {
		specialists = DefaultSpecialists()
	}
	return &DebateDiagnostician{Base: llmBase, Specialists: specialists}, nil
}

// NewDiagnosticianVision builds the vision-augmented diagnostician for the
// given provider. The rule provider does not support vision and returns the
// rule-based agent. The supplied store, when non-nil, enables RAG on top of
// the vision path.
func NewDiagnosticianVision(provider string, store knowledge.Store) (agent.Agent, error) {
	if provider == "" || provider == ProviderRule {
		return &DiagnosticianAgent{}, nil
	}
	base, err := buildDiagnostician(provider, store)
	if err != nil {
		return nil, err
	}
	llmBase, ok := base.(*LLMDiagnostician)
	if !ok {
		return base, nil
	}
	return &LLMDiagnosticianVision{Base: llmBase}, nil
}

// NewDiagnosticianReflexion builds the Reflexion / Chain-of-Verification
// diagnostician for the given provider. The rule provider has no LLM to
// critique with and returns the rule-based agent. The supplied store, when
// non-nil, enables RAG on the initial draft (the same retrieved citations are
// then threaded into each critique round). maxRounds <= 0 defaults to 1.
func NewDiagnosticianReflexion(provider string, store knowledge.Store, maxRounds int) (agent.Agent, error) {
	if provider == "" || provider == ProviderRule {
		return &DiagnosticianAgent{}, nil
	}
	base, err := buildDiagnostician(provider, store)
	if err != nil {
		return nil, err
	}
	llmBase, ok := base.(*LLMDiagnostician)
	if !ok {
		return base, nil
	}
	if maxRounds <= 0 {
		maxRounds = 1
	}
	return &ReflexionDiagnostician{Base: llmBase, MaxRounds: maxRounds}, nil
}

func buildDiagnostician(provider string, store knowledge.Store) (agent.Agent, error) {
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
			Client:    llm.NewAnthropic(key, model),
			Fallback:  &DiagnosticianAgent{},
			Knowledge: store,
		}, nil

	case ProviderLLMOpenAI:
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, errors.New("OPENAI_API_KEY env var not set")
		}
		model := envDefault("BODH_OPENAI_MODEL", DefaultOpenAIModel)
		return &LLMDiagnostician{
			Client:    llm.NewOpenAI(key, model),
			Fallback:  &DiagnosticianAgent{},
			Knowledge: store,
		}, nil

	case ProviderLLMOllama:
		host := os.Getenv("OLLAMA_HOST")
		model := envDefault("BODH_OLLAMA_MODEL", DefaultOllamaModel)
		return &LLMDiagnostician{
			Client:    llm.NewOllama(host, model),
			Fallback:  &DiagnosticianAgent{},
			Knowledge: store,
		}, nil

	default:
		return nil, fmt.Errorf("unknown diagnostician provider %q (expected rule|llm-anthropic|llm-openai|llm-ollama)", provider)
	}
}

// LoadDefaultKnowledge reads BODH_KNOWLEDGE_CORPUS, loads the JSON corpus at
// that path, indexes it with the supplied embedder into a new in-memory Store,
// and returns it. When the env var is unset (returns nil, nil) callers proceed
// without RAG. Embedding / parse errors are surfaced.
func LoadDefaultKnowledge(ctx context.Context, embedder embeddings.Embedder) (knowledge.Store, error) {
	path := os.Getenv("BODH_KNOWLEDGE_CORPUS")
	if path == "" {
		return nil, nil
	}
	if embedder == nil {
		return nil, errors.New("LoadDefaultKnowledge: nil embedder")
	}
	docs, err := knowledge.LoadCorpus(path)
	if err != nil {
		return nil, err
	}
	store := knowledge.NewInMemoryStore(embedder)
	if err := store.Index(ctx, docs); err != nil {
		return nil, err
	}
	return store, nil
}

// DefaultEmbedderForProvider returns a sensible embedder paired with the
// supplied diagnostician provider, reading the same env vars the LLM client
// uses (OPENAI_API_KEY, OLLAMA_HOST, VOYAGE_API_KEY). Returns (nil, nil) when
// no compatible credential is available; the caller can then log and skip
// RAG. Errors are returned only for hard misconfigurations.
func DefaultEmbedderForProvider(provider string) (embeddings.Embedder, error) {
	switch provider {
	case "", ProviderRule:
		return nil, nil
	case ProviderLLMOpenAI:
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, nil
		}
		return embeddings.NewOpenAI(key, envDefault("BODH_OPENAI_EMBED_MODEL", DefaultOpenAIEmbedModel)), nil
	case ProviderLLMOllama:
		host := os.Getenv("OLLAMA_HOST")
		return embeddings.NewOllama(host, envDefault("BODH_OLLAMA_EMBED_MODEL", DefaultOllamaEmbedModel)), nil
	case ProviderLLMAnthropic:
		// Anthropic does not ship an embedding model; use Voyage when configured.
		key := os.Getenv("VOYAGE_API_KEY")
		if key == "" {
			return nil, nil
		}
		return embeddings.NewVoyage(key, envDefault("BODH_VOYAGE_EMBED_MODEL", DefaultVoyageEmbedModel)), nil
	default:
		return nil, fmt.Errorf("unknown diagnostician provider %q", provider)
	}
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// llmClientForProvider returns an llm.Client for the given LLM provider
// constant (ProviderLLMAnthropic / ProviderLLMOpenAI / ProviderLLMOllama),
// reading API keys and model overrides from the same env vars used by
// NewDiagnostician. Returns an error for provider="rule" or any unknown
// provider — call sites should special-case rule before calling this.
func llmClientForProvider(provider string) (llm.Client, error) {
	return llmClientForProviderModel(provider, "")
}

// llmClientForProviderModel is the model-override variant of
// llmClientForProvider. When model is empty the provider's env default is
// used; when set it wins over both the env override and the compile-time
// default. Used by the cascade factory so two tiers can share one provider
// but run on different model identifiers.
func llmClientForProviderModel(provider, model string) (llm.Client, error) {
	switch provider {
	case ProviderLLMAnthropic:
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, errors.New("ANTHROPIC_API_KEY env var not set")
		}
		if model == "" {
			model = envDefault("BODH_ANTHROPIC_MODEL", DefaultAnthropicModel)
		}
		return llm.NewAnthropic(key, model), nil
	case ProviderLLMOpenAI:
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, errors.New("OPENAI_API_KEY env var not set")
		}
		if model == "" {
			model = envDefault("BODH_OPENAI_MODEL", DefaultOpenAIModel)
		}
		return llm.NewOpenAI(key, model), nil
	case ProviderLLMOllama:
		host := os.Getenv("OLLAMA_HOST")
		if model == "" {
			model = envDefault("BODH_OLLAMA_MODEL", DefaultOllamaModel)
		}
		return llm.NewOllama(host, model), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider %q (expected llm-anthropic|llm-openai|llm-ollama)", provider)
	}
}

// Default per-provider model identifiers for the cascade tiers. The small
// tier is the cheap, fast model; the large tier is the more capable
// escalation target. Override either by passing explicit smallModel /
// largeModel arguments to NewDiagnosticianCascade.
const (
	DefaultAnthropicSmallModel = "claude-haiku-4-5"
	DefaultAnthropicLargeModel = "claude-sonnet-4-6"
	DefaultOpenAISmallModel    = "gpt-4o-mini"
	DefaultOpenAILargeModel    = "gpt-4o"
	DefaultOllamaSmallModel    = "llama3.2:3b"
	DefaultOllamaLargeModel    = "llama3.2"
)

// cascadeDefaultModels returns the (small, large) model defaults for a given
// LLM provider. Returns an error for the rule provider or any unknown name.
func cascadeDefaultModels(provider string) (string, string, error) {
	switch provider {
	case ProviderLLMAnthropic:
		return DefaultAnthropicSmallModel, DefaultAnthropicLargeModel, nil
	case ProviderLLMOpenAI:
		return DefaultOpenAISmallModel, DefaultOpenAILargeModel, nil
	case ProviderLLMOllama:
		return DefaultOllamaSmallModel, DefaultOllamaLargeModel, nil
	default:
		return "", "", fmt.Errorf("unknown cascade provider %q (expected llm-anthropic|llm-openai|llm-ollama)", provider)
	}
}

// NewDiagnosticianCascade builds a small→big CascadeDiagnostician on the
// supplied LLM provider. smallModel and largeModel override the per-provider
// defaults; pass "" for either to use the defaults documented on
// DefaultAnthropicSmallModel and friends:
//
//	anthropic: small=claude-haiku-4-5, large=claude-sonnet-4-6
//	openai:    small=gpt-4o-mini,      large=gpt-4o
//	ollama:    small=llama3.2:3b,      large=llama3.2
//
// Both tiers share the provider's API key (read from env), the shared
// knowledge.Store for RAG, and a DiagnosticianAgent as the per-tier
// rule-based fallback. The rule provider does not support cascading and is
// rejected with an error — callers should special-case rule first.
func NewDiagnosticianCascade(provider, smallModel, largeModel string, store knowledge.Store) (agent.Agent, error) {
	if provider == "" || provider == ProviderRule {
		return nil, fmt.Errorf("cascade diagnostician requires an LLM provider, got %q", provider)
	}
	smallDefault, largeDefault, err := cascadeDefaultModels(provider)
	if err != nil {
		return nil, err
	}
	if smallModel == "" {
		smallModel = smallDefault
	}
	if largeModel == "" {
		largeModel = largeDefault
	}
	smallClient, err := llmClientForProviderModel(provider, smallModel)
	if err != nil {
		return nil, err
	}
	largeClient, err := llmClientForProviderModel(provider, largeModel)
	if err != nil {
		return nil, err
	}
	return &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{
			Client:    smallClient,
			Fallback:  &DiagnosticianAgent{},
			Knowledge: store,
		},
		LargeTier: &LLMDiagnostician{
			Client:    largeClient,
			Fallback:  &DiagnosticianAgent{},
			Knowledge: store,
		},
	}, nil
}

// NewQuestioner builds the history & symptom questioner agent for the given
// provider. Empty provider or "rule" returns the deterministic
// QuestionerAgent. LLM providers wrap the rule-based agent as the
// per-call fallback, so the pipeline always advances to the diagnostic
// supervisor.
func NewQuestioner(provider string) (agent.Agent, error) {
	switch provider {
	case "", ProviderRule:
		return &QuestionerAgent{}, nil
	case ProviderLLMAnthropic, ProviderLLMOpenAI, ProviderLLMOllama:
		client, err := llmClientForProvider(provider)
		if err != nil {
			return nil, err
		}
		return &LLMQuestioner{
			Client:   client,
			Fallback: &QuestionerAgent{},
		}, nil
	default:
		return nil, fmt.Errorf("unknown questioner provider %q (expected rule|llm-anthropic|llm-openai|llm-ollama)", provider)
	}
}

// NewInboxTriage builds the clinical inbox triage agent for the given
// provider. Empty provider or "rule" returns the deterministic
// InboxTriageAgent. LLM providers wrap the rule-based agent as the per-call
// fallback. The same taskQueue is wired into both the LLM agent and its
// fallback so urgent / general messages always reach the nurse queue.
func NewInboxTriage(provider string, taskQueue *tasks.Queue) (agent.Agent, error) {
	switch provider {
	case "", ProviderRule:
		return &InboxTriageAgent{Tasks: taskQueue}, nil
	case ProviderLLMAnthropic, ProviderLLMOpenAI, ProviderLLMOllama:
		client, err := llmClientForProvider(provider)
		if err != nil {
			return nil, err
		}
		return &LLMInboxTriage{
			Client:   client,
			Fallback: &InboxTriageAgent{Tasks: taskQueue},
			Tasks:    taskQueue,
		}, nil
	default:
		return nil, fmt.Errorf("unknown inbox_triage provider %q (expected rule|llm-anthropic|llm-openai|llm-ollama)", provider)
	}
}

// NewReasoningVerifier builds the clinical reasoning verifier agent for the
// given provider. Empty provider or "rule" returns the deterministic
// ReasoningVerifierAgent. LLM providers wrap the rule-based agent as the
// per-call fallback. The hitl flag is passed through to both the LLM agent
// and its fallback so verified diagnoses route identically through
// human_review when sign-off is required.
func NewReasoningVerifier(provider string, hitl bool) (agent.Agent, error) {
	switch provider {
	case "", ProviderRule:
		return &ReasoningVerifierAgent{HITL: hitl}, nil
	case ProviderLLMAnthropic, ProviderLLMOpenAI, ProviderLLMOllama:
		client, err := llmClientForProvider(provider)
		if err != nil {
			return nil, err
		}
		return &LLMReasoningVerifier{
			Client:   client,
			Fallback: &ReasoningVerifierAgent{HITL: hitl},
			HITL:     hitl,
		}, nil
	default:
		return nil, fmt.Errorf("unknown reasoning_verifier provider %q (expected rule|llm-anthropic|llm-openai|llm-ollama)", provider)
	}
}
