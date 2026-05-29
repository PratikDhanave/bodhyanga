// Command toolcalling runs the "02-Tool-Calling" lesson: an agent that
// advertises tools to the LLM and observes which tools the model selects.
//
// # Default provider: Ollama (local, no API key required)
//
// Make sure Ollama is running and the model is pulled:
//
//	ollama serve
//	ollama pull qwen2.5:7b
//	go run ./cmd/toolcalling/...
//
// # Switching to Anthropic
//
//	ANTHROPIC_API_KEY=sk-ant-... go run ./cmd/toolcalling/... -provider anthropic
//
// # Lesson exercises
//
// 1. Ask for the current time (should trigger getDateTime):
//
//	go run ./cmd/toolcalling/...
//
// 2. Ask something that doesn't need a tool (should reply with text):
//
//	go run ./cmd/toolcalling/... -message "What is 2 + 2?"
//
// 3. Compare models — does a smaller model still call the right tool?
//
//	go run ./cmd/toolcalling/... -model llama3.2
//	go run ./cmd/toolcalling/... -provider anthropic -model claude-haiku-4-5
//
// 4. Add a new tool: implement a localTool in pkg/agent/toolcalling/,
//    register it in the registry in tools.go, and re-run.
//
// # Environment variables
//
//	TOOLCALLING_PROVIDER  — "ollama" (default) or "anthropic"
//	TOOLCALLING_MODEL     — model name override
//	OLLAMA_HOST           — Ollama base URL (default: http://localhost:11434)
//	ANTHROPIC_API_KEY     — required when provider=anthropic
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/PratikDhanave/bodh/pkg/agent/toolcalling"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

func main() {
	providerFlag := flag.String("provider",
		envDefault("TOOLCALLING_PROVIDER", "ollama"),
		`LLM provider: "ollama" (default, no API key) or "anthropic"`)
	modelFlag := flag.String("model",
		envDefault("TOOLCALLING_MODEL", ""),
		"Model name (default: qwen2.5:7b for ollama, claude-haiku-4-5 for anthropic)")
	ollamaHostFlag := flag.String("ollama-host",
		envDefault("OLLAMA_HOST", llm.OllamaDefaultHost),
		"Ollama base URL (used when provider=ollama)")
	messageFlag := flag.String("message",
		"What time is it?",
		"Message to send to the agent")
	verboseFlag := flag.Bool("verbose", false, "Print provider/model info, tool calls, and response details")
	flag.Parse()

	// Resolve model default per provider.
	model := *modelFlag
	if model == "" {
		switch *providerFlag {
		case "anthropic":
			model = "claude-haiku-4-5"
		default: // ollama
			model = toolcalling.DefaultModel
		}
	}

	client, err := buildClient(*providerFlag, model, *ollamaHostFlag)
	if err != nil {
		log.Fatalf("toolcalling: %v", err)
	}

	if *verboseFlag {
		fmt.Printf("toolcalling: provider=%s  model=%s\n", *providerFlag, model)
		fmt.Printf("toolcalling: message=%q\n", *messageFlag)
		fmt.Printf("toolcalling: tools=%v\n\n", toolNames())
	}

	// Build callbacks: always log tool calls; verbose mode adds extra context.
	callbacks := toolcalling.AgentCallbacks{
		OnToolCall: func(name string, args json.RawMessage) {
			if *verboseFlag {
				fmt.Printf("→ tool call: %s(%s)\n", name, args)
			} else {
				fmt.Printf("[tool call: %s]\n", name)
			}
		},
	}
	if *verboseFlag {
		callbacks.OnStart = func(msg string) {
			fmt.Println("→ sending to LLM…")
		}
		callbacks.OnFinish = func(resp string) {
			fmt.Printf("\n[%d chars]\n", len(resp))
		}
	}

	text, err := toolcalling.RunAgent(context.Background(), client, *messageFlag, nil, callbacks)
	if err != nil {
		log.Fatalf("toolcalling: %v", err)
	}

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

// toolNames returns the names of all registered tools for display.
func toolNames() []string {
	tools := toolcalling.Tools()
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// envDefault returns the env var value or the fallback string.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
