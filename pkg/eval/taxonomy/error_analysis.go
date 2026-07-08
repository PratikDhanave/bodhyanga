package taxonomy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OpenCode is a single annotation produced during the "open coding" phase of
// error analysis (Chapter 3, Shankar & Husain). The analyst reviews a trace
// and records a freeform observation plus a binary pass/fail judgement.
type OpenCode struct {
	TraceID      string    `json:"trace_id"`
	Observation  string    `json:"observation"`
	FirstFailure bool      `json:"first_failure"`
	Acceptable   bool      `json:"acceptable"`
	Timestamp    time.Time `json:"timestamp"`
}

// AxialCode is produced during the "axial coding" phase: similar open codes
// are grouped under a named category with a formal definition. Each axial
// code may link to a FailureMode ID in the taxonomy.
type AxialCode struct {
	Name        string   `json:"name"`
	Definition  string   `json:"definition"`
	Examples    []string `json:"examples"`
	TraceIDs    []string `json:"trace_ids"`
	FailureMode string   `json:"failure_mode,omitempty"`
}

// AnalysisStats summarises an ErrorAnalysisSession for dashboards.
type AnalysisStats struct {
	TotalOpenCodes    int     `json:"total_open_codes"`
	TotalAxialCodes   int     `json:"total_axial_codes"`
	AcceptableCount   int     `json:"acceptable_count"`
	UnacceptableCount int     `json:"unacceptable_count"`
	AcceptableRate    float64 `json:"acceptable_rate"`
	FirstFailureCount int     `json:"first_failure_count"`
	Saturated         bool    `json:"saturated"`
}

// ErrorAnalysisSession holds all state for one error-analysis pass. It
// supports both open coding (freeform annotation) and axial coding (grouping
// observations into named categories).
type ErrorAnalysisSession struct {
	ID         string      `json:"id"`
	OpenCodes  []OpenCode  `json:"open_codes"`
	AxialCodes []AxialCode `json:"axial_codes"`
	Saturated  bool        `json:"saturated"`
	CreatedAt  time.Time   `json:"created_at"`
}

