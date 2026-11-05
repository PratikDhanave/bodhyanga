# Bodhyanga — Release 1.0 Readiness & Plan

> Evidence-based evaluation of everything currently in the repo and a concrete
> plan to a **credible 1.0**. Generated from live gate runs on `fix/care-ui-auth`
> (based on `main` @ `e32dfd3`) on 2026‑07‑31. Pairs with [ROADMAP.md](../ROADMAP.md).

---

## 0. TL;DR

**Two different "1.0"s exist, and they are months apart.**

| Track | What it means | Status | Distance |
|---|---|---|---|
| **A — OSS reference 1.0** | Engineering-complete reference; all gates green (incl. frontend); console actually builds, logs in and works; docs accurate | **~90% there** | days–weeks |
| **B — Clinical-grade 1.0** | Deployable for real patient care: validated, certified, under a BAA | **~15% there** | quarters (mostly non-code) |

The repo already carries `v1.0.0` and `v1.1.0` **tags**, but recent reality
undercut that claim: the embedded SPA had been frozen for weeks on a broken
build, backend auth was never wired, and the e2e suite was effectively 1/39.
**PR #58 closes most of that gap.** The single highest-leverage process fix is
adding a **frontend CI gate** — its absence is why the rot went unnoticed.

**Recommendation:** merge #58, add the frontend gate, cut an honest **`v1.2`
reference release** (Track A). Run Track B as a separately-scoped program with a
hard go/no-go gate at *clinical validation*.

---

## 1. Current-state evaluation (measured, not claimed)

All results below are from **actually running the gates**, not from docs.

### 1.1 Backend (Go) — 🟢 healthy

| Gate | Command | Result |
|---|---|---|
| Build | `go build ./...` | ✅ clean |
| Vet | `go vet ./...` | ✅ clean |
| Tests | `go test ./...` | ✅ **69 packages pass, 0 fail** (42 have no test files) |
| Race | `go test -race ./...` | enforced in CI (not re-run locally) |
| Coverage | `go tool cover -func` | ⚠️ **measured 70.9% — BELOW the 80% CI gate.** The gate is effectively **red on `main`**: 42 packages sit at 0% (mostly `cmd/*/main.go` entrypoints). README's "100% coverage" refers to 23 *library* packages, not the whole module. |
| Vuln | `govulncheck ./...` | enforced in CI |
| Nested module | `cd maf && go build/test` | separate CI job |

TODO/FIXME markers in Go source: **4** (very clean). ~35 documented
"planned / not-yet / stub" call-outs — these are the known gaps, not surprises.

### 1.2 Frontend (`cmd/care/web`) — 🟡 green *only after PR #58*

| Gate | Result |
|---|---|
| `tsc -b && vite build` | ❌ **broke** on `main` (missing `CostAnalytics` type/method) → ✅ fixed in #58 |
| `vitest run` | ✅ **14/14 tests pass** (6 files) |
| `playwright test` (e2e) | ⚠️ was **1/39** on the stale build; #58 fixes the build + auth blockers, but specs still need a **credential fix** (`clinician` → `clinician@hospital.org`; the login input is `type=email`) |
| Embedded `dist/` freshness | ❌ was frozen at Jul‑25 (7 routes behind source, no login) → ✅ rebuilt in #58 |

**The embedded SPA is `go:embed`'d and committed, but CI never rebuilds or tests
it.** So `src/` could stop compiling and the app kept serving a stale bundle
with nobody noticing. This is the #1 process defect.

### 1.3 Auth / console — 🟢 working *after PR #58*

Verified end-to-end via headless browser:
- `POST /api/auth/login` → token; `/api/*` without token → **401**, with token → **200**; wrong password → **401**.
- Login form → **dashboard** (19 agents load); **Audit Log** page renders (filters + events table).
- Full nav restored: Clinician Dashboard, Patient Portal, Audit Log (previously missing), i18n, dark mode.

Known follow-up: `/ws/hitl` WebSocket can't send a `Bearer` header, so live HITL
updates fail under auth (needs token-in-query or cookie auth for WS/SSE).

### 1.4 Product surface (from ROADMAP) — 🟢 broad, 🟡 deep in places

Roadmap item status: **84 🟢 done · 3 🟡 in-progress · 36 ⚪ planned · 1 🔵**.

- Phases **1–8** (MARA platform, care services, AI patterns, compliance-as-code,
  interop, clinical breadth, evaluation, production hardening): **largely done.**
- Phases **9–10** (compliance *maturity*, real UX): **mostly ⚪ planned.**

> ⚠️ Doc discrepancy to fix: ROADMAP marks Phase 5–8 **headers** 🟢 "Done" while
> their **bullets** are 🟡/⚪. Trust the bullets. README's "100% coverage" and the
> v1.0/v1.1 "production-ready" framing also need a truth pass.

### 1.5 Open PRs

| PR | Branch | Release role |
|---|---|---|
| **#58** | `fix/care-ui-auth` | **Release-critical** — makes the console build, log in, and show the audit page |
| **#57** | `docs/archify-flow-diagrams` | Docs — 6 architecture/flow diagrams + CLAUDE.md invariants |

---

## 2. Release scorecard

