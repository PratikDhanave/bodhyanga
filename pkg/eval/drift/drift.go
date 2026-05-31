// Package drift monitors diagnostic accuracy over time and alerts when
// performance degrades beyond a configurable threshold.
//
// Typical usage:
//
//	m := drift.NewDriftMonitor(0.05) // 5% drop triggers warning
//	m.LoadBaseline("data/cases/drift_baseline.json")
//	m.Record(drift.DataPoint{...})
//	alerts := m.CheckDrift()
//	m.PrintReport(alerts)
package drift

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DataPoint is one accuracy measurement for a provider/condition pair at a
// point in time.
type DataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Condition string    `json:"condition"`
	Provider  string    `json:"provider"`
	Accuracy  float64   `json:"accuracy"`
	CaseCount int       `json:"case_count"`
}

// DriftAlert is raised when the recent average accuracy for a
// provider/condition pair drops more than Threshold below the baseline.
type DriftAlert struct {
	Condition string  `json:"condition"`
	Provider  string  `json:"provider"`
	Baseline  float64 `json:"baseline"`
	Current   float64 `json:"current"`
	Drop      float64 `json:"drop"`
	Threshold float64 `json:"threshold"`
	Message   string  `json:"message"`
	// Severity is "warning" for a drop > threshold and "critical" for a drop
	// more than twice the threshold (i.e. > 10% when threshold is 5%).
	Severity string `json:"severity"`
}

// baselineFile is the on-disk format for baseline accuracies.
type baselineFile struct {
	Version   int                `json:"version"`
	Baselines map[string]float64 `json:"baselines"` // key: "provider:condition"
}

// historyFile is the on-disk format for the recorded history.
type historyFile struct {
	Version int         `json:"version"`
	Points  []DataPoint `json:"points"`
}

// DriftMonitor tracks diagnostic accuracy over time and raises alerts when
// recent performance degrades relative to a known baseline.
type DriftMonitor struct {
	// baseline maps "provider:condition" to its reference accuracy (0–1).
	baseline map[string]float64
	history  []DataPoint
	// threshold is the minimum fractional drop that triggers a warning alert
	// (default 0.05 = 5 percentage points).
	threshold float64
}

// NewDriftMonitor creates a DriftMonitor with the given drop threshold.
// Pass 0 to use the default of 0.05 (5%).
func NewDriftMonitor(threshold float64) *DriftMonitor {
	if threshold <= 0 {
		threshold = 0.05
	}
	return &DriftMonitor{
		baseline:  make(map[string]float64),
		threshold: threshold,
	}
}

// key returns the canonical map key for a provider/condition pair.
func key(provider, condition string) string {
	return provider + ":" + condition
}

// LoadBaseline reads a JSON file of baseline accuracies produced by
// SaveBaseline or by an external tool. Unknown keys are ignored so old files
// remain forward-compatible.
//
// JSON shape:
//
//	{
//	  "version": 1,
//	  "baselines": { "rule:hypertension": 0.85, ... }
//	}
func (m *DriftMonitor) LoadBaseline(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("drift: read baseline %s: %w", path, err)
	}
	var bf baselineFile
	if err := json.Unmarshal(b, &bf); err != nil {
		return fmt.Errorf("drift: parse baseline %s: %w", path, err)
	}
	for k, v := range bf.Baselines {
		m.baseline[k] = v
	}
	return nil
}

// SetBaseline sets the baseline accuracy for a provider/condition pair
// directly, without loading from disk.
func (m *DriftMonitor) SetBaseline(provider, condition string, accuracy float64) {
	m.baseline[key(provider, condition)] = accuracy
}

// Record appends a new data point to the in-memory history.
func (m *DriftMonitor) Record(dp DataPoint) {
	m.history = append(m.history, dp)
}

// recentAverage computes the mean accuracy of the last (up to) n data points
// that match the given provider and condition. Returns (mean, count).
func (m *DriftMonitor) recentAverage(provider, condition string, n int) (float64, int) {
	// Walk backwards to gather the most recent points.
	var sum float64
	count := 0
	for i := len(m.history) - 1; i >= 0 && count < n; i-- {
		dp := m.history[i]
		if dp.Provider == provider && dp.Condition == condition {
			sum += dp.Accuracy
			count++
		}
	}
	if count == 0 {
		return 0, 0
	}
	return sum / float64(count), count
}

// CheckDrift compares the recent average accuracy (last 10 points per
// provider/condition pair) against the loaded baseline. An alert is raised
// whenever the drop exceeds the monitor's threshold.
func (m *DriftMonitor) CheckDrift() []DriftAlert {
	const window = 10

	// Collect every unique provider/condition pair that appears in the
	// baseline so we can check each one independently.
	checked := make(map[string]bool)
	var alerts []DriftAlert

	for k, baseAcc := range m.baseline {
		if checked[k] {
			continue
		}
		checked[k] = true

		// Reverse-parse the key.
		provider, condition := splitKey(k)
		recent, count := m.recentAverage(provider, condition, window)
		if count == 0 {
			// No recent data for this pair — nothing to alert on.
			continue
		}

		drop := baseAcc - recent
		if drop <= m.threshold {
			continue
		}

		severity := "warning"
		if drop > m.threshold*2 {
			severity = "critical"
		}

		alerts = append(alerts, DriftAlert{
			Condition: condition,
			Provider:  provider,
			Baseline:  baseAcc,
			Current:   recent,
			Drop:      drop,
			Threshold: m.threshold,
			Message: fmt.Sprintf(
				"%s/%s accuracy dropped %.1f pp (baseline %.1f%% → recent %.1f%%, threshold %.1f pp)",
				provider, condition,
				drop*100, baseAcc*100, recent*100, m.threshold*100,
			),
			Severity: severity,
		})
	}
	return alerts
}

// SaveHistory persists the in-memory history slice to a JSON file.
// The parent directory is created if it does not exist.
func (m *DriftMonitor) SaveHistory(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("drift: mkdir %s: %w", filepath.Dir(path), err)
	}
	hf := historyFile{Version: 1, Points: m.history}
	b, err := json.MarshalIndent(hf, "", "  ")
	if err != nil {
		return fmt.Errorf("drift: marshal history: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("drift: write history %s: %w", path, err)
	}
	return nil
}

// LoadHistory appends data points from a saved history file into the monitor.
func (m *DriftMonitor) LoadHistory(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("drift: read history %s: %w", path, err)
	}
	var hf historyFile
	if err := json.Unmarshal(b, &hf); err != nil {
		return fmt.Errorf("drift: parse history %s: %w", path, err)
	}
	m.history = append(m.history, hf.Points...)
	return nil
}

// PrintReport writes a human-readable drift report to stdout.
// When alerts is empty it prints a brief "no drift detected" summary.
func (m *DriftMonitor) PrintReport(alerts []DriftAlert) {
	fmt.Printf("Drift Monitor Report — threshold %.1f pp\n", m.threshold*100)
	fmt.Println("--------------------------------------------------------------")
	if len(alerts) == 0 {
		fmt.Println("No drift detected.")
		return
	}
	for _, a := range alerts {
		icon := "[WARNING]"
		if a.Severity == "critical" {
			icon = "[CRITICAL]"
		}
		fmt.Printf("%s %s\n", icon, a.Message)
	}
	fmt.Printf("\n%d alert(s) total.\n", len(alerts))
}

// splitKey is the inverse of key(): "provider:condition" → ("provider",
// "condition"). If there is no colon the whole string is treated as the
// provider and the condition is empty.
func splitKey(k string) (provider, condition string) {
	for i := 0; i < len(k); i++ {
		if k[i] == ':' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}
