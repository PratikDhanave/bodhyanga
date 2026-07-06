# Testing Conventions

Observed testing patterns and the **actual enforced gates**, with `file:line` evidence. See
[README.md](README.md) for context.

---

## 1. Database tests — `pkg/dbtest`

**RULE: For a DB-backed test, call `db := dbtest.NewDB(t)` and use `db.Store` + `db.TenantID`;
it lazily boots one shared Postgres container per test binary and auto-wipes your tenant on
cleanup. If Docker is absent the test SKIPS, never fails.**

```go
func TestEvalStoreRoundTrip(t *testing.T) {
	db := dbtest.NewDB(t)                 // pkg/persistence/postgres/postgres_integration_test.go:26
	db.Store.SaveInteraction(ctx, db.TenantID, rec)
	got, _ := db.Store.ListInteractions(ctx, db.TenantID)
}
```

- Docker-missing skip: `t.Skip("docker not available; ...")` (`pkg/dbtest/dbtest.go:76-78`);
  container-start failure also skips, not fails (`:84`). Spin-up gated by `sync.Once` — one
  container per binary (`:80`).
- Container: `tcpostgres.Run(ctx, "postgres:16-alpine", ...)` with `WithSQLDriver("pgx")`,
  90s timeout.
- **Isolation is per-tenant, not per-DB:** `newTenantID()` → `"tenant_test_" + 16 hex`;
  migrations run as the owner role, the real pool opens as non-superuser `bodh_app` so RLS
  actually applies. `AssertNoBypassRLS(t, db)` guards against false-positive isolation.
- **Two skip idioms coexist:** external-package integration tests
  (`*_integration_test.go`, use `dbtest`) vs. internal-package tests (`postgres_test.go`, use
  the local `requirePostgres(t)` helper for unexported symbols). Same skip behavior.

---

## 2. Build-tag test stubs

**RULE: A `!tag` test proves the stub contract (runs in default CI); the real test is gated
behind the tag and runs only in a dedicated lane — so default `make test`/`make build` stays
green.**

- **otel:** `pkg/observability/otlp_test.go` (`//go:build otel`, real exporter vs. httptest
  fake OTLP collector) runs only via `make test-otel` → `go test -tags otel
  ./pkg/observability/...`. The `!otel` path is covered by the stub.
