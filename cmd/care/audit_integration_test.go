package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/registry"
)

// TestAuditEndpoint_EndToEndWithOrchestrator stands up a miniature
// orchestrator (intake agent, allowAll-ish policy, in-memory bus + reg),
// wires an audit.Recorder into it, publishes a clinical message, then hits
// the same auditListHandler installed in main.go and asserts the events
// surface in the JSON response. This is the in-process equivalent of the
// `go run ./cmd/care && curl /audit` smoke test the sandbox blocks.
func TestAuditEndpoint_EndToEndWithOrchestrator(t *testing.T) {
	ctx := context.Background()

	logger := observability.NewStdLogger()
	env := &orchestration.SimpleEnvironment{Logger: logger, Clock: observability.SystemClock{}}
	reg := registry.NewInMemory()
	bus := comm.NewInMemoryBus()
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 65536},
		governance.RequireCaseIDPolicy{},
	)

	// One real agent is enough to exercise the message + (clinical) allow
	// audit hooks. IntakeAgent is zero-config.
	intake := &agents.IntakeAgent{}
	if err := reg.Register(ctx, intake); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := audit.NewInMemory()
	orch := orchestration.NewOrchestrator(reg, bus, policy, env)
	orch.SetAuditRecorder(rec)
	orch.Start(ctx)

	// Clinical message: presentation type triggers RequireCaseIDPolicy +
	// recorded allow, then intake handles → recorded KindMessage.
	bus.Publish(ctx, agent.NewMessage(
		"user", "intake", agent.RoleUser, "presentation", "{}",
		map[string]any{
			"case_id":   "case-smoke",
			"tenant_id": "t-1",
		}))

	// Wait for at least 2 events to settle.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(rec.List()) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now hit the production handler.
	handler := auditListHandler(rec)
	rec1 := httptest.NewRecorder()
	handler(rec1, httptest.NewRequest(http.MethodGet, "/audit?limit=10", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec1.Code, rec1.Body.String())
	}
	var events []audit.Event
	if err := json.Unmarshal(rec1.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec1.Body.String())
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (policy allow + message), got %d", len(events))
	}

	// Every event from a tenant-tagged publish must carry tenant_id.
	for _, e := range events {
		if e.TenantID != "t-1" {
			t.Errorf("missing tenant_id correlation: %+v", e)
		}
		if e.CaseID != "case-smoke" {
			t.Errorf("missing case_id correlation: %+v", e)
		}
	}
}
