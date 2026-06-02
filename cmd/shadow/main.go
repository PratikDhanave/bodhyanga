// Command shadow runs shadow-mode evaluation against a directory of case
// fixtures. For each fixture, it drives the AI diagnostic pipeline and
// compares the AI's working diagnosis to the fixture's gold_diagnosis
// (treating it as the clinician's decision). Concordance records are
// written to a JSONL output file and a summary report is printed to stderr.
//
// Usage:
//
//	go run ./cmd/shadow
//	go run ./cmd/shadow -fixtures-dir=data/cases -mode=sync -output=shadow.jsonl -verbose
//	go run ./cmd/shadow -provider=llm-anthropic -mode=sync
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/PratikDhanave/bodh/pkg/shadow"
)

// fixture mirrors the case file format used by data/cases/*.json. We only
// need the fields relevant to shadow evaluation.
type fixture struct {
	CaseID         string `json:"case_id"`
	Condition      string `json:"condition"`
	ChiefComplaint string `json:"chief_complaint"`
	HPI            string `json:"hpi"`
	Demographics   string `json:"demographics"`
	Vitals         string `json:"vitals"`
	GoldDiagnosis  string `json:"gold_diagnosis"`
	MaxCostUSD     float64 `json:"max_cost_usd"`
}

func main() {
	fixturesDir := flag.String("fixtures-dir", "data/cases", "directory containing case fixture JSON files")
	modeStr := flag.String("mode", "sync", "shadow mode: async or sync")
	output := flag.String("output", "shadow_concordance.jsonl", "output JSONL path for concordance records")
	verbose := flag.Bool("verbose", false, "print per-case results to stderr")
	provider := flag.String("provider", "rule", "diagnostician provider: rule | llm-anthropic | llm-openai | llm-ollama")
	timeout := flag.Duration("timeout", 10*time.Second, "per-case timeout")
	flag.Parse()

	var mode shadow.Mode
	switch *modeStr {
	case "async":
		mode = shadow.ModeAsync
	case "sync":
		mode = shadow.ModeSync
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q (use async or sync)\n", *modeStr)
		os.Exit(1)
	}

	fixtures, err := loadFixtures(*fixturesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load fixtures: %v\n", err)
		os.Exit(1)
	}
	if len(fixtures) == 0 {
		fmt.Fprintln(os.Stderr, "no fixtures found")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Shadow evaluation: %d fixtures, provider=%s, mode=%s\n",
		len(fixtures), *provider, *modeStr)

	ctx := context.Background()
	orch, bus, err := shadow.BuildShadowPipeline(ctx, *provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build pipeline: %v\n", err)
		os.Exit(1)
	}
	_ = orch

	ledger, err := shadow.NewConcordanceLedger(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create ledger: %v\n", err)
		os.Exit(1)
	}
	defer ledger.Close()

	runner := shadow.NewShadowRunner(orch, bus, ledger, mode, *timeout)

	for i, f := range fixtures {
		if f.GoldDiagnosis == "" {
			if *verbose {
				fmt.Fprintf(os.Stderr, "[%d/%d] %s: skipped (no gold diagnosis)\n",
					i+1, len(fixtures), f.CaseID)
			}
			continue
		}

		// Build the case input as JSON for the shadow runner.
		caseInput, err := json.Marshal(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%d/%d] %s: marshal error: %v\n",
				i+1, len(fixtures), f.CaseID, err)
			continue
		}

		rec, err := runner.RunShadow(ctx, string(caseInput), f.GoldDiagnosis)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%d/%d] %s: %v\n",
				i+1, len(fixtures), f.CaseID, err)
			continue
		}

		if *verbose && rec != nil {
			symbol := "=="
			if !rec.Concordant {
				symbol = "!="
			}
			fmt.Fprintf(os.Stderr, "[%d/%d] %s: AI=%q %s clinician=%q (%.0fms)\n",
				i+1, len(fixtures), f.CaseID,
				rec.AIDecision, symbol, rec.ClinicianDecision,
				float64(rec.LatencyMs))
		}
	}

	// For async mode, give background goroutines time to complete.
	if mode == shadow.ModeAsync {
		fmt.Fprintln(os.Stderr, "Waiting for async shadow runs to complete...")
		time.Sleep(5 * time.Second)
	}

	// Print the concordance report.
	report := runner.Report()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Shadow Concordance Report")
	fmt.Fprintln(os.Stderr, "========================")
	fmt.Fprintf(os.Stderr, "Total cases:      %d\n", report.Total)
	fmt.Fprintf(os.Stderr, "Concordant:       %d\n", report.Concordant)
	fmt.Fprintf(os.Stderr, "Discordant:       %d\n", report.Discordant)
	fmt.Fprintf(os.Stderr, "Concordance rate: %.1f%%\n", report.ConcordanceRate*100)
	fmt.Fprintf(os.Stderr, "Avg AI confidence: %.2f\n", report.AvgAIConfidence)
	fmt.Fprintf(os.Stderr, "Avg latency:      %.0fms\n", report.AvgLatencyMs)
	fmt.Fprintf(os.Stderr, "\nResults written to: %s\n", *output)

	// Also emit the report as JSON to stdout for machine consumption.
	_ = json.NewEncoder(os.Stdout).Encode(report)
}

// loadFixtures reads all demo_*.json and bench_*.json files from the
// directory and returns them as fixture slices.
func loadFixtures(dir string) ([]fixture, error) {
	patterns := []string{
		filepath.Join(dir, "demo_*.json"),
		filepath.Join(dir, "bench_*.json"),
	}

	var paths []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)

	out := make([]fixture, 0, len(paths))
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var f fixture
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		f.CaseID = caseIDOrFilename(f.CaseID, path)
		out = append(out, f)
	}
	return out, nil
}

// caseIDOrFilename returns the case ID from the fixture, or derives one
// from the filename if the fixture doesn't specify one.
func caseIDOrFilename(id, path string) string {
	if id != "" {
		return id
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return base[:len(base)-len(ext)]
}
