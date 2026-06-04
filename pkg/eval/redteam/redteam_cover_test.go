package redteam

import (
	"context"
	"errors"
	"testing"
)

// --- runOne: executor error ---

func TestRunOne_ExecutorError(t *testing.T) {
	result := runOne(context.Background(), RedTeamCase{
		Name:  "error-case",
		Input: "test",
	}, func(_ context.Context, _ string) (string, error) {
		return "", errors.New("executor boom")
	})
	if result.Passed {
		t.Error("expected failure when executor returns error")
	}
	if result.Err == nil {
		t.Error("expected Err to be set")
	}
	if len(result.Failures) == 0 {
		t.Error("expected at least one failure message")
	}
}

// --- runOne: ShouldNotContain violation ---

func TestRunOne_ShouldNotContainViolation(t *testing.T) {
	result := runOne(context.Background(), RedTeamCase{
		Name:             "not-contain-case",
		Input:            "test",
		ShouldNotContain: []string{"forbidden"},
	}, func(_ context.Context, _ string) (string, error) {
		return "This output contains Forbidden content", nil
	})
	if result.Passed {
		t.Error("expected failure when ShouldNotContain is violated")
	}
}

// --- runOne: ShouldContain not met ---

func TestRunOne_ShouldContainNotMet(t *testing.T) {
	result := runOne(context.Background(), RedTeamCase{
		Name:          "contain-case",
		Input:         "test",
		ShouldContain: []string{"required-text"},
	}, func(_ context.Context, _ string) (string, error) {
		return "This output does not have the required content", nil
	})
	if result.Passed {
		t.Error("expected failure when ShouldContain is not met")
	}
}

// --- runOne: ShouldNotContainPHI with PHI present ---

func TestRunOne_ShouldNotContainPHI_WithPHI(t *testing.T) {
	result := runOne(context.Background(), RedTeamCase{
		Name:                "phi-case",
		Input:               "test",
		ShouldNotContainPHI: true,
	}, func(_ context.Context, _ string) (string, error) {
		return "Patient SSN is 123-45-6789", nil
	})
	if result.Passed {
		t.Error("expected failure when PHI is present")
	}
}

// --- PrintReport: with executor error ---

func TestPrintReport_WithExecutorError(t *testing.T) {
	results := []RedTeamResult{
		{Case: "pass-case", Passed: true},
		{Case: "fail-case", Passed: false, Err: errors.New("boom"), Failures: []string{"executor error: boom"}},
		{Case: "fail-case2", Passed: false, Failures: []string{"issue1", "issue2"}},
	}
	// Just verify it doesn't panic.
	PrintReport(results)
}

// --- PrintReport: all pass ---

func TestPrintReport_AllPass(t *testing.T) {
	results := []RedTeamResult{
		{Case: "case1", Passed: true},
		{Case: "case2", Passed: true},
	}
	PrintReport(results)
}

// --- Run: multiple cases ---

func TestRun_MultipleCases(t *testing.T) {
	cases := []RedTeamCase{
		{Name: "c1", Input: "input1", ShouldRefuse: false},
		{Name: "c2", Input: "input2", ShouldRefuse: true},
	}
	results := Run(context.Background(), cases, func(_ context.Context, input string) (string, error) {
		if input == "input2" {
			return "I cannot provide that information", nil
		}
		return "normal response", nil
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("case 1 should pass")
	}
	if !results[1].Passed {
		t.Error("case 2 should pass (refusal detected)")
	}
}
