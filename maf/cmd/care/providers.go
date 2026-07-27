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

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

const diagnosticianInstructions = "You are a diagnostic decision-support engine " +
	"in a research system. Always answer with the requested structured JSON."

// buildDiagnostician maps -diagnostician to a graph-node binding (a copy of
// cmd/slice's, without the RAG retriever — care runs the LLM engines ungrounded
// for now). rule returns nil so pipeline.New binds its default rule executor.
func buildDiagnostician(kind string, boundary *admit.Boundary, dxCfg agents.DiagnosticianConfig, guard agents.Guard) (*workflow.Executor, error) {
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
				openaioption.WithAPIKey("ollama"),
			),
			openaiprovider.AgentConfig{
				Config:       llmAgentConfig(boundary),
				Model:        cmp.Or(os.Getenv("BODH_OLLAMA_MODEL"), "llama3.2"),
				Instructions: diagnosticianInstructions,
			})
	default:
		return nil, fmt.Errorf("unknown -diagnostician %q (rule | llm-anthropic | llm-ollama)", kind)
	}
	return agents.NewLLMDiagnostician("diagnostician", llm, dxCfg, guard, nil), nil
}

func llmAgentConfig(boundary *admit.Boundary) agent.Config {
	cfg := agent.Config{ID: "diagnostician-llm"}
	if boundary != nil {
		cfg.Middlewares = []agent.Middleware{boundary.Middleware("diagnostician", admit.TypeWorkup)}
	}
	return cfg
}
