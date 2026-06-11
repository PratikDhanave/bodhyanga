# Feature Specification: Remaining Plan Items — UI Polish, Clinical Safety & i18n Completion

**Feature Branch**: `feat/e2e-everything`

**Created**: 2026-06-10

**Status**: Draft

**Input**: User description: "Complete remaining plan.md items: i18n expansion (B6.3-B6.4), accessibility fixes (B5.7-B5.8), responsive improvements (B7.3-B7.4), clinical safety UX (J1-J4), and new pages (B3.1-B3.4)"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Clinician Confirms Critical Decision Before Submission (Priority: P1)

A clinician reviewing an AI-generated diagnosis must explicitly confirm their decision before it takes effect. The system presents a confirmation dialog showing the diagnosis summary, patient context, and confidence level. The clinician must acknowledge they have reviewed the information and optionally provide a reason before the decision is finalized.

**Why this priority**: Clinical safety is the highest priority per the Bodh constitution. Unconfirmed clinical decisions pose direct patient safety risk. Every irreversible AI decision must have human confirmation.

**Independent Test**: Can be tested by navigating to any review, clicking Accept/Reject, verifying a confirmation modal appears with diagnosis details, and confirming the decision is only submitted after explicit confirmation.

**Acceptance Scenarios**:

1. **Given** a clinician is viewing a pending review, **When** they click "Accept", **Then** a confirmation modal appears showing the diagnosis, patient context, and a required reason field
2. **Given** a confirmation modal is open, **When** the clinician clicks "Cancel", **Then** the modal closes and no decision is recorded
3. **Given** a confirmation modal is open, **When** the clinician enters a reason and clicks "Confirm", **Then** the decision is submitted and an audit event is logged
4. **Given** a clinician clicks "Accept" on the dashboard HITL queue, **When** the confirmation modal appears, **Then** the same confirmation flow applies as in the full review page

---

### User Story 2 - Clinician Sees AI Confidence as Human-Readable Category (Priority: P1)

When AI generates a diagnosis with a confidence score, the clinician sees it displayed as a human-readable category (Low/Moderate/High/Very High) with color coding and a percentage, rather than a raw decimal number. A tooltip explains what the confidence level means.

**Why this priority**: Raw confidence numbers (0.92) are easily misinterpreted. Color-coded categories with tooltips reduce cognitive load and prevent misinterpretation of AI certainty.

**Independent Test**: Can be tested by viewing any page that shows confidence values (ReviewDetail, ClinicianDashboard) and verifying the display format, color, and tooltip.

**Acceptance Scenarios**:

1. **Given** a diagnosis has confidence 0.45, **When** displayed, **Then** the label shows "Low" in red with "45%"
2. **Given** a diagnosis has confidence 0.65, **When** displayed, **Then** the label shows "Moderate" in amber with "65%"
3. **Given** a diagnosis has confidence 0.85, **When** displayed, **Then** the label shows "High" in blue with "85%"
4. **Given** a diagnosis has confidence 0.95, **When** displayed, **Then** the label shows "Very High" in green with "95%"
5. **Given** any confidence label, **When** the clinician hovers over it, **Then** a tooltip explains the confidence methodology

---

### User Story 3 - All Pages Display in the Clinician's Preferred Language (Priority: P2)

A clinician who has selected Spanish, French, or Chinese as their language preference sees all UI text (headings, labels, buttons, table headers, status badges, empty states) in their chosen language across every page in the application, not just the 4 pages currently translated.

**Why this priority**: Partial i18n creates a jarring experience — switching between translated and untranslated pages undermines trust. Complete coverage across all 19 pages is necessary for non-English-speaking clinicians.

**Independent Test**: Can be tested by switching locale to any supported language and navigating through every page to verify no English-only strings appear in headings, buttons, or labels.

**Acceptance Scenarios**:

1. **Given** a clinician has selected Spanish, **When** they navigate to the Patients page, **Then** all headings, column headers, and buttons display in Spanish
2. **Given** a clinician has selected French, **When** they navigate to the Audit Log page, **Then** all filter labels, status text, and pagination controls display in French
3. **Given** a clinician has selected Chinese, **When** they navigate to any page, **Then** no hardcoded English strings appear in the main content area
4. **Given** a new locale key is missing, **When** the system looks up the key, **Then** it falls back to the English translation

---

### User Story 4 - Clinician Navigates Tables Using Keyboard Only (Priority: P2)

A clinician who relies on keyboard navigation (due to accessibility needs or efficiency preference) can navigate table rows using arrow keys, select rows with Enter, and use Tab to move between interactive elements within rows. Filter toggle groups are announced as radio groups by screen readers.

