// testhelpers_test.go — shared test helpers for the agents package.
//
// Overview:
//   Provides a minimal silent agent.Environment used by the package's unit
//   tests: a fakeEnv with a fixed clock and an in-memory log capture.
//
// Key declarations:
//   - fakeEnv: minimal Environment implementation for tests.
//   - newFakeEnv: constructor returning a fakeEnv with a fixed time.
//
// See doc.go for package-level documentation.

package agents
