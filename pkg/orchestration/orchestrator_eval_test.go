package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// TestOrchestrator_RecordsEvalOnSupervisorDiagnosisFinal asserts that when
// the diagnostic_supervisor returns no follow-up after receiving a
// terminal "diagnosis" message, the orchestrator mirrors the case into the
// configured eval.Store. The record's metrics come from eval.ScoreCase.
func TestOrchestrator_RecordsEvalOnSupervisorDiagnosisFinal(t *testing.T) {
	state := medical.CaseState{
		CaseID:           "case-001",
		WorkingDiagnosis: "community-acquired pneumonia",
		GoldDiagnosis:    "community-acquired pneumonia",
		TotalCostUSD:     120,
		MaxCostUSD:       500,
	}
	supervisor := &stubAgent{
		id:   "diagnostic_supervisor",
		name: "supervisor",
		handler: func(_ context.Context, _ agent.Message, _ agent.Environment) ([]agent.Message, error) {
			// Terminal hop: returning zero messages triggers the eval mirror.
			return nil, nil
		},
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, supervisor)
	store := eval.NewInMemoryStore()
	o.SetEvalStore(store)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage(
		"diagnostician", "diagnostic_supervisor", agent.RoleAgent,
		"diagnosis", medical.EncodeState(state),
		map[string]any{"case_id": state.CaseID, "message_steps": 9}))

	// Poll briefly for the eval store to receive the record.
	deadline := time.Now().Add(500 * time.Millisecond)
	var got []eval.InteractionRecord
	for time.Now().Before(deadline) {
		got = store.List()
		if len(got) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 eval record, got %d", len(got))
	}
	rec := got[0]
	if rec.ID != "case-001" {
		t.Errorf("record ID: got %q, want %q", rec.ID, "case-001")
	}
	if !rec.Success {
		t.Errorf("expected success=true (diagnosis matches gold, under budget); got %+v", rec)
	}
	if rec.Metrics["accuracy"] != 1 {
		t.Errorf("expected accuracy=1, got %v", rec.Metrics["accuracy"])
	}
}

// TestOrchestrator_NoEvalForNonSupervisorTerminal asserts the eval mirror
// only fires for the diagnostic_supervisor + diagnosis pair — other agents
// returning zero messages must not produce InteractionRecords.
func TestOrchestrator_NoEvalForNonSupervisorTerminal(t *testing.T) {
	worker := &stubAgent{id: "worker", name: "worker"}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	store := eval.NewInMemoryStore()
	o.SetEvalStore(store)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage(
		"user", "worker", agent.RoleUser, "diagnosis", "{}", nil))

	time.Sleep(75 * time.Millisecond)
	if got := store.List(); len(got) != 0 {
		t.Errorf("non-supervisor diagnosis must not write eval record, got %+v", got)
	}
}
