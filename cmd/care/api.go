package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/engagement"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/registry"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

// fixtureFile mirrors the JSON shape used by cmd/demo and cmd/bench.
type fixtureFile struct {
	CaseID          string                   `json:"case_id"`
	Condition       string                   `json:"condition"`
	ChiefComplaint  string                   `json:"chief_complaint"`
	HPI             string                   `json:"hpi"`
	Demographics    string                   `json:"demographics"`
	Vitals          string                   `json:"vitals"`
	GoldDiagnosis   string                   `json:"gold_diagnosis"`
	MaxCostUSD      float64                  `json:"max_cost_usd"`
	RPMObservations []medical.RPMObservation `json:"rpm_observations"`
}

func loadFixtures(dir string) ([]fixtureFile, error) {
	files, err := filepath.Glob(filepath.Join(dir, "demo_*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := make([]fixtureFile, 0, len(files))
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f fixtureFile
		if err := json.Unmarshal(b, &f); err != nil {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func loadFixtureByID(dir, id string) (fixtureFile, bool, error) {
	files, err := loadFixtures(dir)
	if err != nil {
		return fixtureFile{}, false, err
	}
	for _, f := range files {
		if f.CaseID == id {
			return f, true, nil
		}
	}
	return fixtureFile{}, false, nil
}

// llmTraceBuffer captures `[llm-trace] {json}` records emitted via
// pkg/observability so the SPA can display recent calls.
type llmTraceBuffer struct {
	mu   sync.Mutex
	buf  []observability.LLMTrace
	size int
}

func newLLMTraceBuffer(size int) *llmTraceBuffer { return &llmTraceBuffer{size: size} }

func (b *llmTraceBuffer) record(t observability.LLMTrace) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, t)
	if len(b.buf) > b.size {
		b.buf = b.buf[len(b.buf)-b.size:]
	}
}

func (b *llmTraceBuffer) snapshot(limit int) []observability.LLMTrace {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > len(b.buf) {
		limit = len(b.buf)
	}
	out := make([]observability.LLMTrace, limit)
	for i := 0; i < limit; i++ {
		out[i] = b.buf[len(b.buf)-1-i]
	}
	return out
}

// apiServer wires the /api/* routes onto the supplied mux.
type apiServer struct {
	bus         *comm.InMemoryBus
	reg         *registry.InMemoryRegistry
	queue       *tasks.Queue
	reviews     *hitl.Queue
	patients    *medical.PatientRegistry
	notifier    *engagement.InMemoryNotifier
	traces      *llmTraceBuffer
	runs        *runManager
	fixturesDir string
	provider    string
}

func (s *apiServer) mount(mux *http.ServeMux) {
	mux.HandleFunc("/api/runtime", s.runtime)
	mux.HandleFunc("/api/agents", s.agents)
	mux.HandleFunc("/api/fixtures", s.fixtures)
	mux.HandleFunc("/api/tasks", s.tasksHandler)
	mux.HandleFunc("/api/patients", s.patientsList)
	mux.HandleFunc("/api/notifications", s.notifications)
	mux.HandleFunc("/api/panel/report", s.panelReport)
	mux.HandleFunc("/api/llm-traces", s.llmTraces)
	mux.HandleFunc("/api/fine-tune/status", s.fineTuneStatus)
	mux.HandleFunc("/api/bench/latest", s.benchLatest)
	mux.HandleFunc("/api/bench/results", s.benchUpload)

	mux.HandleFunc("GET /api/reviews", s.reviewsList)
	mux.HandleFunc("GET /api/reviews/{id}", s.reviewGet)
	mux.HandleFunc("POST /api/reviews/{id}/decide", s.reviewDecide)

	mux.HandleFunc("/api/runs", startRunHandler(s.bus, s.runs, s.fixturesDir, s.provider))
	mux.HandleFunc("/api/runs/", runEventsHandler(s.runs))
}

func (s *apiServer) runtime(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"diagnostician": s.provider,
		"started_at":    time.Now().UTC().Format(time.RFC3339),
		"hitl_enabled":  true,
		"rag_enabled":   os.Getenv("BODH_KNOWLEDGE_CORPUS") != "",
	})
}

func (s *apiServer) agents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := make([]map[string]any, 0)
	for _, a := range s.reg.List(ctx) {
		out = append(out, map[string]any{
			"id":           a.ID(),
			"name":         a.Name(),
			"capabilities": a.Capabilities(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["id"].(string) < out[j]["id"].(string)
	})
	writeJSON(w, out)
}

func (s *apiServer) fixtures(w http.ResponseWriter, _ *http.Request) {
	files, err := loadFixtures(s.fixturesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(files))
	for _, f := range files {
		out = append(out, map[string]any{
			"case_id":         f.CaseID,
			"condition":       f.Condition,
			"chief_complaint": f.ChiefComplaint,
			"hpi":             f.HPI,
			"demographics":    f.Demographics,
			"vitals":          f.Vitals,
			"gold_diagnosis":  f.GoldDiagnosis,
			"max_cost_usd":    f.MaxCostUSD,
			"file":            f.CaseID + ".json",
		})
	}
	writeJSON(w, out)
}

func (s *apiServer) tasksHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.queue.List())
}

