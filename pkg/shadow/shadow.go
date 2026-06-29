// Package shadow implements shadow-mode deployment for the Bodh diagnostic
// pipeline. Shadow mode runs the AI pipeline in parallel with clinician
// decisions, capturing concordance metrics without affecting patient care.
// This is critical for pre-deployment validation: before an AI diagnostician
// goes live, shadow mode proves its agreement rate with real clinician
// decisions on real cases.
//
// Two modes are supported:
//
//   - ModeAsync: fire-and-forget — the AI pipeline runs in a background
//     goroutine and the concordance record is appended to the ledger when
//     it completes. The caller does not block. Use this in production where
//     the clinician workflow must not be delayed by AI inference latency.
//
//   - ModeSync: the AI pipeline runs inline, the concordance comparison
//     happens before the method returns, and the caller receives the
//     ConcordanceRecord immediately. Use this in offline evaluation and
//     CI/CD gating where you need the result before proceeding.
//
// The ConcordanceLedger is an append-only JSONL file that can be replayed,
// aggregated, or shipped to an analytics pipeline. It mirrors the
// append-only audit log philosophy from pkg/audit.
package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
)

// Mode controls whether the shadow pipeline blocks on the AI result or
// runs asynchronously.
type Mode int

const (
	// ModeAsync launches the AI pipeline in a background goroutine and
	// records the concordance result when it completes. RunShadow returns
	// immediately with a nil record.
	ModeAsync Mode = iota

	// ModeSync runs the AI pipeline inline and returns the concordance
	// record to the caller. Use for offline evaluation and CI gating.
	ModeSync
)

// ConcordanceRecord captures a single clinician-vs-AI comparison.
type ConcordanceRecord struct {
	CaseID            string    `json:"case_id"`
	ClinicianDecision string    `json:"clinician_decision"`
	AIDecision        string    `json:"ai_decision"`
	Concordant        bool      `json:"concordant"`
	AIConfidence      float64   `json:"ai_confidence"`
	Timestamp         time.Time `json:"timestamp"`
	LatencyMs         int64     `json:"latency_ms"`
}

// ConcordanceLedger is a thread-safe, append-only store of concordance
// records. When a file is provided, each record is also written as a JSONL
// line for durable, replayable storage.
type ConcordanceLedger struct {
	records []ConcordanceRecord
	mu      sync.RWMutex
	file    *os.File // append-only JSONL; nil when in-memory only
}

// ConcordanceReport aggregates concordance metrics across a set of records.
// The ByCondition field, when present, breaks the report down by medical
// condition (e.g. "pneumonia", "acs", "sepsis").
type ConcordanceReport struct {
	Total           int                          `json:"total"`
	Concordant      int                          `json:"concordant"`
	Discordant      int                          `json:"discordant"`
	ConcordanceRate float64                      `json:"concordance_rate"`
	AvgAIConfidence float64                      `json:"avg_ai_confidence"`
	AvgLatencyMs    float64                      `json:"avg_latency_ms"`
	ByCondition     map[string]ConcordanceReport `json:"by_condition,omitempty"`
}

// ShadowRunner coordinates shadow-mode evaluation: it drives the AI
// pipeline for a given case, compares the AI decision to the clinician's,
// and records the result in the ledger.
type ShadowRunner struct {
	orchestrator *orchestration.Orchestrator
	ledger       *ConcordanceLedger
	mode         Mode
	bus          comm.Bus
	timeout      time.Duration
}

// NewConcordanceLedger creates a ledger. If path is non-empty, records are
// also appended as JSONL to that file. Pass "" for in-memory only.
func NewConcordanceLedger(path string) (*ConcordanceLedger, error) {
	l := &ConcordanceLedger{}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open ledger file %s: %w", path, err)
		}
		l.file = f
	}
	return l, nil
}

// Append adds a record to the in-memory slice and, when a file is
// configured, writes it as a JSONL line. Write errors are logged to stderr
// but do not block — mirroring pkg/audit's fail-open semantics.
func (l *ConcordanceLedger) Append(rec ConcordanceRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
	if l.file != nil {
		b, err := json.Marshal(rec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "shadow ledger: marshal: %v\n", err)
			return
		}
		b = append(b, '\n')
		if _, err := l.file.Write(b); err != nil {
			fmt.Fprintf(os.Stderr, "shadow ledger: write: %v\n", err)
		}
	}
}

// Summarize computes aggregate concordance metrics from all records in the
// ledger.
func (l *ConcordanceLedger) Summarize() ConcordanceReport {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return summarizeRecords(l.records)
}

