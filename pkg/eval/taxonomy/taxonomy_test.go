package taxonomy

import "testing"

func TestNewTaxonomy(t *testing.T) {
	tax := NewTaxonomy()
	if tax == nil {
		t.Fatal("NewTaxonomy returned nil")
	}
	if len(tax.Modes) != 0 {
		t.Fatalf("expected 0 modes, got %d", len(tax.Modes))
	}
	if tax.ByDomain == nil || tax.ByGulf == nil || tax.BySeverity == nil {
		t.Fatal("index maps must be initialized")
	}
}

func TestRegisterAndLookup(t *testing.T) {
	tax := NewTaxonomy()
	fm := FailureMode{
		ID:       "FM-TEST-001",
		Name:     "Test failure",
		Domain:   "diagnostic",
		Gulf:     GulfGeneralization,
		Severity: SeverityHigh,
		Category: "accuracy",
	}
	tax.Register(fm)

	if len(tax.Modes) != 1 {
		t.Fatalf("expected 1 mode, got %d", len(tax.Modes))
	}

	// FindByDomain
	got := tax.FindByDomain("diagnostic")
	if len(got) != 1 || got[0].ID != "FM-TEST-001" {
		t.Errorf("FindByDomain(diagnostic) = %v, want 1 result", got)
	}
	if len(tax.FindByDomain("care")) != 0 {
		t.Error("FindByDomain(care) should be empty")
	}

	// FindByGulf
	got = tax.FindByGulf(GulfGeneralization)
	if len(got) != 1 {
		t.Errorf("FindByGulf(generalization) = %d, want 1", len(got))
	}

	// FindBySeverity
	got = tax.FindBySeverity(SeverityHigh)
	if len(got) != 1 {
		t.Errorf("FindBySeverity(high) = %d, want 1", len(got))
	}
}

func TestFindByID(t *testing.T) {
	tax := NewTaxonomy()
	tax.Register(FailureMode{ID: "FM-A", Name: "Alpha", Domain: "d", Gulf: GulfComprehension, Severity: SeverityLow})
	tax.Register(FailureMode{ID: "FM-B", Name: "Beta", Domain: "d", Gulf: GulfSpecification, Severity: SeverityMedium})

	fm, ok := tax.FindByID("FM-B")
	if !ok || fm.Name != "Beta" {
		t.Errorf("FindByID(FM-B) = (%v, %v), want Beta", fm, ok)
	}

	_, ok = tax.FindByID("FM-MISSING")
	if ok {
		t.Error("FindByID(FM-MISSING) should return false")
	}
}

func TestTaxonomyStats(t *testing.T) {
	tax := NewTaxonomy()
	tax.Register(FailureMode{ID: "1", Domain: "diagnostic", Gulf: GulfGeneralization, Severity: SeverityCritical, Category: "safety"})
	tax.Register(FailureMode{ID: "2", Domain: "diagnostic", Gulf: GulfSpecification, Severity: SeverityHigh, Category: "accuracy"})
	tax.Register(FailureMode{ID: "3", Domain: "care", Gulf: GulfGeneralization, Severity: SeverityCritical, Category: "safety"})

	s := tax.Stats()
	if s.Total != 3 {
		t.Errorf("Total = %d, want 3", s.Total)
	}
	if s.ByGulf[GulfGeneralization] != 2 {
		t.Errorf("ByGulf[generalization] = %d, want 2", s.ByGulf[GulfGeneralization])
	}
	if s.ByGulf[GulfSpecification] != 1 {
		t.Errorf("ByGulf[specification] = %d, want 1", s.ByGulf[GulfSpecification])
	}
	if s.BySeverity[SeverityCritical] != 2 {
		t.Errorf("BySeverity[critical] = %d, want 2", s.BySeverity[SeverityCritical])
	}
	if s.ByDomain["diagnostic"] != 2 {
		t.Errorf("ByDomain[diagnostic] = %d, want 2", s.ByDomain["diagnostic"])
	}
	if s.ByCategory["safety"] != 2 {
		t.Errorf("ByCategory[safety] = %d, want 2", s.ByCategory["safety"])
	}
}

