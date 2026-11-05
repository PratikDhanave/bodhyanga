// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// kill_switch.go exposes the agentmesh KillSwitchRegistry over HTTP so
// on-call operators can halt all traffic or quarantine a specific agent
// without restarting the process.
//
// # Endpoints
//
//   GET    /admin/kill-switch          — list all currently-active halt scopes
//   POST   /admin/kill-switch          — activate a halt scope
//   DELETE /admin/kill-switch          — clear (re-enable) a halt scope
//
// # Scope encoding (JSON field "scope")
//
//   "global"              — process-wide halt (blocks every message)
//   "agent:<agentID>"     — halt a single named agent
//   "capability:<cap>"    — halt a single capability across all agents
//
// # Reason values
//
//   "policy_violation" | "security_incident" | "operator_request" | "error_budget_exhausted"
//
// # Security note
//
// These endpoints must be protected by the JWT + OPA middleware stack at the
// gateway level (not here — cmd/care's middleware chain already wraps the mux).
// In the default dev compose stack the /admin/ prefix is not exposed outside
// the bodhyanga-net bridge network.
package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/microsoft/agent-governance-toolkit/agent-governance-golang/packages/agentmesh"
)

// killSwitchServer handles /admin/kill-switch/* and delegates state to the
// agentmesh.KillSwitchRegistry owned by the AgentMeshGate.
type killSwitchServer struct {
	reg *agentmesh.KillSwitchRegistry
}

// killSwitchActivateReq is the POST /admin/kill-switch body.
type killSwitchActivateReq struct {
	// Scope is one of "global", "agent:<agentID>", "capability:<cap>".
	Scope   string `json:"scope"`
	// Reason is one of the KillSwitchReason constants.
	Reason  string `json:"reason"`
	// Message is a free-text operator note (PHI-free; stored in history).
	Message string `json:"message"`
}

// killSwitchClearReq is the DELETE /admin/kill-switch body.
type killSwitchClearReq struct {
	Scope   string `json:"scope"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// parseScope converts a string like "global" or "agent:diagnostician" into
// an agentmesh.KillSwitchScope. Returns false if the format is invalid.
func (s *killSwitchServer) parseScope(raw string) (agentmesh.KillSwitchScope, bool) {
	switch {
	case raw == "global":
		return agentmesh.GlobalKillSwitchScope(), true
	case strings.HasPrefix(raw, "agent:"):
		id := strings.TrimPrefix(raw, "agent:")
		if strings.TrimSpace(id) == "" {
			return agentmesh.KillSwitchScope{}, false
		}
		return agentmesh.AgentKillSwitchScope(id), true
	case strings.HasPrefix(raw, "capability:"):
		cap := strings.TrimPrefix(raw, "capability:")
		if strings.TrimSpace(cap) == "" {
			return agentmesh.KillSwitchScope{}, false
		}
		return agentmesh.CapabilityKillSwitchScope(cap), true
	default:
		return agentmesh.KillSwitchScope{}, false
	}
}

// activate handles POST /admin/kill-switch.
// On success it returns 200 with {"scope":"...","event":{...}}.
func (s *killSwitchServer) activate(w http.ResponseWriter, r *http.Request) {
	var req killSwitchActivateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	scope, ok := s.parseScope(req.Scope)
	if !ok {
		http.Error(w, `scope must be "global", "agent:<id>", or "capability:<cap>"`, http.StatusBadRequest)
		return
	}
	event, err := s.reg.Activate(scope, agentmesh.KillSwitchReason(req.Reason), req.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"scope": scope.String(), "event": event})
}

// clear handles DELETE /admin/kill-switch.
// On success it returns 200 with {"scope":"...","event":{...}}.
func (s *killSwitchServer) clear(w http.ResponseWriter, r *http.Request) {
	var req killSwitchClearReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	scope, ok := s.parseScope(req.Scope)
	if !ok {
		http.Error(w, `scope must be "global", "agent:<id>", or "capability:<cap>"`, http.StatusBadRequest)
		return
	}
	event, err := s.reg.Clear(scope, agentmesh.KillSwitchReason(req.Reason), req.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"scope": scope.String(), "event": event})
}

// list handles GET /admin/kill-switch.
// Returns {"active":[{scope,reason,message,since},...]} — only currently-active
// scopes are included; cleared scopes are omitted. History is collapsed to the
// most-recent event per scope so toggles are idempotent in the output.
func (s *killSwitchServer) list(w http.ResponseWriter, _ *http.Request) {
	history := s.reg.History()

	// Walk history front-to-back; later entries overwrite earlier ones for the
	// same scope, so the map always holds the most-recent state.
	latest := make(map[string]agentmesh.KillSwitchHistoryEntry, len(history))
	for _, h := range history {
		latest[h.Scope.String()] = h
	}

	active := make([]map[string]any, 0, len(latest))
	for _, h := range latest {
		if h.Event.Active {
			active = append(active, map[string]any{
				"scope":   h.Scope.String(),
				"reason":  h.Event.Reason,
				"message": h.Event.Message,
				"since":   h.Event.Timestamp,
			})
		}
	}
	writeJSON(w, map[string]any{"active": active})
}

// mount registers the three admin endpoints on mux.
// Must be called before the server starts accepting requests.
func (s *killSwitchServer) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/kill-switch", s.list)
	mux.HandleFunc("POST /admin/kill-switch", s.activate)
	mux.HandleFunc("DELETE /admin/kill-switch", s.clear)
}
