package ckptpg_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/PratikDhanave/bodh-maf/pkg/ckptpg"
)

var (
	poolOnce sync.Once
	pool     *pgxpool.Pool
	poolErr  error
)

// testPool starts one Postgres container per test binary (bodh dbtest pattern).
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	poolOnce.Do(func() {
		ctx := context.Background()
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("slice"),
			tcpostgres.WithUsername("slice"),
			tcpostgres.WithPassword("slice"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			poolErr = err
			return
		}
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			poolErr = err
			return
		}
		pool, poolErr = pgxpool.New(ctx, dsn)
	})
	if poolErr != nil {
		t.Skipf("could not start postgres container: %v", poolErr)
	}
	return pool
}

func testStore(t *testing.T) *ckptpg.Store {
	t.Helper()
	s := ckptpg.New(testPool(t))
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRoundTripPreservesBytes(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	payload := json.RawMessage(`{"step":1,"state":{"pending":["review"]}}`)
	info, err := s.CreateCheckpoint(ctx, "sess-rt", payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.RetrieveCheckpoint(ctx, "sess-rt", info)
	if err != nil {
		t.Fatal(err)
	}
	// jsonb normalizes whitespace; compare semantically.
	var a, b any
	if json.Unmarshal(payload, &a) != nil || json.Unmarshal(got, &b) != nil {
		t.Fatal("unmarshal round-trip payloads")
	}
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatalf("round trip mismatch: %s vs %s", aj, bj)
	}
}

func TestIndexOrderAndLineage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	c1, err := s.CreateCheckpoint(ctx, "sess-idx", json.RawMessage(`{"n":1}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.CreateCheckpoint(ctx, "sess-idx", json.RawMessage(`{"n":2}`), &c1)
	if err != nil {
		t.Fatal(err)
	}
	c3, err := s.CreateCheckpoint(ctx, "sess-idx", json.RawMessage(`{"n":3}`), &c2)
	if err != nil {
		t.Fatal(err)
	}

	infos, err := s.RetrieveIndex(ctx, "sess-idx", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 3 || infos[0].CheckpointID != c1.CheckpointID || infos[2].CheckpointID != c3.CheckpointID {
		t.Fatalf("index order wrong: %+v", infos)
	}

	children, err := s.RetrieveIndex(ctx, "sess-idx", &c1)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].CheckpointID != c2.CheckpointID {
		t.Fatalf("lineage filter wrong: %+v", children)
	}
}

func TestMissingCheckpointIsError(t *testing.T) {
	s := testStore(t)
	_, err := s.RetrieveCheckpoint(context.Background(), "sess-missing",
		workflowInfo("sess-missing", "00000000-0000-0000-0000-000000000000"))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing checkpoint error = %v, want not-found", err)
	}
}

func TestSessionsAreIsolated(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	a, err := s.CreateCheckpoint(ctx, "sess-a", json.RawMessage(`{"who":"a"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCheckpoint(ctx, "sess-b", json.RawMessage(`{"who":"b"}`), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetrieveCheckpoint(ctx, "sess-b", a); err == nil {
		t.Fatal("cross-session retrieval succeeded; sessions must be isolated")
	}
	infos, err := s.RetrieveIndex(ctx, "sess-a", nil)
	if err != nil || len(infos) != 1 {
		t.Fatalf("sess-a index = %v, %v; want exactly its own row", infos, err)
	}
}
