# AGENTS.md — Field guide for AI coding assistants

> **Audience.** Claude Code, Cursor, Copilot, Aider, Codex CLI, and any other
> coding assistant that opens this repo. Humans should start with [README.md](README.md)
> and [CONTRIBUTING.md](CONTRIBUTING.md) instead — this file is the *short, dense*
> orientation, optimised to be loaded as a system prompt or memory.
>
> Read this file completely before suggesting any change. It encodes invariants
> that are easy to violate, expensive to fix, and hard to detect in review.

---

## What this repo is

**Bodhyanga** is a HIPAA-aware, multi-agent medical AI platform written in Go. It is a
working implementation of Microsoft's [Multi-Agent Reference Architecture](https://github.com/microsoft/multi-agent-reference-architecture)
(MARA), specialised for clinical decision support, panel management, and care-services
workflows. It runs Eric Horvitz's *Sequential Diagnosis with Language Models*
pipeline (Gatekeeper / panel / cost-controlled diagnostic loop) end-to-end, plus
care-services workflows (CDM, TCM, virtual office, panel reviews, refills, prior auth,
readmission risk).

**It is a research and engineering reference. It is not a medical device, not
approved for clinical use, and must not be used to make decisions affecting
real patients.** Every file you touch should preserve that disclaimer chain.

---

## Three things that will get you reverted

These are not style preferences. They are load-bearing invariants. If a change
violates any of them, do not propose it — propose a different change.

1. **Never bypass the HITL gate for clinically consequential actions.** Diagnosis
   conclusions, treatment recommendations, discharge plans, prior-auth approvals,
   refill approvals, and outreach to patients on protected conditions (42 CFR Part 2)
   must flow through `agents/human_review.go` → `pkg/hitl.Queue` → clinician decision.
   The gate is the 21st Century Cures Act §3060 "independent review" criterion 4
   expressed as code; without it Bodhyanga becomes a regulated medical device. See
   [docs/governance.md](docs/governance.md) and [docs/compliance.md](docs/compliance.md).

2. **Never widen the PHI surface of the audit log.** `pkg/audit` records *enough
   to reconstruct, never enough to leak*: IDs (case, patient, tenant, trace), kinds,
   timestamps, agent IDs, decision verdicts, redacted reasons. It does **not** record
   chief complaints, HPI narrative, lab values, medication names+doses, free-text
   reasons, or any field that would let a reader recover the patient's clinical
   story from the log alone. The right-to-explanation handler
   (`cmd/care/explain.go`) inherits this invariant — its tests verify forbidden
   JSON keys are absent (`TestExplainHandler_PHIFreeFieldSelection`).

3. **Every LLM-backed agent has a deterministic rule-based fallback, and the
   fallback is the contract.** `LLMDiagnostician → diagnostician.go (rule)`,
   `LLMInboxTriage → rule triage`, `LLMQuestioner → rule questioner`. When the
   LLM times out, returns a refusal pattern (`pkg/safety.IsRefusal`), or fails
   grounding (`pkg/safety.CheckHallucination`), the agent must degrade to its
   rule-based sibling and continue the case — not raise, not crash, not skip
   the message. Bench fixtures (`data/cases/`) are graded on the rule path, so
   regressions here are visible in CI.

---

## Project layout

```
bodhyanga/
├── cmd/                       entry points (six binaries)
│   ├── demo/                  sequential diagnosis headless runner
│   ├── caredemo/              care-services workflows (CDM, TCM, panel, virtual office)
│   ├── care/                  HTTP API server + embedded clinician SPA
│   ├── bench/                 SD-Bench runner with baseline regression gate
│   ├── bodhyangactl/               operator CLI (stdlib-only, no external deps)
│   └── export-hitl-feedback/  turn HITL decisions into SFT / DPO JSONL
├── agents/                    47 agent implementations + factories
├── pkg/                       26 reusable packages (see below)
├── data/
│   ├── cases/                 bench fixtures + manifest + baseline
│   ├── eval/                  red-team cases
│   └── knowledge/             RAG corpus (BM25 + vector)
├── db/migrations/             Postgres schema + RLS policies
├── docs/                      18 deep-dive markdown files
└── .github/workflows/         CI: build + vet + test (race) + govulncheck
```

