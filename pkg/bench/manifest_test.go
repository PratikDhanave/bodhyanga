package bench

import (
	"testing"
)

func TestLoadManifest_ResolvesFixturesRelative(t *testing.T) {
	fixtures, err := LoadManifest("testdata/one_case.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(fixtures) != 1 {
		t.Fatalf("want 1 fixture, got %d", len(fixtures))
	}
	f := fixtures[0]
	if f.ID != "x" || f.Gold != "heart failure" || f.Case.CaseID != "x1" {
		t.Errorf("fixture wrong: %+v", f)
	}
	if len(f.Case.Symptoms) != 1 || f.Case.Symptoms[0] != "shortness of breath" {
		t.Errorf("fixture case not loaded: %+v", f.Case)
	}
}

func TestLoadManifest_Errors(t *testing.T) {
	if _, err := LoadManifest("testdata/empty.json"); err == nil {
		t.Error("empty manifest should error")
	}
	if _, err := LoadManifest("testdata/missing_fixture.json"); err == nil {
		t.Error("missing fixture should error (fail fast)")
	}
	if _, err := LoadManifest("testdata/does_not_exist.json"); err == nil {
		t.Error("missing manifest should error")
	}
	if _, err := LoadManifest("testdata/bad_json.json"); err == nil {
		t.Error("malformed manifest json should error")
	}
	if _, err := LoadManifest("testdata/no_path.json"); err == nil {
		t.Error("manifest case without a path should error")
	}
}

// Vocabulary-drift guard (T020): every gold in the committed suite must be a
// diagnosis the rule engine can actually produce. If a rule change renames a
// diagnosis, this fails loudly instead of silently dropping accuracy.
func TestCommittedManifest_GoldsAreProducible(t *testing.T) {
	producible := map[string]bool{
		"acute coronary syndrome":       true,
		"community-acquired pneumonia":  true,
		"heart failure":                 true,
		"undifferentiated presentation": true,
	}
	fixtures, err := LoadManifest("../../data/cases/manifest.json")
	if err != nil {
		t.Fatalf("load committed manifest: %v", err)
	}
	for _, f := range fixtures {
		if !producible[f.Gold] {
			t.Errorf("case %q gold %q is not a producible rule diagnosis — vocabulary drift", f.ID, f.Gold)
		}
	}
}
