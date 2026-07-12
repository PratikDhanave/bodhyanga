# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

bodh-maf is the **vertical-slice reimplementation of bodh's MARA clinical pipeline on
`microsoft/agent-framework-go`** (MAF-go, public preview, pinned in go.mod). It exists to
answer one question with test evidence: can MAF-go carry bodh's safety boundary? Like
bodh, it is research/education code — **not a medical device, not for clinical use**.

Read first: `README.md` (scope + the 5 exit criteria) → `architecture.md` (design,
Mermaid diagrams, delivery plan) → `../bodh/session.md` (decision record).

## Commands

```bash
go build ./... && go test ./...   # everything; no network except testcontainers
go run ./cmd/slice -verbose       # demo case through the pipeline
go test ./pkg/ckptpg -run TestKillResume -v   # durable-HITL integration test
```

## Ground rules

- **MAF-go stays pinned** (`v0.0.0-20260710122455-75d0a647f739`); upgrades are
  dedicated PRs. bodh is imported via `replace ../bodh` — compliance packages
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
pkg/agents/       intake, test_planner, diagnostician (rule + llm), care_plan
pkg/pipeline/     graph assembly (edges, loop-back, review gate)
pkg/admit/        THE admission boundary (decorator + agent middleware)
pkg/auditbridge/  workflow events → bodh audit trail
pkg/ckptpg/       Postgres checkpoint.Store
pkg/evalhook/     terminal-hop eval records
specs/            Spec Kit artifacts (spec → plan → tasks)
```

<!-- SPECKIT START -->
For technologies, structure, commands, and design decisions, read the current plan.
The active feature is specs/001-slice-safety-boundary (branch
`feat/slice-safety-boundary`): specs/001-slice-safety-boundary/plan.md, with
research.md, data-model.md, contracts/, and quickstart.md alongside it.
<!-- SPECKIT END -->
