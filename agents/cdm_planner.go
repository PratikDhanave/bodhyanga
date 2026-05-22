package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// CDMPlannerAgent generates a condition-specific care plan for chronic disease
// management (CDM) — hypertension, diabetes, CHF, CKD, COPD.
//
// Input message Type:  cdm_request   Content: {"patient_id","condition","case_id"}
// Output message Type: care_plan_ready  Content: medical.CarePlan JSON
// Output routes to:    engagement (for outreach) and panel_manager (for tracking)
type CDMPlannerAgent struct {
	Registry *medical.PatientRegistry
}

type cdmRequest struct {
	PatientID string `json:"patient_id"`
	Condition string `json:"condition"`
	CaseID    string `json:"case_id,omitempty"`
}

func (a *CDMPlannerAgent) ID() string   { return "cdm_planner" }
func (a *CDMPlannerAgent) Name() string { return "Chronic Disease Care Planner" }
func (a *CDMPlannerAgent) Capabilities() []string {
	return []string{"care_plan", "chronic_disease_management"}
}

func (a *CDMPlannerAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req cdmRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[cdm_planner] invalid request: %v", err)
		return nil, err
	}
	plan := medical.BuildCarePlan(req.CaseID, req.PatientID, req.Condition, time.Now())
	env.Logf("[cdm_planner] %s", plan.Summary())

	if a.Registry != nil {
		if rec, ok := a.Registry.Get(req.PatientID); ok {
			rec.Conditions = appendIfMissing(rec.Conditions, plan.Condition)
			rec.Medications = plan.Medications
			a.Registry.Upsert(rec)
		}
	}

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), "engagement", agent.RoleAgent,
		"care_plan_ready", medical.EncodeCarePlan(plan), meta)
	track := agent.NewMessage(a.ID(), "panel_manager", agent.RoleAgent,
		"care_plan_ready", medical.EncodeCarePlan(plan), meta)
	return []agent.Message{out, track}, nil
}

func appendIfMissing(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
