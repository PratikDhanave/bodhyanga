# Implementation Plan: Remaining Plan Items — UI Polish, Clinical Safety & i18n Completion

**Branch**: `feat/e2e-everything` | **Date**: 2026-06-10 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-remaining-plan-items/spec.md`

## Summary

Complete the remaining Bodh plan.md items across 4 workstreams: (1) Clinical safety UX — confirmation modals for irreversible decisions, confidence display categories, alert fatigue prevention, and research demo safeguards; (2) i18n expansion — translate remaining 15 pages from English-only to all 4 locales (~200 new keys per locale); (3) Accessibility — radiogroup semantics on filter toggles and keyboard navigation on data tables; (4) Responsive — 44px touch targets and card-based table fallback for mobile. All work is frontend-only with no backend changes or external dependencies.

## Technical Context

**Language/Version**: Go 1.26 (backend, unchanged), TypeScript + React 18 (frontend, all changes here)

**Primary Dependencies**: React 18, React Router 6, Tailwind CSS, Vite, Vitest, @testing-library/react, Playwright

**Storage**: sessionStorage (demo banner dismiss state), in-memory React state (DND mode)

**Testing**: Vitest + jsdom for unit tests (252 existing), Playwright for E2E (13 existing specs)

**Target Platform**: Modern browsers (Chrome, Firefox, Safari, Edge), tablet (768-1024px), mobile (< 640px)

**Project Type**: Web application (React SPA served by Go HTTP API)

**Performance Goals**: No measurable regression in page load or interaction latency

**Constraints**: All 252+ existing tests must continue to pass. WCAG AA compliance for accessibility items. 4 locales maintained in sync.

**Scale/Scope**: 19 pages, 15 components, ~95 existing i18n keys expanding to ~295 keys per locale

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. MARA Pipeline | PASS | No pipeline changes — frontend-only work |
| II. LLM-Agnostic | PASS | No LLM provider changes |
| III. HIPAA Compliance | PASS | No PHI handling changes; audit log unaffected; confirmation modals strengthen clinical safety |
| IV. Test-First | PASS | All new components will have unit tests; existing 252 tests must continue passing |
| V. Package-First | PASS | New components follow existing `components/` pattern |
| VI. Observability | PASS | No observability changes needed |

**Gate result**: ALL PASS — proceed to Phase 0.

## Project Structure

### Documentation (this feature)

```text
specs/001-remaining-plan-items/
├── spec.md              # Feature specification
├── plan.md              # This file
├── research.md          # Phase 0: research findings
├── data-model.md        # Phase 1: data model
├── quickstart.md        # Phase 1: validation guide
└── checklists/
    └── requirements.md  # Spec quality checklist
