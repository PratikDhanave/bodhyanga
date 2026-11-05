package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
	"github.com/PratikDhanave/bodhyanga/pkg/tasks"
)

// TCMCoordinatorAgent owns transitional care management.
//
// Input message Type:  discharge_event   Content: medical.DischargeSummary JSON
// Side effects:        creates NurseTasks (med reconciliation + N follow-ups)
// Output message Type: tcm_tasks_created  Content: JSON {task_ids,...}
// Output routes to:    engagement (to notify the patient), optionally HITL-gated
//                      so a nurse confirms the welcome message and task plan.
type TCMCoordinatorAgent struct {
	Tasks    *tasks.Queue
	Registry *medical.PatientRegistry
	// HITL routes the patient-facing TCM welcome through human_review so a
	// nurse confirms it before the SMS goes out.
	HITL bool
}

func (a *TCMCoordinatorAgent) ID() string   { return "tcm_coordinator" }
func (a *TCMCoordinatorAgent) Name() string { return "Transitional Care Coordinator" }
func (a *TCMCoordinatorAgent) Capabilities() []string {
	return []string{"tcm", "discharge_followup", "med_reconciliation"}
}

func (a *TCMCoordinatorAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	d, err := medical.DecodeDischarge(msg.Content)
	if err != nil {
		env.Logf("[tcm_coordinator] invalid discharge: %v", err)
		return nil, err
	}

	days := d.FollowUpDays
	if len(days) == 0 {
		days = []int{2, 7, 30}
		if d.HighRisk {
			days = []int{1, 3, 7, 14, 30}
		}
	}

	var rec medical.MedReconciliation
	if a.Registry != nil {
		if existing, ok := a.Registry.Get(d.PatientID); ok {
			rec = medical.ReconcileMedications(existing.Medications, append(d.HomeMeds, d.NewMeds...))
			existing.Medications = append(d.HomeMeds, d.NewMeds...)
			existing.DischargedAt = d.DischargedAt
			a.Registry.Upsert(existing)
		}
	}

	if len(rec.Conflict) > 0 {
		a.Tasks.Create(tasks.NurseTask{
			Kind: "med_reconciliation", PatientID: d.PatientID, CaseID: d.CaseID,
			Priority: "urgent", DueAt: time.Now().Add(24 * time.Hour),
			Subject: "Reconcile discharge medications",
			Detail:  fmt.Sprintf("Conflicts: %v", rec.Conflict),
		})
	}

	createdIDs := []string{}
	for _, d2 := range days {
		t := a.Tasks.Create(tasks.NurseTask{
			Kind: "tcm_follow_up", PatientID: d.PatientID, CaseID: d.CaseID,
			Priority: priorityForDay(d2, d.HighRisk),
			DueAt:    d.DischargedAt.Add(time.Duration(d2) * 24 * time.Hour),
			Subject:  fmt.Sprintf("Day-%d post-discharge follow-up", d2),
			Detail:   "Confirm symptoms, med adherence, schedule visit.",
		})
		createdIDs = append(createdIDs, t.ID)
	}
	env.Logf("[tcm_coordinator] discharge %s primary=%s tasks=%d conflicts=%d",
		d.PatientID, d.PrimaryDx, len(createdIDs), len(rec.Conflict))

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = d.PatientID
	if d.CaseID != "" {
		meta["case_id"] = d.CaseID
	}
	body := fmt.Sprintf(`{"patient_id":%q,"case_id":%q,"task_count":%d}`, d.PatientID, d.CaseID, len(createdIDs))
	var out agent.Message
	if a.HITL {
		out = agent.NewMessage(a.ID(), "human_review", agent.RoleAgent,
			"review_request", body, reviewEnvelope(meta, reviewOptions{
				Kind:       "tcm_welcome_review",
				NextTo:     "engagement",
				NextType:   "tcm_tasks_created",
				Priority:   "high",
				Subject:    "Confirm TCM follow-up for " + d.PatientID + " (" + d.PrimaryDx + ")",
				SLAMinutes: 4 * 60,
			}))
	} else {
		out = agent.NewMessage(a.ID(), "engagement", agent.RoleAgent, "tcm_tasks_created", body, meta)
	}
	return []agent.Message{out}, nil
}

func priorityForDay(day int, high bool) string {
	switch {
	case day <= 2 && high:
		return "urgent"
	case day <= 7:
		return "high"
	default:
		return "normal"
	}
}
