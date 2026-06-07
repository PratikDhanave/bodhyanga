# HIPAA Technical Safeguards Control Mapping

This document maps Bodh's security controls to the HIPAA Technical Safeguards
defined in 45 CFR SS 164.312. Each row identifies the regulatory requirement, the
specific HIPAA section, and the Bodh implementation that satisfies it.

---

## SS 164.312(a)(1) -- Access Control

**Requirement:** Implement technical policies and procedures for electronic
information systems that maintain ePHI to allow access only to those persons or
software programs that have been granted access rights.

### Implementation

Bodh enforces access control through `pkg/auth`, which provides two mechanisms:

**Authentication (JWT RS256):**
The `Authenticator` in `pkg/auth/auth.go` parses Bearer tokens, resolves the
signing key by the JWT header's `kid` field via a `KeyLookup` interface, and
validates the token through two independent paths:

1. An OPA Rego policy (`pkg/auth/rego/authentication.rego`) that verifies the
   signature and issuer claim against the embedded policy.
2. The `golang-jwt/jwt/v5` library's own signature and expiration verification
   as a belt-and-braces check.

Claims carry `TenantID` and `Roles` fields. The signing method is pinned to
RS256; the `alg=none` attack class is closed at the parser layer.

**Authorization (OPA Rego):**
The `Evaluate` method runs named Rego rules (e.g., `data.bodh.explain.allow`)
against structured input containing the caller's roles, tenant, HTTP method,
and path. Role-based access control is enforced at the HTTP middleware layer
in `pkg/mid`. The Rego policies are embedded in the binary, not loaded from
an external file, so they cannot be tampered with at runtime.

**Tenant isolation (PostgreSQL RLS):**
The migration `db/migrations/0002_rls.sql` enables Row-Level Security on
clinical data tables. Every query is scoped by the `tenant_id` set in the
session context. A query from tenant A cannot see rows belonging to tenant B,
even if the application layer has a bug.

| Sub-requirement | Section | Bodh implementation |
|---|---|---|
| Unique user identification | SS 164.312(a)(2)(i) | JWT `sub` claim uniquely identifies users; `Claims.TenantID` scopes to organization |
| Emergency access procedure | SS 164.312(a)(2)(ii) | Kill switch in `cmd/care/kill_switch.go` allows operators to halt all traffic or quarantine specific agents via `POST /admin/kill-switch` without restart |
| Automatic logoff | SS 164.312(a)(2)(iii) | JWT expiration (`exp` claim) with configurable TTL; expired tokens are rejected by both the Rego policy and the jwt/v5 parser |
| Encryption and decryption | SS 164.312(a)(2)(iv) | AES-256-GCM authenticated encryption in `pkg/persistence/crypto.go` for PHI columns; key loaded from `BODH_ENCRYPTION_KEY` env var (base64-encoded 32-byte master key) |

---

## SS 164.312(b) -- Audit Controls

**Requirement:** Implement hardware, software, and/or procedural mechanisms that
record and examine activity in information systems that contain or use ePHI.

### Implementation

Bodh's audit subsystem in `pkg/audit` provides three recorder implementations
with increasing durability:

**InMemory recorder:** Thread-safe slice-backed recorder for tests and demos.
Not suitable for production (restarts lose the log).

**FileStore recorder:** Appends one JSON line per event to a file opened
`O_APPEND|O_CREATE` with mode `0o600`. Audit events are serialized under a
mutex so concurrent goroutines never interleave line boundaries.

**ImmutableRecorder:** Production chain-hash sink (see ADR 0002). Each
`ChainedEvent` carries `SHA-256(prev_hash + "\n" + json(Event))`, creating a
Merkle chain that makes retroactive modification of any historical entry
detectable via `VerifyChain`. Supports optional S3 export to a WORM sink for
6-year retention.

**What is audited:**

| Event Kind | Trigger | Contains |
|---|---|---|
| `KindMessage` | Every agent invocation, before `HandleMessage` runs | Agent ID, message routing (From/To/Type), correlation keys (case_id, tenant_id, trace_id, patient_id) |
| `KindPolicyDecision` | Every governance deny; allows on clinical message types only | Policy verdict (allow/deny), reason, routing metadata |
| `KindReviewDecision` | Every HITL reviewer action (approve/reject/modify) | Reviewer ID, decision, reason, patient_id, case_id |
| `KindAgentError` | Agent `HandleMessage` returns a non-nil error | Agent ID, error text (never msg.Content), routing metadata |

