package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFewShotFile is a tiny helper that drops body into a fresh file under
// t.TempDir() and returns its path. t.Cleanup is automatic via TempDir.
func writeFewShotFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hitl.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// fixtureFourStatuses contains one of each status type so per-status filtering
// can be asserted. The approved and approved_with_modifications records carry
// the full Meta + Payload + Modification shape; rejected and pending are the
// negative path.
const fixtureFourStatuses = `[
  {
    "id": "rev-app-001",
    "kind": "diagnosis_review",
    "status": "approved",
    "subject": "Diagnosis review: CAP",
    "payload": "{\"diagnosis\":\"Community-acquired pneumonia\",\"confidence\":0.87,\"rationale\":\"r\"}",
    "meta": {"case_state": "{\"presentation\":\"58M fever cough\",\"condition\":\"cap\",\"test_results\":{\"chest_xray\":\"RLL infiltrate\"}}"},
    "reviewer_id": "dr-a",
    "decided_at": "2026-05-23T09:00:00Z"
  },
  {
    "id": "rev-mod-001",
    "kind": "diagnosis_review",
    "status": "approved_with_modifications",
    "subject": "Diagnosis review: CHF",
    "payload": "{\"diagnosis\":\"Heart failure exacerbation\",\"confidence\":0.78,\"rationale\":\"r\"}",
    "modification": "{\"diagnosis\":\"Acute decompensated heart failure (ADHF)\",\"confidence\":0.88,\"rationale\":\"r2\"}",
    "meta": {"case_state": "{\"presentation\":\"72F HFrEF dyspnea\",\"condition\":\"chf\"}"},
    "reviewer_id": "dr-b",
    "decided_at": "2026-05-23T10:00:00Z"
  },
  {
    "id": "rev-rej-001",
    "kind": "diagnosis_review",
    "status": "rejected",
    "subject": "Diagnosis review: misfire",
    "payload": "{\"diagnosis\":\"Anxiety\",\"confidence\":0.6,\"rationale\":\"r\"}",
    "meta": {"case_state": "{\"presentation\":\"45F pleuritic chest pain\"}"},
    "reviewer_id": "dr-a",
    "decided_at": "2026-05-23T11:00:00Z"
  },
  {
    "id": "rev-pend-001",
    "kind": "diagnosis_review",
    "status": "pending",
    "subject": "Diagnosis review: URI",
    "payload": "{\"diagnosis\":\"Viral URI\",\"confidence\":0.7,\"rationale\":\"r\"}",
    "meta": {"case_state": "{\"presentation\":\"29M sore throat\"}"}
  }
]`

func TestLoadFewShotCorpus_OnlyApprovedAndModified(t *testing.T) {
	path := writeFewShotFile(t, fixtureFourStatuses)

	docs, err := LoadFewShotCorpus(path)
	if err != nil {
		t.Fatalf("LoadFewShotCorpus: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 docs (approved + approved_with_modifications), got %d", len(docs))
	}

	ids := map[string]Document{docs[0].ID: docs[0], docs[1].ID: docs[1]}
	approved, ok := ids["fewshot-rev-app-001"]
	if !ok {
		t.Fatalf("missing fewshot-rev-app-001; got ids: %v", keysOf(ids))
	}
	if !strings.Contains(approved.Content, "Approved diagnosis:") {
		t.Fatalf("approved content missing 'Approved diagnosis:' header: %s", approved.Content)
	}
	if !strings.Contains(approved.Content, "Community-acquired pneumonia") {
		t.Fatalf("approved content missing payload diagnosis: %s", approved.Content)
	}
	if !strings.Contains(approved.Content, "Presentation: 58M fever cough") {
		t.Fatalf("approved content missing case presentation summary: %s", approved.Content)
	}
	if approved.Metadata["decision"] != "approved" {
		t.Fatalf("approved metadata.decision=%q", approved.Metadata["decision"])
	}
	if approved.Metadata["hitl"] != "true" {
		t.Fatalf("approved metadata.hitl=%q", approved.Metadata["hitl"])
	}
	if approved.Metadata["reviewer_id"] != "dr-a" {
		t.Fatalf("approved metadata.reviewer_id=%q", approved.Metadata["reviewer_id"])
	}
	if !strings.Contains(approved.Source, "hitl-decision/dr-a/2026-05-23T09:00:00Z") {
		t.Fatalf("approved source=%q does not embed reviewer + RFC3339 decided_at", approved.Source)
	}

	modified, ok := ids["fewshot-rev-mod-001"]
	if !ok {
		t.Fatalf("missing fewshot-rev-mod-001")
	}
	if !strings.Contains(modified.Content, "Acute decompensated heart failure") {
		t.Fatalf("modified content should carry the modification text, got: %s", modified.Content)
	}
	if strings.Contains(modified.Content, "Heart failure exacerbation") {
		t.Fatalf("modified content should prefer modification over original payload, got: %s", modified.Content)
	}
	if modified.Metadata["decision"] != "approved_with_modifications" {
		t.Fatalf("modified metadata.decision=%q", modified.Metadata["decision"])
	}

	// Rejected + pending must be absent.
	for _, bad := range []string{"fewshot-rev-rej-001", "fewshot-rev-pend-001"} {
		if _, ok := ids[bad]; ok {
			t.Fatalf("non-positive review %q leaked into corpus", bad)
		}
	}
}

