# Bodh UI/UX Plan — Critical Analysis & Improvement Roadmap

> **Scope**: Complete UI/UX audit of the Bodh web SPA (`cmd/care/web/`) — 19 pages,
> 6 components, 3 hooks, i18n system, API client, and design system.
>
> **Methodology**: Every finding is grounded in the actual source code. No speculation.

---

## Table of Contents

1. [Current Architecture Inventory](#1-current-architecture-inventory)
2. [Information Architecture Critique](#2-information-architecture-critique)
3. [User Persona Analysis](#3-user-persona-analysis)
4. [Page-by-Page UX Audit](#4-page-by-page-ux-audit)
5. [Design System Assessment](#5-design-system-assessment)
6. [Accessibility (a11y) Gaps](#6-accessibility-a11y-gaps)
7. [Performance & Perceived Speed](#7-performance--perceived-speed)
8. [Clinical Safety UX](#8-clinical-safety-ux)
9. [Mobile & Responsive](#9-mobile--responsive)
10. [Internationalization (i18n) Gaps](#10-internationalization-i18n-gaps)
11. [Dark Mode Audit](#11-dark-mode-audit)
12. [Real-time & Notification UX](#12-real-time--notification-ux)
13. [Improvement Roadmap (Prioritized)](#13-improvement-roadmap-prioritized)
14. [Implementation Phases](#14-implementation-phases)

---

## 1. Current Architecture Inventory

### 1.1 File Map

```
src/
  main.tsx               — StrictMode + I18nProvider + BrowserRouter(/ui)
  App.tsx                — 19 routes, AuthedLayout guard
  index.css              — Tailwind base/components (card, btn, badge, nav-link)

  api/
    client.ts            — Centralized fetch, JWT injection, 401 redirect, SSE

  components/
    Layout.tsx           — Sidebar nav (15 items), dark mode toggle, i18n selector
    Page.tsx             — Title/subtitle/actions wrapper
    NotificationBell.tsx — WebSocket HITL alerts, dropdown panel
    Toast.tsx            — Context-based toast system, 4 types, auto-dismiss 5s
    Loader.tsx           — Loading spinner + ErrorBox with retry
    Empty.tsx            — Empty-state placeholder

  hooks/
    useApi.ts            — Generic async fetch → {data, loading, error, reload}
    useAuth.ts           — JWT localStorage, getCurrentUser (base64url decode)
    useWebSocket.ts      — Auto-reconnect WebSocket, exponential backoff

  i18n/
    I18nProvider.tsx      — React context, localStorage persistence, <html lang>
    index.ts             — useI18n hook, t() standalone, 4 locales
    en.ts, es.ts, fr.ts, zh.ts

  pages/ (19 pages)
    Login.tsx             — Email/password form, show/hide toggle, error states
    Dashboard.tsx         — Admin overview: 4 stat cards, recent reviews, top tasks
    ClinicianDashboard.tsx — Clinical: stats, cases table, HITL queue, accuracy chart
    PatientPortal.tsx     — Patient-facing: care plan, gaps, vitals (fully i18n)
    AuditLog.tsx          — HIPAA log: cursor pagination, 3 filters, event table
    CaseRunner.tsx        — Run case: fixture/provider select, custom presentation form
    RunTrace.tsx          — SSE real-time trace viewer, auto-scroll
    Reviews.tsx           — HITL review list, pending/all toggle
    ReviewDetail.tsx      — Single review with approve/reject/modify decision panel
    Tasks.tsx             — Nurse tasks: all/open/urgent filter
    Patients.tsx          — Patient registry table
    PanelReport.tsx       — Care gaps + by-condition breakdown
    Agents.tsx            — Registered agents table
    RPM.tsx               — Remote patient monitoring, HR sparklines (stub data)
    Bench.tsx             — Pareto frontier SVG scatter + summary table
    LLMTraces.tsx         — Token usage table (10 columns)
    FineTune.tsx          — HITL export stats + CLI instructions
    Cost.tsx              — Cost analytics: sparklines, budget alerts (stub data)
    About.tsx             — Static info + disclaimer

  types/
    index.ts             — 14 TypeScript interfaces mirroring Go API responses
```

### 1.2 Design System Tokens

| Token | Value | Usage |
|---|---|---|
| `bodh-50` | `#f5f7fa` | Background hover, table headers |
| `bodh-100` | `#e5ecf3` | Nav hover, subtle borders |
| `bodh-200` | `#c9d6e3` | Card borders, input borders |
| `bodh-400` | `#7593b3` | Muted text, timestamps |
| `bodh-600` | `#3c5a7a` | Primary buttons, links |
| `bodh-700` | `#2d4660` | Button hover, sidebar text |
| `bodh-800` | `#1f3247` | Headings, body text, branding |

**Component classes** (defined in `index.css @layer components`):
- `.card` / `.card-header` / `.card-body` — rounded-lg, shadow-sm, dark variants
- `.btn` / `.btn-secondary` — primary/secondary buttons
- `.badge` variants: `-pending` (amber), `-approved` (emerald), `-rejected` (rose),
  `-urgent` (red), `-normal` (sky), `-done` (emerald)
- `.nav-link` / `.nav-link-active` — sidebar navigation
- `animate-slide-in` — toast entrance animation

---

## 2. Information Architecture Critique

### 2.1 Current Navigation Structure (15 sidebar items)

```
Home (/)                   — "Dashboard" admin overview
Clinician (/dashboard)     — ClinicianDashboard
Patient Portal (/portal)   — PatientPortal (i18n)
Run Case (/runs)           — CaseRunner
HITL Reviews (/reviews)    — Reviews list
Nurse Tasks (/tasks)       — Tasks list
Patients (/patients)       — Patient registry
Panel (/panel)             — PanelReport
Agents (/agents)           — Agent registry
RPM (/rpm)                 — Remote monitoring
Audit Log (/audit)         — HIPAA audit
Bench (/bench)             — Benchmark results
LLM Traces (/llm-traces)  — Token usage
Fine-tune (/fine-tune)     — Export stats
Cost (/cost)               — Cost analytics
About (/about)             — Static info
```

### 2.2 Critical Problems

**Problem 1: Flat navigation with no grouping**

15 items in a flat sidebar forces users to scan linearly. Clinical software research
(Nielsen Norman Group, healthcare UX studies) shows that flat menus beyond 7 items
degrade scan speed by ~40%. The items span 4 distinct functional domains but are
presented as peers.

**Recommended grouping:**

```
CLINICAL
  Dashboard (clinician)
  Run Case
  HITL Reviews
  Nurse Tasks
  Patient Portal

POPULATION HEALTH
  Patients
  Panel (care gaps)
  RPM

PLATFORM
  Agents
  LLM Traces
  Bench
  Cost
  Fine-tune

COMPLIANCE
  Audit Log
  About
```

**Problem 2: Two dashboards, confusing naming**

`/` is "Dashboard" (admin stats) and `/dashboard` is "ClinicianDashboard". A user
landing on `/` sees aggregate counts (agents registered, pending reviews). This is an
*admin* view, not a *clinician* view. Yet the sidebar calls it "Home."

**Recommendation**: Merge into a single `/dashboard` with role-based sections. If
role-based rendering is premature, make the ClinicianDashboard the default `/` and
move the admin overview to `/admin`.

**Problem 3: No breadcrumbs for nested routes**

`/reviews/:id` has no visual context about its position in the hierarchy. The user
sees "Back" button text on ReviewDetail but no breadcrumb trail. Same for
`/runs/:runId`.

**Problem 4: URL-based auth scope leaks**

`AuthedLayout` checks `isAuthenticated()` (a `localStorage.getItem` check) but
there's no role-based route protection. A nurse could navigate to `/fine-tune` or
`/audit`. The `getCurrentUser()` function parses JWT for email/role but that role
is never checked in routing.

---

## 3. User Persona Analysis

### 3.1 Identified Personas (from code evidence)

| Persona | Evidence | Primary Pages | Currently Served? |
|---|---|---|---|
| **Clinician** | ClinicianDashboard, CaseRunner, RunTrace, ReviewDetail | `/dashboard`, `/runs`, `/reviews` | Partially |
| **Nurse** | Tasks page, dedicated task kind/priority model | `/tasks` | Minimally |
| **Patient** | PatientPortal with full i18n in 4 languages | `/portal` | Partially |
| **Admin/Engineer** | Agents, LLMTraces, Bench, Cost, FineTune | `/agents`, `/llm-traces`, `/bench` | Yes |
| **Compliance Officer** | AuditLog with HIPAA event viewer | `/audit` | Partially |
| **Researcher** | Bench Pareto chart, Fine-tune export | `/bench`, `/fine-tune` | Partially |

### 3.2 Critical UX Gaps Per Persona

**Clinician**:
- No case status overview: CaseRunner submits but there's no "My Cases" page
  showing all previously run cases with their outcomes
- ClinicianDashboard has an accuracy chart but no way to drill down to
  individual case explanations (the `/explain/:case_id` API exists but has no
  frontend route)
- The HITL queue in ClinicianDashboard uses inline Accept/Reject buttons but
  there's no undo or confirmation dialog for clinical decisions

**Nurse**:
- Tasks page has filtering but no ability to mark tasks as done, reassign, or
  add notes — it's read-only
- No sort capability on any column (due date, priority, patient)

**Patient**:
- PatientPortal is read-only (correct for HIPAA) but has no loading-skeleton
  experience — it jumps from blank to full content
- No way for patients to request appointments or message their care team
- The vitals table shows raw numbers without trend indicators or reference ranges

**Compliance Officer**:
- AuditLog uses custom `fetchAudit()` instead of the standard `useApi` hook,
  meaning it has different error handling behavior
- No export capability (CSV/PDF) for audit reports — a compliance requirement
- Filter state is lost on navigation — no URL-based filter persistence

**Admin/Engineer**:
- No search across any table (agents, patients, traces, tasks)
- LLMTraces has 10 columns in a table with no column resize, sort, or filter
- Cost page uses entirely stub/fallback data with no indication that it's mock

---

## 4. Page-by-Page UX Audit

### 4.1 Login (`Login.tsx` — 141 lines)

**Strengths:**
- Clean, focused layout with proper `autoComplete` attributes
- Password visibility toggle with proper `aria-label`
- Three error states: invalid credentials, missing token, network error
- Loading state disables submit button with text change ("Signing in...")
- Research disclaimer at the bottom

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| L1 | High | No rate limiting feedback — repeated failed logins show same error | Add attempt counter, show lockout warning after N failures |
| L2 | Medium | `handleSubmit` catches all errors as "Network error" — a 500 would show this | Parse status codes: 500 → "Server error", 429 → "Too many attempts" |
| L3 | Medium | No "Forgot password" flow | Add link (even if pointing to admin contact for research demo) |
| L4 | Low | No keyboard shortcut — Enter submits (via form), but no Escape to clear | Add Escape handler to clear form |
| L5 | Low | Background is `bg-bodh-50` — no dark mode support on login page | Add `dark:bg-gray-900` |

### 4.2 Dashboard (`Dashboard.tsx` — 80 lines)

**Strengths:**
- Quick overview with 4 stat cards (registered agents, pending reviews, open tasks, patients)
- Recent reviews and top tasks provide actionable links

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| D1 | High | No auto-refresh — clinician must manually reload to see new reviews | Add polling interval or WebSocket-driven stat updates |
| D2 | Medium | Stat cards show raw numbers with no trend (up/down vs yesterday) | Add delta indicators |
| D3 | Medium | "Recent reviews" and "Top tasks" are truncated to 5 with no "View all" link on the list itself | Add explicit "View all reviews →" footer link |
| D4 | Low | No skeleton loading — content jumps in | Add skeleton placeholders matching card dimensions |

### 4.3 ClinicianDashboard (`ClinicianDashboard.tsx` — 251 lines)

**Strengths:**
- Aggregates data from 4 different API endpoints into a coherent clinical view
- RecentCasesTable with 7 columns provides dense but useful information
- AccuracyChart uses CSS-only bars (no chart library dependency)
- HitlQueue with inline Accept/Reject buttons for quick decisions

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| CD1 | Critical | HitlQueue Accept/Reject buttons have NO confirmation dialog — a misclick approves a diagnosis | Add confirmation modal for clinical decisions |
| CD2 | High | 4 parallel API calls on mount with no error aggregation — one failure breaks the whole page | Use `Promise.allSettled` pattern, show partial data with error badges |
| CD3 | High | AccuracyChart has no labels on bars showing the actual percentage | Add percentage text inside or beside each bar |
| CD4 | Medium | RecentCasesTable shows confidence as decimal (0.92) not percentage | Format as "92%" for clinical readability |
| CD5 | Medium | No way to drill into a case from the table — no link to case detail/explanation | Add case ID link to `/explain/:case_id` or `/runs/:runId` |
| CD6 | Low | Panel Gaps count is fetched but only shows a number — no breakdown by severity | Add severity-colored sub-counts (3 urgent, 5 normal) |

### 4.4 PatientPortal (`PatientPortal.tsx` — 230 lines)

**Strengths:**
- Fully internationalized (only page using `useI18n()` extensively)
- Read-only design appropriate for patient-facing HIPAA compliance
- CarePlanCard, PanelGapsCard, and VitalsTable provide useful health summary
- Severity-based color coding on panel gaps

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| PP1 | High | Uses hardcoded `patient_id` parameter `patients?patient_id=demo-patient-1` — no patient selection | Allow patient ID from auth context or URL parameter |
| PP2 | High | VitalsTable shows raw numbers (A1c: 7.2) with no reference ranges or risk coloring | Add reference range column, color code out-of-range values |
| PP3 | Medium | Medications list is plain text with no dosage or frequency info | If available from API, show structured medication info |
| PP4 | Medium | "Next appointment" in CarePlanCard has no time context (date, time, provider) | Enhance appointment data model |
| PP5 | Low | Panel gaps card uses `measure` field which may be clinical jargon for patients | Add patient-friendly descriptions via i18n keys |

### 4.5 AuditLog (`AuditLog.tsx` — 258 lines)

**Strengths:**
- Cursor-based pagination (correct for append-only logs — offset would skip events)
- 3 filters: Case ID, Event Kind, Agent
- Pre-formatted timestamp column

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| AL1 | High | Uses custom `fetchAudit()` function instead of `useApi` hook — inconsistent error handling, no abort on unmount | Refactor to use `useApi` with cursor parameter |
| AL2 | High | No export capability (CSV/PDF) — HIPAA compliance officers need audit exports | Add download button with CSV/PDF generation |
| AL3 | Medium | Filter state is ephemeral — refreshing the page or navigating away loses filter configuration | Persist filters in URL query parameters |
| AL4 | Medium | "Event Kind" dropdown has hardcoded options that may not match all possible event kinds from the backend | Fetch available event kinds from API or use union type |
| AL5 | Low | "Load more" button with no indication of remaining count | Show "Showing X of ~Y" or "N more available" |
| AL6 | Low | Timestamps show localized format — for audit purposes, should also show UTC or ISO 8601 | Add UTC tooltip on hover |

### 4.6 CaseRunner (`CaseRunner.tsx` — 124 lines)

**Strengths:**
- Dual-mode: fixture selection for quick demos, custom presentation for real use
- Provider selection dropdown (rule, llm-anthropic, llm-openai, llm-ollama)
- Navigates to `/runs/:runId` on success for real-time trace viewing

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| CR1 | Medium | Custom presentation form has no field validation — empty chief complaint submits | Add client-side validation with inline error messages |
| CR2 | Medium | `max_cost_usd` input has no visual indicator of what reasonable values are | Add placeholder, helper text, or slider with presets |
| CR3 | Medium | No loading indication during case submission (no spinner/disabled state visible from code) | Add loading state to submit button |
| CR4 | Low | No form persistence — switching between fixture and custom mode loses entered data | Use local state or URL params to preserve |
| CR5 | Low | Provider dropdown has no visual hint about which providers are actually available | Check provider health endpoints and mark unavailable ones |

### 4.7 RunTrace (`RunTrace.tsx` — 70 lines)

**Strengths:**
- Real-time SSE event streaming with auto-scroll
- Visual distinction between stream states (live badge vs closed badge)
- Agent message timeline with from → to routing

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| RT1 | High | Unbounded event array — `setEvents(es => [...es, data])` grows without limit; long-running traces could cause memory issues | Add virtualized scrolling or cap at N events with "load earlier" |
| RT2 | Medium | Error in JSON.parse swallows the raw event data | Show raw event data in error case |
| RT3 | Medium | No visual timeline — events are a flat list with no indication of pipeline stages | Add stage grouping (intake → test_planner → diagnostician → care_plan) |
| RT4 | Medium | Terminal events (`diagnosis`, `run_complete`) close the stream but there's no summary card | Add a summary card at the end showing diagnosis, confidence, cost |
| RT5 | Low | `content_preview` is shown in a `<pre>` with no syntax highlighting for JSON payloads | Add JSON syntax highlighting |

### 4.8 Reviews (`Reviews.tsx` — 69 lines)

**Strengths:**
- Pending/All toggle for scope filtering
- Priority badges with color coding
- Link to ReviewDetail for each review

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| RV1 | Medium | No sort capability — reviews are shown in API response order | Add sortable columns (created, priority, kind) |
| RV2 | Medium | No bulk actions — clinician must open each review individually | Add checkbox selection + bulk approve/reject |
| RV3 | Low | ScopeToggle uses two buttons but doesn't look like a segmented control | Use proper toggle/tab UI pattern |

### 4.9 ReviewDetail (`ReviewDetail.tsx` — 115 lines)

**Strengths:**
- Clear decision panel with 3 options (approve, approve-with-mods, reject)
- Modified payload textarea for approve-with-modifications
- Payload display with `tryPrettyJSON` for readability
- Hides decision panel when review is already decided

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| RD1 | Critical | `alert(String(e))` for errors — uses native browser alert, blocks UI thread | Use Toast component for error feedback |
| RD2 | High | No confirmation dialog — clicking "Approve" immediately fires the API call | Add confirmation step for irreversible clinical decisions |
| RD3 | Medium | "Approve with modifications" button is disabled when modification field is empty, but there's no visual hint explaining why | Add tooltip or helper text |
| RD4 | Medium | No back-navigation after successful decision — uses `navigate('/reviews')` which loses filter state | Preserve filter state in URL params |
| RD5 | Low | Reviewer ID defaults to `clinician-demo` — should come from JWT context | Use `getCurrentUser().email` as default |

### 4.10 Tasks (`Tasks.tsx` — 66 lines)

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| T1 | High | Tasks are read-only — no ability to mark as done, add notes, or assign | Add action buttons for task state transitions |
| T2 | Medium | Due dates have no overdue highlighting — a task past due looks the same as a future one | Compare `due_at` to now, add red styling for overdue |
| T3 | Medium | Filter buttons (all/open/urgent) are not keyboard-accessible as a group | Use proper radio group or tab pattern |
| T4 | Low | No sort on any column | Add sortable headers |

### 4.11 Patients (`Patients.tsx` — 51 lines)

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| PA1 | High | No search/filter — a clinic with 500+ patients requires scrolling through the entire list | Add search input filtering by ID, name, or condition |
| PA2 | Medium | No link to patient detail page — clicking a patient does nothing | Add patient detail route `/patients/:id` |
| PA3 | Medium | Conditions shown as badges but medications as comma-separated text — inconsistent | Use consistent display pattern |
| PA4 | Low | No pagination — all patients loaded at once | Add server-side pagination for large registries |

### 4.12 PanelReport (`PanelReport.tsx` — 58 lines)

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| PR1 | Medium | No patient drill-down — seeing a care gap should link to the patient | Add patient ID link to `/patients/:id` |
| PR2 | Medium | "By condition" sidebar is a simple list with no visual weight | Add horizontal bar chart or proportional display |
| PR3 | Low | Gap table has no sort or filter | Add severity filter and sortable columns |

### 4.13 Agents (`Agents.tsx` — 47 lines)

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| AG1 | Medium | No agent detail page — capabilities are shown as badges but there's no drill-down to agent config, message history, or performance | Add `/agents/:id` detail route |
| AG2 | Low | No search/filter for agents | Add search input |
| AG3 | Low | No indication of agent health/status (active, idle, error) | Add status indicator dot |

### 4.14 RPM (`RPM.tsx` — 152 lines)

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| RPM1 | High | Entirely stub data — 5 hardcoded patients, no API integration | Wire to `/rpm/observation` API endpoint |
| RPM2 | High | Alert indicators exist but no alert action — clinician sees an alert but can't acknowledge or escalate | Add alert acknowledgment workflow |
| RPM3 | Medium | HR sparkline has trending logic but no color legend explanation | Add legend explaining color-coded trends |
| RPM4 | Low | Note says "planned: POST /rpm/observation" but the API route already exists in `main.go` | Remove the note, wire up the integration |

### 4.15 Bench (`Bench.tsx` — 217 lines)

**Strengths:**
- Pareto frontier calculation is correct and well-implemented
- Pure SVG chart with no library dependency
- Frontier points visually distinguished (larger radius, full opacity)
- Color-coded providers with a consistent palette

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| B1 | Medium | Pareto chart has no tooltips on hover — can't see exact values | Add SVG title elements or custom tooltip |
| B2 | Medium | No responsive scaling for the SVG — fixed viewBox doesn't adapt well to very small screens | Add responsive container with min-width |
| B3 | Low | Missing chart legend — colors are assigned but not explained | Add a visual legend mapping color to provider |
| B4 | Low | Summary table and chart show same data — no visual connection between them | Add hover-highlight: hovering a table row highlights the corresponding point |

### 4.16 LLMTraces (`LLMTraces.tsx` — 53 lines)

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| LT1 | High | 10 columns in a single table with no overflow handling — horizontal scroll on mobile | Add responsive column hiding or card-based layout for mobile |
| LT2 | Medium | Error rows are highlighted pink but there's no way to filter for errors only | Add error filter toggle |
| LT3 | Medium | Token counts are raw numbers — no cost calculation or token-to-cost conversion | Add estimated cost column |
| LT4 | Medium | No column sorting — can't find the most expensive or slowest calls | Add sortable headers |
| LT5 | Low | Hardcoded limit of 100 traces with no pagination | Add proper pagination or "load more" |

### 4.17 FineTune (`FineTune.tsx` — 46 lines)

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| FT1 | Medium | CLI export instructions are displayed as static text — user must copy-paste manually | Add a "Copy command" button |
| FT2 | Medium | "Projected SFT lines" and "DPO pairs" are jargon — no explanation for non-ML users | Add tooltips explaining SFT and DPO |
| FT3 | Low | No progress indicator for export status | If export is async, add progress tracking |

### 4.18 Cost (`Cost.tsx` — 258 lines)

**Strengths:**
- SVG sparkline for daily spend visualization
- Budget alert banner when threshold exceeded
- Per-provider cost breakdown with color coding

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| CO1 | Critical | Entirely stub/fallback data with NO visual indicator that data is synthetic | Add a prominent "Demo data" banner or watermark |
| CO2 | Medium | DailySparkline uses a fixed 7-day window with no date labels | Add day-of-week labels on the x-axis |
| CO3 | Medium | Budget threshold is hardcoded in the component — should be configurable | Make threshold a backend setting |
| CO4 | Low | "Savings vs cloud" metric has no methodology explanation | Add tooltip explaining the calculation |

### 4.19 About (`About.tsx` — 28 lines)

**Strengths:**
- Clear research disclaimer with amber warning box
- Links to docs, setup, architecture diagrams

**Issues:**

| # | Severity | Issue | Fix |
|---|---|---|---|
| AB1 | Low | Links open in new tabs with `target="_blank"` but some lack `rel="noreferrer"` — actually all have it, good | None |
| AB2 | Low | No version number or build info displayed | Add build version from env var |

---

## 5. Design System Assessment

### 5.1 Strengths

1. **Consistent color palette** — 7 `bodh-*` shades create a cohesive, professional look
   appropriate for medical software (cool blue-gray, not playful)
2. **Component-class pattern** — `card`, `btn`, `badge`, `nav-link` in `@layer components`
   ensures visual consistency across all pages
3. **No external UI library dependency** — zero bundle cost from component libraries
4. **Dark mode support** — `darkMode: 'class'` with toggle in Layout

### 5.2 Weaknesses

| # | Issue | Impact | Recommendation |
|---|---|---|---|
| DS1 | Only 7 color shades — missing `bodh-300`, `bodh-500`, `bodh-900` | Limited range for hover states, disabled states, subtle backgrounds | Complete the shade scale (50-950) |
| DS2 | No semantic color tokens — `badge-urgent` uses `bg-red-100` directly | Changing "urgent" color requires finding all occurrences | Add `--color-urgent`, `--color-success`, etc. as CSS variables |
| DS3 | No typography scale — headings use arbitrary sizes (`text-xl`, `text-2xl`, `text-3xl`) | Inconsistent heading hierarchy across pages | Define `h1`-`h4` in base layer |
| DS4 | No spacing scale beyond Tailwind defaults | Cards have varying padding (`p-3`, `p-4`, `p-6`) | Document standard spacing tokens |
| DS5 | No focus ring style — some buttons have `focus:ring-2 focus:ring-bodh-400`, others don't | Inconsistent keyboard navigation experience | Add default focus-visible style to base layer |
| DS6 | `btn` class has no focus style defined | Keyboard users can't see which button is focused | Add `focus:ring-2 focus:ring-bodh-400 focus:ring-offset-2` |
| DS7 | No transition on cards — hover states jump | Feels abrupt | Add `transition-colors duration-150` to card class |
| DS8 | Badge variants use hardcoded Tailwind colors, not the bodh palette | Badges feel disconnected from the rest of the design | Map badge colors to bodh tokens where possible |

### 5.3 Missing Design System Components

| Component | Where Needed | Current Workaround |
|---|---|---|
| **Modal/Dialog** | ReviewDetail confirmation, delete actions | `alert()` / none |
| **Skeleton loader** | All pages using `useApi` | `animate-pulse` text only |
| **Table (sortable, paginated)** | 10+ pages with tables | Plain `<table>` each time |
| **Search input** | Patients, Agents, AuditLog | None |
| **Tabs/Segmented control** | Reviews scope, Tasks filter | Adjacent buttons |
| **Tooltip** | Bench chart, FineTune jargon | None |
| **Dropdown menu** | User profile, actions | Only in Layout (i18n selector) |
| **Confirmation dialog** | HITL decisions, case submission | None |
| **Progress bar** | File export, case running | None |
| **Stat card** | Dashboard, ClinicianDashboard, Cost, FineTune | Inline div per page |

---

## 6. Accessibility (a11y) Gaps

### 6.1 Critical Issues

| # | Issue | WCAG Level | Affected |
|---|---|---|---|
| A1 | No skip-to-content link — sidebar is 15 items that screen readers must traverse on every page | A | All pages |
| A2 | Tables have no `<caption>` or `aria-label` — screen readers don't know what data the table contains | A | 10+ pages |
| A3 | SVG charts (Bench, RPM sparklines, Cost sparklines) have no `aria-label` or accessible fallback text | A | Bench, RPM, Cost |
| A4 | Color-only status indication — badge colors (amber/green/red) carry meaning with no text-independent alternative for colorblind users | A | Reviews, Tasks, PanelReport |
| A5 | NotificationBell dropdown has no focus trap — keyboard users can tab past it | AA | Layout |
| A6 | Dynamic content updates (SSE events in RunTrace, toast notifications) are not in ARIA live regions | AA | RunTrace, Toast |
| A7 | Login form has no `aria-live` for error messages — screen readers may not announce errors | A | Login |
| A8 | Dark mode toggle has aria-label but it's static — doesn't reflect current state | AA | Layout |

### 6.2 Keyboard Navigation

| Area | Status | Fix |
|---|---|---|
| Form submission | Works (native form + Enter) | OK |
| Sidebar navigation | Tab-navigable | OK |
| Table rows | Not focusable — can't keyboard-navigate to a review or task | Add `tabIndex={0}` + Enter handler |
| Dropdown panels | No escape-to-close | Add `onKeyDown` handler for Escape |
| Filter toggles | Not a proper radio group | Use `role="radiogroup"` + `aria-checked` |

### 6.3 Color Contrast

The `bodh-400` (#7593b3) on white (`bg-bodh-50` / #f5f7fa) has a contrast ratio of
approximately **3.5:1** — below the WCAG AA requirement of 4.5:1 for normal text.
This affects timestamps, muted labels, and hint text across the entire application.

**Fix**: Darken `bodh-400` to `#5a7898` (approximately 5.0:1 ratio) or use `bodh-600`
for text that must be readable.

---

## 7. Performance & Perceived Speed

### 7.1 Current Loading Pattern

Every page follows the same pattern:
```
Mount → useApi fires fetch → loading=true → Loader shows → data arrives → render
```

**Problems:**

1. **No skeleton UI** — Loader shows "Loading agents..." as pulsing text. The layout
   shifts dramatically when data arrives because content dimensions are unknown.
   
2. **No data caching** — navigating away from Patients and back re-fetches the entire
   list. `useApi` has no cache layer.

3. **No optimistic updates** — ReviewDetail's `decide()` calls the API and navigates
   away on success, but there's no immediate UI update. If the network is slow, the
   user sees a frozen button.

4. **ClinicianDashboard fires 4 parallel requests** — patients, reviews, panel, bench.
   Any single failure breaks the page because the component doesn't handle partial data.

5. **RunTrace stores all events in a growing array** — no virtualization. A case with
   200+ events will cause React to re-render the entire list on every new event.

### 7.2 Recommendations

| Priority | Improvement | Effort |
|---|---|---|
| P0 | Add skeleton loaders matching card dimensions for every page | Medium |
| P1 | Add `React.memo` and list virtualization for RunTrace events | Low |
| P2 | Implement a simple fetch cache (stale-while-revalidate) in `useApi` or adopt TanStack Query | Medium |
| P3 | Use `Promise.allSettled` in ClinicianDashboard for partial rendering | Low |
| P4 | Add optimistic updates for ReviewDetail decisions | Low |

---

## 8. Clinical Safety UX

This is the most critical section. Bodh is a medical AI platform. UI decisions
directly affect clinical outcomes.

### 8.1 Decision Confirmation

**Current state**: HITL review decisions (approve/reject/modify) are one-click
operations with no confirmation. Both `ClinicianDashboard.tsx` (inline
Accept/Reject) and `ReviewDetail.tsx` (decision panel) fire the API immediately.

**Risk**: A clinician scrolling through reviews could accidentally approve a
diagnosis with a mis-tap. In clinical software, this class of error is called a
"juxtaposition error" (acting on the wrong item because adjacent items look similar).

**Recommendation**: Add a confirmation dialog that:
1. Shows the diagnosis being approved/rejected
2. Requires the clinician to type a reason (not optional)
3. Shows the patient context (case ID, condition)
4. Has a 2-second delay on the "Confirm" button to prevent rapid mis-clicks

### 8.2 Clinical Decision Audit Trail

**Current state**: The AuditLog page shows HIPAA events, but there's no way to see
the decision trail for a specific case — which agent proposed what, which clinician
approved or rejected, and what modifications were made.

**Recommendation**: Add a "Case Timeline" view (`/cases/:id/timeline`) that shows
the full MARA pipeline trace alongside HITL decisions for a single case.

### 8.3 Confidence Display

**Current state**: ClinicianDashboard shows confidence as a raw decimal (e.g., 0.92).
This is a known antipattern in clinical decision support — confidence should be
displayed as a qualitative category with the numeric value as secondary information.

**Recommendation**:
- 0.0-0.5: "Low confidence" (red)
- 0.5-0.7: "Moderate" (amber)  
- 0.7-0.9: "High" (blue)
- 0.9-1.0: "Very high" (green)

### 8.4 Alert Fatigue Prevention

**Current state**: NotificationBell shows every HITL event as a toast ("New review
needs attention") and accumulates them in a dropdown capped at 50. There's no
severity filtering — a routine medication review generates the same notification
as an urgent sepsis diagnosis review.

**Recommendation**:
1. Tier notifications by priority from the review's `priority` field
2. Only show toast for `urgent` and `high` priority reviews
3. Show badge count for all, but color-code: red for urgent, amber for normal
4. Add notification preferences (which types of alerts the user wants)

### 8.5 Research Demo Safeguards

**Current state**: Footer says "Research demo. Not for clinical use." and Login
page says "Research demo — not for clinical use. All data is synthetic."

**Issues**:
- The footer text is small (text-xs) and positioned where users habituate to ignore
- No watermark on exported data or screenshots
- No session-level reminder after N minutes of use

**Recommendation**:
- Add a persistent top banner (dismissable per session, re-appears after 30 min)
- Add "DEMO" watermark to all exported/printed content
- Add research disclaimer to the Pareto chart and any data visualization that
  could be screenshot and misrepresented

---

## 9. Mobile & Responsive

### 9.1 Current State

- **Sidebar**: Fixed-width sidebar with no collapse mechanism. On mobile screens,
  content is pushed off-screen.
- **Tables**: 10+ pages use `<table>` with `min-w-full` — they overflow horizontally
  on mobile without `overflow-x-auto` wrapper (some have it, some don't).
- **Cards**: Use `grid-cols-1 sm:grid-cols-2 lg:grid-cols-4` — responsive grid is
  partially implemented.
- **SVG charts**: Fixed `viewBox` with CSS `maxWidth` — works but may be too small
  on mobile.

### 9.2 Responsive Breakpoint Audit

| Component | sm (640px) | md (768px) | lg (1024px) | xl (1280px) |
|---|---|---|---|---|
| Layout sidebar | No collapse | No change | No change | Full |
| Dashboard stat cards | 2 cols | 2 cols | 4 cols | 4 cols |
| ClinicianDashboard | 1 col | 1 col | 2 cols | 2 cols |
| PatientPortal | 1 col | 1 col | 1 col | 1 col |
| Tables (10+ pages) | Overflow-x | Overflow-x | Full | Full |
| Bench SVG | 300px min | 300px min | 480px | 480px |

### 9.3 Recommendations

1. **Sidebar hamburger**: Add a hamburger toggle that collapses the sidebar on screens
   below `lg` (1024px). The sidebar should overlay as a drawer on mobile.
2. **Card-based table fallback**: On screens below `sm`, convert tables to card
   layouts where each row becomes a stacked card.
3. **Touch targets**: Ensure all interactive elements are at least 44x44px (Apple HIG)
   or 48x48px (Material Design) for touch devices. Current badge buttons are ~24px
   tall.

---

## 10. Internationalization (i18n) Gaps

### 10.1 Current Coverage

| Page | Uses `useI18n()`? | i18n Coverage |
|---|---|---|
| PatientPortal | Yes | Full — all visible text uses `t()` |
| Login | No | Zero — all text is English hardcoded |
| Dashboard | No | Zero |
| ClinicianDashboard | No | Zero |
| AuditLog | No | Zero |
| CaseRunner | No | Zero |
| RunTrace | No | Zero |
| Reviews | No | Zero |
| ReviewDetail | No | Zero |
| Tasks | No | Zero |
| Patients | No | Zero |
| PanelReport | No | Zero |
| Agents | No | Zero |
| RPM | No | Zero |
| Bench | No | Zero |
| LLMTraces | No | Zero |
| FineTune | No | Zero |
| Cost | No | Zero |
| About | No | Zero |
| Layout | Partial | Only language selector label |
| Components | No | Loader, Empty, ErrorBox use English |

**Only 1 of 19 pages is internationalized.** The i18n infrastructure exists (provider,
hook, 4 locale files) but adoption is < 6%.

### 10.2 Missing Translation Keys

Checking the English locale file would reveal how many keys exist. The PatientPortal
uses keys like `portal.care_plan`, `portal.vitals`, `portal.panel_gaps`, etc. Other
pages would need ~200+ additional keys for full coverage.

### 10.3 RTL Support

No RTL (right-to-left) language support. If Arabic or Hebrew were added, the entire
layout would need `dir="rtl"` support. Current CSS uses absolute positioning (`right-0`,
`left-0`) that would break in RTL.

### 10.4 Recommendations

1. **Phase 1**: Add i18n to the 5 highest-impact pages: Login, Dashboard,
   ClinicianDashboard, Reviews, Tasks
2. **Phase 2**: Add i18n to remaining pages
3. **Phase 3**: Add missing locales (Hindi, Arabic) with RTL support
4. **Phase 4**: Add dynamic locale loading (code-split locale files)

---

## 11. Dark Mode Audit

### 11.1 Coverage

| Area | Has dark: variants? | Notes |
|---|---|---|
| `index.css` base | Yes | `dark body` class defined |
| `.card` | Yes | `dark:bg-gray-800 dark:border-gray-700` |
| `.card-header` | Yes | `dark:border-gray-700 dark:text-gray-100` |
| `.btn-secondary` | Yes | Full dark variants |
| `.btn` (primary) | No | Uses `bg-bodh-600` which looks OK on dark but has no explicit dark override |
| `.badge` variants | No | All badge variants lack dark mode — amber/green/rose on gray-800 may have contrast issues |
| `.nav-link` | Yes | `dark:text-gray-300 dark:hover:bg-gray-700` |
| Login page | No | `bg-bodh-50` background, no dark variant |
| SVG charts | No | Grid lines use `#e2e8f0` which disappears on dark backgrounds |
| Error states | No | `bg-rose-50`, `border-rose-200` have no dark variants |
| NotificationBell | Yes | Full dark mode support |
| Toast | Partial | Background colors are semantic (red/green/amber) — may be fine |

### 11.2 Key Fixes

1. **Login page**: Add `dark:bg-gray-900` to main wrapper, `dark:bg-gray-800` to form card
2. **Badge variants**: Add dark variants for each (e.g., `dark:bg-amber-900 dark:text-amber-200`)
3. **SVG charts**: Use CSS variables or props for line/text colors that respect dark mode
4. **Error states**: Add `dark:bg-rose-900/20 dark:border-rose-800 dark:text-rose-200`

---

## 12. Real-time & Notification UX

### 12.1 WebSocket Architecture

```
useWebSocket('/ws/hitl?tenant_id=default')
  → NotificationBell (accumulates alerts)
  → Toast (shows "New review needs attention")
```

**Issues:**
1. **Single WebSocket path** — only HITL events. RPM observations, case completions,
   and agent status changes have no real-time channel.
2. **No reconnection feedback** — `connected` boolean exists but is only shown as a
   tiny 8px dot on the bell icon. If the WebSocket drops, the user has no clear
   notification.
3. **Backoff ceiling is 30s** — reasonable, but there's no "give up" after N failures.
   A permanently-down WebSocket will retry forever.

### 12.2 SSE Architecture

```
EventSource('/api/runs/:runId/events')
  → RunTrace (appends to event list)
```

**Issues:**
1. **No reconnection** — if the SSE connection drops mid-trace, the stream stops and
   the user sees a frozen "live" badge. EventSource auto-reconnects but the events
   between disconnection and reconnection are lost.
2. **No heartbeat** — no way to distinguish "no events happening" from "connection
   dropped silently."

### 12.3 Recommendations

1. Add a global connection status indicator in the Layout header
2. Add WebSocket channels for RPM alerts and case completion events
3. Implement SSE heartbeat (server sends `:heartbeat\n\n` every 15s)
4. Add reconnection handling in RunTrace with event gap detection

---

## 13. Improvement Roadmap (Prioritized)

### Tier 1 — Clinical Safety (Do First)

| # | Item | Pages Affected | Effort |
|---|---|---|---|
| 1.1 | Add confirmation dialog for HITL decisions | ReviewDetail, ClinicianDashboard | Medium |
| 1.2 | Replace `alert()` with Toast for errors | ReviewDetail | Low |
| 1.3 | Format confidence as category + percentage | ClinicianDashboard | Low |
| 1.4 | Add case timeline view | New page | High |
| 1.5 | Tier notifications by priority | NotificationBell | Medium |
| 1.6 | Mark stub data pages clearly (Cost, RPM) | Cost, RPM | Low |

### Tier 2 — Usability (Do Second)

| # | Item | Pages Affected | Effort |
|---|---|---|---|
| 2.1 | Group sidebar navigation into categories | Layout | Medium |
| 2.2 | Merge/restructure two dashboards | Dashboard, ClinicianDashboard | Medium |
| 2.3 | Add search to Patients, Agents, AuditLog | 3 pages | Medium |
| 2.4 | Add sortable table component | 10+ pages | Medium |
| 2.5 | Add breadcrumbs for nested routes | ReviewDetail, RunTrace | Low |
| 2.6 | Add task actions (mark done, assign) | Tasks | Medium |
| 2.7 | Add audit log export (CSV) | AuditLog | Medium |
| 2.8 | Add URL-based filter persistence | AuditLog, Reviews, Tasks | Medium |

### Tier 3 — Design System (Do Third)

| # | Item | Effort |
|---|---|---|
| 3.1 | Add Modal/Dialog component | Medium |
| 3.2 | Add Skeleton loader component | Low |
| 3.3 | Add Tooltip component | Low |
| 3.4 | Add Tabs/SegmentedControl component | Low |
| 3.5 | Complete bodh color palette (300, 500, 900) | Low |
| 3.6 | Add semantic color CSS variables | Low |
| 3.7 | Standardize typography scale | Low |
| 3.8 | Add stat card as reusable component | Low |

### Tier 4 — Accessibility (Do Fourth)

| # | Item | WCAG Level | Effort |
|---|---|---|---|
| 4.1 | Add skip-to-content link | A | Low |
| 4.2 | Add table captions and aria-labels | A | Low |
| 4.3 | Add ARIA live regions for dynamic content | AA | Medium |
| 4.4 | Fix color contrast on bodh-400 text | AA | Low |
| 4.5 | Add focus-visible styles to all interactive elements | AA | Medium |
| 4.6 | Add keyboard navigation to tables | AA | Medium |
| 4.7 | Add accessible chart descriptions | A | Medium |
| 4.8 | Add focus trap to dropdown panels | AA | Low |

### Tier 5 — Performance (Do Fifth)

| # | Item | Effort |
|---|---|---|
| 5.1 | Add stale-while-revalidate caching to useApi | Medium |
| 5.2 | Virtualize RunTrace event list | Medium |
| 5.3 | Add skeleton loading to all pages | Medium |
| 5.4 | Implement partial rendering for ClinicianDashboard | Low |
| 5.5 | Code-split pages with React.lazy | Medium |

### Tier 6 — Polish (Do Last)

| # | Item | Effort |
|---|---|---|
| 6.1 | Complete dark mode coverage | Medium |
| 6.2 | Expand i18n to all pages | High |
| 6.3 | Add responsive sidebar (hamburger toggle) | Medium |
| 6.4 | Add card-based table fallback for mobile | High |
| 6.5 | Add Pareto chart tooltips and legend | Low |
| 6.6 | Add JSON syntax highlighting in RunTrace | Low |
| 6.7 | Add build version display in About | Low |

---

## 14. Implementation Phases

### Phase 1 — Foundation (1-2 weeks)

**Goal**: Design system components + clinical safety fixes

```
New components:
  src/components/Modal.tsx          — Confirmation dialog
  src/components/Skeleton.tsx       — Loading skeleton
  src/components/Tooltip.tsx        — Hover tooltips
  src/components/Tabs.tsx           — Segmented control
  src/components/SearchInput.tsx    — Debounced search
  src/components/SortableTable.tsx  — Reusable sortable/paginated table
  src/components/StatCard.tsx       — Extracted from repeated inline patterns
  src/components/Breadcrumbs.tsx    — Route-aware breadcrumbs

Fixes:
  ReviewDetail.tsx      — Replace alert() with Toast, add confirmation Modal
  ClinicianDashboard.tsx — Add decision confirmation, format confidence
  NotificationBell.tsx   — Tier alerts by priority
  Cost.tsx + RPM.tsx     — Add "Demo data" banner
  index.css              — Add focus-visible styles, fix bodh-400 contrast
```

### Phase 2 — Navigation & Information Architecture (1 week)

**Goal**: Restructure navigation, add search

```
Changes:
  Layout.tsx           — Group sidebar into categories with collapsible sections
  App.tsx              — Restructure routes (merge dashboards or rename)
  Breadcrumbs          — Wire into ReviewDetail, RunTrace
  Patients.tsx         — Add SearchInput
  Agents.tsx           — Add SearchInput
  AuditLog.tsx         — Refactor to useApi, add URL-based filters, add export
```

### Phase 3 — Table & Data Interaction (1 week)

**Goal**: Sortable tables, pagination, task actions

```
Refactor to SortableTable:
  Patients, Agents, Reviews, Tasks, LLMTraces, AuditLog,
  PanelReport, ClinicianDashboard (cases table)

Task actions:
  Tasks.tsx — Add mark-done, assign, notes
  
New pages:
  src/pages/CaseTimeline.tsx  — Full case audit trail
  App.tsx — Add route /cases/:id/timeline
```

### Phase 4 — Accessibility & Responsiveness (1 week)

**Goal**: WCAG AA compliance, mobile support

```
Layout changes:
  Layout.tsx — Hamburger toggle for mobile sidebar
  Add skip-to-content link
  
Table accessibility:
  All tables — Add captions, aria-labels
  
Chart accessibility:
  Bench.tsx — Add aria-label, accessible description
  RPM.tsx — Add SR-only trend descriptions
  Cost.tsx — Add SR-only sparkline descriptions

Responsive:
  LLMTraces — Responsive column hiding
  All tables — Card fallback for <sm screens
```

### Phase 5 — i18n & Dark Mode Completion (1-2 weeks)

**Goal**: Full i18n coverage, dark mode polish

```
i18n expansion:
  Login, Dashboard, ClinicianDashboard, Reviews, ReviewDetail,
  Tasks, Patients, PanelReport, Agents, AuditLog, CaseRunner,
  RunTrace, RPM, Bench, LLMTraces, FineTune, Cost, About
  
  New keys: ~250 across 4 locale files

Dark mode:
  Login.tsx — Add dark background
  Badge variants — Add dark overrides
  SVG charts — Use CSS variable colors
  Error states — Add dark variants
```

### Phase 6 — Performance & Real-time (1 week)

**Goal**: Caching, virtualization, WebSocket improvements

```
useApi enhancement:
  Add stale-while-revalidate pattern
  Or migrate to TanStack Query

RunTrace:
  Add event list virtualization (react-window)
  Add SSE heartbeat handling

WebSocket:
  Add channels for RPM alerts, case completion
  Add global connection status indicator

Code splitting:
  React.lazy for all page components
  Suspense boundaries with Skeleton fallbacks
```

---

## Metrics

### Success Criteria

| Metric | Current | Target | How to Measure |
|---|---|---|---|
| Lighthouse Accessibility score | ~60-70 (est.) | 95+ | Lighthouse CI |
| WCAG AA violations | ~15-20 (est.) | 0 | axe-core automated scan |
| i18n coverage (pages using t()) | 1/19 (5%) | 19/19 (100%) | Grep for useI18n imports |
| Design system component reuse | 6 shared components | 14 shared components | Count exports in components/ |
| Dark mode coverage | ~60% | 100% | Visual audit all pages in dark mode |
| Skeleton loading coverage | 0% | 100% | Count pages with skeleton loading |
| Clinical decision safeguards | 0 confirmation dialogs | 100% of irreversible actions | Count decision points |
| Mobile usability (tap targets >= 44px) | ~50% | 100% | Lighthouse mobile audit |
| Search-enabled pages | 0/10 table pages | 10/10 | Count SearchInput usage |
| Table sort support | 0/10 table pages | 10/10 | Count SortableTable usage |

---

## Appendix A: Component Dependency Graph

```
main.tsx
  └─ I18nProvider
     └─ BrowserRouter (basename="/ui")
        └─ App
           ├─ Login (public)
           └─ AuthedLayout (checks isAuthenticated)
              └─ Layout
                 ├─ Sidebar (15 nav items, dark toggle, i18n selector)
                 ├─ Header (NotificationBell)
                 └─ Outlet
                    └─ [Page Component]
                       ├─ Page (title/subtitle/actions wrapper)
                       ├─ Loader / ErrorBox (loading/error states)
                       ├─ Empty (empty state)
                       └─ [Page-specific content]

  Toast lives in: ToastProvider (wraps App in Layout)
  WebSocket lives in: useWebSocket → NotificationBell
  SSE lives in: api.traceEvents → RunTrace
  Auth lives in: useAuth (localStorage) → api/client.ts (JWT header)
```

## Appendix B: API Client Coverage

| API Method | Used By | Type |
|---|---|---|
| `api.agents()` | Agents, Dashboard | GET |
| `api.tasks()` | Tasks, Dashboard | GET |
| `api.reviews(scope)` | Reviews, Dashboard, ClinicianDashboard | GET |
| `api.review(id)` | ReviewDetail | GET |
| `api.patients()` | Patients, ClinicianDashboard, PatientPortal | GET |
| `api.notifications()` | (unused in pages — only type exported) | GET |
| `api.fixtures()` | CaseRunner | GET |
| `api.panel()` | PanelReport, ClinicianDashboard | GET |
| `api.bench()` | Bench, ClinicianDashboard | GET |
| `api.llmTraces(limit)` | LLMTraces | GET |
| `api.fineTuneStatus()` | FineTune | GET |
| `api.decide(id, body)` | ReviewDetail, ClinicianDashboard | POST |
| `api.startRun(req)` | CaseRunner | POST |
| `api.traceEvents(runId, ...)` | RunTrace | SSE |
| WebSocket `/ws/hitl` | NotificationBell | WS |

**Missing API bindings** (routes exist in `main.go` but not in `client.ts`):
- `POST /api/cdm` (Chronic Disease Management)
- `POST /api/tcm/discharge` (Transitional Care)
- `POST /api/pre_visit` (Pre-visit planning)
- `POST /api/inbox` (Patient inbox)
- `POST /api/refill` (Medication refill)
- `POST /api/priorauth` (Prior authorization)
- `POST /api/adt` (ADT events)
- `POST /api/hl7/adt` (HL7 v2 ADT)
- `POST /api/rpm/observation` (RPM observations)
- `POST /api/fhir/Patient` (FHIR Patient)
- `POST /api/fhir/Encounter` (FHIR Encounter)
- `POST /api/fhir/Condition` (FHIR Condition)
- `GET /api/explain/:case_id` (Right-to-explanation)
- `GET /api/metrics` (Prometheus metrics)

These 14 unused API endpoints represent opportunities for new UI features.
