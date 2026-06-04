package equity

import (
	"bytes"
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// WilsonCI with n=0
// ---------------------------------------------------------------------------

func TestWilsonCI_NZero(t *testing.T) {
	lo, hi := WilsonCI(0, 0, 1.96)
	if lo != 0 || hi != 0 {
		t.Errorf("expected (0, 0), got (%f, %f)", lo, hi)
	}
}

func TestWilsonCI_AllCorrect_Coverage(t *testing.T) {
	lo, hi := WilsonCI(10, 10, 1.96)
	if lo <= 0 || hi < lo {
		t.Errorf("unexpected bounds: (%f, %f)", lo, hi)
	}
}

// ---------------------------------------------------------------------------
// numericVal: int32 type coverage
// ---------------------------------------------------------------------------

func TestNumericVal_Int32(t *testing.T) {
	m := map[string]any{"age": int32(42)}
	v, ok := numericVal(m, "age")
	if !ok || v != 42.0 {
		t.Errorf("expected 42.0/true, got %f/%v", v, ok)
	}
}

func TestNumericVal_Float32(t *testing.T) {
	m := map[string]any{"age": float32(42.5)}
	v, ok := numericVal(m, "age")
	if !ok || v < 42 {
		t.Errorf("expected ~42.5/true, got %f/%v", v, ok)
	}
}

func TestNumericVal_Int64(t *testing.T) {
	m := map[string]any{"age": int64(99)}
	v, ok := numericVal(m, "age")
	if !ok || v != 99.0 {
		t.Errorf("expected 99.0/true, got %f/%v", v, ok)
	}
}

func TestNumericVal_String(t *testing.T) {
	m := map[string]any{"age": "forty"}
	_, ok := numericVal(m, "age")
	if ok {
		t.Error("expected false for string value")
	}
}

func TestNumericVal_Missing(t *testing.T) {
	m := map[string]any{}
	_, ok := numericVal(m, "age")
	if ok {
		t.Error("expected false for missing key")
	}
}

// ---------------------------------------------------------------------------
// disparityWithinGroup: edge cases
// ---------------------------------------------------------------------------

func TestDisparityWithinGroup_SingleStratum(t *testing.T) {
	g := []StratifiedResult{
		{CaseCount: 10, Accuracy: 0.8},
	}
	d := disparityWithinGroup(g)
	if d != 0 {
		t.Errorf("expected 0 for single stratum, got %f", d)
	}
}

func TestDisparityWithinGroup_AllZeroCaseCount(t *testing.T) {
	g := []StratifiedResult{
		{CaseCount: 0, Accuracy: 0.9},
		{CaseCount: 0, Accuracy: 0.5},
	}
	d := disparityWithinGroup(g)
	if d != 0 {
		t.Errorf("expected 0 when all case counts are 0, got %f", d)
	}
}

// ---------------------------------------------------------------------------
// PrintEquityReport
// ---------------------------------------------------------------------------

func TestPrintEquityReport(t *testing.T) {
	report := EquityReport{
		TotalCases:      10,
		OverallAccuracy: 0.85,
		ByAgeGroup: []StratifiedResult{
			{
				Stratum:   Stratum{AgeGroup: "adult(18-64)"},
				CaseCount: 5,
				Correct:   4,
				Accuracy:  0.8,
				P95CI:     [2]float64{0.3, 0.97},
			},
		},
		BySex:        []StratifiedResult{},
		ByRace:       []StratifiedResult{},
		MaxDisparity: 0.15,
		BiasAlert:    true,
		GeneratedAt:  "2025-01-01T00:00:00Z",
	}

	// Capture stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	PrintEquityReport(report)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if output == "" {
		t.Error("expected non-empty output")
	}
	if !bytes.Contains([]byte(output), []byte("BIAS ALERT")) {
		t.Error("expected BIAS ALERT in output")
	}
}

func TestPrintEquityReport_NoBiasAlert(t *testing.T) {
	report := EquityReport{
		TotalCases:      10,
		OverallAccuracy: 0.9,
		ByAgeGroup:      []StratifiedResult{},
		BySex:           []StratifiedResult{},
		ByRace:          []StratifiedResult{},
		MaxDisparity:    0.05,
		BiasAlert:       false,
		GeneratedAt:     "2025-01-01T00:00:00Z",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	PrintEquityReport(report)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if bytes.Contains([]byte(output), []byte("BIAS ALERT:       YES")) {
		t.Error("should not have BIAS ALERT")
	}
}

// ---------------------------------------------------------------------------
// printStratifiedTable
// ---------------------------------------------------------------------------

func TestPrintStratifiedTable(t *testing.T) {
	results := []StratifiedResult{
		{
			Stratum:   Stratum{AgeGroup: "adult(18-64)"},
			CaseCount: 5,
			Correct:   4,
			Accuracy:  0.8,
			P95CI:     [2]float64{0.3, 0.97},
		},
	}
	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	printStratifiedTable("Test", results, func(s Stratum) string { return s.AgeGroup })
	w.Close()
	os.Stdout = old
}

// ---------------------------------------------------------------------------
// ExtractStratum: edge cases with int types for age
// ---------------------------------------------------------------------------

func TestExtractStratum_IntAge(t *testing.T) {
	m := map[string]any{"age": int(72)}
	s := ExtractStratum(m)
	if s.AgeGroup != "elderly(65+)" {
		t.Errorf("expected elderly(65+), got %q", s.AgeGroup)
	}
}

func TestExtractStratum_GenderFallback(t *testing.T) {
	m := map[string]any{"gender": "Female"}
	s := ExtractStratum(m)
	if s.Sex != "female" {
		t.Errorf("expected female, got %q", s.Sex)
	}
}

func TestExtractStratum_EthnicityOnly(t *testing.T) {
	m := map[string]any{"ethnicity": "Hispanic"}
	s := ExtractStratum(m)
	if s.RaceEthnicity != "hispanic" {
		t.Errorf("expected hispanic, got %q", s.RaceEthnicity)
	}
}

func TestExtractStratum_RaceAndEthnicity(t *testing.T) {
	m := map[string]any{"race": "White", "ethnicity": "Hispanic"}
	s := ExtractStratum(m)
	expected := "white / hispanic"
	if s.RaceEthnicity != expected {
		t.Errorf("expected %q, got %q", expected, s.RaceEthnicity)
	}
}

