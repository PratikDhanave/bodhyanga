# pkg/persistence/postgres

Production-grade PostgreSQL backend for Bodhyanga's `eval.Store`,
`medical.PatientRegistry`, and `audit.Recorder` contracts. The file-based
`pkg/persistence.FileStore` and in-memory implementations next to each
interface remain the supported demo path; `cmd/care` selects the Postgres
backend only when `BODHYANGA_DB_DSN` is set.

## Schema

Three tables, all keyed by `(tenant_id, ...)`:

| Table                  | Purpose                          | PHI handling                                        |
|------------------------|----------------------------------|-----------------------------------------------------|
| `interaction_records`  | `eval.InteractionRecord`         | `metrics` / `metadata` JSONB; metadata may hold PHI |
| `patient_records`      | `medical.PatientRecord`          | Full record in `payload_ct` (BYTEA)                 |
| `audit_events`         | `audit.Event` — append-only      | Pseudonymous IDs only; no free-text body            |

PHI-bearing columns are typed `BYTEA` and named `_ct` (ciphertext) so a
follow-up PR can wire `pgcrypto` / KMS envelope encryption without a schema
migration. Today the package writes plaintext JSON bytes into those columns —
the at-rest encryption boundary is the next planned change.

Indexes (commented inline in `migrations/0001_init.sql`):
- `(tenant_id, created_at DESC)` on every table — workhorse list index.
- `(tenant_id, kind)` on `audit_events` — filter by event kind.
- GIN on `patient_records.conditions` — backs `FilterByCondition` queries.

## RLS model

Tenancy is enforced by Postgres Row-Level Security, not by application
filters. The application is defence in depth — every query in `store.go`
goes through `withTenant(ctx, tenantID, fn)` which:

1. Opens a transaction.
2. Runs `SELECT set_config('app.tenant_id', $1, true)` (LOCAL to the tx).
3. Executes the caller's queries — RLS policies test
   `tenant_id = current_setting('app.tenant_id', true)`.
4. Commits.

A forgotten SET means `current_setting` returns the empty string, which
matches no rows — failure-closed.

Two roles:
- **Owner / migrator** — runs `db/migrations/*.sql`. Has DDL and BYPASSRLS
  is OK because migrations need to act on every tenant's rows. Do NOT use
  this role for the application.
- **`bodhyanga_app`** — application role created by `0002_rls.sql` with
  `NOBYPASSRLS` and minimum CRUD grants. `audit_events` is granted only
  `SELECT, INSERT` — UPDATE and DELETE are withheld so the audit log is
  append-only at the database layer (HIPAA §164.312(b), 21 CFR Part 11).

The `FORCE ROW LEVEL SECURITY` clause means the policy also applies to the
table owner, so a misconfigured connection string can't accidentally bypass
isolation.

## Pointing `cmd/care` at it

```bash
export BODHYANGA_DB_DSN='postgres://bodhyanga_app:bodhyanga_app@localhost:5432/bodhyanga?sslmode=disable'
export BODHYANGA_TENANT_ID='clinic-42'   # optional; defaults to "default"
go run ./cmd/care
```

Startup logs the active backend:
- `[care] persistence backend: postgres (tenant=clinic-42)` — connected.
- `[care] persistence backend: in-memory + file (BODHYANGA_DB_DSN not set)` — demo.
- `[care] persistence backend: in-memory fallback (postgres open failed: ...)`
  — DSN provided but the pool failed to ping; the in-memory path still works.

Migrations run automatically on startup via the owner DSN; on subsequent
boots as `bodhyanga_app` the runner is a no-op (already applied).

## Backups

Use `pg_dump` against the owner role; do NOT dump as `bodhyanga_app` because the
RLS policies would clip the export to the empty tenant. Typical command:

```bash
pg_dump --format=custom --no-owner --no-privileges \
    --table=interaction_records --table=patient_records --table=audit_events \
    "postgres://owner:***@host:5432/bodhyanga" > bodhyanga-$(date +%F).dump
```

For point-in-time recovery, configure Postgres WAL archiving — none of the
application-layer code assumes single-tenant snapshots.

## Hard-delete (GDPR Article 17)

`Store.DeletePatient(ctx, tenantID, patientID)` issues a scoped DELETE on
`patient_records` only. The corresponding rows in `audit_events` are
**deliberately not** cascaded: the audit log keeps the pseudonymous
`patient_id` so the compliance trail remains intact (HIPAA §164.530(j)
requires retention; GDPR Article 17(3)(b) carves out legal-obligation
retention).

If the operator's threat model requires audit purge too:

```sql
-- run as the OWNER role, not bodhyanga_app (which lacks DELETE on audit_events)
SET app.tenant_id = 'tenant-uuid';
DELETE FROM audit_events WHERE patient_id = 'pt-uuid';
```

## What's intentionally not in this PR

- **Encryption at rest** — `payload_ct` is BYTEA but currently holds
  plaintext JSON. The pgcrypto/KMS PR is the next change and needs no
  schema migration.
- **Per-tenant key partitioning** — for very large rosters, declarative
  partitioning by `tenant_id` would let RLS-enabled scans hit a smaller
  page set. Out of scope until benchmarks justify it.
- **Soft-delete / tombstones** — only hard-delete is exposed. A retention
  policy with `valid_until` columns is straightforward to layer on later.
- **Audit purge endpoint** — operator-only SQL until a vetted runbook
  exists.
