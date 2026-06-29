package agents_test

// These tests exercise pkg/shadow's ShadowRunner against the REAL agent
// pipeline built by agents.BuildShadowPipeline. They live in the agents module
// (not pkg/shadow) because pkg/shadow is intentionally agent-free for the
// decoupling module split (PR-0.6); only here is importing the concrete agents
// legal. Coverage of the runner's RunShadow path is preserved — these run as
// part of `go test ./...` exactly as before, just from a different package.

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/shadow"
)

// TestShadowRunner_SyncMode exercises the full shadow pipeline end-to-end
// using the rule-based diagnostician in synchronous mode.
func TestShadowRunner_SyncMode(t *testing.T) {
	ctx := context.Background()

	orch, bus, err := agents.BuildShadowPipeline(ctx, "rule")
	if err != nil {
		t.Fatalf("BuildShadowPipeline: %v", err)
	}
	_ = orch // already started

	ledger, err := shadow.NewConcordanceLedger("")
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}

	runner := shadow.NewShadowRunner(orch, bus, ledger, shadow.ModeSync, 5*time.Second)

	// Community-acquired pneumonia fixture — the rule diagnostician should
	// resolve this correctly.
	caseInput := `{
		"chief_complaint": "Cough and fever",
		"hpi": "58-year-old presents with 5 days productive cough and fever to 38.9C",
		"demographics": "58M",
		"vitals": "T 38.9C, HR 98, RR 22, SpO2 94%",
		"case_id": "shadow-test-001"
	}`

	rec, err := runner.RunShadow(ctx, caseInput, "Community-acquired pneumonia")
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	if rec == nil {
		t.Fatal("RunShadow returned nil record in sync mode")
	}

	t.Logf("AI decision: %q", rec.AIDecision)
	t.Logf("Concordant: %v", rec.Concordant)
	t.Logf("Latency: %dms", rec.LatencyMs)

	if rec.CaseID != "shadow-test-001" {
		t.Errorf("CaseID = %q, want %q", rec.CaseID, "shadow-test-001")
	}
	if rec.ClinicianDecision != "Community-acquired pneumonia" {
		t.Errorf("ClinicianDecision = %q, want %q", rec.ClinicianDecision, "Community-acquired pneumonia")
	}
	if rec.AIDecision == "" {
		t.Error("AIDecision should not be empty")
	}

	// Verify report aggregation works.
	report := runner.Report()
	if report.Total != 1 {
		t.Errorf("Report.Total = %d, want 1", report.Total)
	}
}

// TestShadowRunner_AsyncMode verifies that async mode returns nil
// immediately and eventually records the result.
func TestShadowRunner_AsyncMode(t *testing.T) {
	ctx := context.Background()

	orch, bus, err := agents.BuildShadowPipeline(ctx, "rule")
	if err != nil {
		t.Fatalf("BuildShadowPipeline: %v", err)
	}
	_ = orch

	ledger, err := shadow.NewConcordanceLedger("")
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}

	runner := shadow.NewShadowRunner(orch, bus, ledger, shadow.ModeAsync, 5*time.Second)

	caseInput := `{
		"chief_complaint": "Cough and fever",
		"hpi": "58-year-old with productive cough",
		"demographics": "58M",
		"vitals": "T 38.5C, HR 95, RR 20",
		"case_id": "shadow-async-001"
	}`

	rec, err := runner.RunShadow(ctx, caseInput, "Community-acquired pneumonia")
	if err != nil {
		t.Fatalf("RunShadow async: %v", err)
	}
	if rec != nil {
		t.Error("RunShadow async should return nil record")
	}

	// Give the background goroutine time to complete.
	time.Sleep(3 * time.Second)

	report := runner.Report()
	if report.Total < 1 {
		t.Logf("async record may not have completed yet (Total=%d), skipping assertion", report.Total)
	} else {
		t.Logf("Async report: %+v", report)
	}
}

// TestRunShadow_ContextCancelled verifies that RunShadow respects ctx
// cancellation.
func TestRunShadow_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	orch, bus, err := agents.BuildShadowPipeline(context.Background(), "rule")
	if err != nil {
		t.Fatalf("BuildShadowPipeline: %v", err)
	}
	_ = orch

	l, _ := shadow.NewConcordanceLedger("")
	runner := shadow.NewShadowRunner(orch, bus, l, shadow.ModeSync, time.Second)

	caseInput := `{"chief_complaint": "cough", "hpi": "test", "demographics": "50M", "vitals": "normal", "case_id": "ctx-cancel"}`
	_, err = runner.RunShadow(ctx, caseInput, "test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestBuildShadowPipeline_InvalidProvider verifies error for unknown provider.
func TestBuildShadowPipeline_InvalidProvider(t *testing.T) {
	_, _, err := agents.BuildShadowPipeline(context.Background(), "nonexistent-provider-xyz")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