| Dimension | Score | Evidence |
|---|---|---|
| Backend build/test | 🟢 A | 69 pkgs pass, vet clean, build clean |
| Frontend build/test | 🟡 B− | green only after #58; **no CI gate** |
| Console usability | 🟢 B+ | works after #58; WS-auth follow-up |
| CI/CD rigor | 🟡 C+ | strong Go gates but **coverage gate is red (70.9% < 80%)**; **no frontend gate**; smoke tests good |
| Docs accuracy | 🟡 C+ | comprehensive but overstates "done"/coverage/production-ready |
| Architecture & invariants | 🟢 A | MARA, HITL gate, PHI-min audit, LLM→rule fallback all real |
| Clinical validation | 🔴 F | synthetic fixtures only; no real-case / IRB validation |
| Regulatory/compliance evidence | 🔴 D | policies-as-code exist; **no SOC2/HITRUST/ONC/BAA/21 CFR 11** |
| Multi-tenancy / ops | 🟡 C | single-tenant `/audit`; no SLO dashboards; output-fanout audit deferred |

---

## 3. Track A — Credible OSS 1.0 (the achievable milestone)

**Definition of done:** a fresh clone builds green (Go **and** frontend), the
console runs and authenticates, the e2e suite is green in CI, docs match reality,
and a real tag is cut. Estimated **days–weeks**.

### Blockers (ordered)

1. **Merge PR #58** — unblocks the frontend build, wires backend auth, fixes the
   Audit page. *Nothing else in Track A matters until this lands.*
2. **Add a frontend CI gate** (new job in `.github/workflows/ci.yml`):
   `npm ci → npm run lint → npm run build → npm run test → npx playwright test`.
   Fail the build on any red. *This is the fix that prevents recurrence.*
3. **Fix the e2e credential mismatch** — specs fill `#email` with `clinician`
   but the input is `type=email`; update to `clinician@hospital.org` (or make the
   seed/e2e agree) and get the 13 specs / 39 tests green.
4. **WebSocket/SSE auth** — let `/ws/hitl` and SSE run endpoints authenticate
   (token query-param or cookie) so live updates work under the gate.
5. **Fix the coverage gate (currently RED)** — module total is **70.9%**, below
   the 80% CI gate, so `main`'s CI is failing that step. Either (a) raise
   coverage — the cheap win is testing/excluding the 42 zero-coverage
   `cmd/*/main.go` entrypoints — or (b) scope the gate to library packages
   (`pkg/...`) where the "100% push" actually applies. Pick one and make CI green.
6. **Docs truth pass** — reconcile ROADMAP header vs bullet status; soften
   "production-ready"/"100% coverage"; make the research disclaimer the first
   thing a reader sees. (Merge #57 for the flow diagrams.)
7. **Tag hygiene** — cut `v1.2.0` from a green `main` (or re-baseline the
   existing tags with a CHANGELOG note explaining the correction).

### Nice-to-have before the tag
- Per-agent `HandleMessage` table tests (finish the 🟡 item).
- E2E test for the full HTTP API surface.
- Perf benchmark (message throughput) + a chaos test asserting no PHI leaks.

---

## 4. Track B — Clinical-grade 1.0 (the real regulatory lift)

This is **mostly not code** and gated by validation. Sequence:

### 4.1 Clinical validation (the go/no-go gate)
- Replace synthetic fixtures with **licensed real-world cases**; land the NEJM
  SD‑Bench loader (currently ⚪ pending license).
- Stand up an **IRB-approved shadow-mode pilot** (`pkg/shadow` already captures
  concordance) — measure accuracy/safety against clinicians with no patient impact.
- Publish a **stratified bias audit** (the equity pipeline exists).

### 4.2 Compliance evidence
- **SOC 2 Type II** + **HITRUST CSF** evidence pipeline (code → policy → evidence).
- **ONC certification** artifacts: USCDI conformance, SMART App Launch suite, Bulk Data.
- **21 CFR Part 11** e-signatures for trial deployments; **RFC 3161** timestamping on audit.
- `RequireMinimumNecessaryPolicy`; **right-to-delete** tooling (HIPAA vs GDPR tension);
  **GDPR Article 35 DPIA**.

### 4.3 Legal & identity
- Sign a **BAA**; integrate a **real IdP** (SMART-on-FHIR *scope* policy ships;
  auth-server integration is pending).

### 4.4 Production ops
- Per-request **multi-tenant `/audit`** routing (single-tenant today).
- **Output-fanout audit hooks** (only the inbound hop is audited today).
- Cost/latency **SLO dashboards** (Prometheus + Grafana).

---

## 5. Recommended sequence

```
Now ──► merge #58 ──► add frontend CI gate ──► fix e2e creds + WS auth ──► docs truth pass ──► tag v1.2.0 (Track A done)
                                                                                                    │
                                                                                                    ▼
                                            Track B program (separate, gated): clinical validation ─► compliance evidence ─► BAA/IdP ─► clinical pilot
```

**Do not** advertise clinical readiness until §4.1 passes. Keep the
"not a medical device" disclaimer load-bearing until a BAA + validation exist.

---

## 6. Immediate next actions (this week)

- [ ] Merge **PR #58** (release-critical).
- [ ] Open a PR adding the **frontend CI job**; make it required on `main`.
- [ ] Update the Playwright login helper to `clinician@hospital.org` / `clinic123`; run the suite to a green number.
- [ ] File issues for **WS/SSE auth** and the **ROADMAP/README truth pass**.
- [ ] **Fix the red coverage gate** — total is **70.9%** (< 80%); test/exclude the `cmd/*/main.go` entrypoints or scope the gate to `pkg/...`.
- [ ] Decide tag strategy (`v1.2.0` vs re-baseline) and draft CHANGELOG.

---

*Evaluation method: `go build/vet/test ./...`, `vitest run`, headless-browser auth
flow, `.github/workflows/ci.yml` inspection, and ROADMAP/CHANGELOG status counts.
Re-run these gates before cutting any tag.*
