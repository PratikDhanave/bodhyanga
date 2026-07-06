# CI, Ops & Frontend Conventions

Observed build/CI/infra/frontend patterns, with `file:line` evidence. See
[README.md](README.md) for context and [testing.md](testing.md) for the enforced CI gates.

---

## 1. Makefile structure

**RULE: One unified Makefile drives every workflow; every tunable is a `?=` variable
overridable as `make <target> VAR=value`.**

- Single source of truth (`Makefile:1-18`): "Every workflow lives here — build, test, evals,
  Ollama, SPA, Docker Compose, migrations, Kubernetes, and container image builds."
- Targets grouped under `# ====` ASCII-banner sections (BUILD & TEST, RELEASE, LESSONS,
  EVALS, OLLAMA, OPA, SPA, DOCKER COMPOSE, DB MIGRATIONS, CONTAINER IMAGES, KUBERNETES).
- Toolchain indirection: `GO/DOCKER/NPM/KUBECTL/PKG` all `?=` (`Makefile:22-26`).
- `.DEFAULT_GOAL := help` (`:133`) — bare `make` prints help.

**Per-lesson override convention.** Each runnable lesson gets a
`<NAME>_PROVIDER` / `<NAME>_MODEL` / `<NAME>_MESSAGE` triplet, all `?=`, injected via
`$(if $(X_MODEL),-model=$(X_MODEL),)`. (`Makefile:38-100`, consumed `509-620`.)

**`build-tags` (ADR-0011 remediation):** compiles every openly-buildable tag combo — `go
build` (default), `go build -tags otel`, `go vet -tags otel` (`Makefile:288-307`). `nats`
excluded until its dep lands; `kronk` only builds in `docker-admin`. Its docstring names the
failure mode it guards ("the nats bus silently stopped compiling — no CI lane built it").

---

## 2. CI workflows

**RULE: a single `.github/workflows/ci.yml` runs on push/PR to `main` with least-privilege
`permissions: contents: read`; parallel jobs gate the merge.** Go pinned `1.26`, Node `20`.

| Job | Purpose |
|---|---|
| `build-test` | build / vet / `go test -race -coverprofile`; **coverage gate ≥ 90%**; repeats build+vet+test under `-tags otel` for `pkg/observability`; smoke-runs demo, bench, `TestIntegration` MARA pipeline; renders both kustomize overlays |
| `frontend` | `npm ci` → `tsc --noEmit` → `vitest run --coverage`; **line-coverage gate ≥ 80%** |
| `e2e` | `needs: [build-test, frontend]`; Playwright chromium; uploads `test-results/` on failure |
| `security` | gosec, `continue-on-error: true` — advisory |
| `vuln` | `govulncheck ./...` — blocking |
| `docker` | `docker build -f Dockerfile` (root Dockerfile, tag `bodh/care:ci`) |

**Known gaps:** CI does **not** run `opa-test` (no Rego lane), does **not** run the full
`build-tags` matrix (only `otel`), and invokes tools directly rather than via `make`. See
[testing.md](testing.md) §5.

---

## 3. DB migrations

**RULE: migrations are forward-only, `NNNN_description.sql`, embedded in the binary; the
canonical set is `pkg/migrate/sql/`, applied idempotently and transactionally by
`bodhadmin migrate`.**

- Naming/parsing contract (`pkg/migrate/migrate.go:100-140`): requires `NNNN_description.sql`,
  integer version prefix, rejects duplicate versions and empty files, sorts ascending.
- **No down-migrations — roll forward only** (`Up()` applies each unseen version once,
  `:198-231`).
- Embedded via `//go:embed sql/*.sql` (`pkg/migrate/embedded.go:19`).
- Each migration + its `schema_migrations` row + an `audit_events` row commit **atomically**
  (`:231-269`).
- Tool: `cmd/bodhadmin migrate` (flags `-dsn` from `$BODH_DB_DSN`, `-timeout` 60s,
  `-dry-run`; exit codes 0/1/2). Make wrappers: `make migrate`, `make migrate-status`
  (`Makefile:846-854`).

> **Tech-debt to know:** there are **two** migration directories. The *applied* embedded set
> is `pkg/migrate/sql/` (clean sequential). The top-level `db/migrations/` is a **separate,
> non-embedded** DDL reference with **duplicate `0002` prefixes** — it would fail `Load()`'s
> dup-version check and is never embedded. `pkg/migrate/migrate_doc.go` calls `pkg/migrate/sql`
> the "Bodh-canonical" set. Don't confuse them.

---

## 4. OPA / Rego

**RULE: Rego policies mirror the Go policy structs, live under `pkg/*/rego/`, use
`import rego.v1`, and are tested in-process with `opa test` (no server). Every result rule
returns `{"allowed": bool, "reason": string}`, and the reason strings are a PHI-free,
schema-stable public contract.**

- Locations: `pkg/governance/rego/` (`medical.rego` + `medical_test.rego`) and `pkg/auth/rego/`
  (`authentication/audit/explain/reviews.rego`). Declared once: `OPA_DIRS := pkg/governance/rego
  pkg/auth/rego` (`Makefile:729`).
