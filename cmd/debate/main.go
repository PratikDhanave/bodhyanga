// Command debate runs the "12-Multi-Agent-Debate" lesson: multiple specialized
// agents argue different perspectives on a question, then a neutral chair
// agent synthesises the best answer.
//
// # Default provider: Ollama (local, no API key required)
//
// Make sure Ollama is running and the model is pulled:
//
//	ollama serve
//	ollama pull qwen2.5:7b
//	go run ./cmd/debate/...
//
// # Switching to Anthropic
//
//	ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/debate/... -provider anthropic
//
// # Lesson exercises
//
// 1. Run with the default healthcare architecture question:
//
//	go run ./cmd/debate/...
//
// 2. Ask a different question:
//
//	go run ./cmd/debate/... -question "Should we adopt AI-assisted code review?"
//
// 3. Run verbose to see each debater's position:
//
//	go run ./cmd/debate/... -verbose
//
// 4. Add a new debater: call debate.RunDebateAgent with a custom []debate.Debater.
//
// # Environment variables
//
//	DEBATE_PROVIDER    — "ollama" (default) or "anthropic"
//	DEBATE_MODEL       — model name override
//	OLLAMA_HOST        — Ollama base URL (default: http://localhost:11434)
//	ANTHROPIC_API_KEY  — required when provider=anthropic
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/PratikDhanave/bodhyanga/pkg/agent/debate"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

func main() {
	providerFlag := flag.String("provider",
		envDefault("DEBATE_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default, no API key) or "anthropic"`)
	modelFlag := flag.String("model",
		envDefault("DEBATE_MODEL", ""),
		"Model name (default: qwen2.5:7b for ollama, claude-haiku-4-5 for anthropic)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	questionFlag := flag.String("question",
		"Should we use microservices or a monolith for a new healthcare platform?",
		"Question to debate")
	verboseFlag := flag.Bool("verbose", false, "Print each debater's position and confidence")
	flag.Parse()

	// Resolve model default per provider.
	model := *modelFlag
	if model == "" {
		switch *providerFlag {
		case "anthropic":
			model = "claude-haiku-4-5"
		default: // ollama
			model = debate.DefaultModel
		}
	}

	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		log.Fatalf("debate: %v", err)
	}

	if *verboseFlag {
		fmt.Printf("debate: provider=%s  model=%s\n", *providerFlag, model)
		fmt.Printf("debate: question=%q\n\n", *questionFlag)
	}

	// Build callbacks — always print positions in non-verbose mode; verbose
	// adds the full argument text.
	callbacks := debate.DebateCallbacks{
		OnPosition: func(debater, argument string) {
			if *verboseFlag {
				fmt.Printf("[%s] %s\n\n", debater, argument)
			} else {
				fmt.Printf("[%s argued their position]\n", debater)
			}
		},
		OnSynthesis: func(result string) {
			if *verboseFlag {
				fmt.Printf("\n[chair synthesis]\n%s\n\n", result)
			}
		},
	}

	// Use the default 3-debater panel: Pragmatist, Architect, Operator.
	debaters := debate.DefaultDebaters()

	fmt.Printf("Question: %s\n\n", *questionFlag)
	fmt.Printf("Running debate with %d agents...\n\n", len(debaters))

	result, err := debate.RunDebateAgent(context.Background(), client, *questionFlag, debaters, callbacks)
	if err != nil {
		log.Fatalf("debate: %v", err)
	}

	fmt.Println("--- Synthesis ---")
	fmt.Println(result.Synthesis)
	fmt.Printf("\nConfidence: %.0f%%\n", result.Confidence*100)
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
