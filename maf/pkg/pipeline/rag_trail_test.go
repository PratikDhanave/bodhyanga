package pipeline_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/PratikDhanave/bodhyanga/pkg/embeddings"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/pipeline"
)

// TestRAG_TrailIsPHIandContentFree runs a RAG-enabled case end to end and
// asserts the audit trail leaks neither the raw case ID, the retrieval query,
// nor any snippet content — only PHI-free hop signals (SC-004, FR-009). This is
// the invariant that lets retrieval be enabled without weakening the boundary.
func TestRAG_TrailIsPHIandContentFree(t *testing.T) {
	r, err := agents.LoadRetriever(ragCorpus, &embeddings.Mock{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	rec := audit.NewInMemory()
	b := admit.New(admit.AllowAll(), rec, nil)
	var prompt string
	runner, err := pipeline.NewRunner(pipeline.Config{
		Boundary: b,
		Diagnostician: agents.NewLLMDiagnostician("diagnostician",
			capturingLLM(b, confidentACSNoCite, &prompt),
			agents.DiagnosticianConfig{Tau: 0.85, MaxRounds: 3}, b.Guard, r),
	}, rec)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runner.Run(context.Background(), agents.PatientCase{
		CaseID: "case-rag", Symptoms: []string{"acute chest pain", "diaphoresis"},
		History: []string{"hypertension"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan == nil {
		t.Fatal("RAG case did not finalize; nothing to scan")
	}

	// Sanity: the Evidence block DID reach the model (so retrieval ran) — the
	// content just must not have leaked into the audit trail.
	if !strings.Contains(prompt, "Evidence:") {
		t.Fatal("retrieval did not run; the trail scan would be vacuous")
	}

	trail := rec.List()
	if len(trail) == 0 {
		t.Fatal("empty trail")
	}
	blob, err := json.Marshal(trail)
	if err != nil {
		t.Fatal(err)
	}
	hay := string(blob)

	// Everything the trail must NEVER contain: the raw case ID, a query term,
	// and phrases that appear only in the corpus document body.
	forbidden := []string{
		"case-rag",         // raw case ID (only the sha256[:8] token may appear)
		"acute chest pain", // retrieval query text (de-identified, but still content)
		"diaphoresis",
		"12-lead ECG",    // snippet content unique to the acs-001 corpus doc
		"serial troponin",
	}
	for _, bad := range forbidden {
		if strings.Contains(hay, bad) {
			t.Errorf("audit trail leaked forbidden content %q", bad)
		}
	}
}
