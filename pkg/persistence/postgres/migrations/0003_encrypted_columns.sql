-- 0003_encrypted_columns.sql
-- Documents that payload_ct columns now hold AES-256-GCM encrypted blobs and
-- adds an enc_version sentinel column so future key-rotation tooling can
-- identify which version of the encryption scheme was used at write time.
--
-- Schema note:
--   patient_records.payload_ct already exists (defined in 0001_init.sql).
--   audit_events stores individual text columns for structured query; this
--   migration adds a payload_ct column to carry an encrypted copy of the full
--   event JSON for compliance archives and SIEM export. Both columns may exist
--   simultaneously — the application writes the structured columns AND the
--   encrypted blob. Future reporting agents read payload_ct exclusively.
--
-- Key versioning:
--   enc_version = 'v1' denotes AES-256-GCM with a 12-byte random nonce and
--   16-byte authentication tag, key loaded from BODHYANGA_ENCRYPTION_KEY.
--   When a key is rotated, enc_version is bumped to 'v2', etc.
--
-- No data migration is required: rows written before this migration have
-- NULL payload_ct / enc_version, which the application treats as plaintext
-- passthrough (same semantics as dev mode with no encryption key set).

-- Annotate the existing patient payload column.
COMMENT ON COLUMN patient_records.payload_ct IS 'AES-256-GCM encrypted JSON blob (nonce||ciphertext||tag). NULL implies plaintext dev mode.';

-- Add enc_version to patient_records.
ALTER TABLE patient_records
    ADD COLUMN IF NOT EXISTS enc_version VARCHAR(10) DEFAULT 'v1';

-- Add encrypted payload column to audit_events for SIEM / compliance archive.
ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS payload_ct   BYTEA,
    ADD COLUMN IF NOT EXISTS enc_version  VARCHAR(10) DEFAULT 'v1';

COMMENT ON COLUMN audit_events.payload_ct IS 'AES-256-GCM encrypted JSON blob of the full audit.Event. Structured columns remain for SQL querying.';

-- record the migration
INSERT INTO schema_migrations (version) VALUES (3) ON CONFLICT DO NOTHING;
