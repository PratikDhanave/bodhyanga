package agents

import (
	"context"
	"fmt"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// RPMMonitorAgent ingests remote patient monitoring readings and flags alerts
// before sequential diagnosis proceeds (chronic care / RPM programs).
type RPMMonitorAgent struct{}

func (a *RPMMonitorAgent) ID() string   { return "rpm_monitor" }
func (a *RPMMonitorAgent) Name() string { return "RPM Monitor" }
func (a *RPMMonitorAgent) Capabilities() []string {
	return []string{"rpm_ingest", "vitals_triage", "news2_scoring"}
}

func (a *RPMMonitorAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	if len(state.RPMObservations) > 0 {
		state.RPMAlerts = medical.EvaluateRPMAlerts(state.RPMObservations)
		env.Logf("[rpm_monitor] %d RPM readings, %d alerts", len(state.RPMObservations), len(state.RPMAlerts))
		for _, alert := range state.RPMAlerts {
			env.Logf("[rpm_monitor] ALERT: %s", alert)
		}

		// Calculate NEWS2 from available RPM observations and append to alerts.
		news2Input := news2InputFromRPM(state.RPMObservations)
		news2Result := medical.CalculateNEWS2(news2Input)
		news2Alert := fmt.Sprintf(
			"NEWS2 score=%d risk=%s escalate=%v — %s",
			news2Result.Total,
			news2Result.RiskLevel,
			news2Result.Escalate,
			news2Result.Recommendation,
		)
		state.RPMAlerts = append(state.RPMAlerts, news2Alert)
		env.Logf("[rpm_monitor] NEWS2: %s", news2Alert)
	} else {
		env.Logf("[rpm_monitor] no RPM data — pass through")
	}

	out := agent.NewMessage(
		a.ID(),
		"diagnostic_supervisor",
		agent.RoleAgent,
		"case_state",
		medical.EncodeState(state),
		copyMeta(msg.Metadata),
	)
	return []agent.Message{out}, nil
}

// news2InputFromRPM maps RPMObservation slice values to NEWS2Input fields.
// Fields not present in the observations retain safe zero-value defaults
// (respiratory rate 16, SpO2 98, systolic BP 120, HR 75, Temp 37.0, alert).
func news2InputFromRPM(obs []medical.RPMObservation) medical.NEWS2Input {
	input := medical.NEWS2Input{
		RespiratoryRate: 16,
		SpO2:            98,
		SystolicBP:      120,
		HeartRate:       75,
		Consciousness:   "alert",
		Temperature:     37.0,
	}
	for _, o := range obs {
		switch o.Metric {
		case "respiratory_rate":
			input.RespiratoryRate = int(o.Value)
		case "spo2":
			input.SpO2 = o.Value
		case "on_o2":
			input.OnO2 = o.Value != 0
		case "copd":
			input.COPD = o.Value != 0
		case "systolic_bp":
			input.SystolicBP = int(o.Value)
		case "heart_rate":
			input.HeartRate = int(o.Value)
		case "consciousness_score":
			// Encode: 0=alert, else confused
			if o.Value == 0 {
				input.Consciousness = "alert"
			} else {
				input.Consciousness = "confused"
			}
		case "temperature":
			input.Temperature = o.Value
		}
	}
	return input
}