**What is never audited (PHI exclusion):** Message content (clinical narrative,
test results, diagnosis text, medication names, patient name, DOB, MRN, address,
phone, email). The `Event` struct deliberately excludes `msg.Content`. The
`pickStrings` helper copies only four whitelisted metadata keys.

**Fail-open semantics:** Audit write failures are logged via `env.Logf` and
never block clinical message processing. Blocking clinical flow because the
audit sink is unavailable is the worst outcome.

**Export for compliance officers:** The `/audit` HTTP endpoint returns the full
event list as JSON. `ImmutableRecorder.ExportToS3` uploads event ranges as
gzipped JSONL to S3-compatible storage.

---

## SS 164.312(c)(1) -- Integrity Controls

**Requirement:** Implement policies and procedures to protect ePHI from improper
alteration or destruction.

### Implementation

**Audit log integrity:** The Merkle chain hash in `ImmutableRecorder` (see
SS 164.312(b) above) detects any modification to historical audit entries.
`VerifyChain` reads the file end-to-end, recomputes every hash, and reports the
sequence number of the first broken link. The comparison uses `hmac.Equal` for
constant-time comparison to prevent timing-based attacks.

**Data-at-rest integrity:** AES-256-GCM in `pkg/persistence/crypto.go` provides
authenticated encryption. GCM's authentication tag (16 bytes appended to the
ciphertext) detects any tampering with encrypted PHI columns. A modified
ciphertext fails the `gcm.Open` call with an authentication error.

**Database-level integrity:** PostgreSQL's WAL (Write-Ahead Log) and MVCC
provide crash-recovery guarantees. The `db/migrations/0007_encrypted_columns.sql`
migration adds encrypted columns with NOT NULL constraints to prevent accidental
data loss.

---

## SS 164.312(d) -- Person or Entity Authentication

**Requirement:** Implement procedures to verify that a person or entity seeking
access to ePHI is the one claimed.

### Implementation

**JWT-based authentication:** The `Authenticator` in `pkg/auth/auth.go` verifies
the Bearer token's RS256 signature against the public key for the token's `kid`.
The signing method is pinned -- tokens with other algorithms (including `none`)
are rejected at the parser layer.

**Key management:** The `KeyStore` in `pkg/auth/keystore.go` loads RSA 2048+ PEM
keys from the filesystem (one `.pem` file per `kid`) or from an in-memory JSON
document. Bodh does not manage user credentials; token issuance is the operator's
responsibility. The development path uses `cmd/bodhadmin` to mint tokens signed
by a generated key pair.

**HITL reviewer identity:** Every HITL review decision in `pkg/hitl` records the
`ReviewerID` in the audit log. This is the regulatory anchor for 21 USC
SS 360j(o)(1) carve-out criterion 4 -- proof that a licensed clinician was the
individual who approved the recommendation.

---

## SS 164.312(e)(1) -- Transmission Security

**Requirement:** Implement technical security measures to guard against
unauthorized access to ePHI that is being transmitted over an electronic
communications network.

### Implementation

**TLS termination:** Bodh's HTTP server in `cmd/care` sets the
`Strict-Transport-Security` header with a 2-year max-age and preload directive
on every response:

```
Strict-Transport-Security: max-age=63072000; includeSubDomains; preload
```

TLS termination is handled at the ingress layer (nginx, Envoy, cloud load
balancer). The application server itself can run on HTTP behind the TLS
terminator, which is the standard deployment pattern documented in
`docs/deployment.md`.

**CORS restrictions:** The `pkg/web` framework's CORS handler only echoes
`Access-Control-Allow-Origin` when the request's `Origin` appears in the
explicit allow-list. It never sends a blanket `*` origin. Preflight responses
are cached for 24 hours (`Access-Control-Max-Age: 86400`).

**S3 upload encryption:** Audit log exports to S3 use HTTPS by default (the
S3 endpoint is `https://s3.{region}.amazonaws.com`). AWS SigV4 authentication
is computed per-request using `BODH_S3_ACCESS_KEY` and `BODH_S3_SECRET_KEY`.

---

## PHI De-identification -- HIPAA Safe Harbor Method

**Requirement:** 45 CFR SS 164.514(b)(2) lists 18 categories of identifiers that
must be removed or generalized for de-identification under the Safe Harbor
method.

