// cmd/slice runs one case through the MARA slice pipeline.
//
// Usage:
//
//	go run ./cmd/slice                      # built-in demo case
//	go run ./cmd/slice -case case.json      # case from file
//	go run ./cmd/slice -policy deny         # admission demo (exit code 2)
//	go run ./cmd/slice -trail               # print the audit trail
//	go run ./cmd/slice -tau 0.9 -max-rounds 2 -verbose
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

func main() {
	casePath := flag.String("case", "", "path to a PatientCase JSON file (default: built-in demo case)")
	tau := flag.Float64("tau", 0.85, "diagnostician confidence bar")
	maxRounds := flag.Int("max-rounds", 3, "workups before forced progression")
	policy := flag.String("policy", "allow", "admission policy: allow | deny | deny-agent=<id>")
	trail := flag.Bool("trail", false, "print the audit trail after the run")
	verbose := flag.Bool("verbose", false, "log every executor hop")
	flag.Parse()

	pol, err := parsePolicy(*policy)
	if err != nil {
		fatal(err)
	}

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

	recorder := audit.NewInMemory()
	runner, err := pipeline.NewRunner(pipeline.Config{
		Tau: *tau, MaxRounds: *maxRounds,
		Boundary: admit.New(pol, recorder, nil),
	}, recorder)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("case %s → pipeline (τ=%.2f, max %d rounds, policy=%s)\n",
		runner.CaseToken(pc), *tau, *maxRounds, *policy)

	out, err := runner.Run(context.Background(), pc)
	if err != nil {
		fatal(err)
	}

	if *verbose {
		for _, evt := range out.Events {
			if e, ok := evt.(workflow.ExecutorInvokedEvent); ok {
				fmt.Printf("  hop: %s\n", e.ExecutorID)
			}
		}
	}
	for _, f := range out.Forced {
		fmt.Printf("  forced progression: case %s at confidence %.2f after %d rounds\n",
			f.CaseIDHash, f.Confidence, f.Rounds)
	}
	if *trail {
		printTrail(recorder.List())
	}

	switch {
	case out.Denied:
		fmt.Fprintln(os.Stderr, "slice: case denied by governance (see -trail)")
		os.Exit(2)
	case out.Plan != nil:
		printPlan(*out.Plan)
	default:
		for _, f := range out.Failures {
			fmt.Fprintf(os.Stderr, "slice: executor %s failed: %v\n", f.ExecutorID, f.Error)
		}
		os.Exit(1)
	}
}

func printTrail(events []audit.Event) {
	fmt.Printf("\naudit trail (%d events):\n", len(events))
	for _, e := range events {
		line := fmt.Sprintf("  %-16s", string(e.Kind))
		if e.Decision != "" {
			line += " decision=" + e.Decision
		}
		if e.AgentID != "" {
			line += " agent=" + e.AgentID
		}
		if e.Type != "" {
			line += " type=" + e.Type
		}
		if e.Reason != "" {
			line += " reason=" + e.Reason
		}
		line += " case=" + e.CaseID
		fmt.Println(line)
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
