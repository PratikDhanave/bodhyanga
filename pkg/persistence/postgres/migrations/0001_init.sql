-- 0001_init.sql
-- Initial schema for Bodh's Postgres backend.
--
-- Three tables are required by the contracts implemented in
-- pkg/persistence/postgres:
--   * interaction_records   -- eval.Store
--   * patient_records       -- medical.PatientRegistry
--   * audit_events          -- audit.Recorder (append-only)
--
-- PHI handling (HIPAA §164.312):
--   Columns that will hold PHI at-rest are typed as bytea and named with the
--   _ct (ciphertext) suffix where the application is expected to encrypt the
--   value before insertion. Today the application writes plaintext-bytes
--   wrappers; a follow-up PR will wire pgcrypto / envelope encryption (KMS
--   data key per row) without a schema migration.
--
-- Opaque IDs:
--   patient_id and case_id are operator-issued strings (UUID v4 is the
--   recommended generator). They are NEVER MRNs. The schema enforces only that
--   they are non-empty for PHI-bearing rows; format is up to the caller.
--
-- Tenancy:
--   Every PHI-bearing row carries tenant_id. RLS in 0002_rls.sql is the
--   enforcement boundary; the unique constraints below are defence in depth
--   so a missing SET app.tenant_id can never cross-tenant alias an ID.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ----------------------------------------------------------------------------
-- interaction_records  --  eval.InteractionRecord
-- ----------------------------------------------------------------------------
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

-- (tenant_id, created_at) is the workhorse index — every list query is
-- filtered by tenant (via RLS) and ordered chronologically.
CREATE INDEX IF NOT EXISTS interaction_records_tenant_created_idx
    ON interaction_records (tenant_id, created_at DESC);

-- ----------------------------------------------------------------------------
-- patient_records  --  medical.PatientRecord
-- ----------------------------------------------------------------------------
--
-- patient_id is an opaque routing token (UUID or operator-issued). The full
-- demographic + vitals payload travels in payload_ct as bytea so the
-- pgcrypto/KMS PR can swap to ciphertext with zero schema churn. The
-- demo/file path writes the JSON bytes verbatim into payload_ct.
--
-- Columns surfaced outside payload_ct are limited to those used for routing
-- and roster-level rules: indexable fields that are not, on their own,
-- identifying. Conditions are stored as TEXT[] for the FilterByCondition
-- query path; codes are ICD/SNOMED, not PHI in isolation.
CREATE TABLE IF NOT EXISTS patient_records (
    tenant_id       TEXT        NOT NULL,
    patient_id      TEXT        NOT NULL CHECK (patient_id <> ''),
    conditions      TEXT[]      NOT NULL DEFAULT '{}',
    rpm_enrolled    BOOLEAN     NOT NULL DEFAULT FALSE,
    discharged_at   TIMESTAMPTZ,
    payload_ct      BYTEA       NOT NULL,                   -- PHI: encrypt in future PR
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, patient_id)
);

CREATE INDEX IF NOT EXISTS patient_records_tenant_created_idx
    ON patient_records (tenant_id, created_at DESC);
-- GIN on conditions only; RLS already filters by tenant_id so a per-tenant
-- GIN would need the btree_gin extension and buys little. Future PR can
-- partition by tenant for very large rosters.
CREATE INDEX IF NOT EXISTS patient_records_conditions_idx
    ON patient_records USING GIN (conditions);

-- ----------------------------------------------------------------------------
-- audit_events  --  audit.Event (append-only, HIPAA §164.312(b))
-- ----------------------------------------------------------------------------
--
-- This table is the immutable audit trail. The application role is GRANTed
-- INSERT and SELECT only — never UPDATE or DELETE. Operators retain those
-- privileges out-of-band for retention pruning; a WORM sink in front of the
-- DB is recommended for FDA/21 CFR Part 11 deployments.
CREATE TABLE IF NOT EXISTS audit_events (
    tenant_id    TEXT        NOT NULL,
    id           TEXT        NOT NULL,
    at           TIMESTAMPTZ NOT NULL,
    kind         TEXT        NOT NULL,
    agent_id     TEXT,
    message_id   TEXT,
    from_id      TEXT,
    to_id        TEXT,
    type         TEXT,
    patient_id   TEXT,
    case_id      TEXT,
    trace_id     TEXT,
    decision     TEXT,
    reason       TEXT,
    reviewer_id  TEXT,
    error_text   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS audit_events_tenant_at_idx
    ON audit_events (tenant_id, at DESC);
CREATE INDEX IF NOT EXISTS audit_events_tenant_kind_idx
    ON audit_events (tenant_id, kind);

-- record the migration
INSERT INTO schema_migrations (version) VALUES (1) ON CONFLICT DO NOTHING;
