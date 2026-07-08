package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/engagement"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// EngagementAgent sends patient outreach for several upstream agents:
//
//	care_plan_ready      -> intro the new plan
//	engagement_request   -> gap-in-care SMS
//	tcm_tasks_created    -> post-discharge check-in
//	refill_response      -> refill status update
type EngagementAgent struct {
	Notifier engagement.Notifier
}

func (a *EngagementAgent) ID() string   { return "engagement" }
func (a *EngagementAgent) Name() string { return "Patient Engagement" }
func (a *EngagementAgent) Capabilities() []string {
	return []string{"patient_outreach", "notification"}
}

func (a *EngagementAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	patientID, _ := msg.Metadata["patient_id"].(string)
	caseID, _ := msg.Metadata["case_id"].(string)
	switch msg.Type {
	case "care_plan_ready":
		plan, err := medical.DecodeCarePlan(msg.Content)
		if err != nil {
			return nil, err
		}
		body := fmt.Sprintf("Your %s care plan is ready: %s. Next review %s.",
			plan.Condition, summaryGoals(plan), plan.NextReview.Format("Jan 2"))
		return a.send(env, patientID, caseID, engagement.ChannelSMS, "care_plan", body, plan.Condition)
	case "engagement_request":
		var gap medical.CareGap
		if err := json.Unmarshal([]byte(msg.Content), &gap); err != nil {
			return nil, err
		}
		body := engagement.FormatGapMessage(gap.PatientID, gap.Rule, gap.Detail)
		return a.send(env, gap.PatientID, caseID, engagement.ChannelSMS, "care_gap", body, gap.Rule)
	case "tcm_tasks_created":
		body := "Welcome home! A nurse will call within 48 hours to check on you. Reply HELP for urgent issues."
		return a.send(env, patientID, caseID, engagement.ChannelSMS, "tcm_welcome", body, "tcm_followup")
	case "refill_response":
		var resp map[string]string
		if err := json.Unmarshal([]byte(msg.Content), &resp); err != nil {
			return nil, err
		}
		body := fmt.Sprintf("Your refill request for %s is %s.", resp["medication"], resp["status"])
		return a.send(env, resp["patient_id"], caseID, engagement.ChannelSMS, "refill_status", body, resp["status"])
	default:
		return nil, nil
	}
}

func (a *EngagementAgent) send(env agent.Environment, patientID, caseID string,
	ch engagement.Channel, tmpl, body, reason string) ([]agent.Message, error) {
	if patientID == "" {
		return nil, nil
	}
	n := a.Notifier.Send(engagement.Notification{
		PatientID: patientID,
		Channel:   ch,
		Template:  tmpl,
		Body:      body,
		Reason:    reason,
		CaseID:    caseID,
	})
	env.Logf("[engagement] %s -> %s template=%s reason=%s", ch, patientID, tmpl, reason)
	out := agent.NewMessage(a.ID(), "", agent.RoleAgent, "engagement_sent",
		fmt.Sprintf(`{"id":%q,"patient_id":%q,"channel":%q,"template":%q}`, n.ID, patientID, ch, tmpl),
		map[string]any{"patient_id": patientID, "case_id": caseID})
	return []agent.Message{out}, nil
}

func summaryGoals(plan medical.CarePlan) string {
	if len(plan.Goals) == 0 {
		return "follow up scheduled"
	}
	g := plan.Goals[0]
	return fmt.Sprintf("target %s %.1f %s", g.Metric, g.Target, g.Unit)
}
