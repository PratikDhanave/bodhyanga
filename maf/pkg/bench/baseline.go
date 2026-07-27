package bench

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// accuracyToleranceP is the allowed accuracy drop (percentage points) before a
// regression fires — bodh's accuracyToleranceP, accuracy-only.
const accuracyToleranceP = 1.0

// Baseline is the committed accuracy record (no mean_cost_usd — accuracy-only).
type Baseline struct {
	Version   int             `json:"version"`
	Providers []BaselineEntry `json:"providers"`
}

// BaselineEntry is one provider's recorded accuracy.
type BaselineEntry struct {
	Provider    string  `json:"provider"`
	Cases       int     `json:"cases"`
	AccuracyPct float64 `json:"accuracy_pct"`
	RecordedAt  string  `json:"recorded_at"`
}

// LoadBaseline reads a baseline file. A MISSING file is informational, not an
// error: it returns (nil, nil) so the gate can print a "create one" hint. A
// malformed file IS an error.
func LoadBaseline(path string) (*Baseline, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("bench: read baseline %q: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("bench: parse baseline %q: %w", path, err)
	}
	return &b, nil
}

// DetectRegression returns a message per provider whose current accuracy fell
// more than the tolerance below its baseline. A nil baseline (none recorded)
// yields no regressions.
func DetectRegression(b *Baseline, s Summary) []string {
	if b == nil {
		return nil
	}
	var msgs []string
	for _, e := range b.Providers {
		if e.Provider != s.Provider {
			continue
		}
		if s.AccuracyPct+accuracyToleranceP < e.AccuracyPct {
			msgs = append(msgs, fmt.Sprintf(
				"accuracy regression for %s: baseline %.1f%% → now %.1f%% (tolerance %.1fpp)",
				e.Provider, e.AccuracyPct, s.AccuracyPct, accuracyToleranceP))
		}
	}
	return msgs
}

// WriteBaseline overwrites path with the current summary as the new baseline.
// now is injected for deterministic tests; cmd passes time.Now().
func WriteBaseline(path string, s Summary, now time.Time) error {
	b := Baseline{
		Version: 2,
		Providers: []BaselineEntry{{
			Provider:    s.Provider,
			Cases:       s.Cases,
			AccuracyPct: s.AccuracyPct,
			RecordedAt:  now.UTC().Format(time.RFC3339),
		}},
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