func TestLoadFewShotCorpus_HandlesArrayAndWrapperFormats(t *testing.T) {
	// Bare array.
	arrPath := writeFewShotFile(t, fixtureFourStatuses)
	arrDocs, err := LoadFewShotCorpus(arrPath)
	if err != nil {
		t.Fatalf("LoadFewShotCorpus(array): %v", err)
	}
	if len(arrDocs) != 2 {
		t.Fatalf("array form: want 2 docs, got %d", len(arrDocs))
	}

	// Wrapper form: {"reviews": [...]}.
	wrapBody := `{"reviews": ` + fixtureFourStatuses + `}`
	wrapPath := writeFewShotFile(t, wrapBody)
	wrapDocs, err := LoadFewShotCorpus(wrapPath)
	if err != nil {
		t.Fatalf("LoadFewShotCorpus(wrapper): %v", err)
	}
	if len(wrapDocs) != 2 {
		t.Fatalf("wrapper form: want 2 docs, got %d", len(wrapDocs))
	}

	// Both formats must yield the same IDs.
	got := map[string]bool{arrDocs[0].ID: true, arrDocs[1].ID: true}
	for _, d := range wrapDocs {
		if !got[d.ID] {
			t.Fatalf("wrapper-form doc %q absent from array-form result", d.ID)
		}
	}
}

func TestLoadFewShotCorpus_BadJSONErrors(t *testing.T) {
	path := writeFewShotFile(t, "{not valid json")
	if _, err := LoadFewShotCorpus(path); err == nil {
		t.Fatalf("expected parse error on malformed JSON")
	}
}

func TestLoadFewShotCorpus_EmptyFileReturnsEmpty(t *testing.T) {
	path := writeFewShotFile(t, "")
	docs, err := LoadFewShotCorpus(path)
	if err != nil {
		t.Fatalf("LoadFewShotCorpus(empty): %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("want 0 docs from empty file, got %d", len(docs))
	}
}

func TestLoadFewShotCorpus_OnSampleFixture(t *testing.T) {
	// Resolve repo root from this file's location: pkg/knowledge → ../../data.
	// Tests run with CWD set to the package directory, so this is stable across
	// re-runs without any environment fiddling.
	path, err := filepath.Abs(filepath.Join("..", "..", "data", "hitl-sample.json"))
	if err != nil {
		t.Fatalf("resolve sample fixture path: %v", err)
	}
	t.Cleanup(func() {
		// Nothing to clean — included so reruns are explicit about leaving
		// the fixture untouched even if a future test mutates files.
	})

	docs, err := LoadFewShotCorpus(path)
	if err != nil {
		t.Fatalf("LoadFewShotCorpus(sample): %v", err)
	}
	// data/hitl-sample.json: rev-cap-001 (approved) + rev-chf-001 (modified)
	// + rev-pe-001 (rejected — excluded) + rev-pe-002 (approved) +
	// rev-pneumonia-vague (pending — excluded) + rev-cap-002 (approved) = 4.
	if len(docs) != 4 {
		t.Fatalf("want 4 docs from data/hitl-sample.json (2 approved CAP + 1 modified CHF + 1 approved PE), got %d", len(docs))
	}

	ids := make(map[string]bool, len(docs))
	for _, d := range docs {
		ids[d.ID] = true
	}
	want := []string{
		"fewshot-rev-cap-001",
		"fewshot-rev-chf-001",
		"fewshot-rev-pe-002",
		"fewshot-rev-cap-002",
	}
	for _, w := range want {
		if !ids[w] {
			t.Errorf("missing expected fewshot doc %q (got %v)", w, ids)
		}
	}
	for _, banned := range []string{
		"fewshot-rev-pe-001",        // rejected
		"fewshot-rev-pneumonia-vague", // pending
	} {
		if ids[banned] {
			t.Errorf("non-positive review %q must not be in fewshot corpus", banned)
		}
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
