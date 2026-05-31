// Command websearchtools demonstrates giving a Bodh agent web search capability
// using the DuckDuckGo Instant Answers API (no API key required).
//
// # What it does
//
// The agent receives a question, calls web_search one or more times to gather
// current information, then summarises what it found. The tool loop runs at
// most 5 rounds.
//
// # Default provider: Ollama (no API key required)
//
//	ollama serve
//	ollama pull llama3.2
//	go run ./cmd/websearchtools/...
//
// # Switching providers
//
//	go run ./cmd/websearchtools/... -provider anthropic
//	ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/websearchtools/... -provider anthropic
//
//	go run ./cmd/websearchtools/... -provider openai
//	OPENAI_API_KEY=sk-... go run ./cmd/websearchtools/... -provider openai
//
// # Flags
//
//	-provider     LLM provider: ollama (default), anthropic, openai
//	-model        Model override (defaults to provider default)
//	-ollama-host  Ollama base URL (default http://localhost:11434)
//	-message      Question to answer (default: Go language features)
//	-verbose      Print raw tool calls and results
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/tools"
)

const (
	defaultMessage = "What are the latest Go programming language features?"
	maxToolRounds  = 5
)

func main() {
	providerFlag := flag.String("provider",
		envDefault("WEBSEARCH_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default, no API key), "anthropic", or "openai"`)
	modelFlag := flag.String("model",
		envDefault("WEBSEARCH_MODEL", ""),
		"Model override (default depends on provider)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	messageFlag := flag.String("message",
		envDefault("WEBSEARCH_MESSAGE", defaultMessage),
		"Question for the agent to answer using web search")
	verboseFlag := flag.Bool("verbose", false, "Print raw tool calls and results")
	flag.Parse()

	model := *modelFlag
	if model == "" {
		model = defaultModel(*providerFlag)
	}

	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "websearchtools: build client: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("websearchtools: provider=%s  model=%s\n", client.Provider(), client.Model())
	fmt.Printf("websearchtools: question: %s\n\n", *messageFlag)

	answer, err := runAgent(context.Background(), client, *messageFlag, *verboseFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "websearchtools: agent error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(answer)
}

// runAgent executes the web-search tool loop and returns the model's final answer.
func runAgent(ctx context.Context, client llm.Client, question string, verbose bool) (string, error) {
	registry := tools.NewRegistry(&tools.WebSearchTool{})

	system := `You are a helpful assistant with access to a web_search tool.
When asked a question, use the web_search tool to find current information, then
synthesise a clear, concise answer. Cite the URLs of the sources you used.`

	req := llm.Request{
		System: system,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: question},
		},
		Tools:       registry.LLMTools(),
		MaxTokens:   2048,
		Temperature: 0.2,
	}

	for round := 0; round < maxToolRounds; round++ {
		resp, err := client.Complete(ctx, req)
		if err != nil {
			return "", fmt.Errorf("llm call (round %d): %w", round+1, err)
		}

		// No tool calls — the model produced its final answer.
		if len(resp.ToolCalls) == 0 {
			return strings.TrimSpace(resp.Content), nil
		}

		if verbose {
			fmt.Printf("[round %d] model requested %d tool call(s)\n", round+1, len(resp.ToolCalls))
		}

		// Append the assistant's tool-call turn to the conversation.
		req.Messages = append(req.Messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call and collect results.
		var toolResults []llm.ToolResult
		for _, call := range resp.ToolCalls {
			if verbose {
				fmt.Printf("[tool call] %s(%s)\n", call.Name, string(call.Arguments))
			}
			result := invokeToolSafe(ctx, registry, call, verbose)
			toolResults = append(toolResults, llm.ToolResult{
				ToolUseID: call.ID,
				Content:   string(result),
			})
		}

		// Feed results back as a user turn.
		req.Messages = append(req.Messages, llm.Message{
			Role:        llm.RoleUser,
			ToolResults: toolResults,
		})
	}

	return "", fmt.Errorf("tool loop exceeded %d rounds without a final answer", maxToolRounds)
}

// invokeToolSafe executes a single tool call, returning a stub JSON error
// document on failure so the conversation can continue.
func invokeToolSafe(ctx context.Context, reg *tools.Registry, call llm.ToolCall, verbose bool) json.RawMessage {
	t, ok := reg.Get(call.Name)
	if !ok {
		errMsg := fmt.Sprintf(`{"error":"unknown tool %q"}`, call.Name)
		if verbose {
			fmt.Printf("[tool result] %s → %s\n", call.Name, errMsg)
		}
		return json.RawMessage(errMsg)
	}
	result, err := t.Invoke(ctx, call.Arguments)
	if err != nil {
		errBody, _ := json.Marshal(map[string]string{"error": err.Error(), "tool": call.Name})
		if verbose {
			fmt.Printf("[tool result] %s → error: %v\n", call.Name, err)
		}
		return errBody
	}
	if verbose {
		// Pretty-print first 300 chars of the result to avoid flooding the terminal.
		preview := string(result)
		if len(preview) > 300 {
			preview = preview[:300] + "…"
		}
		fmt.Printf("[tool result] %s → %s\n", call.Name, preview)
	}
	return result
}

// buildClient constructs an llm.Client for the given provider.
func buildClient(provider, model, ollamaHost string) (llm.Client, error) {
	switch provider {
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY must be set when provider=anthropic")
		}
		return llm.NewAnthropic(apiKey, model), nil
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY must be set when provider=openai")
		}
		return llm.NewOpenAI(apiKey, model), nil
	case "ollama":
		return llm.NewOllama(ollamaHost, model), nil
	default:
		return nil, fmt.Errorf("unknown provider %q — use ollama, anthropic, or openai", provider)
	}
}

// defaultModel returns the default model name for a provider.
func defaultModel(provider string) string {
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-6"
	case "openai":
		return "gpt-4o"
	default: // ollama
		return "llama3.2"
	}
}

// envDefault returns the env var value or the fallback string.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
