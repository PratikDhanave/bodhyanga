package main

import (
	"cmp"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/provider/anthropicprovider"
	"github.com/microsoft/agent-framework-go/provider/openaiprovider"
	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"

	"github.com/PratikDhanave/bodh/pkg/embeddings"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

const diagnosticianInstructions = "You are a diagnostic decision-support engine " +
	"in a research system. Always answer with the requested structured JSON."

// buildDiagnostician maps -diagnostician to a graph-node binding. The graph
// itself never changes (spec US4): rule returns nil so pipeline.New binds its
// default rule executor; LLM engines return the same-protocol executor.
func buildDiagnostician(kind string, boundary *admit.Boundary, dxCfg agents.DiagnosticianConfig, guard agents.Guard, retriever *agents.Retriever) (*workflow.Executor, error) {
	var llm *agent.Agent
	switch kind {
	case "rule":
		return nil, nil
	case "llm-anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("-diagnostician=llm-anthropic requires ANTHROPIC_API_KEY")
		}
		llm = anthropicprovider.NewAgent(
			anthropic.NewClient(anthropicoption.WithAPIKey(key)),
			anthropicprovider.AgentConfig{
				Config:       llmAgentConfig(boundary),
				Model:        cmp.Or(os.Getenv("BODH_ANTHROPIC_MODEL"), "claude-sonnet-4-6"),
				Instructions: diagnosticianInstructions,
			})
	case "llm-ollama":
		host := cmp.Or(os.Getenv("OLLAMA_HOST"), "http://localhost:11434")
		llm = openaiprovider.NewChatCompletionsAgent(
			openai.NewClient(
				openaioption.WithBaseURL(host+"/v1"),
				openaioption.WithAPIKey("ollama"), // Ollama ignores the key but the SDK requires one
			),
			openaiprovider.AgentConfig{
				Config:       llmAgentConfig(boundary),
				Model:        cmp.Or(os.Getenv("BODH_OLLAMA_MODEL"), "llama3.2"),
				Instructions: diagnosticianInstructions,
			})
	default:
		return nil, fmt.Errorf("unknown -diagnostician %q (rule | llm-anthropic | llm-ollama)", kind)
	}
	return agents.NewLLMDiagnostician("diagnostician", llm, dxCfg, guard, retriever), nil
}

// buildEmbedder maps -embedder to a RAG embedding backend. mock is the
// deterministic offline default (no network, no creds); ollama uses a local
// server. The automated suite never selects a networked backend (spec FR-010).
func buildEmbedder(kind string) (embeddings.Embedder, error) {
	switch kind {
	case "mock":
		return &embeddings.Mock{}, nil
	case "ollama":
		host := cmp.Or(os.Getenv("OLLAMA_HOST"), "http://localhost:11434")
		model := cmp.Or(os.Getenv("BODH_OLLAMA_EMBED_MODEL"), "nomic-embed-text")
		return embeddings.NewOllama(host, model), nil
	default:
		return nil, fmt.Errorf("unknown -embedder %q (mock | ollama)", kind)
	}
}

// llmAgentConfig gives every LLM engine the same identity and defense-in-depth
// admission middleware on the provider call itself.
func llmAgentConfig(boundary *admit.Boundary) agent.Config {
	cfg := agent.Config{ID: "diagnostician-llm"}
	if boundary != nil {
		cfg.Middlewares = []agent.Middleware{boundary.Middleware("diagnostician", admit.TypeWorkup)}
	}
	return cfg
}
