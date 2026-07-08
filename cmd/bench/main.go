// Command bench is the SD-Bench style fixture runner. It loads a manifest of
// case fixtures, drives each through the full diagnostic pipeline headlessly
// for every requested provider, captures the final CaseState, and prints an
// accuracy-vs-cost report — the Pareto frontier MAI-DxO frames its benchmark
// around. It also supports per-provider baseline regression gating so CI can
// fail when accuracy drops or cost climbs above a known floor/ceiling.
//
// Usage:
//
//	go run ./cmd/bench
//	go run ./cmd/bench -manifest=data/cases/manifest.json
//	go run ./cmd/bench -providers=rule,llm-anthropic,llm-openai,llm-ollama,llm-kronk
//	go run ./cmd/bench -fail-on-regression
//	go run ./cmd/bench -update-baseline
//	go run ./cmd/bench -json   # machine-readable output
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/protocol"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// fixture is the canonical in-memory case payload. The fields populated from
// the per-case JSON file feed the diagnostic pipeline; the fields populated
// from the manifest entry (Tags, ManifestPath) drive bench-level filtering
// and bookkeeping. Manifest-level GoldDiagnosis / MaxCostUSD / Condition
// override the per-case file when present so the manifest is the
// authoritative source of bench expectations.
type fixture struct {
	CaseID          string                   `json:"case_id"`
	Condition       string                   `json:"condition"`
	ChiefComplaint  string                   `json:"chief_complaint"`
	HPI             string                   `json:"hpi"`
	Demographics    string                   `json:"demographics"`
	Vitals          string                   `json:"vitals"`
	GoldDiagnosis   string                   `json:"gold_diagnosis"`
	MaxCostUSD      float64                  `json:"max_cost_usd"`
	RPMObservations []medical.RPMObservation `json:"rpm_observations"`

	// Populated by the loader from manifest metadata / filesystem path.
	FilePath string   `json:"-"`
	Tags     []string `json:"-"`
}

// manifestEntry mirrors data/cases/manifest.json entries.
type manifestEntry struct {
	ID            string   `json:"id"`
	Path          string   `json:"path"`
	GoldDiagnosis string   `json:"gold_diagnosis"`
	MaxCostUSD    float64  `json:"max_cost_usd"`
	Condition     string   `json:"condition"`
	Tags          []string `json:"tags"`
}

type manifestFile struct {
	Version     int             `json:"version"`
	Description string          `json:"description"`
	Cases       []manifestEntry `json:"cases"`
}

type result struct {
	Fixture  fixture
	State    medical.CaseState
	Metrics  eval.CaseMetrics
	Provider string
	Latency  time.Duration
}

type providerRun struct {
	Provider string
	Skipped  bool
	SkipMsg  string
	Results  []result
}

// providerSummary aggregates a provider's per-case results into the SD-Bench
// columns the headline table reports. Pareto is the accuracy / cost ratio
// (Accuracy fraction divided by mean cost in USD) — a higher number is
// strictly better, capturing "more correct diagnoses per dollar spent".
type providerSummary struct {
	Provider       string  `json:"provider"`
	Cases          int     `json:"cases"`
	AccuracyPct    float64 `json:"accuracy_pct"`
	MeanCostUSD    float64 `json:"mean_cost_usd"`
	MeanSteps      float64 `json:"mean_steps"`
	UnderBudgetPct float64 `json:"under_budget_pct"`
	MeanRPMAlerts  float64 `json:"mean_rpm_alerts"`
	Pareto         float64 `json:"pareto"`
}

// baselineEntry is one provider's pass/fail thresholds recorded in
// data/cases/baseline.json and consulted by -fail-on-regression. AccuracyPct
// is a floor (regression = current < baseline - tolerance); MeanCostUSD is a
// ceiling (regression = current > baseline + tolerance).
type baselineEntry struct {
	Provider    string  `json:"provider"`
	Cases       int     `json:"cases"`
	AccuracyPct float64 `json:"accuracy_pct"`
	MeanCostUSD float64 `json:"mean_cost_usd"`
	RecordedAt  string  `json:"recorded_at"`
}

type baselineFile struct {
	Version   int             `json:"version"`
	Providers []baselineEntry `json:"providers"`
}

// regressionTolerance allows tiny numerical jitter without flagging a
// regression. Accuracy must drop by more than 1 pp and cost must rise by
// more than 1¢ for the gate to fire.
const (
	accuracyToleranceP = 1.0  // percentage points
	costToleranceUSD   = 0.01 // dollars
)

