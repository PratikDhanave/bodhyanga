package singleturneval

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// RunEval tests
// ---------------------------------------------------------------------------

func TestRunEval_BasicCase(t *testing.T) {
	cases := []EvalCase{
		{Input: "What is 2+2?", Expected: map[string]any{"answer": "4"}},
		{Input: "What is 3+3?", Expected: map[string]any{"answer": "6"}},
	}

	executor := func(_ context.Context, input string) (string, error) {
		if strings.Contains(input, "2+2") {
			return "The answer is 4", nil
		}
		return "The answer is 6", nil
	}

	evaluators := map[string]EvaluatorFunc{
		"exact_match": ExactMatchEvaluator,
	}

	results, err := RunEval(context.Background(), cases, executor, evaluators)
	if err != nil {
		t.Fatalf("RunEval returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for i, r := range results {
		if r.Err != nil {
			t.Errorf("result[%d] unexpected error: %v", i, r.Err)
		}
		score, ok := r.Scores["exact_match"]
		if !ok {
			t.Errorf("result[%d] missing exact_match score", i)
		}
		if score != 1.0 {
			t.Errorf("result[%d] expected score 1.0, got %.2f", i, score)
		}
	}
}

func TestRunEval_ExecutorError(t *testing.T) {
	cases := []EvalCase{
		{Input: "good", Expected: map[string]any{"answer": "ok"}},
		{Input: "bad", Expected: map[string]any{"answer": "ok"}},
	}

	executor := func(_ context.Context, input string) (string, error) {
		if input == "bad" {
			return "", errors.New("llm timeout")
		}
		return "ok", nil
	}

	evaluators := map[string]EvaluatorFunc{
		"exact_match": ExactMatchEvaluator,
	}

	results, err := RunEval(context.Background(), cases, executor, evaluators)
	if err != nil {
		t.Fatalf("RunEval returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First case should succeed.
	if results[0].Err != nil {
		t.Errorf("result[0] unexpected error: %v", results[0].Err)
	}
	if results[0].Scores["exact_match"] != 1.0 {
		t.Errorf("result[0] expected score 1.0, got %.2f", results[0].Scores["exact_match"])
	}

	// Second case should have an error and empty scores.
	if results[1].Err == nil {
		t.Fatal("result[1] expected error, got nil")
	}
	if len(results[1].Scores) != 0 {
		t.Errorf("result[1] expected empty scores, got %v", results[1].Scores)
	}
}

func TestRunEval_MultipleEvaluators(t *testing.T) {
	cases := []EvalCase{
		{Input: "capital of France?", Expected: map[string]any{"answer": "paris"}},
	}

	executor := func(_ context.Context, _ string) (string, error) {
		return "The capital is Paris", nil
	}

	evaluators := map[string]EvaluatorFunc{
		"exact_match": ExactMatchEvaluator,
		"not_empty":   NotEmptyEvaluator,
	}

	results, err := RunEval(context.Background(), cases, executor, evaluators)
	if err != nil {
		t.Fatalf("RunEval returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if len(r.Scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(r.Scores))
	}
	if r.Scores["exact_match"] != 1.0 {
		t.Errorf("exact_match expected 1.0, got %.2f", r.Scores["exact_match"])
	}
	if r.Scores["not_empty"] != 1.0 {
		t.Errorf("not_empty expected 1.0, got %.2f", r.Scores["not_empty"])
	}
}

func TestRunEval_EmptyCases(t *testing.T) {
	executor := func(_ context.Context, _ string) (string, error) {
		return "should not be called", nil
	}

	evaluators := map[string]EvaluatorFunc{
		"exact_match": ExactMatchEvaluator,
	}

	results, err := RunEval(context.Background(), nil, executor, evaluators)
	if err != nil {
		t.Fatalf("RunEval returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// ExactMatchEvaluator tests
// ---------------------------------------------------------------------------

func TestExactMatchEvaluator_Match(t *testing.T) {
	score := ExactMatchEvaluator("The capital is Paris", map[string]any{"answer": "paris"})
	if score != 1.0 {
		t.Errorf("expected 1.0, got %.2f", score)
	}
}

func TestExactMatchEvaluator_NoMatch(t *testing.T) {
	score := ExactMatchEvaluator("I don't know", map[string]any{"answer": "paris"})
	if score != 0.0 {
		t.Errorf("expected 0.0, got %.2f", score)
	}
}

func TestExactMatchEvaluator_MissingKey(t *testing.T) {
	score := ExactMatchEvaluator("The capital is Paris", map[string]any{})
	if score != 0.0 {
		t.Errorf("expected 0.0, got %.2f", score)
	}
}

func TestExactMatchEvaluator_WrongType(t *testing.T) {
	score := ExactMatchEvaluator("The answer is 42", map[string]any{"answer": 42})
	if score != 0.0 {
		t.Errorf("expected 0.0, got %.2f", score)
	}
}

// ---------------------------------------------------------------------------
// NotEmptyEvaluator tests
// ---------------------------------------------------------------------------

func TestNotEmptyEvaluator_NonEmpty(t *testing.T) {
	score := NotEmptyEvaluator("hello", nil)
	if score != 1.0 {
		t.Errorf("expected 1.0, got %.2f", score)
	}
}

func TestNotEmptyEvaluator_Empty(t *testing.T) {
	score := NotEmptyEvaluator("", nil)
	if score != 0.0 {
		t.Errorf("expected 0.0, got %.2f", score)
	}
}

func TestNotEmptyEvaluator_WhitespaceOnly(t *testing.T) {
	score := NotEmptyEvaluator("   ", nil)
	if score != 0.0 {
		t.Errorf("expected 0.0, got %.2f", score)
	}
}

// ---------------------------------------------------------------------------
// truncate tests
// ---------------------------------------------------------------------------

func TestTruncate_ShortString(t *testing.T) {
	got := truncate("hi", 10)
	if got != "hi" {
		t.Errorf("expected %q, got %q", "hi", got)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestTruncate_NeedsTruncation(t *testing.T) {
	got := truncate("hello world", 8)
	if got != "hello..." {
		t.Errorf("expected %q, got %q", "hello...", got)
	}
}

func TestTruncate_MaxLenLessThanOrEqualThree(t *testing.T) {
	got := truncate("hello", 2)
	if got != "he" {
		t.Errorf("expected %q, got %q", "he", got)
	}

	got = truncate("hello", 3)
	if got != "hel" {
		t.Errorf("expected %q, got %q", "hel", got)
	}
}

// ---------------------------------------------------------------------------
// averageScores tests
// ---------------------------------------------------------------------------

func TestAverageScores_Normal(t *testing.T) {
	avg := averageScores(map[string]float64{"a": 1.0, "b": 0.5})
	if avg != 0.75 {
		t.Errorf("expected 0.75, got %.4f", avg)
	}
}

func TestAverageScores_Empty(t *testing.T) {
	avg := averageScores(map[string]float64{})
	if avg != 0.0 {
		t.Errorf("expected 0.0, got %.4f", avg)
	}
}

// ---------------------------------------------------------------------------
// sortStrings tests
// ---------------------------------------------------------------------------

func TestSortStrings(t *testing.T) {
	s := []string{"c", "a", "b"}
	sortStrings(s)
	expected := []string{"a", "b", "c"}
	for i, v := range s {
		if v != expected[i] {
			t.Errorf("index %d: expected %q, got %q", i, expected[i], v)
		}
	}
}

// ---------------------------------------------------------------------------
// collectEvalNames tests
// ---------------------------------------------------------------------------

func TestCollectEvalNames_FindsNames(t *testing.T) {
	results := []EvalResult{
		{Scores: map[string]float64{"zebra": 0.5, "alpha": 1.0, "mid": 0.8}},
	}
	names := collectEvalNames(results)
	expected := []string{"alpha", "mid", "zebra"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("index %d: expected %q, got %q", i, expected[i], n)
		}
	}
}

func TestCollectEvalNames_NoScores(t *testing.T) {
	results := []EvalResult{
		{Scores: map[string]float64{}},
		{Scores: nil},
	}
	names := collectEvalNames(results)
	if names != nil {
		t.Errorf("expected nil, got %v", names)
	}
}

// ---------------------------------------------------------------------------
// printReportTo tests
// ---------------------------------------------------------------------------

func TestPrintReportTo_WithResults(t *testing.T) {
	results := []EvalResult{
		{
			Case:   EvalCase{Input: "What is 2+2?"},
			Output: "4",
			Scores: map[string]float64{"exact_match": 1.0},
		},
		{
			Case:   EvalCase{Input: "What is 1+1?"},
			Output: "wrong",
			Scores: map[string]float64{"exact_match": 0.0},
		},
	}

	var buf bytes.Buffer
	printReportTo(&buf, results)
	out := buf.String()

	// Check that case numbers appear.
	if !strings.Contains(out, "0") {
		t.Error("output should contain case number 0")
	}
	if !strings.Contains(out, "1") {
		t.Error("output should contain case number 1")
	}

	// Check PASS/FAIL markers.
	if !strings.Contains(out, "PASS") {
		t.Error("output should contain PASS")
	}
	if !strings.Contains(out, "FAIL") {
		t.Error("output should contain FAIL")
	}

	// Check header elements.
	if !strings.Contains(out, "Input") {
		t.Error("output should contain header 'Input'")
	}
	if !strings.Contains(out, "Output") {
		t.Error("output should contain header 'Output'")
	}
	if !strings.Contains(out, "exact_match") {
		t.Error("output should contain evaluator name 'exact_match'")
	}

	// Check summary line.
	if !strings.Contains(out, "1/2 pass") {
		t.Errorf("output should contain '1/2 pass', got:\n%s", out)
	}
}

func TestPrintReportTo_NoResults(t *testing.T) {
	var buf bytes.Buffer
	printReportTo(&buf, nil)
	out := buf.String()

	if !strings.Contains(out, "no results to report") {
		t.Errorf("expected 'no results to report', got: %q", out)
	}
}

func TestPrintReportTo_ErrorCase(t *testing.T) {
	results := []EvalResult{
		{
			Case:   EvalCase{Input: "test question"},
			Output: "",
			Scores: map[string]float64{},
			Err:    errors.New("connection refused"),
		},
	}

	var buf bytes.Buffer
	printReportTo(&buf, results)
	out := buf.String()

	if !strings.Contains(out, "[err:") {
		t.Errorf("output should contain '[err:', got:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Error("error case should be marked FAIL")
	}
}
