package equity

import (
	"math"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/eval"
)

// ----------------------------------------------------------------------------
// WilsonCI tests
// ----------------------------------------------------------------------------

func TestWilsonCI_ZeroN(t *testing.T) {
	lo, hi := WilsonCI(0, 0, 1.96)
	if lo != 0 || hi != 0 {
		t.Fatalf("expected (0,0) for n=0, got (%.4f, %.4f)", lo, hi)
	}
}

func TestWilsonCI_AllCorrect(t *testing.T) {
	lo, hi := WilsonCI(10, 10, 1.96)
	// Upper bound should be 1.0, lower bound should be well above 0.5
	if hi != 1.0 {
		t.Fatalf("expected hi=1.0 for 10/10, got %.4f", hi)
	}
	if lo < 0.65 {
		t.Fatalf("expected lo > 0.65 for 10/10, got %.4f", lo)
	}
}

func TestWilsonCI_NoneCorrect(t *testing.T) {
	lo, hi := WilsonCI(0, 10, 1.96)
	// Lower bound should be 0.0; upper bound should be below 0.35
	if lo != 0.0 {
		t.Fatalf("expected lo=0.0 for 0/10, got %.4f", lo)
	}
	if hi > 0.35 {
		t.Fatalf("expected hi < 0.35 for 0/10, got %.4f", hi)
	}
}

func TestWilsonCI_HalfCorrect(t *testing.T) {
	lo, hi := WilsonCI(5, 10, 1.96)
	// For p=0.5, n=10, z=1.96 the Wilson interval is roughly [0.237, 0.763]
	if lo < 0.20 || lo > 0.35 {
		t.Fatalf("unexpected lo for 5/10: %.4f", lo)
	}
	if hi < 0.65 || hi > 0.80 {
		t.Fatalf("unexpected hi for 5/10: %.4f", hi)
	}
	// Interval must be symmetric around 0.5
	mid := (lo + hi) / 2
	if math.Abs(mid-0.5) > 0.05 {
		t.Fatalf("expected interval centred near 0.5, mid=%.4f", mid)
	}
}

func TestWilsonCI_Bounds(t *testing.T) {
	// Returned values must always be in [0,1]
	cases := [][2]int{{0, 1}, {1, 1}, {3, 5}, {99, 100}}
	for _, c := range cases {
		lo, hi := WilsonCI(c[0], c[1], 1.96)
		if lo < 0 || hi > 1 || lo > hi {
			t.Errorf("invalid CI [%.4f, %.4f] for %d/%d", lo, hi, c[0], c[1])
		}
	}
}

// ----------------------------------------------------------------------------
// ExtractStratum tests
// ----------------------------------------------------------------------------

func TestExtractStratum_AgeGroupFromAge(t *testing.T) {
	cases := []struct {
		age      float64
		expected string
	}{
		{0, "pediatric(<18)"},
		{17.9, "pediatric(<18)"},
		{18, "adult(18-64)"},
		{45, "adult(18-64)"},
		{64.9, "adult(18-64)"},
		{65, "elderly(65+)"},
		{90, "elderly(65+)"},
	}
	for _, tc := range cases {
		s := ExtractStratum(map[string]any{"age": tc.age})
		if s.AgeGroup != tc.expected {
			t.Errorf("age %.1f: expected %q, got %q", tc.age, tc.expected, s.AgeGroup)
		}
	}
}

func TestExtractStratum_ExplicitAgeGroup(t *testing.T) {
	s := ExtractStratum(map[string]any{
		"age":       25.0,
		"age_group": "Pediatric(<18)", // explicit overrides computed
	})
	if s.AgeGroup != "pediatric(<18)" {
		t.Errorf("expected explicit age_group to win, got %q", s.AgeGroup)
	}
}

func TestExtractStratum_SexAndGender(t *testing.T) {
	// "sex" takes precedence over "gender"
	s := ExtractStratum(map[string]any{"sex": "Female", "gender": "nonbinary"})
	if s.Sex != "female" {
		t.Errorf("expected %q, got %q", "female", s.Sex)
	}
	// fall back to gender when sex absent
	s2 := ExtractStratum(map[string]any{"gender": "Male"})
	if s2.Sex != "male" {
		t.Errorf("expected %q, got %q", "male", s2.Sex)
	}
}

