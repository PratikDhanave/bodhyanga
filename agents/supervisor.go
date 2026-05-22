package agents

import (
	"context"
	"fmt"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// DiagnosticSupervisor emulates MAI-DxO: coordinates the virtual physician panel
// through sequential diagnosis (ask → test → verify → diagnose).
type DiagnosticSupervisor struct{}

func (a *DiagnosticSupervisor) ID() string   { return "diagnostic_supervisor" }
func (a *DiagnosticSupervisor) Name() string { return "Diagnostic Orchestrator (MAI-DxO style)" }
func (a *DiagnosticSupervisor) Capabilities() []string {
	return []string{"supervise_diagnosis", "sequential_diagnosis"}
}

func (a *DiagnosticSupervisor) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	switch msg.Type {
	case "case_state":
		return a.routeInitial(ctx, msg, env)
	case "questions_complete", "tests_complete", "reasoning_verified":
		return a.routeNext(ctx, msg, env)
	case "tests_revise":
		return a.reviseTests(ctx, msg, env)
	case "diagnosis":
		return a.finalize(ctx, msg, env)
	default:
		env.Logf("[supervisor] unhandled type=%s", msg.Type)
		return nil, nil
	}
}

// reviseTests handles a cost_guardian denial by dropping the most expensive
// test from the workup and re-submitting to the guardian. Bounded: when no
// tests remain, the case is aborted with a log line rather than looping.
func (a *DiagnosticSupervisor) reviseTests(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, _ := medical.DecodeState(msg.Content)
	if len(state.TestsOrdered) == 0 {
		env.Logf("[supervisor] tests_revise: no tests left to drop — aborting case %s", state.CaseID)
		return nil, nil
	}
	maxIdx := 0
	for i, t := range state.TestsOrdered {
		if t.CostUSD > state.TestsOrdered[maxIdx].CostUSD {
			maxIdx = i
		}
	}
	dropped := state.TestsOrdered[maxIdx]
	state.TestsOrdered = append(state.TestsOrdered[:maxIdx], state.TestsOrdered[maxIdx+1:]...)
	state.TotalCostUSD -= dropped.CostUSD
	if state.TestResults != nil {
		delete(state.TestResults, dropped.Name)
	}
	env.Logf("[supervisor] tests_revise: dropped %s ($%.0f) — new total $%.2f",
		dropped.Name, dropped.CostUSD, state.TotalCostUSD)
	out := agent.NewMessage(a.ID(), "cost_guardian", agent.RoleAgent,
		"case_state", medical.EncodeState(state), copyMeta(msg.Metadata))
	return []agent.Message{out}, nil
}

func (a *DiagnosticSupervisor) routeInitial(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, _ := medical.DecodeState(msg.Content)
	env.Logf("[supervisor] starting sequential diagnosis for case %s", state.CaseID)
	out := agent.NewMessage(a.ID(), "questioner", agent.RoleAgent, "case_state", msg.Content, copyMeta(msg.Metadata))
	return []agent.Message{out}, nil
}

func (a *DiagnosticSupervisor) routeNext(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	switch msg.Type {
	case "questions_complete":
		env.Logf("[supervisor] history gathered — ordering tests")
		out := agent.NewMessage(a.ID(), "test_planner", agent.RoleAgent, "case_state", msg.Content, copyMeta(msg.Metadata))
		return []agent.Message{out}, nil
	case "tests_complete":
		env.Logf("[supervisor] results available — forming diagnosis")
		out := agent.NewMessage(a.ID(), "diagnostician", agent.RoleAgent, "case_state", msg.Content, copyMeta(msg.Metadata))
		return []agent.Message{out}, nil
	case "reasoning_verified":
		env.Logf("[supervisor] reasoning verified — requesting final diagnosis")
		out := agent.NewMessage(a.ID(), "diagnostician", agent.RoleAgent, "case_state", msg.Content, copyMeta(msg.Metadata))
		return []agent.Message{out}, nil
	}
	return nil, nil
}

func (a *DiagnosticSupervisor) finalize(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, _ := medical.DecodeState(msg.Content)
	env.Logf("[supervisor] ═══ FINAL DIAGNOSIS for case %s ═══", state.CaseID)
	env.Logf("[supervisor] working diagnosis: %s", state.WorkingDiagnosis)
	env.Logf("[supervisor] total diagnostic cost: $%.2f (budget $%.2f)", state.TotalCostUSD, state.MaxCostUSD)
	if state.GoldDiagnosis != "" {
		match := state.WorkingDiagnosis == state.GoldDiagnosis
		env.Logf("[supervisor] gold standard: %s | match=%v", state.GoldDiagnosis, match)
	}
	env.Logf("[supervisor] hypotheses considered: %v", state.Hypotheses)
	if len(state.RPMAlerts) > 0 {
		env.Logf("[supervisor] RPM alerts at intake: %v", state.RPMAlerts)
	}
	metrics := eval.ScoreCase(state, metadataInt(msg.Metadata, "message_steps", 11))
	env.Logf("[supervisor] eval: accuracy=%.0f cost=$%.2f utilization=%.0f%% rpm_alerts=%d",
		metrics.Accuracy*100, metrics.CostUSD, metrics.CostUtilization*100, metrics.RPMAlertCount)
	return nil, nil
}

func metadataInt(m map[string]any, key string, def int) int {
	if m == nil {
		return def
	}
	if v, ok := m[key].(int); ok {
		return v
	}
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return def
}

// copyMeta returns a shallow copy of metadata for downstream messages.
func copyMeta(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func metadataString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func metadataFloat(m map[string]any, key string, def float64) float64 {
	if m == nil {
		return def
	}
	if v, ok := m[key].(float64); ok {
		return v
	}
	return def
}

// PanelReport formats a human-readable summary (for API layer later).
func PanelReport(state medical.CaseState) string {
	return fmt.Sprintf("case=%s diagnosis=%q cost=$%.2f tests=%d questions=%d",
		state.CaseID, state.WorkingDiagnosis, state.TotalCostUSD,
		len(state.TestsOrdered), len(state.QuestionsAsked))
}
