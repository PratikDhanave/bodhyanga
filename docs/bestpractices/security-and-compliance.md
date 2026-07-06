# Security & Compliance Conventions

The rules that make Bodh HIPAA-defensible, with `file:line` evidence. The recurring posture:
**deny / hash / redact by default, and make every exception audited.** See
[README.md](README.md) for the meta-conventions.

> **The single most important caveat in this whole directory:** the cryptographically
> verifiable audit chain (§2) is implemented and tested but **dormant** — `cmd/care` writes
> to a hash-less Postgres table (ADR-0008). Scope any "tamper-evident audit" claim to
> "available via `pkg/audit/immutable.go`", never "in force in production".

---

## 1. PHI handling

**RULE: A raw `CaseID` (potentially an MRN) must NEVER appear on a span, metric label, log,
or audit event. On spans it is always `sha256(caseID)` truncated to the first 8 bytes = 16
hex chars via `hashID`. Prompt/response content is never attached anywhere.**

The single choke point (`pkg/observability/otlp.go:188-193`):

```go
func hashID(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8]) // 8 bytes → 16 hex chars
}
```

- Applied at `otlp.go:133-136` (`case_id`) and `laminar.go:211-215` (`bodh.case_id`, comment:
  "CaseID can be a real MRN — always hash before attaching to a span").
- Prometheus labels deliberately **omit `case_id` entirely** (`pkg/observability/prom.go:36`
  — "case_id is intentionally omitted from all label sets", cites AGENTS.md §2).
- Enforced by test: `pkg/observability/otlp_test.go:176-184`,
  `laminar_test.go:189-197`.
- Defense-in-depth redactor at the log seam: `pkg/phi/redact.go:74-118` implements Safe
  Harbor 45 CFR §164.514(b)(2) patterns (SSN, MRN with literal "MRN" prefix, phone, email,
  dates). It tolerates false positives by design (`redact.go:71-73`).
