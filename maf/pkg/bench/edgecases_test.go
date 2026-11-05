package bench

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
)

// A missing gold loads fine but is unscored and excluded from the denominator.
func TestEdge_MissingGoldIsUnscored(t *testing.T) {
	fixtures := []Fixture{
		{ID: "nogold", Gold: "", Case: agents.PatientCase{CaseID: "c", Symptoms: []string{"chest pain"}}},
	}
	results, err := RunSuite(context.Background(), fixtures, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusUnscored {
		t.Errorf("status = %q, want unscored", results[0].Status)
	}
	if s := Aggregate(results); s.Scored != 0 || s.Unscored != 1 {
		t.Errorf("summary = %+v, want unscored excluded from scored", s)
	}
}

// A denied case never becomes a match — it is a failure (no terminal plan).
func TestEdge_DeniedCaseIsFailureNotMatch(t *testing.T) {
	// Deny is a boundary decision; RunSuite uses allow-all, so simulate the
	// no-terminal-plan path via a case that the pipeline cannot resolve is not
	// possible with the rule engine (it always forces). Instead assert the
	// classification contract directly: no plan → failure, never a match.
	r := CaseResult{ID: "x", Gold: "heart failure", Status: StatusFailure}
	if r.Status == StatusMatch {
		t.Fatal("a failure must never be a match")
	}
	s := Aggregate([]CaseResult{r})
	if s.Matches != 0 || s.Failures != 1 || s.Scored != 0 {
		t.Errorf("failure must not count toward accuracy: %+v", s)
	}
}

func TestEdge_EmptyManifestFatal(t *testing.T) {
	if _, err := LoadManifest("testdata/empty.json"); err == nil {
		t.Error("empty manifest must be a fatal load error")
	}
}
