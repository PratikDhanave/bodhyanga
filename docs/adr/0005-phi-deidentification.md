# ADR 0005: PHI De-identification via SHA-256 Truncated Hashes

**Status:** Accepted

**Date:** 2024-12-25

**Deciders:** Pratik Dhanave

---

## Context

HIPAA's minimum-necessary principle (45 CFR SS 164.502(b)) requires that
covered entities limit the use and disclosure of PHI to the minimum necessary
to accomplish the intended purpose. In Bodh's architecture, three outbound
channels can potentially carry PHI:

1. **Telemetry spans** (OpenTelemetry traces exported to Laminar, Jaeger, or
   any OTLP collector).
2. **Application logs** (stdout/stderr, forwarded via fluentbit/vector to a
   log aggregator).
3. **Audit events** (the chain-hash JSONL file and its S3 export).

These channels serve operational and compliance purposes. The people who query
them (SREs, compliance officers, QA engineers) do not need -- and in many
cases are not authorized to access -- raw Protected Health Information. Yet
they do need the ability to correlate a trace span to a specific case, patient
encounter, or audit event.

The challenge: how to make spans and logs traceable (joinable to the canonical
record in the regulated persistence layer) without making them a secondary
copy of PHI.

HIPAA's Safe Harbor method (45 CFR SS 164.514(b)(2)) enumerates 18 categories
of identifiers that must be removed for de-identification. The most relevant
to Bodh's telemetry are:

- Medical Record Numbers (MRNs)
- Dates more specific than year
- Social Security Numbers
- Telephone numbers
- Email addresses
- Any other unique identifying number, characteristic, or code

## Decision

Apply a two-layer de-identification strategy:

### Layer 1: Hash-based correlation tokens in telemetry

In all OpenTelemetry spans and structured logs, case identifiers are
represented as `sha256(caseID)[:8]` -- the first 8 hex characters (32 bits) of
the SHA-256 hash of the raw case ID. This is stored in the `bodh.case_id`
span attribute.

Properties of this construction:

- **Not reversible.** SHA-256 is a one-way function. Given the 8-character
  hex prefix, an attacker cannot recover the original case ID without
  brute-forcing the hash space. For typical case ID formats (UUID, numeric
  MRN), the search space is large enough that the truncated hash is not
  practically invertible.

- **Joinable.** An investigator who has authorized access to the persistence
  layer can compute `sha256(knownCaseID)[:8]` and join it to the trace spans.
  The join goes from "I know the case ID" to "I can find the spans," not the
  reverse.

- **Collision-tolerant for tracing.** 32 bits of hash gives a collision
  probability of ~0.5 at 65,536 cases (birthday bound). This is acceptable
  for tracing -- a collision means two cases share a span tag, not that PHI
  is leaked. For forensic investigations that require uniqueness, the full
  audit log (which carries the opaque but unique CaseID) is the source of
  truth.

- **Raw MRNs never appear in collectors or logs.** This is a hard rule
  enforced at two layers: the span-creation code in `pkg/observability` and
  the `phi.LoggerWrapper` in the log pipeline.

### Layer 2: Regex-based log redaction (`pkg/phi`)

The `Redactor` in `pkg/phi/redact.go` applies a set of compiled regular
expressions to every log line before it reaches the log sink. The default
pattern set covers the most common HIPAA Safe Harbor identifiers:

| Pattern | Matches | Replacement |
|---|---|---|
| `ssn` | `\b\d{3}-\d{2}-\d{4}\b` | `[REDACTED-SSN]` |
| `mrn` | `(?i)\bMRN[:\s]*[A-Z0-9-]{4,}\b` | `[REDACTED-MRN]` |
| `phone` | `\b\d{3}[-.\s]\d{3}[-.\s]\d{4}\b` | `[REDACTED-PHONE]` |
| `email` | `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b` | `[REDACTED-EMAIL]` |
| `iso_date` | `\b(19\|20)\d{2}-\d{2}-\d{2}\b` | `[REDACTED-DATE]` |
| `us_date` | `\b\d{1,2}/\d{1,2}/(19\|20)\d{2}\b` | `[REDACTED-DATE]` |

