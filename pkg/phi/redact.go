// Package phi provides best-effort PHI redaction for logs, events, and other
// outbound channels that should not carry Protected Health Information.
//
// This is NOT a replacement for a real Data Loss Prevention service. It is the
// inexpensive "first line" — catch obvious identifiers in free-text before they
// reach stdout, a metric backend, or a third-party log sink. For regulated
// production deployments, layer this in front of a DLP service (Google Cloud
// DLP, AWS Macie, Presidio, etc.).
package phi

import (
	"regexp"

	"github.com/PratikDhanave/bodh/pkg/observability"
)

// Pattern is one named regex with its replacement.
type Pattern struct {
	Name    string
	Re      *regexp.Regexp
	Replace string
}

// Redactor applies a set of patterns to a string.
type Redactor struct {
	Patterns []Pattern
}

// DefaultPatterns returns a starter set covering the most common HIPAA Safe
// Harbor identifiers that leak as free text: SSN, MRN, phone, email, dates.
// Add more in production based on the field shapes you actually see.
func DefaultPatterns() []Pattern {
	return []Pattern{
		{Name: "ssn", Re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), Replace: "[REDACTED-SSN]"},
		{Name: "mrn", Re: regexp.MustCompile(`(?i)\bMRN[:\s]*[A-Z0-9-]{4,}\b`), Replace: "[REDACTED-MRN]"},
		{Name: "phone", Re: regexp.MustCompile(`\b\d{3}[-.\s]\d{3}[-.\s]\d{4}\b`), Replace: "[REDACTED-PHONE]"},
		{Name: "email", Re: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), Replace: "[REDACTED-EMAIL]"},
		{Name: "iso_date", Re: regexp.MustCompile(`\b(19|20)\d{2}-\d{2}-\d{2}\b`), Replace: "[REDACTED-DATE]"},
		{Name: "us_date", Re: regexp.MustCompile(`\b\d{1,2}/\d{1,2}/(19|20)\d{2}\b`), Replace: "[REDACTED-DATE]"},
	}
}

// NewRedactor returns a Redactor with the default pattern set.
func NewRedactor() *Redactor {
	return &Redactor{Patterns: DefaultPatterns()}
}

// Redact returns s with every matching pattern replaced by its sentinel.
func (r *Redactor) Redact(s string) string {
	out := s
	for _, p := range r.Patterns {
		out = p.Re.ReplaceAllString(out, p.Replace)
	}
	return out
}

// Add appends a custom pattern (e.g. an internal-MRN format).
func (r *Redactor) Add(p Pattern) {
	r.Patterns = append(r.Patterns, p)
}

// LoggerWrapper implements observability.Logger by redacting before each write.
// Use it in front of the std logger when shipping logs to any backend that
// might be queried by people who don't have a need-to-know for PHI.
type LoggerWrapper struct {
	Redactor *Redactor
	Inner    observability.Logger
}

// Printf redacts every string-typed argument and the format string itself,
// then delegates to the wrapped logger.
func (w *LoggerWrapper) Printf(format string, v ...any) {
	if w.Redactor == nil || w.Inner == nil {
		return
	}
	args := make([]any, len(v))
	for i, a := range v {
		if s, ok := a.(string); ok {
			args[i] = w.Redactor.Redact(s)
		} else {
			args[i] = a
		}
	}
	w.Inner.Printf(w.Redactor.Redact(format), args...)
}

var _ observability.Logger = (*LoggerWrapper)(nil)