### Packages worth knowing before editing

| Package | What it does | Don't change without an issue first |
|---|---|---|
| `pkg/protocol` | `Message{Role, Type, Content, Metadata}` — the wire type | Yes |
| `pkg/agent` | `Agent` interface, `Environment` interface | Yes |
| `pkg/orchestration` | the only place where governance runs before agents | Yes |
| `pkg/comm` | `InMemoryBus`, FIFO per-publish, panic-recovering | Discuss first |
| `pkg/governance` | `Policy` + the shipped policies | Discuss first |
| `pkg/audit` | append-only, PHI-free; HIPAA §164.312(b) | **Especially** yes |
| `pkg/phi` | redaction patterns (SSN/MRN/phone/email/date) | Yes, with tests |
| `pkg/safety` | `IsRefusal`, `CheckHallucination`, `ContainsPHIPatterns` | Yes |
| `pkg/hitl` | review queue: Submit, Decide, Pending, Expire | Yes |
| `pkg/llm` | provider-agnostic client (Anthropic, OpenAI, Ollama, mock) | Add provider freely |
| `pkg/medical` | clinical domain types (`CaseState`, `ReviewRequest`, etc.) | Yes |
| `pkg/persistence` | file + Postgres stores; RLS-bound | Yes |
| `pkg/observability` | structured logger, `LLMTrace`, OTLP export | OK |
| `pkg/eval` | `InteractionRecord`, `LLMJudge`, red-team | OK |
| `pkg/mcp` | Model Context Protocol client (JSON-RPC over HTTP+SSE) | OK |
| `pkg/hl7v2` | HL7 v2 ADT parser (A01-A08) | OK |
| `pkg/fhir` | FHIR R4 domain types (in-process, no external lib) | Discuss before adding deps |
| `pkg/knowledge` | RAG store (hybrid BM25 + vector) | OK |
| `pkg/tools` | LLM-callable tools (drug interactions, risk scores, FHIR query) | OK, with tests |

### Commands (cmd/)

| Binary | Purpose | Key flags |
|---|---|---|
| `demo` | Headless sequential diagnosis | `-diagnostician={rule\|llm-anthropic\|llm-openai\|llm-ollama}` |
| `care` | HTTP API + clinician SPA on `:8088` | `-diagnostician`, `-serve-ui` |
| `caredemo` | In-process care-services workflows | `-workflow={cdm\|tcm\|panel\|virtual-office\|...}` |
| `bench` | SD-Bench runner with baseline regression gate | `-cases`, `-providers`, `-update-baseline`, `-json` |
| `bodhyangactl` | Operator CLI for the `care` API | `reviews list/approve/reject`, `tasks list`, `panel <cond>` |
| `export-hitl-feedback` | HITL decisions → SFT + DPO JSONL | `-in`, `-out-dir` |

---

## Build, test, lint

```bash
go version            # require 1.22+, 1.26 recommended
make ci               # vet + test (race) + govulncheck — the CI gate
make test             # all Go tests
make test-race        # race detector
make cover            # function-level coverage report

# Targeted runs
go test ./pkg/audit/... -run TestRecorder_Append -count=1
make care             # boot HTTP API at :8088 (default DIAGNOSTICIAN=llm-ollama)
make demo             # one-shot sequential diagnosis on a fixture
make bench            # run all SD-Bench fixtures + Pareto report
```

CI runs on every PR (`.github/workflows/ci.yml`):
1. `go build ./...`
2. `go vet ./...`
3. `go test -race -coverprofile=... ./...`
4. `govulncheck ./...`
5. Smoke run of `demo` + `bench` (rule path) — must not regress baseline accuracy.

**Before you propose a change, run `make ci` locally.** PRs that don't pass
CI get bounced; PRs that pass CI but introduce a baseline regression on
`data/cases/baseline.json` need explicit justification in the body.

---

## Doc map — read these before editing the area they cover

