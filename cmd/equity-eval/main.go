// Command equity-eval loads a JSON file of eval.CaseMetrics (as produced by
// cmd/bench -json) and runs a stratified equity analysis.
//
// Usage:
//
//	go run ./cmd/equity-eval -input results.json
//	go run ./cmd/equity-eval -input results.json -fail-on-bias
//	go run ./cmd/equity-eval -input results.json -verbose
//
// The input file must be a JSON array of eval.CaseMetrics objects. Demographic
// fields are read from each element's "metadata" object — see
// pkg/eval/equity.ExtractStratum for the supported keys.
//
// Exit codes:
//
//	0 — success (no bias alert or -fail-on-bias not set)
//	1 — bias alert detected and -fail-on-bias is set, or input error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/PratikDhanave/bodhyanga/pkg/eval/equity"
)

func main() {
	inputPath := flag.String("input", "", "path to JSON file containing []eval.CaseMetrics")
	failOnBias := flag.Bool("fail-on-bias", false, "exit 1 if BiasAlert is true (disparity > 10 pp)")
	verbose := flag.Bool("verbose", false, "print per-stratum detail tables")
	flag.Parse()

	if *inputPath == "" {
		fmt.Fprintln(os.Stderr, "usage: equity-eval -input <file.json> [-fail-on-bias] [-verbose]")
		os.Exit(1)
	}

	metrics, err := loadMetrics(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}

	if len(metrics) == 0 {
		fmt.Fprintln(os.Stderr, "no case metrics found in input file")
		os.Exit(1)
	}

	report := equity.Stratify(metrics)

	if *verbose {
		equity.PrintEquityReport(report)
	} else {
		printSummary(report)
	}

	if *failOnBias && report.BiasAlert {
		fmt.Fprintln(os.Stderr, "FAIL: bias alert — disparity exceeds 10 percentage points")
		os.Exit(1)
	}
}

// loadMetrics reads the JSON file.  It accepts two shapes:
//
//  1. A plain JSON array:  [ { "accuracy": 1.0, "metadata": {...} }, ... ]
//  2. The cmd/bench -json envelope:  { "results": [ {...}, ... ], ... }
//     In the envelope form each element may carry top-level bench fields
//     (accuracy, diagnosis_match, provider, case_id, …) and an optional
//     nested "metadata" object; we synthesize a CaseMetrics from the
//     top-level keys so the bench output can be fed directly.
func loadMetrics(path string) ([]eval.CaseMetrics, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try plain array first.
	var plain []eval.CaseMetrics
	if err := json.Unmarshal(b, &plain); err == nil {
		return plain, nil
	}

	// Try bench envelope: { "results": [...], ... }
	var envelope struct {
		Results []benchResult `json:"results"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return nil, fmt.Errorf("input must be a JSON array of CaseMetrics or a bench envelope: %w", err)
	}
	out := make([]eval.CaseMetrics, 0, len(envelope.Results))
	for _, r := range envelope.Results {
		out = append(out, r.toCaseMetrics())
	}
	return out, nil
}

// benchResult mirrors the per-case JSON objects emitted by cmd/bench -json.
// Only the fields needed by equity analysis are mapped.
type benchResult struct {
	Accuracy       float64        `json:"accuracy"`
	DiagnosisMatch bool           `json:"diagnosis_match"`
	CostUSD        float64        `json:"cost_usd"`
	UnderBudget    bool           `json:"under_budget"`
	RPMAlertCount  int            `json:"rpm_alert_count"`
	// Metadata may carry demographic keys or be absent.
	Metadata       map[string]any `json:"metadata"`
	// Top-level bench fields that get merged into Metadata when Metadata
	// doesn't already contain them.
	CaseID         string         `json:"case_id"`
	Provider       string         `json:"provider"`
}

func (r benchResult) toCaseMetrics() eval.CaseMetrics {
	meta := r.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}
	// Mirror the diagnosis_match flag into Metadata so equity.extractCorrect
	// can use it.
	if _, exists := meta["diagnosis_match"]; !exists {
		meta["diagnosis_match"] = r.DiagnosisMatch
	}
	if r.CaseID != "" {
		if _, exists := meta["case_id"]; !exists {
			meta["case_id"] = r.CaseID
		}
	}
	return eval.CaseMetrics{
		DiagnosisMatch: r.DiagnosisMatch,
		Accuracy:       r.Accuracy,
		CostUSD:        r.CostUSD,
		UnderBudget:    r.UnderBudget,
		RPMAlertCount:  r.RPMAlertCount,
		Metadata:       meta,
	}
}

func printSummary(r equity.EquityReport) {
	sep := "--------------------------------------------------------------------------------"
	fmt.Println(sep)
	fmt.Println("EQUITY SUMMARY")
	fmt.Println(sep)
	fmt.Printf("Generated:        %s\n", r.GeneratedAt)
	fmt.Printf("Total cases:      %d\n", r.TotalCases)
	fmt.Printf("Overall accuracy: %.1f%%\n", r.OverallAccuracy*100)
	fmt.Printf("Max disparity:    %.1f pp\n", r.MaxDisparity*100)
	if r.BiasAlert {
		fmt.Println("Bias alert:       YES (> 10 pp) — run with -verbose for full breakdown")
	} else {
		fmt.Println("Bias alert:       no")
	}
	fmt.Println(sep)
}
