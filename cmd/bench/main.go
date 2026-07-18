// cmd/bench runs the SD-Bench case suite through the rule pipeline and reports
// accuracy (specs/005). Offline: rule engine only, no network, no credentials.
//
//	go run ./cmd/bench                      # default manifest, human table
//	go run ./cmd/bench -json                # machine-readable summary
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PratikDhanave/bodh-maf/pkg/bench"
)

func main() {
	manifest := flag.String("manifest", "data/cases/manifest.json", "case manifest path")
	baselinePath := flag.String("baseline", "data/cases/baseline.json", "accuracy baseline path")
	tau := flag.Float64("tau", 0.85, "diagnostician confidence bar")
	maxRounds := flag.Int("max-rounds", 3, "workups before forced progression")
	asJSON := flag.Bool("json", false, "emit the summary as JSON")
	failOnRegression := flag.Bool("fail-on-regression", false, "exit non-zero if accuracy regressed vs baseline")
	updateBaseline := flag.Bool("update-baseline", false, "overwrite the baseline with this run's accuracy, then exit")
	flag.Parse()

	fixtures, err := bench.LoadManifest(*manifest)
	if err != nil {
		fatal(err)
	}
	results, err := bench.RunSuite(context.Background(), fixtures, bench.Config{Tau: *tau, MaxRounds: *maxRounds})
	if err != nil {
		fatal(err)
	}
	summary := bench.Aggregate(results)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"summary": summary, "cases": results})
	} else {
		bench.WriteTable(os.Stdout, results, summary)
	}

	// Refresh mode: overwrite the baseline and skip the gate.
	if *updateBaseline {
		if err := bench.WriteBaseline(*baselinePath, summary, time.Now()); err != nil {
			fatal(err)
		}
		fmt.Printf("baseline updated: %s (accuracy %.1f%%)\n", *baselinePath, summary.AccuracyPct)
		return
	}

	// Regression gate.
	baseline, err := bench.LoadBaseline(*baselinePath)
	if err != nil {
		fatal(err)
	}
	if baseline == nil {
		fmt.Printf("no baseline at %s — run with -update-baseline to create one\n", *baselinePath)
		return
	}
	regressions := bench.DetectRegression(baseline, summary)
	for _, r := range regressions {
		fmt.Fprintln(os.Stderr, "REGRESSION:", r)
	}
	if len(regressions) > 0 && *failOnRegression {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bench:", err)
	os.Exit(1)
}