### Implementation

**Layer 1 -- Telemetry hashing:** Case identifiers in OpenTelemetry spans are
represented as `sha256(caseID)[:8]` (first 8 hex characters). Raw MRNs never
appear in collectors or trace backends.

**Layer 2 -- Regex redaction:** The `Redactor` in `pkg/phi/redact.go` applies
compiled regular expressions to every log line via the `LoggerWrapper`. The
default pattern set covers SSNs, MRNs (with the `MRN` prefix), US phone
numbers, email addresses, ISO 8601 dates, and US-format dates. Site-specific
patterns are added via `Redactor.Add()`.

**Layer 3 -- Structural exclusion:** The audit `Event` struct excludes
`msg.Content` entirely. The `pickStrings` helper copies only four whitelisted
correlation keys from message metadata. The `patient_id` field is an opaque
Bodh-internal token, never the MRN.

**Layer 4 -- Encryption at rest:** PHI columns in PostgreSQL are encrypted with
AES-256-GCM via `pkg/persistence/crypto.go`. The master key is loaded from the
`BODH_ENCRYPTION_KEY` environment variable. A nil encryptor (no key set) is a
passthrough for local development only; production deployments require the key.

| Safe Harbor Identifier | Category | Bodh Coverage |
|---|---|---|
| Names | (a) | Structural exclusion (never in audit events or spans) |
| Geographic data | (b) | Structural exclusion |
| Dates (except year) | (c) | Regex redaction (`iso_date`, `us_date` patterns) |
| Phone numbers | (d) | Regex redaction (`phone` pattern) |
| Fax numbers | (d) | Not applicable (Bodh does not process fax) |
| Email addresses | (f) | Regex redaction (`email` pattern) |
| SSNs | (g) | Regex redaction (`ssn` pattern) |
| MRNs | (h) | Regex redaction (`mrn` pattern); hash in spans |
| Health plan beneficiary numbers | (i) | Structural exclusion |
| Account numbers | (j) | Structural exclusion |
| Certificate/license numbers | (k) | Structural exclusion |
| Vehicle identifiers | (l) | Not applicable |
| Device identifiers | (m) | Structural exclusion |
| Web URLs | (n) | Structural exclusion |
| IP addresses | (o) | Not logged by application (infrastructure concern) |
| Biometric identifiers | (p) | Not applicable |
| Full face photographs | (q) | Not applicable |
| Any other unique identifying number | (r) | Site-extensible via `Redactor.Add()` |

---

## Summary Matrix

| HIPAA Requirement | Section | Bodh Component | Implementation |
|---|---|---|---|
| Access Control | SS 164.312(a)(1) | `pkg/auth` | OPA Rego policies + JWT RS256 authentication |
| Unique User Identification | SS 164.312(a)(2)(i) | `pkg/auth` | JWT `sub` + `tenant_id` claims |
| Emergency Access | SS 164.312(a)(2)(ii) | `cmd/care/kill_switch.go` | HTTP API to halt/quarantine agents without restart |
| Automatic Logoff | SS 164.312(a)(2)(iii) | `pkg/auth` | JWT `exp` claim with configurable TTL |
| Encryption/Decryption | SS 164.312(a)(2)(iv) | `pkg/persistence/crypto.go` | AES-256-GCM with envelope encryption model |
| Audit Controls | SS 164.312(b) | `pkg/audit` | Append-only chain-hash log with S3 WORM export |
| Integrity Controls | SS 164.312(c)(1) | `pkg/audit/immutable.go` | Merkle chain detects tampering; GCM auth tag protects PHI columns |
| Person/Entity Auth | SS 164.312(d) | `pkg/auth` | JWT RS256 + OPA Rego verification; HITL reviewer ID in audit |
| Transmission Security | SS 164.312(e)(1) | `pkg/web`, ingress | HSTS header, TLS at ingress, CORS allow-list |
| PHI De-identification | SS 164.514(b)(2) | `pkg/phi` | SHA-256 hash in spans, regex redaction in logs, structural exclusion in audit |
| Audit Log Export | SS 164.312(b) | `pkg/audit/immutable.go` | Gzipped JSONL export to S3-compatible WORM storage |
| Row-Level Security | SS 164.312(a)(1) | `db/migrations/0002_rls.sql` | PostgreSQL RLS scoped by tenant_id |
