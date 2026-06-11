# Tasks: Remaining Plan Items — UI Polish, Clinical Safety & i18n Completion

**Input**: Design documents from `specs/001-remaining-plan-items/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, quickstart.md

**Tests**: Included — the spec and constitution require test-first with coverage gates.

**Organization**: Tasks grouped by user story (7 stories from spec.md) for independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1-US7)
- All file paths relative to `cmd/care/web/src/`

---

## Phase 1: Setup

**Purpose**: No new project setup needed — extending existing React SPA. This phase ensures baseline is stable.

- [X] T001 Run `npx vitest run` to confirm all 252 tests pass before starting work
- [X] T002 Run `npx tsc --noEmit` to confirm TypeScript compiles cleanly

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared components and utilities that multiple user stories depend on

**CRITICAL**: US1 and US2 depend on ConfidenceDisplay. US3 depends on i18n keys. US5 depends on CardTable.

- [X] T003 [P] Create `components/ConfidenceDisplay.tsx` — Map 0-1 float to category (Low 0-50%/Moderate 50-70%/High 70-90%/Very High 90-100%), color (red/amber/blue/green with dark mode variants), percentage text, and hover tooltip explaining methodology. Props: `value: number`, `showTooltip?: boolean`, `size?: 'sm' | 'md'`
- [X] T004 [P] Create `components/__tests__/ConfidenceDisplay.unit.tsx` — Test: each threshold boundary (0.49→Low, 0.50→Moderate, 0.69→Moderate, 0.70→High, 0.89→High, 0.90→Very High), tooltip shows on hover, percentage formatting (e.g., 0.923 → "92%"), clamping values outside 0-1
- [X] T005 [P] Create `components/ConfirmationModal.tsx` — Wraps existing `Modal.tsx`. Props: `isOpen`, `title`, `diagnosisSummary`, `patientContext`, `confidence` (renders ConfidenceDisplay), `onConfirm(reason)`, `onCancel`, `confirmDelay=2000`. Required reason textarea. Confirm button disabled for `confirmDelay` ms after open. Cancel button always active.
- [X] T006 [P] Create `components/__tests__/ConfirmationModal.unit.tsx` — Test: renders diagnosis summary and patient context, reason textarea required (confirm disabled when empty), confirm button disabled during delay period, cancel calls onCancel, confirm calls onConfirm with reason text, renders ConfidenceDisplay with passed value
- [X] T007 [P] Create `components/DemoBanner.tsx` — Fixed-position amber banner at top: "RESEARCH DEMO — Not for clinical use". Dismiss button stores timestamp in `sessionStorage('bodh-demo-dismissed')`. On mount: check if >30min since dismiss → re-show. Export as default.
- [X] T008 [P] Create `components/__tests__/DemoBanner.unit.tsx` — Test: renders banner text on mount, dismiss hides banner, reads sessionStorage on mount, re-shows after 30 minutes (mock Date.now), does not show if dismissed < 30 min ago
- [X] T009 [P] Create `components/CardTable.tsx` — Responsive wrapper component. Props: `columns: CardColumn[]` (key, label, showInCard, primary), `data: any[]`, `onRowClick?`, `keyField: string`, `children` (the real table). Renders `children` table with `hidden sm:table` and card layout with `sm:hidden`. Cards show fields where `showInCard=true`, primary field as card title, "View details" link triggers onRowClick.
- [X] T010 [P] Create `components/__tests__/CardTable.unit.tsx` — Test: renders children table by default, renders card layout (mock matchMedia for narrow), cards show only showInCard fields, primary field is card title, "View details" click triggers onRowClick

**Checkpoint**: Foundation components ready — user story implementation can begin in parallel

---

## Phase 3: User Story 1 — Clinician Confirms Critical Decisions (Priority: P1) MVP

**Goal**: Every irreversible clinical decision (accept/reject diagnosis) requires explicit confirmation with reason.

**Independent Test**: Navigate to /reviews, click Accept → modal appears with context → enter reason → confirm → decision submitted.

### Implementation for User Story 1

- [X] T011 [US1] Wire ConfirmationModal into `pages/ReviewDetail.tsx` — Replace existing `alert()` or direct submit calls for Accept/Reject with ConfirmationModal. Pass diagnosis summary from review data, patient context, and confidence. On confirm: call existing API with reason included. Import ConfirmationModal and ConfidenceDisplay.
- [X] T012 [US1] Wire ConfirmationModal into `pages/ClinicianDashboard.tsx` — Add ConfirmationModal for the inline Accept/Reject buttons in the HITL queue section. On click: open modal with review subject as diagnosis summary. On confirm: POST to `/api/reviews/{id}/decide`. Use ConfidenceDisplay for accuracy values in the chart section.
- [X] T013 [US1] Update `pages/__tests__/ClinicianDashboard.unit.tsx` — Add tests: Accept button opens ConfirmationModal, Cancel closes without API call, Confirm with reason triggers API call, ConfidenceDisplay renders for accuracy values
- [X] T014 [US1] Run `npx vitest run` — Verify all tests pass including new ConfirmationModal tests

**Checkpoint**: User Story 1 complete — all clinical decisions require confirmation with reason and audit trail

---

## Phase 4: User Story 2 — AI Confidence as Human-Readable Category (Priority: P1)

**Goal**: Confidence scores displayed as category + percentage + color + tooltip everywhere they appear.

**Independent Test**: View ClinicianDashboard → accuracy chart shows "High (85%)" in blue with tooltip.

### Implementation for User Story 2

- [X] T015 [US2] Audit all pages for raw confidence values — Search codebase for `confidence`, `accuracy`, `score` displays that show raw decimals. Known locations: ClinicianDashboard (accuracy chart), ReviewDetail (diagnosis confidence), PanelReport (gap severity).
- [X] T016 [P] [US2] Replace raw confidence displays with ConfidenceDisplay component in `pages/ReviewDetail.tsx` — Find where confidence/accuracy is rendered as a number and replace with `<ConfidenceDisplay value={...} />`
- [X] T017 [P] [US2] Replace raw confidence displays with ConfidenceDisplay component in `pages/PanelReport.tsx` — If severity scores exist as numbers, wrap with ConfidenceDisplay
- [X] T018 [US2] Run `npx vitest run` — Verify all tests pass

**Checkpoint**: User Story 2 complete — confidence always shows as category with color and tooltip

---

## Phase 5: User Story 3 — Full i18n Coverage (Priority: P2)

**Goal**: All 19 pages display in the clinician's preferred language (en/es/fr/zh).

**Independent Test**: Switch to Spanish → navigate all 19 pages → zero English-only strings in headings/buttons/labels.

### Implementation for User Story 3

- [X] T019 [P] [US3] Add ~200 i18n keys to `i18n/en.ts` — Add keys for: about.* (8), agents.* (12), auditLog.* (15), bench.* (14), caseRunner.* (12), cost.* (14), dashboard.* (5 extend), fineTune.* (12), llmTraces.* (15), panelReport.* (14), patients.* (12), rpm.* (14), reviewDetail.* (15), reviews.* (8 extend), runTrace.* (10). Follow existing dot-separated convention.
- [X] T020 [P] [US3] Add ~200 i18n keys to `i18n/es.ts` — Spanish translations matching all new en.ts keys
- [X] T021 [P] [US3] Add ~200 i18n keys to `i18n/fr.ts` — French translations matching all new en.ts keys
- [X] T022 [P] [US3] Add ~200 i18n keys to `i18n/zh.ts` — Chinese translations matching all new en.ts keys
- [X] T023 [P] [US3] Wire `useI18n()` into `pages/About.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T024 [P] [US3] Wire `useI18n()` into `pages/Agents.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T025 [P] [US3] Wire `useI18n()` into `pages/AuditLog.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T026 [P] [US3] Wire `useI18n()` into `pages/Bench.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T027 [P] [US3] Wire `useI18n()` into `pages/CaseRunner.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T028 [P] [US3] Wire `useI18n()` into `pages/Cost.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T029 [P] [US3] Wire `useI18n()` into `pages/Dashboard.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T030 [P] [US3] Wire `useI18n()` into `pages/FineTune.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T031 [P] [US3] Wire `useI18n()` into `pages/LLMTraces.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T032 [P] [US3] Wire `useI18n()` into `pages/PanelReport.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T033 [P] [US3] Wire `useI18n()` into `pages/Patients.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T034 [P] [US3] Wire `useI18n()` into `pages/RPM.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T035 [P] [US3] Wire `useI18n()` into `pages/ReviewDetail.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T036 [P] [US3] Wire `useI18n()` into `pages/Reviews.tsx` — Extend existing t() calls to cover any remaining English strings
- [X] T037 [P] [US3] Wire `useI18n()` into `pages/RunTrace.tsx` — Import useI18n, replace all hardcoded English strings with t() calls
- [X] T038 [US3] Update all existing page unit tests to wrap renders in `<I18nProvider>` — Apply the same pattern used in Login.unit.tsx and ClinicianDashboard.unit.tsx: import I18nProvider, wrap the render tree. Affected files: About.unit.tsx, Bench.unit.tsx, Cost.unit.tsx, FineTune.unit.tsx, LLMTraces.unit.tsx, PanelReport.unit.tsx, Patients.unit.tsx, RPM.unit.tsx, Reviews.unit.tsx, RunTrace.unit.tsx, Tasks.unit.tsx, PatientPortal.unit.tsx, ReviewDetail.unit.tsx
- [X] T039 [US3] Run `npx vitest run` — Verify all tests pass with I18nProvider wrappers

