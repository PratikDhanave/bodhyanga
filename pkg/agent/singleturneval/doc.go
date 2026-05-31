// Package singleturneval implements the "04-Single-Turn-Evals" lesson.
//
// # What it teaches
//
// Single-turn evals are the simplest form of systematic agent evaluation:
// run the agent once per input row, capture the output, then score it with
// deterministic evaluator functions. No multi-step reasoning traces, no tool
// mocking — just input → output → score.
//
// # What it adds
//
// Structured dataset (EvalCase)
//
//	Each case carries an Input string (the user message), an Expected map
//	(ground-truth values the evaluators compare against), and a Metadata map
//	(labels used only in reporting, not scoring).
//
// Executor pattern
//
//	The caller supplies an executor: func(ctx, input string) (string, error).
//	This lets the same harness drive any backend — an LLM client, a rule
//	engine, or a stub — without coupling the evaluation library to a
//	specific provider. Tests pass a fake executor; production passes a real
//	LLM call.
//
// Evaluator functions (EvaluatorFunc)
//
//	An evaluator is func(output string, expected map[string]any) float64.
//	Each evaluator returns a score in [0.0, 1.0]. Multiple evaluators may
//	run against the same output; RunEval collects all scores into a map.
//	Two built-in evaluators are provided:
//	  - ExactMatchEvaluator   — case-insensitive substring match
//	  - NotEmptyEvaluator     — checks that the output is non-empty
//
// EvalResult
//
//	One EvalResult is produced per EvalCase. It records the original case,
//	the executor's output, the per-evaluator scores, and any error returned
//	by the executor.
//
// # Usage
//
//	cases := []singleturneval.EvalCase{
//	    {Input: "What is the capital of France?", Expected: map[string]any{"answer": "Paris"}},
//	}
//	executor := func(ctx context.Context, input string) (string, error) {
//	    resp, err := llmClient.Complete(ctx, llm.Request{...})
//	    return resp.Content, err
//	}
//	evaluators := map[string]singleturneval.EvaluatorFunc{
//	    "exact_match": singleturneval.ExactMatchEvaluator,
//	    "not_empty":   singleturneval.NotEmptyEvaluator,
//	}
//	results, err := singleturneval.RunEval(ctx, cases, executor, evaluators)
//	singleturneval.PrintReport(results)
package singleturneval
