// redact_test.go — unit tests for the PHI redactor and logging wrapper.
//
// Overview:
//   Confirms the default pattern set redacts SSN, MRN, phone, email, ISO and
//   US date formats while leaving clean text untouched, and that the
//   LoggerWrapper rewrites both the format string and any string-typed
//   arguments before delegating to the inner logger.
//
// Key declarations:
//   - TestRedactorDefaultPatterns: per-pattern coverage table for DefaultPatterns.
//   - TestLoggerWrapperRedactsArgsAndFormat: verifies args and format string are redacted before logging.
//
// See doc.go for package-level documentation.

package phi
