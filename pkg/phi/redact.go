// Package phi provides best-effort PHI redaction for logs, events, and other
// outbound channels that should not carry Protected Health Information.
//
// This is NOT a replacement for a real Data Loss Prevention service. It is the
// inexpensive "first line" — catch obvious identifiers in free-text before they
// reach stdout, a metric backend, or a third-party log sink. For regulated
// production deployments, layer this in front of a DLP service (Google Cloud
// DLP, AWS Macie, Microsoft Presidio, etc.). Those services do contextual
// analysis (NER + transformer-based classifiers) that catches identifiers
// pure regex misses: free-form patient names, addresses in narrative,
// indirectly-identifying date ranges. This package only catches the regex-
// shaped lowest-hanging fruit, on the assumption that anything richer needs
// a separate process boundary anyway.
//
// Defence-in-depth layers (in order of preference):
//  1. Don't put PHI in logs or audit events in the first place — see
//     pkg/audit's deliberate field selection (docs/governance.md).
//  2. Redact at the logger seam (LoggerWrapper below).
//  3. Run a real DLP service downstream of stdout collection.
//  4. Restrict who can read the log sink via access control.
//
// This package is layer 2 — the cheap one that runs in-process per write.
package phi

import (
	"regexp"

	// observability is imported only for the Logger interface; LoggerWrapper
	// is a decorator around any observability.Logger implementation.
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

// Pattern is one named regex with its replacement.
//
// Name is used for debugging and for the future "which pattern matched at
// which offset" telemetry. It is NOT used in the output — the output uses
// Replace verbatim. Keep Name lowercase-snake; some downstream dashboards
// group by it.
type Pattern struct {
	Name    string
	Re      *regexp.Regexp
	Replace string
}

// Redactor applies a set of patterns to a string.
//
// Patterns is exported so callers can append their own (org-specific MRN
// shapes, payer claim IDs, internal ticket numbers) via Add. Order matters:
// patterns are applied in slice order, so a custom pattern that overlaps a
// default should be inserted before the default to win.
type Redactor struct {
	Patterns []Pattern
}

// DefaultPatterns returns a starter set covering the most common HIPAA Safe
// Harbor identifiers (45 CFR §164.514(b)(2)) that leak as free text. The 18
// Safe Harbor identifiers are the regulatory checklist; this set covers the
// shapes most likely to appear in clinical free-text. Production deployments
// should append site-specific shapes (internal MRN format, payer claim IDs,
// custom employee IDs) via Redactor.Add — see docs/governance.md.
//
// Trade-offs explicit:
//
//   - FALSE NEGATIVES are accepted (this is why a real DLP must run downstream
//     in regulated production). A patient's name in narrative is not caught
//     here. A vanity-format MRN like "Patient ID 12345" without the literal
//     "MRN" prefix is not caught here. The Safe Harbor framing
//     ("scrub these shapes") is intentionally narrower than the full
//     Limited Data Set, let alone full PHI semantics.
//
//   - FALSE POSITIVES are tolerated because the cost is a redacted log line,
//     not a clinical impact. An order number that happens to match the SSN
//     shape (`123-45-6789`) gets redacted; acceptable.
func DefaultPatterns() []Pattern {
	return []Pattern{
		// Safe Harbor identifier (i): Social Security Number. Strict
		// hyphenated form; unhyphenated 9-digit blocks are not redacted
		// because the false-positive rate (medical codes, batch IDs) would
		// be too high. If a payer source emits raw SSNs, add a custom
		// pattern at the integration seam.
		{Name: "ssn", Re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), Replace: "[REDACTED-SSN]"},

		// Safe Harbor identifier (g): Medical Record Number. We require
		// the literal "MRN" prefix (case-insensitive) because MRN shapes
		// vary wildly per site (numeric, alphanumeric, hyphenated). The
		// prefix is the only thing reliably present in free text written
		// by humans. Site-specific MRN patterns (e.g. "EPIC-12345678")
		// should be added via Redactor.Add.
		{Name: "mrn", Re: regexp.MustCompile(`(?i)\bMRN[:\s]*[A-Z0-9-]{4,}\b`), Replace: "[REDACTED-MRN]"},

		// Safe Harbor identifier (d): Telephone number. US 10-digit form
		// with hyphen / dot / space separators. Does not match raw 10-digit
		// blocks or international formats (+44, etc.) — false negatives
		// accepted for the false-positive trade-off, see file doc.
		{Name: "phone", Re: regexp.MustCompile(`\b\d{3}[-.\s]\d{3}[-.\s]\d{4}\b`), Replace: "[REDACTED-PHONE]"},

		// Safe Harbor identifier (f): Email address. RFC 5321 is broader
		// than this regex, but the regex catches >99% of real addresses
		// while staying readable. TLD ≥ 2 chars guards against matching
		// "user@server" (internal hostnames).
		{Name: "email", Re: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), Replace: "[REDACTED-EMAIL]"},

		// Safe Harbor identifier (c): Dates more specific than year. ISO
		// 8601 form (YYYY-MM-DD). The (19|20) prefix restricts to plausible
		// human dates and avoids matching CPT-style codes that happen to
		// share the shape. Note: the regulation actually allows year alone,
		// so redacting full ISO dates to "[REDACTED-DATE]" is conservative
		// — a stricter implementation could preserve the year portion.
		{Name: "iso_date", Re: regexp.MustCompile(`\b(19|20)\d{2}-\d{2}-\d{2}\b`), Replace: "[REDACTED-DATE]"},

		// Safe Harbor identifier (c): US-style date M/D/YYYY or MM/DD/YYYY.
		// Same conservative replacement as iso_date. Caveat: this will
		// match the harmless string "8/10/2024" but won't match European
		// "10.08.2024" (intentional — that form is rare in US-deployed
		// clinical text and would create false positives with version
		// strings like "2.1.2024").
		{Name: "us_date", Re: regexp.MustCompile(`\b\d{1,2}/\d{1,2}/(19|20)\d{2}\b`), Replace: "[REDACTED-DATE]"},
	}
}

