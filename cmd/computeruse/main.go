// Command computeruse runs the "10-Computer-Use" lesson: an agent that can
// control a browser/shell to complete tasks that require interacting with
// external systems (web pages, forms, CLIs).
//
// # What it does
//
// The agent is equipped with three computer-use tools:
//
//   - open_url       — fetches a web page and returns stripped text (≤4 KB)
//   - run_command    — runs a safe, read-only shell command (≤4 KB stdout)
//   - take_screenshot — headless stub (returns {"available":false})
//
// The agent loops until it has a final answer or MaxIterations is reached.
//
// # Default provider: Ollama (no API key required)
//
//	ollama serve
//	ollama pull llama3.2
//	go run ./cmd/computeruse/...
//
// # Switching providers
//
//	go run ./cmd/computeruse/... -provider anthropic
//	ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/computeruse/... -provider anthropic
//
//	go run ./cmd/computeruse/... -provider openai
//	OPENAI_API_KEY=sk-... go run ./cmd/computeruse/... -provider openai
//
// # Flags
//
//	-provider     LLM provider: ollama (default), anthropic, openai
//	-model        Model override (defaults to provider default)
//	-ollama-host  Ollama base URL (default http://localhost:11434)
//	-message      Task for the agent (default: find latest Go release)
//	-verbose      Print tool calls and their raw results
//
// # Environment variables
//
//	COMPUTERUSE_PROVIDER  — "ollama" (default), "anthropic", or "openai"
//	COMPUTERUSE_MODEL     — model name override
//	COMPUTERUSE_MESSAGE   — message override
//	OLLAMA_HOST           — Ollama base URL
//	ANTHROPIC_API_KEY     — required when provider=anthropic
//	OPENAI_API_KEY        — required when provider=openai
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/PratikDhanave/bodh/pkg/agent/computeruse"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

const defaultMessage = "Find the current Go version by visiting https://go.dev/dl/ and tell me the latest stable release"

func main() {
	providerFlag := flag.String("provider",
		envDefault("COMPUTERUSE_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default, no API key), "anthropic", or "openai"`)
	modelFlag := flag.String("model",
		envDefault("COMPUTERUSE_MODEL", ""),
		"Model override (default depends on provider)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	messageFlag := flag.String("message",
		envDefault("COMPUTERUSE_MESSAGE", defaultMessage),
		"Task for the agent")
	verboseFlag := flag.Bool("verbose", false, "Print tool calls and their raw results")
	flag.Parse()

	model := *modelFlag
	if model == "" {
		model = defaultModel(*providerFlag)
	}

	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "computeruse: build client: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("computeruse: provider=%s  model=%s\n", client.Provider(), client.Model())
	fmt.Printf("computeruse: task: %s\n\n", *messageFlag)

	cbs := &computeruse.AgentCallbacks{
		OnComputerAction: func(name string, args json.RawMessage) {
			if *verboseFlag {
				fmt.Printf("[computer action] %s(%s)\n", name, string(args))
			} else {
				fmt.Printf("[%s]\n", name)
			}
		},
		OnToolResult: func(name string, result json.RawMessage) {
			if *verboseFlag {
				preview := string(result)
				if len(preview) > 300 {
					preview = preview[:300] + "..."
				}
				fmt.Printf("[%s result] %s\n", name, preview)
			}
		},
	}
	if *verboseFlag {
		cbs.OnStart = func(msg string) {
			fmt.Printf("→ sending to LLM: %q\n\n", msg)
		}
		cbs.OnFinish = func(resp string) {
			fmt.Printf("\n[%d chars total]\n", len(resp))
		}
	}

	answer, err := computeruse.RunAgent(context.Background(), client, *messageFlag, 0, cbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "computeruse: agent error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(answer)
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
		return computeruse.DefaultModel
	}
}

// envDefault returns the env var value or the fallback string.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
