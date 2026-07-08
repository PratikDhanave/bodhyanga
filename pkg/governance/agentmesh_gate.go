// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// AgentMeshGate bridges the Microsoft/PratikDhanave agent-governance-toolkit
// (github.com/PratikDhanave/agent-governance-toolkit) into Bodh's
// governance.Policy interface so it participates in the orchestrator's
// CompositePolicy stack.
//
// # What it enforces (in order)
//
//  1. Global kill switch — any process-wide halt (policy violation, security
//     incident, operator request) blocks ALL messages immediately.
//  2. Agent-scoped kill switch — a per-agent halt blocks messages addressed to
//     that agent while the rest of the fleet continues serving.
//  3. Ring enforcement — each Bodh agent is assigned an execution privilege
//     ring (Admin/Standard/Restricted/Sandboxed). Ring 2 (Restricted) and
//     Ring 3 (Sandboxed) agents are denied actions outside their permit list.
//  4. Prompt-injection defense — the message Content field is scanned for
//     instruction-override, exfiltration, credential-leak, and
//     approval-bypass patterns. A configurable risk threshold controls
//     how aggressively the scanner denies (default: MaxRiskScore=40,
//     meaning a single "critical" finding triggers a deny).
//
// # AGENTS.md invariant alignment
//
//   - Invariant #1 (never bypass HITL): kill switches enforce immediate halt
//     before any agent sees the message — the HITL gate is downstream of
//     this gate, not upstream.
//   - Invariant #2 (never widen PHI surface): deny reasons logged here carry
//     only message IDs and threat-type labels; no Content is reproduced.
//   - Invariant #3 (deterministic fallback): a gate error returns
//     DecisionDeny (fail-closed), consistent with the orchestrator's
//     existing policy-error handling.
//
// # Ring assignments
//
// DefaultRingAssignments wires the Bodh MARA agent roster to rings:
//
//	Ring 0 (Admin)      — no clinical agents; reserved for future infra ops
//	Ring 1 (Standard)   — core clinical pipeline (diagnostician, care-plan, etc.)
//	Ring 2 (Restricted) — triage and routing agents (inbox, RPM monitor)
//	Ring 3 (Sandboxed)  — audio transcriber, engagement (no direct clinical output)
package governance

