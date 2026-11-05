// logging.go — minimal Logger and Clock abstractions used across the system.
//
// Overview:
//   Provides tiny indirection points so agents and orchestrators don't import
//   the standard log package or call time.Now directly, making both testable
//   and replaceable (structured logging, frozen clocks, redacting wrappers).
//
// Key declarations:
//   - Logger: Printf-style interface satisfied by std-log and PHI-redacting wrappers.
//   - StdLogger / NewStdLogger: stdout logger with timestamp and "[bodhyanga]" prefix.
//   - NewLoggerTo: constructs a StdLogger backed by an arbitrary io.Writer (for tests).
//   - Clock: Now() abstraction for deterministic tests.
//   - SystemClock: real-time implementation returning time.Now().UTC().
//
// See doc.go for package-level documentation.

package observability
