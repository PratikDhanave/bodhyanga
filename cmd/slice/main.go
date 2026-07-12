// cmd/slice runs one case through the MARA slice pipeline.
//
// Usage:
//
//	go run ./cmd/slice                      # built-in demo case
//	go run ./cmd/slice -case case.json      # case from file
//	go run ./cmd/slice -tau 0.9 -max-rounds 2 -verbose
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/microsoft/agent-framework-go/workflow/inproc"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

func main() {
	casePath := flag.String("case", "", "path to a PatientCase JSON file (default: built-in demo case)")
	tau := flag.Float64("tau", 0.85, "diagnostician confidence bar")
	maxRounds := flag.Int("max-rounds", 3, "workups before forced progression")
	verbose := flag.Bool("verbose", false, "log every executor hop")
	flag.Parse()

	pc := agents.PatientCase{
		CaseID:   "demo-001",
		Symptoms: []string{"chest pain"},
		History:  []string{"hypertension"},
	}
	if *casePath != "" {
		data, err := os.ReadFile(*casePath)
		if err != nil {
			fatal(err)
		}
		if err := json.Unmarshal(data, &pc); err != nil {
			fatal(fmt.Errorf("parse %s: %w", *casePath, err))
		}
	}

	wf, err := pipeline.New(pipeline.Config{Tau: *tau, MaxRounds: *maxRounds})
	if err != nil {
		fatal(err)
	}

	ctx := context.Background()
	run, err := inproc.Default.RunStreaming(ctx, wf, pc)
	if err != nil {
		fatal(err)
	}
	defer func() { _ = run.Close(ctx) }()

	hash := agents.HashCaseID(pc.CaseID)
	fmt.Printf("case %s → pipeline (τ=%.2f, max %d rounds)\n", hash, *tau, *maxRounds)

	for evt, err := range run.WatchStream(ctx) {
		if err != nil {
			fatal(err)
		}
		switch e := evt.(type) {
		case workflow.ExecutorInvokedEvent:
			if *verbose {
				fmt.Printf("  hop: %s\n", e.ExecutorID)
			}
		case workflow.ExecutorFailedEvent:
			fatal(fmt.Errorf("executor %s failed: %w", e.ExecutorID, e.Error))
		case workflow.ErrorEvent:
			fatal(e.Error)
		case agents.ForcedProgressionEvent:
			fmt.Printf("  forced progression: case %s at confidence %.2f after %d rounds\n",
				e.CaseIDHash, e.Confidence, e.Rounds)
		case workflow.OutputEvent:
			plan, ok := e.Output.(agents.CarePlan)
			if !ok {
				fatal(fmt.Errorf("unexpected output type %T", e.Output))
			}
			printPlan(plan)
		}
	}
}

func printPlan(p agents.CarePlan) {
	fmt.Printf("\ndiagnosis: %s", p.Diagnosis)
	if p.Forced {
		fmt.Print("  [LOW CONFIDENCE — forced progression]")
	}
	fmt.Printf("\ncare plan for case %s:\n", agents.HashCaseID(p.CaseID))
	for i, s := range p.Steps {
		fmt.Printf("  %d. %s\n", i+1, s)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "slice:", err)
	os.Exit(1)
}