func main() {
	manifestPath := flag.String("manifest", "data/cases/manifest.json", "path to case manifest JSON")
	casesDir := flag.String("cases", "", "(legacy) directory of demo_*.json fixtures; only used when -manifest is empty")
	outJSON := flag.Bool("json", false, "emit JSON report on stdout instead of the table")
	timeout := flag.Duration("timeout", 5*time.Second, "wait per case for the pipeline to finalize")
	providersFlag := flag.String("providers", agents.ProviderRule,
		"comma-separated diagnostician providers: rule | llm-anthropic | llm-openai | llm-ollama | llm-kronk")
	baselinePath := flag.String("baseline", "data/cases/baseline.json", "path to per-provider baseline thresholds")
	failOnRegression := flag.Bool("fail-on-regression", false, "exit non-zero when any provider falls below its baseline")
	updateBaseline := flag.Bool("update-baseline", false, "overwrite -baseline with current results and exit 0")
	flag.Parse()

	fixtures, err := loadCases(*manifestPath, *casesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}
	if len(fixtures) == 0 {
		fmt.Fprintln(os.Stderr, "no fixtures found")
		os.Exit(1)
	}

	providers := splitCSV(*providersFlag)
	runs := runAllProviders(providers, fixtures, *timeout)

	if len(runs) == 0 {
		fmt.Fprintln(os.Stderr, "no providers ran successfully")
		os.Exit(1)
	}

	summaries := summarizeRuns(runs)

	if *updateBaseline {
		if err := writeBaseline(*baselinePath, summaries); err != nil {
			fmt.Fprintf(os.Stderr, "write baseline: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wrote baseline for %d provider(s) to %s\n", len(summaries), *baselinePath)
	}

	if *outJSON {
		emitJSON(runs, summaries)
	} else {
		for _, run := range runs {
			if run.Skipped {
				continue
			}
			printProviderTable(run)
		}
		printHeadlineTable(summaries)
	}

	// Regression gating runs whether or not we just rewrote the baseline so
	// the operator sees the diff in the same invocation. -update-baseline
	// implies "the current run IS the baseline" so we skip the gate.
	if !*updateBaseline {
		regressions, err := checkRegressions(*baselinePath, summaries)
		if err != nil {
			// Missing baseline is informational, not fatal.
			fmt.Fprintf(os.Stderr, "baseline check: %v\n", err)
		}
		if len(regressions) > 0 {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "REGRESSIONS DETECTED:")
			for _, r := range regressions {
				fmt.Fprintf(os.Stderr, "  - %s\n", r)
			}
			if *failOnRegression {
				os.Exit(1)
			}
		}
	}
}

