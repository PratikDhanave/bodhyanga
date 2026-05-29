// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/microsoft/agent-governance-toolkit/agent-governance-golang/packages/agentmesh"
)

// newKSServer is a test helper that returns a killSwitchServer backed by a
// fresh, empty registry.
func newKSServer() *killSwitchServer {
	return &killSwitchServer{reg: agentmesh.NewKillSwitchRegistry()}
}

func postJSON(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func deleteJSON(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getRequest(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestKillSwitchServer_ActivateGlobal verifies POST /admin/kill-switch with
// scope "global" returns 200 and the scope string in the response.
func TestKillSwitchServer_ActivateGlobal(t *testing.T) {
	s := newKSServer()
	rec := postJSON(t, http.HandlerFunc(s.activate), "/admin/kill-switch", map[string]string{
		"scope":   "global",
		"reason":  "security_incident",
		"message": "test halt",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["scope"] != "global" {
		t.Errorf("want scope=global, got %v", resp["scope"])
	}
}

// TestKillSwitchServer_ActivateAgent verifies POST with scope "agent:diagnostician".
func TestKillSwitchServer_ActivateAgent(t *testing.T) {
	s := newKSServer()
	rec := postJSON(t, http.HandlerFunc(s.activate), "/admin/kill-switch", map[string]string{
		"scope":   "agent:diagnostician",
		"reason":  "policy_violation",
		"message": "quarantine",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["scope"] != "agent:diagnostician" {
		t.Errorf("want scope=agent:diagnostician, got %v", resp["scope"])
	}
}

// TestKillSwitchServer_InvalidScope verifies a bad scope returns 400.
func TestKillSwitchServer_InvalidScope(t *testing.T) {
	s := newKSServer()
	rec := postJSON(t, http.HandlerFunc(s.activate), "/admin/kill-switch", map[string]string{
		"scope":  "nonsense",
		"reason": "operator_request",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// TestKillSwitchServer_EmptyAgentID verifies "agent:" (no ID) returns 400.
func TestKillSwitchServer_EmptyAgentID(t *testing.T) {
	s := newKSServer()
	rec := postJSON(t, http.HandlerFunc(s.activate), "/admin/kill-switch", map[string]string{
		"scope":  "agent:",
		"reason": "operator_request",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// TestKillSwitchServer_ClearGlobal verifies DELETE un-halts the global scope
// and the list reflects the cleared state.
func TestKillSwitchServer_ClearGlobal(t *testing.T) {
	s := newKSServer()

	// Activate.
	rec := postJSON(t, http.HandlerFunc(s.activate), "/admin/kill-switch", map[string]string{
		"scope":  "global",
		"reason": "security_incident",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("activate want 200, got %d", rec.Code)
	}

	// Clear.
	rec = deleteJSON(t, http.HandlerFunc(s.clear), "/admin/kill-switch", map[string]string{
		"scope":   "global",
		"reason":  "operator_request",
		"message": "incident resolved",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("clear want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// List should show no active scopes.
	rec = getRequest(http.HandlerFunc(s.list), "/admin/kill-switch")
	if rec.Code != http.StatusOK {
		t.Fatalf("list want 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	actives, _ := resp["active"].([]any)
	if len(actives) != 0 {
		t.Errorf("want no active scopes after clear, got %d", len(actives))
	}
}

// TestKillSwitchServer_ListActive verifies the list endpoint returns active scopes.
func TestKillSwitchServer_ListActive(t *testing.T) {
	s := newKSServer()

	// Activate two scopes.
	for _, scope := range []string{"global", "agent:questioner"} {
		rec := postJSON(t, http.HandlerFunc(s.activate), "/admin/kill-switch", map[string]string{
			"scope":  scope,
			"reason": "operator_request",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("activate %s: want 200, got %d", scope, rec.Code)
		}
	}

	rec := getRequest(http.HandlerFunc(s.list), "/admin/kill-switch")
	if rec.Code != http.StatusOK {
		t.Fatalf("list want 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	actives, _ := resp["active"].([]any)
	if len(actives) != 2 {
		t.Errorf("want 2 active scopes, got %d: %v", len(actives), resp)
	}
}

// TestKillSwitchServer_mount verifies all three routes are registered.
func TestKillSwitchServer_mount(t *testing.T) {
	s := newKSServer()
	mux := http.NewServeMux()
	s.mount(mux)

	for _, tc := range []struct {
		method string
		path   string
		body   any
		want   int
	}{
		{http.MethodGet, "/admin/kill-switch", nil, http.StatusOK},
		{http.MethodPost, "/admin/kill-switch", map[string]string{"scope": "global", "reason": "operator_request"}, http.StatusOK},
		{http.MethodDelete, "/admin/kill-switch", map[string]string{"scope": "global", "reason": "operator_request"}, http.StatusOK},
	} {
		var rec *httptest.ResponseRecorder
		switch tc.method {
		case http.MethodGet:
			rec = getRequest(mux, tc.path)
		case http.MethodPost:
			rec = postJSON(t, mux, tc.path, tc.body)
		case http.MethodDelete:
			rec = deleteJSON(t, mux, tc.path, tc.body)
		}
		if rec.Code != tc.want {
			t.Errorf("%s %s: want %d, got %d — %s", tc.method, tc.path, tc.want, rec.Code, rec.Body.String())
		}
	}
}
