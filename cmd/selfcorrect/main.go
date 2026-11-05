// Command selfcorrect runs the "13-Self-Correction" lesson: the reflexion
// pattern for general-purpose agents. The agent produces an initial answer,
// critiques it, and revises it until it meets a quality threshold or the
// maximum number of rounds is reached.
//
// # Default provider: Ollama (local, no API key required)
//
//	ollama serve
//	ollama pull qwen2.5:7b
//	go run ./cmd/selfcorrect/...
//
// # Switching to Anthropic
//
//	ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/selfcorrect/... -provider anthropic
//
// # Lesson exercises
//
// 1. Run with the default task:
//
//	go run ./cmd/selfcorrect/...
//
// 2. Provide a custom task:
//
//	go run ./cmd/selfcorrect/... -task "Explain the CAP theorem in one paragraph"
//
// 3. Lower the score threshold to see fewer revision rounds:
//
//	go run ./cmd/selfcorrect/... -threshold 0.6
//
// 4. Increase max rounds:
//
//	go run ./cmd/selfcorrect/... -max-rounds 5
//
// 5. Watch every step with verbose output:
//
//	go run ./cmd/selfcorrect/... -verbose
//
// # Environment variables
//
//	SELFCORRECT_PROVIDER  — "ollama" (default) or "anthropic" or "openai"
//	SELFCORRECT_MODEL     — model name override
//	OLLAMA_HOST           — Ollama base URL (default: http://localhost:11434)
//	ANTHROPIC_API_KEY     — required when provider=anthropic
//	OPENAI_API_KEY        — required when provider=openai
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/agent/selfcorrect"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

const defaultTask = "Write a Go function that reverses a string"

func main() {
	providerFlag := flag.String("provider",
		envDefault("SELFCORRECT_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default), "anthropic", or "openai"`)
	modelFlag := flag.String("model",
		envDefault("SELFCORRECT_MODEL", ""),
		"Model name (default: qwen2.5:7b for ollama, claude-sonnet-4-6 for anthropic, gpt-4o for openai)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	taskFlag := flag.String("task",
		defaultTask,
		"Task for the self-correcting agent to work on")
	maxRoundsFlag := flag.Int("max-rounds",
		selfcorrect.DefaultMaxRounds,
		"Maximum number of critique→revise rounds")
	thresholdFlag := flag.Float64("threshold",
		selfcorrect.DefaultScoreThreshold,
		"Quality score threshold at which the agent stops (0.0–1.0)")
	verboseFlag := flag.Bool("verbose", false, "Print each round's answer, critique, and revision")
	flag.Parse()

	model := *modelFlag
	if model == "" {
		switch *providerFlag {
		case "anthropic":
			model = "claude-sonnet-4-6"
		case "openai":
			model = "gpt-4o"
		default:
			model = "qwen2.5:7b"
		}
	}

	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		log.Fatalf("selfcorrect: %v", err)
	}

	if *verboseFlag {
		fmt.Printf("selfcorrect: provider=%s  model=%s\n", *providerFlag, model)
		fmt.Printf("selfcorrect: max-rounds=%d  threshold=%.2f\n\n", *maxRoundsFlag, *thresholdFlag)
		fmt.Printf("Task: %s\n%s\n\n", *taskFlag, strings.Repeat("─", 60))
	}

	callbacks := selfcorrect.SelfCorrectCallbacks{}
	if *verboseFlag {
		callbacks.OnAnswer = func(round int, answer string) {
			if round == 1 {
				fmt.Printf("[Round %d] Initial Answer:\n%s\n\n", round, answer)
			}
		}
		callbacks.OnCritique = func(round int, c selfcorrect.CritiqueResult) {
			fmt.Printf("[Round %d] Critique — score=%.2f  is_correct=%v\n", round, c.Score, c.IsCorrect)
			for i, issue := range c.Issues {
				fix := ""
				if i < len(c.Suggestions) {
					fix = "  → " + c.Suggestions[i]
				}
				fmt.Printf("  • %s%s\n", issue, fix)
			}
			fmt.Println()
		}
		callbacks.OnRevision = func(round int, revised string) {
			fmt.Printf("[Round %d] Revised Answer:\n%s\n\n", round, revised)
		}
		callbacks.OnDone = func(result selfcorrect.RevisionResult) {
			fmt.Printf("%s\n", strings.Repeat("─", 60))
			fmt.Printf("Done after %d round(s). Improved: %v\n", result.Rounds, result.Improved)
			fmt.Printf("Final score: %.2f\n\n", result.Critique.Score)
		}
	}

	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(),
		client,
		*taskFlag,
		*maxRoundsFlag,
		*thresholdFlag,
		callbacks,
	)
	if err != nil {
		log.Fatalf("selfcorrect: %v", err)
	}

	// Always print the final answer.
	fmt.Println(result.RevisedAnswer)
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
		return nil, fmt.Errorf("unknown provider %q — use \"ollama\", \"anthropic\", or \"openai\"", provider)
	}
}

// envDefault returns the env var value or the fallback string.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
