// Copyright 2024-2026 Pratik Dhanave and Bodh contributors.
// Portions adapted from github.com/ardanlabs/service/business/sdk/dbtest,
// Copyright (c) 2018-2026 Ardan Labs, licensed under the Apache License 2.0.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package dbtest

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// seedFS holds the JSON fixtures under testdata/. The synthetic-only
// invariant for these files is enforced by review and by the test
// TestFixturesAreSynthetic in dbtest_test.go: no realistic names, no MRN-
// shaped IDs (10+ consecutive digits), no SSN-shaped strings, no DOBs.
//
//go:embed testdata/*.json
var seedFS embed.FS

// SeedPatients is the default synthetic patient roster. Three records covering
// the population-health rule paths Bodh exercises in panel-management tests:
//
//   - patient_test_001: diabetic + hypertensive, A1c above target  -> CDM
//   - patient_test_002: discharged within 30 days                  -> TCM
//   - patient_test_003: RPM-enrolled with stale observations       -> RPM
//
// IDs follow the "patient_test_NNN" pattern. They are NEVER real patient
// IDs from any production system. Demographics are bucketed (age, sex) at
// the policy-relevant resolution only; no names, no DOB, no MRN.
func SeedPatients() []medical.PatientRecord {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	return []medical.PatientRecord{
		{
			PatientID:      "patient_test_001",
			Age:            58,
			Sex:            "F",
			Conditions:     []string{"E11.9", "I10"}, // T2DM, essential HTN
			LastA1c:        8.6,
			LastA1cDate:    now.AddDate(0, -2, 0),
			LastSystolicBP: 142,
			LastBPDate:     now.AddDate(0, -1, 0),
			LastVisit:      now.AddDate(0, -1, -15),
		},
		{
			PatientID:    "patient_test_002",
			Age:          67,
			Sex:          "M",
			Conditions:   []string{"I50.9"}, // heart failure
			LastVisit:    now.AddDate(0, 0, -25),
			DischargedAt: now.AddDate(0, 0, -10),
		},
		{
			PatientID:   "patient_test_003",
			Age:         72,
			Sex:         "F",
			Conditions:  []string{"J44.9"}, // COPD
			RPMEnrolled: true,
			LastSpO2:    93,
			LastVisit:   now.AddDate(0, -3, 0),
		},
	}
}

// SeedCaseStates returns synthetic case_state JSON payloads that exercise the
// diagnostician's specialty rules. Case IDs follow "case_test_NNN".
//
// These are returned as encoded medical.CaseState rather than written to a
// table directly because the persistence layer does not yet have a
// case_state schema — they are useful for tests that want to drive the
// diagnostician agent end-to-end against a tenant-isolated audit log.
func SeedCaseStates() []medical.CaseState {
	return []medical.CaseState{
		{
			CaseID:       "case_test_001",
			Presentation: "synthetic presentation A; not derived from any real patient",
			MaxCostUSD:   500,
			Condition:    "dm",
		},
		{
			CaseID:       "case_test_002",
			Presentation: "synthetic presentation B; not derived from any real patient",
			MaxCostUSD:   750,
			Condition:    "chf",
		},
	}
}

// SeedAuditEvents returns synthetic audit.Event records that exercise the
// audit pipeline. IDs are "event_test_NNN"; PatientID/CaseID match the
// synthetic seed series above.
func SeedAuditEvents() []audit.Event {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	return []audit.Event{
		{
			ID:        "event_test_001",
			At:        now,
			Kind:      audit.KindMessage,
			AgentID:   "diagnostician",
			MessageID: "msg_test_001",
			PatientID: "patient_test_001",
			CaseID:    "case_test_001",
			Type:      "case_state",
		},
		{
			ID:        "event_test_002",
			At:        now.Add(1 * time.Second),
			Kind:      audit.KindPolicyDecision,
			AgentID:   "orchestrator",
			MessageID: "msg_test_002",
			CaseID:    "case_test_001",
			Decision:  "allow",
			Reason:    "synthetic policy allow",
		},
	}
}

