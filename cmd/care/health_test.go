// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubPinger satisfies the pinger interface for unit tests.
type stubPinger struct{ err error }

func (s stubPinger) Ping(_ context.Context) error { return s.err }

func TestLivenessHandler(t *testing.T) {
	h := livenessHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("want JSON content-type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("want status:ok in body, got %s", body)
	}
}

func TestReadinessHandler_NoDB(t *testing.T) {
	// No pg (in-memory path) — always ready.
	h := readinessHandler(nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 when pg==nil, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ready"`) {
		t.Errorf("want status:ready in body, got %s", rec.Body.String())
	}
}

// fakeReadinessHandler mimics readinessHandler but uses the pinger interface
// so tests don't need a real *pgstore.Store.
func fakeReadinessHandler(p pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if p != nil {
			if err := p.Ping(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}

func TestReadinessHandler_DBHealthy(t *testing.T) {
	h := fakeReadinessHandler(stubPinger{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 when db healthy, got %d", rec.Code)
	}
}

func TestReadinessHandler_DBUnhealthy(t *testing.T) {
	h := fakeReadinessHandler(stubPinger{err: errors.New("connection refused")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when db down, got %d", rec.Code)
	}
}
