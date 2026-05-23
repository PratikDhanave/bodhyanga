// redact.go — best-effort PHI redaction for logs and outbound channels.
//
// Overview:
//   Provides a regex-based Redactor that strips common HIPAA Safe Harbor
//   identifiers (SSN, MRN, phone, email, dates) from free-text strings, plus
//   a LoggerWrapper that runs every format string and string argument through
//   the redactor before delegating to an inner observability.Logger. Intended
//   as the cheap first line in front of a real DLP service.
//
// Key declarations:
//   - Pattern: named regex + sentinel replacement.
//   - Redactor / NewRedactor: applies a pattern set to a string.
//   - DefaultPatterns: starter set covering SSN, MRN, phone, email, ISO and US dates.
//   - Redactor.Add: register a custom pattern.
//   - LoggerWrapper: observability.Logger that redacts before forwarding.
//
// See doc.go for package-level documentation.

package phi
