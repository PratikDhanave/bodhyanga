# Data Model: Remaining Plan Items

**Date**: 2026-06-10 | **Spec**: [spec.md](./spec.md)

## Entities

### ConfirmationModal Props

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| isOpen | boolean | yes | Controls modal visibility |
| title | string | yes | Modal heading (e.g., "Confirm Diagnosis Approval") |
| diagnosisSummary | string | yes | AI-generated diagnosis text |
| patientContext | string | yes | Patient name + relevant context |
| confidence | number | yes | 0-1 confidence float from diagnostician |
| onConfirm | (reason: string) => void | yes | Callback when confirmed with reason |
| onCancel | () => void | yes | Callback when cancelled |
| confirmDelay | number | no | Delay in ms before confirm is clickable (default: 2000) |

**State transitions**: Closed → Open (user clicks Accept/Reject) → Reason entered → Confirm enabled (after delay) → Confirmed (fires onConfirm) or Cancelled (fires onCancel) → Closed

### ConfidenceDisplay Props

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| value | number | yes | Confidence score 0-1 |
| showTooltip | boolean | no | Show explanatory tooltip on hover (default: true) |
| size | 'sm' \| 'md' | no | Display size (default: 'md') |

**Derived values**:
| Range | Category | Color (light) | Color (dark) |
|-------|----------|---------------|-------------|
| 0.00-0.50 | Low | red-600 | red-400 |
| 0.50-0.70 | Moderate | amber-600 | amber-400 |
| 0.70-0.90 | High | blue-600 | blue-400 |
| 0.90-1.00 | Very High | emerald-600 | emerald-400 |

### DemoBanner State

| Field | Type | Description |
|-------|------|-------------|
| dismissed | boolean | Whether banner is currently hidden |
| dismissedAt | number \| null | Timestamp (ms) when dismissed, stored in sessionStorage |
| RESHOW_INTERVAL | constant | 30 minutes (1800000 ms) |

### NotificationTier

| Priority | Toast Behavior | Badge Color |
|----------|---------------|-------------|
| urgent | Immediate toast | Red dot |
| high | Immediate toast | Red dot |
| normal | Silent (badge only) | Amber dot |
| low | Silent (badge only) | Amber dot |

### DND Mode State

| Field | Type | Description |
|-------|------|-------------|
| active | boolean | Whether DND is currently enabled |
| expiresAt | number \| null | Timestamp when DND ends |
| duration | 15 \| 30 \| 60 | Selected duration in minutes |

### CardTable Props

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| columns | CardColumn[] | yes | Column definitions for card rendering |
| data | any[] | yes | Row data array |
| onRowClick | (row: any) => void | no | Row click handler |
| keyField | string | yes | Unique key field name |

**CardColumn**:
| Field | Type | Description |
|-------|------|-------------|
| key | string | Data field name |
| label | string | Display label |
| showInCard | boolean | Whether to show in card mode |
| primary | boolean | Whether this is the card title field |

### i18n Key Structure (new keys)

Pages requiring new keys (15 pages × ~13 keys avg = ~200 total):

| Page | Key Prefix | Estimated Keys |
|------|-----------|---------------|
| about | `about.*` | 8 |
| agents | `agents.*` | 12 |
| auditLog | `auditLog.*` | 15 |
| bench | `bench.*` | 14 |
| caseRunner | `caseRunner.*` | 12 |
| cost | `cost.*` | 14 |
| dashboard | (extend existing) | 5 |
| fineTune | `fineTune.*` | 12 |
| llmTraces | `llmTraces.*` | 15 |
| panelReport | `panelReport.*` | 14 |
| patients | `patients.*` | 12 |
| rpm | `rpm.*` | 14 |
| reviewDetail | `reviewDetail.*` | 15 |
| reviews | (extend existing) | 8 |
| runTrace | `runTrace.*` | 10 |
| **Total** | | **~200** |
