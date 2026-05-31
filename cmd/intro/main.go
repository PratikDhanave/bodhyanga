// Command intro runs the "01-Intro-to-Agents" lesson: the simplest possible
// agent — a single LLM call with no tools, no loop, and no memory.
//
// # Default provider: Ollama (local, no API key required)
//
// Make sure Ollama is running and the model is pulled:
//
//	ollama serve
//	ollama pull qwen2.5:7b
//	go run ./cmd/intro/...
//
// # Switching to Anthropic
//
//	ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/intro/... -provider anthropic
//
// # Lesson exercises
//
// 1. Run the agent (default question):
//
//	go run ./cmd/intro/...
//
// 2. Change the message:
//
//	go run ./cmd/intro/... -message "What is the ReAct pattern?"
//
// 3. Change the model (exercise: compare responses):
//
//	go run ./cmd/intro/... -model llama3.2
//	go run ./cmd/intro/... -provider anthropic -model claude-haiku-4-5
//
// 4. Modify the system prompt: edit pkg/agent/intro/prompt.go and re-run.
//
// # Environment variables
//
//	INTRO_PROVIDER   — "ollama" (default) or "anthropic"
//	INTRO_MODEL      — model name override
//	OLLAMA_HOST      — Ollama base URL (default: http://localhost:11434)
//	ANTHROPIC_API_KEY — required when provider=anthropic
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/PratikDhanave/bodh/pkg/agent/intro"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

func main() {
	providerFlag := flag.String("provider",
		envDefault("INTRO_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default, no API key) or "anthropic"`)
	modelFlag := flag.String("model",
		envDefault("INTRO_MODEL", ""),
		"Model name (default: qwen2.5:7b for ollama, claude-haiku-4-5 for anthropic)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	messageFlag := flag.String("message",
		"What is an AI agent?",
		"Message to send to the agent")
	verboseFlag := flag.Bool("verbose", false, "Print provider/model info and timing")
	flag.Parse()

	// Resolve model default per provider — mirrors resolveModels in cmd/evals.
	model := *modelFlag
	if model == "" {
		switch *providerFlag {
		case "anthropic":
			model = "claude-haiku-4-5"
		default: // ollama
			model = intro.DefaultModel
		}
	}

	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		log.Fatalf("intro: %v", err)
	}

	if *verboseFlag {
		fmt.Printf("intro: provider=%s  model=%s\n", *providerFlag, model)
		fmt.Printf("intro: message=%q\n\n", *messageFlag)
	}

	// Run the simplest possible agent: one LLM call, no tools, no loop.
	// The callbacks below print a prefix before/after the response when
	// -verbose is set, showing the lifecycle hooks available to callers.
	callbacks := intro.AgentCallbacks{}
	if *verboseFlag {
		callbacks.OnStart = func(msg string) {
			fmt.Println("→ sending to LLM…")
		}
		callbacks.OnFinish = func(resp string) {
			fmt.Printf("\n[%d chars]\n", len(resp))
		}
	}

	text, err := intro.RunAgent(context.Background(), client, *messageFlag, nil, callbacks)
	if err != nil {
		log.Fatalf("intro: %v", err)
	}

	// Print the response — equivalent to console.log(text) in TypeScript.
	fmt.Println(text)
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
	case "ollama":
		return llm.NewOllama(ollamaHost, model), nil
	default:
		return nil, fmt.Errorf("unknown provider %q — use \"ollama\" or \"anthropic\"", provider)
	}
}

// envDefault returns the env var value or the fallback string.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
