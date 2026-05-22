package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func cdmRequestMsg(patientID, condition, caseID string) agent.Message {
	body := `{"patient_id":"` + patientID + `","condition":"` + condition + `","case_id":"` + caseID + `"}`
	return agent.NewMessage("user", "cdm_planner", agent.RoleUser, "cdm_request",
		body, map[string]any{"case_id": caseID, "patient_id": patientID})
}

func TestCDMPlannerDirectMode(t *testing.T) {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{PatientID: "p1"})
	a := &CDMPlannerAgent{Registry: reg, HITL: false}

	out, err := a.HandleMessage(context.Background(), cdmRequestMsg("p1", "dm", "c1"), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 outputs (engagement + panel_manager), got %d", len(out))
	}
	got := map[string]bool{}
	for _, m := range out {
		got[m.To] = true
		if m.Type != "care_plan_ready" {
			t.Errorf("want Type=care_plan_ready, got %q", m.Type)
		}
	}
	if !got["engagement"] || !got["panel_manager"] {
		t.Errorf("missing recipients: %+v", got)
	}
	rec, _ := reg.Get("p1")
	if len(rec.Medications) == 0 {
		t.Errorf("registry should have been updated with plan medications")
	}
}

func TestCDMPlannerHITLMode(t *testing.T) {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{PatientID: "p1"})
	a := &CDMPlannerAgent{Registry: reg, HITL: true}

	out, err := a.HandleMessage(context.Background(), cdmRequestMsg("p1", "dm", "c1"), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	var review, track *agent.Message
	for i, m := range out {
		if m.To == "human_review" && m.Type == "review_request" {
			review = &out[i]
		}
		if m.To == "panel_manager" && m.Type == "care_plan_ready" {
			track = &out[i]
		}
	}
	if review == nil {
		t.Fatalf("expected a review_request to human_review, got %+v", out)
	}
	if track == nil {
		t.Fatalf("expected a care_plan_ready to panel_manager, got %+v", out)
	}
	if review.Metadata["hitl_next_to"] != "engagement" {
		t.Errorf("hitl_next_to: %v", review.Metadata["hitl_next_to"])
	}
	if review.Metadata["hitl_next_type"] != "care_plan_ready" {
		t.Errorf("hitl_next_type: %v", review.Metadata["hitl_next_type"])
	}
	if k, _ := review.Metadata["hitl_kind"].(string); k != "care_plan_review" {
		t.Errorf("hitl_kind: %v", review.Metadata["hitl_kind"])
	}
	if s, _ := review.Metadata["hitl_subject"].(string); !strings.Contains(s, "dm") {
		t.Errorf("subject should mention condition: %q", s)
	}
}
