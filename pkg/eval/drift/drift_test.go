package drift_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/eval/drift"
)

// makeDP is a test helper that builds a DataPoint with a zero timestamp.
func makeDP(provider, condition string, accuracy float64) drift.DataPoint {
	return drift.DataPoint{
		Timestamp: time.Now(),
		Provider:  provider,
		Condition: condition,
		Accuracy:  accuracy,
		CaseCount: 1,
	}
}

// TestNewDriftMonitor_DefaultThreshold verifies that passing 0 falls back to 5%.
func TestNewDriftMonitor_DefaultThreshold(t *testing.T) {
	m := drift.NewDriftMonitor(0)
	if m == nil {
		t.Fatal("NewDriftMonitor returned nil")
	}
	// Record a baseline and a slightly worse recent value that should NOT
	// trigger at the default 5% threshold.
	m.SetBaseline("rule", "hypertension", 0.90)
	m.Record(makeDP("rule", "hypertension", 0.87)) // 3 pp drop — below default 5%
	alerts := m.CheckDrift()
	if len(alerts) != 0 {
		t.Fatalf("expected no alerts at 3pp drop with 5%% threshold, got %d", len(alerts))
	}
}

// TestCheckDrift_NoAlertBelowThreshold checks that small drops are silent.
func TestCheckDrift_NoAlertBelowThreshold(t *testing.T) {
	m := drift.NewDriftMonitor(0.10) // 10% threshold
	m.SetBaseline("rule", "diabetes", 0.80)
	m.Record(makeDP("rule", "diabetes", 0.75)) // 5 pp drop — below 10%
	alerts := m.CheckDrift()
	if len(alerts) != 0 {
		t.Fatalf("expected no alerts, got %v", alerts)
	}
}

// TestCheckDrift_WarningAlert verifies a warning is raised when drop > threshold.
func TestCheckDrift_WarningAlert(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	m.SetBaseline("rule", "hypertension", 0.90)
	// 8 pp drop — exceeds 5% threshold but less than 10% (2×threshold).
	m.Record(makeDP("rule", "hypertension", 0.82))
	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Severity != "warning" {
		t.Errorf("expected severity 'warning', got %q", alerts[0].Severity)
	}
	if alerts[0].Provider != "rule" {
		t.Errorf("expected provider 'rule', got %q", alerts[0].Provider)
	}
	if alerts[0].Condition != "hypertension" {
		t.Errorf("expected condition 'hypertension', got %q", alerts[0].Condition)
	}
}

// TestCheckDrift_CriticalAlert verifies that a drop > 2×threshold is critical.
func TestCheckDrift_CriticalAlert(t *testing.T) {
	m := drift.NewDriftMonitor(0.05) // 5% threshold → critical at >10%
	m.SetBaseline("llm-anthropic", "asthma", 0.95)
	// 12 pp drop — exceeds 2×threshold.
	m.Record(makeDP("llm-anthropic", "asthma", 0.83))
	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Severity != "critical" {
		t.Errorf("expected severity 'critical', got %q", alerts[0].Severity)
	}
}

// TestCheckDrift_WindowAveragesLast10 verifies that only the last 10 points
// per pair are used when computing the recent average.
func TestCheckDrift_WindowAveragesLast10(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	m.SetBaseline("rule", "copd", 0.90)

	// Record 5 old perfect-accuracy points that should fall outside the window.
	for i := 0; i < 5; i++ {
		m.Record(makeDP("rule", "copd", 1.0))
	}
	// Record 10 new degraded points.
	for i := 0; i < 10; i++ {
		m.Record(makeDP("rule", "copd", 0.70)) // 20 pp drop — critical
	}
	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	// Recent average should be ≈0.70 → drop ≈0.20 → critical.
	if alerts[0].Severity != "critical" {
		t.Errorf("expected 'critical', got %q", alerts[0].Severity)
	}
	if alerts[0].Current > 0.71 || alerts[0].Current < 0.69 {
		t.Errorf("expected recent average ≈0.70, got %f", alerts[0].Current)
	}
}

