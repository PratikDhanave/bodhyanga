// Command drift-monitor loads a bench output JSON, compares it to a baseline
// file, and reports diagnostic accuracy drift alerts.
//
// Usage:
//
//	go run ./cmd/drift-monitor -baseline data/cases/drift_baseline.json \
//	                           -results  bench_output.json
//	go run ./cmd/drift-monitor -baseline data/cases/drift_baseline.json \
//	                           -results  bench_output.json \
//	                           -threshold 0.10 -fail-on-critical
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/eval/drift"
)

// benchResult mirrors the per-case entry emitted by cmd/bench -json.
// Only the fields needed for drift monitoring are decoded; the rest are
// silently ignored.
type benchResult struct {
	Provider  string  `json:"provider"`
	Condition string  `json:"condition"`
	Accuracy  float64 `json:"accuracy"`
	CaseID    string  `json:"case_id"`
}

// benchOutput is the top-level JSON object that cmd/bench -json writes.
type benchOutput struct {
	Results []benchResult `json:"results"`
}

func main() {
	baselinePath := flag.String("baseline", "data/cases/drift_baseline.json",
		"path to the drift baseline JSON file")
	resultsPath := flag.String("results", "",
		"path to the bench -json output file to compare against the baseline")
	threshold := flag.Float64("threshold", 0.05,
		"fractional accuracy drop that triggers a warning alert (default 0.05 = 5pp)")
	failOnCritical := flag.Bool("fail-on-critical", false,
		"exit non-zero when any critical alert is found")
	flag.Parse()

	if *resultsPath == "" {
		fmt.Fprintln(os.Stderr, "drift-monitor: -results is required")
		flag.Usage()
		os.Exit(2)
	}

	// ── Load results ────────────────────────────────────────────────────────
	resultsData, err := os.ReadFile(*resultsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "drift-monitor: read results %s: %v\n", *resultsPath, err)
		os.Exit(1)
	}
	var bo benchOutput
	if err := json.Unmarshal(resultsData, &bo); err != nil {
		fmt.Fprintf(os.Stderr, "drift-monitor: parse results %s: %v\n", *resultsPath, err)
		os.Exit(1)
	}
	if len(bo.Results) == 0 {
		fmt.Fprintln(os.Stderr, "drift-monitor: no results found in bench output")
		os.Exit(1)
	}

	// ── Build monitor ────────────────────────────────────────────────────────
	m := drift.NewDriftMonitor(*threshold)

	if err := m.LoadBaseline(*baselinePath); err != nil {
		// A missing baseline is informational — not a hard error — because the
		// first run won't have one. Warn and exit cleanly.
		fmt.Fprintf(os.Stderr, "drift-monitor: %v\n", err)
		fmt.Fprintln(os.Stderr, "drift-monitor: no baseline loaded — run cannot detect drift")
		os.Exit(0)
	}

	// ── Record each bench result as a DataPoint ──────────────────────────────
	now := time.Now()
	for _, r := range bo.Results {
		m.Record(drift.DataPoint{
			Timestamp: now,
			Provider:  r.Provider,
			Condition: r.Condition,
			Accuracy:  r.Accuracy,
			CaseCount: 1,
		})
	}

	// ── Check and report ─────────────────────────────────────────────────────
	alerts := m.CheckDrift()
	m.PrintReport(alerts)

	// ── Exit code ────────────────────────────────────────────────────────────
	if *failOnCritical {
		for _, a := range alerts {
			if a.Severity == "critical" {
				os.Exit(1)
			}
		}
	}
}
