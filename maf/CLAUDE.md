# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

bodhyanga-maf is the **vertical-slice reimplementation of bodhyanga's MARA clinical pipeline on
`microsoft/agent-framework-go`** (MAF-go, public preview, pinned in go.mod). It exists to
answer one question with test evidence: can MAF-go carry bodhyanga's safety boundary? Like
bodhyanga, it is research/education code — **not a medical device, not for clinical use**.

Read first: `README.md` (scope + the 5 exit criteria) → `architecture.md` (design,
Mermaid diagrams, delivery plan) → `../bodhyanga/session.md` (decision record).

## Commands

```bash
go build ./... && go test ./...   # everything; no network except testcontainers
go run ./cmd/slice -verbose       # demo case through the pipeline
go test ./pkg/ckptpg -run TestKillResume -v   # durable-HITL integration test
```

## Ground rules

- **MAF-go stays pinned** (`v0.0.0-20260710122455-75d0a647f739`); upgrades are
  dedicated PRs. bodhyanga is imported via `replace ../bodhyanga` — compliance packages
  (`pkg/governance`, `pkg/audit`, `pkg/eval`, `pkg/phi`, `pkg/protocol`) are the
  authority; never reimplement them here.
- **Safety invariants** (architecture.md §3): fail-closed governance (policy error =
  deny), fail-open audit (audit failure never blocks care, but deny records are written
  synchronously), and **zero PHI in audit events, checkpoint metadata, logs, or test
  assertions** — case IDs appear only as `agents.HashCaseID` fingerprints.
- Executors stay **stateless** (state rides in messages) — checkpoint/resume depends
  on it.
- Tests never call real LLM APIs (fake providers via `agent.ProviderConfig{Run: ...}`);
  live-LLM runs are manual, behind env flags.

## Layout

```
cmd/slice/        thin CLI: flags, run loop, physician inbox
cmd/care/         HTTP server over the Runner (specs/004)
cmd/bench/        SD-Bench accuracy harness (specs/005)
pkg/agents/       intake, translator, test_planner, diagnostician (rule + llm + RAG), care_plan
pkg/intake/       front door: HL7 v2 ADT / FHIR R4 → PatientCase (specs/002)
pkg/careapi/      HTTP handlers over pipeline.Runner: submit/status/reviews/audit (specs/004)
pkg/bench/        SD-Bench harness: manifest → run → score → accuracy + baseline gate (specs/005)
pkg/pipeline/     graph assembly (edges, loop-back, review gate, opt-in translation hop)
pkg/admit/        THE admission boundary (decorator + agent middleware)
pkg/auditbridge/  workflow events → bodhyanga audit trail
pkg/ckptpg/       Postgres checkpoint.Store
pkg/evalhook/     terminal-hop eval records
specs/            Spec Kit artifacts (spec → plan → tasks)
```

Ingestion: `go run ./cmd/slice -format hl7 -case msg.hl7` (or `-format fhir bundle.json`)
drives the translation front door; default `-format json` keeps the native path. RAG:
`-corpus corpus.json -embedder mock|ollama` grounds the `-diagnostician=llm-*` engines.
HTTP: `go run ./cmd/care` serves the pipeline (submit/status/reviews/audit); `-db <pg>` makes
paused reviews survive restart. Loopback + auth-gap banner — auth is a non-goal (ADR-0006).
Eval: `go run ./cmd/bench` runs the SD-Bench suite — accuracy AND mean cost (the Pareto axis;
specs/006 added per-case cost through the pipeline); `-fail-on-regression` gates CI on accuracy,
`-update-baseline` refreshes it. The gate also runs as a unit test (`go test ./pkg/bench`).

<!-- SPECKIT START -->
For technologies, structure, commands, and design decisions, read the relevant plan.
The safety-boundary slice (specs/001-slice-safety-boundary) is complete; the repo now
grows story by story. Shipped: specs/003-knowledge-rag (RAG grounding), specs/002-fhir-hl7-intake
(HL7/FHIR front door), specs/004-care-api-runner (HTTP care API), specs/005-sdbench-harness
(accuracy eval + baseline gate), specs/006-per-case-cost (Pareto cost axis through the
pipeline — bodhyanga's MAI-DxO cost-aware thesis, reproduced). Each specs/NNN-*/ holds
spec.md → plan.md → tasks.md. The originally-planned slice stories are all shipped.
<!-- SPECKIT END -->
