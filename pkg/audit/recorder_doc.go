// recorder.go — append-only audit log used for HIPAA / 21 CFR Part 11 traceability.
//
// Overview:
//   Defines the Event row written for every message, governance decision, and
//   HITL review decision, and provides both an in-memory recorder for tests
//   and a JSON-lines file store for durable runs. Helpers build Events from
//   the upstream protocol/governance/HITL types without leaking PHI from
//   message content (only metadata correlators are captured).
//
// Key declarations:
//   - Kind / KindMessage, KindPolicyDecision, KindReviewDecision, KindAgentError: event categories.
//   - Event: structured audit row (ids, agent, decision, reason, tenant, trace, etc.).
//   - Recorder: Record + List interface implemented by both storage backends.
//   - InMemory / NewInMemory: slice-backed recorder for tests.
//   - FileStore / OpenFile / FileStore.Close: append-only JSON-lines file backend.
//   - FromMessage, FromPolicyDecision, FromReviewDecision: Event constructors.
//
// See doc.go for package-level documentation.

package audit
