// Package runtime is the per-agent message loop: the thin glue that lets a
// single agent run in its own process while enforcing the identical
// governance + audit boundary the monolith enforces in pkg/orchestration.
//
// Why this exists (agent decoupling, Phase 1)
//
// In the monolith, orchestrator.Start subscribes every agent on the bus and
// wraps each in one closure that runs per message. That closure does five
// things (see pkg/orchestration/orchestrator.go):
//
//  1. Governance evaluate/deny        ─┐ extracted into gateway.Pipeline.Admit
//  2. Audit the agent invocation      ─┘ (task 1.2 — already shipped)
//  3. agent.HandleMessage             ─┐ THIS package
//  4. Eval-mirror side-channel         │ NOT reproduced — see the gap note below
//  5. Fan-out: publish agent outputs  ─┘ THIS package
//
// When each agent becomes a separate binary there is no shared orchestrator to
// run that closure. Runtime.handle reassembles it around ONE agent: it calls
// gateway.Pipeline.Admit (steps 1–2), then — only if admitted —
// agent.HandleMessage (step 3), audits any handler error via
// Pipeline.RecordAgentError, and republishes the outputs (step 5).
//
// The eval-mirror gap (step 4)
//
// Step 4 is NOT reproduced here, and this is a known fidelity gap, not a benign
// omission. In the monolith the eval mirror is per-agent terminal-hop logic
// keyed on exactly one agent: when diagnostic_supervisor finalizes a case
// (returns no output for a type="diagnosis" message), orchestrator.Start writes
// one eval.InteractionRecord via recordCaseEval (orchestrator.go ~:340). There
// is no deployment-wide broadcast subscriber that does this today. A
// diagnostic_supervisor hosted by this Runtime would therefore stop writing
// eval records, and GET /explain/{case_id} — which derives case status from the
// presence of that record (cmd/care/explain.go) — would report finalized cases
// as "pending" forever, with empty metrics.
//
// Closing this BEFORE the supervisor is decoupled means one of: (a) give the
// supervisor's Runtime an injected eval.Store and reproduce recordCaseEval on
// its terminal hop, or (b) build a standalone terminal-diagnosis subscriber
// (Bus.Subscribe on a broadcast/"diagnosis" feed) and wire it once per
// deployment. Until then, only agents that do not drive the eval mirror
// (i.e. every agent except diagnostic_supervisor) are safe to host on Runtime
// with full fidelity.
//
// Fidelity contract: a decoupled agent hosted by Runtime behaves exactly like
// the same agent inside orchestrator.Start for the PER-MESSAGE boundary — the
// ordering (admit before handle, audit-invoke before handle, fan-out after
// handle) and the fail semantics (governance fail-closed, audit fail-open, PHI
// never copied into an audit event) are all inherited from gateway.Pipeline.
// The one deliberate exception is the eval mirror (step 4), documented above.
//
// What Runtime does NOT own: it never calls Bus.Subscribe for peer agents (each
// agent process subscribes only to its own ID), never re-runs governance on
// outbound messages (the NEXT agent's Runtime admits them on receipt, exactly
// as the monolith governs on receive, not on publish), and never recovers
// handler panics itself — pkg/comm.safeHandle already does, and on the durable
// bus that recovery is what drives Ack vs. redelivery.
package runtime
