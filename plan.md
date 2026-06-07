# Bodh — Comprehensive Project Plan

> **Scope**: Full-stack delivery plan — UI, backend, testing, E2E, evaluation,
> infrastructure, documentation, and clinical safety. Every item is grounded in
> actual source code analysis.
>
> **Date**: 2026-06-06
> **Module**: `github.com/PratikDhanave/bodh` (Go 1.26)

---

## Table of Contents

1. [Current State Snapshot](#1-current-state-snapshot)
2. [TRACK A — Backend Engineering](#2-track-a--backend-engineering)
3. [TRACK B — Frontend / UI](#3-track-b--frontend--ui)
4. [TRACK C — Testing (Go Unit Tests)](#4-track-c--testing-go-unit-tests)
5. [TRACK D — Frontend Unit Tests (Vitest)](#5-track-d--frontend-unit-tests-vitest)
6. [TRACK E — E2E Tests (Playwright)](#6-track-e--e2e-tests-playwright)
7. [TRACK F — Evaluation Framework](#7-track-f--evaluation-framework)
8. [TRACK G — Integration & Smoke Tests](#8-track-g--integration--smoke-tests)
9. [TRACK H — Infrastructure & DevOps](#9-track-h--infrastructure--devops)
10. [TRACK I — Documentation & Compliance](#10-track-i--documentation--compliance)
11. [TRACK J — Clinical Safety UX](#11-track-j--clinical-safety-ux)
12. [Execution Schedule](#12-execution-schedule)
13. [Success Criteria](#13-success-criteria)
14. [Risk Register](#14-risk-register)

---

## 1. Current State Snapshot

### 1.1 Project Scale

| Dimension | Count |
|---|---|
| Go source lines (non-test) | ~382K |
| Go test lines | ~533K |
| Go packages (`pkg/`) | 65 directories |
| Go binaries (`cmd/`) | 36 main.go files |
| Agent source files (`agents/`) | 95 |
| Agent test files (`agents/`) | 48 (59 agents untested) |
| Frontend pages | 19 |
| Frontend components | 6 |
| Frontend hooks | 3 |
| API routes (`cmd/care/main.go`) | 27 |
| Makefile targets | 80+ |
| Docs (markdown) | 17 files |
| DB migrations | 7 files |
| CI workflows | 1 (ci.yml) |
| Lessons (cmd/) | 13 |

### 1.2 Go Coverage Summary

| Bucket | Packages | Action |
|---|---|---|
| **0% (no tests)** | agent(root), agentloop, computeruse, multiturneval, singleturneval | Write from scratch |
| **Below 80%** | finetune (64.6%), persistence/postgres (65.9%), eval/federated (73.2%), agent/planning (73.8%), migrate (76.3%), dbtest (77.0%) | Priority 1 |
| **80-90%** | llm (89.3%), tools (88.5%), embeddings (89.8%), governance (89.8%), evals/laminar (89.7%) | Priority 2 |
| **90-95%** | eval/trace (90.7%), auth (90.8%), hitl (91.4%), sqldb (91.5%), memory (92.6%), mcp (93.0%), mcp/server (93.0%), persistence (93.1%), audit (94.2%), knowledge (94.7%) | Priority 3 |
| **95-99%** | 20 packages | Push to ~100% |
| **100% (done)** | 22 packages | Maintain |
| **Overall** | **72.8%** → **target >= 95%** | |

### 1.3 Frontend Test Summary

| Area | Coverage | Gap |
|---|---|---|
| Page unit tests | 6/19 (32%) | 13 pages missing |
| Component unit tests | 0/6 (0%) | All missing |
| Hook unit tests | 0/3 (0%) | All missing |
| API client tests | 0/1 (0%) | Missing |
| i18n tests | 0/2 (0%) | Missing |
| E2E specs | 13/19 pages | 6 pages + 2 flows missing |
| i18n page coverage | 1/19 (5%) | Only PatientPortal uses `useI18n()` |

### 1.4 Infrastructure Gaps

| Area | Status |
|---|---|
| CI (GitHub Actions) | 1 workflow, Go only — no frontend CI, no E2E CI |
| Docker | Dockerfile exists, distroless multi-stage |
| Docker Compose | `zarf/compose/docker_compose.yaml` exists |
| K8s | Base + dev + prod Kustomize overlays exist |
| Monitoring | Prometheus config exists, Grafana missing |
| API documentation | No OpenAPI/Swagger spec |
| Security scanning | govulncheck in CI, no SAST/DAST |

---

## 2. TRACK A — Backend Engineering

### A1. Unwired API Endpoints (14 routes exist in Go, no frontend binding)

These routes exist in `cmd/care/main.go` but have no frontend pages or `api/client.ts` bindings:

| # | Route | Backend Handler | UI Task |
|---|---|---|---|
| A1.1 | `POST /cdm` | `forward(bus, "cdm_planner")` | CDM workflow page |
| A1.2 | `POST /tcm/discharge` | inline handler | TCM discharge page |
| A1.3 | `POST /pre_visit` | `forward(bus, "pre_visit_prep")` | Pre-visit planning page |
| A1.4 | `POST /inbox` | `forward(bus, "inbox_triage")` | Patient inbox page |
| A1.5 | `POST /refill` | `forward(bus, "refill_manager")` | Medication refill page |
| A1.6 | `POST /priorauth` | `forward(bus, "prior_auth")` | Prior auth page |
| A1.7 | `POST /adt` | inline handler | ADT event viewer |
| A1.8 | `POST /hl7/adt` | inline HL7 parser | HL7 event viewer |
| A1.9 | `POST /rpm/observation` | inline handler | Wire to RPM page (currently stub data) |
| A1.10 | `POST /fhir/Patient` | inline handler | FHIR patient import |
| A1.11 | `POST /fhir/Encounter` | inline handler | FHIR encounter viewer |
| A1.12 | `POST /fhir/Condition` | inline handler | FHIR condition list |
| A1.13 | `GET /explain/{case_id}` | `explainHandler()` | Right-to-explanation page |
| A1.14 | `GET /metrics` | `PrometheusHandler()` | Metrics dashboard (or link to Grafana) |

**Action**: Wire `A1.9` (RPM) and `A1.13` (explain) first — they unblock critical UI features.

### A2. Missing Backend Features

| # | Feature | Package | Description |
|---|---|---|---|
| A2.1 | Audit export (CSV/PDF) | `pkg/audit` | Add `ExportCSV(w io.Writer, filters)` for compliance officers |
| A2.2 | Patient search | `pkg/persistence` | Add `SearchPatients(query string) ([]PatientRecord, error)` |
| A2.3 | Task mutations | `pkg/tasks` | Add `UpdateTaskStatus(id, status)`, `AssignTask(id, assignee)` |
| A2.4 | Review list with sort | `pkg/hitl` | Add `ListReviews(sort, order, limit, offset)` |
| A2.5 | Case timeline | `pkg/audit` + `pkg/eval` | Aggregate audit events + eval results for a single case |
| A2.6 | WebSocket channels | `pkg/web` | Add channels for RPM alerts, case completion, agent status |
| A2.7 | Notification preferences | `pkg/notifications` | Add per-user alert tier preferences |
| A2.8 | Cost tracking | `pkg/analytics` | Wire real cost data (currently Cost page is 100% stub) |
| A2.9 | OpenAPI spec | `cmd/care` | Auto-generate or hand-write OpenAPI 3.0 spec for all 27 routes |

### A3. Agent Coverage Gap (59 agents without test files)

| Priority | Action | Count |
|---|---|---|
| High | Write tests for clinical agents (diagnostician variants, care planners) | ~15 |
| Medium | Write tests for workflow agents (intake, triage, coordinators) | ~20 |
| Low | Write tests for utility agents (formatters, validators) | ~24 |

**Pattern**: Each agent test should follow `agents/*_test.go` convention with:
- `TestXxx_HandleMessage_HappyPath` — valid input → expected output
- `TestXxx_HandleMessage_InvalidJSON` — malformed content → error
- `TestXxx_HandleMessage_EmptyContent` — empty string → graceful handling
- `TestXxx_Metadata` — `ID()`, `Name()`, `Capabilities()` return correct values

---

## 3. TRACK B — Frontend / UI

### B1. Design System Components (New Files)

| # | Component | File | Purpose |
|---|---|---|---|
| B1.1 | Modal/Dialog | `components/Modal.tsx` | Confirmation dialogs for clinical decisions |
| B1.2 | Skeleton loader | `components/Skeleton.tsx` | Loading placeholders matching card dimensions |
| B1.3 | Tooltip | `components/Tooltip.tsx` | Hover explanations for jargon (SFT, DPO, etc.) |
| B1.4 | Tabs/SegmentedControl | `components/Tabs.tsx` | Replace button-group filters (Reviews, Tasks) |
| B1.5 | SearchInput | `components/SearchInput.tsx` | Debounced search for Patients, Agents, AuditLog |
| B1.6 | SortableTable | `components/SortableTable.tsx` | Reusable sortable/paginated table for 10+ pages |
| B1.7 | StatCard | `components/StatCard.tsx` | Extract from Dashboard/ClinicianDashboard/Cost/FineTune |
| B1.8 | Breadcrumbs | `components/Breadcrumbs.tsx` | Route-aware breadcrumbs for ReviewDetail, RunTrace |
| B1.9 | ConfirmButton | `components/ConfirmButton.tsx` | Two-step button for irreversible actions |
| B1.10 | ConnectionStatus | `components/ConnectionStatus.tsx` | Global WebSocket/SSE health indicator |

### B2. Page Improvements (Existing Files)

| # | Page | Changes | Priority |
|---|---|---|---|
| B2.1 | Layout.tsx | Group sidebar into 4 categories (Clinical, Population, Platform, Compliance); add hamburger toggle for mobile; add skip-to-content link | High |
| B2.2 | ReviewDetail.tsx | Replace `alert()` with Toast; add confirmation Modal; use `getCurrentUser().email` as default reviewer | Critical |
| B2.3 | ClinicianDashboard.tsx | Add decision confirmation; format confidence as category; use `Promise.allSettled` for 4 parallel API calls | Critical |
| B2.4 | Login.tsx | Add dark mode support; differentiate 500 vs network errors; add rate-limit feedback | Medium |
| B2.5 | AuditLog.tsx | Refactor to use `useApi` hook; add URL-based filter persistence; add CSV export button | High |
| B2.6 | Tasks.tsx | Add task actions (mark done, assign); add overdue highlighting; sort by due date | High |
| B2.7 | Patients.tsx | Add SearchInput; add patient detail link `/patients/:id`; add pagination | High |
| B2.8 | RunTrace.tsx | Add event virtualization (cap at 500); add stage grouping; add summary card on completion | Medium |
| B2.9 | RPM.tsx | Wire to real `/rpm/observation` API; add alert acknowledgment workflow | Medium |
| B2.10 | Cost.tsx | Add "Demo data" banner; wire to real cost tracking when backend ready | Medium |
| B2.11 | Bench.tsx | Add chart tooltips; add color legend; add hover-highlight between table and chart | Low |
| B2.12 | LLMTraces.tsx | Add error filter; add estimated cost column; add sortable headers | Medium |
| B2.13 | FineTune.tsx | Add "Copy command" button; add tooltips for SFT/DPO jargon | Low |

### B3. New Pages

| # | Route | Page | Description |
|---|---|---|---|
| B3.1 | `/cases/:id/timeline` | CaseTimeline.tsx | Full case audit trail + MARA pipeline visualization |
| B3.2 | `/explain/:case_id` | Explanation.tsx | Right-to-explanation view (backend exists, no UI) |
| B3.3 | `/patients/:id` | PatientDetail.tsx | Single patient detail with vitals chart, conditions, history |
| B3.4 | `/agents/:id` | AgentDetail.tsx | Agent config, message history, performance stats |

### B4. Design System Fixes

| # | Fix | Files Affected |
|---|---|---|
| B4.1 | Complete bodh color palette (add 300, 500, 900 shades) | `tailwind.config.js` |
| B4.2 | Add semantic color CSS variables (`--color-urgent`, `--color-success`) | `index.css` |
| B4.3 | Add focus-visible styles to `.btn` and `.btn-secondary` | `index.css` |
| B4.4 | Fix `bodh-400` contrast (3.5:1 → 5.0:1 for WCAG AA) | `tailwind.config.js` |
| B4.5 | Add dark mode to badge variants | `index.css` |
| B4.6 | Add dark mode to Login page | `Login.tsx` |
| B4.7 | Add dark mode to SVG charts (Bench, RPM, Cost) | Respective pages |
| B4.8 | Add dark mode to error states (ErrorBox) | `Loader.tsx` |

### B5. Accessibility (a11y) Fixes

| # | Fix | WCAG Level | Files |
|---|---|---|---|
| B5.1 | Add skip-to-content link | A | Layout.tsx |
| B5.2 | Add `<caption>` or `aria-label` to all tables | A | 10+ pages |
| B5.3 | Add ARIA live regions for dynamic content | AA | RunTrace, Toast |
| B5.4 | Add accessible SVG chart descriptions | A | Bench, RPM, Cost |
| B5.5 | Add focus trap to NotificationBell dropdown | AA | NotificationBell.tsx |
| B5.6 | Add Escape-to-close on dropdown panels | AA | NotificationBell, Layout i18n |
| B5.7 | Add `role="radiogroup"` to filter toggles | AA | Reviews, Tasks |
| B5.8 | Add keyboard navigation to table rows | AA | All table pages |

### B6. i18n Expansion

| Phase | Pages | Est. New Keys |
|---|---|---|
| B6.1 | Login, Dashboard, ClinicianDashboard | ~60 |
| B6.2 | Reviews, ReviewDetail, Tasks | ~50 |
| B6.3 | Patients, PanelReport, AuditLog | ~45 |
| B6.4 | All remaining pages + components | ~95 |
| **Total** | 18 pages + components | **~250 keys × 4 locales** |

### B7. Responsive / Mobile

| # | Fix | Files |
|---|---|---|
| B7.1 | Add hamburger sidebar toggle below `lg` breakpoint | Layout.tsx |
| B7.2 | Add responsive column hiding for LLMTraces (10 cols) | LLMTraces.tsx |
| B7.3 | Ensure all touch targets >= 44px | All interactive elements |
| B7.4 | Add card-based table fallback for `<sm` screens | SortableTable.tsx |

---

## 4. TRACK C — Testing (Go Unit Tests)

### C1. Priority 1 — Packages Below 80% (6 packages)

| # | Package | Current | Target | Test Strategy |
|---|---|---|---|---|
| C1.1 | `pkg/finetune` | 64.6% | >=95% | Test: validate errors (bad JSON, missing fields, empty dataset, single-example), split edge cases, upload HTTP error paths, JSONL format verification |
| C1.2 | `pkg/persistence/postgres` | 65.9% | >=80% | Test: CRUD via `TestDB()` testcontainers, tenant isolation, encryption round-trip, concurrent reads, connection pool exhaustion. DB-dependent — acceptable at 80%. |
| C1.3 | `pkg/eval/federated` | 73.2% | >=95% | Test: single-node federation, conflicting results aggregation, timeout during federation, empty node result, partial failure |
| C1.4 | `pkg/agent/planning` | 73.8% | >=95% | Test: empty plan steps, invalid JSON tool response, context cancellation mid-plan, max-steps overflow |
| C1.5 | `pkg/migrate` | 76.3% | >=95% | Test: empty migration dir, rollback path, duplicate version, already-migrated skip, SQL syntax error |
| C1.6 | `pkg/dbtest` | 77.0% | >=90% | Test: container startup failure, cleanup on test panic, concurrent test isolation, custom config |

### C2. Priority 2 — Packages at 80-90% (5 packages)

| # | Package | Current | Target | Test Strategy |
|---|---|---|---|---|
| C2.1 | `pkg/tools` | 88.5% | >=95% | Test: all uncovered tool execute paths, error handling for each tool, malformed input, tool timeout |
| C2.2 | `pkg/llm` | 89.3% | >=95% | Test: streaming error mid-chunk, GBNF grammar validation, model-not-found, provider fallback, token counting |
| C2.3 | `pkg/embeddings` | 89.8% | >=95% | Test: zero-length input, batch size boundary, unicode handling, embedding dimension mismatch |
| C2.4 | `pkg/governance` | 89.8% | >=95% | Test: composite policy all-reject, empty policy list, policy timeout, concurrent eval, Rego eval error |
| C2.5 | `pkg/evals/laminar` | 89.7% | >=95% | Test: HTTP 500 response, timeout, malformed API response, empty evaluation results, retry backoff |

### C3. Priority 3 — Packages at 90-95% (10 packages)

| # | Package | Current | Target |
|---|---|---|---|
| C3.1 | `pkg/eval/trace` | 90.7% | >=95% |
| C3.2 | `pkg/auth` | 90.8% | >=95% |
| C3.3 | `pkg/hitl` | 91.4% | >=95% |
| C3.4 | `pkg/sqldb` | 91.5% | >=98% |
| C3.5 | `pkg/memory` | 92.6% | >=98% |
| C3.6 | `pkg/mcp` | 93.0% | >=98% |
| C3.7 | `pkg/mcp/server` | 93.0% | >=98% |
| C3.8 | `pkg/persistence` | 93.1% | >=98% |
| C3.9 | `pkg/audit` | 94.2% | >=98% |
| C3.10 | `pkg/knowledge` | 94.7% | >=98% |

### C4. Priority 4 — Packages at 95-99% (20 packages, push to ~100%)

| # | Package | Current | Edge Cases Needed |
|---|---|---|---|
| C4.1 | `pkg/eval/collaborative` | 95.7% | Single annotator, perfect agreement, all-different |
| C4.2 | `pkg/eval` (core) | 96.2% | Nil evaluator, empty dataset, panic recovery |
| C4.3 | `pkg/eval/judges` | 96.2% | TPR=0, TNR=0, bootstrap n=1, degenerate calibration |
| C4.4 | `pkg/shadow` | 96.2% | Nil pipeline, 100% concordance, ledger corruption |
| C4.5 | `pkg/a2a` | 96.3% | Malformed agent card, empty task, streaming cancel |
| C4.6 | `agents/` | 96.6% | 59 untested agents (see A3) |
| C4.7 | `pkg/eval/perturbation` | 96.7% | Identity perturbation, empty input, all-flip |
| C4.8 | `pkg/webhooks` | 96.9% | Delivery retry, invalid URL, timeout, payload overflow |
| C4.9 | `pkg/eval/taxonomy` | 97.1% | Empty failure set, saturation with 0 rounds |
| C4.10 | `pkg/eval/drift` | 97.4% | Zero-length window, single-point, stable series |
| C4.11 | `pkg/eval/rag` | 97.4% | K=0, empty query, empty corpus, NDCG single result |
| C4.12 | `pkg/errs` | 97.7% | All constructors, wrapping depth, Is/As chains |
| C4.13 | `pkg/fhir` | 97.8% | Empty bundle, bulk export 0 files, invalid resource |
| C4.14 | `pkg/hl7v2` | 97.8% | Empty segments, malformed MSH, OBX no value |
| C4.15 | `pkg/notifications` | 97.8% | Non-existent user, batch, overflow |
| C4.16 | `pkg/guardrails` | 98.5% | Empty input, all pass, all fail, timeout |
| C4.17 | `pkg/eval/equity` | 98.6% | Single cohort, zero-count, NaN handling |
| C4.18 | `pkg/cdshooks` | 98.9% | No cards, duplicate hook ID, timeout |
| C4.19 | `pkg/evals/multiturn` | 99.4% | Single-turn conversation, no response, token limit |
| C4.20 | `pkg/medical` | 99.8% | Remaining uncovered getters + formatters |

### C5. Priority 5 — Packages with 0% Coverage (5 new test files)

| # | Package | New File | Tests |
|---|---|---|---|
| C5.1 | `pkg/agent` (root) | `types_test.go` | Message, Agent interface, AgentCallbacks |
| C5.2 | `pkg/agent/agentloop` | `agentloop_test.go` | Loop lifecycle, tool dispatch, exit conditions |
| C5.3 | `pkg/agent/computeruse` | `computeruse_test.go` | Screenshot parsing, action marshaling |
| C5.4 | `pkg/agent/multiturneval` | `multiturneval_test.go` | Conversation turn logic, scoring |
| C5.5 | `pkg/agent/singleturneval` | `singleturneval_test.go` | Single-turn evaluation, prompt templates |

### C6. Exclusions (not counted toward 95% gate)

- `cmd/*` binaries — `main()` entry points, covered by smoke + E2E
- `pkg/audio/kronk`, `pkg/knowledge/rerank/kronk` — build-tag-gated (`kronk`)
- `pkg/bench`, `pkg/chaos` — type-only, no executable statements
- `scripts/zarf_validate` — standalone script

---

## 5. TRACK D — Frontend Unit Tests (Vitest)

### D1. Missing Page Unit Tests (13 files)

Each test follows existing pattern in `__tests__/*.unit.tsx`:
- Mock `fetch` using `mockFetch` from `src/test/setup.ts`
- Render with `@testing-library/react` + `MemoryRouter`
- Assert: loading → data → error → empty states
- Assert: user interactions (click, submit, filter, navigate)

| # | File | Key Test Cases |
|---|---|---|
| D1.1 | `Login.unit.tsx` | Form validation; successful login + redirect; failed login error; show/hide password toggle; network error; loading state |
| D1.2 | `AuditLog.unit.tsx` | Load audit events; filter by case ID, kind, agent; cursor pagination; empty state; error |
| D1.3 | `ClinicianDashboard.unit.tsx` | 4 stat cards render; cases table columns; HITL queue Accept/Reject; accuracy chart bars; partial API failure |
| D1.4 | `PatientPortal.unit.tsx` | Care plan card; panel gaps with severity colors; vitals table; i18n translations; empty patient |
| D1.5 | `Tasks.unit.tsx` | Task list render; all/open/urgent filter toggle; priority badges; due date formatting; empty |
| D1.6 | `Patients.unit.tsx` | Patient list render; conditions as badges; medications display; empty state; error |
| D1.7 | `PanelReport.unit.tsx` | Care gaps table; severity badges; by-condition sidebar; empty/no gaps |
| D1.8 | `Bench.unit.tsx` | Pareto chart SVG renders; frontier calculation; summary table; color mapping; empty |
| D1.9 | `LLMTraces.unit.tsx` | Trace table (10 cols); error row highlighting; refresh button; empty; time formatting |
| D1.10 | `FineTune.unit.tsx` | 4 stat cards; emphasis styling on SFT/DPO; CLI instructions display; empty |
| D1.11 | `Cost.unit.tsx` | Stat cards; provider bars; sparkline SVG; budget alert banner; stub data notice |
| D1.12 | `RPM.unit.tsx` | Patient table; HR sparkline render; alert highlighting; trend coloring; stub data |
| D1.13 | `About.unit.tsx` | Title + subtitle; disclaimer banner; repository links; renders without error |

### D2. Component Unit Tests (6 files)

| # | File | Key Test Cases |
|---|---|---|
| D2.1 | `Empty.unit.tsx` | Renders title; renders hint; no hint = no hint element |
| D2.2 | `Loader.unit.tsx` | Shows "Loading..." default; shows custom "what" text; ErrorBox renders message + retry button |
| D2.3 | `Page.unit.tsx` | Renders title in h1; subtitle optional; actions slot; children rendered |
| D2.4 | `Layout.unit.tsx` | 15 nav links; active route highlighted; dark mode toggle; i18n selector; NotificationBell; footer disclaimer |
| D2.5 | `NotificationBell.unit.tsx` | Badge hidden at 0; badge shows count; click opens dropdown; marks as read; WebSocket message accumulation; dropdown "View all" link |
| D2.6 | `Toast.unit.tsx` | showToast renders toast; 4 type variants (info/success/warning/error); auto-dismiss after 5s; manual dismiss; stacking |

### D3. Hook Unit Tests (3 files)

| # | File | Key Test Cases |
|---|---|---|
| D3.1 | `useApi.unit.ts` | Loading state; data resolved; error state; reload triggers re-fetch; cleanup on unmount (cancelled flag); tick increments |
| D3.2 | `useAuth.unit.ts` | setToken stores in localStorage; getToken retrieves; clearToken removes; isAuthenticated; getCurrentUser parses JWT base64url; malformed token returns null; useLogout clears + navigates |
| D3.3 | `useWebSocket.unit.ts` | Connects on mount; parses JSON messages; reconnects with exponential backoff; caps at 30s; cleanup on unmount; connected state |

### D4. Utility Unit Tests (3 files)

| # | File | Key Test Cases |
|---|---|---|
| D4.1 | `client.unit.ts` | JWT header injection; 401 auto-redirect + token clear; HTTP error throws with status + body; 204 returns undefined; SSE factory |
| D4.2 | `i18n/index.unit.ts` | `t()` returns English; `t()` with locale returns translated; missing key returns key; SUPPORTED_LOCALES correct; LOCALE_LABELS complete |
| D4.3 | `i18n/I18nProvider.unit.tsx` | Provides context; useI18n reads locale; setLocale updates; localStorage persistence; document.lang attribute update |

**Total: 25 new test files, ~130 test cases**

---

## 6. TRACK E — E2E Tests (Playwright)

### E1. Missing Page E2E Specs (6 files)

| # | File | Scenarios |
|---|---|---|
| E1.1 | `bench.spec.ts` | Navigate to /bench; Pareto chart SVG visible; summary table renders; empty state when no data |
| E1.2 | `cost.spec.ts` | Navigate to /cost; stat cards visible; sparkline SVG renders; budget alert when exceeded |
| E1.3 | `patients.spec.ts` | Navigate to /patients; patient table renders; conditions as badges; empty state |
| E1.4 | `reviews.spec.ts` | Navigate to /reviews; review list renders; pending/all toggle; click opens review detail |
| E1.5 | `rpm.spec.ts` | Navigate to /rpm; patient monitoring table renders; sparkline SVGs; alert indicators |
| E1.6 | `tasks.spec.ts` | Navigate to /tasks; task list renders; all/open/urgent filter; priority badges |

### E2. Cross-Cutting Flow Specs (2 files)

| # | File | Flow |
|---|---|---|
| E2.1 | `api-routes.spec.ts` | POST /api/patients → GET /api/patients verifies round-trip; POST /api/reviews/{id}/decide verifies state change; GET /api/audit verifies events logged |
| E2.2 | `full-pipeline.spec.ts` | Login → navigate to /runs → select fixture + provider → submit case → RunTrace page shows SSE events → navigate to /reviews → verify review appears → open review → approve → audit log shows decision |

### E3. About Page (borderline — simple static page)

| # | File | Scenarios |
|---|---|---|
| E3.1 | `about.spec.ts` | Navigate to /about; heading visible; disclaimer banner visible; links present |

**Total: 9 new spec files, ~35 test scenarios**

---

## 7. TRACK F — Evaluation Framework

### F1. Federated Evaluation (73.2% → 95%)

| # | Test | Corner Case |
|---|---|---|
| F1.1 | `TestFederated_SingleNode` | 1 node → no aggregation needed |
| F1.2 | `TestFederated_ConflictingResults` | Nodes disagree → conflict resolution |
| F1.3 | `TestFederated_NodeTimeout` | One node times out → partial results |
| F1.4 | `TestFederated_EmptyNodeResult` | Node returns empty → graceful handling |

### F2. Trace Analysis (90.7% → 95%)

| # | Test | Corner Case |
|---|---|---|
| F2.1 | `TestAnalyze_EmptyTrace` | No spans → empty analysis |
| F2.2 | `TestAnalyze_NoLLMSpans` | All non-LLM → no latency stats |
| F2.3 | `TestAnalyze_SingleSpan` | 1 span → works |
| F2.4 | `TestAnalyze_MalformedSpanData` | Invalid attributes → graceful skip |

### F3. All Other Eval Sub-packages (push to ~100%)

| Package | Tests Needed | Count |
|---|---|---|
| `eval/collaborative` | Single annotator, perfect agreement, all-different | 4 |
| `eval/drift` | Zero-length window, single-point, stable series | 3 |
| `eval/equity` | Single cohort, zero-count, NaN handling | 3 |
| `eval/judges` | TPR=0, TNR=0, bootstrap n=1, perfect classifier | 4 |
| `eval/perturbation` | Identity, empty input, all-flip | 3 |
| `eval/rag` | K=0, empty query, empty corpus, NDCG single | 4 |
| `eval/taxonomy` | Empty failure set, 0 rounds, immediate saturation | 3 |
| `eval` (core) | Nil evaluator, empty dataset, panic recovery | 3 |
| `evals/laminar` | HTTP 500, timeout, malformed response, empty results | 4 |
| `evals/multiturn` | Single-turn, no response, token limit | 3 |

**Total: ~38 new edge-case tests across 12 eval packages**

---

## 8. TRACK G — Integration & Smoke Tests

### G1. MARA Pipeline Integration (extend `pkg/orchestration/integration_test.go`)

| # | Test | Description |
|---|---|---|
| G1.1 | `TestIntegration_OrchestratorBootstrap` | Already exists |
| G1.2 | `TestIntegration_DiagnosticianDirect` | Already exists |
| G1.3 | `TestIntegration_FullPipelineRoundTrip` | **NEW** — intake → test_planner → diagnostician → care_plan |
| G1.4 | `TestIntegration_GovernanceRejectsOversized` | **NEW** — oversized message → rejection |
| G1.5 | `TestIntegration_AuditTrailComplete` | **NEW** — pipeline run → verify audit entry per agent step |

### G2. HTTP API Smoke Tests (new `cmd/care/api_test.go` using `httptest`)

| # | Test | Description |
|---|---|---|
| G2.1 | `TestSmoke_HealthzLiveness` | GET /healthz → 200 |
| G2.2 | `TestSmoke_ReadyzWithDB` | GET /readyz → 200 (mock DB) |
| G2.3 | `TestSmoke_PatientsCRUD` | POST + GET /patients round-trip |
| G2.4 | `TestSmoke_ADTIngestion` | POST /adt → 200, bus message published |
| G2.5 | `TestSmoke_HL7ADTIngestion` | POST /hl7/adt → 200, parsed correctly |
| G2.6 | `TestSmoke_ReviewsWorkflow` | GET /reviews → POST /reviews/{id}/decide |
| G2.7 | `TestSmoke_AuditList` | GET /audit → JSON array |
| G2.8 | `TestSmoke_ExplainEndpoint` | GET /explain/{case_id} → right-to-explanation JSON |

### G3. Shadow Mode Validation (extend `pkg/shadow/shadow_cover_test.go`)

| # | Test | Description |
|---|---|---|
| G3.1 | `TestShadow_RuleDiagnostician_CHF` | Shadow run CHF case → concordance recorded |
| G3.2 | `TestShadow_Summary_MultipleRuns` | 5 runs → summary stats correct |

---

## 9. TRACK H — Infrastructure & DevOps

### H1. CI/CD (`github/workflows/ci.yml`)

| # | Enhancement | Description |
|---|---|---|
| H1.1 | **Raise Go coverage gate** | 80% → 95% |
| H1.2 | **Add frontend unit test job** | `npm ci && npx vitest run --coverage` in `cmd/care/web/` |
| H1.3 | **Add frontend coverage gate** | Fail below 85% |
| H1.4 | **Add E2E job** | Playwright with `go run ./cmd/care` as webServer; upload report on failure |
| H1.5 | **Add TypeScript type-check** | `npx tsc --noEmit` in CI |
| H1.6 | **Add security scanning** | `gosec ./...` or Snyk step |
| H1.7 | **Add K8s validation** | `kubectl kustomize zarf/k8s/dev` + `kubeval` |
| H1.8 | **Add OPA policy test** | `make opa-test` in CI |

### H1.2 Frontend CI Job (YAML)

```yaml
frontend:
  name: vitest + tsc
  runs-on: ubuntu-latest
  defaults:
    run:
      working-directory: cmd/care/web
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-node@v4
      with:
        node-version: '20'
        cache: npm
        cache-dependency-path: cmd/care/web/package-lock.json
    - run: npm ci
    - run: npx tsc --noEmit
    - run: npx vitest run --coverage
    - name: coverage gate
      run: |
        COVERAGE=$(npx vitest run --coverage --reporter=json 2>/dev/null | jq '.total.statements.pct')
        echo "Frontend coverage: ${COVERAGE}%"
        if (( $(echo "$COVERAGE < 85" | bc -l) )); then
          echo "::error::Frontend coverage ${COVERAGE}% below 85%"
          exit 1
        fi
```

### H1.4 E2E CI Job (YAML)

```yaml
e2e:
  name: playwright e2e
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with: { go-version: '1.26', cache: true }
    - uses: actions/setup-node@v4
      with: { node-version: '20' }
    - name: install
      working-directory: cmd/care/web
      run: npm ci && npx playwright install --with-deps chromium
    - name: build frontend
      working-directory: cmd/care/web
      run: npm run build
    - name: run e2e
      working-directory: cmd/care/web
      run: npx playwright test
    - uses: actions/upload-artifact@v4
      if: failure()
      with:
        name: playwright-report
        path: cmd/care/web/playwright-report/
```

### H2. Docker & Compose

| # | Task | Description |
|---|---|---|
| H2.1 | Multi-arch Docker build | Add `--platform linux/amd64,linux/arm64` to CI build |
| H2.2 | Health check in Dockerfile | Add `HEALTHCHECK CMD ["/care", "-healthcheck"]` |
| H2.3 | Compose dev profile | Add Grafana + Jaeger containers for local observability |

### H3. Kubernetes

| # | Task | Description |
|---|---|---|
| H3.1 | HPA (Horizontal Pod Autoscaler) | Add autoscaling based on CPU/memory |
| H3.2 | Helm chart | Convert Kustomize to Helm for easier parameterization |
| H3.3 | Secret management | Add ExternalSecrets or SealedSecrets for API keys |

### H4. Monitoring & Observability

| # | Task | Description |
|---|---|---|
| H4.1 | Grafana dashboard JSON | Pre-built dashboard for request latency, error rate, LLM token usage |
| H4.2 | Alert rules | Prometheus alerting rules: 5xx rate > 1%, LLM latency p95 > 30s |
| H4.3 | Structured logging | Ensure all log output is JSON for log aggregation |

---

## 10. TRACK I — Documentation & Compliance

### I1. API Documentation

| # | Task | Description |
|---|---|---|
| I1.1 | OpenAPI 3.0 spec | Document all 27 routes with request/response schemas |
| I1.2 | Postman collection | Export from OpenAPI for developer testing |
| I1.3 | API versioning strategy | Document v1/ prefix plan for future breaking changes |

### I2. Architecture Documentation

| # | Task | Description |
|---|---|---|
| I2.1 | Update ROADMAP.md | Reflect completed phases, mark new in-progress items |
| I2.2 | Add ADR (Architecture Decision Records) | Document key decisions: LLM abstraction, audit chain-hash, build tags |
| I2.3 | System context diagram | C4 Level 1 diagram for external integrations |
| I2.4 | Container diagram | C4 Level 2 showing Go services, DB, frontend, message bus |

### I3. HIPAA Compliance Documentation

| # | Task | Description |
|---|---|---|
| I3.1 | HIPAA control mapping | Map Bodh features to HIPAA technical safeguards |
| I3.2 | Audit export format | Document the CSV/PDF export format for compliance officers |
| I3.3 | PHI data flow diagram | Show where PHI enters, how it's de-identified, where it exits |
| I3.4 | Incident response playbook | Extend AIIA template with operational runbook |

---

## 11. TRACK J — Clinical Safety UX

These are the highest-priority UI changes because they directly affect clinical outcomes.

### J1. Decision Confirmation (CRITICAL)

| # | Task | Description |
|---|---|---|
| J1.1 | Add Modal component | Reusable confirmation dialog |
| J1.2 | Wire to ReviewDetail | Approve/reject requires confirmation showing diagnosis + patient context |
| J1.3 | Wire to ClinicianDashboard | Inline Accept/Reject in HITL queue requires confirmation |
| J1.4 | Add reason requirement | Reason field becomes required (not optional) for clinical decisions |
| J1.5 | Add 2-second confirm delay | Prevent rapid mis-clicks on irreversible decisions |

### J2. Confidence Display

| # | Task | Description |
|---|---|---|
| J2.1 | Add confidence category | 0-50% "Low" (red), 50-70% "Moderate" (amber), 70-90% "High" (blue), 90-100% "Very high" (green) |
| J2.2 | Format as percentage | Show "92%" not "0.92" |
| J2.3 | Add tooltip with explanation | "AI confidence based on rule-based diagnostic criteria" |

### J3. Alert Fatigue Prevention

| # | Task | Description |
|---|---|---|
| J3.1 | Tier notifications by priority | Only toast for `urgent`/`high`; silent badge for `normal`/`low` |
| J3.2 | Color-code badge count | Red dot for urgent, amber for normal |
| J3.3 | Add "Do Not Disturb" mode | Suppress toasts for N minutes (still accumulate in dropdown) |

### J4. Research Demo Safeguards

| # | Task | Description |
|---|---|---|
| J4.1 | Add persistent top banner | "RESEARCH DEMO — Not for clinical use" dismissable per session, re-appears after 30 min |
| J4.2 | Add "DEMO" watermark to exports | Any CSV/PDF export includes watermark |
| J4.3 | Mark stub-data pages clearly | Cost and RPM pages show "Synthetic data — not connected to live systems" |

---

## 12. Execution Schedule

### Wave 1 — Clinical Safety + Foundation (Week 1-2)

| Track | Items | New Files | Est. Tests |
|---|---|---|---|
| J (Clinical Safety) | J1.1-J1.5, J2.1-J2.3, J4.3 | 2 components | — |
| B (Design System) | B1.1-B1.3, B4.3-B4.4 | 3 components | — |
| C (Backend Priority 1) | C1.1-C1.6 | 6 test files | ~45 |
| D (Frontend Pages) | D1.1-D1.6 | 6 unit tests | ~36 |
| **Subtotal** | | **17 files** | **~81** |

### Wave 2 — Usability + Coverage Push (Week 3-4)

| Track | Items | New Files | Est. Tests |
|---|---|---|---|
| B (Page Improvements) | B2.1-B2.7 | 0 (edits) | — |
| B (New Pages) | B3.1-B3.2 | 2 pages | — |
| C (Backend Priority 2) | C2.1-C2.5 | 5 test extensions | ~30 |
| C (Backend Priority 3) | C3.1-C3.10 | 10 test extensions | ~30 |
| D (Remaining Pages) | D1.7-D1.13 | 7 unit tests | ~42 |
| D (Components) | D2.1-D2.6 | 6 unit tests | ~24 |
| E (E2E) | E1.1-E1.6 | 6 specs | ~24 |
| **Subtotal** | | **36 files** | **~150** |

### Wave 3 — Eval + Integration + Polish (Week 5-6)

| Track | Items | New Files | Est. Tests |
|---|---|---|---|
| C (Backend Priority 4) | C4.1-C4.20 | 20 test extensions | ~60 |
| C (Backend Priority 5) | C5.1-C5.5 | 5 new files | ~20 |
| D (Hooks + Utils) | D3.1-D3.3, D4.1-D4.3 | 6 unit tests | ~30 |
| E (Flows) | E2.1-E2.2, E3.1 | 3 specs | ~12 |
| F (Eval Framework) | F1-F3 | ~12 test extensions | ~38 |
| G (Integration) | G1.3-G1.5, G2.1-G2.8 | 2 files | ~13 |
| **Subtotal** | | **48 files** | **~173** |

### Wave 4 — Infrastructure + Docs (Week 7-8)

| Track | Items | New Files | Est. Tests |
|---|---|---|---|
| H (CI/CD) | H1.1-H1.8 | 1 workflow update | — |
| H (Docker/K8s) | H2.1-H2.3, H3.1-H3.3 | 3 config files | — |
| H (Monitoring) | H4.1-H4.3 | 2 config files | — |
| I (Documentation) | I1.1-I1.3, I2.1-I2.4, I3.1-I3.4 | 8 docs | — |
| B (a11y) | B5.1-B5.8 | 0 (edits) | — |
| B (i18n) | B6.1-B6.4 | 4 locale updates | — |
| B (Responsive) | B7.1-B7.4 | 0 (edits) | — |
| **Subtotal** | | **18 files** | **—** |

### Grand Total

| Metric | Count |
|---|---|
| Total new/modified files | ~119 |
| Total new test cases | ~404 |
| Calendar weeks | 8 |
| Tracks | 10 (A through J) |

---

## 13. Success Criteria

| Metric | Current | Target | Verification Command |
|---|---|---|---|
| Go coverage (overall) | 72.8% | >= 95% | `go test -coverprofile=c.out ./... && go tool cover -func=c.out \| grep total` |
| Go coverage (per pkg/) | varies | >= 95% each (excl. postgres, build-tagged) | `go test -cover ./pkg/XYZ/...` |
| Frontend page unit tests | 6/19 | 19/19 | `npx vitest run` |
| Frontend component tests | 0/6 | 6/6 | `npx vitest run` |
| Frontend hook tests | 0/3 | 3/3 | `npx vitest run` |
| Frontend utility tests | 0/3 | 3/3 | `npx vitest run` |
| E2E Playwright specs | 13 specs | 22 specs (19 pages + 3 flows) | `npx playwright test` |
| Eval framework coverage | 73-99% | >= 99% across 12 sub-packages | `go test -cover ./pkg/eval/...` |
| Integration tests | 2 | 5 | `go test -run TestIntegration ./pkg/orchestration/...` |
| API smoke tests | 0 | 8 | `go test -run TestSmoke ./cmd/care/...` |
| Agent test files | 48/95 | 95/95 | `find agents/ -name "*_test.go" \| wc -l` |
| i18n page coverage | 1/19 | 19/19 | `grep -rl "useI18n" src/pages/ \| wc -l` |
| WCAG AA violations | ~15-20 | 0 | axe-core automated scan |
| CI jobs | 3 (build, vuln, docker) | 6 (+frontend, +e2e, +security) | `.github/workflows/ci.yml` |
| Clinical decision safeguards | 0 confirmations | 100% of irreversible actions | Manual audit |
| Dark mode coverage | ~60% | 100% | Visual audit in dark mode |
| `go vet ./...` | clean | clean | `make vet` |
| `go build ./...` | clean | clean | `make build` |
| `npx tsc --noEmit` | clean | clean | Type check |

---

## 14. Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Testcontainers unavailable in CI | Medium | High | Use `testing.Short()` skip; separate CI job with Docker service |
| E2E tests flaky due to timing | High | Medium | Use Playwright built-in waits; `retries: 1` in CI; avoid `waitForTimeout` |
| Frontend tests break on refactor | Medium | Medium | Use `@testing-library` queries by role/text, not CSS; test behavior |
| Coverage drops when adding new code | Ongoing | Medium | CI gate enforces minimum; require tests in same PR |
| Build-tagged packages unreachable | Low | Low | Excluded from gate; tested separately with `make test-otel` |
| LLM-dependent tests need API keys | High | High | Mock `llm.Client` for all unit tests; real LLM tests opt-in with `-tags integration` |
| i18n key explosion (250 keys × 4 locales) | Medium | Medium | Automated lint for missing keys; CI check for untranslated strings |
| Modal confirmation adds friction | Low | Medium | 2-second delay only for `urgent` decisions; skip for routine approvals if clinician preference set |
| RunTrace memory from unbounded events | Medium | High | Cap at 500 events with virtualized scroll; "load earlier" button |
| WebSocket permanently disconnected | Low | Medium | Global connection indicator; max retry count with "reconnect" button |

---

## Conventions

- Go test files: `*_test.go` alongside source
- Frontend unit tests: `__tests__/*.unit.tsx` (Vitest, jsdom)
- E2E tests: `e2e/*.spec.ts` (Playwright, Chromium)
- No new Go dependencies — use `testing` stdlib + existing helpers
- Frontend tests use existing `@testing-library/react`, `vitest`, `jest-dom`
- All mock data is deterministic — no flaky randomness
- PHI redaction: `sha256(caseID)[:8]` hex — no raw MRNs in test assertions or logs
- API keys: `.env` only — never in tracked files or code
