// Copyright 2025 The Bodh Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Health probe handlers for the cmd/care HTTP API.
//
// Two endpoints are registered:
//
//	GET /healthz  liveness  — always 200 while the process is running.
//	GET /readyz   readiness — 200 when all dependencies are reachable;
//	                          503 when Postgres is configured but unreachable.
//
// The K8s deployment manifest (zarf/k8s/base/deployment.yaml) configures
// kubelet to hit these paths. The docker-compose healthcheck (zarf/compose/
// docker_compose.yaml) uses /healthz.
//
// Design notes:
//   - /healthz performs no I/O — it exists to let the kubelet distinguish a
//     hung process from a live one. A stuck GC or deadlocked goroutine
//     eventually causes the handler to time out, tripping the liveness probe.
//   - /readyz performs a cheap Postgres ping when pg != nil. A transient DB
//     outage (pgBouncer restart, Cloud SQL failover) returns 503, which takes
//     the Pod out of the Service endpoint slice while it reconnects — avoiding
//     a cascade of 500s to real clinician traffic.
//   - Neither endpoint emits PHI (AGENTS.md invariant #2). The response body
//     is a minimal JSON object with a human-readable status string and an
//     RFC3339 timestamp for debuggability.
//   - Both probes are deliberately read-only and unauthenticated — they are
//     called by the infra control plane, not by clinicians, and the
//     NetworkPolicy (zarf/k8s/base/networkpolicy.yaml) already limits ingress
//     to the API gateway namespace.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	pgstore "github.com/PratikDhanave/bodh/pkg/persistence/postgres"
)

// pinger is the subset of pgstore.Store used by readyzHandler. Using a
// narrow interface keeps the handler unit-testable without a real Postgres.
type pinger interface {
	Ping(ctx context.Context) error
}

// livenessHandler returns GET /healthz. Always 200 while the process is
// running. No external I/O, no dependencies.
func livenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// readinessHandler returns GET /readyz. Returns 200 when all wired
// dependencies are healthy; 503 when Postgres is configured but unreachable.
// pg may be nil (in-memory path) — in that case the only check is that the
// process is alive, which matches the liveness behaviour.
func readinessHandler(pg *pgstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if pg != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			if err := pg.Ping(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status": "unavailable",
					"reason": "postgres ping failed",
					"time":   time.Now().UTC().Format(time.RFC3339),
				})
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
	}
}
