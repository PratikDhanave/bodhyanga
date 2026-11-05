# Contributing to Bodhyanga

Thanks for being here. Bodhyanga is a research and engineering reference for medical multi-agent systems — every contribution helps the field even when it doesn't ship into a hospital.

> Before you start: this is **not** clinical software. Nothing you contribute will be deployed for patient care without independent regulatory and clinical validation. If that bar matters to you, contribute confidently — the architecture is designed to support it; getting there is the work.

---

## Table of contents

- [Setup](#setup)
- [Where to start](#where-to-start)
- [Project layout](#project-layout)
- [Design rules from MARA](#design-rules-from-mara)
- [Workflow](#workflow)
- [Adding an agent](#adding-an-agent)
- [Adding a policy](#adding-a-policy)
- [Adding a condition](#adding-a-condition)
- [Adding a test fixture](#adding-a-test-fixture)
- [Writing tests](#writing-tests)
- [Code style](#code-style)
- [Commit and PR conventions](#commit-and-pr-conventions)
- [Documentation expectations](#documentation-expectations)
- [Diagram hygiene](#diagram-hygiene)
- [Reporting bugs](#reporting-bugs)
- [Reporting a security issue](#reporting-a-security-issue)

---

## Setup

```bash
git clone https://github.com/PratikDhanave/bodhyanga.git
cd bodhyanga
go version            # 1.22+ required, 1.26 recommended
go test ./...         # confirm green baseline before changing anything
make ci               # vet + test + govulncheck
```

Optional for LLM and integration work:

```bash
# Anthropic / OpenAI / Ollama
export ANTHROPIC_API_KEY=sk-ant-...
ollama pull llama3.2 && ollama pull nomic-embed-text   # for llm-ollama

# Docker — for Postgres integration tests
docker --version
```

---

## Where to start

Look at the [ROADMAP.md](ROADMAP.md) for the current phase. The **"good first issues"** in the issue tracker are scoped to be doable in a single PR by someone who's read [docs/architecture.md](docs/architecture.md) once.

Patterns we welcome most:

- **A new condition** — easiest entry point. Mirror the asthma PR (see [Adding a condition](#adding-a-condition)).
- **A new specialty rule for `diagnostician`** — high leverage on bench numbers.
- **A new policy in `pkg/governance`** — narrow scope, immediately useful.
- **A new agent for a clinical workflow** — for example a `pharmacist` agent that checks drug-drug interactions before the engagement layer sends a refill response.
- **A clinical fixture** that exposes a current rule-path gap — file an issue first describing the gap.

Patterns to discuss in an issue **before** writing:

- Changes to `pkg/protocol` or `pkg/agent` (the wire format / contract).
- Changes to `pkg/orchestration` beyond what's required to wire a new sink.
- New bus / registry implementations — design discussion first.
- LLM model defaults — verify on the bench before swapping.
- Anything that touches `pkg/audit` field selection — the "enough to reconstruct, never enough to leak" rule is load-bearing.

---

## Project layout

```
bodhyanga/
├── cmd/                     entry points
│   ├── demo/                Sequential diagnosis
│   ├── caredemo/            Care services
│   ├── care/                HTTP API server
│   ├── bench/               SD-Bench runner
│   ├── bodhyangactl/             Operator CLI
│   └── export-hitl-feedback/  HITL feedback export
├── agents/                  agent implementations
├── pkg/                     reusable packages
│   ├── protocol/            Message
│   ├── agent/               Agent + Environment interfaces
│   ├── registry/            agent discovery
│   ├── comm/                bus (in-memory + future swaps)
│   ├── orchestration/       Orchestrator + Environment impl
│   ├── governance/          Policy + the shipped/planned policies
│   ├── medical/             clinical domain types
│   ├── tasks/               nurse work queue
│   ├── engagement/          SMS/email/IVR notifier
│   ├── fhir/                FHIR R4 adapter
│   ├── hl7v2/               HL7 v2 ADT parser
│   ├── analytics/           cohort metrics
│   ├── persistence/         file + Postgres stores
│   ├── memory/              short-term KV
│   ├── observability/       logger + clock + LLMTrace
│   ├── eval/                InteractionRecord, CaseMetrics, LLMJudge, redteam
│   ├── hitl/                HITL queue
│   ├── phi/                 PHI redaction
│   ├── audit/               append-only audit recorder
│   ├── llm/                 provider-agnostic LLM client
│   ├── embeddings/          embedding adapters
│   ├── knowledge/           RAG store (hybrid vector + BM25)
│   ├── safety/              guardrails (refusal, hallucination, PHI patterns)
│   └── tools/               MCP-style tools for tool-using agents
├── data/
│   ├── cases/               bench fixtures + manifest + baseline
│   ├── eval/                red-team cases
│   └── knowledge/           RAG corpus
├── db/migrations/           Postgres schema + RLS
├── docs/                    deep documentation
└── .github/workflows/       CI
```

---

## Design rules from MARA

Internalise these. They're not style preferences — they're what makes the system testable, auditable, and safe to evolve.

1. **Orchestrator coordinates; agents do not call each other.** Use `Message.To`.
2. **One agent = one meaningful clinical capability**, not one FHIR endpoint.
3. **Registry for discovery; bus for delivery.** Do not mix responsibilities.
4. **Governance runs before `HandleMessage`.** Fail closed.
5. **Prefer message-driven flows** for multi-step diagnosis; supervisor agent plans steps.
6. **Version agents and prompts**; rollback when eval metrics drop.
7. **Clinical safety:** outputs are decision **support**, not autonomous care; human attestation required for production.
8. **Cost-aware:** explicit `max_cost_usd`; avoid defaulting to "order everything."
9. **PHI hygiene:** opaque `case_id`; never log full narrative in production info-level logs.
10. **Standards-first:** prefer FHIR / HL7 / SNOMED / LOINC over custom shapes when integrating externally.

Violations get caught in review. If a rule feels wrong, open a discussion issue — the rules are negotiable, but the burden of proof is on the change.

---

## Workflow

1. **Find or open an issue** describing the change. For non-trivial work, get a thumbs-up from a maintainer before you start coding.
2. **Branch off `main`** with a descriptive name: `feat/asthma-care-plan`, `fix/uri-verifier-finalize`, `docs/architecture-deep-dive`.
3. **Write the code, then the test, then the docs.** All three land in the same PR. Untested PRs get bounced.
4. **Run the full check:**
   ```bash
   gofmt -l .                # must produce no output
   go vet ./...              # clean
   go test ./...             # all green
   make ci                   # vet + test + govulncheck
   go run ./cmd/bench -fail-on-regression -providers=rule   # if you touched anything diagnostic
   ```
5. **Commit** with a conventional-commit-style message (see [Commit and PR conventions](#commit-and-pr-conventions)).
6. **Push and open a PR** with a body that includes summary, design notes, test plan, deferred items, and a screenshot/log excerpt where it helps.
7. **Respond to review.** Push fixups; squash on merge.

---

## Adding an agent

The asthma PR (#6) is a good "no platform changes needed" template, but it's an extension, not a new agent. For a true new agent, see this walkthrough.

### 1. Create the file

`agents/your_agent.go`:

```go
package agents

import (
    "context"

    "github.com/PratikDhanave/bodhyanga/pkg/agent"
    "github.com/PratikDhanave/bodhyanga/pkg/medical"
)

type YourAgent struct {
    // dependency-injected state (e.g. *medical.PatientRegistry)
}

func (a *YourAgent) ID() string   { return "your_agent_id" }
func (a *YourAgent) Name() string { return "Your Agent (Human Label)" }
func (a *YourAgent) Capabilities() []string {
    return []string{"capability_one", "capability_two"}
}

func (a *YourAgent) HandleMessage(
    ctx context.Context, msg agent.Message, env agent.Environment,
) ([]agent.Message, error) {
    state, err := medical.DecodeState(msg.Content)
    if err != nil {
        return nil, err
    }
    env.Logf("[your_agent_id] processing case=%s", state.CaseID)

    // ... your logic ...

    out := agent.NewMessage(
        a.ID(),
        "next_agent_id",
        agent.RoleAgent,
        "your_output_type",
        medical.EncodeState(state),
        copyMeta(msg.Metadata),
    )
    return []agent.Message{out}, nil
}
```

### 2. Write a doc.go

`agents/your_agent_doc.go`:

```go
// Package agents — your_agent.
//
// YourAgent does [one specific thing]. It consumes [input type], performs
// [logic], and emits [output type] addressed to [next agent].
//
// HITL gate: when HITL=true, the output is wrapped as a review_request
// with hitl_kind=[your_review_kind].
//
// Failure modes:
//   - Bad input JSON → returns error, supervisor handles
//   - [other modes]
package agents
```

Bodhyanga uses split doc files (one per source file) so `go doc` finds them and so each file's purpose is documented in isolation.

### 3. Write the test

`agents/your_agent_test.go`:

```go
package agents

import (
    "context"
    "testing"

    "github.com/PratikDhanave/bodhyanga/pkg/agent"
    "github.com/PratikDhanave/bodhyanga/pkg/observability"
    "github.com/PratikDhanave/bodhyanga/pkg/orchestration"
)

func TestYourAgent_HandleMessage(t *testing.T) {
    cases := []struct {
        name   string
        input  string
        want   string
        wantErr bool
    }{
        // table-driven cases
    }
    a := &YourAgent{}
    env := &orchestration.SimpleEnvironment{Logger: observability.NewStdLogger()}
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            msg := agent.NewMessage("test", a.ID(), agent.RoleUser, "your_input_type", tc.input, nil)
            out, err := a.HandleMessage(context.Background(), msg, env)
            if (err != nil) != tc.wantErr {
                t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
            }
            if !tc.wantErr {
                if len(out) != 1 || out[0].Content != tc.want {
                    t.Errorf("got=%q want=%q", out[0].Content, tc.want)
                }
            }
        })
    }
}
```

### 4. Register in the entry point

In `cmd/care/main.go` (and/or `cmd/demo` / `cmd/caredemo` as appropriate):

```go
_ = reg.Register(ctx, &agents.YourAgent{
    // inject any state
})
```

### 5. Route from the supervisor (if needed)

If your agent fits into the diagnostic state machine, add a branch in `DiagnosticSupervisor.routeNext` for your new message type.

If it's a care-services agent, it routes itself by subscribing to its input type.

### 6. Document

Update [docs/agents.md](docs/agents.md) with your agent's row. Update [docs/api.md](docs/api.md) if you added a message type.

### PR checklist

- [ ] Table-driven test in `agents/your_agent_test.go`
- [ ] Doc file `agents/your_agent_doc.go`
- [ ] Capability documented in [docs/agents.md](docs/agents.md)
- [ ] No direct agent-to-agent function calls (use `Message.To`)
- [ ] Governance: `case_id`, budget, content length all respected
- [ ] Logs use `env.Logf`; no raw MRN/SSN
- [ ] Domain types live in `pkg/medical`, not in the agent file
- [ ] If HITL-gated: envelope keys documented

---

## Adding a policy

See [docs/governance.md § Adding a policy](docs/governance.md#adding-a-policy). Summary:

1. Add the type in `pkg/governance/`.
2. Table-driven test in `pkg/governance/medical_policy_test.go`.
3. Document in [docs/governance.md](docs/governance.md).
4. Wire it into the composer in `cmd/care/main.go`.

---

## Adding a condition

See [docs/data-model.md § Adding a condition](docs/data-model.md#adding-a-condition). The asthma PR (#6) is the worked example. Summary:

1. `medical.BuildCarePlan` — new `case "X":` branch.
2. `medical.DefaultPanelRules()` — new rules.
3. `agents/test_planner.go` — new entry in `conditionWorkups` map.
4. `agents/diagnostician.go` — new specialty rule in `inferDiagnosis` with **illustrative-only** comments on thresholds.
5. `pkg/medical/types.go` — new `TestCatalog` entries if needed.
6. Tests + README updates.
7. Add a fixture under `data/cases/` and register in `data/cases/manifest.json`.
8. Re-run bench: `go run ./cmd/bench -update-baseline`.

**Zero platform changes required.** That's the whole point.

---

## Adding a test fixture

1. Create `data/cases/your_case.json` with the `PatientPresentation` JSON shape (see [docs/data-model.md § PatientPresentation](docs/data-model.md#patientpresentation)).
2. Use opaque `case_id` (e.g. `bench-042`). Never put a real MRN or patient name.
3. Add an entry to `data/cases/manifest.json` with `id`, `path`, `gold_diagnosis`, `max_cost_usd`, `condition`, and meaningful `tags`.
4. Run the bench: `go run ./cmd/bench -manifest=data/cases/manifest.json -providers=rule`.
5. If it's intentionally a coverage gap (e.g. PE — currently no rule), tag it `coverage_gap` and don't update the baseline.
6. If it's expected to pass, refresh: `go run ./cmd/bench -update-baseline -providers=rule`.

---

## Writing tests

```bash
go test ./...                              # everything
go test ./pkg/governance/... -v            # one package, verbose
go test -run TestBuildCarePlanAsthma ./pkg/medical/...   # single test
go test -race ./...                        # race detector — should be clean
go test -count=10 ./pkg/comm/...           # flake-check
```

Conventions:
- **Table-driven.** Each test takes a slice of cases. Use `t.Run(tc.name, ...)` for sub-test naming so failures point at the specific case.
- **No `time.Sleep`** in tests. Use channels, contexts, or the fake clock.
- **No network in unit tests.** Mock the LLM client with `llm.Mock`. Integration tests that need network go under build tags or `testing.Short` skips.
- **No PHI** in test data. Synthetic only — opaque IDs, no real-sounding names.
- **Test the reason strings** in policy tests — the audit log reads them.

---

## Code style

- `gofmt -s` — simplified format. CI enforces.
- `go vet` — clean. CI enforces.
- `goimports` recommended for import ordering.
- **No emojis in code or commits** unless the user explicitly asked for them — this is a clinical tool, not a chatbot.
- **No comments restating the obvious.** Comment the *why*, not the *what*.
- **No exported-symbol stutter:** `agents.NewAgent` not `agents.NewAgentsAgent`.

Naming:
- Agent IDs: `snake_case` (`diagnostic_supervisor`, `inbox_triage`)
- Capabilities: `snake_case` (`ask_question`)
- Message types: `snake_case` (`case_state`, `tests_complete`)
- Condition keys: `snake_case` short (`htn`, `dm`, `chf`, `ckd`, `copd`, `asthma`)
- Med classes: `snake_case` (`diuretic`, `inhaled_corticosteroid`)
- Metric keys: `snake_case` (`a1c`, `sbp`, `act_score`)

---

## Commit and PR conventions

### Commit messages

Conventional-commit subject + body:

```
feat(agents): rule-path specialty rules for ACS / sepsis / DKA / CHF

Bench moves 42.9% → 85.7% on the rule provider.

[body explaining what / why / deferred / numbers]

Co-Authored-By: ...
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `build`, `ci`.
Scopes match top-level paths: `agents`, `medical`, `governance`, `bench`, `care`, `hl7v2`, `observability`, etc.

### PR body template

```markdown
## Summary

[1-3 sentences]

## What's in

- [bullet]

## Test plan

- [ ] `go vet ./...`
- [ ] `go test ./...`
- [ ] [feature-specific smoke]

## Bench impact (if applicable)

| | Cases | Accuracy | Mean Cost |
|---|---|---|---|
| before | ... | ... | ... |
| after | ... | ... | ... |

## Deferred

- [bullet]
```

### Squashing

We squash-merge by default — the PR title becomes the squashed commit subject; the PR body becomes the body. Conventional-commit subjects keep main's history readable.

For multi-commit PRs that want their history preserved (rare — usually for very large changes split into reviewable steps), request `Merge` instead of squash in the PR description.

---

## Documentation expectations

Every PR that changes user-visible behaviour updates docs in the same PR:

| Change | Update |
|---|---|
| New agent | [docs/agents.md](docs/agents.md) |
| New HTTP endpoint | [docs/api.md](docs/api.md) |
| New message type | [docs/api.md § Message types](docs/api.md#message-types) |
| New policy | [docs/governance.md](docs/governance.md) |
| New condition | [docs/data-model.md § Adding a condition](docs/data-model.md#adding-a-condition) + README condition list |
| New env var | [docs/deployment.md § Environment variables](docs/deployment.md#environment-variables) |
| New LLM provider | [docs/operations.md](docs/operations.md) + [README AI features](README.md) |
| Changed bench numbers | `data/cases/baseline.json` (only via `-update-baseline`) + PR body |
| Breaking change to public Go API | `CHANGELOG.md` + bump module version |

If you're unsure whether docs need updating, ask in the PR.

---

## Diagram hygiene

If a PR changes platform boxes or agent flow, update:

- [`Architecture.png`](../Architecture.png)
- [`architectureflow.png`](../architectureflow.png)
- The matching mermaid view in [docs/diagrams.md](docs/diagrams.md)

The diagrams are the front door for newcomers — stale diagrams confuse more than no diagram at all.

---

## Reporting bugs

Open an issue with:

- Bodhyanga git SHA / version
- Go version
- Reproduction steps (ideally a failing test or a `curl` against `cmd/care`)
- Expected vs actual behaviour
- Relevant log excerpts (REDACT PHI even from synthetic cases — keep the habit)

---

## Reporting a security issue

**Do not file a public issue for security vulnerabilities.** Email the maintainer directly. Include:

- A description of the issue.
- A reproduction.
- Affected versions / configurations.

We'll acknowledge within 48 hours and coordinate disclosure. For HIPAA-relevant issues (PHI exposure, audit-log integrity, encryption gaps), please flag clearly in the subject line.

---

## License and CLA

Bodhyanga is open source. By contributing, you agree your contributions are licensed under the same terms as the project (see [LICENSE](LICENSE)). No separate CLA is required today.

---

## Thank you

Building clinical AI safely is a long arc. Every contribution — code, docs, tests, bug reports, design discussions — moves the field. Glad you're here.
