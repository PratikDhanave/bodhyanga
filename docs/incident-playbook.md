# AI-Specific Incident Playbook

> Operational runbooks for incidents unique to AI-driven clinical systems. Pairs with [docs/operations.md § Runbooks](operations.md#runbooks) (which covers the more general ops incidents like "denied messages" and "queue backups"). This file targets the AI-specific failure modes that don't have parallels in conventional microservice ops.

---

## Playbook structure

Every playbook in this file follows the same six-phase structure adapted from NIST SP 800-61 (Computer Security Incident Handling Guide), extended for AI-specific signals:

1. **Detection** — what the signal looks like; where it surfaces
2. **Triage** — initial severity classification (P0/P1/P2/P3) and on-call routing
3. **Escalation** — who else gets paged and when
4. **Containment** — stop the harm from spreading
5. **Eradication + Recovery** — fix the root cause and resume service
6. **Post-mortem** — blameless review; capture the lesson

---

## Six AI-specific incident scenarios

### Scenario 1 — Model output appears clinically dangerous in a real case

**Detection.** A clinician reports via the HITL queue UI or directly via on-call channel that a Bodh-recommended diagnosis or care plan would cause patient harm if acted on. Or: the audit log surfaces a `KindReviewDecision` with `decision=reject` AND `reason` includes phrases like "would harm," "contraindicated," "incorrect."

**Triage.** **P0.** Page on-call clinician + on-call engineer + compliance officer.

**Escalation.**
- T+0: page on-call engineer
- T+15 min: page service owner + compliance officer
- T+30 min: notify medical director (deployment-specific contact)
- T+60 min: prepare external notification if pattern repeats (more than one case)

**Containment.**
1. Identify the case_id, agent_id, and timestamp from the audit log.
2. Use `GET /explain/{case_id}` to pull the full chain (PR / Right-to-explanation handler).
3. Determine: was this a one-off or systematic?
   - **One-off** (e.g. LLM hallucination on this single case): no immediate config change needed; preserve audit trail.
   - **Systematic** (same wrong recommendation across multiple cases): flip the affected agent's `HITL` flag to `true` if not already; raise `MinConfidence` threshold; or disable the LLM variant via env var and let the rule-based fallback take over.

**Eradication + Recovery.**
- One-off: no recovery needed for the system; the HITL gate caught it (or should have — if it didn't, that's the systematic finding).
- Systematic: identify the prompt / threshold / data input that caused the pattern. Fix in code; deploy. Update bench fixtures to catch the pattern in CI going forward.

**Post-mortem.** Within 5 business days. Cover: when the issue started, when detected, who was affected (count of cases), why HITL didn't catch it (if applicable), what changed, what alerts to add. Document in `docs/incidents/YYYY-MM-DD-clinically-dangerous-output.md` (this directory is itself an open file).

---

### Scenario 2 — Prompt injection detected in production

**Detection.** `pkg/safety.IsRefusal` or `pkg/safety.ContainsPHIPatterns` triggers in `LLMDiagnostician` or `LLMInboxTriage` against a real user input. The `[llm-trace]` JSON line records the trigger; the audit event records `KindAgentError` with `error_text` mentioning the safety check.

**Triage.** **P1** by default. **P0** if the injection appears to come from a malicious external system (not a confused patient).

**Escalation.**
- T+0: on-call engineer
- T+30 min: security team + service owner

**Containment.**
1. Identify the source of the injection attempt. Inbox messages from a specific patient? An ADT feed from a specific hospital? A FHIR import?
2. Block the source at the ingress layer if it's clearly malicious (a script, not a confused patient).
3. Inspect the audit log for prior attempts from the same source — was this the first or pattern?
4. Verify no PHI leaked in the model's response (`pkg/safety.ContainsPHIPatterns` ran on output).

**Eradication + Recovery.**
- If patient-source: train the inbox triage on the pattern (add a red-team fixture).
- If external-system-source: investigate the upstream system; coordinate with their security team.
- Expand `pkg/safety` patterns to catch this specific injection shape.

**Post-mortem.** Within 7 days. Cover: injection pattern, whether it succeeded, what was actually exfiltrated (audit-log-grade reconstruction), what red-team fixtures now cover this pattern.

---

### Scenario 3 — RAG corpus contains outdated guideline contributing to a misdiagnosis

**Detection.** Clinician escalation, or bench regression alert when a new corpus revision drops accuracy.

**Triage.** **P1.** Page service owner + clinical lead.

**Escalation.**
- T+0: on-call engineer + service owner
- T+60 min: clinical lead

**Containment.**
1. Identify the corpus document(s) implicated. From audit + `[llm-trace]` records, find which RAG citations the diagnosis used.
2. Verify the corpus document is in fact outdated against current clinical evidence. (Don't assume; check.)
3. Temporarily disable RAG (`unset BODH_KNOWLEDGE_CORPUS`) and restart `cmd/care`. The LLM diagnostician will run without retrieval. Bench accuracy may drop, but the misleading citation is gone.

**Eradication + Recovery.**
- Update the corpus document(s) with current clinical evidence.
- Re-enable RAG (`export BODH_KNOWLEDGE_CORPUS=...`).
- Add a bench fixture that exercises the corrected document.
- Schedule a quarterly corpus review.

**Post-mortem.** Within 10 days. Cover: how the outdated guideline got into the corpus, what review process failed, what cadence prevents recurrence.

---

### Scenario 4 — Silent model behavior change (LLM provider rolled out new weights)

**Detection.** Bench regression alert in nightly CI; OR fallback rate spikes; OR confidence-distribution shifts on the dashboard.

**Triage.** **P1.** Page on-call engineer.

**Escalation.**
- T+0: on-call engineer
- T+60 min: service owner if not stabilising

**Containment.**
1. Verify the provider's status page / changelog. Did the provider announce a model change?
2. Pin the model version explicitly (`BODH_ANTHROPIC_MODEL=claude-sonnet-4-6-20260520` instead of `claude-sonnet-4-6`).
3. If accuracy regressed, either switch to a different model tier or fall back to the rule-based provider while you investigate.

**Eradication + Recovery.**
- Decide: keep the new model and adjust prompts, or pin to the previous version permanently?
- Update the model card for the affected variant with the new behavior characteristics.
- Add bench fixtures that catch this specific regression pattern.

**Post-mortem.** Within 7 days. Cover: when the change happened (provider's, ours, or both), what signal would have caught it sooner, what model-card update should land.

---

### Scenario 5 — Audit log integrity check failed

**Detection.** Periodic Merkle chain-hash check (when wired) fails. OR the audit row count diverges from the expected per-tenant rate. OR the `audit_events` table has rows that shouldn't be there (e.g. rows for a `tenant_id` not in the registered tenant list).

**Triage.** **P0.** Compliance officer + on-call engineer + service owner.

**Escalation.**
- T+0: on-call engineer + compliance officer
- T+30 min: service owner + CISO (deployment-specific)
- T+60 min: prepare to notify the covered entity if integrity loss is confirmed

**Containment.**
1. **Do not delete or modify the suspicious audit data.** It IS the forensic record; preserve it.
2. Identify the time window of the integrity-check failure.
3. Determine: was this a write that shouldn't have happened (someone bypassed the GRANT model), a delete that shouldn't have happened (UPDATE/DELETE permission leaked), or a tampering attempt (someone with DB access changed the chain)?
4. Lock further writes if necessary by temporarily reducing the `bodh_app` role's privileges to SELECT-only until investigation completes.

**Eradication + Recovery.**
- Root-cause the integrity loss. Revoke any unauthorised access. Rotate credentials.
- Restore from the WORM sink (S3 Object Lock / Azure Immutable Blob) if data was modified.
- Re-establish the chain-hash baseline from the WORM-restored events.

**Post-mortem.** Within 5 business days. **Mandatory regulator notification consideration** under HIPAA §164.404 (60 days). Cover: scope of integrity loss, affected patients (if any), regulatory disclosure analysis, controls added.

---

### Scenario 6 — Tenant isolation breach detected via SQL audit

**Detection.** A query in the audit log shows a `tenant_id` reading rows belonging to a different `tenant_id`. OR an integration test for RLS regresses. OR a BI tool reports cross-tenant data in a per-tenant dashboard.

**Triage.** **P0.** Highest severity. Compliance + security + service owner.

**Escalation.**
- T+0: on-call engineer + compliance officer
- T+15 min: service owner + CISO + legal
- T+60 min: prepare to notify affected covered entities (tenants) under HIPAA §164.410

**Containment.**
1. Identify exactly which records were exposed and to which tenant. The audit log is the forensic source.
2. Lock the offending caller. Rotate that tenant's API credentials. Disable any service account / role that may have BYPASSRLS.
3. Verify the RLS policies in Postgres are still ENABLED and FORCED (`SELECT * FROM pg_policies WHERE tablename = 'patient_records';`).
4. Run the BYPASSRLS sanity-check from `pkg/persistence/postgres/postgres_test.go` against the live DB.

**Eradication + Recovery.**
- Fix the RLS policy / role privilege issue that allowed the breach.
- Run a full audit of every tenant_id-bound query path.
- Notify affected covered entities under HIPAA §164.410 (within 60 days, ideally far sooner).
- Prepare HHS Secretary notification under §164.408 if ≥500 affected.

**Post-mortem.** Within 3 business days. **Mandatory regulator notification under HIPAA Breach Notification Rule** unless the data was properly encrypted per the Safe Harbor exclusion. Cover: scope of exposure, technical root cause, control failures, what changes to prevent recurrence.

---

## On-call rotation expectations

| Role | When paged | Required response time |
|---|---|---|
| On-call engineer | Any P0 / P1 | 15 min |
| Service owner | P0, or P1 >60 min unresolved | 30 min |
| Clinical lead | Scenarios 1, 3 | 60 min |
| Compliance officer | Scenarios 5, 6, or P0 PHI-related | 30 min |
| Security / CISO | Scenarios 2, 5, 6 if external-attacker suspected | 30 min |
| Medical director | Scenario 1 only | 120 min |

Operator deployments customise this matrix. The names belong to specific people; the roles are the architectural contract.

---

## What this playbook does NOT cover

These belong to the operator's broader incident response, not Bodh-specific:

- Cloud provider outages (AWS / Azure / GCP) — standard cloud IR
- Database outages — Postgres-specific runbooks
- TLS certificate expiry — cert-manager / DNS rotation
- DDoS / volumetric attacks at the API gateway — WAF + rate-limit IR
- Compromised developer laptop with API keys — IAM rotation runbook
- Workforce HR issues (badge revocation, etc.) — HR + IT

The Bodh-specific playbooks above describe scenarios where the AI behavior itself is the incident.

---

## Reference

- NIST SP 800-61 Rev. 2 — Computer Security Incident Handling Guide
- HIPAA Breach Notification Rule (45 CFR Part 164 Subpart D)
- [docs/operations.md § Runbooks](operations.md#runbooks) — non-AI operational runbooks
- [docs/governance.md § Audit events](governance.md#audit-events) — what the audit log captures
- [docs/aigp-governance-mapping.md](aigp-governance-mapping.md) — AIGP body-of-knowledge cross-reference
