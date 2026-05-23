package agents

import (
	"sync"
	"time"
)

// fakeEnv is the minimal agent.Environment used by agent unit tests.
// It is silent (logs are dropped) and returns a fixed time for determinism.
// Logf is mutex-protected so concurrent agents (e.g. DebateDiagnostician's
// fan-out goroutines) can call it without racing.
type fakeEnv struct {
	now  time.Time
	mu   sync.Mutex
	logs []string
}

func newFakeEnv() *fakeEnv {
	return &fakeEnv{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
}

func (e *fakeEnv) Now() time.Time { return e.now }
func (e *fakeEnv) Logf(format string, args ...any) {
	// Capture for tests that want to assert on log content; otherwise ignored.
	e.mu.Lock()
	defer e.mu.Unlock()
	e.logs = append(e.logs, format)
}
