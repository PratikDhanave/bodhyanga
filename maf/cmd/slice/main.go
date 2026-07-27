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
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/ckptpg"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

func main() {
	casePath := flag.String("case", "", "path to a PatientCase JSON file (default: built-in demo case)")
	tau := flag.Float64("tau", 0.85, "diagnostician confidence bar")
	maxRounds := flag.Int("max-rounds", 3, "workups before forced progression")
	policy := flag.String("policy", "allow", "admission policy: allow | deny | deny-agent=<id>")
	trail := flag.Bool("trail", false, "print the audit trail after the run")
	verbose := flag.Bool("verbose", false, "log every executor hop")
	review := flag.Bool("review", true, "pause at the physician-review gate")
	diagnostician := flag.String("diagnostician", "rule", "diagnostic engine: rule | llm-anthropic | llm-ollama")
	corpus := flag.String("corpus", "", "knowledge corpus JSON for RAG grounding (empty = RAG off; llm-* engines only)")
	embedder := flag.String("embedder", "mock", "RAG embedder: mock | ollama")
	format := flag.String("format", "json", "inbound format: json | hl7 | fhir (non-json requires -case and enables the translation front door)")
	db := flag.String("db", os.Getenv("SLICE_DB"), "postgres URL for durable checkpoints (enables -resume)")
	resume := flag.String("resume", "", "resume a paused session by ID (requires -db)")
	approve := flag.Bool("approve", false, "answer the pending review with approval")
	reject := flag.Bool("reject", false, "answer the pending review with rejection")
	reviewer := flag.String("reviewer", "", "reviewer ID for -approve/-reject")
	flag.Parse()

	pol, err := parsePolicy(*policy)
	if err != nil {
		fatal(err)
	}

	// Non-JSON formats (HL7/FHIR) enable the translation front door and are fed
	// to the pipeline as raw bytes; JSON keeps the native PatientCase path.
	translate := *format != "json"
	var pc agents.PatientCase
	var inbound agents.Inbound
	if translate {
		if *casePath == "" {
			fatal(fmt.Errorf("-format %s requires -case <file>", *format))
		}
		data, err := os.ReadFile(*casePath)
		if err != nil {
			fatal(err)
		}
		inbound = agents.Inbound{Format: agents.Format(*format), Payload: data}
	} else {
		pc = agents.PatientCase{
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
	}

	ctx := context.Background()
	recorder := audit.NewInMemory()
	boundary := admit.New(pol, recorder, nil)

	// RAG is opt-in and additive: -corpus "" leaves retriever nil (disabled),
	// so the diagnostician node behaves exactly as before (spec US2/US3).
	var retriever *agents.Retriever
	if *corpus != "" {
		emb, err := buildEmbedder(*embedder)
		if err != nil {
			fatal(err)
		}
		retriever, err = agents.LoadRetriever(*corpus, emb, 0)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("RAG enabled: corpus=%s embedder=%s\n", *corpus, *embedder)
	}

	dx, err := buildDiagnostician(*diagnostician, boundary,
		agents.DiagnosticianConfig{Tau: *tau, MaxRounds: *maxRounds}, boundary.Guard, retriever)
	if err != nil {
		fatal(err)
	}
	evalStore := eval.NewInMemoryStore()
	runner, err := pipeline.NewRunner(pipeline.Config{
		Tau: *tau, MaxRounds: *maxRounds, Review: *review,
		Boundary: boundary, Diagnostician: dx, Translate: translate,
		EvalStore: evalStore, EngineName: *diagnostician,
	}, recorder)
	if err != nil {
		fatal(err)
	}

	if *db != "" {
		pool, err := pgxpool.New(ctx, *db)
		if err != nil {
			fatal(fmt.Errorf("connect -db: %w", err))
		}
		defer pool.Close()
		store := ckptpg.New(pool)
		if err := store.EnsureSchema(ctx); err != nil {
			fatal(err)
		}
		runner.WithCheckpointStore(store)
	}

	if *approve || *reject {
		if *reviewer == "" {
			fatal(fmt.Errorf("-approve/-reject require -reviewer <id>"))
		}
		dec := agents.ReviewDecision{Approved: *approve, ReviewerID: *reviewer}
		runner.Responder = func(req *workflow.ExternalRequest) (*workflow.ExternalResponse, error) {
			if r, ok := workflow.PortableValueAs[agents.ReviewRequest](req.Data); ok {
				fmt.Printf("  review: case %s %q conf=%.2f → %s by %s\n",
					r.CaseToken, r.Diagnosis, r.Confidence, decisionWord(*approve), *reviewer)
			}
			return req.CreateResponse(dec)
		}
	}

	var out *pipeline.Outcome
	if *resume != "" {
		if *db == "" {
			fatal(fmt.Errorf("-resume requires -db"))
		}
		fmt.Printf("resuming session %s\n", *resume)
		out, err = runner.Resume(ctx, *resume)
	} else if translate {
		fmt.Printf("inbound %s → pipeline (τ=%.2f, max %d rounds, policy=%s, review=%v)\n",
			*format, *tau, *maxRounds, *policy, *review)
		out, err = runner.RunInbound(ctx, inbound, "")
	} else {
		fmt.Printf("case %s → pipeline (τ=%.2f, max %d rounds, policy=%s, review=%v)\n",
			runner.CaseToken(pc), *tau, *maxRounds, *policy, *review)
		out, err = runner.Run(ctx, pc, "")
	}
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
	if out.Eval != nil {
		fmt.Printf("\neval: scenario=%s success=%v confidence=%.2f rounds=%.0f forced=%.0f decision=%v\n",
			out.Eval.Scenario, out.Eval.Success, out.Eval.Metrics["confidence"],
			out.Eval.Metrics["rounds"], out.Eval.Metrics["forced"], out.Eval.Metadata["review_decision"])
	}

	switch {
	case out.Denied:
		fmt.Fprintln(os.Stderr, "slice: case denied by governance (see -trail)")
		os.Exit(2)
	case out.Paused:
		fmt.Printf("\ncase awaiting physician review — session %s\n", out.SessionID)
		fmt.Printf("resume with: slice -db <url> -resume %s -approve -reviewer <id>\n", out.SessionID)
		os.Exit(3)
	case out.Rejected != nil:
		fmt.Printf("\ncase %s: diagnosis %q REJECTED by %s — no care plan\n",
			agents.HashCaseID(out.Rejected.CaseID), out.Rejected.Diagnosis, out.Rejected.ReviewerID)
	case out.Plan != nil:
		printPlan(*out.Plan)
	default:
		for _, f := range out.Failures {
			fmt.Fprintf(os.Stderr, "slice: executor %s failed: %v\n", f.ExecutorID, f.Error)
		}
		os.Exit(1)
	}
}

func decisionWord(approved bool) string {
	if approved {
		return "APPROVE"
	}
	return "REJECT"
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