// loadCases reads cases from the manifest when manifestPath points at a real
// file. When no manifest is available (or it can't be parsed), and casesDir
// is non-empty, it falls back to globbing demo_*.json fixtures for
// backward compatibility with the original cmd/bench shape.
func loadCases(manifestPath, casesDir string) ([]fixture, error) {
	if manifestPath != "" {
		if _, err := os.Stat(manifestPath); err == nil {
			return loadManifest(manifestPath)
		} else if casesDir == "" && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if casesDir == "" {
		casesDir = filepath.Dir(manifestPath)
		if casesDir == "" || casesDir == "." {
			casesDir = "data/cases"
		}
	}
	return loadFixturesGlob(casesDir)
}

// loadManifest reads the manifest file and resolves every case it references,
// merging manifest-level metadata (gold diagnosis, max cost, condition, tags)
// over the per-case JSON. Paths in the manifest are resolved relative to the
// manifest file itself so the runner works no matter where it's invoked from.
func loadManifest(path string) ([]fixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m manifestFile
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if len(m.Cases) == 0 {
		return nil, fmt.Errorf("manifest %s declares no cases", path)
	}
	manifestDir := filepath.Dir(path)
	out := make([]fixture, 0, len(m.Cases))
	for _, entry := range m.Cases {
		if entry.Path == "" {
			return nil, fmt.Errorf("manifest entry %s missing path", entry.ID)
		}
		casePath := entry.Path
		if !filepath.IsAbs(casePath) {
			// Try manifest-relative first, then cwd-relative as a fallback so
			// users can invoke either from the repo root or from elsewhere.
			rel := filepath.Join(manifestDir, filepath.Base(casePath))
			if _, err := os.Stat(rel); err == nil {
				casePath = rel
			}
		}
		f, err := readFixtureFile(casePath)
		if err != nil {
			return nil, fmt.Errorf("manifest case %s: %w", entry.ID, err)
		}
		// Manifest metadata overrides per-file values so the manifest is the
		// single source of truth for bench expectations.
		if entry.ID != "" {
			f.CaseID = entry.ID
		}
		if entry.GoldDiagnosis != "" {
			f.GoldDiagnosis = entry.GoldDiagnosis
		}
		if entry.MaxCostUSD > 0 {
			f.MaxCostUSD = entry.MaxCostUSD
		}
		if entry.Condition != "" {
			f.Condition = entry.Condition
		}
		f.Tags = entry.Tags
		out = append(out, f)
	}
	return out, nil
}

// loadFixturesGlob preserves the original demo_*.json globbing path so older
// invocations without a manifest still work.
func loadFixturesGlob(dir string) ([]fixture, error) {
	files, err := filepath.Glob(filepath.Join(dir, "demo_*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := make([]fixture, 0, len(files))
	for _, path := range files {
		f, err := readFixtureFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func readFixtureFile(path string) (fixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return fixture{}, err
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		return fixture{}, fmt.Errorf("%s: %w", path, err)
	}
	f.FilePath = path
	return f, nil
}

// runAllProviders walks the requested providers in order and runs every
// fixture against each, skipping providers whose credentials aren't set. The
// returned slice preserves provider order so JSON / table output is stable.
func runAllProviders(providers []string, fixtures []fixture, timeout time.Duration) []providerRun {
	runs := make([]providerRun, 0, len(providers))
	for _, p := range providers {
		// Construct once up front to validate credentials; we'll rebuild per
		// case inside runCase so each case gets a fresh agent instance.
		if _, err := agents.NewDiagnostician(p); err != nil {
			fmt.Fprintf(os.Stderr, "provider %s skipped: %v\n", p, err)
			runs = append(runs, providerRun{Provider: p, Skipped: true, SkipMsg: err.Error()})
			continue
		}
		factory := func() (agent.Agent, error) { return agents.NewDiagnostician(p) }
		results := make([]result, 0, len(fixtures))
		for _, f := range fixtures {
			start := time.Now()
			state, ok := runCase(f, timeout, factory)
			lat := time.Since(start)
			if !ok {
				fmt.Fprintf(os.Stderr, "[%s] case %s did not finalize within %s — skipping\n",
					p, f.CaseID, timeout)
				continue
			}
			// ScoreCase uses state.GoldDiagnosis / MaxCostUSD so make sure
			// those reflect the manifest values, not whatever the upstream
			// agents may have left behind.
			if state.GoldDiagnosis == "" {
				state.GoldDiagnosis = f.GoldDiagnosis
			}
			if state.MaxCostUSD == 0 {
				state.MaxCostUSD = f.MaxCostUSD
			}
			m := eval.ScoreCase(state, 11)
			results = append(results, result{
				Fixture: f, State: state, Metrics: m, Provider: p, Latency: lat,
			})
		}
		runs = append(runs, providerRun{Provider: p, Results: results})
	}
	return runs
}

// runCase spins up a fresh diagnostic pipeline (HITL off — bench is headless),
// publishes the presentation, and returns the final CaseState captured from
// the supervisor's terminal `diagnosis` message. diagFactory builds the
// diagnostician agent for the run (rule-based or LLM-backed). The done channel
// + select{} pattern replaces the time.Sleep used in cmd/demo so dozens of
// cases finish as fast as the pipeline lets them.
func runCase(f fixture, timeout time.Duration, diagFactory func() (agent.Agent, error)) (medical.CaseState, bool) {
	logger := observability.NewStdLogger()
	env := &orchestration.SimpleEnvironment{Logger: logger, Clock: observability.SystemClock{}}
	reg := registry.NewInMemory()
	bus := comm.NewInMemoryBus()
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 65536},
		governance.RequireCaseIDPolicy{},
		governance.MaxDiagnosticCostPolicy{},
		governance.ResearchDisclaimerPolicy{},
	)

	diag, err := diagFactory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build diagnostician: %v\n", err)
		return medical.CaseState{}, false
	}

	ctx := context.Background()
	for _, a := range []agent.Agent{
		&agents.IntakeAgent{},
		&agents.RPMMonitorAgent{},
		&agents.DiagnosticSupervisor{},
		&agents.QuestionerAgent{},
		&agents.TestPlannerAgent{},
		&agents.CostGuardianAgent{},
		diag,
		&agents.ReasoningVerifierAgent{},
	} {
		_ = reg.Register(ctx, a)
	}
	orchestration.NewOrchestrator(reg, bus, policy, env).Start(ctx)

	// Capture the final state by subscribing to the supervisor's own ID and
	// watching for the verifier's `diagnosis` message. The bus delivers to
	// every handler, so this sits alongside the real supervisor handler.
	done := make(chan medical.CaseState, 1)
	var once sync.Once
	bus.Subscribe("diagnostic_supervisor", func(_ context.Context, msg protocol.Message) {
		if msg.Type != "diagnosis" {
			return
		}
		state, err := medical.DecodeState(msg.Content)
		if err != nil {
			return
		}
		once.Do(func() { done <- state })
	})

	pres := medical.PatientPresentation{
		ChiefComplaint:  f.ChiefComplaint,
		HPI:             f.HPI,
		Demographics:    f.Demographics,
		Vitals:          f.Vitals,
		Condition:       f.Condition,
		RPMObservations: f.RPMObservations,
	}
	meta := map[string]any{
		"case_id":        f.CaseID,
		"trace_id":       "bench-" + f.CaseID,
		"max_cost_usd":   f.MaxCostUSD,
		"gold_diagnosis": f.GoldDiagnosis,
		"message_steps":  11,
	}
	bus.Publish(ctx, agent.NewMessage("user", "intake", agent.RoleUser,
		"presentation", medical.EncodePresentation(pres), meta))

	select {
	case state := <-done:
		return state, true
	case <-time.After(timeout):
		return medical.CaseState{}, false
	}
}

// summarizeRuns reduces every successful providerRun to the aggregate row
// printed in the headline table and written to the baseline.
func summarizeRuns(runs []providerRun) []providerSummary {
	out := make([]providerSummary, 0, len(runs))
	for _, run := range runs {
		if run.Skipped || len(run.Results) == 0 {
			continue
		}
		out = append(out, summarizeProvider(run))
	}
	return out
}

func summarizeProvider(run providerRun) providerSummary {
	n := len(run.Results)
	if n == 0 {
		return providerSummary{Provider: run.Provider}
	}
	var (
		accSum, costSum, stepsSum, alertsSum float64
		under                                int
	)
	for _, r := range run.Results {
		accSum += r.Metrics.Accuracy
		costSum += r.Metrics.CostUSD
		stepsSum += float64(r.Metrics.MessageSteps)
		alertsSum += float64(r.Metrics.RPMAlertCount)
		if r.Metrics.UnderBudget {
			under++
		}
	}
	nf := float64(n)
	accFrac := accSum / nf
	meanCost := costSum / nf
	pareto := 0.0
	if meanCost > 0 {
		pareto = accFrac / meanCost
	}
	return providerSummary{
		Provider:       run.Provider,
		Cases:          n,
		AccuracyPct:    100 * accFrac,
		MeanCostUSD:    meanCost,
		MeanSteps:      stepsSum / nf,
		UnderBudgetPct: 100 * float64(under) / nf,
		MeanRPMAlerts:  alertsSum / nf,
		Pareto:         pareto,
	}
}

// checkRegressions compares current summaries against a baseline. Returns one
// human-readable string per provider that fell below its baseline. A missing
// baseline is not an error; a malformed baseline is.
func checkRegressions(path string, summaries []providerSummary) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no baseline at %s (run with -update-baseline to create one)", path)
		}
		return nil, err
	}
	var bf baselineFile
	if err := json.Unmarshal(b, &bf); err != nil {
		return nil, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return detectRegressions(bf, summaries), nil
}

