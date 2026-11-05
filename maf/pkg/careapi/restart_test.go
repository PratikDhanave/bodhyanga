package careapi

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/ckptpg"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/pipeline"
)

var (
	rpoolOnce sync.Once
	rpool     *pgxpool.Pool
	rpoolErr  error
)

func restartPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	rpoolOnce.Do(func() {
		ctx := context.Background()
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("care"), tcpostgres.WithUsername("care"),
			tcpostgres.WithPassword("care"), tcpostgres.BasicWaitStrategies())
		if err != nil {
			rpoolErr = err
			return
		}
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			rpoolErr = err
			return
		}
		rpool, rpoolErr = pgxpool.New(ctx, dsn)
	})
	if rpoolErr != nil {
		t.Skipf("could not start postgres: %v", rpoolErr)
	}
	return rpool
}

// buildDurableAPI wires a care API over a Runner backed by the given ckptpg
// store + recorder — one "process lifetime".
func buildDurableAPI(t *testing.T, store *ckptpg.Store, rec *audit.InMemory) *testAPI {
	t.Helper()
	b := admit.New(admit.AllowAll(), rec, nil)
	runner, err := pipeline.NewRunner(pipeline.Config{
		Boundary: b, Review: true, Tau: 0.85, MaxRounds: 2,
	}, rec)
	if err != nil {
		t.Fatal(err)
	}
	runner.WithCheckpointStore(store)
	api := New(runner, NewRegistry(), rec)
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return &testAPI{api: api, srv: srv, rec: rec, reg: api.reg}
}

// TestRestart_PausedReviewSurvivesAcrossLifetimes is the feature's proof
// obligation: pause a case for review over HTTP, discard the entire API +
// Runner + in-memory registry, rebuild fresh backed by the SAME Postgres
// checkpoints, recover the pending list, and answer the review — the care plan
// lands and the second lifetime's trail shows the resume+completion (SC-003,
// FR-009). The HTTP analog of pkg/ckptpg.TestKillResume.
func TestRestart_PausedReviewSurvivesAcrossLifetimes(t *testing.T) {
	pool := restartPool(t)
	store := ckptpg.New(pool)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}

	// --- Lifetime 1: submit → pause for review. ---
	rec1 := audit.NewInMemory()
	api1 := buildDurableAPI(t, store, rec1)
	var h handleResponse
	if code := api1.doJSON(t, "POST", "/v1/cases", chestPain, &h); code != 201 {
		t.Fatalf("submit = %d", code)
	}
	if h.Status != StatusAwaitingReview {
		t.Fatalf("case did not pause: %+v", h)
	}
	var parked Entry
	api1.doJSON(t, "GET", "/v1/cases/"+h.CaseToken, nil, &parked)

	// --- Restart: everything in-memory is gone; only Postgres remains. ---
	rec2 := audit.NewInMemory()
	api2 := buildDurableAPI(t, store, rec2)
	// Rebuild the pending list from the checkpoint store (the API persists the
	// session ids it started; here we supply that set).
	if err := RecoverPending(context.Background(), store, api2.reg,
		map[string]Entry{h.SessionID: parked}); err != nil {
		t.Fatal(err)
	}

	// The recovered API shows the pending review.
	var reviews reviewsBody
	api2.doJSON(t, "GET", "/v1/reviews", nil, &reviews)
	if len(reviews.Reviews) != 1 || reviews.Reviews[0].SessionID != h.SessionID {
		t.Fatalf("recovered review list = %+v", reviews.Reviews)
	}

	// Answer it on the NEW lifetime → resume from Postgres → care plan.
	var final Entry
	if code := api2.doJSON(t, "POST", "/v1/reviews/"+h.SessionID,
		answerRequest{Approved: true, ReviewerID: "dr-restart"}, &final); code != 200 {
		t.Fatalf("post-restart approve = %d", code)
	}
	if final.Status != StatusCompleted || len(final.PlanSteps) == 0 {
		t.Fatalf("resume did not complete the case: %+v", final)
	}

	// The second lifetime's trail carries the resume+completion — the case
	// continued across the restart.
	sawCarePlan := false
	for _, e := range rec2.List() {
		if e.AgentID == "care_plan" {
			sawCarePlan = true
		}
	}
	if !sawCarePlan {
		t.Error("second lifetime's trail has no care_plan admission — resume did not continue the case")
	}
}
