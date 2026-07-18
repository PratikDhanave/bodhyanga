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
//     bodh's trail, which is already PHI-free.
//   - Auth is a documented non-goal for this slice (ADR-0006 parity): the
//     server binds loopback by default and prints an auth-gap banner. A real
//     deployment fronts it with authenticated ingress.
//   - Durability comes from the Runner's checkpoint store, not this package.
//     Paused cases survive restart only with -db; the in-memory registry is
//     rebuilt from the store via RecoverPending (recover.go).
package careapi
