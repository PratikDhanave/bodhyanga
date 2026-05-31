// Command singleturneval demonstrates Lesson 04 — Single-Turn Evals.
//
// # What it does
//
// Builds a small dataset of "capital city" knowledge questions, runs each
// question through an LLM, scores the output with two evaluators
// (exact_match and not_empty), and prints a results table.
//
// # How to run
//
//	# Against local Ollama (default, no API key needed):
//	ollama serve
//	ollama pull qwen2.5:7b
//	go run ./cmd/singleturneval/...
//
//	# With verbose output (prints each case's full LLM response):
//	go run ./cmd/singleturneval/... -verbose
//
//	# Switch to Anthropic:
//	ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/singleturneval/... -provider anthropic
//
// # Flags
//
//	-provider    LLM provider: "ollama" (default) or "anthropic"
//	-model       Model name (defaults per provider: qwen2.5:7b / claude-haiku-4-5)
//	-ollama-host Ollama base URL (default: http://localhost:11434)
//	-verbose     Print full LLM outputs (default: truncated)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/PratikDhanave/bodh/pkg/agent/singleturneval"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

func main() {
	providerFlag := flag.String("provider",
		envDefault("SINGLETURNEVAL_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default, no API key needed) or "anthropic"`)
	modelFlag := flag.String("model",
		envDefault("SINGLETURNEVAL_MODEL", ""),
		"Model name (default: qwen2.5:7b for ollama, claude-haiku-4-5 for anthropic)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	verboseFlag := flag.Bool("verbose", false,
		"Print full LLM output for each case (default: truncated in table)")
	flag.Parse()

	// Resolve model default per provider.
	model := *modelFlag
	if model == "" {
		switch *providerFlag {
		case "anthropic":
			model = "claude-haiku-4-5"
		default: // ollama
			model = singleturneval.DefaultModel
		}
	}

	// Build the LLM client.
	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		log.Fatalf("singleturneval: client: %v", err)
	}

	fmt.Printf("singleturneval: provider=%s  model=%s\n\n", *providerFlag, model)

	// Build the sample dataset — 3 capital-city questions.
	cases := buildDataset()
	fmt.Printf("singleturneval: loaded %d cases\n\n", len(cases))

	// Build the executor: calls the LLM with the system prompt + user input.
	executor := buildExecutor(client, *verboseFlag)

	// Define evaluators.
	evaluators := map[string]singleturneval.EvaluatorFunc{
		"exact_match": singleturneval.ExactMatchEvaluator,
		"not_empty":   singleturneval.NotEmptyEvaluator,
	}

	// Run the evaluation.
	ctx := context.Background()
	results, err := singleturneval.RunEval(ctx, cases, executor, evaluators)
	if err != nil {
		log.Fatalf("singleturneval: RunEval: %v", err)
	}

	// Print the results table.
	fmt.Println()
	singleturneval.PrintReport(results)
}

// buildDataset returns the evaluation dataset.
// Each case tests whether the agent can recall a capital city.
func buildDataset() []singleturneval.EvalCase {
	return []singleturneval.EvalCase{
		{
			Input:    "What is the capital of France?",
			Expected: map[string]any{"answer": "Paris"},
			Metadata: map[string]any{"category": "Europe", "difficulty": "easy"},
		},
		{
			Input:    "What is the capital of Japan?",
			Expected: map[string]any{"answer": "Tokyo"},
			Metadata: map[string]any{"category": "Asia", "difficulty": "easy"},
		},
		{
			Input:    "What is the capital of Brazil?",
			Expected: map[string]any{"answer": "Brasilia"},
			Metadata: map[string]any{"category": "South America", "difficulty": "medium"},
		},
	}
}

// buildExecutor returns an executor function that calls the LLM with the
// singleturneval.SystemPrompt and the given user input, returning the
// model's text response.
func buildExecutor(client llm.Client, verbose bool) func(ctx context.Context, input string) (string, error) {
	return func(ctx context.Context, input string) (string, error) {
		req := llm.Request{
			System: singleturneval.SystemPrompt,
			Messages: []llm.Message{
				{Role: llm.RoleUser, Content: input},
			},
			MaxTokens:   256,
			Temperature: 0.0, // deterministic for evals
		}

		resp, err := client.Complete(ctx, req)
		if err != nil {
			return "", fmt.Errorf("llm complete: %w", err)
		}

		if verbose {
			fmt.Printf("[llm] input: %q\n[llm] output: %q\n\n", input, resp.Content)
		}

		return resp.Content, nil
	}
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
