# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| main    | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability in Bodh, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, email **i.pratikdhanave@gmail.com** with:

1. A description of the vulnerability
2. Steps to reproduce
3. Potential impact assessment
4. Suggested fix (if any)

You should receive a response within 48 hours. We will work with you to understand the issue and coordinate disclosure.

## Security Considerations

Bodh is a **research and engineering reference** for medical AI systems. It is **not approved for clinical use** and must not be used to make decisions affecting real patients.

That said, the codebase implements production-grade security patterns as a reference:

### PHI Protection

- **`pkg/phi`** — Safe Harbor de-identification (SSN, MRN, phone, email, date patterns)
- **`pkg/audit`** — Append-only audit log; `Event.Reason` is always PHI-free
- **OTel spans** — `bodh.case_id` is always `sha256(caseID)[:8]` hex, never a raw MRN
- **`pkg/persistence/crypto`** — AES-256-GCM column-level encryption for PHI at rest
- **Immutable audit** — Merkle chain-hash for tamper-evident audit trail

### Authentication & Authorization

- **`pkg/auth`** — JWT (HS256) credential store + SMART on FHIR (RS256) validator
- **`pkg/governance`** — Policy-based access control (OPA Rego, SMART scopes, Part 2 consent, break-glass)
- **Row-level security** — PostgreSQL RLS with `SET LOCAL app.tenant_id` per query
- **Demo credentials** — `cmd/care` seeds the illustrative login
  `clinician@hospital.org` / `clinic123` **only in this research build**. It logs
  a startup security warning and honours **`BODH_DEMO_USERS=0`** to seed nothing;
  any non-demo deployment must set it and wire its own credential store.

### Network Security

- **Helm chart** — NetworkPolicy (deny-all + allowlist), distroless image, no shell
- **Build tags** — `otel`, `kronk`, `nats` isolate heavyweight dependencies

### What We Do NOT Protect Against

- Physical access to the server
- Compromised cloud provider credentials
- Social engineering of clinicians with valid credentials
- Malicious agents registered by an admin (governance gates messages, not agent registration)

## Dependency Security

We pin all direct dependencies in `go.mod` and run `govulncheck` in CI (`make ci`).

To check for known vulnerabilities locally:

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

## API Key Handling

- API keys go in `.env` only (gitignored) — never in tracked files
- `.env.example` contains blank placeholders
- `BODH_ENCRYPTION_KEY` (base64, 32 bytes) encrypts PHI columns
- `BODH_JWT_SECRET` signs JWT tokens — change from default in production
