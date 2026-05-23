// recorder_test.go — unit tests for the audit recorders and event constructors.
//
// Overview:
//   Verifies that the in-memory recorder captures message and policy-decision
//   events with metadata correlators populated, that the FileStore round-trips
//   JSON-line records (stamping IDs and timestamps), and that FromReviewDecision
//   produces a correctly-typed review event.
//
// Key declarations:
//   - TestInMemoryRecord: covers FromMessage / FromPolicyDecision into InMemory.
//   - TestFileStoreRoundTrip: persists three events and re-reads them.
//   - TestFromReviewDecision: validates HITL review event construction.
//
// See doc.go for package-level documentation.

package audit
