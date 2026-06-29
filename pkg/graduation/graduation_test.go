package graduation

import (
	"math"
	"testing"
)

func TestStageRankAndValid(t *testing.T) {
	if StageDevelopment.Rank() != 0 || StageAutopilot.Rank() != 3 {
		t.Fatalf("rank ordering wrong: dev=%d auto=%d", StageDevelopment.Rank(), StageAutopilot.Rank())
	}
	if Stage("bogus").Valid() {
		t.Error("unknown stage must be invalid")
	}
	if Stage("bogus").Rank() != -1 {
		t.Error("unknown stage rank must be -1")
	}
}

func TestStageNextPrevBoundaries(t *testing.T) {
	if s, ok := StageDevelopment.Next(); !ok || s != StageShadow {
		t.Errorf("development.Next = %v,%v want shadow,true", s, ok)
	}
	if _, ok := StageAutopilot.Next(); ok {
		t.Error("autopilot.Next must be false (terminal)")
	}
	if _, ok := StageDevelopment.Prev(); ok {
		t.Error("development.Prev must be false (terminal)")
	}
	if s, ok := StageAutopilot.Prev(); !ok || s != StageCopilot {
		t.Errorf("autopilot.Prev = %v,%v want copilot,true", s, ok)
	}
	if _, ok := Stage("bogus").Next(); ok {
		t.Error("invalid stage Next must be false")
	}
}

func TestParseStage(t *testing.T) {
	if s, err := ParseStage("copilot"); err != nil || s != StageCopilot {
		t.Errorf("ParseStage(copilot) = %v,%v", s, err)
	}
	if _, err := ParseStage("supervisor"); err == nil {
		t.Error("ParseStage must reject unknown stage")
	}
}

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestWilsonLowerBound_KnownValue(t *testing.T) {
	// 90/100 at z=1.96 → ≈0.8256 (hand-computed).
	got := WilsonLowerBound(90, 100, 1.96)
	if !approx(got, 0.8256, 0.0005) {
		t.Errorf("WilsonLowerBound(90,100,1.96) = %.5f, want ≈0.8256", got)
	}
}

func TestWilsonLowerBound_PenalizesSmallSamples(t *testing.T) {
	// Same 0.9 rate, fewer trials ⇒ strictly lower bound.
	small := WilsonLowerBound(9, 10, 1.96)
	big := WilsonLowerBound(90, 100, 1.96)
	if !(small < big) {
		t.Errorf("expected small-sample bound (%.4f) < large-sample bound (%.4f)", small, big)
	}
	// A perfect-but-tiny record must NOT reach a high bound.
	perfectTiny := WilsonLowerBound(5, 5, 1.96)
	if perfectTiny >= 0.95 {
		t.Errorf("5/5 should not clear 0.95 (got %.4f) — small samples must be penalized", perfectTiny)
	}
}

func TestWilsonLowerBound_Edges(t *testing.T) {
	if WilsonLowerBound(0, 0, 1.96) != 0 {
		t.Error("total=0 must return 0")
	}
	if got := WilsonLowerBound(5, 3, 1.96); !approx(got, WilsonLowerBound(3, 3, 1.96), 1e-9) {
		t.Error("successes>total must clamp to total")
	}
	// z<=0 falls back to DefaultZ.
	if WilsonLowerBound(90, 100, 0) != WilsonLowerBound(90, 100, DefaultZ) {
		t.Error("z<=0 should use DefaultZ")
	}
}

func TestEvaluate_PromotesWhenBarMet(t *testing.T) {
	// 990/1000 at 95% clears 0.95 comfortably.
	d := Evaluate(StageShadow, 990, 1000, DefaultCriteria())
	if d.Outcome != OutcomePromote || d.To != StageCopilot {
		t.Fatalf("expected promote shadow→copilot, got %s→%s (%s)", d.From, d.To, d.Reason)
	}
}

func TestEvaluate_HoldsOnInsufficientObservations(t *testing.T) {
	// Perfect rate but only 10 obs < 100 required.
	d := Evaluate(StageShadow, 10, 10, DefaultCriteria())
	if d.Outcome != OutcomeHold || d.To != StageShadow {
		t.Fatalf("expected hold for too-few obs, got %s (%s)", d.Outcome, d.Reason)
	}
}

func TestEvaluate_HoldsOnLowRate(t *testing.T) {
	// Enough obs, but rate too low to clear 0.95 lower bound.
	d := Evaluate(StageCopilot, 800, 1000, DefaultCriteria())
	if d.Outcome != OutcomeHold {
		t.Fatalf("expected hold for low rate, got %s (%s)", d.Outcome, d.Reason)
	}
}

func TestEvaluate_TerminalAndInvalid(t *testing.T) {
	if d := Evaluate(StageAutopilot, 1000, 1000, DefaultCriteria()); d.Outcome != OutcomeHold || d.To != StageAutopilot {
		t.Errorf("autopilot must hold (terminal), got %s→%s", d.From, d.To)
	}
	if d := Evaluate(Stage("bogus"), 1000, 1000, DefaultCriteria()); d.Outcome != OutcomeHold {
		t.Errorf("invalid stage must hold, got %s", d.Outcome)
	}
}
