package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// writeFixture serialises reviews to a tmp file and returns its path.
func writeFixture(t *testing.T, reviews []medical.ReviewRequest) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	b, err := json.Marshal(reviews)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []map[string]any
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("invalid JSONL line %q: %v", string(line), err)
		}
		out = append(out, m)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return out
}

func approved(id, caseID, payload string) medical.ReviewRequest {
	return medical.ReviewRequest{
		ID:          id,
		Kind:        "diagnosis_review",
		Status:      medical.ReviewApproved,
		CaseID:      caseID,
		PatientID:   "pt-001",
		Payload:     payload,
		Detail:      "case detail for " + caseID,
		SubmittedAt: time.Date(2026, 5, 23, 9, 0, 0, 0, time.UTC),
		DecidedAt:   time.Date(2026, 5, 23, 9, 5, 0, 0, time.UTC),
		ReviewerID:  "dr-alice",
	}
}

func modified(id, caseID, original, modification string) medical.ReviewRequest {
	r := approved(id, caseID, original)
	r.Status = medical.ReviewModified
	r.Modification = modification
	return r
}

func rejected(id, caseID, payload, reason string) medical.ReviewRequest {
	r := approved(id, caseID, payload)
	r.Status = medical.ReviewRejected
	r.Reason = reason
	return r
}

func pendingReq(id, caseID, payload string) medical.ReviewRequest {
	r := approved(id, caseID, payload)
	r.Status = medical.ReviewPending
	r.DecidedAt = time.Time{}
	r.ReviewerID = ""
	return r
}