// NewErrorAnalysisSession creates a new session with a random UUID and the
// current timestamp.
func NewErrorAnalysisSession() *ErrorAnalysisSession {
	return &ErrorAnalysisSession{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
}

// AddOpenCode appends an open-coding observation for a single trace. The
// timestamp is set to the current time. If this is the first unacceptable
// code in the session, FirstFailure is set to true automatically.
func (s *ErrorAnalysisSession) AddOpenCode(traceID, observation string, acceptable bool) {
	firstFailure := false
	if !acceptable && !s.hasUnacceptable() {
		firstFailure = true
	}

	s.OpenCodes = append(s.OpenCodes, OpenCode{
		TraceID:      traceID,
		Observation:  observation,
		FirstFailure: firstFailure,
		Acceptable:   acceptable,
		Timestamp:    time.Now(),
	})
}

// hasUnacceptable returns true if any existing open code is unacceptable.
func (s *ErrorAnalysisSession) hasUnacceptable() bool {
	for _, oc := range s.OpenCodes {
		if !oc.Acceptable {
			return true
		}
	}
	return false
}

// GroupIntoAxialCodes clusters unacceptable open codes by observation
// similarity (exact substring matching on lowercased text). Each cluster
// becomes an AxialCode. The resulting axial codes are stored on the session
// and also returned.
//
// This is a simple heuristic suitable for small-to-medium trace sets. For
// larger corpora an LLM-assisted clustering step would replace this.
func (s *ErrorAnalysisSession) GroupIntoAxialCodes() []AxialCode {
	// Collect only failing codes.
	var failing []OpenCode
	for _, oc := range s.OpenCodes {
		if !oc.Acceptable {
			failing = append(failing, oc)
		}
	}

	if len(failing) == 0 {
		s.AxialCodes = nil
		return nil
	}

	// Group by normalised observation prefix (first 40 chars, lowered).
	type group struct {
		canon    string
		traces   []string
		examples []string
	}
	groups := make(map[string]*group)
	var order []string // preserve insertion order

	for _, oc := range failing {
		key := normaliseKey(oc.Observation)
		g, ok := groups[key]
		if !ok {
			g = &group{canon: oc.Observation}
			groups[key] = g
			order = append(order, key)
		}
		g.traces = append(g.traces, oc.TraceID)
		if len(g.examples) < 3 {
			g.examples = append(g.examples, oc.Observation)
		}
	}

	axial := make([]AxialCode, 0, len(groups))
	for _, key := range order {
		g := groups[key]
		axial = append(axial, AxialCode{
			Name:       g.canon,
			Definition: fmt.Sprintf("Group of %d failing traces with similar observations.", len(g.traces)),
			Examples:   g.examples,
			TraceIDs:   g.traces,
		})
	}

	s.AxialCodes = axial
	return axial
}

// normaliseKey produces a grouping key from an observation string. It
// lowercases, trims whitespace, and truncates to at most 40 characters.
func normaliseKey(obs string) string {
	s := strings.TrimSpace(strings.ToLower(obs))
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// CheckSaturation returns true when no new axial code categories have
// appeared in the last window open codes. This signals theoretical
// saturation: additional annotations are unlikely to reveal new failure
// modes.
//
// The method also sets s.Saturated as a side effect.
func (s *ErrorAnalysisSession) CheckSaturation(window int) bool {
	if window <= 0 {
		window = 10
	}
	if len(s.OpenCodes) < window {
		s.Saturated = false
		return false
	}

	// Build a set of normalised keys from all codes before the window.
	tail := len(s.OpenCodes) - window
	knownKeys := make(map[string]bool)
	for _, oc := range s.OpenCodes[:tail] {
		if !oc.Acceptable {
			knownKeys[normaliseKey(oc.Observation)] = true
		}
	}

	// Check whether any code in the window introduces a new key.
	for _, oc := range s.OpenCodes[tail:] {
		if !oc.Acceptable {
			key := normaliseKey(oc.Observation)
			if !knownKeys[key] {
				s.Saturated = false
				return false
			}
		}
	}

	s.Saturated = true
	return true
}

// sessionFile is the on-disk JSON representation.
type sessionFile struct {
	Version int                   `json:"version"`
	Session *ErrorAnalysisSession `json:"session"`
}

// ExportJSON writes the session to a JSON file. The parent directory is
// created if it does not exist.
func (s *ErrorAnalysisSession) ExportJSON(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("taxonomy: mkdir %s: %w", filepath.Dir(path), err)
	}
	sf := sessionFile{Version: 1, Session: s}
	b, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("taxonomy: marshal session: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("taxonomy: write %s: %w", path, err)
	}
	return nil
}

// ImportJSON reads a previously exported session from a JSON file,
// replacing the current session state.
func (s *ErrorAnalysisSession) ImportJSON(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("taxonomy: read %s: %w", path, err)
	}
	var sf sessionFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return fmt.Errorf("taxonomy: parse %s: %w", path, err)
	}
	if sf.Session == nil {
		return fmt.Errorf("taxonomy: %s contains no session data", path)
	}
	*s = *sf.Session
	return nil
}

// Stats returns aggregate counts for the session.
func (s *ErrorAnalysisSession) Stats() AnalysisStats {
	st := AnalysisStats{
		TotalOpenCodes:  len(s.OpenCodes),
		TotalAxialCodes: len(s.AxialCodes),
		Saturated:       s.Saturated,
	}
	for _, oc := range s.OpenCodes {
		if oc.Acceptable {
			st.AcceptableCount++
		} else {
			st.UnacceptableCount++
		}
		if oc.FirstFailure {
			st.FirstFailureCount++
		}
	}
	if st.TotalOpenCodes > 0 {
		st.AcceptableRate = float64(st.AcceptableCount) / float64(st.TotalOpenCodes)
	}
	return st
}
