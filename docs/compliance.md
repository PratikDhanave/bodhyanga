# Bodhyanga — Security & HIPAA compliance

This document is the deep reference for how Bodhyanga approaches PHI handling, security architecture, HIPAA, HITECH, 42 CFR Part 2, GDPR, audit frameworks, de-identification, and the governance policies that live in the codebase. The [main README](../README.md) keeps a short summary and links here.

> **Note:** Bodhyanga is a research demo. The text below describes the architectural target needed for a production HIPAA-aligned deployment. The current codebase does **not** ship with a BAA, encryption-at-rest, identity provider, or audit pipeline.

---

## PHI handling principles

Bodhyanga follows these principles for **Protected Health Information (PHI)**:

1. **Minimum necessary** — agents see only the fields required for their task. Don't pass the full clinical narrative when only the chief complaint is needed.
2. **Opaque identifiers** — `case_id` and `patient_id` are routing tokens, not MRNs. Real MRNs / SSNs / DOBs never appear in logs or message Metadata in production.
3. **Fail closed** — governance denies messages missing correlation IDs; a denied message never reaches an agent.
4. **Auditable everything** — every interaction is a `Message` with a UUID. Every agent emits structured logs. Every governance denial logs its `Reason`.
5. **No PHI in third-party telemetry** — if you wire OpenTelemetry, redact `Content` and `Metadata` before export.
6. **De-identify for evaluation** — `gold_diagnosis` and benchmark records use synthetic or de-identified data; never production PHI in CI.
7. **Encrypted at rest and in transit** — production deployments must encrypt the bus (TLS), the persistence layer (KMS-backed AES-256-GCM), and all backups.
8. **Right to delete** — Bodhyanga's persistence layer must support point-deletion of a patient's records (GDPR Article 17 / state laws).

## Security architecture

Production target architecture (not yet built into the codebase):

```
                ┌──────────────────────────────────────────────┐
                │   Identity Provider (Keycloak / Azure AD /    │
                │   Okta / SMART on FHIR auth server)           │
                └─────────────┬────────────────────────────────┘
                              │ OIDC / OAuth 2.0
┌─────────────────────────────▼──────────────────────────────────┐
│                       API Gateway / Ingress                    │
│   - Terminates TLS 1.3 (cert from cert-manager / KMS)          │
│   - Validates JWT (RS384 / EdDSA), introspects opaque tokens   │
│   - Enforces rate limits, WAF rules                            │
│   - Strips identifying headers from upstream logs              │
└─────────────────────────────┬──────────────────────────────────┘
                              │ mTLS
┌─────────────────────────────▼──────────────────────────────────┐
│                      Bodhyanga API (cmd/care)                       │
│   - Scope check (SMART scopes → governance policies)            │
│   - Patient context binding (sub, fhirUser, encounter)         │
│   - Emits audit event for every request                        │
└─────────────────────────────┬──────────────────────────────────┘
                              │ in-process (today) / mTLS (split)
┌─────────────────────────────▼──────────────────────────────────┐
│                     Orchestrator + Bus                         │
│   - All messages tagged with trace_id, tenant_id               │
│   - Bus encrypts payload (for Kafka/NATS production)            │
└─────────────────────────────┬──────────────────────────────────┘
                              │
┌─────────────────────────────▼──────────────────────────────────┐
│                         Persistence                            │
│   - PostgreSQL with column-level encryption for PHI fields      │
│   - Backups encrypted (KMS), retention policy                   │
│   - Row-level security per tenant                              │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────▼──────────────────────────────────┐
│   Observability sink (PHI-redacted): Loki / Tempo / Prometheus │
│   Audit sink (immutable, WORM): S3 Object Lock / Azure Blob immutability │
└─────────────────────────────────────────────────────────────────┘
```

### Authentication

