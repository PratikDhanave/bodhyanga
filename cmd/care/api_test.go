package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
)

// TestLLMTraceBuffer_SnapshotReturnsMostRecentFirst asserts the trace buffer
// returns the most recently recorded items first.
func TestLLMTraceBuffer_SnapshotReturnsMostRecentFirst(t *testing.T) {
	b := newLLMTraceBuffer(10)
	for i := 0; i < 5; i++ {
		b.record(observability.LLMTrace{Provider: "p", Model: "m", AgentID: "", InputTokens: i})
	}
	snap := b.snapshot(3)
	if len(snap) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap))
	}
	// Newest (i=4) first.
	if snap[0].InputTokens != 4 || snap[1].InputTokens != 3 || snap[2].InputTokens != 2 {
		t.Errorf("expected reverse chronological [4,3,2], got [%d,%d,%d]",
			snap[0].InputTokens, snap[1].InputTokens, snap[2].InputTokens)
	}
}

// TestLLMTraceBuffer_RingBufferEvicts asserts that the buffer caps itself at
// the configured size and snapshot(N) is bounded by that cap.
func TestLLMTraceBuffer_RingBufferEvicts(t *testing.T) {
	b := newLLMTraceBuffer(3)
	for i := 0; i < 5; i++ {
		b.record(observability.LLMTrace{InputTokens: i})
	}
	snap := b.snapshot(10)
	if len(snap) != 3 {
		t.Fatalf("expected ring-buffer cap 3, got %d", len(snap))
	}
	// Oldest retained should be the third-from-last record (i=2), newest first.
	if snap[0].InputTokens != 4 || snap[2].InputTokens != 2 {
		t.Errorf("expected newest=4 oldest=2, got newest=%d oldest=%d",
			snap[0].InputTokens, snap[2].InputTokens)
	}
}

// TestLoadFixtures_ReadsDemoJSONFromDir writes two valid fixtures and one
// invalid one, and asserts loadFixtures returns the two valid ones in
// sorted filename order.
func TestLoadFixtures_ReadsDemoJSONFromDir(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bravo", fixtureFile{CaseID: "case-bravo", Condition: "cap"})
	writeFixture(t, dir, "alpha", fixtureFile{CaseID: "case-alpha", Condition: "chf"})
	// Invalid JSON — loadFixtures must skip it.
	if err := os.WriteFile(filepath.Join(dir, "demo_bad.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	got, err := loadFixtures(dir)
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 valid fixtures, got %d (%+v)", len(got), got)
	}
	if got[0].CaseID != "case-alpha" || got[1].CaseID != "case-bravo" {
		t.Errorf("expected sorted by filename [alpha, bravo], got [%s, %s]",
			got[0].CaseID, got[1].CaseID)
	}
}

// TestLoadFixtureByID_HitAndMiss checks both the present and absent paths.
func TestLoadFixtureByID_HitAndMiss(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "one", fixtureFile{CaseID: "case-one", ChiefComplaint: "fever"})
	writeFixture(t, dir, "two", fixtureFile{CaseID: "case-two", ChiefComplaint: "cough"})

	f, ok, err := loadFixtureByID(dir, "case-two")
	if err != nil {
		t.Fatalf("hit error: %v", err)
	}
	if !ok || f.ChiefComplaint != "cough" {
		t.Errorf("expected hit on case-two, got ok=%v f=%+v", ok, f)
	}

	if _, ok, err := loadFixtureByID(dir, "case-unknown"); err != nil || ok {
		t.Errorf("expected miss with no error, got ok=%v err=%v", ok, err)
	}
}

// TestAPIAgents_ReturnsRegisteredAgentsSortedByID registers three stub agents
// in non-sorted order and asserts the handler returns them sorted by id.
func TestAPIAgents_ReturnsRegisteredAgentsSortedByID(t *testing.T) {
	s, _, _ := newTestAPIServer(t)
	ctx := context.Background()
	for _, a := range []agent.Agent{
		stubAgent{id: "zeta", caps: []string{"a"}},
		stubAgent{id: "alpha", caps: []string{"b"}},
		stubAgent{id: "mu", caps: []string{"c"}},
	} {
		if err := s.reg.Register(ctx, a); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()
	s.agents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(got))
	}
	want := []string{"alpha", "mu", "zeta"}
	for i, w := range want {
		if got[i]["id"] != w {
			t.Errorf("position %d: want %s, got %v", i, w, got[i]["id"])
		}
	}
}