func TestExtractStratum_RaceEthnicity(t *testing.T) {
	// both race and ethnicity
	s := ExtractStratum(map[string]any{"race": "Asian", "ethnicity": "Chinese"})
	if s.RaceEthnicity != "asian / chinese" {
		t.Errorf("unexpected race/eth: %q", s.RaceEthnicity)
	}
	// race only
	s2 := ExtractStratum(map[string]any{"race": "Black"})
	if s2.RaceEthnicity != "black" {
		t.Errorf("unexpected race: %q", s2.RaceEthnicity)
	}
}

func TestExtractStratum_Defaults(t *testing.T) {
	s := ExtractStratum(map[string]any{})
	if s.AgeGroup != "unknown" {
		t.Errorf("expected unknown age_group, got %q", s.AgeGroup)
	}
	if s.Sex != "unknown" {
		t.Errorf("expected unknown sex, got %q", s.Sex)
	}
	if s.RaceEthnicity != "unknown" {
		t.Errorf("expected unknown race, got %q", s.RaceEthnicity)
	}
}

// ----------------------------------------------------------------------------
// Stratify tests
// ----------------------------------------------------------------------------

func makeMetric(correct bool, meta map[string]any) eval.CaseMetrics {
	acc := 0.0
	if correct {
		acc = 1.0
	}
	return eval.CaseMetrics{
		DiagnosisMatch: correct,
		Accuracy:       acc,
		Metadata:       meta,
	}
}

func TestStratify_Empty(t *testing.T) {
	r := Stratify(nil)
	if r.TotalCases != 0 {
		t.Fatalf("expected 0 cases, got %d", r.TotalCases)
	}
	if r.BiasAlert {
		t.Fatal("expected no bias alert for empty input")
	}
}

func TestStratify_OverallAccuracy(t *testing.T) {
	metrics := []eval.CaseMetrics{
		makeMetric(true, nil),
		makeMetric(true, nil),
		makeMetric(false, nil),
		makeMetric(false, nil),
	}
	r := Stratify(metrics)
	if r.TotalCases != 4 {
		t.Fatalf("expected 4 cases, got %d", r.TotalCases)
	}
	if math.Abs(r.OverallAccuracy-0.5) > 0.001 {
		t.Fatalf("expected overall accuracy 0.50, got %.3f", r.OverallAccuracy)
	}
}

func TestStratify_AgeGroupBreakdown(t *testing.T) {
	metrics := []eval.CaseMetrics{
		// 2 pediatric: 2/2 correct
		makeMetric(true, map[string]any{"age": 5.0}),
		makeMetric(true, map[string]any{"age": 10.0}),
		// 2 adult: 1/2 correct
		makeMetric(true, map[string]any{"age": 30.0}),
		makeMetric(false, map[string]any{"age": 40.0}),
		// 2 elderly: 0/2 correct
		makeMetric(false, map[string]any{"age": 70.0}),
		makeMetric(false, map[string]any{"age": 80.0}),
	}
	r := Stratify(metrics)

	byAge := indexStratResults(r.ByAgeGroup, func(s Stratum) string { return s.AgeGroup })

	if byAge["pediatric(<18)"].Accuracy != 1.0 {
		t.Errorf("pediatric accuracy should be 1.0, got %.2f", byAge["pediatric(<18)"].Accuracy)
	}
	if math.Abs(byAge["adult(18-64)"].Accuracy-0.5) > 0.001 {
		t.Errorf("adult accuracy should be 0.5, got %.2f", byAge["adult(18-64)"].Accuracy)
	}
	if byAge["elderly(65+)"].Accuracy != 0.0 {
		t.Errorf("elderly accuracy should be 0.0, got %.2f", byAge["elderly(65+)"].Accuracy)
	}
}

func TestStratify_BiasAlert(t *testing.T) {
	// Create a large disparity: pediatric = 1.0, adult = 0.5, elderly = 0.0
	// Gap of 1.0 > 0.10 => BiasAlert should be true
	metrics := []eval.CaseMetrics{
		makeMetric(true, map[string]any{"age": 5.0}),
		makeMetric(true, map[string]any{"age": 10.0}),
		makeMetric(true, map[string]any{"age": 30.0}),
		makeMetric(false, map[string]any{"age": 40.0}),
		makeMetric(false, map[string]any{"age": 70.0}),
		makeMetric(false, map[string]any{"age": 80.0}),
	}
	r := Stratify(metrics)
	if !r.BiasAlert {
		t.Errorf("expected BiasAlert=true, MaxDisparity=%.3f", r.MaxDisparity)
	}
	if r.MaxDisparity < 0.10 {
		t.Errorf("expected MaxDisparity > 0.10, got %.3f", r.MaxDisparity)
	}
}

