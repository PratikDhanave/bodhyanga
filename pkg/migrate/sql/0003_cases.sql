-- 0003_cases.sql
-- Case state + interaction record tables.
--
-- case_state holds the live diagnostic-loop state for a case so a
-- restart of cmd/care can pick up an in-flight case from the last
-- audited transition. The payload_ct column is the JSON-encoded
-- pkg/medical.CaseState (PHI), pending a pgcrypto/KMS wrap; the
-- routing columns above the payload are correlation handles, not
-- PHI on their own.
--
-- interaction_records is the eval.InteractionRecord sink — one row
-- per completed case, with metrics + metadata as JSONB so the SD-Bench
-- runner can query without round-tripping through the application.
-- This table also exists in the legacy
-- pkg/persistence/postgres/migrations/0001_init.sql; CREATE TABLE IF
-- NOT EXISTS lets the new runner coexist with a DB already migrated
-- by the old one without raising "relation already exists".

CREATE TABLE IF NOT EXISTS case_state (
    tenant_id      TEXT        NOT NULL,
    case_id        TEXT        NOT NULL CHECK (case_id <> ''),
    patient_id     TEXT        NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'open',
    payload_ct     BYTEA       NOT NULL,
    last_step      INTEGER     NOT NULL DEFAULT 0,
    cost_cents     INTEGER     NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at      TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, case_id)
);

CREATE INDEX IF NOT EXISTS case_state_tenant_status_idx
    ON case_state (tenant_id, status);
CREATE INDEX IF NOT EXISTS case_state_tenant_updated_idx
    ON case_state (tenant_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS interaction_records (
    tenant_id   TEXT        NOT NULL,
    id          TEXT        NOT NULL,
    scenario    TEXT        NOT NULL DEFAULT '',
    success     BOOLEAN     NOT NULL DEFAULT FALSE,
    metrics     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    metadata    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    started_at  TIMESTAMPTZ NOT NULL,
    ended_at    TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS interaction_records_tenant_created_idx
    ON interaction_records (tenant_id, created_at DESC);
