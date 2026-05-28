-- 0002_patients.sql
-- patient_records: the population roster.
--
-- patient_id is an opaque routing token (UUID or operator-issued); it
-- is never the MRN. The full demographic + vitals payload travels in
-- payload_ct as bytea so a future pgcrypto/KMS PR can swap to
-- ciphertext with zero schema churn.
--
-- Columns surfaced outside payload_ct are limited to those used for
-- routing and roster-level rules: indexable fields that are not, on
-- their own, identifying. Conditions are stored as TEXT[] for the
-- FilterByCondition query path; codes are ICD/SNOMED, not PHI in
-- isolation.
--
-- RLS policies for this table are applied in 0005_rls.sql. This
-- migration only creates the table + indexes so the tables and the
-- policies can be reviewed independently.

CREATE TABLE IF NOT EXISTS patient_records (
    tenant_id       TEXT        NOT NULL,
    patient_id      TEXT        NOT NULL CHECK (patient_id <> ''),
    conditions      TEXT[]      NOT NULL DEFAULT '{}',
    rpm_enrolled    BOOLEAN     NOT NULL DEFAULT FALSE,
    discharged_at   TIMESTAMPTZ,
    payload_ct      BYTEA       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, patient_id)
);

CREATE INDEX IF NOT EXISTS patient_records_tenant_created_idx
    ON patient_records (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS patient_records_conditions_idx
    ON patient_records USING GIN (conditions);
