package agents

import "time"

// fakeEnv is the minimal agent.Environment used by agent unit tests.
// It is silent (logs are dropped) and returns a fixed time for determinism.
type fakeEnv struct {
	now  time.Time
	logs []string
}

func newFakeEnv() *fakeEnv {
	return &fakeEnv{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
}

func (e *fakeEnv) Now() time.Time { return e.now }
func (e *fakeEnv) Logf(format string, args ...any) {
	// Capture for tests that want to assert on log content; otherwise ignored.
	e.logs = append(e.logs, format)
}
