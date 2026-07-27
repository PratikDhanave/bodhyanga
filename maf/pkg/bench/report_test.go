package bench

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTable_RendersCasesAndAggregate(t *testing.T) {
	results := []CaseResult{
		{ID: "acs", Gold: "acute coronary syndrome", Produced: "acute coronary syndrome", Status: StatusMatch},
		{ID: "undiff", Gold: "undifferentiated presentation", Produced: "undifferentiated presentation", Status: StatusMatch, Forced: true},
	}
	s := Aggregate(results)
	var buf bytes.Buffer
	WriteTable(&buf, results, s)
	out := buf.String()
	for _, want := range []string{"CASE", "acs", "undiff", "accuracy 100.0%", "unscored=0"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

// Accuracy = matches ÷ scored; unscored and failures are excluded from the
// denominator (SC-002).
func TestAggregate_AccuracyExcludesUnscoredAndFailures(t *testing.T) {
	results := []CaseResult{
		{ID: "a", Status: StatusMatch},
		{ID: "b", Status: StatusMatch},
		{ID: "c", Status: StatusMiss},
		{ID: "d", Status: StatusUnscored},
		{ID: "e", Status: StatusFailure},
	}
	s := Aggregate(results)
	if s.Cases != 5 || s.Scored != 3 || s.Matches != 2 || s.Unscored != 1 || s.Failures != 1 {
		t.Fatalf("counts wrong: %+v", s)
	}
	// 2 of 3 scored → 66.7%.
	if got := s.AccuracyPct; got < 66.6 || got > 66.7 {
		t.Errorf("accuracy = %.2f, want ~66.7", got)
	}
}

func TestAggregate_NoScoredCasesIsZeroNotNaN(t *testing.T) {
	s := Aggregate([]CaseResult{{Status: StatusUnscored}, {Status: StatusFailure}})
	if s.AccuracyPct != 0 {
		t.Errorf("accuracy = %v, want 0 when nothing is scored", s.AccuracyPct)
	}
}
