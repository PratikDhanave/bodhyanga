# Bodh — Governance & Policy Reference

Every policy in Bodh runs **before** any agent sees a message. This document is the field guide: every policy that ships, every policy that's planned, the composition model, and how the audit pipeline turns governance decisions into a HIPAA-relevant forensic record.

> Pairs with [docs/compliance.md](compliance.md) for the regulatory framing (HIPAA, GDPR, 42 CFR Part 2) and [architecture.md § Orchestrator](architecture.md#orchestrator-the-only-place-where-governance-runs) for where in the message flow policies execute.

---

## Contents

- [The Policy interface](#the-policy-interface)
- [Composition: fail fast](#composition-fail-fast)
- [Shipped policies](#shipped-policies)
- [Planned policies](#planned-policies)
- [Stateful policies](#stateful-policies)
- [Audit events](#audit-events)
- [PHI redaction](#phi-redaction)
- [Adding a policy](#adding-a-policy)
- [Testing policies](#testing-policies)

---

## The Policy interface

```go
type Policy interface {
    Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error)
}

type PolicyResult struct {
    Decision  Decision     // DecisionAllow | DecisionDeny
    Reason    string
    CheckedAt time.Time
}
```

- `Evaluate` runs **synchronously, before** the agent's `HandleMessage`.
- Returning an error is treated as a deny (the orchestrator logs `policy error for msg X: ...` and short-circuits).
- `Reason` is recorded in the audit event — make it specific, actionable, and PHI-free.
- `CheckedAt` is the policy timestamp (use `time.Now().UTC()` via helpers `allow(reason)` / `deny(reason)`).

### Helpers

```go
func allow(reason string) PolicyResult {
    return PolicyResult{Decision: DecisionAllow, Reason: reason, CheckedAt: time.Now().UTC()}
}

func deny(reason string) PolicyResult {
    return PolicyResult{Decision: DecisionDeny, Reason: reason, CheckedAt: time.Now().UTC()}
}
```

Use these in your policies for consistency.

---

## Composition: fail fast

```go
type CompositePolicy struct {
    Policies []Policy
}

func (c CompositePolicy) Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error) {
    for _, p := range c.Policies {
        res, err := p.Evaluate(ctx, msg)
        if err != nil { return PolicyResult{}, err }
        if res.Decision == DecisionDeny { return res, nil }
    }
    return allow("composite: all checks passed"), nil
}
```

**Deny if any child denies.** First-deny short-circuits the rest. Order matters for which `Reason` shows up in the audit event — put the most informative checks early.

Typical composition (matches `cmd/care/main.go`):

```go
policy := governance.NewComposite(
    governance.MaxContentLengthPolicy{Max: 65536},   // DoS / prompt-bomb cap
    governance.RequireCaseIDPolicy{},                // correlation enforced
    governance.MaxDiagnosticCostPolicy{},            // per-case budget
    governance.ResearchDisclaimerPolicy{},           // intent marker
)
```

For multi-tenant deployments add:

```go
governance.RequireTenantPolicy{},
governance.RequireScopePolicy{Required: map[string]string{
    "cdm_request":     "user/CarePlan.write",
    "discharge_event": "user/Encounter.read",
    "panel_request":   "user/PatientRoster.read",
}},
```

---

## Shipped policies

### `MaxContentLengthPolicy`

**Purpose:** DoS / prompt-bombing protection. Denies messages whose `Content` exceeds the limit.

```go
type MaxContentLengthPolicy struct {
    Max int  // bytes
}
```

**Default:** 65536 bytes (64 KB). Plenty for clinical narrative; small enough that a malicious caller can't exhaust memory.

**Always runs** (no message-type filter).

### `RequireCaseIDPolicy`

**Purpose:** Correlation enforcement. Every clinical message must carry a `case_id` in `Metadata`. **Foundation of the audit log** — without correlation, you can't reconstruct a case.

```go
func (RequireCaseIDPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
    clinicalTypes := map[string]bool{
        "presentation": true, "case_state": true, "ask_question": true,
        "order_test": true, "diagnosis": true, "verify_reasoning": true,
    }
    if !clinicalTypes[msg.Type] {
        return allow("non-clinical message"), nil
    }
    if msg.Metadata == nil {
        return deny("missing metadata with case_id"), nil
    }
    if _, ok := msg.Metadata["case_id"].(string); !ok {
        return deny("case_id required for clinical messages"), nil
    }
    return allow("case_id present"), nil
}
```

Non-clinical message types (system, observer, evaluator) pass through. The clinical-types set is the source of truth elsewhere (orchestrator uses it to decide which `policy_decision` allows are worth auditing — keeps the audit log signal-rich).

### `MaxDiagnosticCostPolicy`

**Purpose:** Per-case budget enforcement. Denies `order_test` messages that would push `spent_usd + test_cost_usd > max_cost_usd`.

```go
func (MaxDiagnosticCostPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
    if msg.Type != "order_test" {
        return allow("not a test order"), nil
    }
    maxCost, _ := msg.Metadata["max_cost_usd"].(float64)
    testCost, _ := msg.Metadata["test_cost_usd"].(float64)
    spent, _ := msg.Metadata["spent_usd"].(float64)
    if spent+testCost > maxCost {
        return deny("test would exceed diagnostic budget"), nil
    }
    return allow("within diagnostic budget"), nil
}
```

Note: today `test_planner` enforces the budget locally via `cost_guardian`. This policy is the **defence in depth** — if someone bypasses `cost_guardian`, the policy still catches budget violations. As Bodh's MCP tool surface grows (lab APIs, imaging AI), this becomes the single chokepoint for "no agent / tool may order something we can't afford."

### `ResearchDisclaimerPolicy`

**Purpose:** Tag the deployment as research-only. Always allows; the `Reason` documents intent (`"research demo — not for clinical use"`). Production deployments would replace this with a real `ProductionAttestationPolicy` requiring a signed assertion that the deployment context is approved.

### `RequireTenantPolicy`

**Purpose:** Multi-tenant isolation. Denies any message without `tenant_id` in metadata.

```go
func (RequireTenantPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
    if msg.Metadata == nil {
        return deny("tenant_id required: no metadata"), nil
    }
    if v, ok := msg.Metadata["tenant_id"].(string); !ok || v == "" {
        return deny("tenant_id required"), nil
    }
    return allow("tenant_id present"), nil
}
```

Pair with Postgres RLS — the `tenant_id` is used by `pkg/persistence/postgres` to set `app.tenant_id` on every query.

### `RequireScopePolicy`

**Purpose:** SMART-on-FHIR scope enforcement.

```go
type RequireScopePolicy struct {
    Required map[string]string  // msg.Type → required scope
}
```

Example:

```go
RequireScopePolicy{Required: map[string]string{
    "cdm_request":      "user/CarePlan.write",
    "discharge_event":  "user/Encounter.read",
    "panel_request":    "user/PatientRoster.read",
    "inbox_message":    "user/Communication.write",
}}
```

Reads scopes from `msg.Metadata["scopes"]` as a space-separated string (OAuth 2.0 convention from token introspection). Any matching scope allows; missing scope denies.

The API gateway should inject scopes from the validated JWT into `msg.Metadata` before the message reaches the bus.

---

## Planned policies

These are designed but not yet shipped. Each extends the same `Policy` interface — adding any of them is a single `Composite` insertion, no orchestrator change.

### `RedactNarrativePolicy`

**Purpose:** Strip clinical narrative from log-bound copies of the message, while preserving the structured fields downstream agents need.

Implementation sketch:
```go
type RedactNarrativePolicy struct {
    Redactor *phi.Redactor
}

func (p RedactNarrativePolicy) Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error) {
    // mutates msg.Content in place to redact PHI patterns
    // returns allow
    msg.Content = p.Redactor.Redact(msg.Content)
    return allow("narrative redacted for downstream propagation"), nil
}
```

Defence in depth on top of the `phi.LoggerWrapper`: redact PHI at the **message boundary** so even structured logs of `msg.Content` are PHI-free.

### `Part2ConsentPolicy`

**Purpose:** 42 CFR Part 2 — substance-use treatment records require explicit consent for many disclosures HIPAA otherwise allows.

```go
type Part2ConsentPolicy struct {
    Registry *medical.PatientRegistry  // to look up Part-2-tagged patients
}
```

Logic: if the patient referenced by `patient_id` is flagged as Part-2, require an explicit `part2_consent_token` in metadata with a valid signature and a recent timestamp. Deny otherwise.

### `BreakGlassPolicy`

**Purpose:** Allow emergency cross-patient reads with explicit audit. The clinician sets `break_glass=true` + `break_glass_reason="..."` in metadata; the policy allows the message but records a high-severity audit event.

The point isn't to block break-glass — it's to make break-glass impossible to do **silently**. Every break-glass invocation gets a clear forensic record for post-hoc review.

### `RateLimitPolicy`

**Purpose:** Per-tenant or per-clinician QPS cap. Needs windowed state (see [Stateful policies](#stateful-policies)).

```go
type RateLimitPolicy struct {
    Key   string           // "tenant_id" or "clinician_id"
    Max   int              // max msgs per window
    Window time.Duration   // sliding window
    Cache  *RateCache      // pre-bound by orchestrator
}
```

### `RequireMinimumNecessaryPolicy`

**Purpose:** HIPAA Privacy Rule §164.502(b) — the minimum-necessary standard. A policy that asserts the message carries no more PHI than the agent needs for its declared capability. Sketch: maintain a per-agent allowlist of message fields; deny messages with fields outside the allowlist.

This is the hardest policy to ship correctly. It's listed here as a design direction, not a near-term roadmap item.

---

## Stateful policies

Today's policies are deliberately **stateless** — predictable cost, easy to audit, no surprising performance characteristics. Three planned policies need state:

- `RateLimitPolicy` — sliding window per key
- `BreakGlassPolicy` — active session list
- `Part2ConsentPolicy` — recent consent token cache

The proposed shape: a `StatefulPolicy` extension that takes a context-bound `SessionStore`. The orchestrator pre-binds `trace_id`, `tenant_id`, and a session cache into the context before calling `Evaluate`. Policies that need state read it from the context value; stateless policies ignore it.

```go
// Future:
type StatefulPolicy interface {
    Policy
    NeedsState() []string  // declares what state slots it needs
}

type ContextKey string
const SessionStoreKey ContextKey = "bodh.session_store"
```

This keeps the audit log out of live policy evaluation (forensic, not load-bearing) while letting policies do bounded windowed work.

---

## Audit events

Every governance decision becomes an `audit.Event`. The Event schema is deliberately narrow:

```go
type Event struct {
    ID         string    // UUID per event
    At         time.Time
    Kind       Kind      // message | policy_decision | review_decision | agent_error
    AgentID    string
    MessageID  string
    From, To, Type string
    TenantID   string    // correlation only — never PHI
    PatientID  string    // OPAQUE — never MRN
    CaseID     string    // opaque
    TraceID    string
    Decision   string    // allow | deny | approve | reject
    Reason     string    // policy reason or review rationale
    ReviewerID string    // for HITL decisions
    ErrorText  string    // for agent_error — err.Error() ONLY, never msg.Content
}
```

### What's deliberately absent

- No narrative
- No diagnosis text
- No medication names
- No test result values
- No patient name, DOB, MRN
- No `msg.Content` ever

**Rationale:** the audit log is a **pointer** into the system. Its job is to let an investigator reconstruct who-did-what-when and pull the regulated data from a separately-controlled store under separate access control. If the audit log is ever compromised, the worst case is leaked routing — not leaked patient data.

This is the project's expression of HIPAA's minimum-necessary principle applied to the audit pipeline itself.

### Hooks (PR #12)

The orchestrator records events at four points:

| Hook | Kind | Trigger |
|---|---|---|
| Policy denied | `policy_decision` | `Decision == DecisionDeny` |
| Policy allowed (clinical types) | `policy_decision` | allow on clinical message type |
| Agent invocation | `message` | handler about to run |
| Agent error | `agent_error` | `HandleMessage` returned err |

Plus a fifth at the HITL gate: every approve / reject is a `review_decision` event with the `ReviewerID`.

### Fail-open semantics

```go
if err := o.recorder.Record(event); err != nil {
    o.env.Logf("audit record error: %v", err)
    // CONTINUE PROCESSING — never block on audit failure
}
```

Audit failures are operational issues (sink down, disk full, network partition). Blocking the diagnostic flow because audit failed is the worst outcome — the clinician now has nothing to look at, and the failure isn't actually safer. We log the audit failure (which itself appears in operational logs) and continue.

### Production sink chain

```
agent layer
    ↓
orchestrator.recordAudit
    ↓
pkg/audit.Recorder (in-memory OR Postgres)
    ↓
Postgres audit_events table
    (RLS-enforced, SELECT+INSERT only at DB grants — append-only)
    ↓
hourly export job (k8s CronJob)
    ↓
WORM sink (S3 Object Lock / Azure Immutable Blob)
    ↓
6-year retention with retention lock
```

Add Merkle chain-hashing for tamper evidence: each export job computes `hash_n = sha256(hash_n-1 || serialize(events_n))` and writes the chain into a separate signed manifest. Any tampering with historical events breaks the chain and is detectable.

---

## PHI redaction

`pkg/phi` ships `Redactor` and `LoggerWrapper` for first-line PHI defence. The default patterns:

```go
func DefaultPatterns() []Pattern {
    return []Pattern{
        {Name: "ssn",      Re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),                  Replace: "[REDACTED-SSN]"},
        {Name: "mrn",      Re: regexp.MustCompile(`(?i)\bMRN[:\s]*[A-Z0-9-]{4,}\b`),         Replace: "[REDACTED-MRN]"},
        {Name: "phone",    Re: regexp.MustCompile(`\b\d{3}[-.\s]\d{3}[-.\s]\d{4}\b`),        Replace: "[REDACTED-PHONE]"},
        {Name: "email",    Re: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), Replace: "[REDACTED-EMAIL]"},
        {Name: "iso_date", Re: regexp.MustCompile(`\b(19|20)\d{2}-\d{2}-\d{2}\b`),           Replace: "[REDACTED-DATE]"},
        {Name: "us_date",  Re: regexp.MustCompile(`\b\d{1,2}/\d{1,2}/(19|20)\d{2}\b`),       Replace: "[REDACTED-DATE]"},
    }
}
```

### Adding custom patterns

```go
redactor := phi.NewRedactor()
redactor.Add(phi.Pattern{
    Name:    "acme_mrn",
    Re:      regexp.MustCompile(`\bACME-\d{8}\b`),
    Replace: "[REDACTED-MRN]",
})
redactor.Add(phi.Pattern{
    Name:    "payer_claim_id",
    Re:      regexp.MustCompile(`\bCLM-\d{12}\b`),
    Replace: "[REDACTED-CLAIM]",
})
```

### The `LoggerWrapper`

Sits in front of any `observability.Logger`. Redacts every format string and every string-typed argument before delegating:

```go
inner := observability.NewStdLogger()
env := &orchestration.SimpleEnvironment{
    Logger: &phi.LoggerWrapper{Redactor: phi.NewRedactor(), Inner: inner},
    Clock:  observability.SystemClock{},
}
```

### Production layering

The shipped patterns are a **first line**. For production, layer in front of a DLP service:

| Service | Notes |
|---|---|
| Google Cloud DLP | API-based; supports custom info types; pay per request |
| AWS Macie | S3-focused but extends to streams via Kinesis; classification + alerting |
| Microsoft Presidio | Open-source self-hostable; NLP-based entity recognition |

The order should be: `Bodh redact patterns` → `DLP service` → `log sink`. The Bodh patterns catch obvious shapes cheaply; the DLP service catches the NLP-y cases (names mid-sentence, addresses in narrative) more expensively.

---

## Adding a policy

1. Pick the package — `pkg/governance/`.
2. Add your type:

   ```go
   type MyPolicy struct {
       // any config
   }

   func (p MyPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
       // your logic
       if shouldDeny {
           return deny("specific reason for the audit log"), nil
       }
       return allow("specific reason for the audit log"), nil
   }
   ```

3. Add a table-driven test in `pkg/governance/medical_policy_test.go` (or a new `mypolicy_test.go`).
4. Document the policy in this file's [Shipped policies](#shipped-policies) section (or [Planned](#planned-policies) if not yet wired).
5. Wire it into the composer in `cmd/care/main.go` (and `cmd/demo` / `cmd/caredemo` if applicable):

   ```go
   policy := governance.NewComposite(
       // existing policies...
       governance.MyPolicy{},
   )
   ```

6. Re-run `go test ./pkg/governance/...` and `go test ./...`.

That's it. No orchestrator change. No agent change.

---

## Testing policies

```bash
go test ./pkg/governance/... -v
```

Table-driven shape:

```go
func TestMyPolicy(t *testing.T) {
    p := MyPolicy{Threshold: 100}
    cases := []struct {
        name     string
        msg      protocol.Message
        wantAllow bool
        wantReason string
    }{
        {
            name: "allow under threshold",
            msg:  protocol.Message{Type: "thing", Metadata: map[string]any{"value": 50.0}},
            wantAllow: true,
            wantReason: "under threshold",
        },
        {
            name: "deny over threshold",
            msg:  protocol.Message{Type: "thing", Metadata: map[string]any{"value": 150.0}},
            wantAllow: false,
            wantReason: "value exceeds threshold",
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            res, err := p.Evaluate(context.Background(), tc.msg)
            if err != nil { t.Fatal(err) }
            got := res.Decision == DecisionAllow
            if got != tc.wantAllow {
                t.Errorf("allow=%v want %v (reason=%q)", got, tc.wantAllow, res.Reason)
            }
            if res.Reason != tc.wantReason {
                t.Errorf("reason=%q want %q", res.Reason, tc.wantReason)
            }
        })
    }
}
```

Reason strings end up in the audit log and operational dashboards. Test them — they're part of the public contract.
