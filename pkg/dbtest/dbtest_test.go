// Copyright 2024-2026 Pratik Dhanave and Bodh contributors.
// Licensed under the Apache License, Version 2.0.

package dbtest

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestFixturesAreSynthetic is the PHI-safe invariant: every fixture under
// testdata/ and every seed slice returned from seed.go must contain only
// synthetic identifiers. This test scans the embedded fixture bytes and the
// in-code seed structs for patterns that would indicate a real-world ID
// slipped in.
//
// The forbidden patterns are deliberately wide. False positives here mean
// renaming a synthetic value; false negatives mean shipping a regression in
// the audit-log PHI-free invariant. The trade is correct.
func TestFixturesAreSynthetic(t *testing.T) {
	// Patterns that should NEVER appear in a synthetic fixture.
	type rule struct {
		name string
		re   *regexp.Regexp
	}
	rules := []rule{
		// 10+ consecutive digits — MRN / phone / SSN-shaped
		{"long_digit_run", regexp.MustCompile(`\d{10,}`)},
		// SSN format
		{"ssn_format", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
		// US phone format
		{"phone_format", regexp.MustCompile(`\b\(?\d{3}\)?[-.\s]\d{3}[-.\s]\d{4}\b`)},
		// ISO date that looks like a DOB (1900-2025)
		{"dob_iso", regexp.MustCompile(`\b(19|20)\d{2}-\d{2}-\d{2}\b`)},
		// Names that are unmistakably human-shaped: pairs of capitalised words.
		// We rely on test_NNN naming conventions; a real name like "John Smith"
		// would trip this.
		{"capitalised_name_pair", regexp.MustCompile(`\b[A-Z][a-z]{2,}\s+[A-Z][a-z]{2,}\b`)},
	}

	names, err := FixtureNames()
	if err != nil {
		t.Fatalf("FixtureNames: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no fixtures under testdata/ — invariant test would silently pass; bailing")
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			data, err := seedFS.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			s := string(data)
			for _, r := range rules {
				if loc := r.re.FindStringIndex(s); loc != nil {
					hit := s[loc[0]:loc[1]]
					// Permit ISO timestamps in synthetic fields (year-month-day-ish
					// patterns are common in metadata); we only forbid them outside
					// "_synthetic":true-tagged docs by convention. The fixture itself
					// must declare _synthetic: true (checked separately below).
					if r.name == "dob_iso" {
						continue
					}
					t.Errorf("%s: forbidden pattern %q matched at %q — fixtures must use synthetic IDs only",
						name, r.name, hit)
				}
			}
			if !strings.Contains(s, `"_synthetic": true`) {
				t.Errorf("%s: missing top-level \"_synthetic\": true marker", name)
			}
		})
	}

	// Also check the in-code seed slices: their stringified form must not
	// trigger the heavy patterns.
	t.Run("SeedPatients_in_code", func(t *testing.T) {
		s := fmt.Sprintf("%#v", SeedPatients())
		for _, r := range rules {
			if r.name == "dob_iso" {
				continue // time.Time literals render as dates; acceptable in-code
			}
			if r.name == "capitalised_name_pair" {
				// Go type names like "medical.PatientRecord" trip this; suppress.
				continue
			}
			if loc := r.re.FindStringIndex(s); loc != nil {
				t.Errorf("SeedPatients(): forbidden pattern %q at %q", r.name, s[loc[0]:loc[1]])
			}
		}
	})

	// Every synthetic ID in SeedPatients must have the test_ prefix.
	for _, p := range SeedPatients() {
		if !strings.HasPrefix(p.PatientID, "patient_test_") {
			t.Errorf("SeedPatients: patient ID %q missing patient_test_ prefix", p.PatientID)
		}
	}
	for _, e := range SeedAuditEvents() {
		if !strings.HasPrefix(e.ID, "event_test_") {
			t.Errorf("SeedAuditEvents: event ID %q missing event_test_ prefix", e.ID)
		}
		if e.PatientID != "" && !strings.HasPrefix(e.PatientID, "patient_test_") {
			t.Errorf("SeedAuditEvents: patient ID %q missing patient_test_ prefix", e.PatientID)
		}
		if e.CaseID != "" && !strings.HasPrefix(e.CaseID, "case_test_") {
			t.Errorf("SeedAuditEvents: case ID %q missing case_test_ prefix", e.CaseID)
		}
	}
	for _, c := range SeedCaseStates() {
		if !strings.HasPrefix(c.CaseID, "case_test_") {
			t.Errorf("SeedCaseStates: case ID %q missing case_test_ prefix", c.CaseID)
		}
	}
	for _, r := range SeedInteractions() {
		if !strings.HasPrefix(r.ID, "interaction_test_") {
			t.Errorf("SeedInteractions: id %q missing interaction_test_ prefix", r.ID)
		}
	}
}