func TestStratify_NoBiasAlert(t *testing.T) {
	// All strata perform identically => no disparity
	meta := map[string]any{"age": 30.0, "sex": "male"}
	metrics := []eval.CaseMetrics{
		makeMetric(true, meta),
		makeMetric(true, meta),
		makeMetric(true, meta),
	}
	r := Stratify(metrics)
	if r.BiasAlert {
		t.Errorf("expected no bias alert when all strata match, MaxDisparity=%.3f", r.MaxDisparity)
	}
}

func TestStratify_MetadataCorrectKey(t *testing.T) {
	// "correct" key in Metadata should override Accuracy=0
	metrics := []eval.CaseMetrics{
		{Accuracy: 0.0, Metadata: map[string]any{"correct": true, "age": 25.0}},
		{Accuracy: 1.0, Metadata: map[string]any{"correct": false, "age": 25.0}},
	}
	r := Stratify(metrics)
	if math.Abs(r.OverallAccuracy-0.5) > 0.001 {
		t.Fatalf("expected 0.5 accuracy using 'correct' key, got %.3f", r.OverallAccuracy)
	}
}

func TestStratify_DiagnosisMatchKey(t *testing.T) {
	metrics := []eval.CaseMetrics{
		{Accuracy: 0.0, Metadata: map[string]any{"diagnosis_match": true}},
		{Accuracy: 0.0, Metadata: map[string]any{"diagnosis_match": false}},
	}
	r := Stratify(metrics)
	if math.Abs(r.OverallAccuracy-0.5) > 0.001 {
		t.Fatalf("expected 0.5 accuracy using 'diagnosis_match' key, got %.3f", r.OverallAccuracy)
	}
}

func TestStratify_SexBreakdown(t *testing.T) {
	metrics := []eval.CaseMetrics{
		makeMetric(true, map[string]any{"sex": "male"}),
		makeMetric(true, map[string]any{"sex": "male"}),
		makeMetric(false, map[string]any{"sex": "female"}),
		makeMetric(false, map[string]any{"sex": "female"}),
	}
	r := Stratify(metrics)
	bySex := indexStratResults(r.BySex, func(s Stratum) string { return s.Sex })
	if bySex["male"].Accuracy != 1.0 {
		t.Errorf("male accuracy should be 1.0, got %.2f", bySex["male"].Accuracy)
	}
	if bySex["female"].Accuracy != 0.0 {
		t.Errorf("female accuracy should be 0.0, got %.2f", bySex["female"].Accuracy)
	}
}

func TestStratify_CIBoundsPresent(t *testing.T) {
	metrics := []eval.CaseMetrics{
		makeMetric(true, map[string]any{"sex": "male"}),
		makeMetric(false, map[string]any{"sex": "male"}),
	}
	r := Stratify(metrics)
	if len(r.BySex) == 0 {
		t.Fatal("expected at least one sex stratum")
	}
	for _, sr := range r.BySex {
		if sr.P95CI[0] < 0 || sr.P95CI[1] > 1 || sr.P95CI[0] > sr.P95CI[1] {
			t.Errorf("invalid CI [%.4f, %.4f] for stratum %v", sr.P95CI[0], sr.P95CI[1], sr.Stratum)
		}
	}
}

func TestStratify_GeneratedAtPopulated(t *testing.T) {
	r := Stratify([]eval.CaseMetrics{makeMetric(true, nil)})
	if r.GeneratedAt == "" {
		t.Fatal("expected GeneratedAt to be set")
	}
}

// ----------------------------------------------------------------------------
// helper
// ----------------------------------------------------------------------------

func indexStratResults(results []StratifiedResult, keyFn func(Stratum) string) map[string]StratifiedResult {
	out := make(map[string]StratifiedResult, len(results))
	for _, r := range results {
		out[keyFn(r.Stratum)] = r
	}
	return out
}
