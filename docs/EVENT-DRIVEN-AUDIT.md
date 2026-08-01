# Bodh — Whole-Platform Event-Driven Audit

> Goal: make Bodh *pure event-driven* end-to-end. This audit inventories every
> surface that is still request/response or fetch-on-mount, defines the target
> event contracts, and gives a phased migration plan. Generated 2026-08-01 from
> the live tree.

---

## 0. Executive summary

**The core is already event-driven; the edges are not.**

- 🟢 **Agent mesh** — agents never call each other; they emit typed messages on
  the in-process bus (`pkg/comm`), routed by the orchestrator. The only direct
  `HandleMessage` calls are *internal composition* (fallback siblings, cascade
  tiers, vision base) — not bus bypasses. This layer is pure event-driven.
- 🟢 **Run trace** — the case-runner streams bus events to the SPA over SSE
  (`/api/runs/{id}/events`), with buffered replay. Event-driven.
- 🔴 **Backend state changes don't broadcast** — 48 read handlers (`writeJSON`
  of current state) vs 9 `bus.Publish` sites. Mutations (review decided, task
  created, patient upserted, panel recomputed) do **not** emit an event a client
  can subscribe to.
- 🔴 **The WebSocket hub was mounted but dead** — `main.go` built the hub *from*
  the review queue but never set `reviews.Hub`, so `Queue.Submit` never called
  `Hub.NotifySubmit`. The notification bell connected and received nothing.
  **(Fixed in the first slice — see §5.)**
- 🔴 **15 SPA pages fetch-on-mount** — 22 `useApi()` call sites, 28 `reload`
  buttons. Data is a one-shot snapshot; nothing updates when the underlying
  state changes. The `hitl.Queue` doc comment literally assumes *"a UI polling
  /reviews/pending every 5 seconds."*

**Net:** the platform is event-driven where agents talk to agents, and
request/response everywhere a human (via the SPA) observes state. Closing the
gap means a **backend broadcast layer** + a **frontend subscription layer**.

---

## 1. Audit — non-event-driven surfaces

### 1.1 Backend

| Surface | Today | Target |
|---|---|---|
| Review submit / decide | `Queue.Submit`/`Decide` mutate in-memory; **now** notify `Hub` (§5) | Broadcast `review_submitted` / `review_decided` ✅ (slice 1) |
| Nurse tasks | `tasks.Queue` mutated by agents; read via `GET /api/tasks` | Broadcast `task_created` / `task_updated` |
| Patients | `POST /patients` upserts registry; read via `GET /api/patients` | Broadcast `patient_upserted` |
| Panel report | recomputed on `GET /api/panel/report` | Broadcast `panel_recomputed` on roster change |
| Runs | SSE per-run ✅ | keep |
| Notifications | `engagement.NewInMemory()` inbox, read via `GET /api/notifications` | Broadcast `notification_sent` |
| Cost / bench / traces | snapshot reads | low priority; poll or on-demand is fine |

The hub currently speaks only two event kinds (`NotifySubmit`, `NotifyDecide`).
It needs to become a **general per-tenant event fan-out**, or a second bus tap
(like the run manager's broadcast subscriber) should translate domain bus
messages into WS events.

### 1.2 Frontend (`cmd/care/web`)

Fetch-on-mount pages (each re-fetches only on manual `reload`):

```
Dashboard  ClinicianDashboard  Reviews  ReviewDetail  Tasks  Patients
Panel      PatientPortal       Agents   RPM           Bench  LLMTraces
Cost       FineTune            CaseRunner(fixtures)
```

Already event-driven: `RunTrace` (SSE), `NotificationBell` (WS, but see §5).

Every page uses the same `useApi(fn, deps)` hook — a one-shot fetch with a
`reload()`. There is no mechanism for the server to tell a page "your data
changed."

---

## 2. Target event contracts

### 2.1 Bus messages (already typed — the source of truth)
Diagnostic: `presentation · case_state · questions_complete · tests_complete ·
diagnosis_proposal · diagnosis · review_request`. Care-services: `cdm_request ·
care_plan_ready · discharge_event · tcm_tasks_created · panel_request ·
panel_report · engagement_sent · …` (see [README Clinical message protocol]).

### 2.2 Client event stream (WS) — proposed
Generalize `/ws/hitl` → `/ws/events?tenant_id=…` carrying a small envelope:

```json
{ "kind": "review_submitted", "tenant_id": "default", "id": "…", "ts": "…" }
```

Kinds (v1): `review_submitted`, `review_decided`, `task_created`,
`task_updated`, `patient_upserted`, `panel_recomputed`, `notification_sent`.
The payload is intentionally **thin** (id + kind) — clients treat it as an
*invalidation signal* and re-read the authoritative endpoint. This keeps PHI off
the socket and avoids duplicating the read model.

### 2.3 Auth
Streams (SSE + WS) authenticate with a **60s stream ticket**
(`POST /api/auth/stream-ticket`), same-origin enforced. (Shipped.)

---

## 3. Phased migration plan

**Phase 1 — Backend broadcast layer**
1. ✅ Wire `reviews.Hub` (slice 1).
2. Add a bus tap (mirror `newRunManager`'s broadcast subscriber) that maps
   domain bus messages → WS envelopes and fans them to per-tenant subscribers.
3. Emit `task_*`, `patient_upserted`, `panel_recomputed`, `notification_sent`.
4. Rename the endpoint to `/ws/events` (keep `/ws/hitl` as an alias one release).

**Phase 2 — Frontend subscription layer**
1. Add `useLiveResource(fetchFn, deps, kinds[])`: wraps `useApi`, opens the WS
   once (shared), and calls `reload()` when an event of a matching `kind`
   arrives (debounced). One hook swap per page.
2. Migrate the high-signal pages first: Reviews, Tasks, Dashboard,
   ClinicianDashboard, Patients, Panel.

**Phase 3 — Remove polling assumptions**
1. Delete the "poll every 5s" comment/contract on `hitl.Queue`.
2. Drop `reload` buttons where an event now covers the update (keep as a manual
   fallback where useful).
3. E2E: assert a second browser context sees a review appear **without reload**.

---

## 4. Risks & trade-offs

- **Thin vs fat events.** Thin (id+kind → re-read) keeps PHI off the socket and
  the read model single-sourced, at the cost of an extra fetch per event. Chosen
  for HIPAA hygiene (mirrors the audit-log "enough to reconstruct, never enough
  to leak" stance).
- **Reconnect / missed events.** WS drops must trigger a full `reload()` on
  reconnect so no update is lost (the hook handles this).
- **Fan-out cost.** In-process hub is fine for one binary; a horizontally-scaled
  deployment needs the bus swapped to NATS/Kafka and a shared fan-out — already
  the documented production path.
- **Tenant isolation.** Every WS subscription is tenant-scoped; the envelope
  carries `tenant_id` and the hub must never cross tenants (single-tenant today).

---

## 5. Slice 1 (done) — HITL event path wired

`main.go` now constructs the hub before `orch.Start` and sets `reviews.Hub`, so
`Queue.Submit`/`Decide` broadcast `review_submitted`/`review_decided` to
connected clinicians over the ticket-authenticated `/ws/hitl`. Previously this
path was dead (hub mounted, queue never notified it). This unblocks Phase 2 for
the Reviews and Dashboard pages.

---

*Method: static inventory of `useApi`/`reload`/`bus.Publish`/`writeJSON` sites,
`pkg/hitl` hub/queue wiring, and the orchestrator bus discipline. Re-run the
greps in §1 to track progress.*
