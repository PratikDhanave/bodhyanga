package agents

import (
	"context"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// RPMMonitorAgent ingests remote patient monitoring readings and flags alerts
// before sequential diagnosis proceeds (chronic care / RPM programs).
type RPMMonitorAgent struct{}

func (a *RPMMonitorAgent) ID() string   { return "rpm_monitor" }
func (a *RPMMonitorAgent) Name() string { return "RPM Monitor" }
func (a *RPMMonitorAgent) Capabilities() []string {
	return []string{"rpm_ingest", "vitals_triage"}
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