- Targets pass `--v1-compatible`: `opa-test` / `opa-fmt --write` / `opa-check`
  (`Makefile:742-755`); `_opa_install` auto-installs the OPA CLI.
- `authentication.rego` pins RS256 as defense against `alg=none`; `default auth := false`.
- Tests live beside policy as `<name>_test.rego`, package `<pkg>_test`, `test_*` rules with
  `with input as {…}`, asserting exact reason strings.
- **Gap:** `opa-test` is local-only — not wired into `ci.yml`.

---

## 5. Infra (`zarf/`)

**RULE: all deploy artifacts live under `zarf/` — Compose for local dev, Kustomize
base+overlays for k8s, one Dockerfile per binary. All service images are version-pinned.**

- **Compose** (`zarf/compose/docker_compose.yaml`, `make compose-up`): pinned
  `postgres:16.4-alpine`, `ollama/ollama:0.5.4`, `bodh-care`, plus a full
  Prometheus/Grafana/Tempo/Loki observability stack (there is **no Laminar container** in
  compose — "Postgres + Laminar + Bodh" in older docs is really Postgres + Ollama + care +
  the o11y stack). `compose-clean` guards volume deletion with an interactive `[y/N]` prompt.
- **Kustomize:** `zarf/k8s/base/` holds full manifests
  (deployment/service/configmap/hpa/pdb/networkpolicy/…); `dev/` and `prod/` are patch
  overlays selected by `K8S_ENV`. `k8s-validate` renders + `kubectl apply --dry-run=client`
  (no cluster needed); `k8s-diff`/`k8s-apply`/`k8s-status`/`k8s-logs` scoped to namespace
  `bodh`. CI independently validates both overlays.
- **Dockerfiles:** `zarf/docker/Dockerfile.{care,bodhctl,bodhadmin}`; `docker-admin` passes
  `--build-arg TAGS=kronk`. (CI's `docker` job builds a *separate* root `Dockerfile`.)
- **Env config:** copy `.env.example` → `.env` (gitignored), `set -a; . ./.env; set +a`. All
  keys empty with commented defaults, framed as "research / demo use".

---

## 6. Frontend conventions (`cmd/care/web`)

The SPA is **React 18 + TypeScript + Vite + Tailwind** (not Vue — a stale claim now fixed in
CLAUDE.md).

**RULE: build is always `tsc -b && vite build` — types gate the bundle.** (`package.json:9`.)
React `^18.3.1`, `react-router-dom ^6`, `vitest`, `@playwright/test`, `tailwindcss ^3`.
Scripts: `dev`/`build`/`preview`, `lint` = `tsc -b --noEmit`, `test` = `vitest run`,
`test:e2e` = `playwright test`.

**RULE: TypeScript is maximally strict** — `strict` + `noUnusedLocals` + `noUnusedParameters`
+ `noFallthroughCasesInSwitch` (`tsconfig.json:14-17`); CI enforces via `tsc --noEmit`.

**RULE: `src/` is split by role** — `pages/`, `components/`, `hooks/`, `api/`, `i18n/`,
`types/`, `test/`; each carries a co-located `__tests__/`. ~20 pages, ~19 shared components.

**RULE: routing is a single declarative `<Routes>` tree in `App.tsx` (react-router v6)** —
public `/login`, an authed `<AuthedLayout>` element route wrapping the rest, URL params like
`/runs/:runId`, catch-all `path="*"` → `<Navigate to="/" replace />`.

**RULE: i18n is build-time static dictionaries, no LLM/network; four locales `en/es/fr/zh`;
lookup falls back locale → English → raw key.** `src/i18n/index.ts`:
`t(key, locale) = translations[locale]?.[key] ?? translations.en[key] ?? key`; `useI18n()`
throws outside `<I18nProvider>`.

### Clinical safety-UX patterns (specs/001)

These are load-bearing safety controls, not cosmetics — preserve their behavior when editing:

- **Confirmation modal** (`ConfirmationModal.tsx`): destructive AI-diagnosis confirmation
  requires a typed free-text reason **AND** a mandatory time delay (`confirmDelay = 2000ms`)
  before Confirm enables; 44px min touch target; red `danger` styling.
- **Confidence display** (`ConfidenceDisplay.tsx`): raw model probability is **always**
  rendered as a labeled, color-coded category (Low<0.5 / Moderate<0.7 / High<0.9 / Very High)
  with an explanatory `role="tooltip"` — never a bare number; value clamped to [0,1].
- **Research-demo banner** (`DemoBanner.tsx`): a persistent "RESEARCH DEMO — Not for clinical
  use" banner shows by default, stays dismissed only 30 min (sessionStorage), and **fails open
  (visible)** if storage is unavailable. (Ties to ADR-0006 — the research-vs-clinical identity
  question.)

Tailwind uses a custom `bodh-*` palette with `dark:` variants throughout.