**Why this priority**: WCAG AA compliance requires keyboard operability. Clinical environments often use keyboard-heavy workflows. Filter toggles without proper ARIA semantics are invisible to screen reader users.

**Independent Test**: Can be tested by tabbing to a table, using arrow keys to navigate rows, pressing Enter to select, and verifying screen reader announcements for filter groups.

**Acceptance Scenarios**:

1. **Given** a table page (Patients, AuditLog, etc.), **When** a user tabs to the table, **Then** the first row receives focus with a visible focus indicator
2. **Given** a row is focused, **When** the user presses Down Arrow, **Then** focus moves to the next row
3. **Given** a row is focused, **When** the user presses Enter, **Then** the row's primary action is triggered (e.g., navigation to detail page)
4. **Given** the Reviews or Tasks page, **When** a screen reader encounters the filter toggles, **Then** it announces them as a radio group with the selected option

---

### User Story 5 - Clinician Uses the Application on a Tablet (Priority: P3)

A clinician using a tablet (768px-1024px width) can comfortably tap all interactive elements, which meet the minimum 44px touch target size. On small screens (<640px), data tables transform into a card-based layout showing key information without horizontal scrolling.

**Why this priority**: Clinical environments increasingly use tablets at bedside. Touch targets below 44px cause frustrating mis-taps. Horizontal scrolling on mobile makes tables unusable.

**Independent Test**: Can be tested by resizing the browser to tablet/mobile widths and verifying touch target sizes and card layout rendering.

**Acceptance Scenarios**:

1. **Given** the application is viewed on a tablet, **When** the user taps any button, link, or interactive element, **Then** the touch target is at least 44x44 pixels
2. **Given** the application is viewed on a screen narrower than 640px, **When** a data table is displayed, **Then** it renders as stacked cards instead of a horizontal table
3. **Given** a card layout on mobile, **When** the user views a patient record card, **Then** key fields (name, condition, status) are clearly visible without scrolling

---

### User Story 6 - Clinician Manages Notification Overload (Priority: P3)

A clinician receiving many notifications throughout the day only sees toast notifications for urgent and high-priority alerts. Normal and low-priority notifications accumulate silently in the notification badge. The badge uses color coding (red for urgent, amber for normal) to indicate severity at a glance. A "Do Not Disturb" mode suppresses all toasts for a configurable period.

**Why this priority**: Alert fatigue is a well-documented clinical safety issue. Non-tiered notifications cause clinicians to ignore all alerts, including critical ones.

**Independent Test**: Can be tested by triggering notifications of different priorities and verifying toast behavior, badge colors, and DND mode.

**Acceptance Scenarios**:

1. **Given** an urgent notification arrives, **When** the clinician is viewing any page, **Then** a toast notification appears immediately
2. **Given** a normal-priority notification arrives, **When** the clinician is viewing any page, **Then** no toast appears but the badge count increments
3. **Given** the notification badge shows alerts, **When** there are urgent alerts, **Then** the badge is red; when only normal, **Then** the badge is amber
4. **Given** the clinician enables "Do Not Disturb", **When** any notification arrives during the DND period, **Then** no toast appears but notifications still accumulate in the dropdown

---

### User Story 7 - System Clearly Identifies Demo Context (Priority: P3)

Users of the research demo clearly understand that the system is not for clinical use. A persistent banner appears at the top of the application. Pages with synthetic data (Cost, RPM) display clear "Synthetic data" indicators. Any exported data includes a demo watermark.

**Why this priority**: Misuse of a research demo for clinical decisions poses safety and liability risks. Clear visual indicators prevent accidental reliance on demo data.

**Independent Test**: Can be tested by verifying the banner appears on page load, checking synthetic data indicators on Cost/RPM pages, and confirming exports include watermarks.

**Acceptance Scenarios**:

1. **Given** any page in the application, **When** the page loads, **Then** a persistent banner states "RESEARCH DEMO — Not for clinical use"
2. **Given** the banner has been dismissed, **When** 30 minutes pass, **Then** the banner reappears
3. **Given** the Cost or RPM page, **When** the page loads, **Then** a "Synthetic data — not connected to live systems" notice is visible
4. **Given** any data export (CSV/PDF), **When** the export is generated, **Then** it includes a "DEMO" watermark

---

### Edge Cases

- What happens when a clinician dismisses a confirmation modal and navigates away? The decision is not recorded and the review remains pending.
- How does the system handle confidence values outside the 0-1 range? Values are clamped to [0, 1] before categorization.
- What happens if an i18n key is missing for a locale? The system falls back to the English (en) translation.
- How does card layout handle tables with many columns? Only the 3-4 most important fields are shown in the card; a "View details" link provides full information.
- What happens if a clinician enables DND and an urgent alert arrives? The toast is suppressed but the badge still shows red; DND does not suppress the badge color.

