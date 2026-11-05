package careapi

import (
	"fmt"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
)

// N distinct cases submitted in parallel yield N independent handles and
// registry rows, with per-case audit trails that don't cross-contaminate
// (SC-006, FR-014). Run with -race to catch shared-state bugs.
func TestConcurrent_IndependentCases(t *testing.T) {
	ta := newTestAPI(t, nil)
	const n = 12

	var wg sync.WaitGroup
	tokens := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pc := agents.PatientCase{
				CaseID:   fmt.Sprintf("PT-%d", i),
				Symptoms: []string{"chest pain"},
			}
			var h handleResponse
			if code := ta.doJSON(t, "POST", "/v1/cases", pc, &h); code != 201 {
				t.Errorf("case %d submit = %d", i, code)
				return
			}
			tokens[i] = h.CaseToken
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, tok := range tokens {
		if tok == "" {
			t.Fatalf("case %d got no token", i)
		}
		if seen[tok] {
			t.Fatalf("duplicate token %q — cases collided", tok)
		}
		seen[tok] = true

		// Each case's audit read is scoped to its own token.
		var body auditBody
		ta.doJSON(t, "GET", "/v1/cases/"+tok+"/audit", nil, &body)
		for _, ev := range body.Events {
			if ev.CaseToken != tok {
				t.Errorf("case %s audit leaked event for %s", tok, ev.CaseToken)
			}
		}
	}
	if len(seen) != n {
		t.Errorf("want %d distinct cases, got %d", n, len(seen))
	}
}