func TestString(t *testing.T) {
	tax := NewTaxonomy()
	tax.Register(FailureMode{ID: "1", Gulf: GulfComprehension, Severity: SeverityLow, Domain: "d"})
	got := tax.String()
	if got == "" {
		t.Error("String() should not be empty")
	}
}

func TestDefaultMedicalTaxonomy(t *testing.T) {
	tax := DefaultMedicalTaxonomy()
	if tax == nil {
		t.Fatal("DefaultMedicalTaxonomy returned nil")
	}

	// Verify minimum number of registered failure modes (spec requires 15+).
	if len(tax.Modes) < 15 {
		t.Errorf("expected at least 15 modes, got %d", len(tax.Modes))
	}

	// Verify all three gulfs are represented.
	s := tax.Stats()
	for _, g := range []Gulf{GulfComprehension, GulfSpecification, GulfGeneralization} {
		if s.ByGulf[g] == 0 {
			t.Errorf("gulf %q has 0 modes; expected at least one", g)
		}
	}

	// Verify all four severity levels are represented.
	for _, sev := range []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow} {
		if s.BySeverity[sev] == 0 {
			t.Errorf("severity %q has 0 modes; expected at least one", sev)
		}
	}

	// Spot-check a few well-known IDs.
	for _, id := range []string{"FM-DX-001", "FM-DX-002", "FM-CP-002", "FM-GV-001", "FM-AG-001", "FM-EV-001"} {
		fm, ok := tax.FindByID(id)
		if !ok {
			t.Errorf("expected to find %s in default taxonomy", id)
			continue
		}
		if fm.Name == "" {
			t.Errorf("%s has empty Name", id)
		}
		if fm.Description == "" {
			t.Errorf("%s has empty Description", id)
		}
	}

	// Verify HIPAA references are present on governance/compliance modes.
	gvModes := tax.FindByDomain("governance")
	hipaaCount := 0
	for _, fm := range gvModes {
		if fm.HIPAARef != "" {
			hipaaCount++
		}
	}
	if hipaaCount == 0 {
		t.Error("governance domain has no HIPAA references")
	}

	// Verify all domains expected by Bodhyanga are covered.
	expectedDomains := []string{"diagnostic", "care", "governance", "interop", "agent", "eval"}
	for _, d := range expectedDomains {
		if len(tax.FindByDomain(d)) == 0 {
			t.Errorf("expected at least one mode in domain %q", d)
		}
	}
}

func TestGulfConstants(t *testing.T) {
	// Ensure the string values are stable (they appear in JSON).
	if GulfComprehension != "comprehension" {
		t.Errorf("GulfComprehension = %q", GulfComprehension)
	}
	if GulfSpecification != "specification" {
		t.Errorf("GulfSpecification = %q", GulfSpecification)
	}
	if GulfGeneralization != "generalization" {
		t.Errorf("GulfGeneralization = %q", GulfGeneralization)
	}
}

func TestSeverityConstants(t *testing.T) {
	if SeverityCritical != "critical" {
		t.Errorf("SeverityCritical = %q", SeverityCritical)
	}
	if SeverityHigh != "high" {
		t.Errorf("SeverityHigh = %q", SeverityHigh)
	}
	if SeverityMedium != "medium" {
		t.Errorf("SeverityMedium = %q", SeverityMedium)
	}
	if SeverityLow != "low" {
		t.Errorf("SeverityLow = %q", SeverityLow)
	}
}

func TestUniqueIDs(t *testing.T) {
	tax := DefaultMedicalTaxonomy()
	seen := make(map[string]bool)
	for _, fm := range tax.Modes {
		if seen[fm.ID] {
			t.Errorf("duplicate failure mode ID: %s", fm.ID)
		}
		seen[fm.ID] = true
	}
}
