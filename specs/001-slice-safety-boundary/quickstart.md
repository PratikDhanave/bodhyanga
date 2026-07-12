# Quickstart: validating the Slice Safety Boundary

Prerequisites: Go 1.26; Docker (testcontainers + local Postgres); `../bodh` checked
out (local replace). Optional for live-LLM runs: `ANTHROPIC_API_KEY` or Ollama at
`$OLLAMA_HOST`.

## 1. Automated proof (all five exit criteria)

```bash
go test ./...                       # unit + admission + audit-trail contracts
go test ./pkg/ckptpg -run TestKillResume -v   # durable HITL (testcontainers)
```

Expected: green; no network calls except the ephemeral Postgres container.

## 2. Admission fidelity by hand (SC-001)

```bash
go run ./cmd/slice -policy allow            # completes; exit 0
go run ./cmd/slice -policy deny             # halts at intake; exit 2; deny in trail
go run ./cmd/slice -policy deny-agent=care_plan   # halts only at care_plan
```

## 3. Durable physician review by hand (SC-003)

```bash
docker compose -f zarf/compose.yml up -d postgres   # or any local PG + migration
export SLICE_DB=postgres://slice:slice@localhost:5432/slice

go run ./cmd/slice -db "$SLICE_DB"          # pauses: prints session + request ID; exit 3
# simulate redeploy: nothing to keep alive — the process already exited
go run ./cmd/slice -db "$SLICE_DB" -resume <session> -approve -reviewer dr-a
# → care plan printed; trail shows review_decision/approve spanning two lifetimes
```

Reject path: `-reject -reviewer dr-a` → no care plan; eval record Success=false.

## 4. Engine swap (SC-004)

```bash
go run ./cmd/slice -diagnostician rule
go run ./cmd/slice -diagnostician llm-ollama      # needs OLLAMA_HOST
go run ./cmd/slice -diagnostician llm-anthropic   # needs ANTHROPIC_API_KEY
```

Expected: identical output structure and trail shape; only the diagnosis content may
differ. Automated tests cover the LLM path with a fake provider — these live runs are
optional verification, never CI.

## 5. Eval mirror (SC-005)

After any completed run the CLI prints the eval summary; assert one record per case:
`Scenario=mara-slice/<token>`, metrics `confidence/rounds/forced`, review decision in
metadata. (In tests: read `eval.Store.List()`.)

Full contracts: [admission-and-review](contracts/admission-and-review.md) ·
[audit-trail](contracts/audit-trail.md) ·
[checkpoint-postgres](contracts/checkpoint-postgres.md) ·
data model: [data-model.md](data-model.md)
