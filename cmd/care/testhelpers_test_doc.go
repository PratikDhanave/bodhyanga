// testhelpers_test.go — shared helpers for cmd/care unit tests.
//
// Overview:
//   Provides test-only constructors and stubs used by api_test.go and
//   runs_test.go. Keeping these together avoids per-test duplication and
//   gives a single place to evolve the test-side wiring as the production
//   apiServer grows.
//
// Key declarations:
//   - newTestAPIServer: builds an apiServer with in-memory bus/registry/queues
//     and a per-test fixtures TempDir.
//   - stubAgent: a minimal agent.Agent implementation for registry tests.
//   - writeFixture: writes a fixtureFile to disk as demo_<name>.json.
//   - waitForCondition: deadline-bounded polling used in place of time.Sleep.
//
// See main_doc.go for command-level documentation.

package main
