# Bodhyanga — Architecture diagrams

This file is the visual index of the Bodhyanga platform. It is the deep companion to the [main README](../README.md), which keeps the two canonical PNGs and a short summary. The 25 mermaid diagrams below stay in sync with the source tree as agents and packages evolve.

Start with the two PNGs referenced in the README (`Architecture.png` and `architectureflow.png`), then drill into the mermaid views below, grouped A–F by topic.

> **Legend (mermaid sections):** rounded boxes = agents or services · cylinders = data stores · dashed lines = optional / async · solid lines = synchronous calls or message hops · `human_review` gate is always shown explicitly.

---

## 0. Canonical platform diagrams (start here)

### Platform architecture — [`Architecture.png`](../Architecture.png)

The Microsoft-style **Multi-Agent Reference Architecture (MARA)** mapping — user, application, orchestrator, classifier, registry, knowledge/storage, supervisor/worker agents, MCP, observability, evaluation.

<p align="center">
  <img src="../Architecture.png" alt="Bodhyanga platform architecture aligned with Microsoft MARA" width="820"/>
</p>

**How each box maps to the Go code:**

| Area on diagram | Bodhyanga implementation | Go path |
|-----------------|---------------------|---------|
| User / User application | Entry that publishes the first `protocol.Message` | `cmd/demo`, `cmd/caredemo`, `cmd/care` (HTTP), `cmd/bodhyangactl` (CLI) |
| Orchestrator | Subscribes agents, runs governance, calls `HandleMessage`, republishes outputs | `pkg/orchestration` |
| Classifier | Route intent (chest pain vs rash, refill vs urgent) | `inbox_triage` agent · future `pkg/classifier` |
| Agent registry | Lists agents, resolves capabilities | `pkg/registry` |
| Knowledge layer | Guidelines, differentials, RAG | Planned: DB + vector index |
| Storage layer | Case + roster + queue + reviews + audit | `pkg/memory`, `pkg/persistence` (JSON file), `pkg/audit` (append-only) · planned PostgreSQL |
| Supervisor agent | Decompose diagnosis | `agents/supervisor.go` |
| Workers | Intake, questioner, tests, cost, dx, verify, CDM/TCM/panel/VO/RPM/engagement/readmission/human-review | `agents/*.go` (17 agents + HITL gate) |
| Integration / MCP / tools | Labs, imaging, LLM APIs, FHIR, HL7 v2 | `pkg/fhir` (today) · planned MCP tools |
| Governance | Allow/deny boundary; PHI redaction | `pkg/governance`, `pkg/phi` |
| Observability | Logs, traces, clocks, audit | `pkg/observability`, `pkg/audit` |
| Evaluation | Accuracy vs gold, cost metrics | `pkg/eval` + `cmd/bench` |

### Clinical flow — [`architectureflow.png`](../architectureflow.png)

