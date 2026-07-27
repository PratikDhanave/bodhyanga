// cmd/care serves the MARA pipeline over HTTP (specs/004).
//
//	go run ./cmd/care                       # rule engine, loopback :8088
//	go run ./cmd/care -diagnostician=llm-ollama
//	go run ./cmd/care -db postgres://...     # durable review across restart
//
// Endpoints: POST /v1/cases, GET /v1/cases/{token}, GET /v1/cases/{token}/audit,
// GET /v1/reviews, POST /v1/reviews/{session}, plus /healthz and /readyz.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/careapi"
	"github.com/PratikDhanave/bodh-maf/pkg/ckptpg"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"

	"flag"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8088", "listen address (loopback by default — no auth)")
	policy := flag.String("policy", "allow", "admission policy: allow | deny | deny-agent=<id>")
	diagnostician := flag.String("diagnostician", "rule", "engine: rule | llm-anthropic | llm-ollama")
	tau := flag.Float64("tau", 0.85, "diagnostician confidence bar")
	maxRounds := flag.Int("max-rounds", 3, "workups before forced progression")
	db := flag.String("db", os.Getenv("SLICE_DB"), "postgres URL for durable checkpoints (paused cases survive restart)")
	flag.Parse()

	pol, err := parsePolicy(*policy)
	if err != nil {
		fatal(err)
	}

	recorder := audit.NewInMemory()
	boundary := admit.New(pol, recorder, nil)
	dx, err := buildDiagnostician(*diagnostician, boundary,
		agents.DiagnosticianConfig{Tau: *tau, MaxRounds: *maxRounds}, boundary.Guard)
	if err != nil {
		fatal(err)
	}
	runner, err := pipeline.NewRunner(pipeline.Config{
		Tau: *tau, MaxRounds: *maxRounds, Review: true,
		Boundary: boundary, Diagnostician: dx, EngineName: *diagnostician,
	}, recorder)
	if err != nil {
		fatal(err)
	}

	durable := *db != ""
	if durable {
		pool, err := pgxpool.New(context.Background(), *db)
		if err != nil {
			fatal(fmt.Errorf("connect -db: %w", err))
		}
		defer pool.Close()
		store := ckptpg.New(pool)
		if err := store.EnsureSchema(context.Background()); err != nil {
			fatal(err)
		}
		runner.WithCheckpointStore(store)
	}

	api := careapi.New(runner, careapi.NewRegistry(), recorder)

	banner(*addr, *diagnostician, *policy, durable)
	if err := http.ListenAndServe(*addr, api.Handler()); err != nil {
		fatal(err)
	}
}

func banner(addr, engine, policy string, durable bool) {
	fmt.Printf("care API on http://%s  (engine=%s policy=%s)\n", addr, engine, policy)
	fmt.Println("  ⚠  NOT A MEDICAL DEVICE — research/education only, not for clinical use.")
	fmt.Println("  ⚠  RESEARCH SERVER — no authentication is wired (ADR-0006). Bind loopback; front with authenticated ingress for any real use.")
	if durable {
		fmt.Println("  ⚠  -db durably checkpoints paused cases, but this binary does NOT yet auto-recover the pending-review list after a restart (a durably-checkpointed review would be unlisted by GET /v1/reviews). See pkg/careapi/doc.go.")
	} else {
		fmt.Println("  ⚠  no -db: paused cases awaiting review will NOT survive a restart.")
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
	fmt.Fprintln(os.Stderr, "care:", err)
	os.Exit(1)
}
