// Package guardrails implements synchronous pre-output checks that run before
// LLM-generated output reaches the user. This is the "last mile" safety layer
// in the MARA pipeline: after an agent produces output but before it is
// returned to the caller, every registered guardrail runs in sequence.
//
// Guardrails are cheap, deterministic checks (regex, blocklist, JSON parse)
// rather than LLM-based classifiers. They complete in microseconds per check,
// making them suitable for synchronous insertion in the hot path. For heavier
// classifiers (toxicity models, DLP services), run them asynchronously and
// use the ActionLog action to avoid blocking the response.
//
// The pipeline returns the first failing check and a recommended Action:
//   - ActionReject: return a safe fallback message instead of the LLM output.
//   - ActionRetry:  retry the LLM call once (useful for malformed JSON/SQL).
//   - ActionLog:    log the violation but allow the output through.
//
// Design rationale from "Evals for AI Engineers" Chapter 9: guardrails are
// not evals. Evals measure quality offline; guardrails enforce safety online.
// A guardrail that blocks too aggressively degrades the product; one that
// blocks too rarely is a liability. Tune thresholds per deployment.
package guardrails

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// CheckResult is the outcome of a single guardrail check.
type CheckResult struct {
	Safe    bool          `json:"safe"`
	Reason  string        `json:"reason,omitempty"`
	Rule    string        `json:"rule"`
	Latency time.Duration `json:"latency"`
}

// Guardrail is the interface every check must implement. Name returns a
// stable identifier used in metrics and logging. Check inspects the output
// and returns a CheckResult.
type Guardrail interface {
	Name() string
	Check(ctx context.Context, output string) CheckResult
}

// Action is the recommended response when a guardrail fires.
type Action string

const (
	// ActionReject means the output should be replaced with a safe fallback.
	ActionReject Action = "reject"
	// ActionRetry means the LLM call should be retried once.
	ActionRetry Action = "retry"
	// ActionLog means the violation should be logged but the output allowed.
	ActionLog Action = "log"
)

// PipelineStats tracks aggregate guardrail metrics. All fields are safe for
// concurrent reads via Pipeline.Stats(), which holds a read lock.
type PipelineStats struct {
	TotalChecks  int64            `json:"total_checks"`
	TotalBlocked int64            `json:"total_blocked"`
	ByRule       map[string]int64 `json:"by_rule"`
	AvgLatency   time.Duration    `json:"avg_latency"`
}

// ---------------------------------------------------------------------------
// Pipeline
// ---------------------------------------------------------------------------

// Pipeline runs a sequence of guardrails against LLM output. Guards are
// evaluated in registration order; the first failure short-circuits.
type Pipeline struct {
	guards []Guardrail
	// actions maps guardrail Name() to the Action taken on failure.
	actions map[string]Action
	mu      sync.RWMutex
	stats   PipelineStats
	// totalLatencyNs accumulates nanoseconds for averaging.
	totalLatencyNs int64
}

// NewPipeline creates a pipeline with the given guardrails. Each guardrail
// is paired with a default action via the actionFor helper. To override
// the action for a specific guardrail, use SetAction after construction.
func NewPipeline(guards ...Guardrail) *Pipeline {
	actions := make(map[string]Action, len(guards))
	for _, g := range guards {
		actions[g.Name()] = actionFor(g)
	}
	return &Pipeline{
		guards:  guards,
		actions: actions,
		stats: PipelineStats{
			ByRule: make(map[string]int64),
		},
	}
}

// SetAction overrides the action for a named guardrail.
func (p *Pipeline) SetAction(name string, action Action) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.actions[name] = action
}

// Run executes every guardrail in order against output. It returns the first
// failing CheckResult and the associated Action. If all guardrails pass, it
// returns a safe result with an empty Action.
func (p *Pipeline) Run(ctx context.Context, output string) (CheckResult, Action) {
	start := time.Now()

	for _, g := range p.guards {
		result := g.Check(ctx, output)
		if !result.Safe {
			p.recordCheck(result.Rule, true, time.Since(start))
			action := p.actionForName(result.Rule)
			return result, action
		}
	}

	elapsed := time.Since(start)
	p.recordCheck("", false, elapsed)
	return CheckResult{
		Safe:    true,
		Rule:    "",
		Latency: elapsed,
	}, ""
}

