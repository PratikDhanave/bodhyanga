package agents

// File supervisor.go: the diagnostic_supervisor agent — a pure state
// machine on msg.Type that drives the MAI-DxO-style sequential diagnosis
// loop documented in docs/architecture.md § Sequential diagnosis state
// machine and docs/agents.md § diagnostic_supervisor.
//
// What this agent IS:
//
//   - The router for an 8-agent, 12-message-per-case diagnostic workflow.
//   - The terminal scoring point: when reasoning_verifier emits
//     type="diagnosis", finalize() logs the result, scores it against
//     state.GoldDiagnosis if present, and lets the orchestrator's
//     eval.Store side-channel persist the InteractionRecord.
//   - Stateless across cases — every routing decision is computed from
//     msg.Type and msg.Content alone; the supervisor is a zero-value
//     struct that can be shared across all in-flight cases without lock.
//
// What this agent is NOT:
//
//   - **No clinical logic.** The supervisor never inspects test results,
//     vital signs, conditions, or diagnoses to make routing decisions. If
//     you find yourself adding `if state.Condition == "acs"` here, the
//     clinical logic belongs in diagnostician.go (specialty rules) or
//     test_planner.go (workup dispatch).
//   - **No direct agent calls.** Routing happens by emitting a new
//     agent.Message addressed to the next agent's ID. The bus + the
//     orchestrator's 30-line closure handle delivery, governance, and
//     audit.
//   - **No memory of prior cases.** Each invocation operates on the
//     CaseState payload it just received. Cross-case state would live in
//     pkg/memory or pkg/persistence, never here.

import (
	"context"
	"fmt"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// DiagnosticSupervisor emulates MAI-DxO: coordinates the virtual physician
// panel through sequential diagnosis (ask → test → verify → diagnose).
//
// Zero-value-usable: there is no construction step. cmd/demo and cmd/care
// register a &DiagnosticSupervisor{} directly. The orchestrator wires the
// eval.Store side-channel (PR #12) at its own layer, so this struct stays
// dependency-free even as audit / eval persistence is added behind it.
type DiagnosticSupervisor struct{}

// ID is the bus address the orchestrator and every other agent route to.
// Changing this string is a breaking change (every agent that emits to
// "diagnostic_supervisor" would need updating). The id is also matched
// in pkg/orchestration/orchestrator.go for the eval-store side-channel
// (only this agent's terminal "diagnosis" hop triggers ScoreCase).
func (a *DiagnosticSupervisor) ID() string   { return "diagnostic_supervisor" }
func (a *DiagnosticSupervisor) Name() string { return "Diagnostic Orchestrator (MAI-DxO style)" }

// Capabilities advertise to the registry that this agent supervises a
// diagnostic case. Not currently consumed by FindByCapability dispatch
// (the supervisor is hard-coded as the routing hub by the rest of the
// pipeline), but kept for documentation and future capability-based
// dispatch (e.g. an inbox_triage that wants to discover an active case
// supervisor).
func (a *DiagnosticSupervisor) Capabilities() []string {
	return []string{"supervise_diagnosis", "sequential_diagnosis"}
}

// HandleMessage is the state machine. Each case in the switch describes a
// distinct point in the sequential diagnosis flow. See docs/architecture.md
// § Sequential diagnosis state machine for the full 12-message trace.
//
// Routing table:
//
//	incoming Type            previous agent       next action
//	───────────────────────────────────────────────────────────────────
//	"case_state"             rpm_monitor / intake forward to questioner
//	"questions_complete"     questioner           forward to test_planner
//	"tests_complete"         cost_guardian        forward to diagnostician
//	"reasoning_verified"     reasoning_verifier   re-ask diagnostician
//	"tests_revise"           cost_guardian        drop costliest test, retry
//	"diagnosis"              reasoning_verifier   finalize + score (terminal)
//
// Unknown types are logged and dropped. This is the right behaviour
// because the orchestrator's audit log already captured the unknown
// message as KindMessage; an error return here would record a redundant
// KindAgentError with the same case_id and confuse downstream forensics.
func (a *DiagnosticSupervisor) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	switch msg.Type {
	case "case_state":
		// First entry from rpm_monitor (which itself was forwarded by
		// intake). Kick off history-taking.
		return a.routeInitial(ctx, msg, env)
	case "questions_complete", "tests_complete", "reasoning_verified":
		// Mid-flow hops. routeNext disambiguates by msg.Type to pick
		// the next agent in the pipeline.
		return a.routeNext(ctx, msg, env)
	case "tests_revise":
		// Cost-rework loop driven by cost_guardian's denial. See
		// reviseTests; bounded to avoid an infinite trim-and-retry.
		return a.reviseTests(ctx, msg, env)
	case "diagnosis":
		// Terminal: reasoning_verifier accepted (or fell through to
		// low-confidence Undifferentiated). Score, log, stop.
		return a.finalize(ctx, msg, env)
	default:
		// Unknown message type. Log and drop — never propagate, never
		// error (the orchestrator already has a KindMessage audit
		// entry; double-recording as KindAgentError would mislead).
		env.Logf("[supervisor] unhandled type=%s", msg.Type)
		return nil, nil
	}
}

