package agents

import (
	"context"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func seedPanelRegistry() *medical.PatientRegistry {
	now := time.Now()
	reg := medical.NewPatientRegistry()
	// pt-1: diabetic with overdue A1c → 1 gap
	reg.Upsert(medical.PatientRecord{
		PatientID: "pt-1", Conditions: []string{"dm"},
		LastA1c: 9.5, LastA1cDate: now.AddDate(0, -1, 0),
		LastVisit: now.AddDate(0, -1, 0),
	})
	// pt-2: hypertensive with uncontrolled BP → 1 gap
	reg.Upsert(medical.PatientRecord{
		PatientID: "pt-2", Conditions: []string{"htn"},
		LastSystolicBP: 162,
		LastVisit:      now.AddDate(0, -1, 0),
	})
	return reg
}

func TestPanelManagerDirectMode(t *testing.T) {
	reg := seedPanelRegistry()
	a := &PanelManagerAgent{Registry: reg, HITL: false}
	msg := agent.NewMessage("user", "panel_manager", agent.RoleUser,
		"panel_request", `{"condition":"all"}`, map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) < 2 {
		t.Fatalf("want panel_report + at least one engagement_request, got %d", len(out))
	}
	if out[0].Type != "panel_report" {
		t.Errorf("first output must be panel_report, got %q", out[0].Type)
	}
	for _, m := range out[1:] {
		if m.To != "engagement" || m.Type != "engagement_request" {
			t.Errorf("expected engagement_request to engagement, got %s/%s", m.To, m.Type)
		}
	}
}

func TestPanelManagerHITLMode(t *testing.T) {
	reg := seedPanelRegistry()
	a := &PanelManagerAgent{Registry: reg, HITL: true}
	msg := agent.NewMessage("user", "panel_manager", agent.RoleUser,
		"panel_request", `{"condition":"all"}`, map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) < 2 {
		t.Fatalf("want panel_report + at least one review_request, got %d", len(out))
	}
	for _, m := range out[1:] {
		if m.To != "human_review" || m.Type != "review_request" {
			t.Errorf("expected review_request to human_review, got %s/%s", m.To, m.Type)
		}
		if k, _ := m.Metadata["hitl_kind"].(string); k != "outreach_review" {
			t.Errorf("hitl_kind: %v", m.Metadata["hitl_kind"])
		}
	}
}

func TestPriorityFromSeverity(t *testing.T) {
	cases := map[string]string{
		"urgent": "urgent",
		"warn":   "high",
		"info":   "normal",
		"":       "normal",
		"other":  "normal",
	}
	for in, want := range cases {
		if got := priorityFromSeverity(in); got != want {
			t.Errorf("priorityFromSeverity(%q): want %q got %q", in, want, got)
		}
	}
}