| If you're touching… | Read first |
|---|---|
| Any agent | [docs/agents.md](docs/agents.md) — every shipped agent, contract, failure modes |
| Orchestrator, bus, registry | [docs/architecture.md](docs/architecture.md) |
| HTTP routes, /api, /explain | [docs/api.md](docs/api.md) |
| Policy or governance | [docs/governance.md](docs/governance.md) + [docs/compliance.md](docs/compliance.md) |
| Audit, observability, OTLP | [docs/operations.md](docs/operations.md) |
| Clinical types | [docs/data-model.md](docs/data-model.md) |
| FHIR / HL7 / SNOMED / RxNorm / LOINC | [docs/healthcare-standards.md](docs/healthcare-standards.md) |
| LLM clients / RAG / safety | [docs/ai-features.md](docs/ai-features.md) |
| Bench / eval / red-team | [docs/evaluation.md](docs/evaluation.md) |
| Deployment | [docs/deployment.md](docs/deployment.md) |
| Adding an LLM agent | [docs/model-cards/TEMPLATE.md](docs/model-cards/TEMPLATE.md) — required for every variant |
| New condition / new policy / large change | [docs/aiia-template.md](docs/aiia-template.md) — required in the PR body |
| Production incidents | [docs/incident-playbook.md](docs/incident-playbook.md) |
| AIGP body-of-knowledge alignment | [docs/aigp-governance-mapping.md](docs/aigp-governance-mapping.md) |
| Agent-to-agent / MCP / A2A | [docs/agent-interop.md](docs/agent-interop.md) |

---

## Coding conventions

- **Go 1.26.** `go.mod` is minimal: production deps are `github.com/google/uuid`
  and stdlib. Adding a new direct dependency is a design decision — file an
  issue first unless it's a stdlib-replacement (e.g. a Postgres driver under
  build tag).
- **Every `.go` source file has a sibling `_doc.go` companion** documenting the
  package contract or the file's role. New files follow the pattern.
- **Tests are table-driven and live next to the file they test.** Long-form
  test setup goes in `*_test.go` helpers; never put PHI-shaped data in a fixture
  (use synthetic IDs like `patient_123`, `case_456`).
- **Inline comments explain *why*, not *what*.** The codebase has a deliberate
  high-density-comments convention (see `pkg/safety/safety.go`,
  `pkg/audit/recorder.go`, `cmd/care/explain.go`) — preserve it. If you're
  removing comments to "clean up," stop.
- **All `time.Now()` calls go through `observability.Clock`** so tests can
  inject a fake clock. Don't sprinkle real wallclock.
- **No `panic()` in agent code.** The bus recovers panics
  (`pkg/comm/bus.go:InMemoryBus.publishWithRecovery`), but agents should return
  the error and let the orchestrator audit it (`audit.KindAgentError`).
- **Filenames are `lower_snake_case.go`.** Package names match directory names.

---

## How to add things

| Adding… | Pattern | Required artefacts |
|---|---|---|
| A new agent | Mirror `agents/test_planner.go` | impl + test + `_doc.go` + entry in `docs/agents.md` |
| A new LLM agent variant | Mirror `agents/diagnostician_llm.go` | impl + test + rule fallback + model card under `docs/model-cards/` + bench delta |
| A new policy | Mirror `pkg/governance/cost_guardian.go` | impl + table-driven test + entry in `docs/governance.md` |
| A new condition (asthma, COPD, etc.) | Mirror PR #6 (asthma) | care plan template + panel rules + types + bench fixture + AIIA in PR body |
| A new specialty rule | Mirror `agents/diagnostician.go` (ACS / sepsis / DKA / CHF rules) | rule + tests + bench delta + entry in `docs/agents.md` |
| A new bench fixture | Drop into `data/cases/` | manifest entry + (if regression-grade) baseline update |
| A new audit event kind | `pkg/audit/recorder.go` | const + field selection comment + tests + `docs/governance.md` entry |
| A new HTTP endpoint | Mirror `cmd/care/explain.go` | route reg + handler + test + `docs/api.md` entry |
| A new LLM provider | Mirror `pkg/llm/anthropic.go` | client + tests + provider matrix entry in `docs/ai-features.md` |
| A new MCP server integration | Use `pkg/mcp.Client` | tool wrapper + test + `docs/agent-interop.md` entry |
| A new RAG corpus document | Drop into `data/knowledge/` | content + provenance + AIIA in PR body |