**Checkpoint**: User Story 3 complete — all 19 pages translated into 4 locales

---

## Phase 6: User Story 4 — Keyboard Table Navigation & Radiogroup Filters (Priority: P2)

**Goal**: Tables navigable via keyboard. Filter toggles announced as radio groups by screen readers.

**Independent Test**: Tab to table → arrow keys move rows → Enter activates. Screen reader announces filter toggles as radiogroup.

### Implementation for User Story 4

- [X] T040 [P] [US4] Add `role="radiogroup"` and `aria-label` to filter container in `pages/Reviews.tsx` — Wrap filter buttons in a div with `role="radiogroup"` and `aria-label="Filter reviews"`. Add `role="radio"` and `aria-checked={isActive}` to each filter button.
- [X] T041 [P] [US4] Add `role="radiogroup"` and `aria-label` to filter container in `pages/Tasks.tsx` — Same radiogroup pattern as Reviews.tsx. Wrap filter toggles with `role="radiogroup"` and `aria-label="Filter tasks"`.
- [X] T042 [US4] Create keyboard navigation hook or utility for tables — Implement a `useTableKeyboardNav(rowCount, onActivate)` hook that manages: `activeRowIndex` state, ArrowUp/ArrowDown handlers, Enter handler calling onActivate(index), roving tabindex (only active row has tabIndex=0, others tabIndex=-1).
- [X] T043 [US4] Add keyboard navigation to `pages/Patients.tsx` table — Apply useTableKeyboardNav hook. Add `tabIndex`, `onKeyDown`, `aria-activedescendant` to tbody rows. Add `:focus-visible` ring style.
- [X] T044 [P] [US4] Add keyboard navigation to `pages/AuditLog.tsx` table — Same pattern as Patients.tsx
- [X] T045 [P] [US4] Add keyboard navigation to `pages/LLMTraces.tsx` table — Same pattern as Patients.tsx
- [X] T046 [P] [US4] Add keyboard navigation to `pages/Agents.tsx` table — Same pattern as Patients.tsx
- [X] T047 [P] [US4] Add keyboard navigation to remaining table pages — Apply to: Bench.tsx, Cost.tsx, RPM.tsx, PanelReport.tsx, Tasks.tsx, Reviews.tsx
- [X] T048 [US4] Add `tr:focus-visible` styles in `index.css` — Add `tr:focus-visible { outline: 2px solid var(--color-bodh-600); outline-offset: -2px; border-radius: 4px; }` with dark mode variant
- [X] T049 [US4] Run `npx vitest run` — Verify all tests pass