// reviseTests handles a cost_guardian denial by dropping the most expensive
// test from the workup and re-submitting to the guardian. Bounded: when no
// tests remain, the case is aborted with a log line rather than looping.
//
// The cost-rework loop in plain English: test_planner emitted N tests
// totalling state.TotalCostUSD. cost_guardian compared that to
// state.MaxCostUSD and decided it busts the budget; rather than reject
// the case outright, it routes a "tests_revise" back to the supervisor.
// The supervisor drops the costliest test (the cheapest signal-loss per
// dollar saved heuristic), recomputes the total, and re-sends to
// cost_guardian. Each round drops one test; in the worst case the loop
// terminates when TestsOrdered is empty (then the diagnostician will
// fall through to Undifferentiated and finalize on low confidence).
//
// This is illustrative — production cost optimisation would pick the
// test with the lowest expected information gain per dollar, not just
// the highest cost. See bodhyanga-linkedin/01-bench-42-to-85.md for the
// per-condition workup map that informs which tests carry the most
// diagnostic signal per condition.
func (a *DiagnosticSupervisor) reviseTests(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, _ := medical.DecodeState(msg.Content)
	// Bounded termination: empty workup means no further trim is
	// possible. We don't loop; we log and let the case die quietly.
	// The orchestrator's KindMessage audit captures the dead-end.
	if len(state.TestsOrdered) == 0 {
		env.Logf("[supervisor] tests_revise: no tests left to drop — aborting case %s", state.CaseID)
		return nil, nil
	}
	// Find the most expensive test in the current workup; drop it.
	// Ties go to the lowest index (first-listed) — stable + cheap.
	maxIdx := 0
	for i, t := range state.TestsOrdered {
		if t.CostUSD > state.TestsOrdered[maxIdx].CostUSD {
			maxIdx = i
		}
	}
	dropped := state.TestsOrdered[maxIdx]
	state.TestsOrdered = append(state.TestsOrdered[:maxIdx], state.TestsOrdered[maxIdx+1:]...)
	state.TotalCostUSD -= dropped.CostUSD
	// Important: also drop the simulated result so the diagnostician
	// can't fire a specialty rule on a test the budget no longer
	// includes (would be a stale signal from a previous round).
	if state.TestResults != nil {
		delete(state.TestResults, dropped.Name)
	}
	env.Logf("[supervisor] tests_revise: dropped %s ($%.0f) — new total $%.2f",
		dropped.Name, dropped.CostUSD, state.TotalCostUSD)
	// Re-submit to cost_guardian (NOT test_planner) — the workup is
	// authoritative; we just need the budget check to pass.
	out := agent.NewMessage(a.ID(), "cost_guardian", agent.RoleAgent,
		"case_state", medical.EncodeState(state), copyMeta(msg.Metadata))
	return []agent.Message{out}, nil
}

// routeInitial is the first stop: an intake-emitted (or rpm_monitor-
// forwarded) case_state arrives and we send it to questioner to begin
// history taking. We pass msg.Content through unchanged — intake has
// already constructed the CaseState JSON; no need to decode + re-encode
// just to add the supervisor's name to it.
func (a *DiagnosticSupervisor) routeInitial(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, _ := medical.DecodeState(msg.Content)
	env.Logf("[supervisor] starting sequential diagnosis for case %s", state.CaseID)
	out := agent.NewMessage(a.ID(), "questioner", agent.RoleAgent, "case_state", msg.Content, copyMeta(msg.Metadata))
	return []agent.Message{out}, nil
}

// routeNext picks the next agent in the pipeline based on which step
// just completed. Each case is a single hop — the supervisor is the
// ONLY agent that ever needs this branching. Other agents have a fixed
// (input type, output type) pair.
func (a *DiagnosticSupervisor) routeNext(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	switch msg.Type {
	case "questions_complete":
		// questioner finished its 3–5 history questions; hand off to
		// test_planner. test_planner reads state.Condition to pick
		// the condition-specific workup (PR #11) — the supervisor
		// itself never branches on condition.
		env.Logf("[supervisor] history gathered — ordering tests")
		out := agent.NewMessage(a.ID(), "test_planner", agent.RoleAgent, "case_state", msg.Content, copyMeta(msg.Metadata))
		return []agent.Message{out}, nil
	case "tests_complete":
		// cost_guardian approved the workup (or the rework loop
		// finished). Send the case state — including TestResults
		// populated by test_planner's simulator — to diagnostician
		// for specialty-rule inference (see diagnostician.go).
		env.Logf("[supervisor] results available — forming diagnosis")
		out := agent.NewMessage(a.ID(), "diagnostician", agent.RoleAgent, "case_state", msg.Content, copyMeta(msg.Metadata))
		return []agent.Message{out}, nil
	case "reasoning_verified":
		// Optional verifier feedback path. The shipped
		// reasoning_verifier today emits type="diagnosis" directly
		// (accept or low-conf fallback), so this branch is reserved
		// for richer LLM-backed verifiers (see
		// agents/reasoning_verifier_llm.go) that may want to bounce
		// back to the diagnostician for a revised proposal before
		// committing to a final diagnosis.
		env.Logf("[supervisor] reasoning verified — requesting final diagnosis")
		out := agent.NewMessage(a.ID(), "diagnostician", agent.RoleAgent, "case_state", msg.Content, copyMeta(msg.Metadata))
		return []agent.Message{out}, nil
	}
	return nil, nil
}

