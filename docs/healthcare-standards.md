# Bodh — Healthcare standards & interoperability

This document is the deep reference for which healthcare interoperability standards Bodh aligns with, what is implemented today, and what is stubbed for future work. The [main README](../README.md) keeps a short summary and links here. **Use this as the integration checklist when you connect Bodh to a real EHR, payer, or device fleet.**

Real-world clinical software must speak the lingua franca of healthcare IT. Each section below names the standard, why it matters for Bodh, and the current implementation status.

---

## FHIR R4

**Standard:** HL7 FHIR R4 — RESTful healthcare API; resource-based JSON/XML; widely adopted by Epic, Cerner/Oracle Health, athenahealth, Allscripts, Meditech, NextGen, eClinicalWorks, etc.

**Spec:** https://www.hl7.org/fhir/R4/

**Implemented in Bodh** ([pkg/fhir/fhir.go](../pkg/fhir/fhir.go)):

| FHIR resource | Bodh mapping | Notes |
|---------------|--------------|-------|
| `Patient` | `PatientToRecord` → `medical.PatientRecord` | gender, birthDate → age |
| `Observation` | `ObservationToRPM` → `medical.RPMObservation` | code text + LOINC fallback (8480-6 SBP, 29463-7 weight, 59408-5/2708-6 SpO₂, 8867-4 HR, 4548-4 HbA1c, 2339-0 glucose) |
| `Encounter` | `EncounterToADT` → `medical.ADTEvent` | status=`finished` → discharge; `in-progress`/`arrived` → admit |
| `Condition` | `ConditionCode` → patient condition code | SNOMED-preferred, ICD fallback, text last |
| `CarePlan` (export) | `CarePlanToObservations` | goals exported as Observations |

**HTTP endpoints** ([cmd/care/main.go](../cmd/care/main.go)):

| Method | Path | Body | Effect |
|--------|------|------|--------|
| `POST` | `/fhir/Patient` | FHIR R4 Patient | upsert into registry |
| `POST` | `/fhir/Encounter` | FHIR R4 Encounter | publish ADT event |
| `POST` | `/fhir/Condition` | FHIR R4 Condition | append condition code |

**Planned FHIR work:**
- Full Bundle / transaction support
- Search params (`Patient?identifier=...`, `Observation?patient=...&code=...&date=gt...`)
- Capability statement (`/metadata`)
- US Core profiles (USCDI v3+)
- `MedicationRequest`, `MedicationStatement`, `AllergyIntolerance`, `Immunization`, `DocumentReference`
- Subscription / Subscription R5 backports (push notifications)
- FHIR Questionnaire / QuestionnaireResponse for history-taking

## HL7 v2

**Standard:** HL7 v2.x — pipe-delimited messages, the workhorse of hospital interoperability for 30+ years. Still ubiquitous for ADT, lab orders/results, scheduling, charges.

**Common message types relevant to Bodh:**

| Trigger | Description | Bodh consumer |
|---------|-------------|---------------|
| `ADT^A01` | Admit / Visit notification | `readmission_tracker` |
| `ADT^A03` | Discharge / End visit | `readmission_tracker` → triggers `tcm_coordinator` |
| `ADT^A02`/`A06`/`A07` | Transfer | `readmission_tracker` |
| `ORM^O01` | Order message (lab/imaging) | `test_planner` (future) |
| `ORU^R01` | Observation result (labs, vitals) | `rpm_monitor` (future) |
| `SIU^S12` | Scheduling — appointment book | `pre_visit_prep` (future) |
| `DFT^P03` | Detailed financial transaction (charges) | `pkg/eval` (future) |
| `MDM^T02` | Medical document management | future doc agent |

**Status in Bodh:** not yet implemented. A `pkg/hl7v2` parser is a planned addition. Recommended Go libraries to evaluate: open-source HL7 parsers, or a thin custom decoder for the segments above.

**Architectural note:** every HL7 v2 message converts into a single `protocol.Message` on the bus. The bridge is a stateless adapter — no clinical logic.

## SMART on FHIR / OAuth 2.0 / OIDC

**Standard:** SMART App Launch Framework — OAuth 2.0 + OpenID Connect profile that lets third-party apps launch securely inside (or alongside) an EHR.

**Spec:** https://hl7.org/fhir/smart-app-launch/

**Why it matters for Bodh:**
- Lets a clinician click a "Bodh" button inside Epic / Cerner and have Bodh open with the active patient already loaded.
- Provides the standard mechanism for **scope-based authorization**: `patient/Observation.read`, `user/Patient.read`, `system/*.read`.

**Status:** not yet implemented. A `pkg/auth/smart` package is planned. The HTTP API (`cmd/care`) currently has **no authentication** — it is a research demo.

**Production checklist (for when this is added):**

| Requirement | Notes |
|-------------|-------|
| Standalone launch | `iss=<fhirBase>&launch=<token>` flow |
| EHR launch | `iss`, `aud=<clientID>`, `launch` parameter |
| Confidential vs public client | confidential = backend (Bodh server); public = browser app |
| `code` + PKCE | always require PKCE (S256) |
| ID Token (`fhirUser`) | identifies clinician for audit |
| Backend services | `system/*` scopes — needed for batch panel runs |
| Token endpoint authentication | private_key_jwt (RS384) for backend services |
| Scope mapping | each Bodh capability maps to a SMART scope |
| Token rotation | short access tokens + refresh; observe `exp` and `iat` |
| Audit | log every token, every resource read, every write |

## FHIR Bulk Data API

**Standard:** FHIR Bulk Data Access — async export of `Patient`, `Group`, `System` level data as NDJSON.

**Spec:** https://hl7.org/fhir/uv/bulkdata/