| Mechanism | When |
|-----------|------|
| **OAuth 2.0 + OIDC (SMART on FHIR)** | EHR-launched apps, clinician identity |
| **OAuth 2.0 backend services** (`client_credentials` with `private_key_jwt`) | Server-to-server: panel batch jobs, ADT ingest from HIE |
| **mTLS** | Service-to-service inside the cluster |
| **JWT** | Stateless authn at the API gateway (RS384, EdDSA — avoid HS256 with shared secrets) |
| **API keys** | Limited; for non-PHI metadata only |

**Never use:**
- HTTP basic auth without TLS pinning
- Long-lived bearer tokens
- API keys checked into source
- Username/password in logs or query strings

### Authorization

- **Coarse-grained (route)** — gateway checks scope (`patient/*.read`, `user/CarePlan.write`).
- **Fine-grained (resource)** — Bodhyanga governance policies check `tenant_id`, `patient_id`, and `case_id`. Extend `pkg/governance` with `RequireScopePolicy{Scope: "user/CarePlan.write"}`.
- **Patient-context binding** — a SMART-launched session is locked to one `patient_id`; cross-patient reads are denied.

### Cryptography

| Concern | Target |
|---------|--------|
| TLS | 1.3 only; HSTS; OCSP stapling; modern ciphers |
| Data at rest | AES-256-GCM with KMS-managed keys; rotate yearly minimum |
| Field-level | PHI columns (MRN, DOB, narrative) encrypted with envelope keys |
| Hashing | SHA-256 / SHA-3 for non-secret; Argon2id for passwords; HMAC-SHA-256 for MAC |
| Signing | EdDSA (Ed25519) or RSA-PSS; never MD5 / SHA-1 / unpadded RSA |
| Random | `crypto/rand` only |

### Key management

- Use **AWS KMS / Azure Key Vault / GCP KMS / HashiCorp Vault** — never raw keys in env vars.
- Customer-managed keys (CMK) for BAA-eligible AWS/Azure/GCP services.
- Key rotation policy documented; emergency rotation drill once a year.

### Secrets

- Never in env vars in plaintext for prod (use sealed-secrets, ESO, or sidecar injection).
- Pre-commit hook for credential scanning (gitleaks / trufflehog).
- Short-lived secrets via Vault / IAM Roles for Service Accounts.

### Network segmentation

- Separate subnets for ingress / app / data tiers.
- Default-deny network policies (Kubernetes NetworkPolicy or VPC SG).
- Egress filter — only known FHIR endpoints, the e-prescribing gateway, the notification provider, etc.

### Vulnerability management

- `go vet ./...`, `staticcheck`, `gosec` in CI.
- `govulncheck` against the module graph.
- Dependabot / Renovate weekly.
- Container scans (Trivy / Snyk).
- Annual third-party penetration test before any production launch.

---

## HIPAA Privacy Rule

**45 CFR Part 160 and 164 Subparts A and E.**

The Privacy Rule governs **uses and disclosures of PHI** by covered entities (providers, plans, clearinghouses) and their **business associates** (vendors processing PHI on their behalf). If Bodhyanga is deployed by a covered entity, **the operator is a business associate** and must sign a Business Associate Agreement (BAA).

### Required practices in a production Bodhyanga deployment

| Requirement | How Bodhyanga maps |
|-------------|---------------|
| **Minimum Necessary Standard** | `RequireCaseIDPolicy` ensures correlation tokens only; planned `RedactNarrativePolicy` for logs |
| **Use & Disclosure** | `pkg/governance` denies messages outside allowed flows; audit captures every cross-boundary disclosure |
| **Patient Right to Access** (45 CFR 164.524) | `GET /patients/{id}` (planned) returns the patient's record bundle |
| **Right to Amendment** (164.526) | Persistence layer must support corrections with audit trail |
| **Right to Accounting of Disclosures** (164.528) | Immutable audit log |
| **Right to Restrict** (164.522) | Per-patient consent flags on `PatientRecord` |
| **Notice of Privacy Practices** (164.520) | Operator obligation; Bodhyanga ships a template |
| **Marketing & Sale** (164.508) | Engagement notifier must respect opt-out and never sell PHI |

### Business Associate Agreement (BAA)

A BAA is required between the covered entity and Bodhyanga's operator. The BAA must include the elements in 45 CFR 164.504(e):

