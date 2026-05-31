package singleturneval

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// SystemPrompt is the default system prompt used for the single-turn eval agent.
// It instructs the model to give concise, direct answers — ideal for
// knowledge-recall benchmarks where brevity aids scoring.
const SystemPrompt = `You are a knowledgeable assistant. Answer the user's question
concisely and directly. Provide only the essential information requested.`

// DefaultModel is the default LLM model used when no model flag is supplied.
// qwen2.5:7b is a capable, fast local model available via Ollama.
const DefaultModel = "qwen2.5:7b"

// RunEval executes an evaluation run over the provided cases.
//
// For each case:
//  1. The executor is called with the case Input; errors are captured in EvalResult.Err.
//  2. Every evaluator in the evaluators map is applied to the output.
//  3. An EvalResult is appended to the returned slice.
//
// RunEval itself never returns an error; executor errors are per-case and
// recorded in EvalResult.Err so the caller can distinguish a failed LLM call
// from a low score.
func RunEval(
	ctx context.Context,
	cases []EvalCase,
	executor func(ctx context.Context, input string) (string, error),
	evaluators map[string]EvaluatorFunc,
) ([]EvalResult, error) {
	results := make([]EvalResult, 0, len(cases))

	for _, c := range cases {
		result := EvalResult{
			Case:   c,
			Scores: make(map[string]float64, len(evaluators)),
		}

		output, err := executor(ctx, c.Input)
		result.Output = output
		result.Err = err

		if err == nil {
			for name, fn := range evaluators {
				result.Scores[name] = fn(output, c.Expected)
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// PrintReport writes a formatted evaluation table to stdout.
//
// The table shows each case's input (truncated), output (truncated), per-evaluator
// scores, an average score, and a pass/fail marker. A summary line at the end
// prints the number of cases that passed (average score >= 0.5).
func PrintReport(results []EvalResult) {
	printReportTo(os.Stdout, results)
}

// printReportTo is the testable variant of PrintReport — it writes to w instead of stdout.
func printReportTo(w io.Writer, results []EvalResult) {
	if len(results) == 0 {
		fmt.Fprintln(w, "singleturneval: no results to report")
		return
	}

	// Collect evaluator names in stable insertion order by scanning the first
	// result that has scores. All results share the same evaluator set.
	evalNames := collectEvalNames(results)

	// Build tabwriter header.
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Header row.
	header := "#\tInput\tOutput\t"
	for _, name := range evalNames {
		header += name + "\t"
	}
	header += "Avg\tPass"
	fmt.Fprintln(tw, header)

	// Separator row.
	sep := "-\t-----\t------\t"
	for range evalNames {
		sep += "-----\t"
	}
	sep += "---\t----"
	fmt.Fprintln(tw, sep)

	pass, fail := 0, 0

	for i, r := range results {
		input := truncate(r.Case.Input, 30)
		output := truncate(r.Output, 30)

		if r.Err != nil {
			output = "[err: " + truncate(r.Err.Error(), 25) + "]"
		}

		avg := averageScores(r.Scores)
		passStr := "PASS"
		if r.Err != nil || avg < 0.5 {
			passStr = "FAIL"
			fail++
		} else {
			pass++
		}

		line := fmt.Sprintf("%d\t%s\t%s\t", i, input, output)
		for _, name := range evalNames {
			score := r.Scores[name]
			line += fmt.Sprintf("%.2f\t", score)
		}
		line += fmt.Sprintf("%.2f\t%s", avg, passStr)
		fmt.Fprintln(tw, line)
	}

	fmt.Fprintln(tw)
	fmt.Fprintf(tw, "Total\t\t\t")
	for range evalNames {
		fmt.Fprint(tw, "\t")
	}
	fmt.Fprintf(tw, "\t%d/%d pass\n", pass, len(results))
	_ = tw.Flush()
}

// ExactMatchEvaluator is a built-in EvaluatorFunc that returns 1.0 when the
// output contains the expected "answer" string (case-insensitive), 0.0 otherwise.
//
// Expected map key: "answer" (string).
func ExactMatchEvaluator(output string, expected map[string]any) float64 {
	answer, ok := expected["answer"]
	if !ok {
		return 0.0
	}
	answerStr, ok := answer.(string)
	if !ok {
		return 0.0
	}
	if strings.Contains(strings.ToLower(output), strings.ToLower(answerStr)) {
		return 1.0
	}
	return 0.0
}

// NotEmptyEvaluator is a built-in EvaluatorFunc that returns 1.0 when the
// output is non-empty (after whitespace trimming), 0.0 otherwise.
func NotEmptyEvaluator(output string, _ map[string]any) float64 {
	if strings.TrimSpace(output) != "" {
		return 1.0
	}
	return 0.0
}

// collectEvalNames returns evaluator names in a consistent order derived from
// the first result that carries scores.
func collectEvalNames(results []EvalResult) []string {
	for _, r := range results {
		if len(r.Scores) > 0 {
			names := make([]string, 0, len(r.Scores))
			for k := range r.Scores {
				names = append(names, k)
			}
			// Sort for stable output.
			sortStrings(names)
			return names
		}
	}
	return nil
}

// sortStrings sorts a string slice in-place (insertion sort — small N).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// averageScores returns the mean of all values in the map, or 0 if empty.
func averageScores(scores map[string]float64) float64 {
	if len(scores) == 0 {
		return 0
	}
	var sum float64
	for _, v := range scores {
		sum += v
	}
	return sum / float64(len(scores))
}

// truncate shortens s to at most maxLen runes, appending "..." if trimmed.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