```

### Source Code (repository root)

```text
cmd/care/web/src/
├── components/
│   ├── ConfirmationModal.tsx    # NEW — clinical decision confirmation
│   ├── ConfidenceDisplay.tsx    # NEW — confidence category/color/tooltip
│   ├── DemoBanner.tsx           # NEW — persistent research demo banner
│   ├── CardTable.tsx            # NEW — card-based table for mobile
│   ├── NotificationBell.tsx     # MODIFY — add tier filtering + DND mode
│   ├── Modal.tsx                # EXISTS — reused by ConfirmationModal
│   ├── Toast.tsx                # EXISTS — unchanged (tiers in NotificationBell)
│   └── __tests__/
│       ├── ConfirmationModal.unit.tsx  # NEW
│       ├── ConfidenceDisplay.unit.tsx  # NEW
│       ├── DemoBanner.unit.tsx         # NEW
│       └── CardTable.unit.tsx          # NEW
├── pages/
│   ├── ReviewDetail.tsx         # MODIFY — wire ConfirmationModal
│   ├── ClinicianDashboard.tsx   # MODIFY — wire ConfirmationModal + ConfidenceDisplay
│   ├── Cost.tsx                 # MODIFY — add synthetic data notice + i18n
│   ├── RPM.tsx                  # MODIFY — add synthetic data notice + i18n
│   ├── Reviews.tsx              # MODIFY — radiogroup on filters + i18n
│   ├── Tasks.tsx                # MODIFY — radiogroup on filters + i18n
│   └── [13 more pages]         # MODIFY — i18n wiring + keyboard nav on tables
├── i18n/
│   ├── en.ts                    # MODIFY — ~200 new keys
│   ├── es.ts                    # MODIFY — ~200 new keys
│   ├── fr.ts                    # MODIFY — ~200 new keys
│   └── zh.ts                    # MODIFY — ~200 new keys
└── index.css                    # MODIFY — 44px touch targets, card layout styles
```

**Structure Decision**: All changes are within the existing `cmd/care/web/src/` directory following established patterns. 4 new component files, 4 new test files, modifications to 19 existing pages and 4 locale files.

## Complexity Tracking

No constitution violations — this section is not applicable.

## Implementation Phases

### Phase A: Clinical Safety Components (P1 — User Stories 1 & 2)

**New files**:
1. `components/ConfirmationModal.tsx` — Wraps existing Modal with: diagnosis summary display, patient context, confidence display, required reason textarea, Cancel/Confirm buttons with 2-second delay on Confirm
2. `components/ConfidenceDisplay.tsx` — Maps 0-1 float to category (Low/Moderate/High/Very High), color (red/amber/blue/green), percentage text, hover tooltip
3. `components/__tests__/ConfirmationModal.unit.tsx` — Test: renders with props, reason required, cancel closes, confirm submits after delay, audit callback
4. `components/__tests__/ConfidenceDisplay.unit.tsx` — Test: each threshold boundary, tooltip shows, percentage formatting

**Modified files**:
5. `pages/ReviewDetail.tsx` — Replace `alert()` calls with ConfirmationModal for Accept/Reject
6. `pages/ClinicianDashboard.tsx` — Wire ConfirmationModal to HITL queue Accept/Reject buttons; use ConfidenceDisplay for accuracy values

### Phase B: i18n Expansion (P2 — User Story 3)

**Modified files** (15 pages to wire + 4 locale files):
1. `i18n/en.ts`, `es.ts`, `fr.ts`, `zh.ts` — Add ~200 keys each covering: About, Agents, AuditLog, Bench, CaseRunner, Cost, Dashboard, FineTune, LLMTraces, PanelReport, Patients, RPM, ReviewDetail, Reviews, RunTrace, Tasks
2. Wire `useI18n()` into each of the 15 un-translated pages: `About.tsx`, `Agents.tsx`, `AuditLog.tsx`, `Bench.tsx`, `CaseRunner.tsx`, `Cost.tsx`, `Dashboard.tsx`, `FineTune.tsx`, `LLMTraces.tsx`, `PanelReport.tsx`, `Patients.tsx`, `RPM.tsx`, `ReviewDetail.tsx`, `Reviews.tsx`, `RunTrace.tsx`
3. Update existing tests that assert on English strings to use I18nProvider wrapper (same pattern as Login/ClinicianDashboard fix)

### Phase C: Accessibility (P2 — User Story 4)

**Modified files**:
1. `pages/Reviews.tsx` — Add `role="radiogroup"` to filter container, `role="radio"` + `aria-checked` to each filter button
2. `pages/Tasks.tsx` — Same radiogroup treatment on filter toggles
3. All 10+ table pages — Add `tabIndex={0}` on tbody, `onKeyDown` handler for ArrowUp/ArrowDown/Enter, visible `:focus-visible` ring on rows
4. `index.css` — Add `tr:focus-visible` styles with 2px outline meeting WCAG AA contrast

### Phase D: Responsive & Mobile (P3 — User Story 5)

**New files**:
1. `components/CardTable.tsx` — Responsive wrapper: renders children `<table>` on `>=sm`, card layout on `<sm>`. Accepts `columns` config to pick 3-4 fields for cards.
2. `components/__tests__/CardTable.unit.tsx` — Test: renders table at default width, renders cards at narrow width, shows "View details" link

**Modified files**:
3. `index.css` — Add `.touch-target-min { min-width: 44px; min-height: 44px; }` utility; apply to `.btn`, `.btn-secondary`, `.nav-link`, interactive elements
4. Table pages — Wrap tables with CardTable component for mobile fallback

### Phase E: Alert Fatigue & Demo Safeguards (P3 — User Stories 6 & 7)

**New files**:
1. `components/DemoBanner.tsx` — Persistent top banner with dismiss → sessionStorage → 30-min timer reshow
2. `components/__tests__/DemoBanner.unit.tsx` — Test: renders banner, dismiss hides, timer re-shows

**Modified files**:
3. `components/NotificationBell.tsx` — Add priority tier filtering: toast only for urgent/high, badge color logic (red/amber), DND toggle in dropdown
4. `components/Layout.tsx` — Add DemoBanner above main content
5. `pages/Cost.tsx` — Add "Synthetic data" notice
6. `pages/RPM.tsx` — Add "Synthetic data" notice

### Phase F: Test Updates

1. Update all existing page unit tests that render pages now using `useI18n()` to wrap in `<I18nProvider>`
2. Add new tests for all new components (4 test files in Phase A/D/E)
3. Verify all 252+ tests pass after each phase
