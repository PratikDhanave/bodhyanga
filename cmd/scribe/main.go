// Command scribe is the ambient clinical scribe CLI. It reads a clinician-
// dictated transcript (from -text flag or stdin) and outputs structured
// ScribeOutput JSON.
//
// # Usage
//
//	echo "Patient has BP 120/80, heart rate 72, takes metformin" | go run ./cmd/scribe/...
//	go run ./cmd/scribe/... -text "BP 140/90, allergic to penicillin"
//
// # With LLM extraction
//
//	go run ./cmd/scribe/... -provider anthropic -text "..."
//	go run ./cmd/scribe/... -provider ollama -model llama3.2 -text "..."
//
// # Environment variables
//
//	SCRIBE_PROVIDER      — "none" (default, rule-based only), "ollama", "anthropic", "openai"
//	SCRIBE_MODEL         — model name override
//	OLLAMA_HOST          — Ollama base URL (default: http://localhost:11434)
//	ANTHROPIC_API_KEY    — required when provider=anthropic
//	OPENAI_API_KEY       — required when provider=openai
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func main() {
	providerFlag := flag.String("provider",
		envDefault("SCRIBE_PROVIDER", "none"),
		`LLM provider: "none" (rule-based only), "ollama", "anthropic", "openai"`)
	modelFlag := flag.String("model",
		envDefault("SCRIBE_MODEL", ""),
		"Model name (provider-specific default if empty)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	textFlag := flag.String("text", "", "Transcript text (if empty, reads from stdin)")
	verboseFlag := flag.Bool("verbose", false, "Print provider/model info to stderr")
	flag.Parse()

	// Read transcript from -text flag or stdin.
	transcript := *textFlag
	if transcript == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("scribe: reading stdin: %v", err)
		}
		transcript = string(data)
	}
	if transcript == "" {
		log.Fatal("scribe: no transcript provided (use -text or pipe to stdin)")
	}

	// Build LLM client (or nil for rule-based only).
	var client llm.Client
	if *providerFlag != "none" {
		model := resolveModel(*providerFlag, *modelFlag)
		var err error
		client, err = buildClient(*providerFlag, model, *ollamaHostFlag)
		if err != nil {
			log.Fatalf("scribe: %v", err)
		}
		if *verboseFlag {
			fmt.Fprintf(os.Stderr, "scribe: provider=%s  model=%s\n", *providerFlag, model)
		}
	} else if *verboseFlag {
		fmt.Fprintf(os.Stderr, "scribe: mode=rule-based (no LLM)\n")
	}

	input := medical.ScribeInput{
		Transcript: transcript,
	}

	ctx := context.Background()
	out, err := medical.AutoScribe(ctx, client, input)
	if err != nil {
		log.Fatalf("scribe: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatalf("scribe: encoding output: %v", err)
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
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY must be set when provider=openai")
		}
		return llm.NewOpenAI(apiKey, model), nil
	case "ollama":
		return llm.NewOllama(ollamaHost, model), nil
	default:
		return nil, fmt.Errorf("unknown provider %q — use \"none\", \"ollama\", \"anthropic\", or \"openai\"", provider)
	}
}

// resolveModel picks a default model for the provider if none was specified.
func resolveModel(provider, model string) string {
	if model != "" {
		return model
	}
	switch provider {
	case "anthropic":
		return envDefault("BODHYANGA_ANTHROPIC_MODEL", "claude-sonnet-4-6")
	case "openai":
		return envDefault("BODHYANGA_OPENAI_MODEL", "gpt-4o")
	case "ollama":
		return envDefault("BODHYANGA_OLLAMA_MODEL", "llama3.2")
	default:
		return "unknown"
	}
}

// envDefault returns the env var value or the fallback string.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
