# ADR 0007: Deployment topology — modular monolith → trust-tier split, not 24 per-agent services

**Status:** Proposed

**Date:** 2026-06-30

**Deciders:** project owner (Pratik Dhanave)

> Authored from the principal-engineer architecture review (2026). Gated by
> ADR-0006 (project identity). Grounded in `specs/002-agent-decoupling/plan.md`
> and the Phase-0 work already on `feat/agent-decoupling-phase0`.

---

## Context

The in-flight decoupling plan (`specs/002-agent-decoupling/plan.md`) splits the
`cmd/care` monolith into **~24 per-agent binaries** communicating over NATS
JetStream, plus a `cmd/api` backend with "zero agent code." The work so far has
delivered two distinct kinds of change that the plan conflates:

1. **Modularization (done, valuable):** interface extraction
   (`pkg/medical/store.go` `PatientStore`, `pkg/tasks/store.go` `TaskStore`,
   `pkg/hitl/store.go` `ReviewStore`), `hitl.BuildResumeMessage` so the backend
   resumes HITL without importing `agents`, Postgres adapters for the stores, the
   removal of every `pkg/`→`agents` import edge (the agent-free `pkg/` milestone),
   and `pkg/gateway` (a per-agent governance+audit admission pipeline). This is
   "turn the monolith into a modular monolith with clean seams." It is correct
   regardless of the final topology and is reversible.

2. **Physical distribution (the expensive bet):** ~24 separate binaries / images /
   Deployments / Secrets / NetworkPolicies over a NATS mesh.

The justification for #2 in the plan is thinner than it appears. At low / research
volume, 24 services impose a large operational tax for benefits that do not
materialize:

- NATS becomes a new single point of failure requiring HA (clustering, PDB,
  anti-affinity). The durable bus is not even proven yet — its integration test
  hangs (see ADR-0008).
- 24 images to build, scan, sign, store, and patch; per-agent dashboards, SLOs,
  alerts, and on-call surface.
- Schema versioning across independently-deployed binaries (fat `CaseState` JSON
  on the wire — see ADR-0009).
- The HITL flow becomes a **non-transactional saga**: the backend does
  `ReviewStore.Decide()` (a Postgres commit) and then publishes a resume message
  (NATS) with no atomicity — a crash between them strands a clinical case.

The *one* benefit that genuinely justifies a split in a PHI context is the
**security boundary**: the public HTTP surface (`cmd/api`) must contain no agent
code and must not hold the `ANTHROPIC_API_KEY`-reading symbol. That is real and
defensible. But it argues for a small number of **trust tiers**, not per-agent
granularity — there is no security reason to isolate the cardiologist agent from
the nephrologist agent (both are stateless, secret-free, byte-identical in their
trust profile).

## Decision drivers

- Operability must match team size (Conway's law): a ~1–few-person team cannot
  carry 24 services' worth of on-call and deploy choreography.
- The security boundary (no agent code / no secrets in the public backend) is the
  only hard driver for a split, and it is satisfied at trust-tier granularity.
- The modularization work creates seams cheaply, so splitting *later* is cheap —
  which means splitting *now*, maximally, is premature optimization.
- Patient-safety and operational cost both favor the smallest topology that meets
  the security boundary and any real scaling need.

## Options considered

### A. Pure per-agent: ~24 binaries (the current plan)

- **Pros:** Finest isolation; each agent independently deployable/scalable; the
  intake binary literally cannot read the diagnostician's API key.
- **Cons:** 24× operational fan-out; NATS HA SPOF; schema-versioning and
  saga complexity; topology far larger than the team can operate; isolation finer
  than the threat model requires.

### B. Trust-tier split: 3–4 services (recommended)

- **Pros:** Satisfies the security boundary (public backend / stateless-rule
  agents / secret-holding LLM agents / state-mutating agents). Drastically lower
  operational surface than A. Groups by trust + resource profile, which is what
  actually differs. Composes cleanly with the gateway pipeline already built.
- **Cons:** Re-introduces shared blast radius *within* a tier (one panic kills the
  tier); tier boundaries are a judgment call that can drift; less independent
  scaling than A (rarely needed at current volume).

### C. Stay a modular monolith; split nothing physically yet

- **Pros:** Lowest operational cost; the seam work still pays off (testability,
  clarity); zero distributed-systems risk. Best fit for a small team / research
  identity (ADR-0006 Option A or C).
- **Cons:** Does not achieve the security boundary (the backend still links agent
  code and secrets); does not enable independent scaling if it is ever needed.

## Decision

**Proposed: bank the modularization (continue the Phase-0 seam work), then split
to trust tiers (Option B), not per-agent (Option A).** If ADR-0006 lands on the
research path, prefer Option C (modular monolith) until a concrete scaling or
security need is demonstrated. In all cases, re-decide the *physical* split with a
real case-volume / cost model rather than treating "24 microservices" as the
default. The interface and gateway work means this re-decision is cheap to make
later.

## Consequences

### Positive

- Achieves the security boundary at a fraction of the operational cost of 24 services.
- Matches topology to team capacity; avoids a Conway's-law mismatch.
- Preserves all the modularization value already shipped.

### Negative

- Reverses a previously-chosen direction (the per-agent-binary decision in the plan),
  which requires updating `specs/002-agent-decoupling/plan.md`.
- Trust-tier grouping must be defined and held; it is less mechanically obvious than
  "one binary per agent."

### Risks

- If the project later genuinely needs per-agent scaling (high volume, one hot
  agent), trust-tier grouping must be re-split — but that is the cheap direction
  thanks to the seams.
- Choosing C and never achieving the security boundary leaves agent code + secrets
  in the public backend; acceptable only under the research identity.

## References

- `specs/002-agent-decoupling/plan.md` — the current per-agent (24-service) plan
- `pkg/medical/store.go`, `pkg/tasks/store.go`, `pkg/hitl/store.go` — the extracted seams
- `pkg/gateway/` — per-agent admission pipeline (composes with any topology)
- `pkg/hitl/resume.go` (`BuildResumeMessage`) — the HITL saga touchpoint
- ADR-0006 (identity, gating), ADR-0008 (substrate-first), ADR-0009 (thin events)