- Permitted uses/disclosures
- Safeguards (refer to Security Rule controls)
- Subcontractor flow-through (e.g., Twilio for SMS must also have a BAA)
- Reporting of unauthorized use/disclosure (incl. breaches)
- Return / destruction of PHI on termination
- Compliance with Privacy Rule when carrying out covered entity obligations

**Common BAA-eligible subcontractors** when running Bodhyanga on cloud infrastructure:

| Provider | Notes |
|----------|-------|
| AWS (most services) | BAA available; HIPAA-eligible service list applies |
| Azure | BAA available; HIPAA/HITRUST Blueprint |
| Google Cloud | BAA available; check service list (e.g., not all AI Platform features) |
| Twilio / SendGrid | Sign their BAA before sending PHI in SMS/email |
| Datadog / New Relic | Sign their BAA before piping logs / traces |
| OpenAI / Anthropic / Azure OpenAI | OpenAI requires Zero Data Retention agreement; Azure OpenAI has standard BAA via Azure; Anthropic offers BAA via API on request |

## HIPAA Security Rule (administrative / physical / technical safeguards)

**45 CFR Part 160 and 164 Subparts A and C.**

The Security Rule applies to **electronic PHI (ePHI)** and is organized into three families of safeguards. Each safeguard is either **Required** (R) or **Addressable** (A) — both must be implemented, but Addressable allows documented alternative measures if the standard one is unreasonable.

### Administrative safeguards (164.308)

| Standard | Status | Bodhyanga / operator responsibility |
|----------|--------|--------------------------------|
| Security Management Process (R) | operator | Risk analysis, risk management, sanction policy, info system activity review |
| Assigned Security Responsibility (R) | operator | Named Security Officer |
| Workforce Security (R) | operator | Authorization, supervision, termination procedures |
| Information Access Management (R) | operator + Bodhyanga | RBAC + SMART scopes |
| Security Awareness & Training (R) | operator | Annual workforce training |
| Security Incident Procedures (R) | operator | Incident response plan; Bodhyanga audit log is the forensic source |
| Contingency Plan (R) | operator | Data backup, DR, emergency mode, testing |
| Evaluation (R) | operator | Periodic technical/non-technical evaluation |
| Business Associate Contracts (R) | operator | BAAs with covered entities and subcontractors |

### Physical safeguards (164.310)

| Standard | Status | Notes |
|----------|--------|-------|
| Facility Access Controls (A) | operator | Datacenter controls (cloud provider attestation) |
| Workstation Use (R) | operator | Clinician device policies |
| Workstation Security (R) | operator | Locking, encryption |
| Device & Media Controls (R) | operator | Disposal, reuse, accountability, backup |

For cloud-deployed Bodhyanga, the cloud provider's HIPAA / SOC 2 attestation covers most physical safeguards (with a BAA in place).

### Technical safeguards (164.312)