When adding a clinically consequential change (new condition, new agent variant,
LLM model swap, new HTTP endpoint exposing clinical content), the PR body
**must** include a completed AIIA from [docs/aiia-template.md](docs/aiia-template.md).
Reviewers will block the PR otherwise.

---

## Don't do these things

1. **Don't commit to `main` directly.** Branch + PR + CI green before merge.
2. **Don't add an LLM agent without a rule-based fallback.** See invariant #3 above.
3. **Don't put PHI in tests, fixtures, logs, or audit fields.** Synthetic IDs only.
4. **Don't bypass the orchestrator** by having agents call each other directly.
   Agents emit messages onto the bus; the orchestrator routes after policy evaluation.
5. **Don't write policy logic inside agents.** All `Policy.Evaluate` lives in
   `pkg/governance`; agents see only allowed messages.
6. **Don't modify audit log entries after write.** Append-only. To revise a
   decision, append a new event with the correction reason.
7. **Don't add an external FHIR library.** `pkg/fhir` is intentionally minimal
   and in-process. Same for HL7 v2.
8. **Don't ship LLM calls without `observability.LLMTrace`** and without a
   `pkg/safety` post-check on the output.
9. **Don't disable a safety check** (refusal pattern, hallucination grounding,
   PHI shape detection) because a test is failing. Fix the test or the agent.
10. **Don't broaden the audit log surface** to include PHI fields "just to debug."
    Use `observability.Logger` with `phi.LoggerWrapper`; the audit log stays clean.
11. **Don't bump `pkg/protocol.Message`** without an issue first — the wire
    format is load-bearing across every agent and every external integration.
12. **Don't add an OTel-tagged dep to the default build.** `pkg/observability/otel`
    is gated behind `//go:build otel` so the default `go.sum` stays minimal.
13. **Don't introduce a 2-version diamond in `go.mod`.** When upgrading
    `go.opentelemetry.io/otel`, upgrade the whole family in lockstep and run
    `go mod tidy` afterwards.
14. **Don't push to remote without the user's explicit "yes."** Especially
    not to `main`, and especially not with `--force`.

---

## Quick mental model

```
Inbound message
  │
  ▼
Orchestrator.handleIncoming
  ├─ Policy.Evaluate   (deny → audit.KindPolicyDecision{Deny}, return)
  └─ Bus.Publish(msg)
        │
        ▼
   Subscribed agent(s).HandleMessage
        ├─ rule path                    ─┐
        └─ LLM path                      ├──► emit reply onto bus
              ├─ safety.IsRefusal? → fallback to rule path
              ├─ safety.CheckHallucination? → fallback
              └─ safety.ContainsPHIPatterns? → fallback + audit
        │
        ▼
   audit.Recorder.Record(KindMessage / KindAgentError / KindReviewDecision)
```

The bus is FIFO per publish. Two agents emitting on the same trigger see no
ordering guarantee between *their* messages, but each agent's own emissions
land in publish order. If you need causal ordering across agents, attach a
`causal_id` to `Metadata` and let the downstream consumer reorder.

---

## When in doubt

- **For architectural questions:** [docs/architecture.md](docs/architecture.md)
- **For "is this a clinical decision?":** Yes, ask. Default to gating it behind HITL.
- **For "is this PHI?":** If you're not sure, treat it as PHI.
- **For "is this a policy or an agent?":** If it can refuse a message, it's a policy.
- **For "should this go in `pkg/` or `agents/`?":** `pkg/` is reusable substrate;
  `agents/` is a specific behaviour.

If a change is large enough that this checklist doesn't cover it, file an issue
describing the change and tag it `discussion-first` before opening a PR.

---

*Last reviewed against `main` on 2026-05-26. If the repo has drifted, update
this file in the same PR that introduces the drift.*
