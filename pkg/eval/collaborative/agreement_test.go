package collaborative

import (
	"math"
	"testing"
	"time"
)

// makeAnnotations is a test helper that creates annotations for a single
// annotator from a map of traceID -> label.
func makeAnnotations(annotatorID, criterion string, labels map[string]Label) []Annotation {
	now := time.Now()
	var anns []Annotation
	for traceID, label := range labels {
		anns = append(anns, Annotation{
			TraceID:     traceID,
			AnnotatorID: annotatorID,
			Criterion:   criterion,
			Label:       label,
			Reasoning:   "test reasoning for " + traceID,
			Timestamp:   now,
		})
	}
	return anns
}

func TestCohensKappa_PerfectAgreement(t *testing.T) {
	labels := map[string]Label{
		"t1": LabelPass,
		"t2": LabelFail,
		"t3": LabelPass,
		"t4": LabelFail,
		"t5": LabelPass,
	}

	a := makeAnnotations("alice", "accuracy", labels)
	b := makeAnnotations("bob", "accuracy", labels)

	result := CohensKappa(a, b, "accuracy")

	if result.PercentAgreement != 1.0 {
		t.Errorf("expected 100%% agreement, got %.2f%%", result.PercentAgreement*100)
	}
	if result.CohensKappa != 1.0 {
		t.Errorf("expected kappa=1.0, got %.4f", result.CohensKappa)
	}
	if result.Interpretation != "almost perfect" {
		t.Errorf("expected 'almost perfect', got %q", result.Interpretation)
	}
	if len(result.Disagreements) != 0 {
		t.Errorf("expected 0 disagreements, got %d", len(result.Disagreements))
	}
}

func TestCohensKappa_NoAgreement(t *testing.T) {
	labelsA := map[string]Label{
		"t1": LabelPass,
		"t2": LabelPass,
		"t3": LabelFail,
		"t4": LabelFail,
	}
	labelsB := map[string]Label{
		"t1": LabelFail,
		"t2": LabelFail,
		"t3": LabelPass,
		"t4": LabelPass,
	}

	a := makeAnnotations("alice", "accuracy", labelsA)
	b := makeAnnotations("bob", "accuracy", labelsB)

	result := CohensKappa(a, b, "accuracy")

	if result.PercentAgreement != 0.0 {
		t.Errorf("expected 0%% agreement, got %.2f%%", result.PercentAgreement*100)
	}
	if result.CohensKappa >= 0.0 {
		t.Errorf("expected negative kappa, got %.4f", result.CohensKappa)
	}
	if result.Interpretation != "poor" {
		t.Errorf("expected 'poor', got %q", result.Interpretation)
	}
	if len(result.Disagreements) != 4 {
		t.Errorf("expected 4 disagreements, got %d", len(result.Disagreements))
	}
}

func TestCohensKappa_PartialAgreement(t *testing.T) {
	// Classic example from the literature:
	// A: pass pass pass fail fail
	// B: pass pass fail fail fail
	// Observed agreement: 4/5 = 0.80
	labelsA := map[string]Label{
		"t1": LabelPass,
		"t2": LabelPass,
		"t3": LabelPass,
		"t4": LabelFail,
		"t5": LabelFail,
	}
	labelsB := map[string]Label{
		"t1": LabelPass,
		"t2": LabelPass,
		"t3": LabelFail,
		"t4": LabelFail,
		"t5": LabelFail,
	}

	a := makeAnnotations("alice", "accuracy", labelsA)
	b := makeAnnotations("bob", "accuracy", labelsB)

	result := CohensKappa(a, b, "accuracy")

	if math.Abs(result.PercentAgreement-0.80) > 0.01 {
		t.Errorf("expected ~80%% agreement, got %.2f%%", result.PercentAgreement*100)
	}

	// With 3/5 pass for A and 2/5 pass for B:
	// Pe = (3/5)*(2/5) + (2/5)*(3/5) = 0.24 + 0.24 = 0.48
	// Kappa = (0.80 - 0.48) / (1 - 0.48) = 0.32/0.52 ~= 0.615
	if result.CohensKappa < 0.5 || result.CohensKappa > 0.7 {
		t.Errorf("expected kappa ~0.615, got %.4f", result.CohensKappa)
	}

	if len(result.Disagreements) != 1 {
		t.Errorf("expected 1 disagreement, got %d", len(result.Disagreements))
	}
}

