// cmd/bench-suite runs the Bodhyanga performance benchmark suite and prints a
// formatted results table.
//
// Usage:
//
//	go run ./cmd/bench-suite [flags]
//
// Flags:
//
//	-benchtime=5s     Per-benchmark run time (passed to go test -benchtime).
//	-json             Emit results as JSON instead of the formatted table.
//	-compare=FILE     Path to a baseline JSON file; adds a delta column.
//
// Example (table output):
//
//	go run ./cmd/bench-suite -benchtime=5s
//
// Example (JSON + compare):
//
//	go run ./cmd/bench-suite -json > baseline.json
//	# ... make changes ...
//	go run ./cmd/bench-suite -json -compare=baseline.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// BenchResult holds the parsed output of a single go benchmark line.
type BenchResult struct {
	Name       string  `json:"name"`
	Iterations int64   `json:"iterations"`
	NsPerOp    float64 `json:"ns_per_op"`
	// Extra metrics extracted from -benchmem / custom ReportMetric lines.
	AllocsPerOp int64              `json:"allocs_per_op,omitempty"`
	BytesPerOp  int64              `json:"bytes_per_op,omitempty"`
	Extra       map[string]float64 `json:"extra,omitempty"`
}

// SuiteResults is the top-level JSON output.
type SuiteResults struct {
	RunAt     time.Time     `json:"run_at"`
	BenchTime string        `json:"bench_time"`
	Results   []BenchResult `json:"results"`
}

var (
	flagBenchTime = flag.String("benchtime", "3s", "per-benchmark run time (e.g. 5s)")
	flagJSON      = flag.Bool("json", false, "emit JSON instead of a table")
	flagCompare   = flag.String("compare", "", "baseline JSON file to compare against")
)

func main() {
	flag.Parse()

	results, err := runBenchmarks(*flagBenchTime)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bench-suite: %v\n", err)
		os.Exit(1)
	}

	suite := SuiteResults{
		RunAt:     time.Now().UTC(),
		BenchTime: *flagBenchTime,
		Results:   results,
	}

	if *flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(suite); err != nil {
			fmt.Fprintf(os.Stderr, "bench-suite: encode json: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Load baseline if provided.
	var baseline []BenchResult
	if *flagCompare != "" {
		baseline, err = loadBaseline(*flagCompare)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bench-suite: load baseline: %v\n", err)
			os.Exit(1)
		}
	}

	printTable(results, baseline)
}

// runBenchmarks invokes `go test ./pkg/bench/... -bench=. -benchtime=T`
// and returns parsed results.
func runBenchmarks(benchtime string) ([]BenchResult, error) {
	args := []string{
		"test",
		"./pkg/bench/...",
		"-bench=.",
		"-benchmem",
		"-benchtime=" + benchtime,
		"-run=^$", // skip non-benchmark tests
		"-count=1",
	}
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go test: %w", err)
	}
	return parseBenchOutput(string(out)), nil
}

// benchLineRe matches standard go benchmark output lines.
// Example: BenchmarkBusThroughput/subscribers_1-8   500000   2345 ns/op   48 B/op   1 allocs/op
var benchLineRe = regexp.MustCompile(
	`^(Benchmark\S+)\s+(\d+)\s+([\d.]+)\s+ns/op(.*)$`,
)

// extraMetricRe matches extra ReportMetric output.
// Example: 12345.00 cases/s
var extraMetricRe = regexp.MustCompile(`([\d.]+)\s+(\S+)`)

func parseBenchOutput(output string) []BenchResult {
	var results []BenchResult
	for _, line := range strings.Split(output, "\n") {
		m := benchLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		r := BenchResult{
			Name: m[1],
		}
		r.Iterations, _ = strconv.ParseInt(m[2], 10, 64)
		r.NsPerOp, _ = strconv.ParseFloat(m[3], 64)

		// Parse trailing extras (B/op, allocs/op, custom metrics).
		tail := strings.TrimSpace(m[4])
		for _, em := range extraMetricRe.FindAllStringSubmatch(tail, -1) {
			val, _ := strconv.ParseFloat(em[1], 64)
			switch em[2] {
			case "B/op":
				r.BytesPerOp = int64(val)
			case "allocs/op":
				r.AllocsPerOp = int64(val)
			default:
				if r.Extra == nil {
					r.Extra = make(map[string]float64)
				}
				r.Extra[em[2]] = val
			}
		}
		results = append(results, r)
	}
	return results
}