// Stats returns a snapshot of pipeline statistics.
func (p *Pipeline) Stats() PipelineStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// Deep-copy ByRule to avoid races on the caller side.
	byRule := make(map[string]int64, len(p.stats.ByRule))
	for k, v := range p.stats.ByRule {
		byRule[k] = v
	}
	return PipelineStats{
		TotalChecks:  p.stats.TotalChecks,
		TotalBlocked: p.stats.TotalBlocked,
		ByRule:       byRule,
		AvgLatency:   p.stats.AvgLatency,
	}
}

func (p *Pipeline) recordCheck(rule string, blocked bool, latency time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.TotalChecks++
	if blocked {
		p.stats.TotalBlocked++
		p.stats.ByRule[rule]++
	}
	totalNs := atomic.AddInt64(&p.totalLatencyNs, latency.Nanoseconds())
	p.stats.AvgLatency = time.Duration(totalNs / p.stats.TotalChecks)
}

func (p *Pipeline) actionForName(name string) Action {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if a, ok := p.actions[name]; ok {
		return a
	}
	return ActionReject
}

// actionFor returns the default action for a guardrail based on its type.
func actionFor(g Guardrail) Action {
	switch g.(type) {
	case *SQLSyntaxGuardrail, *JSONValidityGuardrail:
		return ActionRetry
	case *LengthGuardrail, *MedicalDisclaimerGuardrail:
		return ActionLog
	default:
		return ActionReject
	}
}

// ---------------------------------------------------------------------------
// PIIGuardrail
// ---------------------------------------------------------------------------

// PIIGuardrail checks for PII/PHI patterns in output: SSN, credit card,
// phone, email, and MRN. These patterns are intentionally strict to avoid
// false positives on clinical codes and numeric identifiers.
type PIIGuardrail struct{}