func TestCohensKappa_NoCommonTraces(t *testing.T) {
	a := makeAnnotations("alice", "accuracy", map[string]Label{"t1": LabelPass})
	b := makeAnnotations("bob", "accuracy", map[string]Label{"t99": LabelFail})

	result := CohensKappa(a, b, "accuracy")

	if result.PercentAgreement != 0.0 {
		t.Errorf("expected 0 agreement, got %.2f", result.PercentAgreement)
	}
	if result.CohensKappa != 0.0 {
		t.Errorf("expected kappa=0, got %.4f", result.CohensKappa)
	}
}

func TestCohensKappa_DifferentCriterion(t *testing.T) {
	// Annotations for criterion "accuracy" should not affect criterion "tone".
	a := makeAnnotations("alice", "accuracy", map[string]Label{"t1": LabelPass})
	b := makeAnnotations("bob", "tone", map[string]Label{"t1": LabelFail})

	result := CohensKappa(a, b, "accuracy")
	if result.CohensKappa != 0.0 {
		t.Errorf("expected kappa=0 when criteria differ, got %.4f", result.CohensKappa)
	}
}

func TestCohensKappa_AnnotatorIDs(t *testing.T) {
	a := makeAnnotations("alice", "safety", map[string]Label{"t1": LabelPass})
	b := makeAnnotations("bob", "safety", map[string]Label{"t1": LabelPass})

	result := CohensKappa(a, b, "safety")

	if result.AnnotatorA != "alice" {
		t.Errorf("expected annotator A = 'alice', got %q", result.AnnotatorA)
	}
	if result.AnnotatorB != "bob" {
		t.Errorf("expected annotator B = 'bob', got %q", result.AnnotatorB)
	}
	if result.Criterion != "safety" {
		t.Errorf("expected criterion = 'safety', got %q", result.Criterion)
	}
}

func TestFleissKappa_PerfectAgreement(t *testing.T) {
	labels := map[string]Label{
		"t1": LabelPass,
		"t2": LabelFail,
		"t3": LabelPass,
	}
	annotations := [][]Annotation{
		makeAnnotations("alice", "accuracy", labels),
		makeAnnotations("bob", "accuracy", labels),
		makeAnnotations("carol", "accuracy", labels),
	}

	fk := FleissKappa(annotations, "accuracy")

	if math.Abs(fk-1.0) > 0.001 {
		t.Errorf("expected Fleiss' kappa ~1.0, got %.4f", fk)
	}
}

func TestFleissKappa_NoAgreement(t *testing.T) {
	// Two raters, completely opposite labels, equal marginals.
	labelsA := map[string]Label{
		"t1": LabelPass,
		"t2": LabelFail,
	}
	labelsB := map[string]Label{
		"t1": LabelFail,
		"t2": LabelPass,
	}
	annotations := [][]Annotation{
		makeAnnotations("alice", "accuracy", labelsA),
		makeAnnotations("bob", "accuracy", labelsB),
	}

	fk := FleissKappa(annotations, "accuracy")

	if fk >= 0.0 {
		t.Errorf("expected negative Fleiss' kappa for no agreement, got %.4f", fk)
	}
}

func TestFleissKappa_SingleAnnotator(t *testing.T) {
	annotations := [][]Annotation{
		makeAnnotations("alice", "accuracy", map[string]Label{"t1": LabelPass}),
	}

	fk := FleissKappa(annotations, "accuracy")

	if fk != 0.0 {
		t.Errorf("expected 0.0 for single annotator, got %.4f", fk)
	}
}

func TestFleissKappa_NoCommonTraces(t *testing.T) {
	annotations := [][]Annotation{
		makeAnnotations("alice", "accuracy", map[string]Label{"t1": LabelPass}),
		makeAnnotations("bob", "accuracy", map[string]Label{"t2": LabelFail}),
	}

	fk := FleissKappa(annotations, "accuracy")

	if fk != 0.0 {
		t.Errorf("expected 0.0 for no common traces, got %.4f", fk)
	}
}

func TestFleissKappa_ThreeAnnotatorsPartial(t *testing.T) {
	// Three annotators, some agreement.
	annotations := [][]Annotation{
		makeAnnotations("alice", "accuracy", map[string]Label{
			"t1": LabelPass, "t2": LabelPass, "t3": LabelFail, "t4": LabelFail,
		}),
		makeAnnotations("bob", "accuracy", map[string]Label{
			"t1": LabelPass, "t2": LabelFail, "t3": LabelFail, "t4": LabelFail,
		}),
		makeAnnotations("carol", "accuracy", map[string]Label{
			"t1": LabelPass, "t2": LabelPass, "t3": LabelPass, "t4": LabelFail,
		}),
	}

	fk := FleissKappa(annotations, "accuracy")

	// The value should be between -1 and 1.
	if fk < -1.0 || fk > 1.0 {
		t.Errorf("Fleiss' kappa out of range: %.4f", fk)
	}
	// With this data, annotators agree on t1 (all pass) and t4 (all fail),
	// but disagree on t2 and t3. Expect moderate agreement.
	if fk < 0.0 {
		t.Errorf("expected positive Fleiss' kappa, got %.4f", fk)
	}
}