// TestCheckDrift_NoDataForPair returns no alerts when there are no history
// points for a baseline pair.
func TestCheckDrift_NoDataForPair(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	m.SetBaseline("rule", "flu", 0.80)
	// No history recorded for this pair.
	alerts := m.CheckDrift()
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts with no history, got %d", len(alerts))
	}
}

// TestCheckDrift_MultipleProviders checks independent alerts per provider.
func TestCheckDrift_MultipleProviders(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	m.SetBaseline("rule", "flu", 0.80)
	m.SetBaseline("llm-openai", "flu", 0.85)

	m.Record(makeDP("rule", "flu", 0.60))       // 20 pp drop — critical
	m.Record(makeDP("llm-openai", "flu", 0.82)) // 3 pp drop — no alert

	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert (only rule), got %d", len(alerts))
	}
	if alerts[0].Provider != "rule" {
		t.Errorf("expected alert from 'rule', got %q", alerts[0].Provider)
	}
}

// TestRecord_MultipleConditions ensures points for different conditions don't
// bleed into each other's averages.
func TestRecord_MultipleConditions(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	m.SetBaseline("rule", "flu", 0.90)
	m.SetBaseline("rule", "copd", 0.80)

	m.Record(makeDP("rule", "flu", 0.50))  // 40 pp drop
	m.Record(makeDP("rule", "copd", 0.78)) // 2 pp drop — below threshold

	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert (only flu), got %d: %+v", len(alerts), alerts)
	}
	if alerts[0].Condition != "flu" {
		t.Errorf("expected condition 'flu', got %q", alerts[0].Condition)
	}
}

// TestSaveAndLoadHistory round-trips the history through disk.
func TestSaveAndLoadHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")

	m1 := drift.NewDriftMonitor(0.05)
	m1.Record(makeDP("rule", "hypertension", 0.85))
	m1.Record(makeDP("rule", "diabetes", 0.70))
	if err := m1.SaveHistory(path); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}

	m2 := drift.NewDriftMonitor(0.05)
	if err := m2.LoadHistory(path); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	m2.SetBaseline("rule", "hypertension", 0.90)
	alerts := m2.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert after round-trip, got %d", len(alerts))
	}
}

// TestLoadBaseline_Valid checks that a well-formed baseline JSON is parsed.
func TestLoadBaseline_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")

	bf := map[string]any{
		"version": 1,
		"baselines": map[string]float64{
			"rule:hypertension": 0.90,
		},
	}
	b, _ := json.MarshalIndent(bf, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	m := drift.NewDriftMonitor(0.05)
	if err := m.LoadBaseline(path); err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	m.Record(makeDP("rule", "hypertension", 0.60)) // 30 pp drop
	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert after LoadBaseline, got %d", len(alerts))
	}
}

// TestLoadBaseline_Missing returns an error for a non-existent file.
func TestLoadBaseline_Missing(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	err := m.LoadBaseline("/does/not/exist/baseline.json")
	if err == nil {
		t.Fatal("expected error for missing baseline file, got nil")
	}
}

// TestPrintReport_NoAlerts just confirms it doesn't panic.
func TestPrintReport_NoAlerts(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	m.PrintReport(nil)
	m.PrintReport([]drift.DriftAlert{})
}

// TestPrintReport_WithAlerts confirms non-empty alerts don't panic.
func TestPrintReport_WithAlerts(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	m.SetBaseline("rule", "flu", 0.90)
	m.Record(makeDP("rule", "flu", 0.60))
	alerts := m.CheckDrift()
	m.PrintReport(alerts) // should not panic
}

// TestDropFieldAccuracy checks that the Drop field equals Baseline - Current.
func TestDropFieldAccuracy(t *testing.T) {
	m := drift.NewDriftMonitor(0.05)
	m.SetBaseline("rule", "sepsis", 0.88)
	m.Record(makeDP("rule", "sepsis", 0.75)) // 13 pp drop
	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	want := a.Baseline - a.Current
	if abs(a.Drop-want) > 1e-9 {
		t.Errorf("Drop %f != Baseline %f - Current %f = %f", a.Drop, a.Baseline, a.Current, want)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