**Checkpoint**: User Story 4 complete — full keyboard navigation on all tables, radiogroup on filter toggles

---

## Phase 7: User Story 5 — Tablet & Mobile Responsive (Priority: P3)

**Goal**: 44px touch targets on tablet. Card-based table fallback on mobile (<640px).

**Independent Test**: Resize to 375px → tables show as cards. Resize to 768px → all buttons >= 44px.

### Implementation for User Story 5

- [X] T050 [US5] Add minimum touch target styles in `index.css` — Add `.btn, .btn-secondary, .nav-link, button, a[role="button"], select, input[type="checkbox"], input[type="radio"] { min-height: 44px; min-width: 44px; }` scoped to touch-capable viewports with `@media (pointer: coarse)`. Also ensure padding brings small buttons to 44px.
- [X] T051 [P] [US5] Wrap `pages/Patients.tsx` table with CardTable component — Define columns config with showInCard flags, wrap existing table in CardTable, pass onRowClick for navigation
- [X] T052 [P] [US5] Wrap `pages/AuditLog.tsx` table with CardTable component — Define columns (timestamp, kind, agent, case_id as card fields)
- [X] T053 [P] [US5] Wrap `pages/LLMTraces.tsx` table with CardTable component — Define columns (model, tokens, duration, cost as card fields)
- [X] T054 [P] [US5] Wrap `pages/Tasks.tsx` table with CardTable component — Define columns (task, priority, status, due date as card fields)
- [X] T055 [P] [US5] Wrap remaining table pages with CardTable — Apply to: Reviews.tsx, Agents.tsx, Bench.tsx, Cost.tsx, RPM.tsx, PanelReport.tsx
- [X] T056 [US5] Run `npx vitest run` — Verify all tests pass