// TestExportSFT_WritesApprovedReviewsAsAssistantTurns: two approved reviews
// → exactly two SFT lines whose assistant content equals the approved payload.
func TestExportSFT_WritesApprovedReviewsAsAssistantTurns(t *testing.T) {
	reviews := []medical.ReviewRequest{
		approved("r-1", "case-001", `{"diagnosis":"CAP","confidence":0.87}`),
		approved("r-2", "case-002", `{"diagnosis":"ADHF","confidence":0.81}`),
	}
	in := writeFixture(t, reviews)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code, err := run(config{in: in, format: "sft", outDir: outDir, minReviews: 1, includeRejected: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	lines := readJSONL(t, filepath.Join(outDir, "sft.jsonl"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 SFT lines, got %d", len(lines))
	}
	for i, line := range lines {
		msgs, ok := line["messages"].([]any)
		if !ok || len(msgs) != 3 {
			t.Fatalf("line %d: expected 3 messages, got %v", i, line["messages"])
		}
		roles := []string{}
		for _, m := range msgs {
			roles = append(roles, m.(map[string]any)["role"].(string))
		}
		want := []string{"system", "user", "assistant"}
		for j, r := range want {
			if roles[j] != r {
				t.Fatalf("line %d msg %d: want role %q got %q", i, j, r, roles[j])
			}
		}
		assistant := msgs[2].(map[string]any)["content"].(string)
		if assistant != reviews[i].Payload {
			t.Fatalf("line %d: assistant=%q, want %q", i, assistant, reviews[i].Payload)
		}
	}
}

// TestExportDPO_ConstructsPairsFromModifications: one approved_with_modifications
// → one DPO line with chosen != rejected.
func TestExportDPO_ConstructsPairsFromModifications(t *testing.T) {
	reviews := []medical.ReviewRequest{
		modified("r-mod", "case-003",
			`{"diagnosis":"Pneumonia, unspecified","confidence":0.55}`,
			`{"diagnosis":"Community-acquired pneumonia","confidence":0.87}`),
	}
	in := writeFixture(t, reviews)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code, err := run(config{in: in, format: "dpo", outDir: outDir, minReviews: 1, includeRejected: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit 0 wanted, got %d", code)
	}
	lines := readJSONL(t, filepath.Join(outDir, "dpo.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 DPO line, got %d", len(lines))
	}
	chosen := lines[0]["chosen"].(string)
	rej := lines[0]["rejected"].(string)
	if chosen == rej {
		t.Fatalf("chosen == rejected (%q) — DPO requires a distinction", chosen)
	}
	if !strings.Contains(chosen, "Community-acquired pneumonia") {
		t.Fatalf("chosen=%q, want modification payload", chosen)
	}
	if !strings.Contains(rej, "unspecified") {
		t.Fatalf("rejected=%q, want original payload", rej)
	}
}

// TestExportDPO_PairsApprovedWithRejectedForSameCase: one approved + one
// rejected sharing case_id → one DPO pair.
func TestExportDPO_PairsApprovedWithRejectedForSameCase(t *testing.T) {
	reviews := []medical.ReviewRequest{
		approved("r-ok", "case-shared", `{"diagnosis":"CAP","confidence":0.87}`),
		rejected("r-bad", "case-shared", `{"diagnosis":"viral URI","confidence":0.4}`, "missed consolidation on CXR"),
	}
	in := writeFixture(t, reviews)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code, err := run(config{in: in, format: "dpo", outDir: outDir, minReviews: 1, includeRejected: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit 0 wanted, got %d", code)
	}
	lines := readJSONL(t, filepath.Join(outDir, "dpo.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 DPO line, got %d", len(lines))
	}
	chosen := lines[0]["chosen"].(string)
	rej := lines[0]["rejected"].(string)
	if !strings.Contains(chosen, "CAP") {
		t.Fatalf("chosen=%q, want approved payload", chosen)
	}
	if !strings.Contains(rej, "viral URI") {
		t.Fatalf("rejected=%q, want rejected payload", rej)
	}
}

// TestExportNoDecidedReviewsExitsCleanly: only pending entries → empty files,
// exit 0, "no decisions" stdout message.
func TestExportNoDecidedReviewsExitsCleanly(t *testing.T) {
	reviews := []medical.ReviewRequest{
		pendingReq("r-p1", "case-x", `{"diagnosis":"?","confidence":0.0}`),
		pendingReq("r-p2", "case-y", `{"diagnosis":"?","confidence":0.0}`),
	}
	in := writeFixture(t, reviews)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code, err := run(config{in: in, format: "both", outDir: outDir, minReviews: 1, includeRejected: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit 0 wanted, got %d", code)
	}
	if !strings.Contains(stdout.String(), "no decisions found") {
		t.Fatalf("expected 'no decisions found' on stdout, got %q", stdout.String())
	}
	for _, name := range []string{"sft.jsonl", "dpo.jsonl"} {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Size() != 0 {
			t.Fatalf("expected empty %s, got %d bytes", name, info.Size())
		}
	}
}

// TestExportSkipsRejectedWhenIncludeRejectedFalse: -include-rejected=false →
// no SFT line for the rejected review.
func TestExportSkipsRejectedWhenIncludeRejectedFalse(t *testing.T) {
	reviews := []medical.ReviewRequest{
		approved("r-ok", "case-1", `{"diagnosis":"CAP"}`),
		rejected("r-bad", "case-2", `{"diagnosis":"bad"}`, "wrong"),
	}
	in := writeFixture(t, reviews)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code, err := run(config{in: in, format: "sft", outDir: outDir, minReviews: 1, includeRejected: false}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit 0 wanted, got %d", code)
	}
	lines := readJSONL(t, filepath.Join(outDir, "sft.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 SFT line (rejected excluded), got %d", len(lines))
	}
	meta := lines[0]["meta"].(map[string]any)
	if meta["review_id"].(string) != "r-ok" {
		t.Fatalf("expected r-ok in SFT, got review_id=%v", meta["review_id"])
	}
}

// TestExportEnforcesMinReviewsWarning: decided count below threshold writes a
// warning to stderr but still exits 0.
func TestExportEnforcesMinReviewsWarning(t *testing.T) {
	reviews := []medical.ReviewRequest{
		approved("r-only", "case-1", `{"diagnosis":"CAP"}`),
	}
	in := writeFixture(t, reviews)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code, err := run(config{in: in, format: "sft", outDir: outDir, minReviews: 5, includeRejected: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit 0 wanted (warning only), got %d", code)
	}
	if !strings.Contains(stderr.String(), "warning") {
		t.Fatalf("expected warning on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "only 1") {
		t.Fatalf("expected count in warning, got %q", stderr.String())
	}
}
