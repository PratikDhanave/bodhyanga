# Bodhyanga × AIGP — Mapping IAPP's AI Governance Professional Body of Knowledge onto a Working Codebase

> **Purpose.** The IAPP's [Certified AI Governance Professional](https://iapp.org/certify/aigp/) (AIGP) credential covers AI governance as a discipline — principles, applicable laws, and governance throughout the AI lifecycle. Bodhyanga is a working open-source implementation that demonstrates many of these topics in code. This document maps each AIGP knowledge area onto Bodhyanga's current implementation and identifies where Bodhyanga could grow to cover the rest.

> **Audience.** Three readers:
> 1. **AIGP candidates** studying for the exam who want a concrete reference implementation to ground the abstract material against.
> 2. **AI governance practitioners** designing programs at their organisations who need patterns they can adapt.
> 3. **Bodhyanga maintainers** evaluating where to strengthen the project's governance posture.

> **Disclaimer.** This document is educational, not certification preparation material. It is not affiliated with or endorsed by the IAPP. Pursuing AIGP certification requires the IAPP's official study materials and training. Bodhyanga remains a research and engineering reference; it is not approved for clinical use.

---

## Contents

- [Executive summary](#executive-summary)
- [Domain I — Foundations of AI governance](#domain-i--foundations-of-ai-governance)
- [Domain II — Laws, standards and frameworks](#domain-ii--laws-standards-and-frameworks)
- [Domain III — Governing AI development](#domain-iii--governing-ai-development)
- [Domain IV — Governing AI deployment and use](#domain-iv--governing-ai-deployment-and-use)
- [Gap analysis — where Bodhyanga could be stronger](#gap-analysis--where-bodhyanga-could-be-stronger)
- [Practice scenarios using Bodhyanga](#practice-scenarios-using-bodhyanga)
- [Recommended additions for full AIGP coverage](#recommended-additions-for-full-aigp-coverage)
- [Cross-reference index](#cross-reference-index)

---

## Executive summary

AIGP covers four domains:

| Domain | Bodhyanga's coverage | Gap |
|---|---|---|
| **I — Foundations of AI governance** | Strong. The architecture itself is the governance — orchestrator + composable policies + audit + HITL gate are governance primitives, not features bolted on. | Org-level governance documentation (committee structures, RACI matrices) is out of scope for a codebase. |
| **II — Laws, standards and frameworks** | Strong for HIPAA / GDPR / 21st Century Cures Act / 42 CFR Part 2. Mentions but doesn't enforce EU AI Act, NIST AI RMF, ISO/IEC 42001. | EU AI Act risk classification specifically (which Bodhyanga isn't subject to as research) and ISO 42001 audit trail formatting. |
| **III — Governing AI development** | Strong for design-time governance (rule fallback contract, illustrative-only thresholds, MARA design rules). Strong on training/test data governance (synthetic only, no PHI in CI, red-team eval catalogue). | Model training itself isn't part of Bodhyanga — we use vendor LLMs and rule-based logic, not custom-trained models. So "training" governance maps differently. |
| **IV — Governing AI deployment and use** | Strong. HITL gate, audit pipeline, drift consideration via bench, transparent fallback, condition-aware confidence thresholds. | Active drift monitoring (vs point-in-time bench), equity audit pipeline, post-deployment incident reporting workflow. |

**Bottom line:** Bodhyanga demonstrates ~70% of AIGP's body of knowledge through code. The remaining 30% is either organisational (committees, training programs) or specific to high-risk EU AI Act systems Bodhyanga doesn't claim to be.

---

## Domain I — Foundations of AI governance

### A. Understand what AI is and why it needs governance

| AIGP topic | Bodhyanga's expression |
|---|---|
| AI definitions and taxonomy | `pkg/llm.Client` abstracts over Anthropic, OpenAI, Ollama — Bodhyanga recognises these as **generative AI / LLM systems**. Rule-based agents are recognised as **deterministic expert systems**. The two are composed via the fallback contract — see [docs/architecture.md § LLM agent fallback contract](architecture.md#llm-agent-fallback-contract). |
| Why AI needs governance | The repo's [research disclaimer](../README.md#research-disclaimer-read-first) explicitly enumerates the reasons: thresholds are illustrative not clinical, demo agents simulate answers, the regulatory bar matters, and "safety, trust, transparency, and human oversight" are the heart of the future product. |
| Risks AI introduces | Documented per-agent in [docs/agents.md](agents.md) under each agent's "Failure modes" section. Adversarial cases catalogued in `pkg/eval/redteam.go` + `data/eval/redteam.json` — six adversarial categories tested (prompt injection, conflicting labs, symptom drift, cost gaming, PHI leakage, confidence overstatement). |
| Stakeholder considerations | The HITL gate ([docs/architecture.md § Human-in-the-loop pattern](architecture.md#human-in-the-loop-pattern)) is the architectural recognition that clinicians, patients, and reviewers are stakeholders with veto power, not just users. |

### B. Establish and communicate organizational expectations for AI governance

For a codebase, "organisational expectations" become **documented design rules**. Bodhyanga has them.

| Expectation | Where it's documented |
|---|---|
| Design rules from MARA | [CONTRIBUTING.md § Design rules](../CONTRIBUTING.md#design-rules-from-mara) — 10 rules including "clinical safety: outputs are decision support, not autonomous care; human attestation required for production" |
| Naming conventions reflecting governance | All agent IDs in `snake_case`, all condition keys `snake_case`, all magic numbers carry "illustrative — educational demo, not a clinical guideline" comments |
| Commit + PR conventions | [CONTRIBUTING.md § Commit and PR conventions](../CONTRIBUTING.md#commit-and-pr-conventions) requires test + docs + bench impact for diagnostic changes |
| Security issue handling | [CONTRIBUTING.md § Reporting a security issue](../CONTRIBUTING.md#reporting-a-security-issue) — separate from public bug reports |
| Disclaimer expectations | Every threshold in code carries an inline comment marking it illustrative; the README disclaimer is at the top; LinkedIn articles ([bodhyanga-linkedin/](../bodhyanga-linkedin/)) all end with the disclaimer |

### C. Establish policies and procedures to apply throughout the AI life cycle

This is where Bodhyanga's architecture is the documentation:

| Life cycle phase | Procedure in Bodhyanga |
|---|---|
| Design | Agents are designed against [docs/architecture.md](architecture.md) discipline — they don't call each other directly; they emit messages; governance runs before any handler |
| Build | Every PR includes table-driven tests for the changed agent's `HandleMessage` ([CONTRIBUTING.md § Adding an agent](../CONTRIBUTING.md#adding-an-agent)) |
| Training data | Synthetic only — `data/cases/`, `data/eval/redteam.json`, `data/knowledge/corpus.json`. No PHI ever in CI |
| Testing | `go test ./...` + `go run ./cmd/bench -fail-on-regression` enforces both unit and integration tests. CI runs both. |
| Release | Squash-merge with conventional-commit subjects; baseline regression files (`data/cases/baseline.json`) gate accuracy + cost regressions |
| Deployment | Documented in [docs/deployment.md](deployment.md) — Postgres backend, OTel export, BAA chain, K8s manifests |
| Monitoring | [docs/operations.md § SLI / SLO recommendations](operations.md#sli--slo-recommendations) — 10 SLIs with target SLOs |
| Decommissioning | GDPR Article 17 hard-delete path documented in [pkg/persistence/postgres/README.md](../pkg/persistence/postgres/README.md) |

---

## Domain II — Laws, standards and frameworks

### A. How existing data privacy laws apply to AI

| Law | Bodhyanga's coverage |
|---|---|
| **HIPAA Privacy Rule** (45 CFR §§160, 164 Subparts A & E) | [docs/compliance.md § HIPAA Privacy Rule](compliance.md#hipaa-privacy-rule). Minimum necessary enforced via `RequireCaseIDPolicy` + audit field selection. BAA chain enumerated. |
| **HIPAA Security Rule** (45 CFR Part 164 Subpart C) — administrative / physical / technical safeguards | [docs/compliance.md § HIPAA Security Rule](compliance.md#hipaa-security-rule). §164.312(b) audit controls **wired** (PR #12). Encryption-at-rest schema ready; column-level pgcrypto pending. |
| **HIPAA Breach Notification Rule** (45 CFR Part 164 Subpart D) | [docs/compliance.md § HIPAA Breach Notification](compliance.md#hipaa-breach-notification-rule). Safe Harbor exclusion documented as the reason for KMS-backed encryption. |
| **HITECH** + **21st Century Cures Act / Information Blocking Rule** | [docs/compliance.md § HITECH](compliance.md#hitech-and-the-information-blocking-rule). FHIR adapter exposes patient-mediated read access. |
| **42 CFR Part 2** (substance-use treatment) | Planned `Part2ConsentPolicy` documented in [docs/governance.md § Planned policies](governance.md#planned-policies). |
| **GDPR Articles 5, 9, 22, 35, etc.** | [docs/compliance.md § GDPR and international privacy](compliance.md#gdpr-and-international-privacy). Article 22 ("not subject to a decision based solely on automated processing") **architecturally satisfied** by the HITL gate. Article 17 (right to erasure) implemented as hard-delete-by-patient-id. |
| **State laws** (CMIA, NY SHIELD, Texas HB 300, BIPA, MHMDA, CCPA/CPRA) | Enumerated in [docs/compliance.md § State privacy laws](compliance.md#state-privacy-laws). Bodhyanga's architecture supports the technical controls; operator policies handle the rest. |

### B. How other types of existing laws apply to AI

| Law / regime | Bodhyanga's posture |
|---|---|
| **FDA Software-as-a-Medical-Device (SaMD)** | Bodhyanga is positioned to fit the **21 CFR §3060 CDS carve-out** (21 USC §360j(o)) via the HITL gate. See [bodhyanga-linkedin/02-cures-act-as-code.md](../bodhyanga-linkedin/02-cures-act-as-code.md). |
| **21 CFR Part 11** (electronic records & signatures) | Audit pipeline shape is compatible (append-only at DB GRANTs, opaque event IDs). E-signature support is planned. |
| **ONC Health IT Certification** | USCDI conformance + SMART App Launch are planned. FHIR R4 (Patient / Observation / Encounter / Condition) is implemented. |
| **Anti-discrimination (Title VII, ADA, ECOA)** | Bodhyanga's domain is clinical, not employment/lending. Equity audit pipeline (planned) addresses stratified accuracy across demographics. |
| **Copyright / IP** | LLM provider terms govern. Bodhyanga doesn't train models; it composes with vendor LLMs under their terms. RAG corpus is synthetic. |
| **Product liability** | The research disclaimer at the top of every file is the explicit position: "Not approved for clinical use." |

### C. AI-specific laws

| Regime | Bodhyanga's posture |
|---|---|
| **EU AI Act** (Regulation 2024/1689) | Bodhyanga is research-only, so high-risk classification doesn't bind. But the architecture aligns with high-risk requirements (Articles 9-15): risk management (governance composer), data governance (synthetic only), record-keeping (audit), transparency (provenance type), human oversight (HITL gate), accuracy/robustness (bench regression gating), cybersecurity (PHI redaction, opaque IDs). |
| **US Executive Order 14110** (revoked Jan 2025) and successor frameworks | Tracked via documentation references. |
| **Colorado AI Act** (SB24-205) | Algorithmic discrimination prevention — Bodhyanga's transparent rule-based diagnostician with provenance is the right architectural shape. |
| **NYC Local Law 144** (automated employment decisions) | Out of scope (clinical, not employment). |
| **California, Connecticut, Texas, Utah AI consumer protection statutes** | Apply if Bodhyanga ever ships consumer-facing care. Tracked but not enforced. |

### D. Industry standards and tools

| Standard / framework | Bodhyanga's coverage |
|---|---|
| **NIST AI RMF (AI 600-1)** | The four functions — Govern, Map, Measure, Manage — map onto Bodhyanga's [governance composer](governance.md), [agent registry + capabilities](architecture.md#registry-semantics), [bench + audit + LLMTrace](operations.md), and [HITL gate + planned BreakGlass policy](governance.md#planned-policies). |
| **ISO/IEC 42001** (AI management systems) | Audit log shape + documented processes (CONTRIBUTING / ROADMAP / docs) are the substrate. Formal certification would require organisation-level controls Bodhyanga-as-codebase can't supply. |
| **ISO/IEC 23894** (AI risk management) | Risk identification embedded in per-agent failure-mode documentation; mitigation in fallback contract + safety guardrails. |
| **HITRUST CSF / SOC 2 Type II / ISO 27001** | Documented as production targets in [docs/compliance.md § HITRUST CSF, SOC 2, ISO 27001/27799](compliance.md#hitrust-csf-soc-2-iso-27001--27799). |
| **OWASP Top 10 for LLM Applications** | Addressed in `pkg/safety` (prompt injection check, hallucination grounding via RAG, PHI patterns in output, refusal detection). |
| **MITRE ATLAS** | Adversarial cases catalogued in `pkg/eval/redteam.go`. |

---

## Domain III — Governing AI development

### A. Govern the designing and building of the AI system

| AIGP topic | Bodhyanga's implementation |
|---|---|
| AI system design principles | [docs/architecture.md § Design rules from MARA](architecture.md#design-rules-from-mara). Notably: clinical safety = decision support, not autonomous care. |
| Selecting the right AI approach | The fallback contract (deterministic rules under LLM agents) is an explicit choice: don't replace deterministic logic where you have it; layer LLMs above as enhancement. |
| Privacy by design | Opaque `case_id` / `patient_id` as routing tokens. PHI redaction at the logger seam. Audit event field selection ("enough to reconstruct, never enough to leak"). All architectural, not bolted on. |
| Security by design | Fail-closed governance. Append-only audit at DB GRANTs. mTLS planned via SPIFFE/SPIRE. See [docs/agent-interop.md § Proposal 6](agent-interop.md#proposal-6-spiffespire-integration-when-on-kubernetes-medium-impact-2-weeks). |
| Trustworthy AI characteristics | Bodhyanga's positioning maps directly: **valid + reliable** (bench), **safe** (HITL + fallback), **secure + resilient** (RLS + redaction), **accountable + transparent** (audit + provenance), **explainable + interpretable** (rule-based fallback + rationale field), **privacy-enhanced** (PHI redaction + opaque IDs), **fair with managed bias** (red-team + equity audit planned). |

### B. Govern the collection and use of data in training and testing

Bodhyanga doesn't train custom models — we compose vendor LLMs + rule-based logic. So training-data governance maps onto **test data governance**, **RAG corpus governance**, and **bench fixture governance**:

| Topic | Bodhyanga's practice |
|---|---|
| Data collection consent | Out of scope — Bodhyanga uses only synthetic data. Production deployments must add this on the operator side. |
| Data quality | Synthetic fixtures designed with care: opaque IDs, no PHI, condition keys match the manifest, gold diagnoses are clinically reasonable. Manifest schema v2 makes the data contract explicit. |
| Data minimisation | Agents see only the fields they need (`Capabilities()` declares intent). PHI columns deliberately separated from audit columns (`payload_ct` vs the event metadata fields). |
| Bias detection | Stratified bench evaluation is planned per [ROADMAP Phase 7](../ROADMAP.md). LLM-as-judge in `pkg/eval.LLMJudge` already produces dimensional scores including `differential_quality` and `safety`. |
| Red-team / adversarial testing | `pkg/eval/redteam.go` + `data/eval/redteam.json` — 6 categories: prompt injection, conflicting labs, symptom drift, cost gaming, PHI leakage, confidence overstatement. |
| Provenance / lineage | `pkg/medical.Provenance` records which method (rule / llm / debate / reflexion / cascade), which provider/model, which inputs, and the round-by-round trace for multi-call methods. Carried in `DiagnosisProposal.ProvenanceJSON`. |
| Test data isolation from prod | CI uses synthetic only. The bench runs against `data/cases/` fixtures. Postgres integration tests use testcontainers — ephemeral, no cross-environment contamination. |

### C. Govern the release, monitoring and maintenance of the AI system

| Topic | Bodhyanga's practice |
|---|---|
| Release procedures | Squash-merge to `main` with conventional-commit subjects. CI gates: `go vet`, `go test`, `govulncheck`, `docker build`, optional bench regression. |
| Versioning | Standard semver via Git tags. The audit event carries `agent_id` + `MessageID` so post-hoc reconstruction can pin behaviour to a specific commit + agent version. |
| Monitoring | Per-call `LLMTrace` records (provider, model, tokens, cached_tokens, stop_reason, duration). OTel export ready (PR #7). Audit pipeline records every message (PR #12). |
| Drift detection | The bench's baseline regression file (`data/cases/baseline.json`) catches accuracy + cost regressions in CI. Active drift monitoring (per-day rolling accuracy) is on [ROADMAP Phase 7](../ROADMAP.md) as ⚪ planned. |
| Incident response | The audit log is the forensic record. Runbooks for four common incidents documented in [docs/operations.md § Runbooks](operations.md#runbooks). |
| Maintenance / lifecycle | `ROADMAP.md` enumerates the phase plan with decision log. Deferred items per PR carry "deferred" sections to keep the backlog honest. |

---

## Domain IV — Governing AI deployment and use

### A. Evaluate key factors and risks relevant to the deployment decision

| Topic | Bodhyanga's practice |
|---|---|
| Risk classification | EU AI Act high-risk classification is the right shape for clinical decision support. Bodhyanga's architecture aligns even without legal binding. |
| Use-case fit assessment | Per-agent `Capabilities()` enumerate the supported uses. Anything not covered is explicitly out of scope. |
| Stakeholder analysis | HITL gates encode "this action affects a real human and requires their disposition." The seven gated agents are the documented list. |
| Vendor due diligence | LLM provider matrix in README is paired with BAA notes (OpenAI requires ZDR; Azure OpenAI inherits Azure's BAA; Anthropic offers BAA via API). |
| Cost-benefit | Cost-aware diagnosis via `cost_guardian` enforces a per-case budget. The Pareto column in `cmd/bench` makes the accuracy-vs-cost tradeoff explicit. |
| Legal / contractual obligations | Documented per-regime in [docs/compliance.md](compliance.md). |

### B. Perform key activities to assess the AI system

This is where AIGP's "AI impact assessment" maps onto Bodhyanga's concrete artefacts:

| Assessment activity | Bodhyanga's artefact |
|---|---|
| **AI impact assessment (AIIA)** — purpose, expected benefits, potential harms | [README § Research disclaimer](../README.md#research-disclaimer-read-first) + per-agent failure modes in [docs/agents.md](agents.md). Each agent enumerates its purpose, expected output, and failure modes. |
| **Privacy impact assessment (PIA / DPIA)** | [docs/compliance.md](compliance.md) is the artefact. GDPR Article 35 DPIA template is planned per [ROADMAP Phase 9](../ROADMAP.md). |
| **Fundamental rights impact assessment** (EU AI Act) | The HITL gate is the central fundamental-rights control (Article 22 alignment). Equity audit (planned) extends coverage. |
| **Bias / fairness assessment** | LLM-as-judge dimensional scores + planned stratified bench evaluation. |
| **Security / robustness assessment** | Red-team eval + safety guardrails + bench regression gating. |
| **Explainability assessment** | The provenance type + rule-based fallback's interpretable thresholds + the audit log's per-decision rationale field. |
| **Compliance assessment** | `pkg/governance` policies are the executable compliance assertions. Adding `Part2ConsentPolicy`, `BreakGlassPolicy`, `RedactNarrativePolicy` extends coverage. |

### C. Govern the deployment and use

| Topic | Bodhyanga's practice |
|---|---|
| Deployment configuration management | `BODHYANGA_*` env vars enumerated in [docs/deployment.md § Environment variables](deployment.md#environment-variables). Each one has an explicit purpose. |
| Access control | Multi-tenant via `RequireTenantPolicy` + Postgres RLS. SMART-on-FHIR scope check via `RequireScopePolicy`. JWT validation (real, not just string check) is planned per [docs/agent-interop.md § Proposal 3](agent-interop.md#proposal-3-real-jwt-validation-in-requirescopepolicy-high-impact-3-5-days). |
| Logging + monitoring + alerting | Every message → audit event. Every LLM call → `LLMTrace` JSON line + optional OTel span. Recommended SLIs/SLOs in [docs/operations.md](operations.md). |
| Human oversight | HITL gate on seven agents. Lossless-on-reject. Cascading approvals. Reviewer rationale captured in audit. |
| Incident management | Append-only audit + runbooks. WORM sink (S3 Object Lock / Azure Immutable Blob) planned for forensic integrity. |
| Transparency to users | Provenance type carries the method + model + confidence. Citations from RAG are surfaced. The fallback path is logged. |
| Continuous improvement | Bench + LLM-judge + red-team are the feedback loops. Phase-tagged ROADMAP items make planning continuous. |
| Decommissioning | GDPR Article 17 hard-delete path documented. Audit retention separate from PHI retention (HIPAA vs GDPR tension resolved deliberately). |

---

## Gap analysis — where Bodhyanga could be stronger

Bodhyanga covers ~70% of AIGP's body of knowledge through code + docs. The remaining 30% breaks into three categories:

### Category 1 — Organisational (out of scope for any codebase)

These belong to the organisation deploying Bodhyanga, not the codebase itself:

- AI governance committee structure (RACI matrix, escalation paths)
- AI ethics review board procedures
- Workforce AI training program
- Vendor management framework
- Internal communication of AI policies
- Periodic governance review cadence
- AI principles statement / responsible-use policy

A real deployment would author these documents alongside Bodhyanga; the codebase points to where they belong (e.g. CONTRIBUTING.md's reference to a "Security Officer" naming is the seam).

### Category 2 — Specific to high-risk EU AI Act systems Bodhyanga doesn't currently claim to be

- Notified body / conformity assessment artefacts
- CE marking documentation
- Post-market monitoring per Article 17 (EU AI Act, not GDPR)
- Serious incident reporting to authorities under Article 73
- Registration in the EU database for high-risk systems

Bodhyanga's research-only positioning means these don't bind. A deployment that wanted to ship into the EU as a real clinical product would add them — and the existing audit + provenance + HITL machinery would feed them.

### Category 3 — Real gaps in Bodhyanga that could close (and what would change)

These are concrete additions where Bodhyanga's coverage of AIGP topics could move from "addressed in docs" to "enforced in code":

1. **Active drift monitor** — currently the bench is point-in-time + CI-gated. A continuous drift monitor (per-condition rolling accuracy with alerts) would close the "monitoring AI in production" topic.
2. **Equity audit pipeline** — stratified accuracy by age / sex / race / SES bracket. Without this, fairness is asserted but not measured.
3. **DPIA / AIIA template generator** — given Bodhyanga's repo state, generate a stub DPIA or AIIA. This would make Bodhyanga useful as a starting point for actual EU AI Act submissions.
4. **Incident response runbook with documented IR test cadence** — runbooks exist for ops issues; AI-specific incident scenarios (model hallucinated dangerous output, RAG corpus poisoned, prompt injection in production) are documented less rigorously.
5. **Vendor risk register for LLM providers** — currently BAA chain is described prose; a structured register with per-vendor risk ratings + last-review dates would be a governance artefact.
6. **Bias bounty / red-team programme** — `pkg/eval/redteam.go` is the catalogue. A protocol for soliciting external red-team contributions (and rewarding them) would extend it.
7. **Right-to-explanation handler** — given a `case_id`, return a human-readable explanation that surfaces the diagnosis, rationale, citations, provenance, audit trail, and any HITL decision. This is GDPR Article 22 + Article 15 in user-facing form.
8. **Model card per LLM-backed agent** — currently the LLM variants document their behaviour in code comments + [docs/agents.md](agents.md). A formal Model Card per Mitchell et al. (2018) would be the AI-systems-engineering canonical artefact.
9. **Algorithmic impact register** — a top-level `IMPACT_REGISTER.md` that enumerates every decision Bodhyanga's agents can recommend, the stakeholders affected, the mitigations in place, and the residual risk. Living document.

Of these, **#7 (right-to-explanation handler) and #8 (model cards)** are the most defensible per-AIGP-domain quick wins. Each is ~3-5 days of work and produces an artefact that maps directly to an AIGP knowledge area.

---

## Practice scenarios using Bodhyanga

These are AIGP-style scenarios anchored in Bodhyanga's actual implementation. Each is suitable as an exam-prep practice question or as a discussion prompt in a governance review.

### Scenario 1 — Fallback contract as a governance control

A clinical AI startup is reviewing its diagnostic system before a HIPAA risk assessment. The system uses LLM-backed agents for primary diagnosis. The reviewer asks: "What happens if the LLM provider has an outage during a patient consultation?"

In Bodhyanga, every LLM-backed agent has a deterministic rule-based fallback. The case always finalises. The audit log records which path was taken via the `[llm-trace]` JSON line and the resulting `audit.Event.agent_id`. Operators can graph the fallback rate per agent as an operational SLI.

**AIGP topics this exercises:**
- Domain I.B — Organisational expectations (the contract is documented in code)
- Domain III.A — Design-time governance
- Domain IV.B — Robustness assessment

### Scenario 2 — Right-to-explanation for a denied refill

A patient files a complaint after their refill request was denied. They invoke their right to explanation under GDPR Article 22(3). The compliance team asks: "Can you reconstruct exactly what happened and why?"

In Bodhyanga, the audit log records every step:
- The `inbox_message` arrived from the patient
- `inbox_triage` classified it as `refill_request`
- `refill_manager` evaluated against the patient's care plan
- Off-plan medication → escalated to nurse task (no auto-approval)
- Nurse reviewed and rejected with rationale in the `Reason` field

The narrative isn't in the audit log (PHI lives in Postgres under separate access controls), but the audit log is the index that lets the compliance team pull the regulated content and produce a full explanation.

**AIGP topics:**
- Domain II.A — GDPR Articles 15, 22
- Domain IV.A — Stakeholder analysis
- Domain IV.C — Transparency to users

### Scenario 3 — Algorithmic impact assessment for a new condition

The team wants to add support for pulmonary embolism (PE) diagnosis. What's the AIIA process?

In Bodhyanga:
1. Add the condition's care plan, test workup, diagnostic rule, and fixture per [docs/data-model.md § Adding a condition](data-model.md#adding-a-condition).
2. Update the bench manifest with `gold_diagnosis: "Pulmonary embolism"`.
3. Run the bench to measure accuracy + cost.
4. Update `data/cases/baseline.json` only after a clinical SME has reviewed the rule's thresholds.
5. The PR description includes: purpose, expected benefits, potential harms, mitigations (HITL gate covers consequential outputs), residual risk (the rule path's accuracy + the LLM fallback's accuracy on this condition).

The PR becomes the AIIA artefact. The PR template (planned per [CONTRIBUTING.md](../CONTRIBUTING.md)) enforces the structure.

**AIGP topics:**
- Domain III.A — Designing the system
- Domain IV.B — AI impact assessment
- Domain IV.C — Governing deployment

### Scenario 4 — Vendor LLM selection under HIPAA

The team must choose between three LLM providers for the diagnostician. What does HIPAA require?

In Bodhyanga, the architecture is provider-agnostic via `pkg/llm.Client`. The governance choice is:
- **Anthropic** — BAA available via API on request
- **OpenAI** — Zero Data Retention agreement (separate from generic BAA)
- **Azure OpenAI** — inherits Azure's BAA; region availability matters
- **Ollama (local)** — no BAA needed (no PHI leaves the cluster)

The decision is documented in the README's BAA chain table and the actual choice is configuration (`-diagnostician=llm-...`). The fallback contract means even if the chosen provider is unavailable, no PHI leaks to a backup provider that doesn't have a BAA.

**AIGP topics:**
- Domain II.A — HIPAA Privacy Rule (BAA chain)
- Domain IV.A — Vendor due diligence
- Domain III.A — Selecting the right AI approach

### Scenario 5 — EU AI Act high-risk classification

A future Bodhyanga deployment in the EU is classified as high-risk under Annex III (Article 6). What controls does Article 9-15 require, and how does Bodhyanga map?

| Article | Requirement | Bodhyanga's coverage |
|---|---|---|
| 9 | Risk management system | Governance composer + planned policies |
| 10 | Data + data governance | Synthetic only in CI; PHI redaction; opaque IDs |
| 11 | Technical documentation | docs/ tree (15+ files) |
| 12 | Record-keeping | Audit pipeline (PR #12) |
| 13 | Transparency to deployers | Provenance type + LLMTrace + agent cards (planned) |
| 14 | Human oversight | HITL gate on 7 agents (lossless-on-reject) |
| 15 | Accuracy, robustness, cybersecurity | Bench gating + red-team + PHI redaction + RLS + planned SPIFFE |

**AIGP topics:**
- Domain II.C — AI-specific laws
- Domain IV.A — Risk classification
- Domain IV.B — Multiple assessment activities

---

## Recommended additions for full AIGP coverage

Beyond the operational gaps in [Category 3](#category-3--real-gaps-in-bodhyanga-that-could-close-and-what-would-change), three documentation additions would make Bodhyanga significantly more useful as an AIGP study aid:

### Addition A — `docs/aiia-template.md` (AI Impact Assessment template)

A fillable template for each new condition / agent / model swap. Sections:
- Purpose and intended use
- Stakeholders affected (clinician, patient, payer, etc.)
- Expected benefits (concrete + measurable)
- Potential harms (concrete + measurable)
- Mitigations (which Bodhyanga primitives — HITL, fallback, audit, governance policies — are activated)
- Residual risk (after mitigations)
- Monitoring plan (which SLIs, alert thresholds)
- Sunset criteria (when to retire / replace)

The PR template (CONTRIBUTING.md) would reference this for any clinically consequential change.

### Addition B — `docs/model-cards/` directory

One file per LLM-backed agent variant. Per Mitchell et al. (2018) Model Cards for Model Reporting:
- Model details (provider, model, version)
- Intended use (the agent's `Capabilities()`)
- Factors (demographic, clinical context)
- Metrics (bench performance, latency, fallback rate)
- Evaluation data (which fixtures, which red-team subsets)
- Training data (N/A for vendor LLMs; "the vendor's training corpus")
- Quantitative analyses (per-condition accuracy, equity stratification when available)
- Ethical considerations (HITL gate, illustrative-only thresholds)
- Caveats and recommendations (fallback contract, not for clinical use)

### Addition C — `docs/incident-playbook.md`

AI-specific incident scenarios with playbooks:
- "Model output appears clinically dangerous in a real case"
- "Prompt injection detected via safety guardrail in production"
- "RAG corpus contains an outdated guideline that contributed to a misdiagnosis"
- "LLM provider quietly changed model behaviour overnight"
- "Audit log integrity check failed"
- "Tenant isolation breach detected via SQL audit"

Each scenario: detection, triage, escalation, containment, eradication, recovery, post-mortem. Cross-references to the runbooks in [docs/operations.md](operations.md).

These three additions plus the Category 3 gaps would move Bodhyanga from "covers AIGP topics in passing" to "exemplary AIGP reference implementation with documented controls."

---

## Cross-reference index

For AIGP candidates wanting to find Bodhyanga's coverage of a specific topic, here's the index by AIGP body-of-knowledge code:

| AIGP code | Topic | Bodhyanga primary reference |
|---|---|---|
| I.A | What AI is and why it needs governance | [README § Research disclaimer](../README.md#research-disclaimer-read-first), [docs/architecture.md](architecture.md) |
| I.B | Organisational expectations | [CONTRIBUTING.md](../CONTRIBUTING.md) |
| I.C | Lifecycle policies | [docs/governance.md](governance.md), [docs/operations.md](operations.md), [ROADMAP.md](../ROADMAP.md) |
| II.A | Privacy laws | [docs/compliance.md § HIPAA Privacy Rule](compliance.md#hipaa-privacy-rule) |
| II.A | GDPR | [docs/compliance.md § GDPR](compliance.md#gdpr-and-international-privacy) |
| II.B | Other existing laws | [docs/compliance.md § Regulatory pathway](../README.md#regulatory-pathway) |
| II.C | AI-specific laws (EU AI Act, etc.) | This document + [docs/compliance.md](compliance.md) |
| II.D | Standards + tools | [docs/compliance.md § HITRUST / SOC 2 / ISO](compliance.md#hitrust-csf-soc-2-iso-27001--27799) |
| III.A | Designing + building | [docs/architecture.md § Design rules](architecture.md#design-rules-from-mara), [CONTRIBUTING.md § Adding an agent](../CONTRIBUTING.md#adding-an-agent) |
| III.B | Training/test data | [docs/evaluation.md](evaluation.md), `pkg/eval/redteam.go`, `data/cases/` |
| III.C | Release + monitor + maintain | [docs/operations.md](operations.md), [ROADMAP.md](../ROADMAP.md) |
| IV.A | Deployment risk evaluation | [docs/deployment.md](deployment.md), [bodhyanga-linkedin/03-postgres-rls-hipaa.md](../bodhyanga-linkedin/03-postgres-rls-hipaa.md) |
| IV.B | Assessment activities | This document § Practice scenarios, [docs/evaluation.md](evaluation.md) |
| IV.C | Govern deployment + use | [docs/architecture.md § HITL](architecture.md#human-in-the-loop-pattern), [docs/governance.md § Audit events](governance.md#audit-events) |

---

## Closing — Bodhyanga as AIGP curriculum

The AIGP credential covers AI governance as a discipline. Bodhyanga is one expression of that discipline in code. Reading the AIGP body of knowledge alongside Bodhyanga's docs and source gives:

- **Concrete examples** of every domain-III topic ("how would you actually implement minimum necessary in code?" → look at audit field selection)
- **Worked patterns** for the architectural questions AIGP raises ("what does human oversight architecture look like?" → look at the HITL gate)
- **A failure catalogue** in `pkg/eval/redteam.go` — adversarial cases that test the governance machinery itself
- **A path from research to production** — ROADMAP.md enumerates the work from "research demo" to "BAA-eligible deployment"

Bodhyanga isn't AIGP study material. Bodhyanga is a place to **practice** what AIGP teaches.

---

*The IAPP and AIGP designation are property of the International Association of Privacy Professionals. This document is not affiliated with or endorsed by the IAPP. AIGP candidates should use the IAPP's official study materials.*
