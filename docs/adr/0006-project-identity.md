# ADR 0006: Project identity — research platform vs clinical product

**Status:** Needs-Decision

**Date:** 2026-06-30

**Deciders:** project owner (Pratik Dhanave)

> Authored from the principal-engineer architecture review (2026). This is the
> gating decision: ADRs 0007–0012 each resolve differently depending on its
> outcome. It is deliberately left open for the owner to decide.

---

## Context

Bodh currently presents itself as **two different products at once**, and the
codebase honestly reflects both — which is the root cause of much of the
architectural incoherence the other ADRs address.

**Signals that Bodh is a research / teaching / reference artifact:**

- A persistent "RESEARCH DEMO" banner ships in the UI (`cmd/care/web/src/components/DemoBanner.tsx`).
- The default runtime is in-memory and non-durable: `medical.NewPatientRegistry()`,
  `tasks.NewQueue()`, `hitl.NewQueue()` wired in `cmd/care/main.go`; the default
  bus is `comm.InMemoryBus` (`pkg/comm/bus.go:72-106`, explicitly "no FIFO, no
  backpressure, no retries, no persistence — a process crash loses them").
- Source comments repeatedly use phrases like "research demo" to justify
  fail-open / best-effort behaviour.
- The MARA pipeline, the lessons (`cmd/intro`, `cmd/toolcalling`), and the blog
  posts frame the project as a reference implementation of a multi-agent pattern.

**Signals that Bodh is a HIPAA-grade clinical product:**

- A governance / audit / HITL safety substrate (`pkg/governance`, `pkg/audit`,
  `pkg/hitl`) modelled on real regulatory text (HIPAA §164.312(b), 21 CFR Part 11).
- PHI de-identification by SHA-256 hashing (ADR-0005), a chain-hash tamper-evident
  audit design (ADR-0002), and an AIIA template + incident playbook.
- A `pkg/graduation` autonomy ladder whose terminal stage (`autopilot`) encodes an
  AI **autonomously making clinical decisions**.

These two identities have **opposite architectural optima.** A teaching platform
should optimize for clarity, pattern-correctness, and pedagogy, and should *not*
carry the weight of production hardening or regulatory machinery. A clinical
product invokes a Software-as-a-Medical-Device (SaMD) regulatory pathway
(FDA 510(k)/De Novo or an explicit non-device-CDS positioning under the 21st
Century Cures Act), a Quality Management System (ISO 13485), risk management as a
*process* (ISO 14971, not just a template), clinical validation (not just SD-Bench
accuracy), and change control where a model or prompt update is a regulated change.

The incoherence is already visible in the code: ADR-0002 was *Accepted* and
specifies `ImmutableRecorder` as the production chain-hash audit sink, yet it is
wired **nowhere** in `cmd/care` (only in `cmd/audit-verify`); production audit is
a plain `INSERT` (`pkg/persistence/postgres/store.go RecordAuditEvent`) with no
hash chain. A team that had firmly chosen "regulated clinical product" would not
let an accepted compliance ADR sit unimplemented; a team that had chosen "teaching
platform" would not have built the chain-hash recorder at all. The drift IS the
symptom of the undecided identity.

## Decision drivers

- Almost every other open ADR (0007 topology, 0008 substrate, 0009 data model,
  0010 evals, 0012 autopilot) resolves trivially once identity is fixed, and stays
  incoherent until then.
- The cost asymmetry is enormous: the clinical path implies regulatory, QMS,
  validation, and liability obligations measured in person-years; the research path
  implies almost none.
- Operability and team size (see ADR-0007): the clinical path's architecture must
  be operable by whatever team actually exists.
- Honesty to users: shipping clinical-looking software that is actually a research
  demo (or vice versa) is a safety and trust problem in a medical context.

## Options considered

### A. Research / reference platform (teaching artifact)

- **Pros:** Frees the project from SaMD/QMS/validation weight. Optimizes for the
  thing it is already good at (a clear MARA reference). Lets the substrate be
  "illustrative" without claiming production guarantees. Most of the open ADRs
  collapse to "keep it simple, keep it correct as a pattern."
- **Cons:** Must *stop* implying clinical readiness — soften the HIPAA-grade
  claims, keep the demo banner, and explicitly fence anything that looks like it
  acts on real patients. Caps the project's ambition.

### B. Clinical product headed for real patients (SaMD)

- **Pros:** Matches the substrate ambition and the regulatory framing already in
  the code. Gives a clear, defensible north star.
- **Cons:** Triggers the full regulatory pathway, a QMS, clinical validation, and
  change control. The current architecture is far from this bar (fail-open audit,
  non-durable default bus, drift from ADR-0002, no clinical validation, an
  `autopilot` concept that is likely not shippable — see ADR-0012). This is a
  multi-year commitment with real liability.

### C. Staged: research platform now, with a credible, fenced path toward clinical

- **Pros:** Honest about today (research/demo) while keeping the option open.
  Lets the substrate harden (ADR-0008) and the seams form (ADR-0007) without
  prematurely claiming clinical readiness. Autopilot stays fenced (ADR-0012) as a
  research concept until the regulatory path is real.
- **Cons:** Requires discipline to not let "demo" features masquerade as
  production ones. The two identities still coexist for a while, so the team must
  hold the boundary explicitly (e.g. a documented "research-only" gate on anything
  patient-facing).

## Decision

**Deferred to the project owner.** This ADR does not pre-decide identity; it makes
the choice explicit and shows that it gates the rest. The architecture review's
recommendation, if forced, is **Option C (staged)**: treat Bodh as a research /
reference platform *today* — keep the demo banner, fence autonomy, and stop
implying clinical readiness — while using the modularization and substrate work
(ADRs 0007–0009) to build a credible, optional path toward a future regulated
product. That preserves optionality at the lowest present cost and keeps the
project honest about what it currently is.

Record the decision here (update Status to Accepted with the chosen option) before
acting on ADRs 0007, 0008, 0009, 0010, or 0012 — they each cite this one.

## Consequences

### Positive

- Unblocks every downstream ADR with a single decision.
- Forces the implicit, drifting identity into an explicit, reviewable one.

### Negative

- Choosing the clinical path commits to a large, slow, expensive regulatory program.
- Choosing the research path requires walking back some HIPAA-grade marketing/claims.

### Risks

- Leaving this undecided (the status quo) lets the codebase keep drifting — accepted
  ADRs go unimplemented (ADR-0002), and clinical-looking features accrete without the
  validation that would make them safe to use, which is itself a patient-safety risk.

## References

- `cmd/care/web/src/components/DemoBanner.tsx` — the "RESEARCH DEMO" banner
- `cmd/care/main.go` — in-memory registries; `selectBackends` audit wiring
- `pkg/comm/bus.go:72-106` — non-durable default `InMemoryBus`
- `pkg/audit/immutable.go` vs `pkg/persistence/postgres/store.go` (`RecordAuditEvent`) — the drift from ADR-0002
- `pkg/graduation/` — the autonomy ladder (see ADR-0012)
- ADR-0002 (chain-hash audit), ADR-0005 (PHI de-identification) — existing clinical-framing decisions