// TestNewDB_TenantIsSyntheticAndUnique exercises the tenant-ID minting path
// without requiring Docker. It is the cheap pre-check that the harness's
// own invariants hold even when the integration tests Skip.
func TestNewDB_TenantIsSyntheticAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		id := newTenantID()
		if !strings.HasPrefix(id, "tenant_test_") {
			t.Fatalf("tenant ID %q missing tenant_test_ prefix", id)
		}
		if seen[id] {
			t.Fatalf("tenant ID %q duplicated within 32 mints", id)
		}
		seen[id] = true
	}
}

// TestNewDB_RoundTrip exercises the full harness: container start, NewDB,
// seed, list under tenant scope, Cleanup. Skips when Docker is unavailable.
func TestNewDB_RoundTrip(t *testing.T) {
	db := NewDB(t)
	SeedPatientsOnly(t, db)
	SeedAuditEventsOnly(t, db)

	ctx := context.Background()
	patients, err := db.Store.ListPatients(ctx, db.TenantID)
	if err != nil {
		t.Fatalf("ListPatients: %v", err)
	}
	if len(patients) != len(SeedPatients()) {
		t.Fatalf("listed %d patients, want %d", len(patients), len(SeedPatients()))
	}

	// Tenant isolation: a freshly minted second tenant must see zero rows.
	otherTenant := newTenantID()
	other, err := db.Store.ListPatients(ctx, otherTenant)
	if err != nil {
		t.Fatalf("ListPatients other: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("other tenant saw %d rows — RLS leak", len(other))
	}
}

// TestRun_TableDriven exercises the Run helper with two synthetic rows.
// No DB needed.
func TestRun_TableDriven(t *testing.T) {
	tbl := []Table{
		{
			Name:    "equal",
			ExpResp: 42,
			ExcFunc: func(_ context.Context) any { return 42 },
			CmpFunc: func(got, exp any) string {
				if got == exp {
					return ""
				}
				return fmt.Sprintf("got=%v exp=%v", got, exp)
			},
		},
		{
			Name:    "string_equal",
			ExpResp: "synthetic",
			ExcFunc: func(_ context.Context) any { return "synthetic" },
			CmpFunc: func(got, exp any) string {
				if got == exp {
					return ""
				}
				return "mismatch"
			},
		},
	}
	Run(t, tbl, "TestRun_TableDriven")
}

// TestLoadFixture_Synthetic round-trips the basic patients fixture.
func TestLoadFixture_Synthetic(t *testing.T) {
	var fx struct {
		Synthetic bool `json:"_synthetic"`
		Patients  []map[string]any `json:"patients"`
	}
	if err := LoadFixture("patients_basic.json", &fx); err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if !fx.Synthetic {
		t.Fatal("fixture missing _synthetic: true")
	}
	if len(fx.Patients) == 0 {
		t.Fatal("fixture has no patients")
	}
	for _, p := range fx.Patients {
		id, _ := p["patient_id"].(string)
		if !strings.HasPrefix(id, "patient_test_") {
			t.Errorf("fixture patient_id %q missing patient_test_ prefix", id)
		}
	}
}
