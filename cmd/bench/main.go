// Command bench is the SD-Bench style fixture runner. It loads every case
// JSON in -cases (defaults to data/cases), drives each through the full
// diagnostic pipeline headlessly, captures the final CaseState, and prints an
// accuracy-vs-cost report — the Pareto frontier MAI-DxO frames its benchmark
// around.
//
// Usage:
//
//	go run ./cmd/bench
//	go run ./cmd/bench -cases=data/cases
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

	// FilePath is set by the loader, not by the JSON.
	FilePath string `json:"-"`
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
	Results  []result
}

func main() {
	casesDir := flag.String("cases", "data/cases", "directory holding case JSON fixtures")
	outJSON := flag.Bool("json", false, "emit JSON report on stdout instead of the table")
	timeout := flag.Duration("timeout", 3*time.Second, "wait per case for the pipeline to finalize")
	providersFlag := flag.String("providers", agents.ProviderRule,
		"comma-separated diagnostician providers: rule | llm-anthropic | llm-openai | llm-ollama")
	flag.Parse()

	fixtures, err := loadFixtures(*casesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}
	if len(fixtures) == 0 {
		fmt.Fprintln(os.Stderr, "no fixtures found")
		os.Exit(1)
	}

	providers := splitCSV(*providersFlag)
	runs := make([]providerRun, 0, len(providers))
	for _, p := range providers {
		diagFactory := func() (agent.Agent, error) { return agents.NewDiagnostician(p) }
		if _, err := diagFactory(); err != nil {
			fmt.Fprintf(os.Stderr, "provider %s skipped: %v\n", p, err)
			continue
		}
		results := make([]result, 0, len(fixtures))
		for _, f := range fixtures {
			start := time.Now()
			state, ok := runCase(f, *timeout, diagFactory)
			lat := time.Since(start)
			if !ok {
				fmt.Fprintf(os.Stderr, "[%s] case %s did not finalize within %s — skipping\n", p, f.CaseID, *timeout)
				continue
			}
			m := eval.ScoreCase(state, 11)
			results = append(results, result{Fixture: f, State: state, Metrics: m, Provider: p, Latency: lat})
		}
		runs = append(runs, providerRun{Provider: p, Results: results})
	}

	if len(runs) == 0 {
		fmt.Fprintln(os.Stderr, "no providers ran successfully")
		os.Exit(1)
	}

	if *outJSON {
		out := make([]map[string]any, 0)
		summary := make(map[string]any, len(runs))
		for _, run := range runs {
			for _, r := range run.Results {
				out = append(out, map[string]any{
					"provider":          r.Provider,
					"case_id":           r.Fixture.CaseID,
					"file":              r.Fixture.FilePath,
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
			summary[run.Provider] = summarize(run.Results)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"results": out,
			"summary": summary,
		})
		return
	}

	for _, run := range runs {
		printProviderTable(run)
	}
	if len(runs) > 1 {
		printComparison(runs)
	}
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

func loadFixtures(dir string) ([]fixture, error) {
	files, err := filepath.Glob(filepath.Join(dir, "demo_*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := make([]fixture, 0, len(files))
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var f fixture
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		f.FilePath = path
		out = append(out, f)
	}
	return out, nil
}

// runCase spins up a fresh diagnostic pipeline (HITL off — bench is headless),
// publishes the presentation, and returns the final CaseState captured from
// the supervisor's terminal `diagnosis` message. diagFactory builds the
// diagnostician agent for the run (rule-based or LLM-backed).
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
	// watching for the verifier's `diagnosis` message. This is a second
	// handler alongside the supervisor — the bus delivers to every handler.
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

func printProviderTable(run providerRun) {
	fmt.Println()
	fmt.Printf("Provider: %s\n", run.Provider)
	fmt.Println(rule(78))
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
	fmt.Println()
	s := summarize(run.Results)
	fmt.Printf("Summary [%s]: cases=%d  mean_accuracy=%.0f%%  mean_cost=$%.2f  under_budget=%.0f%%\n",
		run.Provider, s.Cases, s.MeanAccuracyPct, s.MeanCostUSD, s.UnderBudgetPct)
}

func printComparison(runs []providerRun) {
	fmt.Println()
	fmt.Println("Provider comparison")
	fmt.Println(rule(78))
	fmt.Printf("%-16s %6s %8s %8s %10s\n", "provider", "cases", "acc%", "cost$", "under-bud%")
	fmt.Println(rule(78))
	for _, run := range runs {
		s := summarize(run.Results)
		fmt.Printf("%-16s %6d %8.0f %8.2f %10.0f\n",
			run.Provider, s.Cases, s.MeanAccuracyPct, s.MeanCostUSD, s.UnderBudgetPct)
	}
}

type bench struct {
	Cases           int     `json:"cases"`
	MeanAccuracyPct float64 `json:"mean_accuracy_pct"`
	MeanCostUSD     float64 `json:"mean_cost_usd"`
	UnderBudgetPct  float64 `json:"under_budget_pct"`
}

func summarize(results []result) bench {
	if len(results) == 0 {
		return bench{}
	}
	var accSum, costSum float64
	under := 0
	for _, r := range results {
		accSum += r.Metrics.Accuracy
		costSum += r.Metrics.CostUSD
		if r.Metrics.UnderBudget {
			under++
		}
	}
	n := float64(len(results))
	return bench{
		Cases:           len(results),
		MeanAccuracyPct: 100 * accSum / n,
		MeanCostUSD:     costSum / n,
		UnderBudgetPct:  100 * float64(under) / n,
	}
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
