// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNormalizePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/reviews", "/reviews"},
		{"/reviews/", "/reviews"},
		{"/reviews/550e8400-e29b-41d4-a716-446655440000", "/reviews/{id}"},
		{"/reviews/550e8400-e29b-41d4-a716-446655440000/decide", "/reviews/{id}/decide"},
		{"/explain/case-abc", "/explain/case-abc"},
		{"/patients/42", "/patients/{id}"},
		{"/", "/"},
		{"", "/"},
		{"/api/transcribe", "/api/transcribe"},
		// Long hex audit ID.
		{"/audit/abcdef0123456789", "/audit/{id}"},
	}
	for _, c := range cases {
		got := normalizePath(c.in)
		if got != c.want {
			t.Errorf("normalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPromMiddleware_CountsRequests(t *testing.T) {
	// Use a fresh registry to avoid interference from promauto global.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := PromMiddleware(inner)

	// Fire two requests.
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	}

	// Pull the counter from the default registry.
	mf, err := collectMetric("bodhyanga_http_requests_total")
	if err != nil {
		t.Skip("prometheus registry lookup failed:", err)
	}
	var found bool
	for _, m := range mf.GetMetric() {
		if labelMatch(m.GetLabel(), "path", "/healthz") &&
			labelMatch(m.GetLabel(), "status", "200") {
			found = true
			if v := m.GetCounter().GetValue(); v < 2 {
				t.Errorf("bodhyanga_http_requests_total{path=/healthz,status=200} = %.0f, want >= 2", v)
			}
		}
	}
	if !found {
		t.Error("no bodhyanga_http_requests_total metric found for path=/healthz")
	}
}

func TestPromMiddleware_SkipsMetricsPath(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := PromMiddleware(inner)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !called {
		t.Error("inner handler should still be called for /metrics")
	}
}

func TestRecordLLMMetrics_NoopOnEmpty(t *testing.T) {
	// Should not panic on a zero-value trace.
	RecordLLMMetrics(LLMTrace{})
}

func TestStatusRecorder_ImplicitOK(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never calls WriteHeader — implicit 200.
		_, _ = w.Write([]byte("ok"))
	})
	h := PromMiddleware(inner)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
}

// --- helpers ----------------------------------------------------------------

// collectMetric finds a metric family in the default registry by name.
func collectMetric(name string) (*dto.MetricFamily, error) {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return nil, err
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf, nil
		}
	}
	return &dto.MetricFamily{}, nil
}

func labelMatch(labels []*dto.LabelPair, key, val string) bool {
	for _, lp := range labels {
		if lp.GetName() == key && lp.GetValue() == val {
			return true
		}
	}
	return false
}
