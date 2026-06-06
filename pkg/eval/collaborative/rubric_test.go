package collaborative

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRubric(t *testing.T) {
	r := NewRubric()
	if r == nil {
		t.Fatal("NewRubric returned nil")
	}
	if len(r.Criteria) != 0 {
		t.Errorf("expected empty criteria, got %d", len(r.Criteria))
	}
}

func TestRubric_AddCriterion(t *testing.T) {
	r := NewRubric()

	c := RubricCriterion{
		Name:       "test_criterion",
		Definition: "A test criterion",
		PassDef:    "It passes",
		FailDef:    "It fails",
		Version:    1,
		UpdatedAt:  time.Now(),
	}

	r.AddCriterion(c)

	if len(r.Criteria) != 1 {
		t.Fatalf("expected 1 criterion, got %d", len(r.Criteria))
	}
	if r.Criteria[0].Name != "test_criterion" {
		t.Errorf("expected name 'test_criterion', got %q", r.Criteria[0].Name)
	}
}

func TestRubric_AddCriterion_NoDuplicate(t *testing.T) {
	r := NewRubric()

	c1 := RubricCriterion{Name: "criterion_a", Definition: "First version"}
	c2 := RubricCriterion{Name: "criterion_a", Definition: "Second version"}

	r.AddCriterion(c1)
	r.AddCriterion(c2) // Should be no-op.

	if len(r.Criteria) != 1 {
		t.Fatalf("expected 1 criterion (no duplicates), got %d", len(r.Criteria))
	}
	if r.Criteria[0].Definition != "First version" {
		t.Errorf("expected first version, got %q", r.Criteria[0].Definition)
	}
}

func TestRubric_UpdateCriterion(t *testing.T) {
	r := NewRubric()

	c1 := RubricCriterion{Name: "criterion_a", Definition: "First version", Version: 1}
	r.AddCriterion(c1)

	c2 := RubricCriterion{Name: "criterion_a", Definition: "Updated version", Version: 2}
	r.UpdateCriterion("criterion_a", c2)

	if len(r.Criteria) != 1 {
		t.Fatalf("expected 1 criterion after update, got %d", len(r.Criteria))
	}
	if r.Criteria[0].Definition != "Updated version" {
		t.Errorf("expected 'Updated version', got %q", r.Criteria[0].Definition)
	}
	if r.Criteria[0].Version != 2 {
		t.Errorf("expected version 2, got %d", r.Criteria[0].Version)
	}
}

func TestRubric_UpdateCriterion_NotFound(t *testing.T) {
	r := NewRubric()

	c := RubricCriterion{Name: "new_criterion", Definition: "Brand new"}
	r.UpdateCriterion("new_criterion", c)

	if len(r.Criteria) != 1 {
		t.Fatalf("expected 1 criterion (appended), got %d", len(r.Criteria))
	}
	if r.Criteria[0].Name != "new_criterion" {
		t.Errorf("expected 'new_criterion', got %q", r.Criteria[0].Name)
	}
}

func TestRubric_GetCriterion(t *testing.T) {
	r := NewRubric()
	r.AddCriterion(RubricCriterion{Name: "alpha", Definition: "Alpha definition"})
	r.AddCriterion(RubricCriterion{Name: "beta", Definition: "Beta definition"})

	got, ok := r.GetCriterion("alpha")
	if !ok {
		t.Fatal("expected to find 'alpha'")
	}
	if got.Definition != "Alpha definition" {
		t.Errorf("expected 'Alpha definition', got %q", got.Definition)
	}

	_, ok = r.GetCriterion("gamma")
	if ok {
		t.Error("expected 'gamma' not found")
	}
}

func TestRubric_ExportImportJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rubric.json")

	// Create and export a rubric.
	original := NewRubric()
	original.AddCriterion(RubricCriterion{
		Name:       "accuracy",
		Definition: "Is the output accurate?",
		PassDef:    "Output is correct",
		FailDef:    "Output is incorrect",
		Examples: []RubricExample{
			{
				Input:     "test input",
				Output:    "test output",
				Label:     LabelPass,
				Reasoning: "It was correct",
			},
		},
		DecisionRules: []string{"Rule 1", "Rule 2"},
		Version:       3,
		UpdatedAt:     time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
	})

	if err := original.ExportJSON(path); err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	// Verify the file was created.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("exported file not found: %v", err)
	}

	// Import into a new rubric.
	imported := NewRubric()
	if err := imported.ImportJSON(path); err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}

	if len(imported.Criteria) != 1 {
		t.Fatalf("expected 1 criterion, got %d", len(imported.Criteria))
	}

	c := imported.Criteria[0]
	if c.Name != "accuracy" {
		t.Errorf("expected name 'accuracy', got %q", c.Name)
	}
	if c.Definition != "Is the output accurate?" {
		t.Errorf("unexpected definition: %q", c.Definition)
	}
	if c.Version != 3 {
		t.Errorf("expected version 3, got %d", c.Version)
	}
	if len(c.Examples) != 1 {
		t.Errorf("expected 1 example, got %d", len(c.Examples))
	}
	if len(c.DecisionRules) != 2 {
		t.Errorf("expected 2 decision rules, got %d", len(c.DecisionRules))
	}
}