- **Never copy `msg.Content` into audit:** `pkg/gateway/pipeline.go:44` ("the logger must
  never be handed msg.Content (PHI)"), `:113-115` (`record()` logs only routing/kind
  metadata). Even the injection scanner logs threat-type labels + msg_id, never content
  (`pkg/governance/agentmesh_gate.go:210-221`, Invariant #2).

> **Doc nuance:** CLAUDE.md shorthands this as `sha256(caseID)[:8]`. The `[:8]` is *bytes*,
> rendering as *16 hex chars*. The code (`otlp.go`) is authoritative.

---

## 2. Audit log — append-only Merkle chain

**RULE: Every audit line is `Hash = SHA-256(prevHash + "\n" + json(Event))`; any retroactive
edit breaks every subsequent hash, detectable via `VerifyChain`. The file is
`O_APPEND|O_CREATE`. (Satisfies HIPAA §164.312(b) / 21 CFR Part 11 §11.10(e).)**

Chain construction (`pkg/audit/immutable.go:151-156`):

```go
h := sha256.New()
h.Write([]byte(prevHash))
h.Write([]byte("\n"))
h.Write(eventJSON)
hash := hex.EncodeToString(h.Sum(nil))
```

- Genesis prevHash = all-zeros SHA-256 (`immutable.go:75-77`); chain advances only *after* a
  successful write (`:171-178`); append-only open `O_APPEND|O_CREATE|O_WRONLY, 0o600`
  (`:86`).
- `VerifyChain` recomputes end-to-end and returns the broken seq, constant-time compare via
  `hmac.Equal` (`:205-254`). S3 WORM export is fire-and-forget so it can't block the clinical
  hot path (`:186-196`).

**Caveat — ADR-0008 (verified):** `ImmutableRecorder` is wired **nowhere** in `cmd/care`.
`cmd/care/main.go:661-671` (`selectBackends`) returns only `pg.AuditRecorder(tenant)`
(plain `INSERT INTO audit_events`, `pkg/persistence/postgres/store.go:502-533`) or
`audit.NewInMemory()`. The production table has **no hash/prev_hash/seq column**; ordering is
by wall-clock `at DESC`. This is a drift from **Accepted ADR-0002**. Production
tamper-resistance today is only RLS + `GRANT SELECT,INSERT` (`db/migrations/0001_init.sql`,
`0005_rls.sql`). Verification tooling: `cmd/audit-verify`.

---

## 3. Governance policy stack

**RULE: Every policy implements `Evaluate(ctx, msg) (PolicyResult, error)`; policies compose
via `CompositePolicy` (fail-fast, first-deny-wins); a non-nil error is treated as deny.**

```go
type Policy interface {
	Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error)
}   // pkg/governance/policy.go:94-96
```

- `Decision` is binary `allow`/`deny`, and the string value is an **on-disk contract**
  (`policy.go:23-38`). `CompositePolicy.Evaluate` runs in order, short-circuits on first deny
  (`policy.go:145-186`).
- Composed declaratively at each binary's startup, e.g. `cmd/care/main.go:70-77`
  (`NewAgentMeshGate` + `NewComposite(MaxContentLengthPolicy{Max:65536}, …)`).

The composable policies (all `pkg/governance/`):

| Policy | Regulatory anchor / behavior |
|---|---|
| `MaxContentLengthPolicy` | DoS/prompt-bomb cap; zero Max → error (fail-closed) |
| `AgentMeshGate` | kill switches + rings + prompt-injection defense |
| `Part2ConsentPolicy` | 42 CFR Part 2 SUD; requires `part2_consent="true"` else deny |
| `SMARTScopePolicy` | SMART-on-FHIR scope + token validation; missing scope → deny |
| `BreakGlassPolicy` | emergency override w/ mandatory audit (see §4) |
| `RateLimitPolicy` | per-tenant / per-agent token-bucket RPS |
| `RedactNarrativePolicy` | strips HPI/assessment/plan free-text, returns ALLOW (advisory) |
| `OPAPolicy` | Rego-backed policy |

**Kill switches — `agentmesh_gate.go:161-226`** run four checks in order: (1) global kill
switch, (2) agent-scoped kill switch, (3) ring enforcement (Admin/Standard/Restricted/
Sandboxed; unassigned defaults Restricted), (4) prompt-injection defense (default
`MaxRiskScore=40`). Operator runtime control via `KillSwitches()` accessor, wired to
`/admin/kill-switch`. Gate errors → deny (fail-closed).

---

## 4. Fail-closed vs. fail-open

**RULE: Fail CLOSED on governance/middleware (deny on error), fail OPEN on audit (never block
message flow on audit-sink failure).**

```go
// governance — fail closed  (pkg/gateway/pipeline.go:60-66)
res, err := p.Policy.Evaluate(ctx, msg)
if err != nil { p.logf(...); return false }

// audit — fail open  (pkg/gateway/pipeline.go:113-124)
func (p *Pipeline) record(e audit.Event) {
	if p.Recorder == nil { return }
	if err := p.Recorder.Record(e); err != nil { p.logf(...) } // logged, not returned
}
```

**The one deliberate exception — break-glass audit is fail-CLOSED:** a failed break-glass
audit *denies* access (`break_glass_policy.go:170-172`). Rationale: an emergency override with
no durable record is worse than a denied override.

---

## 5. Auth — OPA Rego + JWT, SMART-on-FHIR, break-glass

**RULE: JWT signature/issuer is verified inside a Rego rule; authorization is a separate named
Rego rule per endpoint; `exp` is also re-checked in Go as defense-in-depth.**

- `Authenticate` verifies signature+issuer via Rego `data.bodh.auth.auth`
  (`pkg/auth/auth.go:176-184`); Go-side `exp` re-check `:192` ("even if the Rego policy's exp
  handling silently changes").
- `Evaluate` runs a named Rego rule per endpoint, e.g. `data.bodh.explain.allow`
  (`auth.go:203-212`). Rego modules embedded via `//go:embed` (`pkg/auth/rules.go:14-33`);
  sources in `pkg/auth/rego/` and `pkg/governance/rego/`.
- Rego pins RS256 as defense against `alg=none` (`pkg/auth/rego/authentication.rego:5-8`),
  `default auth := false` (`:26`).
- **SMART scopes** (`pkg/governance/smart_scope_policy.go:40-71`): validate token against
  issuer/JWKS, assert a required scope (e.g. `patient/Observation.read`); valid-but-missing →
  deny; on allow, write patient context to metadata.

Testing Rego: `make opa-test` → `opa test --v1-compatible pkg/governance/rego pkg/auth/rego`
(`Makefile:742-745`), auto-installs the OPA CLI, runs in-process (no server). **Not wired
into CI** — local gate only.

**Break-glass flow (`pkg/governance/break_glass_policy.go`):**

```go
// audit BEFORE allow; audit failure → deny  (:135-175)
if err := p.Recorder.RecordBreakGlass(e); err != nil {
	return PolicyResult{}, err // fail-closed (the §4 exception)
}
return allow("break_glass access granted"), nil
```

Requires `break_glass_reason` + `break_glass_clinician` metadata else deny (`:132`).
`BreakGlassRecorder` is defined in `governance` (not `audit`) to avoid an import cycle.
Regulatory anchor: HIPAA §164.312(b) + post-hoc review 24–72h.

---

## 6. Autopilot fence

**RULE: `Criteria.AllowAutopilot` defaults to false and is checked BEFORE the statistical
(Wilson) gate — a perfect record can never promote copilot→autopilot without explicit
authorization. Autonomy is a regulated (SaMD) decision, not a statistical one.**

```go
// pkg/graduation/graduation.go:84-95 — fence BEFORE the Wilson gate
if next == StageAutopilot && !c.AllowAutopilot {
	d.Reason = "promotion to autopilot requires explicit authorization ..."
	return d
}
```

`DefaultCriteria` leaves it false with `MinObservations:100, MinWilsonLower:0.95`
(`graduation.go:29-34`). Enforced by `pkg/graduation/fence_test.go:11-49`: fenced by default
even with a perfect record; authorization still doesn't bypass the Wilson gate; the fence is
specific to the autopilot target (shadow→copilot unaffected). ADR-0012.

**Caveat:** `pkg/graduation` currently has **zero non-test importers** — it is a tested but
unwired library (ADR-0010, ADR-0012). Promotion is not yet driven by real telemetry.
