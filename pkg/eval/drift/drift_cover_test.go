package drift

import (
	"os"
	"path/filepath"
	"testing"
)

// --- LoadBaseline edge cases ---

func TestLoadBaseline_FileNotFound(t *testing.T) {
	m := NewDriftMonitor(0.05)
	err := m.LoadBaseline("/nonexistent/path/baseline.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadBaseline_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewDriftMonitor(0.05)
	err := m.LoadBaseline(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- SaveHistory / LoadHistory edge cases ---

func TestSaveHistory_InvalidDir(t *testing.T) {
	m := NewDriftMonitor(0.05)
	err := m.SaveHistory("/proc/nonexistent/subdir/history.json")
	if err == nil {
		t.Fatal("expected error for invalid directory")
	}
}

func TestLoadHistory_FileNotFound(t *testing.T) {
	m := NewDriftMonitor(0.05)
	err := m.LoadHistory("/nonexistent/history.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadHistory_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_hist.json")
	if err := os.WriteFile(path, []byte("!!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewDriftMonitor(0.05)
	err := m.LoadHistory(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSaveAndLoadHistory_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")

	m := NewDriftMonitor(0.05)
	m.Record(DataPoint{Provider: "rule", Condition: "flu", Accuracy: 0.9, CaseCount: 10})
	m.Record(DataPoint{Provider: "rule", Condition: "flu", Accuracy: 0.8, CaseCount: 5})

	if err := m.SaveHistory(path); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}

	m2 := NewDriftMonitor(0.05)
	if err := m2.LoadHistory(path); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(m2.history) != 2 {
		t.Errorf("expected 2 points after LoadHistory, got %d", len(m2.history))
	}
}

// --- splitKey edge case ---

func TestSplitKey_NoColon(t *testing.T) {
	provider, condition := splitKey("nocolon")
	if provider != "nocolon" {
		t.Errorf("provider = %q, want %q", provider, "nocolon")
	}
	if condition != "" {
		t.Errorf("condition = %q, want empty", condition)
	}
}

// --- CheckDrift with critical severity ---

func TestCheckDrift_CriticalSeverity(t *testing.T) {
	m := NewDriftMonitor(0.05)
	m.SetBaseline("rule", "flu", 0.9)
	// Record points well below baseline (drop > 2*threshold = 0.10)
	for i := 0; i < 5; i++ {
		m.Record(DataPoint{Provider: "rule", Condition: "flu", Accuracy: 0.7, CaseCount: 1})
	}
	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Severity != "critical" {
		t.Errorf("severity = %q, want %q", alerts[0].Severity, "critical")
	}
}

// --- CheckDrift with no recent data ---

func TestCheckDrift_NoRecentData(t *testing.T) {
	m := NewDriftMonitor(0.05)
	m.SetBaseline("rule", "asthma", 0.85)
	// No data points recorded
	alerts := m.CheckDrift()
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts with no data, got %d", len(alerts))
	}
}

// --- SaveHistory to read-only file ---

func TestSaveHistory_WriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readonly-subdir")
	// Create a directory where we try to write a file, then make it read-only
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(path, "history.json")
	// Create the file then make the directory read-only
	if err := os.WriteFile(filePath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the file itself read-only
	if err := os.Chmod(filePath, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filePath, 0o644) // cleanup

	m := NewDriftMonitor(0.05)
	m.Record(DataPoint{Provider: "rule", Condition: "flu", Accuracy: 0.9})
	err := m.SaveHistory(filePath)
	if err == nil {
		t.Fatal("expected error writing to read-only file")
	}
}

// --- CheckDrift: warning severity (not critical) ---

func TestCheckDrift_WarningSeverity(t *testing.T) {
	m := NewDriftMonitor(0.05)
	m.SetBaseline("rule", "flu", 0.9)
	// Drop of 0.08 (> 0.05 threshold but < 0.10 = 2*threshold)
	for i := 0; i < 5; i++ {
		m.Record(DataPoint{Provider: "rule", Condition: "flu", Accuracy: 0.82, CaseCount: 1})
	}
	alerts := m.CheckDrift()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Severity != "warning" {
		t.Errorf("severity = %q, want %q", alerts[0].Severity, "warning")
	}
}

// --- PrintReport with alerts ---

func TestPrintReport_WithAlerts(t *testing.T) {
	m := NewDriftMonitor(0.05)
	m.SetBaseline("rule", "flu", 0.9)
	for i := 0; i < 5; i++ {
		m.Record(DataPoint{Provider: "rule", Condition: "flu", Accuracy: 0.7})
	}
	alerts := m.CheckDrift()
	// Should not panic.
	m.PrintReport(alerts)
}

// --- PrintReport with no alerts ---

func TestPrintReport_NoAlerts(t *testing.T) {
	m := NewDriftMonitor(0.05)
	m.PrintReport(nil)
}
