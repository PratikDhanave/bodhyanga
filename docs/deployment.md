# Bodhyanga — Deployment Guide

How to run Bodhyanga in development, in pilot, and in production. Targeted at engineers and platform operators.

> **Status disclaimer.** Bodhyanga is a research / engineering reference. It is **not** a medical device, **not** approved for clinical use, **not** under a Business Associate Agreement. The deployment topologies described here are architectural targets — not operational claims. See [docs/compliance.md](compliance.md) for the regulatory caveats.

---

## Contents

- [Topology decision tree](#topology-decision-tree)
- [Development (local)](#development-local)
- [Single binary + Postgres](#single-binary--postgres)
- [Container image](#container-image)
- [Kubernetes](#kubernetes)
- [Multi-tenant cells](#multi-tenant-cells)
- [Environment variables](#environment-variables)
- [Backup, DR, business continuity](#backup-dr-business-continuity)
- [Scaling and tenancy](#scaling-and-tenancy)
- [HIPAA / BAA chain](#hipaa--baa-chain)

---

## Topology decision tree

| Use case | Topology |
|---|---|
| Reading the code, running tests | **Local** — `go run ./cmd/demo` |
| Demo to a stakeholder | **Local** — `go run ./cmd/caredemo` then `cmd/care -addr=:8088` |
| Single-tenant pilot, no PHI | **Single binary + Postgres**, in-memory engagement |
| Single-tenant pilot, real PHI | **Single binary + Postgres + Twilio (BAA) + Loki (BAA)** behind an API gateway with OIDC |
| Multi-tenant SaaS, small fleet | **K8s deployment + Postgres RLS + NATS JetStream + OTel collector** |
| Large multi-tenant SaaS | **Cell architecture** — per-tenant K8s namespace + bus + DB |
| Hospital integration | Single binary + HL7 v2 endpoint behind hospital VPN + FHIR adapter for read-back |

---

## Development (local)

### Prereqs

- Go 1.22+ (1.26 recommended)
- (Optional) Docker — for Postgres integration tests and the OTel collector smoke test
- (Optional) Ollama — for the local LLM provider

### Common commands

```bash
git clone https://github.com/PratikDhanave/bodhyanga.git
cd bodhyanga

# Diagnostic flow (rule-based)
go run ./cmd/demo
go run ./cmd/demo -case=data/cases/demo_chf.json

# Care services (all 5 workflows, HITL on)
go run ./cmd/caredemo
go run ./cmd/caredemo -workflow=cdm  # or tcm / panel / virtual_office / adt

# HTTP API
go run ./cmd/care -addr=:8088

# SD-Bench runner
go run ./cmd/bench -manifest=data/cases/manifest.json -providers=rule

# LLM-backed diagnostician
export ANTHROPIC_API_KEY=sk-ant-...
go run ./cmd/demo -diagnostician=llm-anthropic

# Compare providers
go run ./cmd/bench -providers=rule,llm-anthropic,llm-openai,llm-ollama \
  -manifest=data/cases/manifest.json

# Operator CLI
go install ./cmd/bodhyangactl
bodhyangactl reviews pending
bodhyangactl reviews approve <id>

# Full validation
make all       # vet + test + build
make ci        # + govulncheck
```

### Smoke checks

```bash
# Liveness
curl localhost:8088/healthz

# Submit a presentation
curl -s -X POST localhost:8088/cdm \
  -H 'content-type: application/json' \
  -d '{"patient_id":"pt-001","condition":"dm","case_id":"case-001"}'

# Inspect audit (PR #12)
curl -s localhost:8088/audit?limit=10 | jq .

# Inspect tasks
curl -s localhost:8088/tasks | jq .
```

---

## Single binary + Postgres

The first step beyond pure-in-memory. Adds durable persistence + tenant isolation via PostgreSQL row-level security.

### 1. Provision Postgres

Any Postgres 14+ instance works. Required: `superuser` access to run migrations, and a `bodhyanga_app` role created by the migrations.

```bash
# Local docker postgres
docker run -d --name bodhyanga-pg -p 5432:5432 \
  -e POSTGRES_PASSWORD=postgres \
  postgres:16

# Create the database
psql -h localhost -U postgres -c "CREATE DATABASE bodhyanga;"
```

### 2. Run migrations

Migrations live in two places (kept in sync; `go:embed` can't reach `../../../db/`):

- `db/migrations/` — canonical
- `pkg/persistence/postgres/migrations/` — embedded, used by the in-process runner

The in-process runner runs on `pkg/persistence/postgres.New(...)` boot. For a one-shot manual application:

```bash
psql -h localhost -U postgres -d bodhyanga -f db/migrations/0001_init.sql
psql -h localhost -U postgres -d bodhyanga -f db/migrations/0002_rls.sql
```

### 3. Start cmd/care pointing at Postgres

```bash
export BODHYANGA_DB_DSN="postgres://bodhyanga_app:strong-password@localhost:5432/bodhyanga?sslmode=disable"
export BODHYANGA_TENANT_ID="tenant-pilot"  # single-tenant binary scopes everything to this
go run ./cmd/care -addr=:8088
```

Startup log includes `using postgres backend` on success, with the active tenant.

### 4. Verify tenant isolation

The Postgres tests in `pkg/persistence/postgres/postgres_test.go` cover this. The proof: insert two patients with the same `patient_id` under different `tenant_id` values; each tenant's `List` returns only their row. A BYPASSRLS sanity check fails the test if RLS regresses.

### Schema overview

| Table | Purpose | Append-only? |
|---|---|---|
| `interaction_records` | Per-case eval metrics (accuracy, cost, steps) | No (updates / deletes allowed) |
| `patient_records` | Population roster | No |
| `audit_events` | HIPAA §164.312(b) audit log | **Yes** — SELECT + INSERT grants only at DB |

PHI columns are typed `BYTEA` with `_ct` suffix marking the future column-level encryption boundary. They hold plaintext JSON today; pgcrypto / KMS envelope encryption is the next PR.

See [pkg/persistence/postgres/README.md](../pkg/persistence/postgres/README.md) for full schema details, RLS policies, and the GDPR Article 17 hard-delete path.

---

## Container image

Minimal Dockerfile (planned; the agent-spawned PR #10 builds a static binary that lands at `./care`):

```dockerfile
# Build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-w -s' -o /out/care ./cmd/care

# Run stage — distroless static
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/care /care
USER nonroot:nonroot
EXPOSE 8088
ENTRYPOINT ["/care"]
```

Build with OTel support enabled:

```bash
docker build --build-arg GO_BUILD_TAGS=otel -t bodhyanga-care:latest .
```

(Add the build-tag arg to your Dockerfile.)

---

## Kubernetes

A minimal Deployment + Service + ConfigMap + SealedSecret. Replace placeholders for your environment.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bodhyanga-care
  namespace: bodhyanga
spec:
  replicas: 3
  selector:
    matchLabels: { app: bodhyanga-care }
  template:
    metadata:
      labels: { app: bodhyanga-care }
    spec:
      serviceAccountName: bodhyanga-care
      containers:
        - name: care
          image: ghcr.io/your-org/bodhyanga-care:latest
          ports:
            - containerPort: 8088
          env:
            - name: BODHYANGA_LISTEN_ADDR
              value: ":8088"
            - name: BODHYANGA_DB_DSN
              valueFrom:
                secretKeyRef: { name: bodhyanga-db, key: dsn }
            - name: BODHYANGA_TENANT_ID
              valueFrom:
                configMapKeyRef: { name: bodhyanga-config, key: tenantId }
            - name: BODHYANGA_OTEL_EXPORT
              value: "1"
            - name: BODHYANGA_OTEL_ENDPOINT
              value: "http://otel-collector.observability:4318"
            - name: ANTHROPIC_API_KEY
              valueFrom:
                secretKeyRef: { name: bodhyanga-llm, key: anthropic }
          readinessProbe:
            httpGet: { path: /readyz, port: 8088 }
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet: { path: /healthz, port: 8088 }
            initialDelaySeconds: 15
            periodSeconds: 30
          resources:
            requests: { cpu: 100m, memory: 128Mi }
            limits: { cpu: 500m, memory: 512Mi }
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            capabilities: { drop: ["ALL"] }
---
apiVersion: v1
kind: Service
metadata:
  name: bodhyanga-care
  namespace: bodhyanga
spec:
  selector: { app: bodhyanga-care }
  ports:
    - port: 8088
      targetPort: 8088
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: bodhyanga-care
  namespace: bodhyanga
spec:
  podSelector:
    matchLabels: { app: bodhyanga-care }
  policyTypes: ["Ingress", "Egress"]
  ingress:
    - from:
        - podSelector:
            matchLabels: { app: api-gateway }
  egress:
    - to:
        - podSelector:
            matchLabels: { app: postgres }
      ports:
        - port: 5432
    - to:
        - namespaceSelector:
            matchLabels: { name: observability }
      ports:
        - port: 4318  # OTel HTTP
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except: [10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16]
      ports:
        - port: 443  # LLM provider HTTPS
```

The `NetworkPolicy` is default-deny except for: ingress from the API gateway, egress to Postgres + OTel collector + LLM provider HTTPS. Tighten to specific LLM provider CIDRs for the most restrictive deployments.

---

## Multi-tenant cells

For the largest scale, isolate tenant cells with their own bus + DB + monitoring; blast radius is bounded to one cell.

```
                  ┌──────────────────┐
                  │  Gateway / IdP   │
                  │ tenant routing → │
                  └──────┬───────────┘
       ┌──────────┬──────┴───────┬──────────┐
       │          │              │          │
       ▼          ▼              ▼          ▼
   ┌─────────┐ ┌─────────┐  ┌─────────┐ ┌─────────┐
   │ Cell A  │ │ Cell B  │  │ Cell C  │ │ Cell D  │
   │ (T1-50) │ │ (T51-90)│  │  (T91)  │ │  (T92)  │
   └─────────┘ └─────────┘  └─────────┘ └─────────┘
   each cell: K8s namespace + Postgres + NATS + OTel
```

Cell sizing: typically 50-100 small tenants per shared cell, or one cell per large enterprise tenant. The gateway routes by tenant ID (host header, JWT claim, or path prefix).

Inside a cell, RLS still enforces per-tenant isolation. Cell boundaries protect against blast radius (DB corruption, bus partition, runaway tenant).

---

## Environment variables

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `BODHYANGA_LISTEN_ADDR` | no | `:8088` | HTTP listen address |
| `BODHYANGA_TENANT_ID` | when DB-backed | (none) | Single-tenant binary identity |
| `BODHYANGA_DB_DSN` | no | (in-memory) | Postgres DSN; selects backend at boot |
| `BODHYANGA_OTEL_EXPORT` | no | `0` | Set `1` to enable OTLP export (build must include `-tags otel`) |
| `BODHYANGA_OTEL_ENDPOINT` | no | `http://localhost:4318` | OTLP collector HTTP endpoint |
| `BODHYANGA_KNOWLEDGE_CORPUS` | no | (RAG disabled) | Path to RAG corpus JSON |
| `BODHYANGA_LOG_LEVEL` | no | `info` | `debug` / `info` / `warn` / `error` |
| `ANTHROPIC_API_KEY` | for `llm-anthropic` | (none) | Anthropic Messages API key |
| `OPENAI_API_KEY` | for `llm-openai` | (none) | OpenAI API key |
| `OLLAMA_HOST` | for `llm-ollama` | `http://localhost:11434` | Local Ollama URL |
| `VOYAGE_API_KEY` | for Anthropic-path RAG embeddings | (none) | Voyage key |
| `BODHYANGA_ANTHROPIC_MODEL` | no | `claude-sonnet-4-6` | Override Anthropic chat model |
| `BODHYANGA_OPENAI_MODEL` | no | `gpt-4o` | Override OpenAI chat model |
| `BODHYANGA_OLLAMA_MODEL` | no | `llama3.2` | Override Ollama chat model |
| `BODHYANGA_OPENAI_EMBED_MODEL` | no | `text-embedding-3-small` | Override OpenAI embedding model |
| `BODHYANGA_OLLAMA_EMBED_MODEL` | no | `nomic-embed-text` | Override Ollama embedding model |
| `BODHYANGA_VOYAGE_EMBED_MODEL` | no | `voyage-3` | Override Voyage embedding model |

Production (planned env additions): `BODHYANGA_TLS_CERT`, `BODHYANGA_TLS_KEY`, `BODHYANGA_OIDC_ISSUER`, `BODHYANGA_OIDC_AUDIENCE`, `BODHYANGA_BUS_URL` (Kafka/NATS), `BODHYANGA_MAX_COST_USD` (override default budget).

---

## Backup, DR, business continuity

| Concern | Target | Mechanism |
|---|---|---|
| RPO (max acceptable data loss) | 15 min | Postgres continuous WAL archiving |
| RTO (max acceptable downtime) | 4 h full / 30 min read-only | Hot standby + read replicas |
| Backup cadence | Continuous WAL + daily snapshot | pgBackRest / WAL-G / cloud-managed |
| Backup encryption | KMS-managed AES-256-GCM | Cloud KMS / Vault Transit |
| Backup retention | 30 days hot, 6 years cold (HIPAA) | Lifecycle policy to cold storage |
| Cross-region copy | Quarterly | Provider-native (S3 CRR / Azure GRS) |
| Audit log retention | 6 years | WORM sink (S3 Object Lock, Azure Immutable Blob) |
| DR drill | Quarterly tabletop, annual game-day | Documented runbook |
| Patient data right-to-delete | On request | `*Store.DeletePatient(ctx, tenantID, patientID)` |

For the WORM audit sink, the `pkg/audit.FileStore` ships a file-backed implementation as a starting point; production forwards events from there to S3 Object Lock or Azure Immutable Blob via a sidecar / Vector / Fluent Bit pipeline.

---

## Scaling and tenancy

### Stateless agents

Agents hold no per-request state — they read from `Environment` and injected stores. Horizontal scaling is trivial: bump `replicas` in the Deployment.

### Bus throughput

`InMemoryBus` is per-process. Production bus must be horizontally scalable:

- **NATS JetStream** — recommended for cloud-native, low-ops deployments. WorkQueue policy + `max_ack_pending=1` gives FIFO per recipient.
- **Kafka** — recommended when an existing Kafka footprint exists. Topic per recipient with single-partition or sticky-partition strategy.
- **RabbitMQ** — direct exchange + queue per recipient.

The swap is a `pkg/comm.Bus` implementation. Zero agent code changes.

### Sticky tenancy

Partition by `tenant_id` at the bus level. Inside a cell, Postgres row-level security still enforces isolation as defence in depth. The gateway is responsible for tenant routing; the application is responsible for honouring the tenant context (RLS + `RequireTenantPolicy`).

### LLM rate limiting

LLM providers rate-limit per API key. For multi-tenant deployments, plan for either:

- **Shared key + per-tenant rate limit** in the application (use a sliding window per `tenant_id`). Risk: noisy neighbour.
- **Key per tenant** — cleaner but costs more in subscription management.

The cascade pattern (cheap small model first, escalate on low confidence) reduces effective QPS to the expensive model.

---

## HIPAA / BAA chain

If a covered entity deploys Bodhyanga, the operator becomes a business associate under 45 CFR §164.504(e). BAAs required with:

| Subcontractor | Notes |
|---|---|
| Cloud provider (AWS / Azure / GCP) | Use only HIPAA-eligible services; sign their BAA |
| Twilio / SendGrid | Engagement notifier — BAA before sending any PHI in SMS / email |
| Datadog / New Relic | Telemetry — BAA before piping logs / traces; redact PHI even with BAA in place |
| OpenAI | Zero Data Retention agreement (separate from generic BAA) |
| Azure OpenAI | Standard Azure BAA covers; check service availability per region |
| Anthropic | BAA available via API on request |
| Postgres host (managed) | Cloud provider BAA covers managed Postgres on AWS/Azure/GCP |

See [docs/compliance.md](compliance.md) for the full HIPAA Privacy / Security / Breach Notification crosswalk, GDPR mapping, and de-identification model.

---

## Quick reference — single-tenant pilot

```bash
# 1. Postgres
docker run -d --name bodhyanga-pg -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:16
psql -h localhost -U postgres -c "CREATE DATABASE bodhyanga;"
psql -h localhost -U postgres -d bodhyanga -f db/migrations/0001_init.sql
psql -h localhost -U postgres -d bodhyanga -f db/migrations/0002_rls.sql

# 2. OTel collector (optional)
docker run -d --name otel-collector -p 4318:4318 \
  otel/opentelemetry-collector:latest

# 3. Build with OTel + start
go build -tags otel -o ./care ./cmd/care
BODHYANGA_DB_DSN="postgres://bodhyanga_app:postgres@localhost:5432/bodhyanga?sslmode=disable" \
BODHYANGA_TENANT_ID="pilot" \
BODHYANGA_OTEL_EXPORT=1 \
ANTHROPIC_API_KEY=sk-ant-... \
./care -addr=:8088

# 4. Smoke
curl localhost:8088/healthz
curl -X POST localhost:8088/cdm -H 'content-type: application/json' \
  -d '{"patient_id":"pt-001","condition":"dm","case_id":"case-pilot-001"}'
curl -s localhost:8088/audit?limit=5 | jq .
```
