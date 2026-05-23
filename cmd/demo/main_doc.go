// main.go — entry point for the sequential-diagnosis demo (MAI-DxO style).
//
// Overview:
//   Wires up the diagnostic agents (intake through reasoning_verifier) onto
//   an in-memory bus and registry, publishes a case (default pneumonia or
//   from -case JSON), and sleeps briefly for the pipeline to finalize.
//
// Key declarations:
//   - main: parses flags, builds the orchestrator, and publishes the case.
//   - loadCase: loads a case fixture from JSON or returns the default.
//   - pneumoniaDefault / pneumoniaMeta: built-in demo case + metadata.
//
// See doc.go for package-level documentation.

package main