**Checkpoint**: User Story 5 complete — tablet touch targets met, mobile card layouts working

---

## Phase 8: User Story 6 — Alert Fatigue Prevention (Priority: P3)

**Goal**: Toast only for urgent/high. Badge color-coded. DND mode available.

**Independent Test**: Trigger notifications of different priorities → verify toast vs silent behavior and badge colors.

### Implementation for User Story 6

- [X] T057 [US6] Modify `components/NotificationBell.tsx` — Add `priority` field support to notification items. Filter toast triggering: only call `showToast` for `urgent` or `high` priority. Change badge color: red if any notification has `urgent`/`high` priority, amber otherwise. Add DND toggle button in dropdown header with duration selector (15min/30min/1hr). DND state: `{active: boolean, expiresAt: number | null}` in component state. When DND active: suppress toast calls but still increment badge.
- [X] T058 [US6] Update `components/__tests__/NotificationBell.unit.tsx` — Add tests: urgent notification shows toast, normal notification does not show toast, badge is red with urgent notifications, badge is amber with only normal notifications, DND toggle suppresses toasts, DND expires after selected duration
- [X] T059 [US6] Run `npx vitest run` — Verify all tests pass

**Checkpoint**: User Story 6 complete — tiered notifications with DND mode

---

## Phase 9: User Story 7 — Research Demo Safeguards (Priority: P3)

**Goal**: Persistent demo banner, synthetic data notices, export watermarks.

**Independent Test**: Load any page → demo banner visible. Navigate to Cost/RPM → synthetic data notice. Export → DEMO watermark.

### Implementation for User Story 7

- [X] T060 [US7] Add DemoBanner to `components/Layout.tsx` — Import DemoBanner, render above the main content area (below header, above `<Outlet />`). Ensure it's visible on all pages.
- [X] T061 [P] [US7] Add "Synthetic data" notice to `pages/Cost.tsx` — Add a yellow/amber info banner below the page title: "Synthetic data — not connected to live systems". Use existing badge/alert styling.
- [X] T062 [P] [US7] Add "Synthetic data" notice to `pages/RPM.tsx` — Same synthetic data banner as Cost.tsx
- [X] T063 [US7] Add DEMO watermark to CSV export functions — If any page has a CSV export button (AuditLog, Bench), prepend "# DEMO — NOT FOR CLINICAL USE" as the first line of the CSV output. If PDF export exists, add watermark text.
- [X] T064 [US7] Run `npx vitest run` — Verify all tests pass

**Checkpoint**: User Story 7 complete — demo safeguards active on all pages

---

## Phase 10: Polish & Cross-Cutting Concerns

**Purpose**: Final validation, cleanup, and cross-story integration

