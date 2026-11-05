// cmd/filesystemtools is Lesson 06 — File System Tools.
//
// It demonstrates giving an LLM agent access to read_file, write_file, and
// list_directory tools. The agent is asked to read hello.txt from a temporary
// directory and list all files in that directory.
//
// Usage:
//
//	go run ./cmd/filesystemtools
//	go run ./cmd/filesystemtools -provider anthropic
//	go run ./cmd/filesystemtools -provider openai
//	go run ./cmd/filesystemtools -verbose
//
// Provider selection:
//
//	-provider ollama      (default) local Ollama, no API key required
//	-provider anthropic   requires ANTHROPIC_API_KEY
//	-provider openai      requires OPENAI_API_KEY
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/tools"
)

func main() {
	provider := flag.String("provider", "ollama", "LLM provider: ollama | anthropic | openai")
	model := flag.String("model", "", "model override (empty = provider default)")
	ollamaHost := flag.String("ollama-host", llm.OllamaDefaultHost, "Ollama base URL")
	verbose := flag.Bool("verbose", false, "print every tool call and result")
	flag.Parse()

	ctx := context.Background()

	// --- Build the LLM client ------------------------------------------------
	client := buildClient(*provider, *model, *ollamaHost)
	fmt.Printf("[filesystemtools] provider=%s model=%s\n", client.Provider(), client.Model())

	// --- Set up a temporary sandbox directory --------------------------------
	sandboxDir, err := os.MkdirTemp("", "bodhyanga-lesson06-*")
	if err != nil {
		fatalf("create sandbox: %v", err)
	}
	defer os.RemoveAll(sandboxDir)

	// Populate a few sample files.
	sampleFiles := map[string]string{
		"hello.txt": "Hello from Bodhyanga Lesson 06! This is the file system tools lesson.",
		"notes.txt": "Agent can read and write files safely.\nPaths are restricted to relative paths only.",
		"data.json": `{"lesson":6,"topic":"file system tools","status":"complete"}`,
	}
	for name, content := range sampleFiles {
		if err := os.WriteFile(filepath.Join(sandboxDir, name), []byte(content), 0o644); err != nil {
			fatalf("write sample file %s: %v", name, err)
		}
	}

	// Change working directory into the sandbox so relative paths resolve.
	origDir, err := os.Getwd()
	if err != nil {
		fatalf("getwd: %v", err)
	}
	if err := os.Chdir(sandboxDir); err != nil {
		fatalf("chdir sandbox: %v", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck

	fmt.Printf("[filesystemtools] sandbox=%s (contains %d sample files)\n", sandboxDir, len(sampleFiles))

	// --- Build the tool registry ---------------------------------------------
	reg := tools.NewRegistry(
		tools.ReadFileTool{},
		tools.WriteFileTool{},
		tools.ListDirectoryTool{},
	)

	// --- System prompt -------------------------------------------------------
	system := `You are a helpful assistant with access to file system tools.
When asked about files, use the available tools to read their contents and list directories.
Always use the tools rather than guessing file contents.`

	// --- Initial user message ------------------------------------------------
	userMessage := `Read the file named hello.txt and tell me what it says. Then list all files in the current directory (".") and tell me what files are available.`

	fmt.Printf("[filesystemtools] question: %s\n\n", userMessage)

	// --- Agentic tool loop ---------------------------------------------------
	const maxRounds = 6

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: userMessage},
	}

	req := llm.Request{
		System:    system,
		Messages:  messages,
		MaxTokens: 1024,
		Tools:     reg.LLMTools(),
	}

	var finalAnswer string

	for round := 0; round < maxRounds; round++ {
		resp, err := client.Complete(ctx, req)
		if err != nil {
			fatalf("llm complete (round %d): %v", round+1, err)
		}

		if *verbose {
			fmt.Printf("[round %d] stop_reason=%s content_len=%d tool_calls=%d\n",
				round+1, resp.StopReason, len(resp.Content), len(resp.ToolCalls))
		}

		// No tool calls: the model produced its final answer.
		if len(resp.ToolCalls) == 0 {
			finalAnswer = resp.Content
			break
		}

		// Append the assistant's tool-call turn.
		assistantMsg := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		req.Messages = append(req.Messages, assistantMsg)

		// Execute each tool call and collect results.
		var toolResults []llm.ToolResult
		for _, call := range resp.ToolCalls {
			if *verbose {
				fmt.Printf("  [tool call] %s(%s)\n", call.Name, string(call.Arguments))
			}
			result := invokeRegistryTool(ctx, reg, call, *verbose)
			toolResults = append(toolResults, llm.ToolResult{
				ToolUseID: call.ID,
				Content:   string(result),
			})
		}

		// Append the user turn with all tool results.
		req.Messages = append(req.Messages, llm.Message{
			Role:        llm.RoleUser,
			ToolResults: toolResults,
		})
	}

	if finalAnswer == "" {
		fatalf("agent did not produce a final answer within %d rounds", maxRounds)
	}

	fmt.Println("\n--- Agent response ---")
	fmt.Println(finalAnswer)
}

// invokeRegistryTool runs one tool call against the registry and returns
// the JSON result. Unknown tools and errors are returned as JSON error objects
// so the agent loop always gets a parseable result.
func invokeRegistryTool(ctx context.Context, reg *tools.Registry, call llm.ToolCall, verbose bool) json.RawMessage {
	t, ok := reg.Get(call.Name)
	if !ok {
		if verbose {
			fmt.Printf("  [tool result] unknown tool %q\n", call.Name)
		}
		return json.RawMessage(`{"error":"unknown tool"}`)
	}
	res, err := t.Invoke(ctx, call.Arguments)
	if err != nil {
		if verbose {
			fmt.Printf("  [tool result] %s error: %v\n", call.Name, err)
		}
		errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
		return errBody
	}
	if verbose {
		// Print a compact preview (first 200 chars) of the result.
		preview := string(res)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		fmt.Printf("  [tool result] %s -> %s\n", call.Name, preview)
	}
	return res
}

// buildClient constructs an llm.Client for the requested provider.
func buildClient(provider, model, ollamaHost string) llm.Client {
	switch strings.ToLower(provider) {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			fatalf("ANTHROPIC_API_KEY is not set")
		}
		m := model
		if m == "" {
			m = envOr("BODHYANGA_ANTHROPIC_MODEL", "claude-sonnet-4-6")
		}
		return llm.NewAnthropic(key, m)

	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			fatalf("OPENAI_API_KEY is not set")
		}
		m := model
		if m == "" {
			m = envOr("BODHYANGA_OPENAI_MODEL", "gpt-4o")
		}
		return llm.NewOpenAI(key, m)

	default: // ollama
		m := model
		if m == "" {
			m = envOr("BODHYANGA_OLLAMA_MODEL", "llama3.2")
		}
		return llm.NewOllama(ollamaHost, m)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "filesystemtools: "+format+"\n", args...)
	os.Exit(1)
}
