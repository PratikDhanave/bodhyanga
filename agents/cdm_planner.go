package agents

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// CDMPlannerAgent generates a condition-specific care plan for chronic disease
// management (CDM) — hypertension, diabetes, CHF, CKD, COPD.
//
// Input message Type:  cdm_request   Content: {"patient_id","condition","case_id"}
// Output message Type: care_plan_ready  Content: medical.CarePlan JSON
// Output routes to:    engagement (for outreach, optionally HITL-gated) and
//
//	panel_manager (for tracking, always direct).
type CDMPlannerAgent struct {
	Registry medical.PatientStore
	// HITL routes the patient-facing engagement message through human_review
	// so a clinician approves the care plan before SMS goes out.
	HITL bool
}

type cdmRequest struct {
	PatientID string `json:"patient_id"`
	Condition string `json:"condition"`
	CaseID    string `json:"case_id,omitempty"`
	// Well-child fields (used when Condition == "well_child").
	AgeMonths int     `json:"age_months,omitempty"`
	WeightKg  float64 `json:"weight_kg,omitempty"`
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
	now := time.Now()
	var plan medical.CarePlan
	switch strings.ToLower(req.Condition) {
	case "well_child":
		plan = medical.BuildPediatricWellChildCarePlan(req.CaseID, req.PatientID, req.AgeMonths, req.WeightKg, now)
	case "post_mi":
		plan = medical.BuildPostMICarePlan(req.CaseID, req.PatientID, now)
	case "post_stroke", "post_stroke_ischemic", "post_stroke_hemorrhagic":
		strokeType := "ischemic"
		if strings.Contains(strings.ToLower(req.Condition), "hemorrhagic") {
			strokeType = "hemorrhagic"
		}
		plan = medical.BuildPostStrokeCarePlan(req.CaseID, req.PatientID, strokeType, now)
	default:
		plan = medical.BuildCarePlan(req.CaseID, req.PatientID, req.Condition, now)
	}
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

	planJSON := medical.EncodeCarePlan(plan)
	var out agent.Message
	if a.HITL {
		out = agent.NewMessage(a.ID(), "human_review", agent.RoleAgent,
			"review_request", planJSON, reviewEnvelope(meta, reviewOptions{
				Kind:       "care_plan_review",
				NextTo:     "engagement",
				NextType:   "care_plan_ready",
				Priority:   "normal",
				Subject:    "Approve " + plan.Condition + " care plan for " + req.PatientID,
				SLAMinutes: 24 * 60,
			}))
	} else {
		out = agent.NewMessage(a.ID(), "engagement", agent.RoleAgent,
			"care_plan_ready", planJSON, meta)
	}
	track := agent.NewMessage(a.ID(), "panel_manager", agent.RoleAgent,
		"care_plan_ready", planJSON, copyMeta(meta))
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
