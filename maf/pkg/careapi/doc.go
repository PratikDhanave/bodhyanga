// Package careapi is the HTTP surface over the MARA pipeline Runner (specs/004).
// It wraps pkg/pipeline.Runner in a net/http handler: submit a case, poll its
// status/result, list and answer physician reviews (durable across restart),
// and read a case's audit trail.
//
// Contract:
//
//   - Wrap the Runner, don't reimplement it. Handlers call Runner.Run /
//     Runner.Resume and translate Outcomes into a PHI-free registry the reads
//     serve from. No handler re-implements pipeline logic.
//   - PHI-free responses (non-negotiable). Every response body and registry
//     Entry carries only opaque fingerprints (case token, session id) and
//     clinical decision-support output (diagnosis, plan steps) — never a raw
//     case id, patient name, symptom, or narrative. The audit read filters
//     bodhyanga's trail, which is already PHI-free.
//   - Auth is a documented non-goal for this slice (ADR-0006 parity): the
//     server binds loopback by default and prints an auth-gap banner. A real
//     deployment fronts it with authenticated ingress.
//   - Durability comes from the Runner's checkpoint store, not this package.
//     With -db, a paused case is durably checkpointed and resumable by session
//     id across a process restart (proven by restart_test.go). KNOWN GAP: the
//     running cmd/care binary does NOT yet auto-rebuild the in-memory
//     pending-review LIST after a restart — RecoverPending (recover.go) is the
//     mechanism, but wiring it at boot needs durable session-summary
//     persistence (the API's `started` set is in-memory only). Until then, a
//     restart leaves a durably-checkpointed review unlisted by GET /v1/reviews.
//     This is a documented follow-up, not a claim the binary currently meets.
package careapi
