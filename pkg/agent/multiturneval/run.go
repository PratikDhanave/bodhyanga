package multiturneval

import (
	"context"
	"fmt"
	"text/tabwriter"
	"os"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// DefaultModel is the default model used when none is specified.
const DefaultModel = "qwen2.5:7b"

// SystemPrompt is the default system prompt for multi-turn eval conversations.
const SystemPrompt = "You are a helpful assistant. Answer questions concisely and accurately."

// RunMultiTurnEval runs all cases against the given client and returns results.
//
// For each case:
//  1. Starts a fresh ConversationHistory.
//  2. For each turn: sends the accumulated history to the client, appends the
//     user message and assistant response, then runs the optional CheckFn.
//  3. Scores the final output using FinalEvaluators.
//
// An error is returned only for unexpected infrastructure failures; per-case
// errors are recorded inside MultiTurnResult.Err and do not abort the run.
func RunMultiTurnEval(ctx context.Context, cases []MultiTurnCase, client llm.Client) ([]MultiTurnResult, error) {
	results := make([]MultiTurnResult, 0, len(cases))

	for _, c := range cases {
		result := runOne(ctx, c, client)
		results = append(results, result)
	}

	return results, nil
}

// runOne executes a single MultiTurnCase and returns its MultiTurnResult.
func runOne(ctx context.Context, c MultiTurnCase, client llm.Client) MultiTurnResult {
	result := MultiTurnResult{
		Case:   c,
		Scores: make(map[string]float64),
	}

	history := &ConversationHistory{}

	systemPrompt := c.System
	if systemPrompt == "" {
		systemPrompt = SystemPrompt
	}

	var lastOutput string

	for i, turn := range c.Turns {
		// Append the user message to history.
		history.Messages = append(history.Messages, llm.Message{
			Role:    llm.RoleUser,
			Content: turn.UserMessage,
		})

		// Call the LLM with full history.
		req := llm.Request{
			System:      systemPrompt,
			Messages:    history.Messages,
			MaxTokens:   512,
			Temperature: 0.0,
		}

		resp, err := client.Complete(ctx, req)
		if err != nil {
			result.Err = fmt.Errorf("turn %d: llm.Complete: %w", i+1, err)
			result.TurnResults = append(result.TurnResults, TurnResult{
				Turn:  i + 1,
				Input: turn.UserMessage,
				Check: err.Error(),
			})
			return result
		}

		output := resp.Content

		// Append the assistant response to history.
		history.Messages = append(history.Messages, llm.Message{
			Role:    llm.RoleAssistant,
			Content: output,
		})

		lastOutput = output

		// Run optional per-turn check.
		checkResult := ""
		if turn.CheckFn != nil {
			checkResult = turn.CheckFn(output)
		}

		result.TurnResults = append(result.TurnResults, TurnResult{
			Turn:   i + 1,
			Input:  turn.UserMessage,
			Output: output,
			Check:  checkResult,
		})
	}

	result.FinalOutput = lastOutput

	// Score the final output.
	for name, evalFn := range c.FinalEvaluators {
		result.Scores[name] = evalFn(lastOutput)
	}

	return result
}

// PrintMultiTurnReport prints a human-readable summary of multi-turn eval results.
func PrintMultiTurnReport(results []MultiTurnResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Println("\n=== Multi-Turn Eval Report ===")

	for _, r := range results {
		fmt.Printf("\nCase: %s\n", r.Case.Name)

		if r.Err != nil {
			fmt.Printf("  ERROR: %v\n", r.Err)
			continue
		}

		// Print turn-by-turn results.
		for _, tr := range r.TurnResults {
			fmt.Printf("  Turn %d:\n", tr.Turn)
			fmt.Printf("    User:      %s\n", tr.Input)
			if tr.Output != "" {
				out := tr.Output
				if len(out) > 120 {
					out = out[:117] + "..."
				}
				fmt.Printf("    Assistant: %s\n", out)
			}
			if tr.Check != "" {
				fmt.Printf("    Check FAIL: %s\n", tr.Check)
			}
		}

		// Print final scores.
		if len(r.Scores) > 0 {
			fmt.Fprintln(w, "  Evaluator\tScore")
			fmt.Fprintln(w, "  ---------\t-----")
			var total float64
			for name, score := range r.Scores {
				fmt.Fprintf(w, "  %s\t%.2f\n", name, score)
				total += score
			}
			avg := total / float64(len(r.Scores))
			fmt.Fprintf(w, "  AVERAGE\t%.2f\n", avg)
			_ = w.Flush()

			// Overall pass/fail.
			if avg >= 0.5 {
				fmt.Printf("  Result: PASS (avg=%.2f)\n", avg)
			} else {
				fmt.Printf("  Result: FAIL (avg=%.2f)\n", avg)
			}
		}
	}

	fmt.Println("\n==============================")
}
