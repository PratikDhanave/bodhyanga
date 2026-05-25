# Bodh × Agent Interoperability — Design Notes from "Generative AI on Kubernetes" Chapter 9

> **Purpose.** Bodh today is a closed multi-agent platform with strong internal coordination (MARA bus, governance, audit, HITL) but limited interoperability with the wider agentic AI ecosystem. This document maps Chapter 9 of *Generative AI on Kubernetes* (O'Reilly) — which covers MCP, A2A, security patterns, state management, and Kubernetes operational concerns — onto Bodh's architecture and proposes concrete additions ordered by impact.
>
> **Audience.** Maintainers planning the next 6-12 months of work, and contributors looking for high-impact issues to take on.

---

## Contents

- [Executive summary](#executive-summary)
- [Where Bodh is today vs the chapter's framing](#where-bodh-is-today-vs-the-chapters-framing)
- [Concept-to-code mapping](#concept-to-code-mapping)
- [Gap analysis](#gap-analysis)
- [Proposals — ordered by impact](#proposals--ordered-by-impact)
- [Recommended priority](#recommended-priority)
- [Open questions](#open-questions)
- [What stays Bodh-specific](#what-stays-bodh-specific)

---

## Executive summary

Chapter 9 of *Generative AI on Kubernetes* identifies three operational challenges for agentic applications: **security, agent coordination, and state management**. For each it offers concrete patterns:

- **MCP (Model Context Protocol)** as the agent-to-tool standard
- **A2A (Agent-to-Agent) Protocol** as the inter-agent coordination standard
- **Four security patterns**: agent impersonation, service account delegation, OAuth2 token exchange (RFC 8693), and SPIFFE/SPIRE
- **MCP Gateways** as centralised policy enforcement
- **KV stores + databases + checkpointing** for state
- **Kubernetes primitives** as the operational substrate

Bodh's architecture is **already shaped to absorb most of these** — the bus-routed Message pattern is conceptually A2A; the `pkg/tools` package is MCP-shaped; the governance composer maps onto MCP Gateway policies; the audit log is exactly what compliance-grade MCP/A2A telemetry needs. **What's missing is the wire-protocol compatibility and the production-grade identity layer.**

This document identifies **nine concrete additions** ordered by impact:

1. **MCP client** in Bodh — let LLM-backed agents call external MCP tools (lab feeds, imaging AI, FHIR resources). Highest impact, ~2 weeks.
2. **A2A server endpoints** for Bodh's specialist agents — let external orchestrators delegate to Bodh's diagnostician / panel manager / etc.
3. **Real JWT validation** in `RequireScopePolicy` — close the gap between "scope string is present" and "scope is cryptographically valid."
4. **Checkpointing for long-running HITL flows** — survive pod restarts during multi-day clinician reviews.
5. **Redis-backed KV store** for session state — `pkg/memory` swap for production.
6. **SPIFFE/SPIRE integration** when Bodh runs on Kubernetes — zero-trust workload identity.
7. **Agent card endpoints** for every Bodh agent — A2A discovery.
8. **MCP server façade** in front of Bodh's `pkg/tools` — expose them via MCP wire format.
9. **SubjectAccessReview wiring** for Kubernetes RBAC integration.

The first three are the must-haves; the next three are strong second-tier; the last three are operational maturity nice-to-haves for K8s deployments.

---

## Where Bodh is today vs the chapter's framing

### Chapter 9's three core challenges

| Challenge | Bodh's current state |
|---|---|
| **Security** — identity, auth, RBAC across agent + tool boundaries | Strong **inside** the MARA bus (governance policies + audit + HITL). Limited **outside** — `RequireScopePolicy` checks scope strings but doesn't validate JWTs; no token exchange; no workload identity. |
| **Agent coordination** — discovery, delegation, progress tracking | Strong **inside** (orchestrator, registry, bus, capability lookup). Zero **outside** — Bodh agents can't be called by external orchestrators; Bodh can't delegate to external agents. |
| **State management** — context across turns + pod restarts | Postgres backend exists (PR #10). `pkg/memory.InMemoryKV` for session state, no production swap. No checkpointing pattern for long-running cases. |

### The two protocols Bodh doesn't yet speak

| Protocol | Bodh's equivalent today | Gap |
|---|---|---|
| **MCP** (Model Context Protocol) | `pkg/tools` — internal Go interfaces called directly by `LLMDiagnosticianTools` | Not wire-compatible with MCP servers. Can't call Anthropic / Google / Microsoft / community MCP tools. Can't be called as an MCP tool. |
| **A2A** (Agent-to-Agent) | `pkg/protocol.Message` over `pkg/comm.Bus` | Not wire-compatible with A2A. Can't expose Bodh's `reasoning_verifier` or `panel_manager` to external orchestrators. Can't delegate from Bodh to external agents (e.g. a third-party radiology AI). |

### The four security patterns

| Pattern | Bodh's status | Notes |
|---|---|---|
| **Agent Impersonation (Token Passthrough)** | Not implemented. `RequireScopePolicy` reads a `scopes` string but doesn't validate it. | The closest analog: pass `case_id` + `tenant_id` through metadata. But that's correlation, not authentication. |
| **Service Account Delegation** | Not implemented. Bodh doesn't run on K8s in production yet. | Easy to add when Bodh deploys on K8s — `pkg/governance/k8s.go` could host SubjectAccessReview wiring. |
| **OAuth2 Token Exchange (RFC 8693)** | Not implemented. | Would replace today's "trust the gateway-injected scope string" model with real cryptographic delegation. High impact for multi-tenant deployments. |
| **SPIFFE/SPIRE** | Not implemented. | For zero-trust deployments — when Bodh integrates with hospital EHRs / pharmacy / labs over mTLS, this is the right pattern. |

### MCP Gateways

Bodh's **orchestrator + governance composer** is *conceptually* an MCP gateway for internal traffic:
- Centralised policy enforcement (✓ — every message hits the composer)
- Authorisation decisions (✓ — `Decision == DecisionDeny` path)
- Rate limiting (planned via `RateLimitPolicy`)
- Audit logging (✓ — PR #12)
- Multi-tenancy (✓ — `RequireTenantPolicy`)

But it's not a gateway in the chapter's sense: it doesn't sit between Bodh agents and *external* MCP servers, because Bodh doesn't speak MCP yet. After MCP client integration (proposal 1), the orchestrator's policy composer **becomes** Bodh's MCP gateway — same code, new scope.

### State management

| Pattern in chapter | Bodh's status |
|---|---|
| In-RAM dictionaries for dev | `pkg/memory.InMemoryKV` (already there) |
| Redis-backed KV for session state | Not implemented — `pkg/memory` has only the in-memory impl |
| KV + DB layered approach | Partial — Postgres exists (PR #10) but isn't paired with a fast KV layer |
| Checkpointing for long-running workflows | Not implemented |

---

## Concept-to-code mapping

Where each chapter concept lives in Bodh's source tree:

| Chapter concept | Bodh's nearest analogue | File |
|---|---|---|
| MCP tool discovery (JSON schema definitions) | `Agent.Capabilities()` + `Registry.FindByCapability()` | `pkg/agent/agent.go`, `pkg/registry/registry.go` |
| MCP tool invocation (JSON-RPC) | Bus `Publish` with `Message.Type` as method | `pkg/comm/bus.go` |
| Agent card (JSON discovery doc) | Implicit — `Capabilities()` returns a string slice, no public discovery endpoint | — (proposal 7 adds it) |
| Task lifecycle (`created` / `in_progress` / `completed`) | `CaseState` flows through agents but has no explicit lifecycle states; `cmd/care`'s HTTP responses are synchronous | — (proposal 2 adds it) |
| Artifact streaming | Not implemented; LLM streaming exists at the `llm.Client` layer but isn't propagated through the bus | — |
| OAuth2 / OIDC validation | `RequireScopePolicy` reads `metadata.scopes` (untyped string check) | `pkg/governance/medical_policy.go:80-109` |
| ServiceAccount token validation | Not implemented | — (proposal 9 adds it) |
| Token exchange (RFC 8693) | Not implemented | — (proposal 3 adds it) |
| SPIFFE workload identity | Not implemented | — (proposal 6 adds it) |
| MCP gateway policy enforcement | Orchestrator + `governance.CompositePolicy` | `pkg/orchestration/orchestrator.go`, `pkg/governance/policy.go` |
| KV store for session state | `pkg/memory.InMemoryKV` | `pkg/memory/memory.go` |
| Database for long-term memory | `pkg/persistence/postgres.Store` (PR #10) | `pkg/persistence/postgres/store.go` |
| Checkpointing | Not implemented; `CaseState` is the closest concept, but it's per-message, not durable | — (proposal 4 adds it) |
| TokenReview / SubjectAccessReview | Not implemented | — (proposal 9 adds it) |
| HPA / scaling | `agents/*` are stateless; horizontal scaling is trivial in principle but no K8s manifests committed yet | `Dockerfile` exists; HPA pending |

---

## Gap analysis

Three gap categories, ordered by clinical impact:

### Gap 1: External tool integration

Bodh's LLM-backed agents can use only internal Go tools (`pkg/tools`). To integrate with:
- **Hospital lab feeds** (LOINC-coded results)
- **Imaging AI inference services** (DICOM SR via DICOMweb, or radiology AI vendors)
- **FHIR servers** at partner hospitals
- **RxNorm / NDC lookup services**
- **The growing MCP tool ecosystem** (Anthropic, Google, Microsoft, community-published servers)

…Bodh needs an **MCP client** that speaks the wire protocol. Today, every external tool requires a custom Go adapter. With MCP, any vendor-published MCP server is available with zero adapter code.

This is the single highest-impact gap. Hospital integrations are the #1 friction in clinical AI deployment, and MCP is the path of least resistance.

### Gap 2: External agent integration

Bodh can't:
- Be called by an external orchestrator (e.g. an EHR-embedded copilot that wants to delegate "diagnose this case" to Bodh's panel)
- Delegate to external agents (e.g. a third-party radiology AI that should interpret a CT scan)
- Participate in federated diagnostic networks (Bodh deployment at Hospital A asking Bodh deployment at Hospital B "have you seen a case like this?")

These all want A2A. The protocol is exactly the right shape — task lifecycles, agent cards, artifact streaming, asynchronous delegation.

### Gap 3: Identity beyond correlation

Bodh's `tenant_id`, `case_id`, `patient_id`, `trace_id` are **routing tokens**. They're not authenticated. If a malicious client knows the right tenant ID, they can send messages claiming any user.

In production this is closed by:
- The API gateway validating user JWTs before injecting context
- The orchestrator running governance policies that check the JWT signature
- mTLS between services so the bus only accepts messages from authorised peers

Bodh has the *seams* for all of these (the `RequireScopePolicy` is the JWT seam; the bus interface is the mTLS seam) but the **actual validation isn't wired**. Closing this is necessary before any deployment that touches real PHI.

---

## Proposals — ordered by impact

### Proposal 1: MCP client in Bodh (HIGH impact, ~2 weeks)

**Add:** `pkg/mcp/client.go` — JSON-RPC over HTTP+SSE client speaking MCP wire format.

**Why:** Unlocks the entire MCP ecosystem. The next 50 hospital / lab / imaging / pharmacy integrations become "install an MCP server" instead of "write a Go adapter."

**Shape:**

```go
package mcp

type Client interface {
    // Discover tools the server exposes
    ListTools(ctx context.Context) ([]Tool, error)

    // Invoke a specific tool with arguments
    CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error)

    // Discover resources (read-only data sources)
    ListResources(ctx context.Context) ([]Resource, error)

    Close() error
}

type Tool struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    InputSchema map[string]any `json:"inputSchema"` // JSON Schema
}

type ToolResult struct {
    Content []ContentItem `json:"content"`
    IsError bool          `json:"isError"`
}

// HTTPClient is the default implementation
type HTTPClient struct {
    endpoint   string
    httpClient *http.Client
    auth       AuthProvider // pluggable: Bearer / mTLS / SPIFFE
}
```

**Integration into Bodh:**

The `agents/diagnostician_tools.go` agent currently calls internal Go tools. Replace with MCP client calls:

```go
// Today
type DiagnosticianTools struct {
    Tools tools.Registry // internal Go
}

// After
type DiagnosticianTools struct {
    MCPClients map[string]mcp.Client // keyed by tool category
}
```

The `agent.Capabilities()` slice maps onto MCP tool names. A diagnostician agent might call:
- `lab_query` (MCP server at the hospital lab)
- `imaging_ai_interpret` (vendor radiology AI MCP server)
- `rxnorm_lookup` (NLM MCP server)

**Governance:** Every MCP call goes through `governance.CompositePolicy` before invocation (proposal: a new `MCPPolicyEnforcer` that wraps the MCP client). The composer enforces tenant scoping, cost limits, allowlists of which agents can call which tool categories.

**Audit:** Every `CallTool` produces an `audit.Event` with `Kind=KindMessage`, agent_id, tool name, MCP server URL (sanitised), result status. **No tool arguments or results** in the audit event — those are PHI; the event records that the call happened, not what was said.

**Effort breakdown:**
- ~3 days: JSON-RPC over HTTP+SSE client + structured types
- ~3 days: Auth providers (Bearer, mTLS, SPIFFE-aware)
- ~3 days: Policy enforcer wrapper + audit integration
- ~3 days: Refactor `diagnostician_tools.go` to use the new client
- ~2 days: Tests + an example MCP server fixture for the integration test

### Proposal 2: A2A server endpoints for Bodh agents (HIGH impact, ~2-3 weeks)

**Add:** `pkg/a2a/server.go` — HTTP + JSON-RPC server exposing Bodh agents as A2A-discoverable services.

**Why:** Lets external orchestrators delegate to Bodh's specialist agents. The most concrete use case: an EHR-embedded clinician copilot at Hospital A delegates "second opinion on this case" to a Bodh deployment that runs the multi-expert diagnostic panel.

Also enables **federated Bodh** — multiple Bodh deployments coordinating without needing a shared bus.

**Shape:**

```go
package a2a

// AgentCard is the discovery document
type AgentCard struct {
    AgentID      string   `json:"agent_id"`
    Name         string   `json:"name"`
    Description  string   `json:"description"`
    Skills       []string `json:"skills"`         // ≈ Bodh's Capabilities()
    InputModes   []string `json:"input_modes"`    // MIME types
    OutputModes  []string `json:"output_modes"`
    Protocols    []string `json:"protocols"`      // ["a2a/v1"]
    Endpoint     string   `json:"endpoint"`       // where to POST tasks
}

// Task is the lifecycle-tracked unit of delegated work
type Task struct {
    ID         string                 `json:"id"`
    AgentID    string                 `json:"agent_id"`
    Status     TaskStatus             `json:"status"`     // created | in_progress | completed | failed | cancelled
    Input      map[string]any         `json:"input"`
    Output     map[string]any         `json:"output,omitempty"`
    Artifacts  []Artifact             `json:"artifacts,omitempty"`
    CreatedAt  time.Time              `json:"created_at"`
    UpdatedAt  time.Time              `json:"updated_at"`
}
```

**Integration:**

The `cmd/care` HTTP server gets new endpoints:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/a2a/agents` | List all A2A-exposed Bodh agents |
| `GET` | `/a2a/agents/{agent_id}/card` | Agent card (discovery) |
| `POST` | `/a2a/agents/{agent_id}/tasks` | Submit a task |
| `GET` | `/a2a/tasks/{task_id}` | Task status |
| `DELETE` | `/a2a/tasks/{task_id}` | Cancel a task |
| `GET` | `/a2a/tasks/{task_id}/stream` (SSE) | Stream artifacts as they're produced |

Internally, an A2A task submission becomes a bus message addressed to the named agent. The HTTP layer handles task ID minting, status persistence (in the audit log + Postgres), and SSE streaming.

**Not every Bodh agent should be A2A-exposed.** Candidates:
- `reasoning_verifier` — "is this proposed diagnosis sound?" is a great A2A skill
- `panel_manager` — "give me the care gaps for this patient roster" is great
- `cdm_planner` — "build a care plan for this diabetes patient" is great
- `diagnostician` (LLM variant) — "give me a working diagnosis for this case state" is great

Agents that **should not** be A2A-exposed (at least initially):
- `intake`, `rpm_monitor`, `cost_guardian` — internal-only plumbing
- `human_review` — exposing this to external orchestrators creates compliance complications
- `engagement` — outbound PHI to external orchestrators isn't safe without a BAA

The agent's `Capabilities()` slice already declares what it can do; the A2A layer just makes it discoverable over HTTP.

**Effort:** ~10-15 days. Higher than MCP client because there's lifecycle tracking, SSE streaming, agent card discovery, and task persistence to design.

### Proposal 3: Real JWT validation in `RequireScopePolicy` (HIGH impact, ~3-5 days)

**Today:**

```go
func (p RequireScopePolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
    // ... reads metadata["scopes"] as space-separated string ...
    // No signature validation, no audience check, no issuer check.
}
```

This is correlation, not authentication. An attacker who knows the right scope string format can submit any clinical message and `RequireScopePolicy` will pass.

**Fix:** Replace the scope-string check with full JWT validation against a configured OIDC issuer.

```go
type RequireScopePolicy struct {
    Required   map[string]string // msg.Type → required scope
    JWKSURL    string             // OIDC issuer's JWKS endpoint
    Issuer     string             // expected iss claim
    Audience   string             // expected aud claim
    keyCache   *keyCache          // refreshed periodically
}

func (p *RequireScopePolicy) Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error) {
    required, ok := p.Required[msg.Type]
    if !ok {
        return allow("no scope required for type"), nil
    }
    tokenStr, _ := msg.Metadata["jwt"].(string)
    if tokenStr == "" {
        return deny("jwt required for " + msg.Type), nil
    }
    claims, err := p.validateJWT(ctx, tokenStr)
    if err != nil {
        return deny("jwt validation failed: " + err.Error()), nil
    }
    if !hasScope(claims.Scope, required) {
        return deny("required scope missing: " + required), nil
    }
    return allow("scope " + required + " present (jwt verified)"), nil
}
```

**Why this matters:** Closes the gap between the chapter's "agent impersonation" and "token exchange" patterns and what Bodh actually enforces. Once JWTs are validated, the next step (RFC 8693 token exchange) is a small extension — the validator already knows how to verify; it just needs to also know how to mint dual-identity tokens.

**Effort:** ~3-5 days for validation + caching + tests. Token exchange (RFC 8693) is a separate ~5-day extension on top.

### Proposal 4: Checkpointing for long-running HITL flows (MEDIUM impact, ~5 days)

**Problem:** A HITL `discharge_review` may sit in the queue for 3 days while the reviewing physician is on the wards. If the Bodh pod restarts during those 3 days, today the in-memory `hitl.Queue` is lost. Postgres persistence (PR #10) addresses this partly — the review is in `patient_records` — but the **resumption logic** isn't fully baked. Specifically:

- When a pod boots, it doesn't rehydrate pending reviews from Postgres into the in-memory queue
- A long-running diagnostic flow that's mid-reasoning (e.g. Reflexion at round 2 of 3) loses its position

**Fix:** Implement Chapter 9's "checkpoint after each major step" pattern at two levels:

1. **HITL queue rehydration on boot** — `pkg/hitl.Queue` reads pending reviews from `*Store.ListPendingReviews(tenantID)` at startup.
2. **Reflexion / Cascade / Debate checkpointing** — after each round, write a `RoundState` row to a new `agent_checkpoints` table. On resume, read the latest checkpoint and continue from there.

```go
// pkg/checkpoint/checkpoint.go (new)
type Checkpoint struct {
    ID         string         `json:"id"`
    CaseID     string         `json:"case_id"`
    TenantID   string         `json:"tenant_id"`
    AgentID    string         `json:"agent_id"`
    Step       int            `json:"step"`        // sequential per case+agent
    State      []byte         `json:"state"`       // serialized agent-specific state
    CreatedAt  time.Time      `json:"created_at"`
}

type Store interface {
    Save(ctx context.Context, cp Checkpoint) error
    LoadLatest(ctx context.Context, tenantID, caseID, agentID string) (*Checkpoint, error)
    LoadAll(ctx context.Context, tenantID, caseID, agentID string) ([]Checkpoint, error)
}
```

**Pattern in the agent code:**

```go
func (a *ReflexionDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
    // Try to resume from a checkpoint
    cp, _ := a.Checkpoints.LoadLatest(ctx, tenantID, caseID, a.ID())
    startRound := 0
    var roundState ReflexionRoundState
    if cp != nil {
        startRound = cp.Step + 1
        json.Unmarshal(cp.State, &roundState)
    }
    for round := startRound; round < a.MaxRounds; round++ {
        // ... do the round work ...
        a.Checkpoints.Save(ctx, Checkpoint{
            CaseID: caseID, AgentID: a.ID(), Step: round, State: encode(roundState),
        })
    }
    return []agent.Message{a.emit(roundState)}, nil
}
```

**Effort:** ~5 days for the checkpoint store + 2 days each for instrumenting Reflexion / Cascade / Debate.

### Proposal 5: Redis-backed KV store for session state (MEDIUM impact, ~3 days)

**Today:** `pkg/memory.InMemoryKV` only. Session state is lost on pod restart.

**Fix:** Add `pkg/memory/redis.go`:

```go
type RedisKV struct {
    client *redis.Client
    prefix string  // namespace by tenant
}

func NewRedisKV(addr, prefix string) *RedisKV { /* ... */ }

func (r *RedisKV) Get(ctx context.Context, key string) ([]byte, error) {
    return r.client.Get(ctx, r.prefix+":"+key).Bytes()
}

func (r *RedisKV) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
    return r.client.Set(ctx, r.prefix+":"+key, val, ttl).Err()
}
```

`cmd/care/main.go` picks the backend at startup:

```go
var kv memory.KV
if dsn := os.Getenv("BODH_REDIS_ADDR"); dsn != "" {
    kv = memory.NewRedisKV(dsn, "bodh:"+tenantID)
} else {
    kv = memory.NewInMemoryKV()
}
```

**Use cases beyond session state:**
- RAG embedding cache (TTL'd)
- JWT validation result cache (TTL = `exp - now - safety_margin` from Proposal 3)
- HITL queue projection cache for fast UI reads
- Recent LLM call cache for prompt caching beyond what providers offer

**Effort:** ~3 days. Standard `go-redis` integration.

### Proposal 6: SPIFFE/SPIRE integration when on Kubernetes (MEDIUM impact, ~2 weeks)

**Why:** When Bodh integrates with hospital systems over mTLS — EHR ADT feeds, lab result streams, pharmacy refill backends — SPIFFE provides cryptographically bound workload identity that can't be exfiltrated.

**Add:**
- `pkg/identity/spiffe.go` — SVID fetcher via Workload API (Unix socket at `/run/spire/sockets/agent.sock`)
- `pkg/comm.SPIFFEBus` — bus implementation that wraps the in-memory bus with mTLS auth using SVIDs (for multi-pod deployments)
- `pkg/governance.RequireSPIFFEIDPolicy` — verify the calling workload's SPIFFE ID against an allowlist
- `deploy/spire/` — example SPIRE Server StatefulSet + Agent DaemonSet

**Shape:**

```go
package identity

type SPIFFESource struct {
    socketPath string
    client     *workloadapi.X509Source
}

func NewSPIFFESource() (*SPIFFESource, error) {
    src, err := workloadapi.NewX509Source(context.Background())
    if err != nil { return nil, err }
    return &SPIFFESource{client: src}, nil
}

func (s *SPIFFESource) CurrentSVID() (*x509svid.SVID, error) {
    return s.client.GetX509SVID()
}

func (s *SPIFFESource) TLSConfig(allowedIDs []string) *tls.Config {
    // Returns mTLS config that:
    // - Uses current SVID as client cert
    // - Validates peer's SPIFFE ID against allowedIDs
    // - Refreshes automatically via the X509Source
}
```

**Integration with the bus:** When the bus is in-cluster Kafka or NATS, the production deployment configures both ends to use mTLS with SPIFFE certs. The auth provider is `pkg/identity.SPIFFESource`.

**Integration with MCP client (Proposal 1):** The MCP client's auth provider can be SPIFFE-backed for connections to internal MCP servers (e.g. a hospital's lab MCP server running in the same cluster).

**Effort:** ~10-15 days including the SPIRE deployment example.

### Proposal 7: Agent card endpoints (LOW-MEDIUM impact, ~2 days)

**Why:** Even without full A2A protocol support (Proposal 2), publishing agent cards is a small step that makes Bodh discoverable.

**Add:** `cmd/care` exposes `GET /a2a/agents` and `GET /a2a/agents/{agent_id}/card` returning A2A-shaped agent cards built from each agent's `Capabilities()`.

Cards are derived, not stored — generated on-the-fly from the registry:

```go
func agentCard(a agent.Agent) AgentCard {
    return AgentCard{
        AgentID:     a.ID(),
        Name:        a.Name(),
        Skills:      a.Capabilities(),
        InputModes:  []string{"application/json"},
        OutputModes: []string{"application/json"},
        Protocols:   []string{"bodh/v1"}, // bodh-native today, "a2a/v1" after proposal 2
    }
}
```

**Effort:** ~2 days. Mostly the cmd/care HTTP wiring + tests.

### Proposal 8: MCP server façade for Bodh's `pkg/tools` (LOW impact, ~5 days)

**Why:** Bodh's `pkg/tools` exposes internal tool helpers (RxNorm lookup, FHIR resource fetcher, etc.). Making these MCP-shaped lets other LLM agents (Claude desktop, Cursor, future Bodh deployments) call them.

**Add:** `cmd/mcp-server/` — a separate binary that wraps the in-process `pkg/tools` with an MCP server façade.

Tools that are good MCP candidates:
- `medical.TestCatalog` lookup
- `medical.DefaultPanelRules` evaluation against a `PatientRecord`
- `medical.ScoreReadmissionRisk` calculation
- Care-plan template lookup by condition

Tools that should **not** be MCP-exposed:
- Anything that mutates `PatientRegistry`
- Anything that publishes engagement notifications

**Effort:** ~5 days. Mostly the MCP server wire format and the tool wrapping. Reuses the JSON-RPC infrastructure from Proposal 1.

### Proposal 9: SubjectAccessReview wiring for K8s RBAC (LOW impact, ~3 days)

**Why:** When Bodh runs on Kubernetes and needs fine-grained per-agent permissions on internal resources (e.g. "agent X can read patient_records but not write"), `SubjectAccessReview` provides the integration.

**Add:** `pkg/governance.RequireK8sRBACPolicy` — calls `SubjectAccessReview` against the cluster API to verify the calling ServiceAccount has the required permission on a custom application resource.

This pattern is described in detail in the chapter and works well for Kubernetes-native deployments. For non-K8s deployments, it's a no-op (the policy is opt-in via configuration).

**Effort:** ~3 days including tests against a kind cluster.

---

## Recommended priority

### Sprint 1 — Wire compatibility (2-3 weeks)

1. **Proposal 1 (MCP client)** — unblocks the ecosystem
2. **Proposal 3 (JWT validation)** — closes a real compliance gap

### Sprint 2 — Externalisation (3-4 weeks)

3. **Proposal 2 (A2A server endpoints)** — most impactful interop story
4. **Proposal 7 (Agent card endpoints)** — small, useful, lands inside Proposal 2 naturally
5. **Proposal 5 (Redis KV)** — needed before scaling past single-pod

### Sprint 3 — Resilience and zero-trust (3-4 weeks)

6. **Proposal 4 (Checkpointing)** — multi-day HITL flows need this
7. **Proposal 6 (SPIFFE/SPIRE)** — for production cluster deployments

### Future / lower priority

8. **Proposal 8 (MCP server façade)** — depends on demand; revisit after the MCP client lands and we see what external tools people want to expose
9. **Proposal 9 (SubjectAccessReview)** — only when Bodh ships a serious K8s deployment story

### Optional intermediate: full RFC 8693 token exchange

On top of Proposal 3 (JWT validation), implementing RFC 8693 token exchange would close the chapter's third security pattern. Roughly +5 days on top of Proposal 3. Pair it with Sprint 2.

---

## Open questions

These are real architectural questions that should be answered before committing to any of the proposals:

### 1. Federation vs. centralisation

The chapter assumes a model where agents are independently deployed and discover each other. Bodh's current model has a central orchestrator per deployment. After A2A:

- Should Bodh's internal agents speak A2A to each other (replacing the in-memory bus)?
- Or should A2A be only for inter-deployment / external communication, with the in-memory bus retained for intra-deployment?

The book's design favours the former (each agent as a separate K8s Deployment). Bodh's current discipline favours the latter (one process, multiple subscribed handlers).

**Recommendation:** Hybrid. Keep the internal bus for low-latency intra-process coordination. Use A2A at the deployment boundary. The `Capabilities()` interface is already shaped to support both.

### 2. MCP gateway vs. orchestrator policies

The chapter introduces MCP Gateways (Microsoft MCP Gateway, IBM ContextForge, Envoy AI Gateway, Solo.io agentgateway). For external MCP calls (Proposal 1), should Bodh:

a) Have its orchestrator + policy composer act as the MCP gateway (less infra, but tighter coupling)
b) Deploy a dedicated MCP gateway in front (more infra, but separates concerns and integrates with the wider MCP ecosystem)

**Recommendation:** Start with (a). The orchestrator already does this work for internal traffic; making the same policies apply to MCP egress is natural. Migrate to (b) when external MCP integrations exceed ~10 endpoints or when policy complexity warrants centralised management.

### 3. Whose identity does an MCP-called tool see?

When the LLM diagnostician calls an external MCP tool to check a lab value, the chapter's four patterns offer different answers:
- **Impersonation:** the tool sees the nurse who triggered the agent
- **Service Account Delegation:** the tool sees the Bodh deployment
- **Token Exchange:** both — composite identity
- **SPIFFE:** the Bodh deployment (workload identity)

**Recommendation:** Default to Token Exchange (composite identity). For HIPAA-aligned deployments, the upstream system often needs *both* — auditing requires the nurse identity for per-user permissions, but rate limiting / quota wants the Bodh deployment identity. RFC 8693 gives both in a single signed token.

### 4. Streaming artifacts and Bodh's bus

The chapter describes A2A's artifact streaming for incremental results. Bodh's bus has no streaming primitive — every `Message` is whole. If a Reflexion diagnostician runs 3 rounds, the bus doesn't see the intermediate rounds; only the final proposal.

For A2A externalisation, this matters: an external caller using A2A expects to see streaming progress. Options:

a) Add streaming to the bus (`Bus.PublishStream(ctx, msg, ch)`)
b) Keep the bus whole-message and add streaming only at the A2A HTTP layer (translate the agent's intermediate checkpoints from Proposal 4 into SSE artifacts)

**Recommendation:** (b). Less surgery on the bus; the checkpoint store is already where intermediate state lives. Future bus implementations (Kafka with multiple messages per task) might offer streaming natively, but A2A doesn't require us to commit yet.

### 5. AGENTS.md for Bodh

The chapter mentions AGENTS.md as one of the AAIF's launch projects — a file format documenting how AI coding agents should interact with a codebase. Bodh could ship its own AGENTS.md describing:
- The MARA pattern (so coding agents don't try to make agents call each other directly)
- The fallback contract (so coding agents don't strip the rule-based fallback when "simplifying")
- The illustrative-only threshold convention (so coding agents don't treat the magic numbers as authoritative)
- The HITL gate semantics
- The no-PHI-in-logs rule

This is a ~2 hour task and pays off every time someone uses an AI coding assistant on the repo. **Quick win.**

---

## What stays Bodh-specific

After MCP + A2A + JWT validation + SPIFFE + checkpointing + Redis + SubjectAccessReview, what's still uniquely Bodh's?

1. **Clinical domain logic.** Diagnostic rules, care-plan templates, panel rules, readmission risk scoring, HL7 v2 parsing, FHIR mapping. None of these belong in the agent framework layer.
2. **HIPAA-aware governance composition.** The specific policies (`RequireCaseIDPolicy`, `MaxDiagnosticCostPolicy`, planned `RedactNarrativePolicy`, `Part2ConsentPolicy`, `BreakGlassPolicy`) are clinical-domain policies. The composer they live in is interop-friendly; the policies themselves are Bodh's contribution to the clinical AI canon.
3. **The HITL gate as the §3060 CDS carve-out enforcer.** The pattern (lossless-on-reject queue with audit trail) is generic, but the *commitment to gate every consequential action* is a clinical-AI architectural stance, not a generic agent pattern.
4. **Specialty diagnostic rules.** PR #11's ACS / sepsis / DKA / CHF / CAP rules. Domain content, not framework.
5. **The bench discipline.** Manifest-driven case fixtures with baseline regression gating in CI. Generic in shape (any agentic system could adopt it), but Bodh's fixture catalogue is clinical content.
6. **The audit event schema.** "Enough to reconstruct, never enough to leak." The shape is portable; the choice of which fields to include and exclude is a HIPAA-informed design decision.

The interop work (proposals 1-9) makes Bodh's clinical strengths *more accessible* to the wider ecosystem without diluting them. A copilot at Hospital A delegating to Bodh's `reasoning_verifier` via A2A still gets all of Bodh's clinical governance, audit logging, and rule-based safety net. The wire protocol is interop; the substance stays Bodh.

---

## What this means for ROADMAP.md

Add a new phase:

### Phase 11 — Agent interoperability (planned)

- ⚪ MCP client in `pkg/mcp` for external tool integration
- ⚪ A2A server endpoints in `cmd/care` for external agent delegation
- ⚪ Real JWT validation in `RequireScopePolicy`
- ⚪ RFC 8693 token exchange support
- ⚪ Checkpoint store for long-running agent state
- ⚪ Redis KV backend for session state
- ⚪ SPIFFE/SPIRE workload identity for K8s deployments
- ⚪ Agent card endpoints (A2A discovery)
- ⚪ MCP server façade for `pkg/tools`
- ⚪ SubjectAccessReview wiring for K8s RBAC
- ⚪ AGENTS.md for AI-assisted coding on Bodh

---

## Try it (when the proposals land)

```bash
# Proposal 1: Call an external MCP tool from a Bodh diagnostician
go run ./cmd/demo -diagnostician=llm-anthropic \
    -mcp-server=https://lab-mcp.hospital.example/v1

# Proposal 2: Delegate to a Bodh agent from external orchestrator
curl -X POST https://bodh.example.org/a2a/agents/reasoning_verifier/tasks \
    -H 'Authorization: Bearer <jwt>' \
    -d '{"input":{"diagnosis":"NSTEMI","rationale":"..."}}'

# Proposal 3+: With real JWT validation, governance now enforces signed claims
curl -X POST https://bodh.example.org/cdm \
    -H 'Authorization: Bearer <jwt-with-user/CarePlan.write-scope>' \
    -d '{"patient_id":"pt-001","condition":"dm","case_id":"case-001"}'
```

---

## Closing

Bodh's architecture is already shaped to absorb MCP and A2A. The investment is mostly wire-protocol work plus the security upgrades that should have happened before any real PHI deployment. After the proposals in this document land, Bodh moves from "interesting reference implementation" to "drop-in clinical AI substrate" — a deployment that any hospital can plug their MCP-spoken tools into, that any orchestrator can delegate to over A2A, and that any compliance team can audit through the standards-aligned identity layer.

The protocols may change. The need for secure communication, standardised coordination, and persistent state will remain constant.

---

*This document is a planning artefact, not a commitment. Specific PRs implementing these proposals will land iteratively; the priority order is recommended but not binding.*