| Standard | Implementation status | Notes |
|----------|-----------------------|-------|
| **Access Control** (R) — unique user ID | planned | OIDC `sub` becomes the user ID; persisted in audit log |
| Emergency access procedure (R) | planned | Break-glass account documented |
| Automatic logoff (A) | planned | Session timeout at API gateway |
| Encryption / decryption (A) | planned | TLS 1.3 + at-rest KMS |
| **Audit Controls** (R) | partial | Every message logged today (see [Audit logging in the README](../README.md#audit-logging)); production needs immutable sink |
| **Integrity** (R) — alterations | planned | Append-only audit; checksum verification |
| Mechanism to authenticate ePHI (A) | planned | Hash + digital signature on persisted records |
| **Person / Entity Authentication** (R) | planned | OIDC / SMART / mTLS |
| **Transmission Security** (R) — integrity & encryption | planned | TLS 1.3 end-to-end |

### HIPAA Security Rule controls — code-level cross-walk

| Control | Bodhyanga code |
|---------|-----------|
| Audit log of every interaction | `pkg/orchestration` logs each `HandleMessage`, every governance decision |
| Access control — correlation | `RequireCaseIDPolicy` in [pkg/governance/medical_policy.go](../pkg/governance/medical_policy.go) |
| Cost / scope limits | `MaxDiagnosticCostPolicy` |
| Content size limit (DoS) | `MaxContentLengthPolicy` |
| PHI redaction (planned) | `RedactNarrativePolicy` |
| Tenant isolation (planned) | `RequireTenantPolicy` |

## HIPAA Breach Notification Rule

**45 CFR Part 164 Subpart D.**

If a breach of unsecured PHI occurs:

| Audience | Deadline | Source |
|----------|----------|--------|
| Affected individuals | 60 days from discovery | 164.404 |
| HHS Secretary | 60 days (≥500 affected) or annually (<500) | 164.408 |
| Prominent media (≥500 in a state) | 60 days | 164.406 |
| Business associate → covered entity | "without unreasonable delay and in no case later than 60 days" | 164.410 |

Bodhyanga's audit log is the **forensic record** that lets the operator determine the breach scope (what PHI, whose, what users).

**Safe Harbor exclusion (encryption):** a breach of *encrypted* data that meets the HHS Guidance (FIPS 140-2/3 cryptographic modules, NIST-approved algorithms) does **not** count as a breach. This is why encryption at rest and in transit is non-negotiable.

## HITECH and the Information Blocking Rule

**HITECH Act (2009):** strengthened HIPAA enforcement, extended liability to business associates, and authorized higher penalties.

**21st Century Cures Act (2016)** and the **ONC Information Blocking Rule (45 CFR Part 171):** prohibit "actions likely to interfere with access, exchange, or use of EHI" by clinicians, EHR vendors, and HIEs. There are eight named exceptions (Preventing Harm, Privacy, Security, Infeasibility, Health IT Performance, Content & Manner, Fees, Licensing).

**Bodhyanga implication:** if Bodhyanga sits inside a HIPAA-regulated workflow, it must not technically block legitimate access requests. The FHIR adapter and HTTP API should expose patient-mediated read access without artificial friction.

## 42 CFR Part 2 (substance-use records)

Stricter than HIPAA for **federally assisted programs treating substance use disorder (SUD)**. Requires patient consent for many disclosures HIPAA otherwise allows. Re-disclosure prohibited without a specific consent each time.

**Bodhyanga status:** not addressed. If Bodhyanga ever stores SUD treatment records, add a `SegmentationPolicy` that flags Part 2 patients and demands an explicit consent token in `Metadata` before disclosing.

## State privacy laws (CMIA, NY SHIELD)

A non-exhaustive list of state laws that may impose obligations stricter than HIPAA:

| Law | Highlights |
|-----|------------|
| **California Confidentiality of Medical Information Act (CMIA)** | Civil + criminal penalties; private right of action |
| **New York SHIELD Act** | Reasonable security for private info of NY residents |
| **Texas HB 300 / Medical Records Privacy Act** | Training, breach notification, broader "covered entity" |
| **Illinois Biometric Information Privacy Act (BIPA)** | Consent + retention rules for biometrics |
| **Washington My Health My Data Act** | Consumer health data outside HIPAA's scope |
| **California Consumer Privacy Act (CCPA / CPRA)** | Some health data is exempt; non-PHI is not |

## GDPR and international privacy

If Bodhyanga processes EU residents' health data, **GDPR Article 9** (special categories) and the **EHDS (European Health Data Space)** apply. Key obligations:

| GDPR Article | Bodhyanga implication |
|--------------|-------------------|
| Art. 5 — lawfulness, fairness, transparency, minimization | Same as HIPAA minimum necessary |
| Art. 6 + 9 — lawful basis & special-category basis | Need explicit consent or substantial public interest |
| Art. 15 — right of access | Patient export API |
| Art. 17 — right to erasure | Persistence layer must support hard-delete by `patient_id` |
| Art. 20 — portability | FHIR / Bulk Data export |
| Art. 25 — Privacy by design | Bodhyanga's governance-before-handler is exactly this |
| Art. 32 — security of processing | Encryption, audit, access control |
| Art. 33 / 34 — breach notification | 72 h to supervisory authority |
| Art. 35 — DPIA | Required for AI-based clinical decision support |
| Art. 22 — automated decision-making | "Right not to be subject to a decision based solely on automated processing" — Bodhyanga's verifier + human review pattern fits this |
| Cross-border transfers (Ch V) | SCCs / adequacy decisions; data residency controls |

Other relevant regimes:
- **UK Data Protection Act 2018** + UK GDPR
- **Canada PIPEDA / PHIPA (Ontario) / HIA (Alberta)**
- **Brazil LGPD**
- **India DPDP Act 2023**

## HITRUST CSF, SOC 2, ISO 27001 / 27799

Three frameworks operators commonly pursue:

| Framework | Scope | Audit type |
|-----------|-------|-----------|
| **HITRUST CSF** | Healthcare-specific framework that incorporates HIPAA, NIST, ISO, PCI | r2 (assessable, full certifiable); the heaviest |
| **SOC 2 (Type II)** | AICPA Trust Services Criteria — Security, Availability, Processing Integrity, Confidentiality, Privacy | Most common SaaS attestation; ~6 month observation |
| **ISO/IEC 27001** | Information security management system | International recognition |
| **ISO/IEC 27799** | Health-sector application of 27002 | Adjunct to 27001 for healthcare |
| **NIST SP 800-66** | HIPAA implementation guide | Not certifiable; useful as a control-mapping checklist |
| **NIST AI RMF (AI 600-1)** | AI Risk Management Framework — generative AI profile | Required if Bodhyanga uses LLMs |

## De-identification and re-identification risk

If Bodhyanga processes data without HIPAA's restrictions, **the data must be de-identified.** HIPAA permits two methods:

1. **Safe Harbor (164.514(b)(2))** — remove the 18 listed identifiers (names, geography smaller than state, dates, SSN, MRN, IPs, biometrics, photos, etc.) AND have no actual knowledge that the remaining data could re-identify.
2. **Expert Determination (164.514(b)(1))** — a qualified statistician certifies "very small risk" of re-identification.

**Re-identification risk** rises sharply with:
- High-resolution dates (use year only)
- Rare diagnoses combined with demographics
- Geographic granularity (ZIP3 vs ZIP5)
- Free-text narrative (clinical notes may contain names, addresses)

**Bodhyanga's role:** today the demo runs only on synthetic data. A production export pipeline must run de-identification *before* records leave the regulated boundary.

## Governance policies in Bodhyanga

Live in [pkg/governance/](../pkg/governance/). Composed via `governance.NewComposite(...)` and applied by the orchestrator before every `HandleMessage`.

| Policy | What it checks | When |
|--------|----------------|------|
| `MaxContentLengthPolicy{Max:65536}` | `len(msg.Content) ≤ 65536` | Always — DoS / prompt-bombing protection |
| `RequireCaseIDPolicy{}` | `metadata.case_id` present for clinical types | `presentation`, `case_state`, `ask_question`, `order_test`, `diagnosis`, `verify_reasoning` |
| `MaxDiagnosticCostPolicy{}` | `spent_usd + test_cost_usd ≤ max_cost_usd` | `order_test` |
| `ResearchDisclaimerPolicy{}` | Always allow; documents intent | All — research demo marker |

**Planned policies** (extend in [pkg/governance/medical_policy.go](../pkg/governance/medical_policy.go)):

| Policy | Purpose |
|--------|---------|
| `RequireTenantPolicy` | Multi-tenant isolation |
| `RequireScopePolicy{Scope}` | SMART-on-FHIR scope check |
| `RedactNarrativePolicy` | Strip clinical narrative from logs |
| `Part2ConsentPolicy` | 42 CFR Part 2 SUD records |
| `BreakGlassPolicy` | Allow emergency cross-patient reads with audit |
| `RateLimitPolicy` | Per-tenant / per-clinician QPS cap |

### Tests

```bash
go test ./pkg/governance/... -v
go test ./pkg/medical/...     -v
```
