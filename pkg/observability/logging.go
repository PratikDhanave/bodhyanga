package observability

import (
	"io"
	"log"
	"os"
	"time"
)

// Logger is a minimal logging abstraction used across the system.
type Logger interface {
	Printf(format string, v ...any)
}

// StdLogger is a thin wrapper around the standard library logger.
type StdLogger struct {
	l *log.Logger
}

// NewStdLogger constructs a logger that writes to stdout with timestamps.
func NewStdLogger() *StdLogger {
	return NewLoggerTo(os.Stdout)
}

// NewLoggerTo constructs a StdLogger that writes to the given writer. Exposed
// primarily for tests that want to capture output into a bytes.Buffer.
func NewLoggerTo(w io.Writer) *StdLogger {
	return &StdLogger{
		l: log.New(w, "[bodh] ", log.LstdFlags|log.Lmicroseconds),
	}
}

// Printf logs a formatted message.
func (s *StdLogger) Printf(format string, v ...any) {
	s.l.Printf(format, v...)
}

// Clock abstracts time for easier testing.
type Clock interface {
	Now() time.Time
}

// SystemClock uses the real system time.
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}
