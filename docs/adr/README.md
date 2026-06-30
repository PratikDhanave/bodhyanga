# Architecture Decision Records

This directory records significant architecture decisions for Bodh, in
[MADR](https://adr.github.io/)-style Markdown. Each ADR is immutable once
Accepted; revisiting a decision means a new ADR that supersedes the old one.

## Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [0001](0001-llm-abstraction-layer.md) | LLM Abstraction Layer | Accepted | 2024-12-15 |
| [0002](0002-audit-chain-hash.md) | Chain-Hash Audit Trail for HIPAA Compliance | Accepted | 2024-12-20 |
| [0003](0003-build-tags-optional-features.md) | Go Build Tags for Optional Features | Accepted | 2024-12-22 |
| [0004](0004-mara-pipeline-architecture.md) | Multi-Agent Reference Architecture (MARA) Pipeline | Accepted | 2024-12-18 |
| [0005](0005-phi-deidentification.md) | PHI De-identification via SHA-256 Truncated Hashes | Accepted | 2024-12-25 |
| [0006](0006-project-identity.md) | Project identity — research platform vs clinical product | **Needs-Decision** | 2026-06-30 |
| [0007](0007-deployment-topology.md) | Deployment topology — modular monolith → trust-tier, not 24 services | Proposed | 2026-06-30 |
| [0008](0008-substrate-first.md) | Harden the safety substrate before distributing | Needs-Decision | 2026-06-30 |
| [0009](0009-thin-events-system-of-record.md) | Thin events + Postgres system-of-record, not fat CaseState messages | Proposed | 2026-06-30 |
| [0010](0010-evals-as-deploy-gate.md) | Evals as the deploy gate; unify eval/shadow/graduation | Needs-Decision | 2026-06-30 |
| [0011](0011-build-and-model-governance.md) | Every build config in CI; pin and eval-gate models and prompts | Accepted | 2026-06-30 |
| [0012](0012-autopilot-is-a-regulated-change.md) | Autonomous clinical operation (autopilot) is a regulated change, fenced | Accepted | 2026-06-30 |

## Two eras

- **0001–0005 (2024):** the founding decisions — the LLM abstraction, the
  chain-hash audit design, build tags, the MARA pipeline, PHI hashing.
- **0006–0012 (2026):** the principal-engineer architecture review, authored
  while decoupling the monolith. These are grounded in the current code with
  `file:line` references and are mostly *Proposed* / *Needs-Decision* — they are
  the agenda for the next phase, not yet settled.

## How the 2026 review relates to the founding decisions

- **0006 (identity) gates everything.** 0007, 0008, 0009, 0010, and 0012 each
  resolve differently depending on whether Bodh is a research platform or a
  clinical product. Decide 0006 first.
- **0008 surfaces a drift from 0002.** ADR-0002 (Accepted) makes
  `ImmutableRecorder` the production chain-hash audit sink, but the production
  path never wired it (it uses a plain Postgres `INSERT`). 0008 proposes closing
  that gap — either re-affirm 0002 or supersede it.
- **0011 extends 0003.** ADR-0003 introduced build tags; 0011 adds the guardrail
  that every shipping tag config must be compiled in CI (the `nats` tag had
  silently rotted), plus model/prompt governance. Partially implemented:
  `make build-tags`.
- **0012 builds on 0004/graduation.** The MARA pipeline (0004) plus the autonomy
  ladder leads to `autopilot`; 0012 fences it as a regulated change. Implemented:
  `Criteria.AllowAutopilot` (default false) in `pkg/graduation`.

## Status legend

- **Accepted** — decided and in force (or implemented).
- **Proposed** — recommended direction, awaiting owner acceptance.
- **Needs-Decision** — a genuine fork the owner must choose; the ADR frames the
  options rather than pre-deciding.