- **kronk:** `kronk_test.go` (`//go:build !kronk`) verifies the stub contract ("New errors
  loudly, Provider/Close are inert but safe") **in default CI**; `kronk_real_test.go`
  (`//go:build kronk`) injects a `fakeBackend` — never a real model — and `t.Skip`s on
  unsupported OS to stay compilable on Windows runners.
- `make build-tags` (`Makefile:288-307`) compile-checks default + `otel` so tagged code can't
  rot (this exists because the `nats` bus silently stopped compiling). `kronk` builds only in
  `docker-admin` (`--build-arg TAGS=kronk`).

---

## 3. Table-driven tests + testdata fixtures

**RULE: Table-driven tests use `cases := []struct{name; ...}` then
`for _, tc := range cases { t.Run(tc.name, func(t *testing.T){...}) }` with human-phrase
subtest names.** Canonical: `pkg/llm/grammar_test.go:28-65`. Both `tt :=` and `tc :=` idioms
are in use.

- **Not everything is table-driven** — one-assertion-per-`func Test<Subject>_<Scenario>` is
  equally idiomatic: `pkg/guardrails/guardrails_test.go` (`TestPIIGuardrail_DetectsSSN`, …),
  `pkg/tools/dosing_test.go`.
- **Fixtures live in `pkg/<name>/testdata/`, loaded through a `t.Helper()` loader:**
  `pkg/hl7v2/testdata/*.hl7` via `loadFixture(t, name)` (`hl7v2_test.go:12-19`). Only two
  `testdata/` dirs exist (`pkg/hl7v2`, `pkg/dbtest`). **No `.golden` / `-update` convention
  exists** — assertions are inline field checks, not golden diffs.

---

## 4. Frontend tests — `cmd/care/web`

**RULE: Unit tests are `*.unit.tsx` (or `.unit.ts`) inside co-located `__tests__/`, run under
Vitest + jsdom with a shared `src/test/setup.ts`. Playwright e2e specs are `*.spec.ts` in
`e2e/`. NEVER name a unit test `*.spec.*` — Playwright's default `testMatch` would try to load
it** (the reason for the `.unit.` convention, stated in `vitest.config.ts`).

- ~41 unit files, ~21 e2e specs.
- Vitest config: `environment: 'jsdom'`, `globals: true`, `setupFiles: ['./src/test/setup.ts']`,
  `restoreMocks: true`.
- Shared setup (`src/test/setup.ts`): `@testing-library/jest-dom/vitest` matchers; `afterEach`
  runs `cleanup()` + `vi.restoreAllMocks()` + `localStorage.clear()`; exports reusable stubs —
  `StubEventSource` (SSE with `eventSourceFire`/`resetEventSources`), a `localStorage`
  polyfill, and `mockFetch(handler)`.
- Unit convention: `@testing-library/react` `render/screen`, `userEvent.setup()`, queries by
  role, fake timers via `vi.useFakeTimers({ shouldAdvanceTime: true })`.
- Playwright (`playwright.config.ts`): `testDir: './e2e'`, `baseURL:
  'http://localhost:8088'`, a `webServer` block; each spec defines its own `login(page)`
  helper and accepts "empty-state OR rows".
- Make wrappers: `test-web`, `test-web-watch`, `test-e2e`, `test-e2e-install`,
  `test-all` (= `test test-web`, **does not** include e2e).

---

## 5. The actually-enforced gates

**RULE: `make ci` (= vet + test + govulncheck) is the local convenience gate; the real gate is
`.github/workflows/ci.yml`. A developer must pass: Go coverage ≥ 90%, frontend line coverage
≥ 80%, and govulncheck — everything else is advisory or a smoke test.**

| Gate | Where | Threshold / behavior |
|---|---|---|
| Go race + coverage | `ci.yml:32-44` | `go test -race -coverprofile`; **fails below 90%** |
| Frontend coverage | `ci.yml:108-121` | `vitest run --coverage`; **fails below 80%** |
| govulncheck | `ci.yml:182-194` | `govulncheck ./...` — blocking |
| gosec | `ci.yml:163-180` | `continue-on-error: true` — advisory |
| bench | `ci.yml:55` | 5-second smoke run only — **no** `-fail-on-regression` |
| kustomize overlays | `ci.yml:61-65` | renders dev + prod |

Note the mismatch: `make coverage-check` (`Makefile:396-400`) fails below **80%** and is
advisory-local; CI's Go gate is stricter at **90%**. `make race`/`make cover` are local only.
CI does **not** run `opa-test` and does **not** run the full `build-tags` matrix (only `otel`
is exercised inline; `kronk`/`nats` never build in open CI).

---

## 6. Eval-as-test / gating (the honest state)

**RULE (observed reality): there is NO clinical eval gate on deploy today.** Coverage +
govulncheck are the only enforced gates. `pkg/graduation`, `pkg/shadow`, and the bench
regression logic are all implemented and unit-tested but **unwired** to any deploy gate
(ADR-0010, Status: Needs-Decision):

- Bench regression gating exists (`cmd/bench/main.go` accuracy floor + cost ceiling, baselined
  in `data/cases/baseline.json`) but CI runs bench **without** `-fail-on-regression`.
- `pkg/graduation` (Wilson-lower gate + autopilot fence) has **zero non-test importers**.
- `pkg/shadow` records `ConcordanceRate` to a JSONL ledger and dead-ends — not fed into
  graduation.
- **Incompatible pass scales:** judge `0.6`, multiturn `0.6`, graduation `0.95` Wilson-lower,
  equity 10pp disparity. Duplicated `diagnosisMatch` and Wilson implementations across
  packages.

Codify these as **aspirational / manual** until ADR-0010 is decided — do not describe them as
gates.