// Close flushes and closes the underlying file, if any.
func (l *ConcordanceLedger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// summarizeRecords is the pure computation core shared by Summarize and
// Report. It computes totals, rates, and averages from a slice of records.
func summarizeRecords(records []ConcordanceRecord) ConcordanceReport {
	n := len(records)
	if n == 0 {
		return ConcordanceReport{}
	}
	var concordant int
	var confSum, latSum float64
	for _, r := range records {
		if r.Concordant {
			concordant++
		}
		confSum += r.AIConfidence
		latSum += float64(r.LatencyMs)
	}
	nf := float64(n)
	return ConcordanceReport{
		Total:           n,
		Concordant:      concordant,
		Discordant:      n - concordant,
		ConcordanceRate: float64(concordant) / nf,
		AvgAIConfidence: confSum / nf,
		AvgLatencyMs:    latSum / nf,
	}
}

// NewShadowRunner constructs a runner that will drive cases through the
// provided orchestrator and record concordance to the ledger.
//
// The bus parameter must be the same bus wired into the orchestrator so
// the runner can subscribe to the diagnostic_supervisor's terminal
// "diagnosis" messages. timeout controls how long to wait for the AI
// pipeline to finalize a case in ModeSync.
func NewShadowRunner(orch *orchestration.Orchestrator, bus comm.Bus, ledger *ConcordanceLedger, mode Mode, timeout time.Duration) *ShadowRunner {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &ShadowRunner{
		orchestrator: orch,
		bus:          bus,
		ledger:       ledger,
		mode:         mode,
		timeout:      timeout,
	}
}

// RunShadow drives the AI pipeline on caseInput (a JSON-encoded
// PatientPresentation or CaseState), compares the AI's working diagnosis
// to clinicianDecision, and records the concordance.
//
// In ModeSync, the record is returned directly. In ModeAsync, a goroutine
// is launched and RunShadow returns (nil, nil) immediately.
func (r *ShadowRunner) RunShadow(ctx context.Context, caseInput string, clinicianDecision string) (*ConcordanceRecord, error) {
	if r.mode == ModeAsync {
		go r.runAndRecord(context.Background(), caseInput, clinicianDecision)
		return nil, nil
	}
	return r.runAndRecord(ctx, caseInput, clinicianDecision)
}

// Report returns an aggregate ConcordanceReport from all records collected
// so far.
func (r *ShadowRunner) Report() ConcordanceReport {
	return r.ledger.Summarize()
}

// runAndRecord is the inner implementation that drives the pipeline, waits
// for the terminal diagnosis, compares, and appends to the ledger.
func (r *ShadowRunner) runAndRecord(ctx context.Context, caseInput string, clinicianDecision string) (*ConcordanceRecord, error) {
	start := time.Now()

	// Parse the case input to extract case_id and build metadata.
	var pres medical.PatientPresentation
	if err := json.Unmarshal([]byte(caseInput), &pres); err != nil {
		return nil, fmt.Errorf("shadow: parse case input: %w", err)
	}

	// We need a case_id. Try to parse it from a CaseState-shaped input first,
	// fall back to a hash of the chief complaint.
	caseID := extractCaseID(caseInput, pres.ChiefComplaint)

	// Subscribe to the supervisor's terminal diagnosis before publishing.
	done := make(chan medical.CaseState, 1)
	var once sync.Once
	r.bus.Subscribe("diagnostic_supervisor", func(_ context.Context, msg agent.Message) {
		if msg.Type != "diagnosis" {
			return
		}
		state, err := medical.DecodeState(msg.Content)
		if err != nil {
			return
		}
		once.Do(func() { done <- state })
	})

	// Publish the presentation to the intake agent.
	meta := map[string]any{
		"case_id":       caseID,
		"trace_id":      "shadow-" + caseID,
		"message_steps": 11,
	}
	r.bus.Publish(ctx, agent.NewMessage("shadow_runner", "intake", agent.RoleUser,
		"presentation", medical.EncodePresentation(pres), meta))

	// Wait for the pipeline to finalize.
	var state medical.CaseState
	select {
	case state = <-done:
	case <-time.After(r.timeout):
		return nil, fmt.Errorf("shadow: case %s did not finalize within %s", caseID, r.timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	latency := time.Since(start)

	concordant := diagnosisMatch(state.WorkingDiagnosis, clinicianDecision)
	confidence := extractConfidence(state)

	rec := ConcordanceRecord{
		CaseID:            caseID,
		ClinicianDecision: clinicianDecision,
		AIDecision:        state.WorkingDiagnosis,
		Concordant:        concordant,
		AIConfidence:      confidence,
		Timestamp:         time.Now().UTC(),
		LatencyMs:         latency.Milliseconds(),
	}

	r.ledger.Append(rec)
	return &rec, nil
}

// diagnosisMatch compares two diagnoses case-insensitively after trimming
// whitespace. This mirrors pkg/eval/metrics.go diagnosisMatch.
func diagnosisMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// extractCaseID tries to pull a case_id from the JSON input; falls back to
// a simple derived string.
func extractCaseID(input, fallback string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(input), &m); err == nil {
		if id, ok := m["case_id"].(string); ok && id != "" {
			return id
		}
	}
	if fallback != "" {
		// Use a truncated version of the chief complaint as an identifier.
		id := strings.ToLower(strings.ReplaceAll(fallback, " ", "-"))
		if len(id) > 32 {
			id = id[:32]
		}
		return id
	}
	return fmt.Sprintf("shadow-%d", time.Now().UnixNano())
}

// extractConfidence attempts to read a confidence score from the case state's
// hypotheses or a direct confidence field. Returns 0.0 when unavailable.
func extractConfidence(state medical.CaseState) float64 {
	// The rule-based diagnostician doesn't emit confidence, but when a
	// working diagnosis is set with hypotheses, we treat a single
	// hypothesis as high confidence.
	if state.WorkingDiagnosis != "" && len(state.Hypotheses) > 0 {
		return 1.0 / float64(len(state.Hypotheses))
	}
	if state.WorkingDiagnosis != "" {
		return 1.0
	}
	return 0.0
}

// BuildShadowPipeline has moved to the agents package
// (agents.BuildShadowPipeline) so that pkg/shadow stays free of any dependency
// on concrete agent implementations — a prerequisite for the agent-decoupling
// module split (decoupling PR-0.6). The ShadowRunner and ConcordanceLedger in
// this package operate purely on the orchestration.Orchestrator + comm.Bus
// abstractions; callers build the pipeline with agents.BuildShadowPipeline and
// pass the result to NewShadowRunner.
