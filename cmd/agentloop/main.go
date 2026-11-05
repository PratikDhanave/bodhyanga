// cmd/agentloop — Lesson 03: Agent Loop.
//
// Runs a full tool-calling loop: the model may call tools, results are fed
// back, and the loop continues until a final text response is produced.
//
// Usage:
//
//	go run ./cmd/agentloop -message "What is the weather in London and what time is it?"
//	go run ./cmd/agentloop -provider anthropic -message "What time is it in UTC?"
//
// Environment variables (fallbacks for flags):
//
//	AGENTLOOP_PROVIDER   ollama | anthropic  (default: ollama)
//	AGENTLOOP_MODEL      model identifier    (default: provider default)
//	OLLAMA_HOST          Ollama base URL     (default: http://localhost:11434)
//	ANTHROPIC_API_KEY    required when provider=anthropic
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/PratikDhanave/bodhyanga/pkg/agent/agentloop"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

func main() {
	// --- flags ---------------------------------------------------------------

	provider := flag.String("provider", envOr("AGENTLOOP_PROVIDER", "ollama"),
		"LLM provider: ollama | anthropic")
	model := flag.String("model", envOr("AGENTLOOP_MODEL", ""),
		"Model identifier (empty = provider default)")
	ollamaHost := flag.String("ollama-host", envOr("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL")
	message := flag.String("message", "What is the weather in London and what time is it?",
		"User message to send to the agent")
	verbose := flag.Bool("verbose", false,
		"Print tool calls and results as they happen")
	maxIter := flag.Int("max-iterations", agentloop.MaxIterations,
		"Maximum number of agent loop iterations")

	flag.Parse()

	// --- build client --------------------------------------------------------

	client, err := buildClient(*provider, *model, *ollamaHost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("provider: %s   model: %s\n", client.Provider(), client.Model())
	fmt.Printf("message:  %s\n\n", *message)

	// --- callbacks -----------------------------------------------------------

	cbs := &agentloop.AgentCallbacks{
		OnStart: func(userMessage string) {
			if *verbose {
				fmt.Printf("[start] user: %s\n", userMessage)
			}
		},
		OnToolCall: func(name string, args json.RawMessage) {
			if *verbose {
				fmt.Printf("[tool-call]   %s  args=%s\n", name, string(args))
			} else {
				fmt.Printf("calling tool: %s\n", name)
			}
		},
		OnToolResult: func(name string, result string) {
			if *verbose {
				fmt.Printf("[tool-result] %s  => %s\n", name, result)
			} else {
				fmt.Printf("tool result:  %s\n", result)
			}
		},
		OnFinish: func(response string) {
			// printed below after RunAgent returns
		},
	}

	// --- run -----------------------------------------------------------------

	response, err := agentloop.RunAgent(context.Background(), client, *message, *maxIter, cbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nfinal response:\n%s\n", response)
}

// buildClient constructs the appropriate llm.Client for the given provider.
func buildClient(provider, model, ollamaHost string) (llm.Client, error) {
	switch provider {
	case "ollama", "":
		m := model
		if m == "" {
			m = agentloop.DefaultModel
		}
		return llm.NewOllama(ollamaHost, m), nil

	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is required for provider=anthropic")
		}
		m := model
		if m == "" {
			m = envOr("BODHYANGA_ANTHROPIC_MODEL", "claude-sonnet-4-6")
		}
		return llm.NewAnthropic(apiKey, m), nil

	default:
		return nil, fmt.Errorf("unknown provider %q (supported: ollama, anthropic)", provider)
	}
}

// envOr returns the environment variable value or the fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