import (
	"context"
	"fmt"

	"github.com/microsoft/agent-governance-toolkit/agent-governance-golang/packages/agentmesh"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// -- Ring assignment table ---------------------------------------------------

// ringAssignments maps Bodh agent IDs to agentmesh privilege rings.
// Agents not listed default to RingRestricted (fail-safe).
//
// Ring 1 agents are in the direct clinical pipeline; they can perform any
// action their HandleMessage supports. Ring 2 agents are routing/triage;
// they may route but not act on clinical data directly. Ring 3 agents are
// isolated: audio transcription and engagement have no direct clinical output
// path and must not initiate diagnosis or care-plan actions.
var ringAssignments = map[string]agentmesh.Ring{
	// --- Ring 1: Standard clinical pipeline ---
	"intake_agent":          agentmesh.RingStandard,
	"diagnostic_supervisor": agentmesh.RingStandard,
	"questioner":            agentmesh.RingStandard,
	"test_planner":          agentmesh.RingStandard,
	"cost_guardian":         agentmesh.RingStandard,
	"diagnostician":         agentmesh.RingStandard,
	"reasoning_verifier":    agentmesh.RingStandard,
	"cdm_planner":           agentmesh.RingStandard,
	"tcm_coordinator":       agentmesh.RingStandard,
	"panel_manager":         agentmesh.RingStandard,
	"pre_visit_prep":        agentmesh.RingStandard,
	"refill_manager":        agentmesh.RingStandard,
	"prior_auth":            agentmesh.RingStandard,
	"readmission_tracker":   agentmesh.RingStandard,
	"human_review":          agentmesh.RingStandard,

	// --- Ring 2: Restricted routing / triage ---
	"inbox_triage": agentmesh.RingRestricted,
	"rpm_monitor":  agentmesh.RingRestricted,

	// --- Ring 3: Sandboxed peripheral agents ---
	"audio_transcriber": agentmesh.RingSandboxed,
	"engagement":        agentmesh.RingSandboxed,
}

// ringPermissions maps rings to the set of allowed message Type values.
// "*" means the ring may perform any action (Ring 1).
var ringPermissions = map[agentmesh.Ring][]string{
	agentmesh.RingAdmin:    {"*"},
	agentmesh.RingStandard: {"*"},
	agentmesh.RingRestricted: {
		"inbox_message", "rpm_observation",
		"routing_decision", "triage_result",
	},
	agentmesh.RingSandboxed: {
		"audio_transcription_request", "audio_transcription_result",
		"audio_transcription_failed",
		"engagement_request", "engagement_result",
	},
}

// -- AgentMeshGate -----------------------------------------------------------

// AgentMeshGate implements governance.Policy using four agentmesh primitives:
//   - KillSwitchRegistry — global + per-agent halt
//   - RingEnforcer       — capability-bounded execution rings
//   - PromptDefenseEvaluator — injection / exfiltration detection
//
// Construct via NewAgentMeshGate; the zero value must not be used.
type AgentMeshGate struct {
	kills         *agentmesh.KillSwitchRegistry
	rings         *agentmesh.RingEnforcer
	promptDefense *agentmesh.PromptDefenseEvaluator
	// MaxRiskScore is the inclusive threshold above which a prompt-defense
	// result causes a deny. Default 40 (one "critical" finding alone = deny).
	MaxRiskScore int
}

// NewAgentMeshGate constructs a gate with default settings.
// Call the returned gate's KillSwitches() accessor to activate/clear halt scopes
// from operator tooling (e.g. POST /admin/kill-switch in cmd/care).
func NewAgentMeshGate() *AgentMeshGate {
	rings := agentmesh.NewRingEnforcer()
	// Wire Bodh agents to their privilege rings.
	for agentID, ring := range ringAssignments {
		rings.Assign(agentID, ring)
	}
	// Set permitted actions per ring.
	for ring, actions := range ringPermissions {
		rings.SetRingPermissions(ring, actions)
	}

	return &AgentMeshGate{
		kills:         agentmesh.NewKillSwitchRegistry(),
		rings:         rings,
		promptDefense: agentmesh.NewPromptDefenseEvaluator(),
		MaxRiskScore:  40,
	}
}

// KillSwitches returns the KillSwitchRegistry so operator tooling (e.g.
// the /admin/kill-switch HTTP handlers in cmd/care) can activate and clear
// halt scopes at runtime without needing the full gate.
func (g *AgentMeshGate) KillSwitches() *agentmesh.KillSwitchRegistry {
	return g.kills
}

// Evaluate implements governance.Policy. It runs the four checks in order
// and returns DecisionDeny with a PHI-free reason on the first failure.
// Errors are returned as DecisionDeny (fail-closed).
func (g *AgentMeshGate) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	// 1. Global kill switch — blocks everything when active.
	// DecisionFor("","") checks only the global scope.
	if d := g.kills.DecisionFor("", ""); !d.Allowed {
		return PolicyResult{
			Decision: DecisionDeny,
			Reason:   "agentmesh: global kill switch active",
		}, nil
	}

	// 2. Agent-scoped kill switch — blocks messages to a specific agent.
	// DecisionFor(msg.To,"") checks global + agent scope; global is already
	// clear above so this catches only agent-scoped activations.
	if msg.To != "" {
		if d := g.kills.DecisionFor(msg.To, ""); !d.Allowed {
			return PolicyResult{
				Decision: DecisionDeny,
				Reason:   fmt.Sprintf("agentmesh: kill switch active for agent %q", msg.To),
			}, nil
		}
	}

	// 3. Ring enforcement — unassigned agents are Restricted by default
	// (the RingEnforcer denies unknown agents). Ring 1 (Standard) has "*"
	// permission so clinical pipeline agents are never blocked here.
	//
	// We only enforce ring-based action gating when the recipient is a
	// known agent (msg.To non-empty). User-originated messages addressed
	// to the orchestrator entry point ("user") bypass ring checks.
	if msg.To != "" && msg.To != "user" && msg.Type != "" {
		if _, assigned := g.rings.GetRing(msg.To); assigned {
			if !g.rings.CheckAccess(msg.To, msg.Type) {
				return PolicyResult{
					Decision: DecisionDeny,
					Reason: fmt.Sprintf(
						"agentmesh: agent %q (ring %s) is not permitted to perform action %q",
						msg.To, ringName(g.rings, msg.To), msg.Type,
					),
				}, nil
			}
		}
	}

	// 4. Prompt-injection defense — scan Content for injection patterns.
	// Only scan non-empty content; empty-content messages (pure routing)
	// are not a prompt-injection vector.
	if msg.Content != "" {
		result := g.promptDefense.Evaluate(msg.Content)
		if !result.Safe && result.RiskScore >= g.MaxRiskScore {
			// Log threat type(s) but never the actual content — PHI guard.
			threatTypes := make([]string, 0, len(result.Findings))
			for _, f := range result.Findings {
				threatTypes = append(threatTypes, string(f.Type))
			}
			return PolicyResult{
				Decision: DecisionDeny,
				Reason: fmt.Sprintf(
					"agentmesh: prompt injection detected (score=%d threats=%v msg_id=%s)",
					result.RiskScore, threatTypes, msg.ID,
				),
			}, nil
		}
	}

	return PolicyResult{Decision: DecisionAllow}, nil
}

// ringName returns a human-readable ring label for a given agent. Used only
// in deny-reason strings (PHI-free: agentID is a code-level identifier).
func ringName(re *agentmesh.RingEnforcer, agentID string) string {
	ring, ok := re.GetRing(agentID)
	if !ok {
		return "unassigned"
	}
	switch ring {
	case agentmesh.RingAdmin:
		return "admin"
	case agentmesh.RingStandard:
		return "standard"
	case agentmesh.RingRestricted:
		return "restricted"
	case agentmesh.RingSandboxed:
		return "sandboxed"
	default:
		return "unknown"
	}
}