// NewRedactor returns a Redactor with the default pattern set.
func NewRedactor() *Redactor {
	return &Redactor{Patterns: DefaultPatterns()}
}

// Redact returns s with every matching pattern replaced by its sentinel.
//
// Patterns run in slice order. A pattern can match in text already partly
// rewritten by an earlier pattern (e.g. an email containing an MRN-shaped
// substring). The Replace strings are designed to be inert under all
// patterns (no digits, no @, no MRN prefix), so once redacted, no further
// pattern fires on the sentinel itself.
func (r *Redactor) Redact(s string) string {
	out := s
	for _, p := range r.Patterns {
		out = p.Re.ReplaceAllString(out, p.Replace)
	}
	return out
}

// Add appends a custom pattern (e.g. an internal-MRN format).
//
// Appended patterns run AFTER the defaults. To make a custom pattern win
// against a default (e.g. site-specific MRN that overlaps the generic MRN
// shape), modify Patterns directly with a prepend. The slice is exported
// to keep this option open without growing the API surface.
func (r *Redactor) Add(p Pattern) {
	r.Patterns = append(r.Patterns, p)
}

// LoggerWrapper implements observability.Logger by redacting before each write.
// Use it in front of the std logger when shipping logs to any backend that
// might be queried by people who don't have a need-to-know for PHI.
//
// WHY THIS WRAPPER EXISTS at all (vs. trusting callers to pre-redact):
// pre-redaction by every call site is a thin moat that erodes over time.
// One Logf("patient %s presented with...", patientName) in a freshly merged
// agent leaks the name to every downstream log consumer until someone
// notices. Putting redaction at the seam (the Logger interface itself)
// means the leak window is bounded by however long it takes Redactor to
// see the shape, regardless of how many call sites exist.
//
// Wire this in main.go / cmd/care/main.go:
//
//	rawLogger := observability.NewStdLogger("bodhyanga")
//	logger    := &phi.LoggerWrapper{Redactor: phi.NewRedactor(), Inner: rawLogger}
//	orch.SetLogger(logger)
type LoggerWrapper struct {
	// Redactor performs the per-write redaction. Replaceable so callers
	// can pre-load site-specific patterns. Nil disables the wrapper
	// (Printf becomes a no-op — see fail-closed note in Printf).
	Redactor *Redactor
	// Inner is the underlying logger that receives redacted output. Any
	// observability.Logger implementation works (std logger, slog adapter,
	// in-memory test logger).
	Inner observability.Logger
}

// Printf redacts every string-typed argument and the format string itself,
// then delegates to the wrapped logger.
//
// Why redact the FORMAT STRING too: format strings sometimes contain literal
// PHI when a developer writes Printf("treating MRN-12345 with...", ...) by
// mistake. Catching the format string at this seam closes that loophole.
//
// Why only STRING-typed args: numeric args (counts, costs, IDs in
// non-string types) cannot match the regex patterns, which all anchor on
// digit/letter/symbol boundaries that wouldn't appear after Sprintf
// formatting of an int. Structs and slices that fmt.Stringer their way
// into the output via %v are NOT redacted by this implementation — that's
// a known limitation. If structured payloads start leaking, the right fix
// is to redact the formatted result post-Sprintf, not here. We deliberately
// did not do that because Inner.Printf must still receive a format+args
// pair (some implementations defer formatting to slog/zap and don't want
// pre-rendered strings).
//
// FAIL-CLOSED on nil: if either Redactor or Inner is nil this becomes a
// no-op rather than crashing or leaking through unredacted. Logs go missing
// (loud operationally) instead of leaking PHI (silently catastrophic).
func (w *LoggerWrapper) Printf(format string, v ...any) {
	if w.Redactor == nil || w.Inner == nil {
		return
	}
	// Build a parallel args slice so we don't mutate the caller's variadic.
	// Reflect-free type switch on string is cheap; non-strings pass through
	// untouched.
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

// Compile-time assertion that LoggerWrapper satisfies the Logger interface.
// If observability.Logger ever grows a method (e.g. Debug, structured
// fields), this line will fail to compile until LoggerWrapper is updated —
// preventing a silent regression where new logger methods bypass redaction.
var _ observability.Logger = (*LoggerWrapper)(nil)
