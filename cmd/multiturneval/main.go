// Command multiturneval is Lesson 05 — Multi-Turn Evals.
//
// It demonstrates how to evaluate an LLM agent across a scripted multi-turn
// conversation, where the agent must maintain context across turns.
//
// Two sample cases are included:
//
//  1. "Capital quiz": turn 1 asks about Japan's capital, turn 2 asks about
//     France's capital. The final response must contain "Paris".
//
//  2. "Math conversation": turn 1 asks for 2+2, turn 2 asks to multiply that
//     by 3. The final response must contain "12".
//
// Usage:
//
//	go run ./cmd/multiturneval/...
//	go run ./cmd/multiturneval/... -provider anthropic
//	go run ./cmd/multiturneval/... -provider ollama -model qwen2.5:7b -verbose
//
// The default provider is Ollama (no API key required).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent/multiturneval"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

func main() {
	providerFlag := flag.String("provider",
		envDefault("MULTITURNEVAL_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default) or "anthropic"`)
	modelFlag := flag.String("model",
		envDefault("MULTITURNEVAL_MODEL", ""),
		"Model to use (defaults to provider default)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL")
	verboseFlag := flag.Bool("verbose", false, "Print verbose output including full conversation")
	flag.Parse()

	// Resolve model default.
	model := *modelFlag
	if model == "" {
		switch *providerFlag {
		case "anthropic":
			model = "claude-haiku-4-5"
		default:
			model = multiturneval.DefaultModel
		}
	}

	// Build LLM client.
	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		log.Fatalf("multiturneval: build client: %v", err)
	}

	fmt.Printf("multiturneval: provider=%s  model=%s\n", *providerFlag, model)

	// Define multi-turn eval cases.
	cases := []multiturneval.MultiTurnCase{
		{
			Name:   "Capital quiz",
			System: "You are a helpful geography assistant. Answer concisely.",
			Turns: []multiturneval.ConversationTurn{
				{
					UserMessage: "What is the capital of Japan?",
					CheckFn: func(output string) string {
						lower := strings.ToLower(output)
						if strings.Contains(lower, "tokyo") {
							return ""
						}
						return "expected Tokyo in response"
					},
				},
				{
					UserMessage: "And what about France?",
				},
			},
			FinalEvaluators: map[string]func(string) float64{
				"contains_paris": func(output string) float64 {
					if strings.Contains(strings.ToLower(output), "paris") {
						return 1.0
					}
					return 0.0
				},
			},
		},
		{
			Name:   "Math conversation",
			System: "You are a helpful math assistant. Give concise numeric answers.",
			Turns: []multiturneval.ConversationTurn{
				{
					UserMessage: "What is 2+2?",
					CheckFn: func(output string) string {
						if strings.Contains(output, "4") {
							return ""
						}
						return "expected 4 in response"
					},
				},
				{
					UserMessage: "Multiply that by 3.",
				},
			},
			FinalEvaluators: map[string]func(string) float64{
				"contains_12": func(output string) float64 {
					if strings.Contains(output, "12") {
						return 1.0
					}
					return 0.0
				},
			},
		},
	}

	ctx := context.Background()

	if *verboseFlag {
		fmt.Printf("multiturneval: running %d cases\n", len(cases))
	}

	results, err := multiturneval.RunMultiTurnEval(ctx, cases, client)
	if err != nil {
		log.Fatalf("multiturneval: run: %v", err)
	}

	multiturneval.PrintMultiTurnReport(results)

	// Determine overall pass/fail.
	allPass := true
	for _, r := range results {
		if r.Err != nil {
			allPass = false
			continue
		}
		if len(r.Scores) == 0 {
			continue
		}
		var total float64
		for _, s := range r.Scores {
			total += s
		}
		avg := total / float64(len(r.Scores))
		if avg < 0.5 {
			allPass = false
		}
	}

	if !allPass {
		fmt.Fprintln(os.Stderr, "multiturneval: FAIL — one or more cases did not pass")
		os.Exit(1)
	}
	fmt.Println("multiturneval: PASS")
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