func detectRegressions(baseline baselineFile, summaries []providerSummary) []string {
	byProvider := make(map[string]baselineEntry, len(baseline.Providers))
	for _, p := range baseline.Providers {
		byProvider[p.Provider] = p
	}
	var out []string
	for _, s := range summaries {
		base, ok := byProvider[s.Provider]
		if !ok {
			continue
		}
		if s.AccuracyPct+accuracyToleranceP < base.AccuracyPct {
			out = append(out, fmt.Sprintf(
				"%s accuracy %.1f%% < baseline %.1f%% (tolerance %.1fpp)",
				s.Provider, s.AccuracyPct, base.AccuracyPct, accuracyToleranceP))
		}
		if s.MeanCostUSD-costToleranceUSD > base.MeanCostUSD {
			out = append(out, fmt.Sprintf(
				"%s mean cost $%.2f > baseline $%.2f (tolerance $%.2f)",
				s.Provider, s.MeanCostUSD, base.MeanCostUSD, costToleranceUSD))
		}
	}
	return out
}

func writeBaseline(path string, summaries []providerSummary) error {
	bf := baselineFile{Version: 1}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range summaries {
		bf.Providers = append(bf.Providers, baselineEntry{
			Provider:    s.Provider,
			Cases:       s.Cases,
			AccuracyPct: s.AccuracyPct,
			MeanCostUSD: s.MeanCostUSD,
			RecordedAt:  now,
		})
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(bf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// formatHeadlineTable returns the markdown-style provider comparison table as
// a string. Exposed for tests.
func formatHeadlineTable(summaries []providerSummary) string {
	var sb strings.Builder
	sb.WriteString("Provider         | Cases | Accuracy | Mean Cost | Mean Steps | Under Budget | RPM Alerts | Pareto\n")
	sb.WriteString("-----------------|-------|----------|-----------|------------|--------------|------------|-------\n")
	for _, s := range summaries {
		fmt.Fprintf(&sb, "%-16s | %5d | %7.1f%% | $%8.2f | %10.1f | %11.0f%% | %10.1f | %6.3f\n",
			s.Provider, s.Cases, s.AccuracyPct, s.MeanCostUSD, s.MeanSteps,
			s.UnderBudgetPct, s.MeanRPMAlerts, s.Pareto)
	}
	return sb.String()
}

func printHeadlineTable(summaries []providerSummary) {
	if len(summaries) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("Headline comparison")
	fmt.Println(rule(95))
	fmt.Print(formatHeadlineTable(summaries))
}

func printProviderTable(run providerRun) {
	fmt.Println()
	fmt.Printf("Provider: %s\n", run.Provider)
	fmt.Println(rule(118))
	fmt.Printf("%-22s %-32s %-32s %6s %8s %6s %8s\n",
		"case_id", "gold_diagnosis", "working_diagnosis", "acc", "cost", "budg%", "latency")
	fmt.Println(rule(118))
	for _, r := range run.Results {
		fmt.Printf("%-22s %-32s %-32s %6.0f $%-7.0f %5.0f%% %7s\n",
			truncate(r.Fixture.CaseID, 22),
			truncate(r.Fixture.GoldDiagnosis, 32),
			truncate(r.State.WorkingDiagnosis, 32),
			r.Metrics.Accuracy*100,
			r.Metrics.CostUSD,
			r.Metrics.CostUtilization*100,
			r.Latency.Round(time.Millisecond),
		)
	}
}

func emitJSON(runs []providerRun, summaries []providerSummary) {
	out := make([]map[string]any, 0)
	for _, run := range runs {
		if run.Skipped {
			continue
		}
		for _, r := range run.Results {
			out = append(out, map[string]any{
				"provider":          r.Provider,
				"case_id":           r.Fixture.CaseID,
				"file":              r.Fixture.FilePath,
				"tags":              r.Fixture.Tags,
				"gold_diagnosis":    r.Fixture.GoldDiagnosis,
				"working_diagnosis": r.State.WorkingDiagnosis,
				"accuracy":          r.Metrics.Accuracy,
				"cost_usd":          r.Metrics.CostUSD,
				"cost_utilization":  r.Metrics.CostUtilization,
				"under_budget":      r.Metrics.UnderBudget,
				"rpm_alert_count":   r.Metrics.RPMAlertCount,
				"diagnosis_match":   r.Metrics.DiagnosisMatch,
				"latency_ms":        r.Latency.Milliseconds(),
			})
		}
	}
	skips := make([]map[string]string, 0)
	for _, run := range runs {
		if run.Skipped {
			skips = append(skips, map[string]string{
				"provider": run.Provider, "reason": run.SkipMsg,
			})
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"results": out,
		"summary": summaries,
		"skipped": skips,
	})
}

func splitCSV(s string) []string {
	out := make([]string, 0)
	cur := ""
	for _, r := range s {
		switch r {
		case ',', ' ':
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		default:
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func rule(n int) string {
	s := make([]byte, n)
	for i := range s {
		s[i] = '-'
	}
	return string(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
