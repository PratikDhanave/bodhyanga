// cmd/planning is Lesson 11 — Long-Horizon Planning.
//
// It demonstrates an agent that decomposes a complex goal into a plan,
// executes each step using tools, observes the results, and adapts the plan
// if needed (replanning). The core pattern is:
//
//	Plan → Execute step → Observe result → Update plan → Repeat
//
// Usage:
//
//	go run ./cmd/planning
//	go run ./cmd/planning -provider anthropic
//	go run ./cmd/planning -provider openai
//	go run ./cmd/planning -goal "count .go files and list the 3 largest"
//	go run ./cmd/planning -verbose
//
// Provider selection:
//
//	-provider ollama      (default) local Ollama, no API key required
//	-provider anthropic   requires ANTHROPIC_API_KEY
//	-provider openai      requires OPENAI_API_KEY
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/agent/planning"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/tools"
)

const defaultGoal = "Find all .go files in the current directory, count them, and list the largest 3"

func main() {
	provider := flag.String("provider", "ollama", "LLM provider: ollama | anthropic | openai")
	model := flag.String("model", "", "model override (empty = provider default)")
	ollamaHost := flag.String("ollama-host", llm.OllamaDefaultHost, "Ollama base URL")
	goal := flag.String("goal", defaultGoal, "planning goal")
	verbose := flag.Bool("verbose", false, "print plan steps, tool calls and results")
	maxSteps := flag.Int("max-steps", planning.DefaultMaxSteps, "maximum number of steps to execute")
	flag.Parse()

	ctx := context.Background()

	// Build the LLM client.
	client := buildClient(*provider, *model, *ollamaHost)
	fmt.Printf("[planning] provider=%s model=%s\n", client.Provider(), client.Model())
	fmt.Printf("[planning] goal: %s\n\n", *goal)

	// Build the tool registry — filesystem tools are a good fit for the
	// default goal (finding and inspecting .go files).
	reg := tools.NewRegistry(
		tools.ListDirectoryTool{},
		tools.ReadFileTool{},
	)

	// Wire verbose callbacks.
	cbs := planning.PlanningCallbacks{
		OnPlanCreated: func(p planning.Plan) {
			fmt.Printf("[plan] Created plan with %d steps for goal: %s\n", len(p.Steps), p.Goal)
			if *verbose {
				for i, s := range p.Steps {
					fmt.Printf("  step %d: [%s] %s → tool=%s args=%v\n", i+1, s.ID, s.Description, s.ToolName, s.ToolArgs)
				}
			}
			fmt.Println()
		},
		OnStepStart: func(s planning.Step, index int) {
			if *verbose {
				fmt.Printf("[step %d/%s] starting: %s (tool=%s)\n", index+1, s.ID, s.Description, s.ToolName)
			}
		},
		OnStepComplete: func(s planning.Step, index int, result string) {
			if *verbose {
				preview := result
				if len(preview) > 300 {
					preview = preview[:300] + "…"
				}
				switch s.Status {
				case "done":
					fmt.Printf("[step %d/%s] done: %s\n", index+1, s.ID, preview)
				case "failed":
					fmt.Printf("[step %d/%s] FAILED: %s\n", index+1, s.ID, s.Error)
				}
				fmt.Println()
			} else {
				// Minimal progress output even without -verbose.
				icon := "✓"
				if s.Status == "failed" {
					icon = "✗"
				}
				fmt.Printf("  %s step %d: %s\n", icon, index+1, s.Description)
			}
		},
		OnPlanComplete: func(result string) {
			fmt.Println()
			fmt.Println("--- Plan complete ---")
		},
		OnReplan: func(reason string, newPlan planning.Plan) {
			fmt.Printf("[replan] %s — revised plan now has %d remaining steps\n", reason, len(newPlan.Steps)-newPlan.Current)
			if *verbose {
				for i := newPlan.Current; i < len(newPlan.Steps); i++ {
					s := newPlan.Steps[i]
					fmt.Printf("  step %d: [%s] %s\n", i+1, s.ID, s.Description)
				}
			}
			fmt.Println()
		},
	}

	result, err := planning.RunPlanningAgent(ctx, client, *goal, reg, cbs, *maxSteps)
	if err != nil {
		fatalf("planning agent failed: %v", err)
	}

	fmt.Println("\n--- Final answer ---")
	fmt.Println(result)
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
	fmt.Fprintf(os.Stderr, "planning: "+format+"\n", args...)
	os.Exit(1)
}