End-to-end **sequential diagnosis** for one case: presentation → RPM monitor → history → tests (with cost check + rework loop) → diagnosis → reasoning verification → human sign-off → final report. The Bodhyanga expression of [MAI-DxO](https://microsoft.ai/news/the-path-to-medical-superintelligence/)'s virtual physician panel.

<p align="center">
  <img src="../architectureflow.png" alt="Sequential diagnosis flow — virtual physician panel" width="820"/>
</p>

**Why images sometimes do not show on GitHub**

| Cause | Fix |
|-------|-----|
| WebP in README | GitHub support is inconsistent; this repo displays the **PNG** export |
| Very large PNG | The README uses HTML `width` so the PNG scales in the browser |
| File not on `main` | Images must be committed on the branch you view |
| Broken relative path | Use `./Architecture.png` from README root |

> The mermaid sections below are the live, in-code companion to these two PNGs — they stay in sync with the source tree as agents are added.

---

## A. Bird's-eye views

### 1. System context (level 1) — Bodhyanga in its world

The actors and external systems Bodhyanga exchanges data with.

```mermaid
flowchart TB
    Clinician[Clinician / Nurse]
    Patient[Patient]
    EHR[Client EHR<br/>Epic · Cerner · athena]
    Device[RPM Device<br/>BP cuff · scale · pulse-ox]
    ADT[Hospital ADT feed<br/>HL7 v2 ADT^A01/A03]
    Payer[Payer / CMS<br/>X12 837 / 835 / 270 / 271]
    Pharma[Pharmacy network<br/>NCPDP SCRIPT]
    SMS[SMS / email gateway<br/>Twilio · SendGrid]
    Bodhyanga((Bodhyanga Platform))

    Clinician -->|web app, SMART launch| Bodhyanga
    Patient -->|SMS reply| SMS
    SMS -->|webhook| Bodhyanga
    EHR -->|FHIR R4 / HL7 v2| Bodhyanga
    Device -->|vendor webhook| Bodhyanga
    ADT -->|HL7 v2 stream| Bodhyanga
    Bodhyanga -->|outreach SMS / email| SMS
    SMS --> Patient
    Bodhyanga -->|FHIR write-back| EHR
    Bodhyanga -->|837 claim| Payer
    Payer -->|835 ERA| Bodhyanga
    Bodhyanga -->|NewRx / RefillRx| Pharma
```

### 2. MARA platform — building blocks → Go packages

Where each MARA concept lives in the codebase.

```mermaid
flowchart TB
    Entry["cmd/demo · cmd/caredemo · cmd/care"]
    Bus["pkg/comm<br/>InMemoryBus"]
    Reg["pkg/registry<br/>InMemoryRegistry"]
    Orch["pkg/orchestration<br/>Orchestrator"]
    Gov["pkg/governance<br/>CompositePolicy"]
    Agents["agents/*.go<br/>17 workers + human_review"]
    Domain["pkg/medical<br/>CaseState · CarePlan · Panel · ADT · RPM · Review"]
    Tasks["pkg/tasks<br/>NurseTask queue"]
    HitlP["pkg/hitl<br/>Review queue"]
    Eng["pkg/engagement<br/>Notifier"]
    Mem["pkg/memory<br/>KV"]
    Obs["pkg/observability<br/>Logger + Clock"]
    Eval["pkg/eval<br/>InteractionRecord"]
    Anal["pkg/analytics<br/>cohort metrics"]
    Pers["pkg/persistence<br/>JSON file store"]
    Fhir["pkg/fhir<br/>R4 adapter"]

    Entry -->|publish| Bus
    Bus --> Orch
    Reg -.lookup.-> Orch
    Orch -->|policy gate| Gov
    Gov -->|allow| Orch
    Orch -->|HandleMessage| Agents
    Agents -->|read/write| Domain
    Agents --> Tasks
    Agents --> HitlP
    Agents --> Eng
    Agents -->|emit msgs| Bus
    Domain -.-> Fhir
    Eval -.-> Anal
    Eval --> Pers
    Mem --> Pers
    Orch -.-> Obs
    Agents -.-> Obs
```

### 3. Logical layers — request through the stack

The same architecture from the message-handling perspective, top-to-bottom.

```mermaid
flowchart TB
    Ingress["Ingress / API Layer<br/>cmd/care HTTP · CLI · FHIR endpoints"]
    GovL["Governance Layer<br/>MaxContentLength · RequireCaseID · MaxDiagnosticCost · ResearchDisclaimer"]
    OrchL["Orchestration & Routing<br/>Orchestrator · Bus · Registry"]
    AgentL["Agent Layer<br/>Diagnostic panel + Care services + HITL gate"]
    DomainL["Domain & State<br/>CaseState · CarePlan · Panel · ADT · RPM · Tasks · Notifier · Reviews"]
    Persist["Persistence & Analytics<br/>JSON store · eval records · cohort metrics"]
    ObsL["Observability<br/>Logger · Clock · planned OpenTelemetry"]

    Ingress -->|publish Message| GovL
    GovL -->|allow| OrchL
    GovL -.->|deny + reason| ObsL
    OrchL --> AgentL
    AgentL <--> DomainL
    DomainL --> Persist
    Persist -.-> ObsL
    AgentL -.-> ObsL
    OrchL -.-> ObsL
```

---

## B. Control plane

### 4. Message lifecycle — what happens to one Message

Every interaction in Bodhyanga is a `protocol.Message`. This is its lifecycle.

```mermaid
sequenceDiagram
    participant Publisher as Publisher (user / agent)
    participant Bus
    participant Orchestrator
    participant Governance
    participant Agent
    participant Bus2 as Bus (re-publish)

    Publisher->>Bus: Publish(Message)
    Bus->>Orchestrator: deliver to subscribers of msg.To
    Orchestrator->>Governance: Evaluate(msg)
    alt all policies allow
        Governance-->>Orchestrator: Allow
        Orchestrator->>Agent: HandleMessage(ctx, msg, env)
        Agent-->>Orchestrator: []Message
        loop each emitted message
            Orchestrator->>Bus2: Publish
        end
    else any policy denies
        Governance-->>Orchestrator: Deny + Reason
        Orchestrator-->>Publisher: log "denied by policy"<br/>(agent never runs — fail closed)
    end
```

### 5. Governance pipeline — composite policy

How `CompositePolicy` short-circuits on the first deny.

```mermaid
flowchart LR
    Msg([Incoming Message]) --> C[CompositePolicy.Evaluate]
    C --> P1[MaxContentLengthPolicy<br/>len Content ≤ 65536]
    P1 -->|allow| P2[RequireCaseIDPolicy<br/>case_id present for clinical types]
    P2 -->|allow| P3[MaxDiagnosticCostPolicy<br/>spent + cost ≤ max_cost_usd]
    P3 -->|allow| P4[ResearchDisclaimerPolicy<br/>always allows; logs intent]
    P4 -->|allow| Run[HandleMessage runs]
    P1 -->|deny| Deny([Log reason · drop · agent never runs])
    P2 -->|deny| Deny
    P3 -->|deny| Deny
    P4 -->|deny| Deny
```

### 6. Package dependency graph

The Go module's actual import structure (no cycles).

```mermaid
flowchart LR
    cmd_demo[cmd/demo]
    cmd_caredemo[cmd/caredemo]
    cmd_care[cmd/care]
    A[agents]
    PA[pkg/agent]
    PProto[pkg/protocol]
    PReg[pkg/registry]
    PComm[pkg/comm]
    POrch[pkg/orchestration]
    PGov[pkg/governance]
    PMed[pkg/medical]
    PTasks[pkg/tasks]
    PEng[pkg/engagement]
    PHitl[pkg/hitl]
    PFhir[pkg/fhir]
    PAnal[pkg/analytics]
    PEval [pkg/eval]
    PPers[pkg/persistence]
    PMem[pkg/memory]
    PObs[pkg/observability]

    cmd_demo --> A
    cmd_caredemo --> A
    cmd_caredemo --> PHitl
    cmd_care --> A
    cmd_care --> PHitl
    A --> PA
    A --> PMed
    A --> PTasks
    A --> PEng
    A --> PHitl
    A --> PEval
    PA --> PProto
    PComm --> PProto
    POrch --> PReg
    POrch --> PComm
    POrch --> PGov
    POrch --> PA
    POrch --> PObs
    PGov --> PProto
    PReg --> PA
    PHitl --> PMed
    PFhir --> PMed
    PAnal --> PMed
    PAnal --> PEval
    PPers --> PEval
```

---

## C. State machines & domain logic

### 7. HITL review lifecycle — state machine

The human-in-the-loop gate from submission to resume.

```mermaid
stateDiagram-v2
    [*] --> pending : Submit
    pending --> approved : Decide(approve)
    pending --> approved_with_modifications : Decide(approve_with_modifications)
    pending --> rejected : Decide(reject)
    pending --> expired : SLA breach
    approved --> [*] : republish payload to NextTo
    approved_with_modifications --> [*] : republish modified payload
    rejected --> [*] : no further action · logged
    expired --> [*] : no further action · logged
```

### 8. Billing period state machine (CMS programs)

Care-time accumulation per patient per program per month — the unit of revenue.

```mermaid
stateDiagram-v2
    [*] --> accruing : enroll patient (CCM / RPM / TCM / PCM)
    accruing --> accruing : log time / reading-day / call
    accruing --> qualified : CPT criteria met
    accruing --> expired : month-end, criteria unmet
    qualified --> claim_submitted : month-end sweep generates 837
    claim_submitted --> paid : 835 ERA received
    claim_submitted --> denied : payer rejection
    denied --> review : create nurse task
    review --> [*]
    paid --> [*]
    expired --> [*]
```

### 9. Nurse task lifecycle (`pkg/tasks`)

```mermaid
stateDiagram-v2
    [*] --> open : Queue.Create
    open --> in_progress : nurse claims
    open --> cancelled : superseded
    in_progress --> done : completed
    in_progress --> cancelled : aborted
    done --> [*]
    cancelled --> [*]
```

### 10. CarePlan template selection

`medical.BuildCarePlan` is deterministic and reproducible by condition.

```mermaid
flowchart TB
    Req[cdm_request<br/>patient_id + condition + case_id] --> Build[BuildCarePlan]
    Build --> Cond{condition}
    Cond -->|htn| HTN[Goal SBP under 130<br/>Med Lisinopril 10 mg<br/>Monitor SBP daily RPM<br/>Review 30 d]
    Cond -->|dm| DM[Goal A1c under 7<br/>Med Metformin 500 mg BID<br/>Monitor glucose daily RPM<br/>Review 90 d]
    Cond -->|chf| CHF[Goal weight under 185<br/>Med Furosemide 40 mg<br/>Monitor weight + SpO2 daily<br/>Review 14 d]
    Cond -->|ckd| CKD[Goal eGFR over 60<br/>Med Losartan 50 mg<br/>Monitor eGFR monthly lab<br/>Review 90 d]
    Cond -->|copd| COPD[Goal SpO2 over 94<br/>Med Tiotropium 18 mcg<br/>Monitor SpO2 daily RPM<br/>Review 30 d]
    Cond -->|other| Generic[Generic encounter follow-up<br/>Review 30 d]
    HTN --> Emit[Emit care_plan_ready]
    DM --> Emit
    CHF --> Emit
    CKD --> Emit
    COPD --> Emit
    Generic --> Emit
```

### 11. Master Patient Index — matching pipeline

Three-stage matching (deterministic shortcut → blocking → Fellegi-Sunter scoring → threshold). Safety-critical: a wrong merge blends two patients into one chart.

```mermaid
flowchart TB
    Incoming[Incoming record<br/>name + DOB + identifiers] --> Det{Deterministic match<br/>exact MRN within tenant<br/>or SSN + DOB?}
    Det -->|yes| AutoLink[Auto-link · skip scoring]
    Det -->|no| Block[Generate blocking keys<br/>soundex name + dob<br/>first3 name + zip<br/>last4 SSN + dob]
    Block --> Compare[Compare within block<br/>candidates only]
    Compare --> Score[Fellegi-Sunter weighted score<br/>SSN 0.35 · name JW · DOB 0.25 · addr 0.10 · phone 0.10 · sex 0.05]
    Score --> Band{Score band}
    Band -->|≥ 0.92| AutoLink2[Auto-link]
    Band -->|0.55 – 0.92| Manual[Manual review task<br/>data steward decides]
    Band -->|under 0.55| Distinct[Treat as new patient]
    AutoLink --> Done([canonical PATIENT_ID])
    AutoLink2 --> Done
    Manual --> Steward[Steward link / split decision<br/>kept reversible in PATIENT_IDENTITY history]
    Steward --> Done
    Distinct --> Done
```

### 12. Inbox triage decision tree (`inbox_triage`)

Classify → route. Refill and PA decisions cascade through HITL when enabled.

```mermaid
flowchart TB
    Inbox[inbox_message<br/>patient_id · subject · body] --> Classify[classifyInbox<br/>keyword match]
    Classify --> Cat{Category}
    Cat -->|refill / prescription| Refill[refill_request → refill_manager]
    Cat -->|prior auth / authorization| PA[prior_auth_request → prior_auth]
    Cat -->|chest pain · SOB · syncope · bleeding| Urgent[NurseTask priority=urgent · due 2 h]
    Cat -->|else| Normal[NurseTask priority=normal · due 24 h]
    Refill --> RefDec{Med on care plan?}
    RefDec -->|yes| Auto[status approved<br/>refill_response → HITL → SMS]
    RefDec -->|no| Review[NurseTask refill review<br/>status review → HITL → SMS]
    PA --> PADec{Low-risk code<br/>DEXA · CT?}
    PADec -->|yes| Submit[status approved → HITL → SMS]
    PADec -->|no| ReviewPA[NurseTask PA review · 48h<br/>status submitted → HITL → SMS]
```

---

## D. Diagnostic flow

### 13. Sequential diagnosis (MAI-DxO panel) — sequence

The eight-agent virtual physician panel.

```mermaid
sequenceDiagram
    participant User
    participant Bus
    participant I as intake
    participant R as rpm_monitor
    participant S as diagnostic_supervisor
    participant Q as questioner
    participant T as test_planner
    participant C as cost_guardian
    participant D as diagnostician
    participant V as reasoning_verifier
    participant HR as human_review

    User->>Bus: type=presentation, To=intake
    Bus->>I: HandleMessage
    I->>Bus: case_state → rpm_monitor
    Bus->>R: RPM triage
    R->>Bus: case_state → diagnostic_supervisor
    Bus->>S: routeInitial
    S->>Bus: case_state → questioner
    Bus->>Q: history taking
    Q->>Bus: questions_complete → supervisor
    S->>Bus: case_state → test_planner
    T->>Bus: case_state → cost_guardian (with spent_usd)
    C->>Bus: tests_complete → supervisor
    S->>Bus: case_state → diagnostician
    D->>Bus: diagnosis_proposal → reasoning_verifier
    V->>Bus: review_request → human_review (HITL on)
    Note over HR: kind=diagnosis_review<br/>SLA 4 h
    HR->>Bus: resume → supervisor (diagnosis)
    Bus->>S: finalize · log gold-match · score CaseMetrics
```

### 14. Cost guardian — budget check decision

```mermaid
flowchart TB
    In[Incoming case_state<br/>after test_planner] --> Calc[Compute total_cost_usd]
    Calc --> Cmp{total ≤ max_cost_usd?}
    Cmp -->|yes| Log1[Log APPROVE<br/>utilization percent]
    Log1 --> Emit[Emit tests_complete → supervisor]
    Cmp -->|no| Log2[Log DENY<br/>over budget]
    Log2 --> Stop[Return no messages<br/>supervisor handles rework · planned]
```

---

## E. Care-services flows

### 15. CDM (chronic disease management) — with HITL

```mermaid
sequenceDiagram
    participant User
    participant Bus
    participant CDM as cdm_planner
    participant Reg as PatientRegistry
    participant Panel as panel_manager
    participant HR as human_review
    participant Clin as Clinician
    participant Eng as engagement
    participant N as Notifier

    User->>Bus: cdm_request (patient_id, condition, case_id)
    Bus->>CDM: HandleMessage
    CDM->>Reg: Upsert(condition, meds)
    CDM->>CDM: BuildCarePlan(condition)
    CDM->>Bus: care_plan_ready → panel_manager (tracking · direct)
    CDM->>Bus: review_request → human_review (HITL)
    Bus->>Panel: track new plan
    Bus->>HR: queue pending (kind=care_plan_review · SLA 24 h)
    Clin->>HR: POST /reviews/id/decide approve
    HR->>Bus: resume → engagement (care_plan_ready)
    Bus->>Eng: HandleMessage
    Eng->>N: send SMS welcome + plan
```

### 16. TCM (transitional care) — full cascade from ADT

The most involved cascade in Bodhyanga: ADT → risk → discharge review → TCM tasks → welcome review → patient SMS.

```mermaid
sequenceDiagram
    participant ADT as ADT feed (HL7 v2)
    participant Bus
    participant RT as readmission_tracker
    participant Reg as PatientRegistry
    participant HR as human_review
    participant Clin as Clinician
    participant TCM as tcm_coordinator
    participant TQ as tasks.Queue
    participant Eng as engagement
    participant N as Notifier

    ADT->>Bus: adt_event (discharge)
    Bus->>RT: HandleMessage
    RT->>Reg: Upsert(DischargedAt)
    RT->>RT: ScoreReadmissionRisk → band
    RT->>Bus: readmission_risk (broadcast)
    RT->>Bus: review_request → human_review<br/>(discharge_review · priority urgent if high)
    Bus->>HR: queue pending
    Clin->>HR: approve
    HR->>Bus: resume → tcm_coordinator (discharge_event)
    Bus->>TCM: HandleMessage
    TCM->>Reg: ReconcileMedications
    TCM->>TQ: Create 5 nurse tasks (med-rec + day 2/7/14/30)
    TCM->>Bus: review_request → human_review<br/>(tcm_welcome_review · cascade)
    Bus->>HR: queue pending
    Clin->>HR: approve cascade
    HR->>Bus: resume → engagement (tcm_tasks_created)
    Bus->>Eng: HandleMessage
    Eng->>N: send TCM welcome SMS
```

### 17. Panel management — population gap-in-care detection

```mermaid
sequenceDiagram
    participant User
    participant Bus
    participant Panel as panel_manager
    participant Reg as PatientRegistry
    participant HR as human_review
    participant Clin as Clinician
    participant Eng as engagement

    User->>Bus: panel_request (condition=all)
    Bus->>Panel: HandleMessage
    Panel->>Reg: List or FilterByCondition
    Reg-->>Panel: roster
    Panel->>Panel: run DefaultPanelRules<br/>a1c · bp · annual visit · chf weight · tcm followup
    Panel->>Bus: panel_report (broadcast)
    loop each open gap
        Panel->>Bus: review_request → human_review<br/>(outreach_review · priority by severity)
        Bus->>HR: queue pending
    end
    Clin->>HR: approve all (or batch select)
    loop each approval
        HR->>Bus: engagement_request → engagement
        Bus->>Eng: send outreach SMS
    end
```

### 18. RPM (remote patient monitoring) real-time pipeline

```mermaid
flowchart LR
    Device[RPM Device<br/>BP cuff · scale · pulse-ox] --> Vendor[Device-vendor cloud]
    Vendor --> API["POST /rpm/observation<br/>cmd/care"]
    API --> Validate["Range + plausibility check<br/>(diastolic 400 mmHg is bad data)"]
    Validate -->|valid| Store[(Time-series store<br/>Bigtable · TimescaleDB<br/>row-key tenant#patient#-ts)]
    Validate -->|valid| Thresh{Threshold breach?}
    Validate -->|invalid| DLQ[(Dead-letter)]
    Thresh -->|yes| Alert[Publish alert event]
    Thresh -->|no| Done([reading persisted])
    Alert --> Task[tasks.Queue.Create<br/>NurseTask priority urgent]
    Task --> NurseUI[Nurse workstation]
    Vendor -.cadence stops.-> Freshness[Freshness monitor<br/>silent device looks like healthy patient]
    Freshness --> Task
```

### 19. Readmission risk scoring decision

```mermaid
flowchart TB
    ADT[ADT event] --> Type{Type}
    Type -->|admit / transfer| Risk1[Update registry · compute risk · broadcast]
    Type -->|discharge| Update[Update DischargedAt · compute risk · broadcast]
    Update --> Score[ScoreReadmissionRisk<br/>age + comorbidity + recent dx<br/>+ BP + A1c]
    Score --> Band{Band}
    Band -->|low| L[Direct → tcm_coordinator<br/>priority normal]
    Band -->|medium| M[Direct → tcm_coordinator<br/>priority high]
    Band -->|high| H[HITL → human_review<br/>priority urgent · SLA 60 min]
    H --> ClinDec{Clinician}
    ClinDec -->|approve| TCM[tcm_coordinator runs]
    ClinDec -->|reject| Stop([no TCM cascade · logged])
    L --> TCM
    M --> TCM
```

### 20. End-to-end patient journey — one CHF patient over 30 days

A composite scenario showing how multiple flows interact for a real patient.

```mermaid
sequenceDiagram
    autonumber
    participant Pt as Patient
    participant Dev as RPM scale
    participant ADT
    participant Bodhyanga
    participant Clin as Clinician
    participant Eng as engagement

    Note over Pt,Eng: Day 0 — enrollment
    Bodhyanga->>Bodhyanga: register PatientRecord(chf)
    Bodhyanga->>Bodhyanga: BuildCarePlan(chf)
    Bodhyanga->>Clin: review_request (care_plan_review)
    Clin->>Bodhyanga: approve
    Bodhyanga->>Eng: SMS welcome + plan
    Eng->>Pt: "Your CHF plan is ready"

    Note over Pt,Eng: Day 7 — RPM cadence
    Dev->>Bodhyanga: weight=185 lb
    Bodhyanga->>Bodhyanga: store · no threshold breach

    Note over Pt,Eng: Day 14 — deterioration
    Dev->>Bodhyanga: weight=192 lb (+4 lb / 24h)
    Bodhyanga->>Bodhyanga: threshold breach · publish alert
    Bodhyanga->>Clin: NurseTask priority=urgent
    Clin->>Pt: phone call · adjust diuretic

    Note over Pt,Eng: Day 20 — hospital admit
    ADT->>Bodhyanga: adt_event admit
    Bodhyanga->>Bodhyanga: ScoreReadmissionRisk · band=high

    Note over Pt,Eng: Day 23 — discharge
    ADT->>Bodhyanga: adt_event discharge
    Bodhyanga->>Clin: review_request (discharge_review · urgent)
    Clin->>Bodhyanga: approve
    Bodhyanga->>Bodhyanga: tcm_coordinator creates 5 tasks
    Bodhyanga->>Clin: review_request (tcm_welcome_review)
    Clin->>Bodhyanga: approve cascade
    Bodhyanga->>Eng: TCM welcome SMS

    Note over Pt,Eng: Day 30 — billing
    Bodhyanga->>Bodhyanga: BillablePeriod qualifies for CCM (CPT 99490)
    Bodhyanga->>Bodhyanga: generate 837 claim
```

---

## F. Deployment & infrastructure

### 21. GCP deployment topology

Production reference architecture on Google Cloud (BAA, Assured Workloads, VPC Service Controls).

```mermaid
flowchart TB
    subgraph External[External]
        EHR[Client EHRs]
        Device[RPM Devices]
        Nurse[Nurses · browser]
    end

    subgraph GCP[Google Cloud · BAA · VPC Service Controls perimeter]
        subgraph Edge[Edge]
            CA[Cloud Armor WAF]
            LB[Global HTTPS LB]
            APIGW[Apigee API Gateway]
        end
        subgraph Ingestion[Ingestion]
            CHC[Cloud Healthcare API<br/>FHIR + HL7v2 stores]
            PubSub[Pub/Sub]
            Dataflow[Dataflow streaming]
        end
        subgraph Services[Services · stateless]
            GKE[GKE Bodhyanga microservices]
            CR[Cloud Run RPM ingest]
        end
        subgraph Data[Data]
            AlloyDB[(AlloyDB Postgres<br/>operational)]
            Bigtable[(Bigtable<br/>RPM time-series)]
            CS[(Cloud Storage<br/>documents)]
            BQ[(BigQuery<br/>analytics)]
        end
        subgraph Security[Security]
            KMS[Cloud KMS]
            SM[Secret Manager]
            IAM[Cloud IAM + IAP]
        end
    end

    Device -->|webhook| APIGW
    EHR -->|FHIR / HL7| CHC
    Nurse -->|HTTPS + OIDC| CA
    CA --> LB
    LB --> GKE
    APIGW --> PubSub
    APIGW --> CR
    CR --> PubSub
    CHC --> PubSub
    PubSub --> Dataflow
    Dataflow --> AlloyDB
    Dataflow --> Bigtable
    GKE --> AlloyDB
    GKE --> Bigtable
    GKE --> CS
    AlloyDB -.Datastream CDC.-> BQ
    GKE -.-> KMS
    GKE -.-> SM
    GKE -.-> IAM
```

### 22. AWS deployment topology

Parallel reference on AWS (BAA-eligible services, multi-AZ).

```mermaid
flowchart TB
    subgraph ExternalA[External]
        EHRa[Client EHRs]
        DeviceA[RPM Devices]
        NurseA[Nurses · browser]
    end

    subgraph AWS[AWS · BAA · VPC private subnets]
        subgraph EdgeA[Edge]
            WAF[AWS WAF]
            ALB[Application LB]
            APIGW2[API Gateway]
        end
        subgraph IngestA[Ingestion]
            EventBridge[EventBridge]
            Kinesis[Kinesis Firehose]
            Lambda[Lambda parsers]
        end
        subgraph ComputeA[Compute]
            EKS[EKS / ECS Fargate<br/>Bodhyanga microservices]
        end
        subgraph DataA[Data]
            RDS[(RDS Postgres<br/>operational)]
            Timestream[(Timestream<br/>RPM time-series)]
            S3[(S3 KMS-encrypted<br/>documents)]
            Redshift[(Redshift<br/>analytics)]
        end
        KMSA[KMS CMK]
        SMA[Secrets Manager]
        IAMA[IAM + IAM Roles for SA]
        CT[CloudTrail<br/>separate audit account]
    end

    DeviceA --> APIGW2
    EHRa --> APIGW2
    NurseA --> WAF
    WAF --> ALB
    ALB --> EKS
    APIGW2 --> EventBridge
    EventBridge --> Kinesis
    Kinesis --> Lambda
    Lambda --> RDS
    Lambda --> Timestream
    EKS --> RDS
    EKS --> Timestream
    EKS --> S3
    RDS -.DMS CDC.-> Redshift
    EKS -.-> KMSA
    EKS -.-> SMA
    EKS -.-> IAMA
    EKS -.->|all API calls| CT
```

### 23. Multi-tenant isolation

How `tenant_id` enforces blast-radius boundaries across the stack.

```mermaid
flowchart TB
    Req[Client request<br/>tenant claim in JWT] --> GW[API Gateway · validate JWT]
    GW --> Ctx[Set tenant_id in request context]
    Ctx --> AppA[App tier<br/>tenant filter set on every query]
    AppA --> RLS{Row-level security}
    RLS -->|small/medium tenant| Shared[(Shared Postgres<br/>RLS predicate<br/>tenant_id = current_tenant)]
    RLS -->|very large tenant| Dedicated[(Dedicated DB instance<br/>cell isolation)]
    AppA --> BusM[Per-tenant Pub/Sub subscriptions<br/>or ordering keys]
    BusM --> Workers[Worker pool · tenant-tagged]
    Workers --> Quota[Per-tenant Dataflow quota<br/>noisy neighbor cap]
```

### 24. HIPAA security architecture (defense-in-depth)

Where each Security-Rule technical safeguard lives.

```mermaid
flowchart TB
    User[Clinician or patient request] --> IdP[OIDC IdP<br/>SMART on FHIR · OIDC]
    IdP -->|access token + id token| GW[API Gateway<br/>JWT validation · WAF · rate limit · IP allowlist]
    GW -->|mTLS| API[Bodhyanga API · 8088]
    API --> Scope[Scope check<br/>RequireScopePolicy]
    Scope --> Tenant[Tenant check<br/>RequireTenantPolicy]
    Tenant --> Authz[Centralized chokepoint<br/>per-resource RBAC]
    Authz -->|allow| Read[Read PHI]
    Authz -->|allow| Write[Write PHI]
    Read --> AuditL[(Audit log · immutable<br/>separate locked account<br/>WORM 6+ years)]
    Write --> AuditL
    Read --> DB[(DB · TLS in-transit<br/>KMS CMK at-rest<br/>column-level encrypt for PHI)]
    Write --> DB
    DB -.replicated.-> Backup[(Cross-region backup · KMS)]
    Authz -->|deny| AuditL

    style AuditL fill:#fdd,stroke:#900
    style DB fill:#dfd,stroke:#070
```

### 25. CI/CD + delivery pipeline

What runs before code reaches production.

```mermaid
flowchart LR
    Dev[Developer push] --> CI{CI pipeline<br/>Cloud Build · GitHub Actions}
    CI --> UT[go test]
    CI --> Vet[go vet · staticcheck · gosec]
    CI --> Vuln[govulncheck]
    CI --> Pol[Policy-as-code<br/>fail if unencrypted bucket<br/>or public DB]
    CI --> Scan[Container CVE scan]
    UT --> Build[Build container]
    Vet --> Build
    Vuln --> Build
    Pol --> Build
    Scan --> Build
    Build --> Reg[Container registry · signed]
    Reg --> Canary[Canary 5 percent traffic]
    Canary --> SLO{SLO healthy 30 min?}
    SLO -->|yes| Roll[Progressive rollout 100 percent]
    SLO -->|no| RB[Auto-rollback]
    Roll --> Prod[Production]
    RB --> Dev
```

---

## G. AI subsystem

The LLM-specific view of Bodhyanga. Every diagram here corresponds to code in `pkg/llm`, `pkg/embeddings`, `pkg/knowledge`, `pkg/safety`, `pkg/observability`, `pkg/eval`, and the LLM-backed agents under `agents/`. See [ai-features.md](ai-features.md) for the prose walk-through.

### 26. LLM provider abstraction — Client interface + adapters + Mock

`pkg/llm.Client` is the single contract every chat adapter implements. Agents take `Client` by interface so swapping providers is a flag, not a refactor.

```mermaid
flowchart TB
    subgraph Agents[LLM-backed agents]
        D[LLMDiagnostician]
        Q[LLMQuestioner]
        I[LLMInboxTriage]
        V[LLMReasoningVerifier]
        J[eval.Judge]
        Vis[LLMDiagnosticianVision]
        Cas[CascadeDiagnostician]
        Deb[DebateDiagnostician]
        Ref[ReflexionDiagnostician]
    end
    Iface(["llm.Client interface<br/>Provider() · Model() · Complete()"])
    Anth["anthropicClient<br/>NewAnthropic(key, model)"]
    OAI["openaiClient<br/>NewOpenAI(key, model)"]
    Oll["ollamaClient<br/>NewOllama(host, model)"]
    Mock["llm.Mock<br/>(tests)"]
    AnthAPI[("api.anthropic.com<br/>/v1/messages")]
    OAIAPI[("api.openai.com<br/>/v1/chat/completions")]
    OllAPI[("localhost:11434<br/>/api/chat")]
    Trace["pkg/observability<br/>LLMTrace"]

    D --> Iface
    Q --> Iface
    I --> Iface
    V --> Iface
    J --> Iface
    Vis --> D
    Cas --> Iface
    Deb --> Iface
    Ref --> Iface
    Iface --> Anth
    Iface --> OAI
    Iface --> Oll
    Iface --> Mock
    Anth -->|x-api-key + anthropic-version| AnthAPI
    OAI -->|Bearer + JSON Schema strict| OAIAPI
    Oll -->|stream=false + format=schema| OllAPI
    Iface -.records.-> Trace
```

### 27. LLM diagnostician with rule-based fallback

The full happy- and sad-path sequence — the LLM call is wrapped in a timeout, and any failure (transport, parse, low confidence) routes through the rule-based fallback so the case always finalises.

```mermaid
sequenceDiagram
    participant Sup as diagnostic_supervisor
    participant LLMD as LLMDiagnostician
    participant Cli as llm.Client
    participant API as Provider API
    participant Rule as DiagnosticianAgent (fallback)
    participant Obs as observability.LLMTrace
    participant Ver as reasoning_verifier

    Sup->>LLMD: case_state
    LLMD->>LLMD: DecodeState · buildRequest<br/>(system cached, JSONSchema)
    LLMD->>Cli: Complete(req) (ctx WithTimeout)
    Cli->>API: HTTP POST
    alt 2xx
        API-->>Cli: response payload
        Cli-->>LLMD: Response{Content, tokens, stop}
        LLMD->>Obs: RecordLLMCall(LLMTrace)
        LLMD->>LLMD: Unmarshal → DiagnosisProposal<br/>check Confidence ≥ MinConfidence (0.3)
        alt confidence ok
            LLMD->>Ver: diagnosis_proposal (LLM output)
        else low confidence
            LLMD->>Rule: HandleMessage (fallback)
            Rule-->>Ver: diagnosis_proposal (rule)
        end
    else error / non-2xx / timeout
        Cli-->>LLMD: err
        LLMD->>Obs: RecordLLMCall(LLMTrace with Error)
        LLMD->>Rule: HandleMessage (fallback)
        Rule-->>Ver: diagnosis_proposal (rule)
    end
```

### 28. RAG retrieval pipeline

When `BODHYANGA_KNOWLEDGE_CORPUS` is set, the LLM diagnostician prepends an "Evidence:" block to the user message and propagates the retrieved Citations onto the final proposal.

```mermaid
flowchart TB
    Case[CaseState] --> Build[buildRetrievalQuery<br/>presentation + condition +<br/>test results + hypotheses]
    Build --> Q{query empty?}
    Q -->|yes| Skip[Skip retrieval]
    Q -->|no| Emb[embeddings.Embed(query)]
    Emb --> Search[knowledge.Store.Search k=4]
    Search --> Top[Top-K Citations<br/>by cosine (or RRF in hybrid)]
    Top --> Inject[Prepend 'Evidence:' block<br/>on user message]
    Inject --> Sys[Use RAG system prompt +<br/>extended JSON schema with citations]
    Sys --> Call[llm.Client.Complete]
    Call --> Decode[Unmarshal DiagnosisProposal]
    Decode --> Recon{Model returned<br/>citations?}
    Recon -->|yes| Keep[Keep model's citations]
    Recon -->|no| Prop[Propagate retrieved citations<br/>for provenance]
    Skip --> NoRAG[Run text-only path]
    Keep --> Out[Emit diagnosis_proposal<br/>with Citations]
    Prop --> Out
    NoRAG --> Out
```

### 29. Vision diagnostician

Wraps the LLM diagnostician — same prompt, same RAG, same fallback. The only addition is reading the image file and attaching it to the user message.

```mermaid
sequenceDiagram
    participant Sup as diagnostic_supervisor
    participant Vis as LLMDiagnosticianVision
    participant Base as LLMDiagnostician (Base)
    participant FS as Filesystem
    participant Cli as llm.Client (vision model)
    participant API as Provider API
    participant Ver as reasoning_verifier

    Sup->>Vis: case_state (msg.Metadata.image_path)
    alt no image_path
        Vis->>Base: HandleMessage (text-only)
        Base-->>Ver: diagnosis_proposal
    else image_path present
        Vis->>FS: os.ReadFile(path)
        alt read failed / unsupported MIME
            Vis->>Base: HandleMessage (fall through)
            Base-->>Ver: diagnosis_proposal (text-only)
        else PNG / JPEG ok
            Vis->>Base: buildRequest(state)<br/>(reuses prompt + RAG)
            Vis->>Vis: req.Messages[0].Images = append(..., img)<br/>+ append image-attached hint to System
            Vis->>Cli: Complete(req)
            Cli->>API: base64-inline image block
            API-->>Cli: structured DiagnosisProposal
            Cli-->>Vis: Response
            Vis->>Ver: diagnosis_proposal
        end
    end
```

### 30. LLM-as-judge grading pipeline

Used by CI gates and batch evaluation. The rubric system prompt is `CacheSystem: true` so large batches pay only cache-read tokens after the first item.

```mermaid
flowchart TB
    Item[GradeItem<br/>scenario, expected, actual, context] --> Build[buildJudgePrompt]
    Build --> Cli["llm.Client.Complete<br/>System=JudgeSystemPrompt (cached)<br/>JSONSchema=judgeSchema<br/>Temperature=0.0"]
    Cli --> API[(Provider API)]
    API --> Resp[Response{Content: JSON}]
    Resp --> Decode[json.Unmarshal raw]
    Decode --> Clamp[clamp01 each<br/>dimensional_score]
    Clamp --> Recompute[Pass := score >= 0.6<br/>model can't lie about Pass]
    Recompute --> Grade[Grade{Score, Pass, Rationale,<br/>DimensionalScores,<br/>Provider, Model}]
    Grade --> CI[CI / report / Pareto plot]

    subgraph Batch[BatchGrade · 4-worker pool]
        Items[[items]]
        Items --> Grade
    end
```

### 31. Safety guard layering

Defence in depth: inputs are redacted *before* the LLM call; outputs are scanned *after*. Refusal detection short-circuits to the rule-based fallback.

```mermaid
flowchart TB
    Input[Agent input<br/>case content + metadata] --> Redact[pkg/phi.Redact<br/>strip SSN / MRN / DOB / phone]
    Redact --> LLM[llm.Client.Complete]
    LLM --> Resp[Response.Content]
    Resp --> Refuse{safety.IsRefusal?}
    Refuse -->|yes| FB[Fall back to rule-based]
    Refuse -->|no| Hall{RAG enabled?}
    Hall -->|yes| Check[safety.CheckHallucination<br/>claim vs citations]
    Check -->|low grounding| FB
    Check -->|grounded| PHI{safety.ContainsPHIPatterns?}
    Hall -->|no| PHI
    PHI -->|matched| Reject[Reject + log + fallback]
    PHI -->|clean| Emit[Emit diagnosis_proposal]
```

### 32. LLM call tracing — JSON log + optional OTel

Every Complete()/Embed() call yields one `LLMTrace`. The stdout line is the default sink; OTLP forwarding is opt-in via env var and hook.

```mermaid
flowchart TB
    Agent[LLM-backed agent] --> Call[llm.Complete or<br/>embeddings.Embed]
    Call --> Build[Build LLMTrace<br/>provider · model · duration_ms<br/>input/output/cached tokens<br/>stop_reason · agent_id<br/>case_id · trace_id · kind]
    Build --> Log["StdLogger.RecordLLMCall<br/>emit '[llm-trace] {json}' line"]
    Log --> Sink1[(stdout log<br/>Loki / Cloud Logging)]
    Log --> Gate{BODHYANGA_OTEL_ENABLED=1<br/>AND otelHook set?}
    Gate -->|yes| Hook["otelHook(LLMTrace)<br/>SetLLMOTelHook(fn)"]
    Hook --> OTLP[(OTLP exporter)]
    OTLP --> Collector[(OTel collector)]
    Gate -->|no| Done[no-op]

    style Sink1 fill:#dfd,stroke:#070
    style Collector fill:#ddf,stroke:#007
```

### 33. Provider matrix decision tree

Which adapter pair to choose, in one diagram.

```mermaid
flowchart TB
    Start([Pick a stack]) --> Goal{Goal}
    Goal -->|Best clinical quality, cloud OK| Anth[ANTHROPIC_API_KEY<br/>claude-sonnet-4-6]
    Goal -->|One key for everything cloud| OAI[OPENAI_API_KEY<br/>gpt-4o + text-embedding-3-small]
    Goal -->|Local / private / free| Oll[Ollama<br/>llama3.2 + nomic-embed-text]

    Anth --> Vision1{Need vision?}
    Vision1 -->|yes| AnthVis[Use Sonnet 4.6 or Opus<br/>PNG / JPEG inline]
    Vision1 -->|no| AnthRAG{Need RAG?}
    AnthRAG -->|yes| Voy[+ VOYAGE_API_KEY<br/>voyage-3 embeddings]
    AnthRAG -->|no| AnthDone([Chat only])

    OAI --> Vision2{Need vision?}
    Vision2 -->|yes| OAIVis[gpt-4o inline image_url]
    Vision2 -->|no| OAIRAG{Need RAG?}
    OAIRAG -->|yes| OAIEmb[Same key,<br/>text-embedding-3-small]
    OAIRAG -->|no| OAIDone([Chat only])

    Oll --> Vision3{Need vision?}
    Vision3 -->|yes| OllVis[ollama pull llama3.2-vision<br/>or bakllava]
    Vision3 -->|no| OllRAG{Need RAG?}
    OllRAG -->|yes| OllEmb[ollama pull nomic-embed-text<br/>or mxbai-embed-large]
    OllRAG -->|no| OllDone([Chat only])
```

---

> The two PNG references — [Architecture.png](../Architecture.png) (MARA platform) and [architectureflow.png](../architectureflow.png) (sequential diagnosis flow) — are the original Microsoft-style platform views. The mermaid diagrams above are the in-code interactive index that stays in sync with the source tree.
