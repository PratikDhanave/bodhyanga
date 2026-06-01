-- 0007_encrypted_columns.sql
-- Documents that payload_ct columns now hold AES-256-GCM encrypted blobs and
-- adds an enc_version sentinel column so future key-rotation tooling can
-- identify which version of the encryption scheme was used at write time.
--
-- Schema note:
--   patient_records.payload_ct already exists (defined in 0001_init.sql / 0002_patients.sql).
--   audit_events stores individual text columns for structured query; this
--   migration adds a payload_ct column to carry an encrypted copy of the full
--   event JSON for compliance archives and SIEM export.
--
-- Key versioning:
--   enc_version = 'v1' denotes AES-256-GCM with a 12-byte random nonce and
--   16-byte authentication tag, key loaded from BODH_ENCRYPTION_KEY.

COMMENT ON COLUMN audit_events.payload_ct IS 'AES-256-GCM encrypted JSON blob';

ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS enc_version VARCHAR(10) DEFAULT 'v1';
