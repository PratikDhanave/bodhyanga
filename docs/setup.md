# Bodhyanga — Provider setup

This document is the focused setup guide for the three LLM providers Bodhyanga supports out of the box: **Anthropic Claude**, **OpenAI**, and **Ollama** (local). Pick one. The diagnostic and care-services demos work without any LLM credential — the rule-based agents are the default. You only need this page when you want to enable the LLM-backed agents or RAG.

> Bodhyanga always ships a rule-based fallback. If your LLM call fails for any reason (no key, timeout, bad JSON, transport error, low confidence), the agent transparently falls back to the deterministic rule-based agent — the case still finalizes.

For deeper architecture, prompts, and tracing details, see [ai-features.md](ai-features.md).

---

## Prerequisites (all providers)

- **Go 1.22+** — `go version` should report `go1.22.x` or newer
- **Clone the repo**
  ```bash
  git clone https://github.com/PratikDhanave/bodhyanga.git
  cd bodhyanga
  ```
- A terminal that can `export` env vars (or a `.env` file plus `set -a; . ./.env; set +a`)

Optional but recommended once your provider works:

- A **knowledge corpus** for RAG. The repo ships [`data/knowledge/corpus.json`](../data/knowledge/corpus.json) (15 synthetic teaching documents — CAP, ADHF, PE, sepsis, T2DM, COPD, HTN, ...). Enable with `export BODHYANGA_KNOWLEDGE_CORPUS=./data/knowledge/corpus.json`.

---

## 1. Anthropic Claude

Use Anthropic when you want the strongest clinical reasoning available today and don't mind paying per token. Vision is supported on Sonnet and Opus.

### Sign up & get a key

1. Create an account at **https://console.anthropic.com/**
2. Visit **Settings → API Keys**, click **Create Key**, copy the `sk-ant-...` value
3. (Optional) Add a small credit balance — you'll see usage on the dashboard

### Required env vars

```bash
export ANTHROPIC_API_KEY=sk-ant-...
# Optional model override (defaults to claude-sonnet-4-6):
# export BODHYANGA_ANTHROPIC_MODEL=claude-sonnet-4-6
```

### Smoke test

```bash
go run ./cmd/demo -diagnostician=llm-anthropic
```

Expected first few log lines:

```
[demo] RAG disabled (BODHYANGA_KNOWLEDGE_CORPUS not set)
[bodhyanga] [intake] case=demo-case-001 chief_complaint="Cough and fever"
[bodhyanga] [supervisor] starting sequential diagnosis for case demo-case-001
[bodhyanga] [diagnostician/llm] provider=anthropic model=claude-sonnet-4-6 diagnosis="Community-acquired pneumonia" confidence=88%
[bodhyanga] [diagnostician/llm] tokens in=812 out=146 cached=0 stop=tool_use
[llm-trace] {"started_at":"...","duration_ms":2438,"provider":"anthropic","model":"claude-sonnet-4-6","agent_id":"diagnostician","case_id":"demo-case-001",...}
[bodhyanga] [reasoning_verifier] VERIFIED diagnosis "Community-acquired pneumonia"
```

### Vision

