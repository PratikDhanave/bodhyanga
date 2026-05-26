# Bodh — Operations Guide

Observability, audit, SLOs, and operational concerns for running Bodh.

> Pairs with [deployment.md](deployment.md) (how to deploy) and [governance.md](governance.md) (policy and audit semantics).

---

## Contents

- [Signals at a glance](#signals-at-a-glance)
- [Structured logs](#structured-logs)
- [Metrics](#metrics)
- [OpenTelemetry export](#opentelemetry-export)
- [LLM call tracing](#llm-call-tracing)
- [Audit pipeline](#audit-pipeline)
- [Bus implementations](#bus-implementations)
- [LLM reliability](#llm-reliability)
- [SLI / SLO recommendations](#sli--slo-recommendations)
- [Runbooks](#runbooks)

---

## Signals at a glance

| Signal | Source | Sink (default) | Sink (production) |
|---|---|---|---|
| Application logs | `observability.Logger` | stdout `[bodh]` prefix | Loki / Cloud Logging via Vector / Fluent Bit |
| LLM call traces | `observability.LLMTrace` | stdout `[llm-trace]` JSON line | OTLP collector via `BODH_OTEL_EXPORT=1` |
| Audit events | `pkg/audit.Recorder` | in-memory | Postgres `audit_events` table + WORM sink |
| Eval records | `pkg/eval.Store` | in-memory | Postgres `interaction_records` table |
| HTTP access logs | (cmd/care default mux) | stdout | API gateway / reverse proxy |
| Metrics | (not wired by default) | — | `expvar` / Prometheus (planned) |
| Distributed traces | OTel spans for LLM calls + future agent hops | OTLP HTTP | OTel collector → Jaeger / Tempo |

---

## Structured logs

The default `observability.StdLogger` writes `[bodh] ...` lines to stdout with microsecond timestamps. Replace it with anything that satisfies the `Logger` interface:

```go
type Logger interface {
    Printf(format string, args ...any)
}
```

### PHI redaction wrapper

For any sink not under a Business Associate Agreement, wrap with `phi.LoggerWrapper`:

```go
inner := observability.NewStdLogger()
redacted := &phi.LoggerWrapper{
    Redactor: phi.NewRedactor(),
    Inner:    inner,
}
env := &orchestration.SimpleEnvironment{
    Logger: redacted,
    Clock:  observability.SystemClock{},
}
```

Default patterns: SSN, MRN, US phone, email, ISO date, US date. Add custom patterns for your deployment:

```go
redactor := phi.NewRedactor()
redactor.Add(phi.Pattern{
    Name:    "internal_mrn",
    Re:      regexp.MustCompile(`\bACME-\d{8}\b`),
    Replace: "[REDACTED-MRN]",
})
```

The redactor is a **first line** — layer it in front of Google Cloud DLP, AWS Macie, or Microsoft Presidio for production-grade DLP.

### Structured-log adapter

For Loki / Cloud Logging / Datadog, wrap zerolog or zap:

```go
type ZapAdapter struct{ Z *zap.SugaredLogger }
func (z *ZapAdapter) Printf(format string, args ...any) {
    z.Z.Infof(format, args...)
}

env := &orchestration.SimpleEnvironment{
    Logger: &ZapAdapter{Z: zapLogger.Sugar()},
    Clock:  observability.SystemClock{},
}
```

---

## Metrics

Today, metrics are observed via log lines (deny rates from policy decisions, success rates from `[llm-trace]`). Production target: `expvar` exposed at `/debug/vars` and a Prometheus exporter via a small wrapper.

Recommended metrics to expose (planned):

| Metric | Type | Labels |
|---|---|---|
| `bodh_messages_total` | counter | `agent_id`, `type`, `tenant_id` |
| `bodh_policy_decisions_total` | counter | `policy`, `decision`, `tenant_id` |
| `bodh_agent_errors_total` | counter | `agent_id` |
| `bodh_llm_calls_total` | counter | `provider`, `model`, `agent_id`, `outcome` |
| `bodh_llm_call_duration_seconds` | histogram | `provider`, `model` |
| `bodh_llm_tokens_total` | counter | `provider`, `model`, `direction` (in/out/cached) |
| `bodh_fallback_total` | counter | `agent_id`, `reason` |
| `bodh_review_queue_depth` | gauge | `priority` |
| `bodh_case_completion_seconds` | histogram | `condition` |
| `bodh_diagnostic_cost_usd` | histogram | `condition`, `provider` |

---

## OpenTelemetry export

PR #7 ships the OTLP exporter behind a `//go:build otel` tag.

### Enabling

```bash
# 1. Populate go.sum for the tagged build (once, with network)
go mod tidy

# 2. Build with the tag
go build -tags otel -o ./care ./cmd/care

# 3. Run with the env var
BODH_OTEL_EXPORT=1 BODH_OTEL_ENDPOINT=http://otel-collector:4318 ./care
```

When `BODH_OTEL_EXPORT` is unset, `cmd/care` uses a no-op stub (no SDK linked at all). The default `go.sum` stays minimal.

### Span shape

Service name: `bodh-care`. Span name: `llm.completion` (or `llm.embedding`). Attributes:

| Attribute | Type | Notes |
|---|---|---|
| `provider` | string | `anthropic` / `openai` / `ollama` / `voyage` |
| `model` | string | e.g. `claude-sonnet-4-6` |
| `agent_id` | string | `diagnostician`, `questioner`, ... |
| `case_id` | string | **sha256 prefix (16 hex)**, never raw — PHI guarantee |
| `trace_id` | string | for correlation with HTTP traces |
| `input_tokens`, `output_tokens`, `cached_tokens` | int | from LLM provider response |
| `stop_reason` | string | `end_turn`, `tool_use`, `max_tokens`, ... |
| `tool_calls` | int | count of tools invoked, if any |
| `kind` | string | `completion` / `embedding` |

Span start = `LLMTrace.StartedAt`. Span end = start + `DurationMs`.

### Collector config (example)

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

exporters:
  loki:
    endpoint: http://loki:3100/loki/api/v1/push
  prometheus:
    endpoint: 0.0.0.0:9464
  otlp/tempo:
    endpoint: tempo:4317
    tls: { insecure: true }

processors:
  batch: {}
  attributes/strip_phi:
    actions:
      - { key: msg.content, action: delete }
      - { key: msg.metadata.patient_name, action: delete }

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [attributes/strip_phi, batch]
      exporters: [otlp/tempo]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [prometheus]
```

The `attributes/strip_phi` processor is **defence in depth** — Bodh's exporter already hashes `case_id` and never emits content, but a collector-level strip catches misconfigured custom spans.

---

## LLM call tracing

Every `llm.Client.Complete` call produces a `[llm-trace]` JSON line on stdout — regardless of whether OTel is enabled.

```
[llm-trace] {"started_at":"2026-05-23T09:14:21Z","duration_ms":2438,"provider":"anthropic","model":"claude-sonnet-4-6","agent_id":"diagnostician","case_id":"demo-case-001","input_tokens":812,"output_tokens":146,"cached_tokens":624,"stop_reason":"tool_use","kind":"completion"}
```

Use this for:

- **Cache hit rate:** `cached_tokens / input_tokens` per provider/model
- **Cost approximation:** tokens × per-million-token price
- **Outlier detection:** `duration_ms` p95 / p99
- **Provider comparison:** group by `provider` and `model`

When OTel is on, the same record is also sent as a span.

---

## Audit pipeline

Per HIPAA §164.312(b), Bodh emits audit events for every clinical message. PR #12 wired this into the orchestrator.

### Event types

| Kind | When | Critical for |
|---|---|---|
| `policy_decision` (allow) | clinical message types passing policy | regulatory traceability — every clinical action attributable |
| `policy_decision` (deny) | any policy denial | security forensics — failed access attempts |
| `message` | every agent invocation | who did what when |
| `agent_error` | `HandleMessage` returned error | reliability + forensics |
| `review_decision` | HITL approve / reject | clinician sign-off provenance |

### Backends in production

```
agent layer
    ↓
orchestrator subscribeAgentLocked
    ↓
audit.Recorder (Postgres-backed in production)
    ↓
audit_events table (append-only at DB grants)
    ↓
hourly/daily export via cron / k8s job
    ↓
WORM sink: S3 Object Lock / Azure Immutable Blob
```

The append-only enforcement at DB grants is the first layer of defence. The WORM sink is the second. Add Merkle chain-hashing for tamper evidence (planned — see [ROADMAP.md](../ROADMAP.md)).

### Retention

| Source | HIPAA target | Operational target |
|---|---|---|
| `audit_events` table | 6 years | Hot for 90 days, then archive |
| WORM sink | 6 years | Cold storage with retention lock |
| Application logs | 30 days | Loki retention class |
| LLM traces | 90 days | OTel collector retention |

---

## Bus implementations

`pkg/comm.Bus` is the interface. Pick an implementation per topology:

### InMemoryBus (default)

- **When:** Demo, single-process pilot
- **Ordering:** No FIFO guarantee per recipient (handlers run in goroutines)
- **Persistence:** None — lost on restart
- **Throughput:** ~100k msg/sec on a laptop, depends on handler cost

### NATS JetStream

- **When:** Cloud-native, mid-scale (recommended)
- **Ordering:** WorkQueue with `max_ack_pending=1` → FIFO per recipient
- **Persistence:** Stream retention configurable (per consumer ACK)
- **Setup:** One stream per logical bus, durable consumers per agent ID

Implementation sketch:

```go
type NATSBus struct {
    nc *nats.Conn
    js jetstream.JetStream
}

func (b *NATSBus) Subscribe(agentID string, h Handler) func() {
    consumer, _ := b.js.CreateConsumer(ctx, "bodh", jetstream.ConsumerConfig{
        Durable:       agentID,
        FilterSubject: fmt.Sprintf("bodh.msg.%s", agentID),
        MaxAckPending: 1,                  // FIFO per recipient
        AckPolicy:     jetstream.AckExplicitPolicy,
    })
    cc, _ := consumer.Consume(func(m jetstream.Msg) {
        var msg protocol.Message
        json.Unmarshal(m.Data(), &msg)
        h(context.Background(), msg)
        m.Ack()
    })
    return func() { cc.Stop() }
}

func (b *NATSBus) Publish(ctx context.Context, msg protocol.Message) {
    data, _ := json.Marshal(msg)
    b.js.Publish(ctx, fmt.Sprintf("bodh.msg.%s", msg.To), data)
}
```

### Kafka

- **When:** Existing Kafka footprint, high throughput
- **Ordering:** FIFO per partition; pin one consumer per partition for total order per recipient
- **Persistence:** Configurable retention (size / time)
- **Setup:** Topic per recipient OR one topic with sticky partitioner keyed on `msg.To`

### RabbitMQ

- **When:** Existing Rabbit deployment
- **Ordering:** FIFO per queue
- **Persistence:** Durable queues
- **Setup:** Direct exchange + one queue per agent ID

---

## LLM reliability

Every LLM-backed agent has a deterministic rule-based fallback. The fallback IS the contract — see [architecture.md § LLM agent fallback contract](architecture.md#llm-agent-fallback-contract).

### Configurable thresholds

```go
type LLMDiagnostician struct {
    Client        llm.Client
    Fallback      *DiagnosticianAgent
    MinConfidence float64        // default 0.6
    Timeout       time.Duration  // default 30s
    MaxRetries    int            // default 1 (one retry on transient errors)
}
```

### Recommended SLOs

| SLI | SLO target | Alert |
|---|---|---|
| LLM call success rate | ≥ 95% | < 90% over 5 min |
| Fallback rate | ≤ 5% | > 10% over 15 min |
| LLM call p99 latency | < 10s | > 15s over 5 min |
| Case completion p95 | < 60s | > 90s over 15 min |
| RPM ingest → alert publish p99 | < 5s | > 10s over 1 min |
| Engagement deliverability | ≥ 98% | < 95% over 1h |
| Panel-rule run latency for 10k patients | < 30s | > 60s |
| Governance deny rate | tracked, alert on spike | 5× baseline |
| Refill auto-approve rate | tracked | drop > 20% |

### Provider failover

For multi-provider robustness, configure the diagnostician factory with provider fallback chains:

```bash
go run ./cmd/care -diagnostician=llm-anthropic-cascade
# Cascade: small/fast model first → fall back to large model on low confidence
# OR -diagnostician=llm-anthropic with the rule-based fallback always present
```

If you want cross-provider failover (Anthropic outage → OpenAI), implement a `MultiProviderClient` that wraps `llm.Client` and tries providers in order. Not shipped today; planned.

---

## Multi-model pool

When Bodh runs on-premise with kronk (local llama.cpp), several models share one GPU — typically a chat diagnostician, a vision diagnostician, and an embedding model. The pool in `pkg/llm/pool` keeps clients resident, with LRU + TTL eviction so idle models release their GPU slots. The pool is opt-in and provider-agnostic; with the default `BODH_LLM_POOL` unset, cmd/care uses the existing single-client path unchanged. See [ai-features.md § Multi-model pool](ai-features.md#multi-model-pool-pkgllmpool) for the design.

### Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `BODH_LLM_POOL` | unset | Set to `1` to enable the pool in `cmd/care`. |
| `BODH_LLM_POOL_MAX` | `0` (unbounded) | Maximum resident clients. For kronk, set to the number of model slots the GPU can hold simultaneously (typically 2–4 on a 24 GB card). |
| `BODH_LLM_POOL_TTL` | `0` (no TTL) | Idle-eviction interval, parsed by `time.ParseDuration` (e.g. `15m`, `1h`). For kronk, a TTL of 10–30 minutes is typical so a model used for one care episode releases its slot before the next shift. |
| `BODH_KRONK_MODELS_DIR` | unset | Directory containing the `.gguf` checkpoint files. The pool loads `<spec.Tag>.gguf` from this directory. |
| `BODH_KRONK_CTX` | `0` (runtime default) | Max context tokens for kronk-loaded models. |
| `BODH_KRONK_GPU_LAYERS` | `-1` (all) | Number of model layers offloaded to GPU. `0` runs entirely on CPU. |

### MaxResident sizing for GPU

A reasonable starting point on a single-GPU host:

| GPU memory | Suggested `BODH_LLM_POOL_MAX` | Notes |
|---|---|---|
| 12 GB (consumer) | `1` | Run one model at a time; rely on TTL eviction to swap. |
| 24 GB (RTX 4090 / A10) | `2` | Chat + embeddings comfortably; vision must swap in. |
| 40 GB (A100 40 GB) | `3` | Chat + vision + embeddings resident; TTL of 30 min is fine. |
| 80 GB (A100 / H100 80 GB) | `4`–`6` | Multi-tier cascade resident; TTL becomes optional. |

The exact number depends on model size and quantization. Q4_K_M weights run roughly 0.6× the FP16 footprint; Q8_0 is roughly 1.1×. Always leave headroom for the KV cache (a 32k context on a 70B model is its own ~3 GB).

### Operational signals

The pool exposes `Stats()` (resident count, hits, misses, evictions, last-eviction time, hit rate). When the rate of evictions stays high under steady load, `BODH_LLM_POOL_MAX` is too small for the workload — either raise it or accept the slowdown. When hit rate stays below 80%, the workload is touching more distinct specs than the pool can keep; consider sharding by tenant or by case-type.

### Failure modes

- **Factory error on miss** — `Get` returns the wrapped error. The pool does not retry; the caller (typically `LLMDiagnostician`) falls through to the rule-based agent per the LLM fallback contract.
- **`pool.ErrKronkNotBuilt`** — the binary was not compiled with `-tags kronk` but a kronk spec was requested. Operator action: rebuild with the tag, or remove the `"kronk"` route from the deployment.
- **Slow `Closer.Close()`** — closers are called under the pool lock. A kronk Unload that takes 5+ seconds will visibly block other `Get` calls. Tune kronk's `unload_timeout` to fail fast and log the orphaned GPU slot.
- **`Get` blocked on full pool** — every entry is pinned. Usually means every agent thread is holding a `Client` without `Release`; check for missing `defer pool.Release(spec)` in custom factories.

---

## SLI / SLO recommendations

Beyond the LLM-specific SLOs above, for the platform layer:

| SLI | Suggested SLO |
|---|---|
| `cmd/care` `/healthz` success rate | 99.9% |
| `cmd/care` `/readyz` success rate | 99.5% |
| End-to-end diagnostic case duration p95 | < 60s |
| Audit write success rate (in-memory) | 99.99% |
| Audit write success rate (Postgres) | 99.9% |
| Audit write p99 latency | < 50ms |
| HITL review SLA met (decisions within `hitl_sla_minutes`) | ≥ 90% |
| Postgres connection pool exhaustion events | 0 / day |

---

## Runbooks

### "Bodh is denying every clinical message"

Symptom: Logs flooded with `message X denied by policy: ...`.

Triage:
1. Inspect the deny reasons: `kubectl logs deploy/bodh-care | grep "denied by policy" | head`
2. Common causes:
   - `case_id required for clinical messages` — caller isn't propagating `case_id` in metadata. Fix the caller (or temporarily relax `RequireCaseIDPolicy` to log instead of deny).
   - `tenant_id required` — `RequireTenantPolicy` enabled but caller isn't setting `tenant_id`. Verify the API gateway is injecting it from the JWT.
   - `test would exceed diagnostic budget` — `MaxDiagnosticCostPolicy` is doing its job; check the case's `max_cost_usd` metadata.
3. Audit the events: `curl localhost:8088/audit?kind=policy_decision&limit=50 | jq '.[] | select(.decision=="deny")'`

### "LLM fallback rate spiked"

Symptom: `[llm-trace]` lines showing `stop_reason=error`; `bodh_fallback_total` increasing.

Triage:
1. Check provider status page (Anthropic / OpenAI / your endpoint).
2. Check network egress: `kubectl exec -it bodh-care-xxx -- curl -s api.anthropic.com/v1/messages`
3. Check API key validity: `kubectl get secret bodh-llm -o yaml`
4. If transient, the rule-based fallback handles it — case still finalizes; investigate without firefighting.
5. If persistent, consider switching providers via `-diagnostician=llm-openai` (cmd/care restart).

### "Postgres audit_events row count is exploding"

Symptom: Disk usage on Postgres instance growing rapidly.

Triage:
1. Check current row count: `psql ... -c "SELECT count(*) FROM audit_events;"`
2. Check rate: `psql ... -c "SELECT date_trunc('hour', at), count(*) FROM audit_events GROUP BY 1 ORDER BY 1 DESC LIMIT 24;"`
3. Identify hot tenant: `psql ... -c "SELECT tenant_id, count(*) FROM audit_events GROUP BY 1 ORDER BY 2 DESC LIMIT 10;"`
4. Common cause: a misconfigured policy that's emitting allow events for **every** message type (not just clinical). Check `isClinicalType` in `pkg/orchestration/orchestrator.go`.
5. Mitigation: archive old rows to S3, run `VACUUM`, consider partitioning by `tenant_id` + month.

### "HITL queue is backing up"

Symptom: `bodh_review_queue_depth` growing; SLA breaches.

Triage:
1. `curl localhost:8088/reviews?status=pending | jq 'length'`
2. Reviewer staffing? Add reviewers via your UI / API.
3. False-positive HITL gates? If certain `hitl_kind` values are 99% auto-approvable, consider relaxing the agent's HITL trigger.
4. SLA-expired reviews stay in the queue with `status=expired` — clean periodically or auto-reject by policy.

### "Case isn't finalizing"

Symptom: A specific case doesn't reach `diagnosis` terminal log.

Triage:
1. Filter audit by case: `curl 'localhost:8088/audit?limit=200' | jq '.[] | select(.case_id=="case-X")'`
2. Identify last message — what type, who emitted it, who was the intended recipient?
3. Common dead-ends:
   - Reasoning verifier reject path (the documented `demo_uri.json` issue — tracked follow-up)
   - Cost guardian deny + max-rework rounds exceeded
   - Pending HITL review
4. Use `bodhctl reviews pending --case=case-X` to check HITL state.
