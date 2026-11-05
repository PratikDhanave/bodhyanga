package careapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/PratikDhanave/bodhyanga/pkg/governance"
	"github.com/microsoft/agent-framework-go/workflow/checkpoint"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/pipeline"
)

type testAPI struct {
	api *API
	srv *httptest.Server
	rec *audit.InMemory
	reg Registry
}

// newTestAPI wires a rule-engine Runner (allow policy by default, in-memory
// audit + registry) into a careapi handler over httptest — no Docker, no LLM.
func newTestAPI(t *testing.T, policy governance.Policy) *testAPI {
	t.Helper()
	if policy == nil {
		policy = admit.AllowAll()
	}
	rec := audit.NewInMemory()
	b := admit.New(policy, rec, nil)
	runner, err := pipeline.NewRunner(pipeline.Config{
		Boundary: b, Review: true, Tau: 0.85, MaxRounds: 2,
	}, rec)
	if err != nil {
		t.Fatal(err)
	}
	// A filesystem checkpoint store on a temp dir gives real pause/resume with
	// no Docker — the offline analog of the Postgres store cmd/care uses with -db.
	store, err := checkpoint.NewFileSystemJSONStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner.WithCheckpointStore(store)
	reg := NewRegistry()
	api := New(runner, reg, rec)
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return &testAPI{api: api, srv: srv, rec: rec, reg: reg}
}

// do issues a request against the test server and returns the status code and
// raw body. body may be nil, a string, or a value to JSON-encode.
func (ta *testAPI) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	switch v := body.(type) {
	case nil:
	case string:
		rdr = bytes.NewBufferString(v)
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, ta.srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// marshal is a test convenience: JSON-encode to a string for substring scans.
func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// doJSON is do() plus decoding the body into v.
func (ta *testAPI) doJSON(t *testing.T, method, path string, body any, v any) int {
	t.Helper()
	code, out := ta.do(t, method, path, body)
	if v != nil && len(out) > 0 {
		if err := json.Unmarshal(out, v); err != nil {
			t.Fatalf("decode %s %s: %v (body %s)", method, path, err, out)
		}
	}
	return code
}