func TestInterpretKappa(t *testing.T) {
	tests := []struct {
		kappa    float64
		expected string
	}{
		{-0.5, "poor"},
		{0.0, "slight"},
		{0.10, "slight"},
		{0.20, "slight"},
		{0.21, "fair"},
		{0.40, "fair"},
		{0.41, "moderate"},
		{0.60, "moderate"},
		{0.61, "substantial"},
		{0.80, "substantial"},
		{0.81, "almost perfect"},
		{1.00, "almost perfect"},
	}

	for _, tt := range tests {
		got := InterpretKappa(tt.kappa)
		if got != tt.expected {
			t.Errorf("InterpretKappa(%.2f) = %q, want %q", tt.kappa, got, tt.expected)
		}
	}
}

func TestFindDisagreements(t *testing.T) {
	a := makeAnnotations("alice", "accuracy", map[string]Label{
		"t1": LabelPass,
		"t2": LabelFail,
		"t3": LabelPass,
	})
	b := makeAnnotations("bob", "accuracy", map[string]Label{
		"t1": LabelPass,
		"t2": LabelPass,
		"t3": LabelFail,
	})

	disagreements := FindDisagreements(a, b, "accuracy")

	if len(disagreements) != 2 {
		t.Fatalf("expected 2 disagreements, got %d", len(disagreements))
	}

	// Verify the disagreements contain reasoning.
	for _, d := range disagreements {
		if d.ReasonA == "" || d.ReasonB == "" {
			t.Errorf("disagreement for %s missing reasoning", d.TraceID)
		}
		if d.LabelA == d.LabelB {
			t.Errorf("disagreement for %s has same labels: %s/%s", d.TraceID, d.LabelA, d.LabelB)
		}
	}
}

func TestFindDisagreements_Empty(t *testing.T) {
	a := makeAnnotations("alice", "accuracy", map[string]Label{"t1": LabelPass})
	b := makeAnnotations("bob", "accuracy", map[string]Label{"t1": LabelPass})

	disagreements := FindDisagreements(a, b, "accuracy")

	if len(disagreements) != 0 {
		t.Errorf("expected 0 disagreements, got %d", len(disagreements))
	}
}

func TestFindDisagreements_WrongCriterion(t *testing.T) {
	a := makeAnnotations("alice", "accuracy", map[string]Label{"t1": LabelPass})
	b := makeAnnotations("bob", "accuracy", map[string]Label{"t1": LabelFail})

	// Query for a different criterion — should find nothing.
	disagreements := FindDisagreements(a, b, "tone")

	if len(disagreements) != 0 {
		t.Errorf("expected 0 disagreements for wrong criterion, got %d", len(disagreements))
	}
}

func TestCohensKappa_AllSameLabel(t *testing.T) {
	// When both annotators label everything the same, Pe = 1.0 and kappa is
	// undefined. We define it as 0.0 in this edge case.
	labels := map[string]Label{
		"t1": LabelPass,
		"t2": LabelPass,
		"t3": LabelPass,
	}

	a := makeAnnotations("alice", "accuracy", labels)
	b := makeAnnotations("bob", "accuracy", labels)

	result := CohensKappa(a, b, "accuracy")

	// Percent agreement is 1.0.
	if result.PercentAgreement != 1.0 {
		t.Errorf("expected 100%% agreement, got %.2f%%", result.PercentAgreement*100)
	}
	// When Pe = 1.0 (all items labeled the same way), kappa is 0/(1-1)
	// which we define as 0.0.
	if math.IsNaN(result.CohensKappa) || math.IsInf(result.CohensKappa, 0) {
		t.Errorf("kappa should be 0.0 when Pe=1, got %.4f", result.CohensKappa)
	}
}

func TestSortStrings(t *testing.T) {
	input := []string{"cherry", "apple", "banana"}
	sortStrings(input)

	expected := []string{"apple", "banana", "cherry"}
	for i, v := range input {
		if v != expected[i] {
			t.Errorf("sortStrings: index %d = %q, want %q", i, v, expected[i])
		}
	}
}
