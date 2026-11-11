1	<div align="center">
2	
3	# Bodhyanga
4	
5	**A governed multi-agent platform for medical sequential diagnosis and value-based care — in Go.**
6	
7	[![CI](https://github.com/PratikDhanave/bodhyanga/actions/workflows/ci.yml/badge.svg)](https://github.com/PratikDhanave/bodhyanga/actions/workflows/ci.yml)
8	[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
9	[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
10	[![Architecture: MARA](https://img.shields.io/badge/architecture-MARA-6E4AFF)](https://microsoft.github.io/multi-agent-reference-architecture/)
11	[![Status: research reference](https://img.shields.io/badge/status-research%20reference-orange)](#-research-use-only)
12	
13	*Bodhyanga* (Sanskrit *bodhyaṅga* — factors of awakening, insight) implements Microsoft's
14	[Multi-Agent Reference Architecture (MARA)](https://microsoft.github.io/multi-agent-reference-architecture/)
15	as a virtual physician panel — inspired by [MAI-DxO](https://microsoft.ai/news/the-path-to-medical-superintelligence/)
16	and the Sequential Diagnosis Benchmark — plus a care-services layer, all on one in-process message bus.
17	
18	</div>
19	
20	---
21	
22	## ⚠️ Research use only
23	
24	**Bodhyanga is a research and engineering reference. It is not a medical device and is not approved for clinical use.**
25	
26	- Do **not** use it for real patient care, triage, treatment, or billing.
27	- Agents use **simulated / rule-based** inference; all clinical thresholds are **illustrative**, not guidelines.
28	- HIPAA, FDA, and ONC alignment described in the docs is **architectural guidance** — the code is not certified, audited, or under a Business Associate Agreement.
29	- All bundled patient data is **synthetic**.
30	
31	---
32	
33	## Why Bodhyanga
34	
35	- **MARA, done properly** — agents never call each other; they emit typed `Message`s routed by a central orchestrator and gated by governance policies *before* any agent sees them.
36	- **Cost-aware diagnosis** — every test carries a virtual price; a Cost Guardian enforces a per-case budget, reproducing the MAI-DxO accuracy-vs-cost tradeoff.
37	- **Fail-closed human oversight** — a HITL gate holds clinical decisions for clinician approval; nothing auto-finalizes past it.
38	- **Auditable by construction** — every hop is an append-only audit event; telemetry carries `sha256(caseID)`, never raw PHI.
39	- **LLM-agnostic** — one `llm.Client` interface; swap Anthropic, OpenAI, Ollama, or an in-process llama.cpp provider at startup, with rule-based fallback so a case always finalizes.
40	- **Stdlib-first** — HTTP API, WebSocket HITL hub, and an embedded Vue/React clinician console with no heavyweight dependencies in the default build.
41	
42	## Architecture
43	
44	Clients → orchestrator → in-process bus → specialized agents, gated by governance, HITL, and an append-only audit log — one Go binary.
45	
46	<div align="center">
47	  <a href="docs/diagrams/platform.architecture.html"><img src="docs/diagrams/platform.architecture.svg" alt="Bodhyanga platform architecture (MARA)" width="820"/></a>
48	</div>
49	
50	> A message is published → the orchestrator routes it to the right agent → the agent returns zero or more messages → governance gates each hop → the workflow continues until a terminal agent decides it is done.
51	
52	**14 interactive diagrams** (pan/zoom, light/dark) cover every flow — see the [**architecture gallery**](docs/architecture-diagrams.md) and [`docs/architecture.md`](docs/architecture.md).
53	
54	## Quick start
55	
56	**Clinician console, zero external dependencies** (deterministic rule engine — no API key, no model):
57	
58	```bash
59	git clone https://github.com/PratikDhanave/bodhyanga.git && cd bodhyanga
60	make care DIAGNOSTICIAN=rule      # HTTP API + embedded SPA on :8088
61	open http://localhost:8088/ui/    # log in: clinician@hospital.org / clinic123
62	```
63	
64	Open **Case runner**, pick the `demo-case-001` fixture, and watch the diagnostic pipeline stream live.
65	
66	> Demo credentials are seeded only for this research build (with a startup warning); set `BODHYANGA_DEMO_USERS=0` to seed none. **Go 1.27** is the only hard requirement; Docker is needed only for the container-backed test suite. Full walkthrough: [`docs/setup.md`](docs/setup.md).
67	
68	Other entry points:
69	
70	```bash
71	make demo                         # sequential-diagnosis walkthrough (MAI-DxO / SD-Bench style)
72	make caredemo                     # care services: CDM, TCM, panel, virtual office
73	make bench                        # SD-Bench runner — accuracy / cost table across fixtures
74	make test                         # full suite (spins up Postgres via testcontainers)
75	docker compose up                 # full local stack
76	```
77	
78	## Features
79	
80	| Area | What's in the box |
81	|---|---|
82	| **Diagnostic panel** | 17+ agents: intake → questioner → test planner → cost guardian → diagnostician → reasoning verifier, plus specialist agents |
83	| **Care services** | Chronic-disease & transitional-care management, panel management, virtual office (pre-visit prep, inbox triage, refills, prior auth), engagement, readmission tracking |
84	| **Governance** | OPA Rego policies, per-tenant rate limits, break-glass, 42 CFR Part 2 consent, an AgentMesh kill-switch + prompt-injection gate |
85	| **Oversight & audit** | Fail-closed HITL queue with a real-time WebSocket hub; append-only, PHI-free, Merkle-chained audit log |
86	| **Interoperability** | FHIR R4, HL7 v2 (ADT/ORU/ORM), CDS Hooks, SMART on FHIR, FHIR Bulk `$export` |
87	| **LLM & RAG** | Provider-agnostic client (Anthropic / OpenAI / Ollama / kronk), hybrid BM25 + vector RAG, LLM-as-judge evaluation, cost accounting |
88	| **Ops** | Prometheus `/metrics`, OpenTelemetry / Laminar tracing, `/healthz` + `/readyz`, Postgres RLS multi-tenancy, Helm chart |
89	
90	Rule-provider bench accuracy is **100%** across the synthetic SD-Bench fixtures.
91	
92	## Documentation
93	
94	| Guide | |
95	|---|---|
96	| [`docs/setup.md`](docs/setup.md) | Install, run, configure |
97	| [`docs/architecture.md`](docs/architecture.md) · [gallery](docs/architecture-diagrams.md) | MARA design + 14 interactive diagrams |
98	| [`docs/api.md`](docs/api.md) | HTTP API reference (`cmd/care`) |
99	| [`docs/agents.md`](docs/agents.md) · [`docs/data-model.md`](docs/data-model.md) | Agent registry + clinical message/domain model |
100	| [`docs/governance.md`](docs/governance.md) · [`docs/evaluation.md`](docs/evaluation.md) | Policies & safety · SD-Bench evaluation |
101	| [`docs/healthcare-standards.md`](docs/healthcare-standards.md) · [`docs/compliance.md`](docs/compliance.md) | FHIR/HL7/CDS · HIPAA & regulatory posture |
102	| [`docs/operations.md`](docs/operations.md) · [`docs/deployment.md`](docs/deployment.md) | Observability, audit · Docker/K8s |
103	| [**`MANUAL.md`**](MANUAL.md) | The complete single-page reference (everything, in depth) |
104	
105	## Repository layout
106	
107	```
108	cmd/        CLI binaries — care (HTTP API + SPA), demo, caredemo, bench, evals, …
109	pkg/        36 library packages — agent, orchestration, comm (bus), governance,
110	            audit, hitl, llm, knowledge (RAG), fhir, hl7v2, phi, observability, …
111	agents/     agent definitions (diagnostic + care services)
112	data/       synthetic case fixtures + knowledge corpus
113	db/         SQL migrations          docs/   guides + diagrams
114	zarf/       Docker Compose + Helm/Kubernetes manifests
115	maf/        nested module: the MARA pipeline reimplemented on Microsoft Agent Framework (Go)
116	```
117	
118	## Contributing
119	
120	Contributions are welcome — see [`CONTRIBUTING.md`](CONTRIBUTING.md) for the workflow, design rules, and how to add an agent, policy, or condition. CI enforces `go vet`, `go test -race`, an 85% coverage gate on `pkg/`, `govulncheck`, and the frontend build. Please also read the [Code of Conduct](CODE_OF_CONDUCT.md).
121	
122	## Security
123	
124	Report vulnerabilities via GitHub's private advisory flow — see [`SECURITY.md`](SECURITY.md). API keys belong only in a gitignored `.env`; never commit secrets or real PHI.
125	
126	## License
127	
128	[Apache License 2.0](LICENSE) — see [`NOTICE`](NOTICE). Changes are tracked in [`CHANGELOG.md`](CHANGELOG.md).
129	
130	> **Not a medical device. Not clinical software. Research and education only.**