func TestRubric_ExportJSON_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "rubric.json")

	r := NewRubric()
	r.AddCriterion(RubricCriterion{Name: "test"})

	if err := r.ExportJSON(path); err != nil {
		t.Fatalf("ExportJSON with nested dir: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created at nested path: %v", err)
	}
}

func TestRubric_ImportJSON_FileNotFound(t *testing.T) {
	r := NewRubric()
	err := r.ImportJSON("/nonexistent/rubric.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRubric_ImportJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRubric()
	err := r.ImportJSON(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestRubric_ImportJSON_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rubric.json")

	// Export a rubric with one criterion.
	original := NewRubric()
	original.AddCriterion(RubricCriterion{Name: "from_file"})
	if err := original.ExportJSON(path); err != nil {
		t.Fatal(err)
	}

	// Import into a rubric that already has criteria.
	target := NewRubric()
	target.AddCriterion(RubricCriterion{Name: "preexisting"})

	if err := target.ImportJSON(path); err != nil {
		t.Fatal(err)
	}

	// Import should replace, not append.
	if len(target.Criteria) != 1 {
		t.Fatalf("expected 1 criterion after import, got %d", len(target.Criteria))
	}
	if target.Criteria[0].Name != "from_file" {
		t.Errorf("expected 'from_file', got %q", target.Criteria[0].Name)
	}
}

func TestDefaultClinicalRubric(t *testing.T) {
	r := DefaultClinicalRubric()

	expectedCriteria := []string{
		"diagnostic_accuracy",
		"phi_safety",
		"care_plan_completeness",
		"tone_appropriateness",
		"tool_call_correctness",
		"fallback_triggered",
	}

	if len(r.Criteria) != len(expectedCriteria) {
		t.Fatalf("expected %d criteria, got %d", len(expectedCriteria), len(r.Criteria))
	}

	for _, name := range expectedCriteria {
		c, ok := r.GetCriterion(name)
		if !ok {
			t.Errorf("missing criterion: %s", name)
			continue
		}

		if c.Definition == "" {
			t.Errorf("criterion %s has empty definition", name)
		}
		if c.PassDef == "" {
			t.Errorf("criterion %s has empty pass definition", name)
		}
		if c.FailDef == "" {
			t.Errorf("criterion %s has empty fail definition", name)
		}
		if len(c.Examples) < 2 {
			t.Errorf("criterion %s has fewer than 2 examples (got %d)", name, len(c.Examples))
		}
		if c.Version < 1 {
			t.Errorf("criterion %s has version %d (expected >= 1)", name, c.Version)
		}
		if c.UpdatedAt.IsZero() {
			t.Errorf("criterion %s has zero UpdatedAt", name)
		}

		// Verify examples have pass and fail.
		hasPass, hasFail := false, false
		for _, ex := range c.Examples {
			if ex.Label == LabelPass {
				hasPass = true
			}
			if ex.Label == LabelFail {
				hasFail = true
			}
		}
		if !hasPass {
			t.Errorf("criterion %s has no pass example", name)
		}
		if !hasFail {
			t.Errorf("criterion %s has no fail example", name)
		}
	}
}

func TestDefaultClinicalRubric_RoundTrip(t *testing.T) {
	// Verify the default rubric survives a JSON round-trip.
	dir := t.TempDir()
	path := filepath.Join(dir, "clinical.json")

	original := DefaultClinicalRubric()
	if err := original.ExportJSON(path); err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	imported := NewRubric()
	if err := imported.ImportJSON(path); err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}

	if len(imported.Criteria) != len(original.Criteria) {
		t.Errorf("criteria count mismatch after round-trip: %d vs %d",
			len(imported.Criteria), len(original.Criteria))
	}

	for i, c := range original.Criteria {
		ic := imported.Criteria[i]
		if c.Name != ic.Name {
			t.Errorf("criterion %d name mismatch: %q vs %q", i, c.Name, ic.Name)
		}
		if len(c.Examples) != len(ic.Examples) {
			t.Errorf("criterion %s example count mismatch: %d vs %d",
				c.Name, len(c.Examples), len(ic.Examples))
		}
	}
}

func TestRubricExample_Labels(t *testing.T) {
	// Verify label constants.
	if LabelPass != "pass" {
		t.Errorf("LabelPass = %q, want 'pass'", LabelPass)
	}
	if LabelFail != "fail" {
		t.Errorf("LabelFail = %q, want 'fail'", LabelFail)
	}
}
