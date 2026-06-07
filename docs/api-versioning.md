# API Versioning Strategy

This document describes Bodh's approach to HTTP API versioning: the current
state (unversioned), the planned transition to versioned routes, the migration
strategy for existing consumers, and the breaking-change policy.

---

## Current State: Unversioned Routes

All HTTP endpoints in `cmd/care` are registered without a version prefix.
Representative routes:

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | Liveness probe |
| GET | `/readyz` | Readiness probe (Postgres ping) |
| GET | `/metrics` | Prometheus metrics |
| POST | `/patients` | Create/update patient records |
| POST | `/cdm` | Forward to CDM planner agent |
| POST | `/tcm/discharge` | Forward to TCM discharge agent |
| POST | `/panel` | Broadcast to panel manager |
| POST | `/pre_visit` | Forward to pre-visit prep agent |
| POST | `/inbox` | Forward to inbox triage agent |
| POST | `/refill` | Forward to refill manager agent |
| POST | `/priorauth` | Forward to prior authorization agent |
| POST | `/adt` | ADT event intake |
| POST | `/hl7/adt` | HL7 v2 ADT message intake |
| POST | `/rpm/observation` | RPM observation intake |
| GET/POST | `/fhir/Patient` | FHIR Patient resource |
| GET/POST | `/fhir/Encounter` | FHIR Encounter resource |
| GET/POST | `/fhir/Condition` | FHIR Condition resource |
| GET | `/tasks` | Task queue listing |
| GET | `/notifications` | Notification listing |
| GET | `/reviews` | HITL review queue listing |
| GET | `/reviews/{id}` | Single review detail |
| POST | `/reviews/{id}/decide` | HITL review decision |
| POST | `/api/transcribe` | Audio transcription |
| GET | `/audit` | Audit event listing |
| GET | `/explain/{case_id}` | Case reasoning trace |
| POST/DELETE/GET | `/admin/kill-switch` | Emergency halt controls |
| GET | `/fhir/bulkdata/kick-off` | FHIR Bulk Data export |
| GET | `/fhir/bulkdata/status/{id}` | Bulk export status |
| GET | `/fhir/bulkdata/download/{id}` | Bulk export download |

This flat structure works during early development when the API surface is
evolving rapidly and there are no external consumers with backward-compatibility
requirements.

---

## Planned: v1 Prefix with Stability Guarantee

When the API stabilizes for external consumption (EHR integrations, third-party
agent developers, compliance automation tools), routes will move under a `/v1/`
prefix:

```
/v1/patients
/v1/reviews
/v1/reviews/{id}
/v1/reviews/{id}/decide
/v1/audit
/v1/explain/{case_id}
/v1/tasks
/v1/notifications
```

### What v1 means

A route under `/v1/` carries a stability guarantee:

- **No removal** of existing fields in response bodies.
- **No type changes** to existing fields (a string stays a string).
- **No semantic changes** to existing fields (a field that returns UTC
  timestamps continues to return UTC timestamps).
- **Additive changes are allowed.** New fields may appear in response bodies.
  New optional query parameters may be added. New endpoints may be added under
  `/v1/`. Clients must tolerate unknown fields.

### Routes excluded from versioning

Infrastructure and operations routes are not versioned because they follow
Kubernetes and Prometheus conventions that have their own stability expectations:

| Path | Reason |
|---|---|
| `/healthz` | Kubernetes liveness probe convention |
| `/readyz` | Kubernetes readiness probe convention |
| `/metrics` | Prometheus `/metrics` convention |
| `/admin/kill-switch` | Internal operator API; not exposed to external consumers |

FHIR routes (`/fhir/Patient`, `/fhir/Encounter`, etc.) carry their own
versioning via the `fhirVersion` parameter in the `Accept` header, per the
FHIR specification. They are not double-versioned under `/v1/`.

HL7 v2 routes (`/hl7/adt`) follow the HL7 v2 message versioning convention
(MSH-12 field). They are not double-versioned.

---

## Migration Strategy

### Phase 1: Dual-Route Period

Both the old unversioned paths and the new `/v1/` paths will be registered
simultaneously. The unversioned path will proxy to the versioned handler
internally:

```go
// New versioned route
mux.HandleFunc("GET /v1/reviews", reviewListHandler())

// Legacy route: proxies to the same handler, adds deprecation header
mux.HandleFunc("GET /reviews", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Deprecation", "true")
    w.Header().Set("Sunset", "2026-03-01")
    w.Header().Set("Link", `</v1/reviews>; rel="successor-version"`)
    reviewListHandler()(w, r)
})
```

The dual-route period provides:

- **Deprecation header.** Every response from the unversioned path includes
  `Deprecation: true` per the IETF Deprecation HTTP header field draft (RFC
  8594 successor). Clients that inspect response headers see the deprecation
  signal immediately.

- **Sunset header.** The `Sunset` header indicates the date after which the
  unversioned route may be removed. Clients that support RFC 8594 can
  automate migration alerts.

- **Link header.** The `Link` header with `rel="successor-version"` points
  to the versioned equivalent, giving clients a machine-readable migration
  target.

### Phase 2: Deprecation Logging

During the dual-route period, every request to an unversioned path is logged
with a structured warning including the caller's `User-Agent`, source IP
(from `X-Forwarded-For` or `RemoteAddr`), and the versioned alternative path.
This gives operators visibility into which integrations still use old paths.

### Phase 3: Removal

After the sunset date, unversioned routes are removed. Requests to the old
paths return `410 Gone` with a JSON body pointing to the versioned path:

```json
{
  "error": "gone",
  "message": "This endpoint has been removed. Use /v1/reviews instead.",
  "successor": "/v1/reviews"
}
```

The 410 response (not 404) signals to caching proxies and clients that the
resource is intentionally gone, not merely missing.

---

## Breaking Change Policy

When a backward-incompatible change is necessary (field removal, type change,
semantic change), the process is:

### 1. Introduce a new version

Register the new behavior under `/v2/` (or `/v3/`, etc.). The previous
version continues to work unchanged.

```
/v1/reviews      -- old behavior (stable, maintained)
/v2/reviews      -- new behavior (breaking change)
```

### 2. Deprecate the old version

Add `Deprecation`, `Sunset`, and `Link` headers to the old version. The
sunset period is a minimum of 6 months for external consumers and 3 months
for internal consumers.

### 3. Communicate the change

Breaking changes are documented in:

- The release notes for the version that introduces the new API.
- A migration guide in `docs/` with before/after examples.
- The `CHANGELOG.md` entry with a `BREAKING` tag.

### 4. Remove the old version

After the sunset date, the old version returns `410 Gone`.

### Version lifecycle

```
  Active          Deprecated         Removed
  (no headers)    (Deprecation +     (410 Gone)
                   Sunset headers)
  [-- v1 --]  --> [-- v1 --]     --> [-- v1 --]
                  [-- v2 --]         [-- v2 --]
```

Only the two most recent versions are maintained simultaneously. When v3 is
introduced, v1 enters removal.

---

## Content Negotiation

All versioned endpoints support content negotiation via the `Accept` header:

| Accept | Response format |
|---|---|
| `application/json` (default) | JSON response body |
| `text/csv` | CSV export (for `/v1/audit` and `/v1/explain/{case_id}`) |

The `Content-Type` response header always matches the actual format returned.
If the requested format is not supported, the endpoint returns
`406 Not Acceptable`.

---

## Client Guidelines

Clients integrating with Bodh's API should follow these practices:

1. **Use versioned paths.** Once `/v1/` routes are available, switch to them.
   Unversioned paths will be removed.

2. **Tolerate unknown fields.** Response bodies may gain new fields in minor
   releases. JSON parsers should use lenient deserialization (ignore unknown
   keys).

3. **Inspect deprecation headers.** Check for `Deprecation: true` on every
   response. Automate alerts when detected.

4. **Send an identifying User-Agent.** This helps operators understand which
   integrations still use deprecated paths during the migration period.

5. **Handle 410 gracefully.** When a removed endpoint returns 410, read the
   `successor` field from the response body and update the client
   configuration.

---

## Timeline

| Milestone | Target | Notes |
|---|---|---|
| Current state | Now | All routes unversioned; internal consumers only |
| v1 routes introduced | When first external integration ships | Dual-route period begins |
| Unversioned routes deprecated | Same release as v1 introduction | Deprecation + Sunset headers added |
| Unversioned routes removed | 6 months after v1 introduction | 410 Gone responses |
| v2 (if needed) | Driven by breaking-change requirements | v1 enters deprecation at v2+6mo |