// TestAPIPanelReport_FlagsDiabetesWithoutA1c covers the panel rule that emits
// an a1c_overdue gap for dm patients with LastA1c==0, and does not produce a
// gap for non-dm patients.
func TestAPIPanelReport_FlagsDiabetesWithoutA1c(t *testing.T) {
	s, _, _ := newTestAPIServer(t)
	s.patients.Upsert(medical.PatientRecord{
		PatientID: "p1", Conditions: []string{"dm"}, LastA1c: 0,
	})
	s.patients.Upsert(medical.PatientRecord{
		PatientID: "p2", Conditions: []string{"htn"}, LastA1c: 0,
	})

	rec := httptest.NewRecorder()
	s.panelReport(rec, httptest.NewRequest(http.MethodGet, "/api/panel/report", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp struct {
		Patients    int                  `json:"patients"`
		Gaps        []map[string]any     `json:"gaps"`
		ByCondition map[string]int       `json:"by_condition"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Patients != 2 {
		t.Errorf("expected patients=2, got %d", resp.Patients)
	}
	if len(resp.Gaps) != 1 {
		t.Fatalf("expected exactly 1 gap, got %d (%+v)", len(resp.Gaps), resp.Gaps)
	}
	g := resp.Gaps[0]
	if g["measure"] != "a1c_overdue" || g["severity"] != "normal" || g["patient_id"] != "p1" {
		t.Errorf("unexpected gap: %+v", g)
	}
}

// TestAPIFineTuneStatus_CountsByStatus submits four reviews (one per terminal
// status plus pending) and verifies the decided/pending tallies.
func TestAPIFineTuneStatus_CountsByStatus(t *testing.T) {
	s, _, _ := newTestAPIServer(t)
	// Helper to submit then decide.
	submit := func(caseID string) string {
		r := s.reviews.Submit(medical.ReviewRequest{
			Kind: "diagnosis_review", CaseID: caseID, NextTo: "x", NextType: "t", Payload: "{}",
		})
		return r.ID
	}
	approveID := submit("c1")
	modifyID := submit("c2")
	rejectID := submit("c3")
	_ = submit("c4") // remains pending

	if _, ok := s.reviews.Decide(approveID, "rv", "approve", "", ""); !ok {
		t.Fatal("approve failed")
	}
	if _, ok := s.reviews.Decide(modifyID, "rv", "approve_with_modifications", "", "tweak"); !ok {
		t.Fatal("modify failed")
	}
	if _, ok := s.reviews.Decide(rejectID, "rv", "reject", "no", ""); !ok {
		t.Fatal("reject failed")
	}

	rec := httptest.NewRecorder()
	s.fineTuneStatus(rec, httptest.NewRequest(http.MethodGet, "/api/fine-tune/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// decided = approved + modified + rejected = 3; pending = 1.
	if v, _ := resp["decided_reviews"].(float64); int(v) != 3 {
		t.Errorf("decided_reviews want 3, got %v", resp["decided_reviews"])
	}
	if v, _ := resp["pending_reviews"].(float64); int(v) != 1 {
		t.Errorf("pending_reviews want 1, got %v", resp["pending_reviews"])
	}
}

// TestAPIBenchUpload_ThenLatestReturnsRows uploads a summary then asserts
// /api/bench/latest returns one row per provider.
func TestAPIBenchUpload_ThenLatestReturnsRows(t *testing.T) {
	s, _, _ := newTestAPIServer(t)
	// Reset the global bench results so prior tests don't leak in.
	benchResults.mu.Lock()
	benchResults.results = nil
	benchResults.mu.Unlock()

	body := strings.NewReader(`{"summary":{"rule":{"accuracy":0.8},"llm-openai":{"accuracy":0.9}}}`)
	rec := httptest.NewRecorder()
	s.benchUpload(rec, httptest.NewRequest(http.MethodPost, "/api/bench/results", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status: %d body=%s", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	s.benchLatest(rec2, httptest.NewRequest(http.MethodGet, "/api/bench/latest", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("latest status: %d", rec2.Code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 provider rows, got %d (%+v)", len(rows), rows)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if p, _ := r["provider"].(string); p != "" {
			seen[p] = true
		}
	}
	if !seen["rule"] || !seen["llm-openai"] {
		t.Errorf("missing providers in response: %+v", rows)
	}
}

// TestAPIReviewDecide_PublishesResumePayloadOnApproval submits a pending
// review then approves it via reviewDecide; asserts the original payload is
// republished to the configured NextTo agent with NextType.
func TestAPIReviewDecide_PublishesResumePayloadOnApproval(t *testing.T) {
	s, bus, _ := newTestAPIServer(t)

	// Subscribe to the engagement agent's topic before we POST so we don't
	// miss the publish.
	gotMsg := make(chan agent.Message, 4)
	bus.Subscribe("engagement", func(_ context.Context, m agent.Message) {
		select {
		case gotMsg <- m:
		default:
		}
	})

	submitted := s.reviews.Submit(medical.ReviewRequest{
		Kind: "care_plan_review", CaseID: "case-x",
		NextTo: "engagement", NextType: "care_plan_ready",
		Payload: `{"plan":"daily-weights"}`,
		Meta:    map[string]any{"case_id": "case-x", "review.reason": "auto"},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/reviews/"+submitted.ID+"/decide",
		strings.NewReader(`{"reviewer_id":"clin","decision":"approve"}`))
	req.SetPathValue("id", submitted.ID)
	rec := httptest.NewRecorder()
	s.reviewDecide(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}

	select {
	case m := <-gotMsg:
		if m.Type != "care_plan_ready" {
			t.Errorf("expected type=care_plan_ready, got %q", m.Type)
		}
		if m.To != "engagement" {
			t.Errorf("expected to=engagement, got %q", m.To)
		}
		if m.Content != `{"plan":"daily-weights"}` {
			t.Errorf("expected original payload, got %q", m.Content)
		}
		if _, present := m.Metadata["review.reason"]; present {
			t.Errorf("expected review.* meta keys stripped, got %+v", m.Metadata)
		}
		if cid, _ := m.Metadata["case_id"].(string); cid != "case-x" {
			t.Errorf("expected case_id retained, got %+v", m.Metadata)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive resume publish on engagement topic")
	}
}
