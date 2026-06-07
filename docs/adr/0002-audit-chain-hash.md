# ADR 0002: Chain-Hash Audit Trail for HIPAA Compliance

**Status:** Accepted

**Date:** 2024-12-20

**Deciders:** Pratik Dhanave

---

## Context

HIPAA Technical Safeguard SS 164.312(b) requires "hardware, software, and
procedural mechanisms that record and examine activity in information systems
that contain or use electronic protected health information." 21 CFR Part 11
SS 11.10(e) further requires that audit trails detect retroactive modifications
for FDA-regulated clinical trial deployments.

Bodh's MARA pipeline generates a stream of auditable events: every agent
invocation, every governance policy decision (allow/deny), every HITL review
decision, and every agent error. These events are the forensic record an
investigator reaches for during HIPAA SS 164.404 breach-scope analysis.

Plain append-only logging (JSONL to a file) satisfies the "record" requirement
but not the "examine" requirement with tamper evidence. An attacker or
malicious insider who gains write access to the log file could silently delete
or modify entries without detection. Regulatory auditors expect a mechanism
that makes such tampering detectable.

The platform needs an audit solution that:

1. Makes any retroactive modification to a historical entry detectable.
2. Works with append-only file I/O (no database required for the primary sink).
3. Does not block the clinical message pipeline on audit writes.
4. Supports export to a WORM (Write-Once-Read-Many) sink for long-term retention.

## Decision

Implement a Merkle chain-hash audit trail in `pkg/audit/immutable.go`. Each
`ChainedEvent` carries three fields beyond the base `Event`:

```go
type ChainedEvent struct {
    Event    Event  `json:"event"`
    PrevHash string `json:"prev_hash"`  // hex SHA-256 of previous entry's Hash
    Hash     string `json:"hash"`       // SHA-256(prev_hash + "\n" + json(Event))
    Seq      int64  `json:"seq"`        // monotonic 0-based sequence number
}
```

The hash chain construction is:

```
Hash[0] = SHA-256(genesis + "\n" + json(Event[0]))
Hash[n] = SHA-256(Hash[n-1] + "\n" + json(Event[n]))
```

where `genesis` is a well-known 64-character all-zeros hex string so verifiers
can reconstruct the chain from scratch without external state.

Key implementation details:

- **`ImmutableRecorder`** wraps any inner `Recorder` (typically `InMemory` for
  the `/audit` HTTP endpoint) with the chain-hash file. Every `Record` call
  stamps the event, computes the hash, appends a JSONL line, advances the
  chain state, and optionally forwards to the inner recorder.

- **Chain restoration on restart.** `NewImmutableRecorder` reads the last line
  of the existing JSONL file to restore `prevHash` and `seq`, so the chain
  continues across process restarts without a gap.

- **`VerifyChain` method.** Reads the file end-to-end, recomputes every hash
  from the stored `Event` JSON, and uses `hmac.Equal` for constant-time
  comparison. Returns the sequence number of the first broken link, or
  `(true, 0, nil)` when the chain is intact.

- **File I/O is serialized via mutex.** The write path holds `mu` from hash
  computation through `f.Write`, ensuring that concurrent goroutines (the
  orchestrator dispatches messages on separate goroutines) never interleave
  JSONL line boundaries.

- **File mode is `0o600` (owner read/write only).** The audit file contains
  tenant IDs and case IDs that are sensitive correlation handles. Group/other
  access would violate SS 164.312(a)(1) access control on the audit artifact.

- **S3 export is fire-and-forget.** When an `S3Sink` is configured, each
  event is uploaded as a gzipped JSONL object in a background goroutine with
  a 30-second timeout. S3 upload failure never blocks the clinical pipeline.
  Bulk export (`ExportToS3`) uploads a range of events as a single object.

- **AWS SigV4 is implemented in stdlib.** The S3 upload uses `net/http` with
  hand-computed AWS Signature Version 4 headers. No AWS SDK dependency is
  pulled into the module, keeping the default binary small.

### Audit event schema

The `Event` struct is deliberately minimal. It carries routing metadata
(AgentID, MessageID, From, To, Type) and four correlation keys (TenantID,
PatientID, CaseID, TraceID). Message content (clinical narrative, test
results, diagnosis text, medication names) is **never** copied into an Event.
This is the project's expression of HIPAA's minimum-necessary principle
(45 CFR SS 164.502(b)) applied to the audit pipeline itself.

The `pickStrings` helper enforces a whitelist of exactly four metadata keys
when copying from a `protocol.Message`. Adding a new key to the whitelist is
a governance decision documented in `docs/governance.md`.

### Recorder implementations

| Implementation | Use case | Durability |
|---|---|---|
| `InMemory` | Tests, demos, the `/audit` HTTP endpoint | Process lifetime only |
| `FileStore` | First-hop local buffer | Survives restart; not WORM |
| `ImmutableRecorder` | Production chain-hash sink | Tamper-evident; exports to S3 WORM |

## Consequences

### Positive

- **Tamper detection is O(n) and offline.** `VerifyChain` requires only the
  JSONL file; no database, no external service. An investigator can verify
  chain integrity on a forensic workstation with no network access.

- **HIPAA SS 164.312(b) and 21 CFR Part 11 SS 11.10(e) are satisfied.** The
  chain hash makes retroactive modification of any historical entry
  detectable: changing one entry breaks the hash of every subsequent entry.

- **No external dependencies for the primary sink.** The chain-hash file is
  pure Go stdlib (crypto/sha256, encoding/json, os). The S3 export is
  optional and uses only stdlib net/http.

- **Fail-open semantics preserve clinical flow.** Audit write failures are
  logged via `env.Logf` and never block message processing. The rationale:
  audit-sink unavailability is operational; blocking clinical flow because
  the audit failed is the worst outcome.

### Negative

- **Sequential writes are required.** The mutex serializes all audit writes
  within a process. Under extreme throughput (thousands of agent hops per
  second), the audit write path could become a bottleneck. Mitigated by
  keeping the per-write cost small (JSON marshal + SHA-256 + file append)
  and by the fact that clinical pipelines are inherently sequential per case.

- **Chain breaks on ungraceful shutdown.** A SIGKILL or power loss may lose
  the tail of unflushed events (the write is not fsync'd per event for
  throughput reasons). The downstream S3 forwarder provides at-least-once
  delivery for the durable copy. A torn write leaves at most one corrupt
  JSONL line, which `VerifyChain` reports and subsequent lines continue from.

- **Verification is O(n) in file size.** For very large audit files (millions
  of events), `VerifyChain` reads the entire file. Production deployments
  mitigate this by rotating the file (logrotate/fluentbit) and verifying
  each segment independently.

### Neutral

- The `InMemory` recorder remains available for tests and demos. It does not
  participate in the chain hash. The chain is exclusively an
  `ImmutableRecorder` concern.

## References

- `pkg/audit/immutable.go` -- ImmutableRecorder, ChainedEvent, VerifyChain
- `pkg/audit/recorder.go` -- Recorder interface, InMemory, FileStore, Event schema
- `pkg/audit/audit_kinds.go` -- KindMessage, KindPolicyDecision, KindReviewDecision, KindAgentError
- `db/migrations/0002_rls.sql` -- Postgres RLS for audit_events table
- `db/migrations/0007_encrypted_columns.sql` -- Encrypted audit columns
