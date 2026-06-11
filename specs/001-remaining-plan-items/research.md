# Research: Remaining Plan Items

**Date**: 2026-06-10 | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

## R1: Confirmation Modal UX Patterns for Clinical Decisions

**Decision**: Use a two-step confirmation with required reason field and 2-second delay on the final confirm button.

**Rationale**: Clinical decision support literature recommends "speed bumps" for irreversible actions. A 2-second delay prevents accidental double-clicks while not being so long as to frustrate experienced clinicians. The required reason field creates an audit trail and forces the clinician to articulate their clinical reasoning.

**Alternatives considered**:
- Simple browser `confirm()` dialog — rejected: no custom content, no reason capture, poor accessibility
- Inline expansion (no modal) — rejected: less visually distinct, easier to miss context
- Three-step confirmation — rejected: excessive friction for routine approvals

## R2: Confidence Score Categorization Thresholds

**Decision**: Use 4 categories with these thresholds:
- 0-50%: "Low" (red, `text-red-600`)
- 50-70%: "Moderate" (amber, `text-amber-600`)
- 70-90%: "High" (blue, `text-blue-600`)
- 90-100%: "Very High" (green, `text-emerald-600`)

**Rationale**: These thresholds align with common clinical decision support confidence bands. The 50% cutoff for "Low" ensures anything below chance is clearly flagged. The 70% threshold for "High" reflects clinical standards where 70%+ is generally considered actionable. Colors follow the existing Bodh palette with dark mode variants.

**Alternatives considered**:
- 3 categories (Low/Medium/High) — rejected: loses nuance at the high end where clinical decisions cluster
- 5 categories — rejected: too granular, adds cognitive load without proportional benefit
- Continuous color gradient — rejected: harder to interpret at a glance, not accessible for color-blind users

## R3: i18n Key Naming Convention

**Decision**: Use dot-separated hierarchical keys matching the existing pattern: `{page}.{element}` (e.g., `agents.title`, `auditLog.filterByKind`).

**Rationale**: The existing codebase uses this pattern (e.g., `login.signIn`, `dashboard.totalCases`). Maintaining consistency reduces cognitive overhead and enables automated tooling to check for missing keys.

**Alternatives considered**:
- Flat keys with underscores — rejected: inconsistent with existing 95 keys
- Component-scoped keys — rejected: creates duplication for shared terms

## R4: Table Keyboard Navigation Pattern

**Decision**: Use `roving tabindex` pattern — only one row is in the tab order at a time, arrow keys move focus between rows, Enter activates the row's primary action.

**Rationale**: This is the WAI-ARIA recommended pattern for composite widgets. It keeps the Tab key efficient (one stop per table, not one per row) while providing full navigation within the table via arrow keys.

**Alternatives considered**:
- Every row as `tabIndex={0}` — rejected: clutters tab order, slow for keyboard users to traverse
- Grid role with cell navigation — rejected: overkill for row-oriented tables, complex to implement
- No keyboard nav (rely on Tab) — rejected: fails WCAG 2.1.1 (Keyboard)

## R5: Card-Based Table Layout Strategy

**Decision**: Create a `CardTable` wrapper component that accepts column config and renders as `<table>` above `sm` breakpoint (640px) or stacked cards below it. Uses Tailwind's `hidden sm:table` / `sm:hidden` classes for the swap.

**Rationale**: CSS-only approach avoids rendering two DOM trees. The column config lets each page specify which 3-4 fields appear in card mode. This follows the progressive enhancement pattern — tables work everywhere, cards are better on mobile.

**Alternatives considered**:
- Responsive horizontal scroll — rejected: usability study consensus is that horizontal scroll is confusing on mobile
- React-virtualized with responsive — rejected: heavy dependency for a simple layout swap
- Media query in CSS only — rejected: needs column config to pick mobile-visible fields

## R6: Alert Tier Classification and DND Implementation

**Decision**: Notifications carry a `priority` field (urgent|high|normal|low). Toast is shown only for urgent/high. Badge color is red if any urgent exists, amber otherwise. DND mode is a React state that suppresses toasts for a user-selected duration (15min, 30min, 1hr) stored in component state.

**Rationale**: Two-tier toast behavior (toast vs silent) is the simplest model that addresses alert fatigue. Badge color provides at-a-glance severity without requiring the dropdown. DND is session-scoped (not persisted) to prevent permanent silencing of critical alerts.

**Alternatives considered**:
- Per-notification-type suppression — rejected: too granular, configuration overhead
- Backend-driven priority — rejected: requires API changes, current work is frontend-only
- Persistent DND in localStorage — rejected: risk of clinician forgetting they silenced alerts across sessions

## R7: Demo Banner Timing and Storage

**Decision**: Use `sessionStorage` for dismiss state with a timestamp. On each page load, check if >30 minutes have passed since dismissal and re-show if so. Banner uses a fixed-position bar at the very top of the layout.

**Rationale**: `sessionStorage` means the banner reappears in new tabs (unlike `localStorage`). The 30-minute timer ensures the reminder persists even in long sessions. Fixed positioning keeps it visible during scroll.

**Alternatives considered**:
- `localStorage` — rejected: banner wouldn't show in new tabs, defeating the purpose
- Cookie-based — rejected: unnecessary for client-only state
- Non-dismissable — rejected: too intrusive for developers who use the demo daily
