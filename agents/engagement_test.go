package agents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/engagement"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func TestEngagementIDNameCapabilities(t *testing.T) {
	a := &EngagementAgent{Notifier: engagement.NewInMemory()}
	if a.ID() != "engagement" {
		t.Errorf("ID=%q", a.ID())
	}
	if a.Name() == "" {
		t.Error("Name must be non-empty")
	}
	caps := a.Capabilities()
	if len(caps) == 0 {
		t.Error("Capabilities must be non-empty")
	}
}

func TestEngagementCarePlanReady(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}

	plan := medical.CarePlan{
		Condition:  "htn",
		NextReview: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Goals: []medical.GoalEntry{
			{Metric: "BP", Target: 130, Unit: "mmHg"},
		},
	}
	meta := map[string]any{"patient_id": "p1", "case_id": "c1"}
	msg := agent.NewMessage("cdm_planner", "engagement", agent.RoleAgent,
		"care_plan_ready", medical.EncodeCarePlan(plan), meta)

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].Type != "engagement_sent" {
		t.Errorf("Type=%s", out[0].Type)
	}
	items := notifier.List()
	if len(items) != 1 {
		t.Fatalf("notifier should have 1 item, got %d", len(items))
	}
	if items[0].Template != "care_plan" {
		t.Errorf("template=%s", items[0].Template)
	}
}

func TestEngagementCarePlanReadyNoGoals(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}

	plan := medical.CarePlan{
		Condition:  "htn",
		NextReview: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Goals:      nil,
	}
	meta := map[string]any{"patient_id": "p1"}
	msg := agent.NewMessage("cdm_planner", "engagement", agent.RoleAgent,
		"care_plan_ready", medical.EncodeCarePlan(plan), meta)

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
}

func TestEngagementCarePlanInvalidJSON(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}
	meta := map[string]any{"patient_id": "p1"}
	msg := agent.NewMessage("cdm_planner", "engagement", agent.RoleAgent,
		"care_plan_ready", "not json", meta)
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestEngagementRequest(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}

	gap := medical.CareGap{
		PatientID: "p1",
		Rule:      "htn_bp_check",
		Detail:    "BP check overdue",
	}
	body, _ := json.Marshal(gap)
	meta := map[string]any{"patient_id": "p1", "case_id": "c1"}
	msg := agent.NewMessage("panel_manager", "engagement", agent.RoleAgent,
		"engagement_request", string(body), meta)

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
}

func TestEngagementRequestInvalidJSON(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}
	meta := map[string]any{"patient_id": "p1"}
	msg := agent.NewMessage("panel_manager", "engagement", agent.RoleAgent,
		"engagement_request", "bad json", meta)
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestEngagementTCMTasksCreated(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}
	meta := map[string]any{"patient_id": "p1", "case_id": "c1"}
	msg := agent.NewMessage("tcm_coordinator", "engagement", agent.RoleAgent,
		"tcm_tasks_created", `{"task_count":3}`, meta)

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	items := notifier.List()
	if len(items) != 1 {
		t.Fatalf("want 1 notification, got %d", len(items))
	}
	if items[0].Template != "tcm_welcome" {
		t.Errorf("template=%s", items[0].Template)
	}
}

func TestEngagementRefillResponse(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}

	resp := map[string]string{
		"patient_id": "p1",
		"medication": "metformin",
		"status":     "approved",
	}
	body, _ := json.Marshal(resp)
	meta := map[string]any{"case_id": "c1"}
	msg := agent.NewMessage("refill_manager", "engagement", agent.RoleAgent,
		"refill_response", string(body), meta)

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	items := notifier.List()
	if len(items) != 1 {
		t.Fatalf("want 1 notification, got %d", len(items))
	}
	if items[0].Template != "refill_status" {
		t.Errorf("template=%s", items[0].Template)
	}
}

func TestEngagementRefillResponseInvalidJSON(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}
	meta := map[string]any{}
	msg := agent.NewMessage("refill_manager", "engagement", agent.RoleAgent,
		"refill_response", "bad", meta)
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestEngagementUnknownTypeReturnsNil(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}
	meta := map[string]any{"patient_id": "p1"}
	msg := agent.NewMessage("unknown", "engagement", agent.RoleAgent,
		"unknown_type", "{}", meta)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("unknown type should return nil, got %d messages", len(out))
	}
}

func TestEngagementSendEmptyPatientID(t *testing.T) {
	notifier := engagement.NewInMemory()
	a := &EngagementAgent{Notifier: notifier}
	// TCM with empty patient_id should return nil.
	meta := map[string]any{"patient_id": ""}
	msg := agent.NewMessage("tcm_coordinator", "engagement", agent.RoleAgent,
		"tcm_tasks_created", `{}`, meta)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("empty patient_id should return nil")
	}
}

func TestSummaryGoals(t *testing.T) {
	plan := medical.CarePlan{
		Goals: []medical.GoalEntry{
			{Metric: "BP", Target: 130, Unit: "mmHg"},
		},
	}
	got := summaryGoals(plan)
	if got == "" {
		t.Error("summaryGoals should return non-empty for plan with goals")
	}

	emptyPlan := medical.CarePlan{}
	got2 := summaryGoals(emptyPlan)
	if got2 != "follow up scheduled" {
		t.Errorf("summaryGoals for empty plan = %q, want 'follow up scheduled'", got2)
	}
}
