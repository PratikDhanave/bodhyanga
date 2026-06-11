# Quickstart Validation Guide: Remaining Plan Items

**Date**: 2026-06-10 | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

## Prerequisites

- Node.js 20+
- Go 1.26+
- Repository cloned and dependencies installed: `cd cmd/care/web && npm ci`

## Validation Scenarios

### V1: Confirmation Modal (FR-001 through FR-003)

```bash
# Start the app
make care

# In browser:
# 1. Navigate to /reviews
# 2. Click on any pending review
# 3. Click "Accept" → Confirmation modal should appear
# 4. Verify: diagnosis summary, patient context, confidence shown
# 5. Verify: "Confirm" button is disabled for 2 seconds
# 6. Type a reason → Click "Confirm"
# 7. Verify: decision submitted, page updates

# Run unit tests
cd cmd/care/web && npx vitest run src/components/__tests__/ConfirmationModal.unit.tsx
```

**Expected**: Modal appears with full context. Confirm button enables after 2s. Reason is required.

### V2: Confidence Display (FR-004 through FR-006)

```bash
# In browser (app already running):
# 1. Navigate to /dashboard (Clinician Dashboard)
# 2. Observe confidence values in the accuracy chart area
# 3. Verify: shows category label (Low/Moderate/High/Very High) + percentage
# 4. Verify: color matches category (red/amber/blue/green)
# 5. Hover over any confidence value → tooltip should explain methodology

# Run unit tests
cd cmd/care/web && npx vitest run src/components/__tests__/ConfidenceDisplay.unit.tsx
```

**Expected**: "85%" shows as "High" in blue. "45%" shows as "Low" in red. Tooltip visible on hover.

### V3: i18n Coverage (FR-013 through FR-015)

```bash
# In browser:
# 1. Open any page
# 2. Click the language selector in the sidebar header
# 3. Switch to Spanish (es)
# 4. Navigate through all 19 pages
# 5. Verify: no English-only strings in headings, buttons, table headers, or labels

# Run all tests (verifies I18nProvider wrappers)
cd cmd/care/web && npx vitest run
```

**Expected**: All 19 pages display in selected locale. Zero hardcoded English in main content.

### V4: Keyboard Navigation (FR-017 through FR-019)

```bash
# In browser:
# 1. Navigate to /patients
# 2. Press Tab until the table receives focus
# 3. Press Down Arrow → focus should move to next row with visible outline
# 4. Press Up Arrow → focus returns to previous row
# 5. Press Enter → navigates to patient detail (or triggers row action)
# 6. Navigate to /reviews
# 7. Tab to filter toggles → screen reader should announce "radiogroup"
```

**Expected**: Arrow keys navigate rows. Enter triggers action. Visible focus ring on rows. Filter toggles announced as radiogroup.

### V5: Touch Targets & Card Layout (FR-020 through FR-022)

```bash
# In browser:
# 1. Open DevTools → Toggle device toolbar → Select "iPad"
# 2. Navigate through pages
# 3. Verify: all buttons and links have minimum 44x44px touch area
# 4. Switch to "iPhone SE" (375px width)
# 5. Navigate to /patients → table should render as stacked cards
# 6. Verify: cards show 3-4 key fields + "View details" link
```

**Expected**: No touch target below 44px on tablet. Card layout on mobile. No horizontal scrolling.

### V6: Alert Fatigue (FR-007 through FR-009)

```bash
# In browser:
# 1. Trigger a notification (via WebSocket or simulated)
# 2. Urgent notification → toast appears immediately
# 3. Normal notification → no toast, badge count increments
# 4. Check badge color: red if urgent exists, amber if normal only
# 5. Open notification dropdown → click DND toggle
# 6. Trigger urgent notification → no toast while DND active, badge still updates
```

**Expected**: Two-tier toast behavior. Color-coded badge. DND suppresses toasts but not badge.

### V7: Demo Safeguards (FR-010 through FR-012)

```bash
# In browser:
# 1. Load any page → "RESEARCH DEMO" banner visible at top
# 2. Dismiss the banner
# 3. Wait 30 minutes (or manually set sessionStorage timestamp) → banner reappears
# 4. Navigate to /cost → "Synthetic data" notice visible
# 5. Navigate to /rpm → "Synthetic data" notice visible
```

**Expected**: Persistent banner on all pages. Reappears after 30 min. Synthetic data notices on Cost/RPM.

## Full Test Suite Validation

```bash
# Frontend unit tests (must all pass)
cd cmd/care/web && npx vitest run
# Expected: 252+ tests pass (increases with new component tests)

# Go backend tests (must be unaffected)
make test
# Expected: all pass, no regressions

# Type check
cd cmd/care/web && npx tsc --noEmit
# Expected: no errors
```