- [X] T065 Run full test suite: `npx vitest run` — All tests must pass (target: 270+)
- [X] T066 Run TypeScript check: `npx tsc --noEmit` — Zero errors
- [X] T067 Run Go backend tests: `make test` — Confirm no regressions
- [X] T068 [P] Manual visual audit: navigate all 19 pages in dark mode — Verify new components (ConfirmationModal, ConfidenceDisplay, DemoBanner, CardTable) render correctly in dark mode
- [X] T069 [P] Manual i18n audit: switch to each locale and navigate all pages — Verify zero English-only strings in all 4 locales
- [X] T070 Commit and push all changes
- [X] T071 Run quickstart.md validation scenarios V1-V7

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — verify baseline
- **Phase 2 (Foundational)**: No dependencies — creates shared components
- **Phase 3 (US1)**: Depends on T003 (ConfidenceDisplay), T005 (ConfirmationModal) from Phase 2
- **Phase 4 (US2)**: Depends on T003 (ConfidenceDisplay) from Phase 2
- **Phase 5 (US3)**: No dependencies on other stories — only needs existing i18n infrastructure
- **Phase 6 (US4)**: No dependencies on other stories — accessibility is cross-cutting
- **Phase 7 (US5)**: Depends on T009 (CardTable) from Phase 2
- **Phase 8 (US6)**: No dependencies on other stories — extends NotificationBell
- **Phase 9 (US7)**: Depends on T007 (DemoBanner) from Phase 2
- **Phase 10 (Polish)**: Depends on all desired stories being complete

### User Story Independence

- **US1 (Confirm)** + **US2 (Confidence)**: Share ConfidenceDisplay but are independently testable
- **US3 (i18n)**: Fully independent — can run in parallel with any other story
- **US4 (Keyboard)**: Fully independent — can run in parallel with any other story
- **US5 (Responsive)**: Fully independent after CardTable is ready
- **US6 (Alerts)**: Fully independent — only touches NotificationBell
- **US7 (Demo)**: Fully independent after DemoBanner is ready

### Parallel Opportunities

- **Phase 2**: All 8 tasks (T003-T010) can run in parallel — different files
- **Phase 5 (i18n)**: T019-T022 (locale files) in parallel, T023-T037 (page wiring) in parallel
- **US3 + US4 + US6**: These 3 stories touch completely different files and can run in parallel
- **US1 + US3**: Can run in parallel (US1 touches ReviewDetail/ClinicianDashboard, US3 touches different aspects)

---

## Parallel Example: Phase 2 (Foundational)

```text
# All 4 component files can be written simultaneously:
Task T003: "Create ConfidenceDisplay.tsx"
Task T005: "Create ConfirmationModal.tsx"
Task T007: "Create DemoBanner.tsx"
Task T009: "Create CardTable.tsx"

# All 4 test files can be written simultaneously:
Task T004: "Create ConfidenceDisplay.unit.tsx"
Task T006: "Create ConfirmationModal.unit.tsx"
Task T008: "Create DemoBanner.unit.tsx"
Task T010: "Create CardTable.unit.tsx"
```

## Parallel Example: Phase 5 (i18n)

```text
# All 4 locale files can be updated simultaneously:
Task T019: "Add keys to en.ts"
Task T020: "Add keys to es.ts"
Task T021: "Add keys to fr.ts"
Task T022: "Add keys to zh.ts"

# All 15 page wiring tasks can run simultaneously:
Task T023-T037: "Wire useI18n() into each page"
```

---

## Implementation Strategy

### MVP First (User Stories 1 + 2 Only)

1. Complete Phase 1: Setup (verify baseline)
2. Complete Phase 2: Foundational (T003-T010)
3. Complete Phase 3: US1 — Confirmation Modals
4. Complete Phase 4: US2 — Confidence Display
5. **STOP and VALIDATE**: Test US1+US2 independently
6. Commit and push — clinical safety features shipped

### Incremental Delivery

1. Setup + Foundational → shared components ready
2. US1 + US2 → Clinical safety (P1) — highest value
3. US3 → i18n coverage (P2) — broadens user base
4. US4 → Keyboard nav (P2) — accessibility compliance
5. US5 + US6 + US7 → Responsive + alerts + demo safeguards (P3)
6. Polish → final validation and commit

---

## Notes

- All file paths are relative to `cmd/care/web/src/`
- [P] tasks = different files, no dependencies between them
- [US*] label maps task to specific user story for traceability
- Run `npx vitest run` after each phase to catch regressions early
- Wrap all new page renders in `<I18nProvider>` in tests (established pattern)
- Follow existing component patterns: functional components, Tailwind classes, dark mode variants
