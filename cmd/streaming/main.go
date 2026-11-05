// Command streaming demonstrates LLM token streaming in Bodhyanga.
//
// It sends a prompt to the configured provider and prints tokens to stdout as
// they arrive — instead of waiting for the complete response. This dramatically
// improves perceived latency for long outputs.
//
// If the provider does not implement the Streamer interface, the command falls
// back to a regular Complete call and prints the full response at once.
//
// # Usage
//
//	go run ./cmd/streaming/...
//	go run ./cmd/streaming/... -provider anthropic
//	go run ./cmd/streaming/... -provider openai -message "What is Go?"
//
// # Flags
//
//	-provider     LLM provider: ollama (default), anthropic, openai
//	-model        Override the default model for the provider
//	-ollama-host  Ollama base URL (default: http://localhost:11434)
//	-message      Prompt to send (default: history of Go in 3 paragraphs)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

func main() {
	provider := flag.String("provider", "ollama",
		`LLM provider: "ollama" (default), "anthropic", or "openai"`)
	model := flag.String("model", "",
		"Override the default model for the chosen provider")
	ollamaHost := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	message := flag.String("message",
		"Explain the history of Go in 3 paragraphs",
		"Prompt to send to the LLM")
	flag.Parse()

	resolvedModel := resolveModel(*provider, *model)

	client, err := buildClient(*provider, resolvedModel, *ollamaHost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "streaming: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("provider=%s  model=%s\n", client.Provider(), client.Model())
	fmt.Printf("prompt: %s\n\n", *message)

	req := llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: *message},
		},
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	ctx := context.Background()
	start := time.Now()

	streamer, ok := llm.AsStreamer(client)
	if ok {
		tokenCount, err := runStreaming(ctx, streamer, req)
		elapsed := time.Since(start)
		fmt.Printf("\n\n---\nelapsed: %s  tokens (approx): %d\n", elapsed.Round(time.Millisecond), tokenCount)
		if err != nil {
			fmt.Fprintf(os.Stderr, "streaming: error: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("[provider does not support streaming — falling back to Complete]")
		resp, err := client.Complete(ctx, req)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Fprintf(os.Stderr, "streaming: complete: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(resp.Content)
		fmt.Printf("\n\n---\nelapsed: %s  input_tokens: %d  output_tokens: %d\n",
			elapsed.Round(time.Millisecond), resp.InputTokens, resp.OutputTokens)
	}
}

// runStreaming drains the stream channel, printing each delta token to stdout.
// Returns the approximate token count (counted as whitespace-separated words
// as a simple proxy) and any error encountered.
func runStreaming(ctx context.Context, s llm.Streamer, req llm.Request) (int, error) {
	ch, err := s.Stream(ctx, req)
	if err != nil {
		return 0, err
	}

	var tokenCount int
	for chunk := range ch {
		if chunk.Err != nil {
			return tokenCount, chunk.Err
		}
		if chunk.Delta != "" {
			fmt.Print(chunk.Delta)
			// Approximate token count: count rune clusters as a proxy.
			tokenCount += utf8.RuneCountInString(strings.TrimSpace(chunk.Delta))
		}
		if chunk.Done {
			break
		}
	}
	// Drain any remaining chunks (e.g. if we broke early).
	for range ch {
	}
	return tokenCount, nil
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

// resolveModel returns the model name, falling back to provider-specific defaults.
func resolveModel(provider, model string) string {
	if model != "" {
		return model
	}
	switch provider {
	case "anthropic":
		if v := os.Getenv("BODHYANGA_ANTHROPIC_MODEL"); v != "" {
			return v
		}
		return "claude-sonnet-4-6"
	case "openai":
		if v := os.Getenv("BODHYANGA_OPENAI_MODEL"); v != "" {
			return v
		}
		return "gpt-4o"
	default: // ollama
		if v := os.Getenv("BODHYANGA_OLLAMA_MODEL"); v != "" {
			return v
		}
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