Sonnet and Opus accept inline images. Pass an `image_path` in case metadata or HTTP API body; Bodhyanga sniffs PNG/JPEG and attaches them. See [ai-features.md § Vision](ai-features.md#vision-diagnostician).

### RAG (Anthropic + Voyage embeddings)

Anthropic doesn't ship an embeddings model — Voyage AI is Anthropic's recommended embedder. Get a key at **https://dash.voyageai.com/**.

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export VOYAGE_API_KEY=pa-...
export BODHYANGA_KNOWLEDGE_CORPUS=./data/knowledge/corpus.json
go run ./cmd/demo -diagnostician=llm-anthropic
# [demo] RAG enabled with 15 docs
# [diagnostician/llm] rag retrieved 4 citations
```

### Cost notes

Typical per-case cost on the default pneumonia demo: ~$0.003 with cache hits, ~$0.012 cold. The system prompt is marked cacheable (`CacheSystem: true`) so repeated calls within the cache TTL pay only for incremental tokens. Monitor on https://console.anthropic.com/usage.

---

## 2. OpenAI

Use OpenAI when you want a single key that covers chat, embeddings, and vision. Strict JSON schema mode is supported.

### Sign up & get a key

1. Create an account at **https://platform.openai.com/**
2. Visit **https://platform.openai.com/api-keys**, click **Create new secret key**, copy the `sk-...` value
3. Set a billing limit on **https://platform.openai.com/account/billing/limits** (recommended)

### Required env vars

```bash
export OPENAI_API_KEY=sk-...
# Optional model override (defaults to gpt-4o):
# export BODHYANGA_OPENAI_MODEL=gpt-4o
```

### Smoke test

```bash
go run ./cmd/demo -diagnostician=llm-openai
```

Expected first few log lines:

```
[demo] RAG disabled (BODHYANGA_KNOWLEDGE_CORPUS not set)
[bodhyanga] [intake] case=demo-case-001 chief_complaint="Cough and fever"
[bodhyanga] [diagnostician/llm] provider=openai model=gpt-4o diagnosis="Community-acquired pneumonia" confidence=86%
[bodhyanga] [diagnostician/llm] tokens in=798 out=142 cached=0 stop=stop
[bodhyanga] [reasoning_verifier] VERIFIED diagnosis "Community-acquired pneumonia"
```

### Vision

`gpt-4o` accepts inline images as `image_url` data URLs. Bodhyanga handles the encoding. Pass `image_path` in metadata to enable.

### RAG (OpenAI all-in-one)

OpenAI has both chat and embeddings under one key — simplest setup.

```bash
export OPENAI_API_KEY=sk-...
export BODHYANGA_KNOWLEDGE_CORPUS=./data/knowledge/corpus.json
go run ./cmd/demo -diagnostician=llm-openai
# [demo] RAG enabled with 15 docs
# [diagnostician/llm] rag retrieved 4 citations
```

Embedding model defaults to `text-embedding-3-small` (1536d). Override with `BODHYANGA_OPENAI_EMBED_MODEL=text-embedding-3-large`.

### Cost notes

Typical per-case cost on the default pneumonia demo: ~$0.005–0.01 on `gpt-4o`, dropping to ~$0.001 on `gpt-4o-mini`. Embeddings cost ~$0.0001 per case on `text-embedding-3-small`. Monitor on https://platform.openai.com/usage.

---

## 3. Ollama (local — free, private)

Use Ollama when you want **zero cloud cost**, **PHI never leaves your machine**, and a workable development loop. Quality is lower than the cloud models, but the rule-based fallback covers any regressions.

### Install Ollama

1. Download from **https://ollama.com/download** (macOS, Linux, Windows)
2. Verify install:
   ```bash
   ollama --version
   ```
3. Start the server (if not auto-started):
   ```bash
   ollama serve   # default http://localhost:11434
   ```

### Pull the models you need

```bash
# Chat — pick one
ollama pull llama3.2          # general-purpose 3B
ollama pull llama3.2:3b       # smaller / faster
ollama pull qwen2.5:14b       # stronger reasoning, ~9GB

# Embeddings (for RAG)
ollama pull nomic-embed-text  # 768d, ~270MB
# or
ollama pull mxbai-embed-large # 1024d

# Vision (for image diagnoses)
ollama pull llama3.2-vision   # multimodal, ~7GB
# or
ollama pull bakllava
```

### Required env vars

```bash
# All optional — these are the defaults:
# export OLLAMA_HOST=http://localhost:11434
# export BODHYANGA_OLLAMA_MODEL=llama3.2
# export BODHYANGA_OLLAMA_EMBED_MODEL=nomic-embed-text
```

### Smoke test

```bash
ollama pull llama3.2
go run ./cmd/demo -diagnostician=llm-ollama
```

Expected first few log lines:

```
[demo] RAG disabled (BODHYANGA_KNOWLEDGE_CORPUS not set)
[bodhyanga] [intake] case=demo-case-001 chief_complaint="Cough and fever"
[bodhyanga] [diagnostician/llm] provider=ollama model=llama3.2 diagnosis="Community-acquired pneumonia" confidence=72%
[bodhyanga] [diagnostician/llm] tokens in=842 out=156 cached=0 stop=stop
[bodhyanga] [reasoning_verifier] VERIFIED diagnosis "Community-acquired pneumonia"
```

### Vision

Use a vision-capable model: `llama3.2-vision`, `bakllava`, or `llava:13b`. Override with `BODHYANGA_OLLAMA_MODEL=llama3.2-vision` and attach an `image_path` in metadata.

### RAG (Ollama all-local)

```bash
ollama pull llama3.2
ollama pull nomic-embed-text
export BODHYANGA_KNOWLEDGE_CORPUS=./data/knowledge/corpus.json
go run ./cmd/demo -diagnostician=llm-ollama
# [demo] RAG enabled with 15 docs
```

### Cost notes

**$0 per request** once models are pulled. First inference is slower (model load); subsequent calls are warm. Hardware: a recent M-series Mac or a Linux box with 16GB+ RAM runs `llama3.2` comfortably; `qwen2.5:14b` needs ~16GB free; vision models prefer a GPU.

---

## Verifying your setup

Once a provider is configured, run the SD-Bench fixture runner and compare against the rule-based baseline:

```bash
go run ./cmd/bench -providers=rule,llm-anthropic
```

Expected output (numbers will vary):

```
Provider: rule
------------------------------------------------------------------------------
case_id                gold_diagnosis                   working_diagnosis                acc     cost   budg%  latency
------------------------------------------------------------------------------------------------------------------
demo-case-001          Community-acquired pneumonia     Community-acquired pneumonia       100 $220       44%      4ms
demo-chf-001           Acute decompensated heart fai…   Acute decompensated heart fai…     100 $245       40%      4ms

Summary [rule]: cases=2  mean_accuracy=100%  mean_cost=$232.50  under_budget=100%

Provider: llm-anthropic
------------------------------------------------------------------------------
case_id                gold_diagnosis                   working_diagnosis                acc     cost   budg%  latency
------------------------------------------------------------------------------------------------------------------
demo-case-001          Community-acquired pneumonia     Community-acquired pneumonia       100 $220       44%   2.4s
demo-chf-001           Acute decompensated heart fai…   Acute decompensated heart fai…     100 $245       40%   2.7s

Summary [llm-anthropic]: cases=2  mean_accuracy=100%  mean_cost=$232.50  under_budget=100%

Provider comparison
------------------------------------------------------------------------------
provider           cases     acc%    cost$  under-bud%
------------------------------------------------------------------------------
rule                   2      100   232.50         100
llm-anthropic          2      100   232.50         100
```

If a provider's row is missing (e.g. you typed `-providers=rule,llm-anthropic` without `ANTHROPIC_API_KEY` set), `cmd/bench` prints `provider llm-anthropic skipped: ANTHROPIC_API_KEY env var not set` and continues with the rest — the LLM path never silently consumes a key it doesn't have.

Run all three side-by-side once each provider is configured:

```bash
go run ./cmd/bench -providers=rule,llm-anthropic,llm-openai,llm-ollama -json | jq .summary
```

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `ANTHROPIC_API_KEY env var not set` | `export ANTHROPIC_API_KEY=sk-ant-...` and re-run |
| `ollama: http 404: model "llama3.2" not found` | `ollama pull llama3.2` |
| `[demo] RAG disabled (no embedder credential for llm-anthropic)` | `export VOYAGE_API_KEY=pa-...` (Anthropic path uses Voyage for embeddings) |
| `[diagnostician/llm] confidence 0.20 below threshold 0.30 — falling back to rule-based` | The LLM is uncertain; the rule-based fallback ran. This is by design. |
| `connection refused` on Ollama | `ollama serve` not running, or wrong `OLLAMA_HOST` |
| Slow Ollama responses | Try a smaller model (`BODHYANGA_OLLAMA_MODEL=llama3.2:3b`) or use a GPU host |

See [ai-features.md](ai-features.md) for the full architecture walk-through and the env-var reference table.
