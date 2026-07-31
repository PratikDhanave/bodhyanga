// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Prometheus metrics for the Bodh care HTTP API.
//
// This file is compiled in every build (no build tag) because Prometheus
// scraping is an operational baseline — there is no dep-size penalty
// significant enough to gate it, and github.com/prometheus/client_golang is
// already an indirect dependency pulled by testcontainers-go.
//
// # Metric families
//
//	bodh_http_requests_total{method, path, status}     counter
//	bodh_http_request_duration_seconds{method, path}   histogram
//	bodh_llm_calls_total{provider, model, agent_id}    counter
//	bodh_llm_tokens_total{provider, model, direction}  counter
//	bodh_llm_duration_seconds{provider, model}         histogram
//
// # Path normalisation
//
// The HTTP middleware passes the raw URL path through normalizePath before
// attaching it as a label. normalizePath replaces UUID, long-hex, and
// pure-numeric path segments with "{id}" to bound cardinality and to prevent
// patient or case identifiers from appearing in metric label values.
//
// # PHI policy
//
// Label values NEVER carry patient IDs, case IDs, MRNs, or any other PHI.
// Numeric segment replacement in normalizePath is the primary guard for the
// HTTP path label. LLM metrics label by provider/model/agent_id only —
// the case_id field from LLMTrace is intentionally omitted (AGENTS.md §2).
package observability

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// -- Metric registrations ----------------------------------------------------

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "bodh",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests processed, labelled by method, normalised path, and status code.",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "bodh",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request wall-clock latency in seconds.",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path"},
	)

	llmCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "bodh",
			Subsystem: "llm",
			Name:      "calls_total",
			Help:      "Total LLM (completion + embedding) calls, labelled by provider, model, and agent_id.",
		},
		[]string{"provider", "model", "agent_id", "kind"},
	)

	llmTokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "bodh",
			Subsystem: "llm",
			Name:      "tokens_total",
			Help:      "Total tokens consumed, labelled by provider, model, and direction (input|output|cached).",
		},
		[]string{"provider", "model", "direction"},
	)

	llmDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "bodh",
			Subsystem: "llm",
			Name:      "duration_seconds",
			Help:      "LLM call latency in seconds.",
			Buckets:   []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30, 60},
		},
		[]string{"provider", "model", "kind"},
	)
)

// -- HTTP middleware ----------------------------------------------------------

// statusRecorder wraps http.ResponseWriter to capture the written status code.
// WriteHeader is called at most once; subsequent calls are no-ops matching
// the standard library's contract.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying ResponseWriter when it is an http.Flusher.
// Server-Sent Events (e.g. the case-run trace stream at /api/runs/{id}/events)
// require Flush after each event; without this pass-through the wrapped writer
// silently swallows Flush and events never reach the client.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying ResponseWriter when it is an http.Hijacker.
// The HITL WebSocket endpoint (/ws/hitl) hijacks the connection to complete the
// upgrade; without this the wrapped writer fails the type assertion and the
// handshake returns 500.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("observability: underlying ResponseWriter is not an http.Hijacker")
	}
	return hj.Hijack()
}

func (r *statusRecorder) code() int {
	if r.status == 0 {
		return http.StatusOK // implicit 200 when WriteHeader was never called.
	}
	return r.status
}

// PromMiddleware wraps next with Prometheus HTTP instrumentation. It records
// bodh_http_requests_total and bodh_http_request_duration_seconds for every
// request. The /metrics endpoint itself is excluded to avoid self-referential
// cardinality noise.
func PromMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip self-scrape of /metrics.
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		path := normalizePath(r.URL.Path)
		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()

		next.ServeHTTP(rec, r)

		dur := time.Since(start).Seconds()
		status := strconv.Itoa(rec.code())
		httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(dur)
	})
}

// PrometheusHandler returns the standard promhttp.Handler() for mounting at
// GET /metrics. It exposes all registered collectors (default Go runtime
// metrics + the bodh_* families above).
func PrometheusHandler() http.Handler {
	return promhttp.Handler()
}

// -- LLM metrics hook --------------------------------------------------------

// RecordLLMMetrics updates the LLM Prometheus counters from an LLMTrace. It
// is called unconditionally from RecordLLMCall so every LLM invocation is
// reflected in the /metrics scrape surface, regardless of whether OTLP export
// is enabled.
//
// PHI note: case_id is intentionally omitted from all label sets.
func RecordLLMMetrics(t LLMTrace) {
	kind := t.Kind
	if kind == "" {
		kind = "completion"
	}
	agentID := t.AgentID
	if agentID == "" {
		agentID = "unknown"
	}

	llmCallsTotal.WithLabelValues(t.Provider, t.Model, agentID, kind).Inc()
	llmDurationSeconds.WithLabelValues(t.Provider, t.Model, kind).Observe(
		float64(t.DurationMs) / 1000.0,
	)
	if t.InputTokens > 0 {
		llmTokensTotal.WithLabelValues(t.Provider, t.Model, "input").Add(float64(t.InputTokens))
	}
	if t.OutputTokens > 0 {
		llmTokensTotal.WithLabelValues(t.Provider, t.Model, "output").Add(float64(t.OutputTokens))
	}
	if t.CachedTokens > 0 {
		llmTokensTotal.WithLabelValues(t.Provider, t.Model, "cached").Add(float64(t.CachedTokens))
	}
}

// -- Path normalisation ------------------------------------------------------

// uuidRE matches a lowercase RFC 4122 UUID.
var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// hexRE matches a long lowercase hex string (>= 16 chars) — e.g. audit event IDs.
var hexRE = regexp.MustCompile(`^[0-9a-f]{16,}$`)

// numRE matches a purely-numeric segment — e.g. page numbers, row IDs.
var numRE = regexp.MustCompile(`^\d+$`)

// normalizePath replaces high-cardinality path segments (UUIDs, hex IDs,
// numeric IDs) with "{id}" to give Prometheus bounded label cardinality.
// It never returns an empty string; the minimum return value is "/".
//
// Examples:
//
//	/reviews                                        → /reviews
//	/reviews/550e8400-e29b-41d4-a716-446655440000   → /reviews/{id}
//	/reviews/550e8400.../decide                     → /reviews/{id}/decide
//	/explain/case-abc                               → /explain/case-abc  (not replaced: mixed)
func normalizePath(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range segs {
		ls := strings.ToLower(s)
		if uuidRE.MatchString(ls) || hexRE.MatchString(ls) || numRE.MatchString(s) {
			segs[i] = "{id}"
		}
	}
	return "/" + strings.Join(segs, "/")
}