// piiPatterns are compiled once at init time. Each entry is a named regex
// for a specific PII type. The patterns mirror the Safe Harbor identifiers
// from 45 CFR 164.514(b)(2), consistent with pkg/phi.DefaultPatterns().
var piiPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{"credit_card", regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`)},
	{"phone", regexp.MustCompile(`\b\d{3}[-.\s]\d{3}[-.\s]\d{4}\b`)},
	{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)},
	{"mrn", regexp.MustCompile(`(?i)\bMRN[:\s]*[A-Z0-9\-]{4,}\b`)},
}

func (g *PIIGuardrail) Name() string { return "pii_check" }

func (g *PIIGuardrail) Check(_ context.Context, output string) CheckResult {
	start := time.Now()
	for _, p := range piiPatterns {
		if p.re.MatchString(output) {
			return CheckResult{
				Safe:    false,
				Reason:  "detected " + p.name + " pattern in output",
				Rule:    g.Name(),
				Latency: time.Since(start),
			}
		}
	}
	return CheckResult{Safe: true, Rule: g.Name(), Latency: time.Since(start)}
}

// ---------------------------------------------------------------------------
// SQLInjectionGuardrail
// ---------------------------------------------------------------------------

// SQLInjectionGuardrail checks generated SQL for injection patterns.
// This guards against prompt-injection attacks that trick an LLM into
// producing destructive SQL.
type SQLInjectionGuardrail struct{}

var sqlInjectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bDROP\s+(TABLE|DATABASE|INDEX|VIEW)\b`),
	regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`),
	regexp.MustCompile(`(?i)\bUNION\s+(ALL\s+)?SELECT\b`),
	regexp.MustCompile(`(?i);\s*(DROP|DELETE|INSERT|UPDATE|ALTER|TRUNCATE)\b`),
	regexp.MustCompile(`--\s*$`),
	regexp.MustCompile(`(?i)\bOR\s+1\s*=\s*1\b`),
	regexp.MustCompile(`(?i)\bEXEC(UTE)?\s+`),
	regexp.MustCompile(`(?i)\bxp_cmdshell\b`),
}

func (g *SQLInjectionGuardrail) Name() string { return "sql_injection_check" }

func (g *SQLInjectionGuardrail) Check(_ context.Context, output string) CheckResult {
	start := time.Now()
	for _, p := range sqlInjectionPatterns {
		if p.MatchString(output) {
			return CheckResult{
				Safe:    false,
				Reason:  "SQL injection pattern detected: " + p.String(),
				Rule:    g.Name(),
				Latency: time.Since(start),
			}
		}
	}
	return CheckResult{Safe: true, Rule: g.Name(), Latency: time.Since(start)}
}

// ---------------------------------------------------------------------------
// SQLSyntaxGuardrail
// ---------------------------------------------------------------------------

// SQLSyntaxGuardrail validates that generated SQL has a recognized statement
// structure. This is a basic structural check, not a full parser. It ensures
// the output starts with a valid SQL keyword and has the expected clauses.
type SQLSyntaxGuardrail struct{}

var validSQLPrefixes = []string{
	"SELECT ", "INSERT ", "UPDATE ", "CREATE ", "ALTER ", "WITH ",
}

func (g *SQLSyntaxGuardrail) Name() string { return "sql_syntax_check" }

func (g *SQLSyntaxGuardrail) Check(_ context.Context, output string) CheckResult {
	start := time.Now()
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return CheckResult{
			Safe:    false,
			Reason:  "empty SQL output",
			Rule:    g.Name(),
			Latency: time.Since(start),
		}
	}
	upper := strings.ToUpper(trimmed)
	valid := false
	for _, prefix := range validSQLPrefixes {
		if strings.HasPrefix(upper, prefix) {
			valid = true
			break
		}
	}
	if !valid {
		return CheckResult{
			Safe:    false,
			Reason:  "SQL does not start with a recognized statement keyword",
			Rule:    g.Name(),
			Latency: time.Since(start),
		}
	}
	// Check for balanced parentheses as a basic structural validation.
	depth := 0
	for _, r := range trimmed {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth < 0 {
			return CheckResult{
				Safe:    false,
				Reason:  "unbalanced parentheses in SQL",
				Rule:    g.Name(),
				Latency: time.Since(start),
			}
		}
	}
	if depth != 0 {
		return CheckResult{
			Safe:    false,
			Reason:  "unbalanced parentheses in SQL",
			Rule:    g.Name(),
			Latency: time.Since(start),
		}
	}
	return CheckResult{Safe: true, Rule: g.Name(), Latency: time.Since(start)}
}

// ---------------------------------------------------------------------------
// ToxicityGuardrail
// ---------------------------------------------------------------------------

// ToxicityGuardrail checks for harmful or toxic content using a blocklist.
// This includes slurs and harmful medical advice patterns. In production,
// this should be supplemented with a classifier-based approach.
type ToxicityGuardrail struct{}

// toxicPatterns are lowercased substrings that indicate harmful content.
// These are intentionally broad to err on the side of caution. The list
// focuses on harmful medical advice rather than general toxicity, since
// Bodhyanga is a clinical platform.
var toxicPatterns = []string{
	"kill yourself",
	"commit suicide",
	"stop taking your medication",
	"ignore your doctor",
	"don't go to the hospital",
	"you don't need medical attention",
	"inject bleach",
	"drink bleach",
	"cure cancer with",
	"guaranteed cure",
	"miracle cure",
	"big pharma doesn't want you to know",
	"doctors are lying",
	"vaccines cause autism",
	"homeopathy cures",
}

func (g *ToxicityGuardrail) Name() string { return "toxicity_check" }

func (g *ToxicityGuardrail) Check(_ context.Context, output string) CheckResult {
	start := time.Now()
	lower := strings.ToLower(output)
	for _, p := range toxicPatterns {
		if strings.Contains(lower, p) {
			return CheckResult{
				Safe:    false,
				Reason:  "toxic content detected: matches blocklist pattern",
				Rule:    g.Name(),
				Latency: time.Since(start),
			}
		}
	}
	return CheckResult{Safe: true, Rule: g.Name(), Latency: time.Since(start)}
}

// ---------------------------------------------------------------------------
// LengthGuardrail
// ---------------------------------------------------------------------------

// LengthGuardrail checks that output does not exceed a maximum length.
// Length is measured in Unicode code points (runes), not bytes, to handle
// multi-byte characters correctly. The default maximum is 2000 runes.
type LengthGuardrail struct {
	MaxRunes int
}

// NewLengthGuardrail returns a LengthGuardrail with the given maximum.
// If maxRunes <= 0, the default of 2000 is used.
func NewLengthGuardrail(maxRunes int) *LengthGuardrail {
	if maxRunes <= 0 {
		maxRunes = 2000
	}
	return &LengthGuardrail{MaxRunes: maxRunes}
}

func (g *LengthGuardrail) Name() string { return "length_check" }

func (g *LengthGuardrail) Check(_ context.Context, output string) CheckResult {
	start := time.Now()
	count := utf8.RuneCountInString(output)
	max := g.MaxRunes
	if max <= 0 {
		max = 2000
	}
	if count > max {
		return CheckResult{
			Safe:    false,
			Reason:  "output length " + itoa(count) + " exceeds maximum " + itoa(max),
			Rule:    g.Name(),
			Latency: time.Since(start),
		}
	}
	return CheckResult{Safe: true, Rule: g.Name(), Latency: time.Since(start)}
}

// ---------------------------------------------------------------------------
// JSONValidityGuardrail
// ---------------------------------------------------------------------------

// JSONValidityGuardrail validates that output is well-formed JSON. This is
// useful for structured output pipelines where the downstream consumer
// expects valid JSON. On failure, ActionRetry is recommended so the LLM
// can regenerate the response.
type JSONValidityGuardrail struct{}

func (g *JSONValidityGuardrail) Name() string { return "json_validity_check" }

func (g *JSONValidityGuardrail) Check(_ context.Context, output string) CheckResult {
	start := time.Now()
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return CheckResult{
			Safe:    false,
			Reason:  "empty output is not valid JSON",
			Rule:    g.Name(),
			Latency: time.Since(start),
		}
	}
	var js json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &js); err != nil {
		return CheckResult{
			Safe:    false,
			Reason:  "invalid JSON: " + err.Error(),
			Rule:    g.Name(),
			Latency: time.Since(start),
		}
	}
	return CheckResult{Safe: true, Rule: g.Name(), Latency: time.Since(start)}
}

// ---------------------------------------------------------------------------
// MedicalDisclaimerGuardrail
// ---------------------------------------------------------------------------

// MedicalDisclaimerGuardrail ensures that outputs containing diagnostic
// language include a research/not-for-clinical-use disclaimer. This is
// critical for HIPAA-grade platforms where LLM output must never be
// mistaken for validated clinical guidance.
type MedicalDisclaimerGuardrail struct{}

// diagnosisIndicators are lowercased substrings that suggest the output
// contains diagnostic or treatment content requiring a disclaimer.
var diagnosisIndicators = []string{
	"diagnosis",
	"diagnose",
	"treatment plan",
	"prescribe",
	"prescription",
	"recommended treatment",
	"clinical assessment",
	"differential diagnosis",
	"prognosis",
	"medical advice",
}

// disclaimerPhrases are lowercased substrings that satisfy the disclaimer
// requirement. The output must contain at least one of these.
var disclaimerPhrases = []string{
	"not for clinical use",
	"research purposes only",
	"not a substitute for professional medical",
	"consult a qualified healthcare",
	"for informational purposes only",
	"should not be used as a substitute",
	"ai-generated",
	"not intended as medical advice",
	"this is not a medical diagnosis",
}

func (g *MedicalDisclaimerGuardrail) Name() string { return "medical_disclaimer_check" }

func (g *MedicalDisclaimerGuardrail) Check(_ context.Context, output string) CheckResult {
	start := time.Now()
	lower := strings.ToLower(output)

	// First check whether the output contains diagnostic language.
	hasDiagnosis := false
	for _, ind := range diagnosisIndicators {
		if strings.Contains(lower, ind) {
			hasDiagnosis = true
			break
		}
	}
	if !hasDiagnosis {
		// No diagnostic content; disclaimer not required.
		return CheckResult{Safe: true, Rule: g.Name(), Latency: time.Since(start)}
	}

	// Diagnostic content found; check for disclaimer.
	for _, d := range disclaimerPhrases {
		if strings.Contains(lower, d) {
			return CheckResult{Safe: true, Rule: g.Name(), Latency: time.Since(start)}
		}
	}

	return CheckResult{
		Safe:    false,
		Reason:  "output contains diagnostic language but no research/clinical disclaimer",
		Rule:    g.Name(),
		Latency: time.Since(start),
	}
}

// ---------------------------------------------------------------------------
// DefaultPipeline
// ---------------------------------------------------------------------------

// DefaultPipeline returns a pipeline with all built-in guardrails in the
// recommended order: fast regex checks first, then structural validation,
// then content checks.
func DefaultPipeline() *Pipeline {
	return NewPipeline(
		&PIIGuardrail{},
		&SQLInjectionGuardrail{},
		&SQLSyntaxGuardrail{},
		&ToxicityGuardrail{},
		NewLengthGuardrail(2000),
		&JSONValidityGuardrail{},
		&MedicalDisclaimerGuardrail{},
	)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// itoa converts an int to a string without importing fmt or strconv, keeping
// the package dependency-light for hot-path usage. Mirrors the pattern in
// pkg/safety/safety.go.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}