// finalize is the terminal step. The supervisor returns nil — no further
// messages — and the orchestrator-side closure (see
// pkg/orchestration/orchestrator.go) notices len(out)==0 +
// agentID=="diagnostic_supervisor" + msg.Type=="diagnosis" and triggers
// recordCaseEval to mirror the case into eval.Store (PR #12 wiring).
//
// Why scoring lives in the orchestrator side-channel and not here: we
// want this struct to stay a zero-value-usable type with no fields. Tying
// eval.Store into the supervisor would force every cmd/* to inject it,
// which couples the demo to a sink it doesn't need. The orchestrator is
// the natural place because it already holds the eval.Store reference for
// EVERY case; pivoting on agent ID + msg type keeps the hook narrow.
func (a *DiagnosticSupervisor) finalize(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, _ := medical.DecodeState(msg.Content)
	env.Logf("[supervisor] ═══ FINAL DIAGNOSIS for case %s ═══", state.CaseID)
	env.Logf("[supervisor] working diagnosis: %s", state.WorkingDiagnosis)
	env.Logf("[supervisor] total diagnostic cost: $%.2f (budget $%.2f)", state.TotalCostUSD, state.MaxCostUSD)
	// gold_diagnosis is set by the bench runner (cmd/bench) and by
	// fixture-driven cmd/demo runs; never present in production where
	// the truth is unknown. The match check is a string-equality only —
	// see eval.ScoreCase for the more permissive synonym-aware scoring.
	if state.GoldDiagnosis != "" {
		match := state.WorkingDiagnosis == state.GoldDiagnosis
		env.Logf("[supervisor] gold standard: %s | match=%v", state.GoldDiagnosis, match)
	}
	env.Logf("[supervisor] hypotheses considered: %v", state.Hypotheses)
	if len(state.RPMAlerts) > 0 {
		env.Logf("[supervisor] RPM alerts at intake: %v", state.RPMAlerts)
	}
	// Local metrics log (orchestrator does a more durable mirror into
	// eval.Store via the side-channel). The "11" default for
	// message_steps matches the canonical 12-message trace (steps 1–11
	// happen before this finalize) — if the metadata key is missing
	// (unit tests, partial flows) we fall back to that constant.
	metrics := eval.ScoreCase(state, metadataInt(msg.Metadata, "message_steps", 11))
	env.Logf("[supervisor] eval: accuracy=%.0f cost=$%.2f utilization=%.0f%% rpm_alerts=%d",
		metrics.Accuracy*100, metrics.CostUSD, metrics.CostUtilization*100, metrics.RPMAlertCount)
	// Returning nil is the signal to the orchestrator that this case is
	// done — recordCaseEval triggers, the bus has no more work for this
	// trace, and the supervisor goroutine returns.
	return nil, nil
}

// metadataInt extracts an integer-valued metadata key with a default.
// JSON-decoded metadata produces float64 even for whole numbers, so this
// helper accepts both int (direct in-process) and float64 (post-JSON)
// shapes. Returns the default when the key is absent or the wrong type —
// never panics on a malformed envelope.
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
//
// Why every routing hop calls this: msg.Metadata is a map shared by
// reference. If we passed msg.Metadata directly to NewMessage, every
// downstream mutation (e.g. adding "hitl_review_id" in human_review,
// adding "spent_usd" in cost_guardian) would silently mutate the
// upstream sender's metadata too — and because handlers run in separate
// goroutines, that's a data race the race detector will flag. Shallow
// copy is enough because the values are primitives (string, float64,
// int) or other maps treated as immutable downstream.
func copyMeta(m map[string]any) map[string]any {
	if m == nil {
		// Return a non-nil empty map so downstream agents can write
		// without an extra nil check.
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// metadataString extracts a string-valued metadata key. Returns "" on
// missing or wrong type. Used by human_review.go for the HITL envelope
// keys (hitl_kind, hitl_next_to, hitl_next_type) and by the supervisor
// for case_id / tenant_id propagation.
func metadataString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// metadataFloat extracts a float64 metadata key with a default. JSON
// decoding produces float64 for every numeric value, so this is the
// canonical "give me a number from metadata" helper. Used for
// hitl_sla_minutes, max_cost_usd, spent_usd.
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
//
// Not a hot path; used by cmd/care HTTP handlers and ad-hoc debug logs.
// Stable string format — tests assert against substrings of this output.
func PanelReport(state medical.CaseState) string {
	return fmt.Sprintf("case=%s diagnosis=%q cost=$%.2f tests=%d questions=%d",
		state.CaseID, state.WorkingDiagnosis, state.TotalCostUSD,
		len(state.TestsOrdered), len(state.QuestionsAsked))
}