**Why it matters for Bodh:** the natural way to populate the `PatientRegistry` from an EHR for panel-management workflows. Pull a payer's attributed-lives roster once per day; reconcile.

**Endpoints to implement:** `$export`, `$export-poll-status`, output NDJSON URL list.

**Status:** planned. The CMS Bulk Data implementation guide (BCDA / DPC) is a useful reference if Bodh is used in a value-based-care setting.

## SMART Health Cards / SMART Health Links

**Standards:** SMART Health Cards (signed JWS-based health record fragments — VC-style) and SMART Health Links (encrypted, shareable manifest of health resources).

**Why it matters:** patient-mediated data sharing without account creation; useful for engagement workflows.

**Status:** not implemented. The engagement agent currently sends only plaintext SMS/email.

## Clinical terminologies

Bodh treats terminologies as opaque code strings today. Production systems should validate codes against authoritative tables and map between systems via UMLS / NLM crosswalks.

| Terminology | What it codes | Typical use in Bodh |
|-------------|---------------|---------------------|
| **SNOMED CT** | Clinical findings, disorders, procedures | `Condition.code`, hypotheses, working diagnosis |
| **LOINC** | Labs and clinical observations | `Observation.code` (already used: 8480-6, 29463-7, 4548-4, …) |
| **ICD-10-CM** | Diagnoses (US) | `Encounter.reasonCode`, claims |
| **ICD-10-PCS** | Inpatient procedures (US) | Procedure history |
| **CPT** | Outpatient procedures (US) | Test catalog, billing codes (99490 CCM, 99453/99454/99457 RPM, 99495/99496 TCM) |
| **HCPCS** | Procedures + supplies (US) | Medicare claims |
| **RxNorm** | Normalized medication names | `MedicationEntry.Name` |
| **NDC** | National Drug Code | Specific product / package |
| **CVX** | Vaccine codes | Immunization records |
| **UCUM** | Units of measure | `Quantity.unit` (mmHg, mg/dL, %, lb, etc.) |
| **NUCC** | Provider taxonomy | Clinician role identifiers |

**Important CPT codes for Bodh service revenue mapping (illustrative):**

| Service | CPT |
|---------|-----|
| Chronic Care Management (CCM) | 99490, 99491, 99437, 99439 |
| Principal Care Management (PCM) | 99424–99427 |
| Remote Physiologic Monitoring (RPM) | 99453 (setup), 99454 (device), 99457/99458 (treatment) |
| Remote Therapeutic Monitoring (RTM) | 98975–98981 |
| Transitional Care Management (TCM) | 99495 (mod complexity), 99496 (high complexity) |
| Behavioral Health Integration | 99492–99494, 99484 |

**Status:** Bodh stores codes as strings; planned `pkg/terminology` for validation and crosswalks.

## DICOM (imaging)

**Standard:** DICOM (Digital Imaging and Communications in Medicine) — modality data, study/series/instance hierarchy, transfer syntaxes, networked services (C-FIND, C-STORE, C-MOVE).

**Spec:** https://www.dicomstandard.org/

**Why it matters for Bodh:** when `test_planner` orders imaging, in a real deployment the result is a DICOM SR (Structured Report) or a JPEG/PNG rendered on a viewer. Today the diagnostician reads a free-text result string. A future imaging-specialist agent would consume DICOM SRs.

**Status:** not implemented. DICOMweb (QIDO-RS / WADO-RS / STOW-RS) is the modern REST-friendly path.

## X12 EDI (eligibility, claims, ERA)

**Standard:** ASC X12 — HIPAA-mandated transactions for healthcare administrative data.

| Transaction | What it is | When Bodh would use it |
|-------------|------------|------------------------|
| 270 / 271 | Eligibility inquiry / response | Verify patient coverage during inbox or visit prep |
| 837 (P/I/D) | Claim submission (professional / institutional / dental) | Bill CCM/RPM/TCM CPT codes |
| 835 | Electronic Remittance Advice (payment posting) | Reconciliation |
| 276 / 277 | Claim status inquiry / response | Follow-up |
| 278 | Services review / authorization | Prior-auth integration |
| 999 | Functional acknowledgment | Operational feedback |

**Status:** not implemented. `prior_auth` agent currently uses an in-memory rule, not an X12 278 transaction.

## NCPDP SCRIPT (e-prescribing)

**Standard:** NCPDP SCRIPT — the e-prescribing standard for transmitting prescriptions between prescribers and pharmacies (used by Surescripts in the US).

**Relevant transactions:** NewRx, RefillRequest, RefillResponse, RxRenewalRequest, CancelRx, DrugUtilizationReview.

**Bodh status:** the `refill_manager` is a stub. Production should integrate with a Surescripts gateway (Surescripts is currently the dominant US e-prescribing network).

## Direct Project (secure clinician messaging)

**Standard:** Direct Project — S/MIME-encrypted SMTP between accredited Direct addresses (`@direct.example.com`). Used for secure clinical messaging and document exchange (CCDAs, referrals).

**Bodh status:** not implemented. A future TCM workflow can ship the discharge summary as a Direct message to the primary care provider.

## CDA / C-CDA documents

**Standard:** Clinical Document Architecture (HL7) — XML clinical documents. **Consolidated CDA (C-CDA)** is the ONC-mandated US flavor with structured templates: Continuity of Care Document (CCD), Discharge Summary, Referral Note, Progress Note, etc.

**Bodh use:** the `tcm_coordinator` produces a `DischargeSummary` struct today. To be ingestable by other EHRs without FHIR support, that struct would be rendered into a C-CDA Discharge Summary document.

**Status:** planned `pkg/cda` for C-CDA export.
