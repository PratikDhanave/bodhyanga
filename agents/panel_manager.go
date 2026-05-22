package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// PanelManagerAgent runs population-health rules over a patient roster and emits
// a PanelReport plus a stream of outreach requests for each open gap.
//
// Input message Type:  panel_request    Content: {"condition":"htn|dm|chf|all"}
// Input message Type:  care_plan_ready  (passively tracks new plans)
// Output message Type: panel_report     Content: medical.PanelReport JSON
// Output message Type: engagement_request (per gap) -> engagement
type PanelManagerAgent struct {
	Registry *medical.PatientRegistry
	Rules    []medical.PanelRule
}

type panelRequest struct {
	Condition string `json:"condition,omitempty"`
}

func (a *PanelManagerAgent) ID() string   { return "panel_manager" }
func (a *PanelManagerAgent) Name() string { return "Panel Manager" }
func (a *PanelManagerAgent) Capabilities() []string {
	return []string{"panel_management", "care_gaps", "population_health"}
}

func (a *PanelManagerAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	switch msg.Type {
	case "care_plan_ready":
		env.Logf("[panel_manager] tracked new care plan")
		return nil, nil
	case "panel_request":
		return a.runPanel(msg, env)
	default:
		return nil, nil
	}
}

func (a *PanelManagerAgent) runPanel(msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req panelRequest
	_ = json.Unmarshal([]byte(msg.Content), &req)
	rules := a.Rules
	if len(rules) == 0 {
		rules = medical.DefaultPanelRules()
	}
	var patients []medical.PatientRecord
	if req.Condition != "" && req.Condition != "all" {
		patients = a.Registry.FilterByCondition(req.Condition)
	} else {
		patients = a.Registry.List()
	}
	report := medical.BuildPanelReport(patients, rules, time.Now())
	env.Logf("[panel_manager] panel=%d gaps=%d roll=%v",
		report.PanelSize, len(report.Gaps), report.ConditionRoll)

	body, _ := json.Marshal(report)
	out := []agent.Message{agent.NewMessage(a.ID(), "", agent.RoleAgent,
		"panel_report", string(body), copyMeta(msg.Metadata))}

	for _, gap := range report.Gaps {
		meta := copyMeta(msg.Metadata)
		meta["patient_id"] = gap.PatientID
		gapBody, _ := json.Marshal(gap)
		out = append(out, agent.NewMessage(a.ID(), "engagement", agent.RoleAgent,
			"engagement_request", string(gapBody), meta))
	}
	return out, nil
}
