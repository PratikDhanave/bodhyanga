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
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/microsoft/agent-framework-go/workflow/inproc"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

func main() {
	casePath := flag.String("case", "", "path to a PatientCase JSON file (default: built-in demo case)")
	tau := flag.Float64("tau", 0.85, "diagnostician confidence bar")
	maxRounds := flag.Int("max-rounds", 3, "workups before forced progression")
	policy := flag.String("policy", "allow", "admission policy: allow | deny | deny-agent=<id>")
	verbose := flag.Bool("verbose", false, "log every executor hop")
	flag.Parse()

	pol, err := parsePolicy(*policy)
	if err != nil {
		fatal(err)
	}
	recorder := audit.NewInMemory()
	boundary := admit.New(pol, recorder, nil)

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

	wf, err := pipeline.New(pipeline.Config{Tau: *tau, MaxRounds: *maxRounds, Boundary: boundary})
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
			if errors.Is(e.Error, admit.ErrDenied) {
				fmt.Fprintf(os.Stderr, "slice: hop %s denied by governance:\n", e.ExecutorID)
				for _, ev := range recorder.List() {
					if ev.Kind == audit.KindPolicyDecision && ev.Decision == string(governance.DecisionDeny) {
						fmt.Fprintf(os.Stderr, "  deny agent=%s type=%s case=%s reason=%s\n",
							ev.AgentID, ev.Type, ev.CaseID, ev.Reason)
					}
				}
				os.Exit(2)
			}
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

func parsePolicy(s string) (governance.Policy, error) {
	switch {
	case s == "allow":
		return admit.AllowAll(), nil
	case s == "deny":
		return admit.DenyAll(), nil
	case strings.HasPrefix(s, "deny-agent="):
		id := strings.TrimPrefix(s, "deny-agent=")
		if id == "" {
			return nil, fmt.Errorf("deny-agent requires an agent id")
		}
		return admit.DenyAgent(id), nil
	default:
		return nil, fmt.Errorf("unknown -policy %q (allow | deny | deny-agent=<id>)", s)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "slice:", err)
	os.Exit(1)
}
