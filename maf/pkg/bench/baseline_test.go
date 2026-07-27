package bench

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadBaseline_MalformedIsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaseline(p); err == nil {
		t.Error("malformed baseline should error")
	}
}

func TestDetectRegression_IgnoresOtherProviders(t *testing.T) {
	base := &Baseline{Providers: []BaselineEntry{{Provider: "llm", AccuracyPct: 99}}}
	// summary is for "rule"; the "llm" baseline entry must not fire.
	if got := DetectRegression(base, Summary{Provider: "rule", AccuracyPct: 10}); len(got) != 0 {
		t.Errorf("cross-provider baseline should not regress, got %v", got)
	}
}

func TestDetectRegression(t *testing.T) {
	base := &Baseline{Providers: []BaselineEntry{{Provider: "rule", AccuracyPct: 90}}}

	// At baseline → no regression.
	if got := DetectRegression(base, Summary{Provider: "rule", AccuracyPct: 90}); len(got) != 0 {
		t.Errorf("no regression expected at baseline, got %v", got)
	}
	// Within tolerance (0.5pp drop) → no regression.
	if got := DetectRegression(base, Summary{Provider: "rule", AccuracyPct: 89.5}); len(got) != 0 {
		t.Errorf("0.5pp drop within tolerance, got %v", got)
	}
	// Beyond tolerance (2pp drop) → regression.
	if got := DetectRegression(base, Summary{Provider: "rule", AccuracyPct: 88}); len(got) != 1 {
		t.Errorf("2pp drop should regress, got %v", got)
	}
	// Nil baseline → no regression (informational path).
	if got := DetectRegression(nil, Summary{Provider: "rule", AccuracyPct: 10}); got != nil {
		t.Errorf("nil baseline yields no regression, got %v", got)
	}
}

func TestLoadBaseline_MissingIsInformational(t *testing.T) {
	b, err := LoadBaseline(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing baseline should not error, got %v", err)
	}
	if b != nil {
		t.Errorf("missing baseline should be nil, got %+v", b)
	}
}

// WriteBaseline round-trips and a subsequent gate sees no regression (SC-004).
func TestWriteBaseline_RoundTripsAndClearsGate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "baseline.json")
	s := Summary{Provider: "rule", Cases: 4, Scored: 4, Matches: 4, AccuracyPct: 100}
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if err := WriteBaseline(path, s, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBaseline(path)
	if err != nil || loaded == nil {
		t.Fatalf("reload = %+v, %v", loaded, err)
	}
	e := loaded.Providers[0]
	if e.Provider != "rule" || e.Cases != 4 || e.AccuracyPct != 100 || e.RecordedAt != "2026-07-18T12:00:00Z" {
		t.Errorf("round-trip mismatch: %+v", e)
	}
	if got := DetectRegression(loaded, s); len(got) != 0 {
		t.Errorf("just-written baseline must not regress, got %v", got)
	}
}