The `LoggerWrapper` type implements `observability.Logger` and wraps any
inner logger with per-write redaction. It is wired at the application boundary
in `cmd/care/main.go`:

```go
rawLogger := observability.NewStdLogger("bodh")
logger    := &phi.LoggerWrapper{Redactor: phi.NewRedactor(), Inner: rawLogger}
```

Site-specific patterns (e.g., custom MRN formats like `EPIC-12345678`) are
added via `Redactor.Add()`.

### Layer 3: Structural exclusion in audit events

The audit `Event` struct in `pkg/audit/recorder.go` deliberately excludes
`msg.Content` (clinical narrative, test results, diagnosis text, medication
names). The `FromMessage` helper copies only routing fields (ID, From, To,
Type) and four whitelisted correlation keys (patient_id, case_id, trace_id,
tenant_id) via the `pickStrings` function.

The `patient_id` in audit events is an opaque Bodh-internal token (e.g.,
`pt-001`), never the MRN. The MRN lives in the persistence layer's PHI
columns behind separate access control and AES-256-GCM encryption (see
`pkg/persistence/crypto.go`).

## Consequences

### Positive

- **HIPAA minimum-necessary standard is satisfied.** Telemetry channels carry
  only non-reversible correlation tokens. Investigators can join tokens to
  cases only if they have separate authorized access to the persistence layer.

- **Defence in depth.** Three independent layers (hash tokens, regex
  redaction, structural exclusion) mean that a single failure (e.g., a
  developer accidentally logging `msg.Content`) is caught by the next layer.
  The regex redactor catches the obvious patterns; the audit schema's
  deliberate field selection prevents structural leaks.

- **No special infrastructure required.** The hash truncation is a stdlib
  `crypto/sha256` call. The regex redactor is compiled once at startup. No
  external DLP service is required for the first line of defense.

- **Site-extensible.** Operators add organization-specific patterns (custom
  MRN shapes, payer claim IDs, internal ticket numbers) via `Redactor.Add()`
  without modifying the core package.

### Negative

- **Regex has false negatives.** The regex set catches structured identifiers
  (SSNs, phone numbers, dates) but cannot catch free-form patient names,
  addresses in narrative, or indirectly identifying date ranges. This is
  explicitly acknowledged in the package documentation. Production
  deployments should layer a real DLP service (Google Cloud DLP, AWS Macie,
  Microsoft Presidio) downstream of the log sink for contextual analysis.

- **Regex has false positives.** An order number matching the SSN shape
  (`123-45-6789`) gets redacted. A date in a version string (`2024-01-15`)
  gets redacted. The cost is a slightly less informative log line, which is
  acceptable because the alternative (missed PHI) is far worse.

- **Hash truncation loses uniqueness.** The 32-bit prefix has a non-trivial
  collision probability at scale. For tracing this is acceptable (collisions
  merge spans, not leak data). For forensic uniqueness, the full-length
  opaque CaseID in the audit log is the authoritative identifier.

### Neutral

- The `LoggerWrapper` redacts the format string itself (not just arguments)
  because developers sometimes write `Printf("treating MRN-12345 with...")`
  by mistake. This closes the format-string PHI leak vector at the cost of
  one extra `Redact` call per log line.

- The `LoggerWrapper` is fail-closed: a nil `Redactor` or nil `Inner` causes
  `Printf` to no-op. Logs go missing (loud operationally) rather than leaking
  PHI unredacted (silently catastrophic).

## References

- `pkg/phi/redact.go` -- Redactor, DefaultPatterns, LoggerWrapper
- `pkg/audit/recorder.go` -- Event schema, FromMessage, pickStrings whitelist
- `pkg/observability/otlp.go` -- sha256(caseID)[:8] span attribute
- `pkg/persistence/crypto.go` -- AES-256-GCM encryption for PHI columns
- 45 CFR SS 164.502(b) -- HIPAA minimum-necessary principle
- 45 CFR SS 164.514(b)(2) -- HIPAA Safe Harbor de-identification method
