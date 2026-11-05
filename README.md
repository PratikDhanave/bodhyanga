<div align="center">

# Bodhyanga

**A governed multi-agent platform for medical sequential diagnosis and value-based care — in Go.**

[![CI](https://github.com/PratikDhanave/bodhyanga/actions/workflows/ci.yml/badge.svg)](https://github.com/PratikDhanave/bodhyanga/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Architecture: MARA](https://img.shields.io/badge/architecture-MARA-6E4AFF)](https://microsoft.github.io/multi-agent-reference-architecture/)
[![Status: research reference](https://img.shields.io/badge/status-research%20reference-orange)](#-research-use-only)

*Bodhyanga* (Sanskrit *bodhyaṅga* — factors of awakening, insight) implements Microsoft's
[Multi-Agent Reference Architecture (MARA)](https://microsoft.github.io/multi-agent-reference-architecture/)
as a virtual physician panel — inspired by [MAI-DxO](https://microsoft.ai/news/the-path-to-medical-superintelligence/)
and the Sequential Diagnosis Benchmark — plus a care-services layer, all on one in-process message bus.

</div>

---

## ⚠️ Research use only

**Bodhyanga is a research and engineering reference. It is not a medical device and is not approved for clinical use.**

- Do **not** use it for real patient care, triage, treatment, or billing.
- Agents use **simulated / rule-based** inference; all clinical thresholds are **illustrative**, not guidelines.
- HIPAA, FDA, and ONC alignment described in the docs is **architectural guidance** — the code is not certified, audited, or under a Business Associate Agreement.
- All bundled patient data is **synthetic**.

---

## Why Bodhyanga

- **MARA, done properly** — agents never call each other; they emit typed `Message`s routed by a central orchestrator and gated by governance policies *before* any agent sees them.
- **Cost-aware diagnosis** — every test carries a virtual price; a Cost Guardian enforces a per-case budget, reproducing the MAI-DxO accuracy-vs-cost tradeoff.
- **Fail-closed human oversight** — a HITL gate holds clinical decisions for clinician approval; nothing auto-finalizes past it.
- **Auditable by construction** — every hop is an append-only audit event; telemetry carries `sha256(caseID)`, never raw PHI.
- **LLM-agnostic** — one `llm.Client` interface; swap Anthropic, OpenAI, Ollama, or an in-process llama.cpp provider at startup, with rule-based fallback so a case always finalizes.
- **Stdlib-first** — HTTP API, WebSocket HITL hub, and an embedded Vue/React clinician console with no heavyweight dependencies in the default build.

## Architecture

Clients → orchestrator → in-process bus → specialized agents, gated by governance, HITL, and an append-only audit log — one Go binary.

<div align="center">
  <a href="docs/diagrams/platform.architecture.html"><img src="docs/diagrams/platform.architecture.svg" alt="Bodhyanga platform architecture (MARA)" width="820"/></a>
</div>

> A message is published → the orchestrator routes it to the right agent → the agent returns zero or more messages → governance gates each hop → the workflow continues until a terminal agent decides it is done.

**14 interactive diagrams** (pan/zoom, light/dark) cover every flow — see the [**architecture gallery**](docs/architecture-diagrams.md) and [`docs/architecture.md`](docs/architecture.md).

## Quick start

**Clinician console, zero external dependencies** (deterministic rule engine — no API key, no model):

```bash
git clone https://github.com/PratikDhanave/bodhyanga.git && cd bodhyanga
make care DIAGNOSTICIAN=rule      # HTTP API + embedded SPA on :8088
open http://localhost:8088/ui/    # log in: clinician@hospital.org / clinic123
```

Open **Case runner**, pick the `demo-case-001` fixture, and watch the diagnostic pipeline stream live.

> Demo credentials are seeded only for this research build (with a startup warning); set `BODHYANGA_DEMO_USERS=0` to seed none. **Go 1.26** is the only hard requirement; Docker is needed only for the container-backed test suite. Full walkthrough: [`docs/setup.md`](docs/setup.md).

Other entry points:

```bash
make demo                         # sequential-diagnosis walkthrough (MAI-DxO / SD-Bench style)
make caredemo                     # care services: CDM, TCM, panel, virtual office
make bench                        # SD-Bench runner — accuracy / cost table across fixtures
make test                         # full suite (spins up Postgres via testcontainers)
docker compose up                 # full local stack
```

## Features

| Area | What's in the box |
|---|---|
| **Diagnostic panel** | 17+ agents: intake → questioner → test planner → cost guardian → diagnostician → reasoning verifier, plus specialist agents |
| **Care services** | Chronic-disease & transitional-care management, panel management, virtual office (pre-visit prep, inbox triage, refills, prior auth), engagement, readmission tracking |
| **Governance** | OPA Rego policies, per-tenant rate limits, break-glass, 42 CFR Part 2 consent, an AgentMesh kill-switch + prompt-injection gate |
| **Oversight & audit** | Fail-closed HITL queue with a real-time WebSocket hub; append-only, PHI-free, Merkle-chained audit log |
| **Interoperability** | FHIR R4, HL7 v2 (ADT/ORU/ORM), CDS Hooks, SMART on FHIR, FHIR Bulk `$export` |
| **LLM & RAG** | Provider-agnostic client (Anthropic / OpenAI / Ollama / kronk), hybrid BM25 + vector RAG, LLM-as-judge evaluation, cost accounting |
| **Ops** | Prometheus `/metrics`, OpenTelemetry / Laminar tracing, `/healthz` + `/readyz`, Postgres RLS multi-tenancy, Helm chart |

Rule-provider bench accuracy is **85.7%** across the synthetic SD-Bench fixtures.

## Documentation

| Guide | |
|---|---|
| [`docs/setup.md`](docs/setup.md) | Install, run, configure |
| [`docs/architecture.md`](docs/architecture.md) · [gallery](docs/architecture-diagrams.md) | MARA design + 14 interactive diagrams |
| [`docs/api.md`](docs/api.md) | HTTP API reference (`cmd/care`) |
| [`docs/agents.md`](docs/agents.md) · [`docs/data-model.md`](docs/data-model.md) | Agent registry + clinical message/domain model |
| [`docs/governance.md`](docs/governance.md) · [`docs/evaluation.md`](docs/evaluation.md) | Policies & safety · SD-Bench evaluation |
| [`docs/healthcare-standards.md`](docs/healthcare-standards.md) · [`docs/compliance.md`](docs/compliance.md) | FHIR/HL7/CDS · HIPAA & regulatory posture |
| [`docs/operations.md`](docs/operations.md) · [`docs/deployment.md`](docs/deployment.md) | Observability, audit · Docker/K8s |
| [**`MANUAL.md`**](MANUAL.md) | The complete single-page reference (everything, in depth) |

## Repository layout

```
cmd/        CLI binaries — care (HTTP API + SPA), demo, caredemo, bench, evals, …
pkg/        36 library packages — agent, orchestration, comm (bus), governance,
            audit, hitl, llm, knowledge (RAG), fhir, hl7v2, phi, observability, …
agents/     agent definitions (diagnostic + care services)
data/       synthetic case fixtures + knowledge corpus
db/         SQL migrations          docs/   guides + diagrams
zarf/       Docker Compose + Helm/Kubernetes manifests
maf/        nested module: the MARA pipeline reimplemented on Microsoft Agent Framework (Go)
```

## Contributing

Contributions are welcome — see [`CONTRIBUTING.md`](CONTRIBUTING.md) for the workflow, design rules, and how to add an agent, policy, or condition. CI enforces `go vet`, `go test -race`, an 85% coverage gate on `pkg/`, `govulncheck`, and the frontend build. Please also read the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

Report vulnerabilities via GitHub's private advisory flow — see [`SECURITY.md`](SECURITY.md). API keys belong only in a gitignored `.env`; never commit secrets or real PHI.

## License

[Apache License 2.0](LICENSE) — see [`NOTICE`](NOTICE). Changes are tracked in [`CHANGELOG.md`](CHANGELOG.md).

> **Not a medical device. Not clinical software. Research and education only.**