## Requirements *(mandatory)*

### Functional Requirements

**Clinical Safety (J1-J4)**:
- **FR-001**: System MUST display a confirmation modal before any irreversible clinical decision (accept/reject diagnosis)
- **FR-002**: The confirmation modal MUST show diagnosis summary, patient context, and AI confidence level
- **FR-003**: The confirmation modal MUST require a reason field before allowing submission
- **FR-004**: System MUST display AI confidence as a human-readable category (Low/Moderate/High/Very High) with color coding
- **FR-005**: System MUST show confidence as a percentage (e.g., "85%") alongside the category label
- **FR-006**: System MUST provide a tooltip explaining the confidence methodology when hovered
- **FR-007**: System MUST tier notifications by priority — toast for urgent/high, silent badge for normal/low
- **FR-008**: System MUST color-code the notification badge — red for urgent, amber for normal
- **FR-009**: System MUST provide a "Do Not Disturb" mode that suppresses toasts for a user-selected period
- **FR-010**: System MUST display a persistent "RESEARCH DEMO" banner on all pages, dismissable per session, reappearing after 30 minutes
- **FR-011**: System MUST show "Synthetic data" notices on Cost and RPM pages
- **FR-012**: System MUST include "DEMO" watermark text in any exported data

**i18n (B6.3-B6.4)**:
- **FR-013**: System MUST provide translations for all 19 pages in 4 locales (en, es, fr, zh)
- **FR-014**: System MUST translate all headings, labels, buttons, table headers, status badges, and empty states
- **FR-015**: System MUST fall back to English when a translation key is missing

**Accessibility (B5.7-B5.8)**:
- **FR-016**: Filter toggle groups (Reviews, Tasks) MUST use `role="radiogroup"` with `role="radio"` on individual options
- **FR-017**: Data tables MUST support keyboard navigation with arrow keys between rows
- **FR-018**: Focused table rows MUST have a visible focus indicator meeting WCAG AA contrast requirements
- **FR-019**: Pressing Enter on a focused table row MUST trigger the row's primary action

**Responsive (B7.3-B7.4)**:
- **FR-020**: All interactive elements MUST have a minimum touch target size of 44x44 CSS pixels
- **FR-021**: Data tables MUST render as card-based layouts on screens narrower than 640px
- **FR-022**: Card layouts MUST show the 3-4 most important fields with a "View details" link

### Key Entities

- **ConfirmationModal**: A reusable dialog component that captures a reason and requires explicit confirmation for clinical decisions
- **ConfidenceDisplay**: A visual component that maps a 0-1 confidence float to a category, color, percentage, and tooltip
- **NotificationTier**: Classification of notifications as urgent, high, normal, or low — determines toast vs. silent behavior
- **DemoBanner**: A persistent UI element indicating research demo status with session-based dismiss and timed reappearance
- **LocaleBundle**: A collection of translation key-value pairs for a single locale covering all 19 pages

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of irreversible clinical decisions require explicit confirmation before submission
- **SC-002**: AI confidence scores are displayed as human-readable categories on all pages where they appear
- **SC-003**: All 19 pages display correctly in all 4 supported languages with zero English-only strings in the main content area
- **SC-004**: All interactive elements meet the 44px minimum touch target on tablet-sized screens
- **SC-005**: All data tables support full keyboard navigation (arrow keys, Enter, Tab)
- **SC-006**: Filter toggles on Reviews and Tasks pages are announced as radio groups by screen readers
- **SC-007**: Data tables render as card layouts on screens narrower than 640px without horizontal scrolling
- **SC-008**: Urgent notifications produce toasts while normal notifications only increment the badge
- **SC-009**: "RESEARCH DEMO" banner is visible on every page by default
- **SC-010**: All 252+ frontend tests continue to pass after changes

## Assumptions

- The existing `useI18n()` hook and `I18nProvider` context pattern will be reused for all new i18n work
- The existing `Toast` component and `NotificationBell` component will be extended (not replaced) for alert tiering
- The Modal component (B1.1) was already planned but will be created as part of the J1 clinical safety work
- Confidence values are always in the 0-1 range as provided by the MARA diagnostician agents
- "Do Not Disturb" mode state is stored in-memory (session-scoped), not persisted to backend
- The demo banner dismiss state is stored in sessionStorage, not localStorage (so it reappears on new tabs)
- Card-based table layouts are a progressive enhancement — the table still functions on all screen sizes
- Export watermarking applies to frontend-generated exports only (CSV downloads via Blob URLs)
