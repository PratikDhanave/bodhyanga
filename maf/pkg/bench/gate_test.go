package bench

import (
	"context"
	"testing"
)

// TestGate_CommittedSuiteMeetsBaseline is the CI gate: running the committed
// suite must not regress against the committed baseline. This runs on every
// `go test ./pkg/bench` — regression protection without invoking the binary
// (SC-003). If a pipeline change drops accuracy, this fails.
func TestGate_CommittedSuiteMeetsBaseline(t *testing.T) {
	fixtures, err := LoadManifest("../../data/cases/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	results, err := RunSuite(context.Background(), fixtures, Config{})
	if err != nil {
		t.Fatal(err)
	}
	summary := Aggregate(results)

	baseline, err := LoadBaseline("../../data/cases/baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	if baseline == nil {
		t.Fatal("committed baseline missing — run `go run ./cmd/bench -update-baseline`")
	}
	if regressions := DetectRegression(baseline, summary); len(regressions) != 0 {
		t.Errorf("committed suite regressed against baseline: %v", regressions)
	}
}