// SeedInteractions returns synthetic eval.InteractionRecord fixtures with
// "interaction_test_NNN" IDs. Useful for tests that exercise the eval store
// surface.
func SeedInteractions() []eval.InteractionRecord {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	return []eval.InteractionRecord{
		{
			ID:        "interaction_test_001",
			Scenario:  "synthetic_diag",
			Success:   true,
			Metrics:   map[string]float64{"accuracy": 0.92, "cost_usd": 12.5},
			Metadata:  map[string]any{"note": "synthetic fixture"},
			StartedAt: now,
			EndedAt:   now.Add(2 * time.Minute),
		},
		{
			ID:        "interaction_test_002",
			Scenario:  "synthetic_diag",
			Success:   false,
			Metrics:   map[string]float64{"accuracy": 0.41, "cost_usd": 18.0},
			Metadata:  map[string]any{"note": "synthetic fixture failure"},
			StartedAt: now.Add(3 * time.Minute),
			EndedAt:   now.Add(7 * time.Minute),
		},
	}
}

// Seed loads the default synthetic seed into the tenant scope of db. Tests
// that want a populated DB call this immediately after NewDB.
//
// Seed is idempotent at the patient level (UpsertPatient is upsert-by-PK)
// but not at the audit level (audit_events is append-only). Calling Seed
// twice on the same DB will duplicate audit rows; if a test needs that
// behaviour it should call SeedPatientsOnly / SeedAuditEventsOnly directly.
func Seed(t *testing.T, db *DB) {
	t.Helper()
	SeedPatientsOnly(t, db)
	SeedAuditEventsOnly(t, db)
	SeedInteractionsOnly(t, db)
}

// SeedPatientsOnly upserts the patient roster. Returns the seeded slice so
// callers can assert against the same payloads they wrote.
func SeedPatientsOnly(t *testing.T, db *DB) []medical.PatientRecord {
	t.Helper()
	patients := SeedPatients()
	ctx := context.Background()
	for _, p := range patients {
		if err := db.Store.UpsertPatient(ctx, db.TenantID, p); err != nil {
			t.Fatalf("dbtest seed: upsert patient %s: %v", p.PatientID, err)
		}
	}
	return patients
}

// SeedAuditEventsOnly records the synthetic audit fixture rows.
func SeedAuditEventsOnly(t *testing.T, db *DB) []audit.Event {
	t.Helper()
	events := SeedAuditEvents()
	ctx := context.Background()
	for _, e := range events {
		if err := db.Store.RecordAuditEvent(ctx, db.TenantID, e); err != nil {
			t.Fatalf("dbtest seed: record audit %s: %v", e.ID, err)
		}
	}
	return events
}

// SeedInteractionsOnly saves the synthetic eval interactions.
func SeedInteractionsOnly(t *testing.T, db *DB) []eval.InteractionRecord {
	t.Helper()
	recs := SeedInteractions()
	ctx := context.Background()
	for _, r := range recs {
		if err := db.Store.SaveInteraction(ctx, db.TenantID, r); err != nil {
			t.Fatalf("dbtest seed: save interaction %s: %v", r.ID, err)
		}
	}
	return recs
}

// LoadFixture decodes a named JSON file under testdata/ into v. Useful for
// tests that want a richer fixture than the defaults above — drop a new
// file under testdata/, append to the synthetic-only allowlist in
// dbtest_test.go, and call LoadFixture("my_fixture.json", &target).
func LoadFixture(name string, v any) error {
	data, err := seedFS.ReadFile("testdata/" + name)
	if err != nil {
		return fmt.Errorf("dbtest: read fixture %s: %w", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("dbtest: decode fixture %s: %w", name, err)
	}
	return nil
}

// FixtureNames lists every file in testdata/. Used by the synthetic-only
// invariant test to enumerate fixtures that must be scanned.
func FixtureNames() ([]string, error) {
	entries, err := seedFS.ReadDir("testdata")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