func loadBaseline(path string) ([]BenchResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var suite SuiteResults
	if err := json.NewDecoder(f).Decode(&suite); err != nil {
		return nil, err
	}
	return suite.Results, nil
}

// printTable prints a human-readable table of benchmark results.
//
// Example output (with baseline):
//
//	Component                         | Throughput       | p50 Latency | p99 Latency | Delta ns/op
//	Bus (1 subscriber)                | -                | -           | -           | -2.3%
//	Orchestrator (3-hop pipeline)     | 45k cases/s      | -           | -           | +1.1%
//	...
func printTable(results, baseline []BenchResult) {
	// Build a map for baseline lookups.
	baseMap := make(map[string]BenchResult, len(baseline))
	for _, r := range baseline {
		baseMap[r.Name] = r
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()

	hasCompare := len(baseline) > 0

	if hasCompare {
		fmt.Fprintf(w, "%-55s\t%14s\t%13s\t%13s\t%12s\n",
			"Component", "Throughput", "p50 Latency", "p99 Latency", "Delta ns/op")
	} else {
		fmt.Fprintf(w, "%-55s\t%14s\t%13s\t%13s\n",
			"Component", "Throughput", "p50 Latency", "p99 Latency")
	}

	sep := strings.Repeat("-", 110)
	fmt.Fprintln(w, sep)

	for _, r := range results {
		throughput := "-"
		if v, ok := r.Extra["cases/s"]; ok {
			throughput = humanRate(v, "cases/s")
		} else if v, ok := r.Extra["msg/s"]; ok {
			throughput = humanRate(v, "msg/s")
		}

		p50 := "-"
		if v, ok := r.Extra["p50_us"]; ok {
			p50 = fmt.Sprintf("%.0fµs", v)
		}
		p99 := "-"
		if v, ok := r.Extra["p99_us"]; ok {
			p99 = fmt.Sprintf("%.0fµs", v)
		}

		// Human-friendly name: strip "Benchmark" prefix and CPU suffix.
		name := friendlyName(r.Name)

		if hasCompare {
			delta := "-"
			if base, ok := baseMap[r.Name]; ok && base.NsPerOp > 0 {
				pct := (r.NsPerOp - base.NsPerOp) / base.NsPerOp * 100
				sign := "+"
				if pct < 0 {
					sign = ""
				}
				delta = fmt.Sprintf("%s%.1f%%", sign, pct)
			}
			fmt.Fprintf(w, "%-55s\t%14s\t%13s\t%13s\t%12s\n",
				name, throughput, p50, p99, delta)
		} else {
			fmt.Fprintf(w, "%-55s\t%14s\t%13s\t%13s\n",
				name, throughput, p50, p99)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Run at: %s  benchtime: %s\n",
		time.Now().UTC().Format(time.RFC3339), *flagBenchTime)
}

// humanRate formats a rate as "250k msg/s", "1.2M cases/s", etc.
func humanRate(rate float64, unit string) string {
	switch {
	case rate >= 1e9:
		return fmt.Sprintf("%.1fG %s", rate/1e9, unit)
	case rate >= 1e6:
		return fmt.Sprintf("%.1fM %s", rate/1e6, unit)
	case rate >= 1e3:
		return fmt.Sprintf("%.0fk %s", rate/1e3, unit)
	default:
		return fmt.Sprintf("%.0f %s", rate, unit)
	}
}

// cpuSuffixRe strips the goroutine-count suffix like "-8" or "-16".
var cpuSuffixRe = regexp.MustCompile(`-\d+$`)

// friendlyName converts "BenchmarkBusThroughput/subscribers_1-8" to
// "BusThroughput/subscribers_1".
func friendlyName(name string) string {
	name = strings.TrimPrefix(name, "Benchmark")
	name = cpuSuffixRe.ReplaceAllString(name, "")
	name = strings.ReplaceAll(name, "_", " ")
	return name
}