func (s *apiServer) patientsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.patients.List())
}

func (s *apiServer) notifications(w http.ResponseWriter, _ *http.Request) {
	if s.notifier == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, s.notifier.List())
}

func (s *apiServer) panelReport(w http.ResponseWriter, _ *http.Request) {
	patients := s.patients.List()
	byCondition := map[string]int{}
	gaps := []map[string]any{}
	for _, p := range patients {
		for _, c := range p.Conditions {
			byCondition[c]++
			if c == "dm" && p.LastA1c == 0 {
				gaps = append(gaps, map[string]any{
					"patient_id": p.PatientID, "measure": "a1c_overdue",
					"severity": "normal", "reason": "no recent A1c on file",
				})
			}
		}
	}
	writeJSON(w, map[string]any{
		"patients":     len(patients),
		"gaps":         gaps,
		"by_condition": byCondition,
	})
}

func (s *apiServer) llmTraces(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, s.traces.snapshot(limit))
}

func (s *apiServer) fineTuneStatus(w http.ResponseWriter, _ *http.Request) {
	all := s.reviews.List()
	decided, pending, sft, dpo := 0, 0, 0, 0
	caseDecisions := map[string]map[medical.ReviewStatus]int{}
	for _, r := range all {
		switch r.Status {
		case medical.ReviewApproved:
			decided++
			sft++
		case medical.ReviewModified:
			decided++
			sft++
			dpo++
		case medical.ReviewRejected:
			decided++
			sft++
		case medical.ReviewPending:
			pending++
		}
		if r.CaseID != "" {
			if caseDecisions[r.CaseID] == nil {
				caseDecisions[r.CaseID] = map[medical.ReviewStatus]int{}
			}
			caseDecisions[r.CaseID][r.Status]++
		}
	}
	for _, m := range caseDecisions {
		if m[medical.ReviewApproved] > 0 && m[medical.ReviewRejected] > 0 {
			dpo++
		}
	}
	writeJSON(w, map[string]any{
		"sft_lines":        sft,
		"dpo_pairs":        dpo,
		"decided_reviews":  decided,
		"pending_reviews":  pending,
	})
}

var benchResults = struct {
	mu      sync.Mutex
	results []map[string]any
}{}

func (s *apiServer) benchLatest(w http.ResponseWriter, _ *http.Request) {
	benchResults.mu.Lock()
	defer benchResults.mu.Unlock()
	writeJSON(w, benchResults.results)
}

func (s *apiServer) benchUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Summary map[string]map[string]any `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := make([]map[string]any, 0, len(body.Summary))
	for provider, s := range body.Summary {
		entry := map[string]any{"provider": provider}
		for k, v := range s {
			entry[k] = v
		}
		out = append(out, entry)
	}
	benchResults.mu.Lock()
	benchResults.results = out
	benchResults.mu.Unlock()
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *apiServer) reviewsList(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("status") == "pending" {
		writeJSON(w, s.reviews.Pending())
		return
	}
	writeJSON(w, s.reviews.List())
}

func (s *apiServer) reviewGet(w http.ResponseWriter, r *http.Request) {
	req, ok := s.reviews.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, req)
}

func (s *apiServer) reviewDecide(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ReviewerID   string `json:"reviewer_id"`
		Decision     string `json:"decision"`
		Reason       string `json:"reason"`
		Modification string `json:"modification"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// hitl.Queue.Decide takes verbs ("approve", "approve_with_modifications",
	// "reject"). The SPA's ReviewDecision type uses the past-tense forms that
	// match medical.ReviewStatus constants. Translate so both wire formats
	// work — bodhctl can send the verb form and the SPA can send the status
	// form interchangeably.
	decision := body.Decision
	switch decision {
	case "approved":
		decision = "approve"
	case "approved_with_modifications":
		decision = "approve_with_modifications"
	case "rejected":
		decision = "reject"
	}
	decided, ok := s.reviews.Decide(r.PathValue("id"), body.ReviewerID, decision, body.Reason, body.Modification)
	if !ok {
		http.Error(w, "not found or invalid decision", http.StatusNotFound)
		return
	}
	if decided.Status == medical.ReviewApproved || decided.Status == medical.ReviewModified {
		payload := decided.Payload
		if decided.Status == medical.ReviewModified && decided.Modification != "" {
			payload = decided.Modification
		}
		meta := map[string]any{}
		for k, v := range decided.Meta {
			if !strings.HasPrefix(k, "review.") {
				meta[k] = v
			}
		}
		s.bus.Publish(r.Context(), agent.NewMessage(
			"human_review", decided.NextTo, agent.RoleAgent, decided.NextType, payload, meta,
		))
	}
	writeJSON(w, decided)
}
