# Bodh — unified developer Makefile.
#
# Every workflow lives here: build, test, evals, Ollama, SPA, Docker Compose,
# migrations, Kubernetes, and container image builds.
#
# Quick-start (local Ollama, no API key):
#   make ollama-pull      # download model + embedding model once
#   make compose-up       # Postgres + care + Ollama + observability stack
#   # or run care alone:
#   make care
#
# Override any variable on the command line:
#   make care DIAGNOSTICIAN=llm-anthropic
#   make evals EVALS_NO_JUDGE=1
#   make bench ARGS="-providers=rule,llm-ollama"
#
# Show every target:
#   make help

# ----- Toolchain ------------------------------------------------------------

GO      ?= go
DOCKER  ?= docker
NPM     ?= npm
KUBECTL ?= kubectl
PKG     ?= ./...

# ----- LLM / RAG defaults ---------------------------------------------------

DIAGNOSTICIAN    ?= llm-ollama
OLLAMA_HOST      ?= http://localhost:11434
OLLAMA_MODEL     ?= qwen2.5:7b
OLLAMA_EMBED     ?= nomic-embed-text
ANTHROPIC_MODEL  ?= claude-sonnet-4-6
OPENAI_MODEL     ?= gpt-4o
KNOWLEDGE_CORPUS ?= ./data/knowledge/corpus.json

# Eval defaults (all override-able).
EVALS_PROVIDER       ?= ollama
EVALS_AGENT_MODEL    ?=
EVALS_JUDGE_MODEL    ?=
EVALS_DATASET        ?= cmd/evals/data/agent-multiturn.json
EVALS_PASS_THRESHOLD ?= 0.6
EVALS_OUTPUT         ?=
EVALS_NO_JUDGE       ?= 0

# ----- Container / Compose --------------------------------------------------

IMG_CARE  ?= localhost/bodh-care:dev
IMG_ADMIN ?= localhost/bodh-kronk:dev
COMPOSE   := $(DOCKER) compose -f zarf/compose/docker_compose.yaml

# ----- Kubernetes -----------------------------------------------------------

K8S_ENV     ?= dev
K8S_CONTEXT ?=

# ----- Shared env block for runnables ---------------------------------------

LLM_ENV := \
	OLLAMA_HOST=$(OLLAMA_HOST) \
	BODH_OLLAMA_MODEL=$(OLLAMA_MODEL) \
	BODH_ANTHROPIC_MODEL=$(ANTHROPIC_MODEL) \
	BODH_OPENAI_MODEL=$(OPENAI_MODEL) \
	BODH_KNOWLEDGE_CORPUS=$(KNOWLEDGE_CORPUS)

EVALS_ENV := \
	EVALS_PROVIDER=$(EVALS_PROVIDER) \
	OLLAMA_HOST=$(OLLAMA_HOST) \
	EVALS_DATASET=$(EVALS_DATASET) \
	EVALS_PASS_THRESHOLD=$(EVALS_PASS_THRESHOLD) \
	EVALS_NO_JUDGE=$(EVALS_NO_JUDGE)

WEB := cmd/care/web

# ----- Default target -------------------------------------------------------

.DEFAULT_GOAL := help

# ===========================================================================
# HELP
# ===========================================================================

.PHONY: help
help:
	@echo "Bodh Makefile  (Go $$($(GO) version | awk '{print $$3}'))"
	@echo ""
	@echo "━━━ Build & Test ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make all             vet + test + build"
	@echo "  make build           go build ./..."
	@echo "  make test            go test ./..."
	@echo "  make race            go test -race ./..."
	@echo "  make vet             go vet ./..."
	@echo "  make lint            go vet + staticcheck (installs if missing)"
	@echo "  make cover           go test -coverprofile + summary"
	@echo "  make ci              vet + test + govulncheck"
	@echo "  make tidy            go mod tidy"
	@echo "  make fmt             go fmt ./..."
	@echo "  make clean           remove build artifacts"
	@echo ""
	@echo "━━━ Lessons (agent course) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make lesson-01       01-Intro-to-Agents: single LLM call (no tools, no loop)"
	@echo "  Variables: INTRO_MESSAGE INTRO_MODEL INTRO_PROVIDER"
	@echo ""
	@echo "━━━ Runnables ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make care            HTTP API + SPA at http://localhost:8088"
	@echo "  make demo            sequential diagnosis (cmd/demo)"
	@echo "  make caredemo        CDM/TCM/Panel demo (cmd/caredemo)"
	@echo "  make bench           provider benchmark (cmd/bench)"
	@echo "  make bench-compare   rule vs $(DIAGNOSTICIAN) side-by-side"
	@echo "  make bodhadmin       build + install cmd/bodhadmin"
	@echo "  make bodhctl         build + install cmd/bodhctl"
	@echo "  make export-hitl     export HITL feedback to /tmp/bodh-ft"
	@echo ""
	@echo "━━━ Evals (multi-turn agent evaluation) ━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make evals           run eval suite (default: Ollama, no API key)"
	@echo "  make evals-fast      evals without LLM judge (EVALS_NO_JUDGE=1)"
	@echo "  make evals-anthropic run evals against Anthropic (needs ANTHROPIC_API_KEY)"
	@echo "  make evals-save      run evals, save scores to /tmp/bodh-evals.json"
	@echo "  Variables: EVALS_PROVIDER EVALS_AGENT_MODEL EVALS_DATASET EVALS_PASS_THRESHOLD"
	@echo ""
	@echo "━━━ Ollama (local LLM) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make ollama-check    verify Ollama is reachable + list models"
	@echo "  make ollama-pull     pull $(OLLAMA_MODEL) + $(OLLAMA_EMBED)"
	@echo "  make ollama-ps       list running models"
	@echo ""
	@echo "━━━ Open Policy Agent (OPA) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make opa-test        run opa test on embedded Rego policies"
	@echo "  make opa-fmt         opa fmt --write on all .rego files"
	@echo "  make opa-check       opa check (parse + type-check) all .rego files"
	@echo "  (installs opa CLI via go install if not found)"
	@echo ""
	@echo "━━━ Clinician console SPA ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make web-install     npm install in $(WEB)"
	@echo "  make web-build       npm run build (regenerates go:embed dist/)"
	@echo "  make web-dev         Vite hot-reload on :5173"
	@echo "  make web-clean       remove node_modules / dist"
	@echo "  make test-web        Vitest component tests"
	@echo "  make test-web-watch  Vitest watch mode"
	@echo "  make test-e2e        Playwright e2e (needs Chromium)"
	@echo "  make test-e2e-install  one-time: download Chromium (~200 MB)"
	@echo "  make test-all        go test + Vitest"
	@echo ""
	@echo "━━━ Docker Compose (local dev stack) ━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make compose-up      start full stack (Postgres, care, Ollama, Prometheus, Grafana, Tempo, Loki)"
	@echo "  make compose-down    stop + remove containers"
	@echo "  make compose-ps      list container status"
	@echo "  make compose-logs    stream logs (Ctrl-C to stop)"
	@echo "  make compose-build   (re)build bodh-care image used by compose"
	@echo "  make compose-restart restart bodh-care container only"
	@echo "  make compose-clean   down + delete named volumes (wipes DB!)"
	@echo ""
	@echo "━━━ Database migrations ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make migrate         run all pending SQL migrations via cmd/bodhadmin"
	@echo "  make migrate-status  show applied / pending migrations"
	@echo "  DSN defaults to BODH_DB_DSN env var or postgres://postgres:postgres@localhost:5432/bodh"
	@echo ""
	@echo "━━━ Container images ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make docker          build $(IMG_CARE)"
	@echo "  make docker-admin    build $(IMG_ADMIN) (-tags=kronk)"
	@echo "  make docker-run      run $(IMG_CARE) locally on :8088"
	@echo ""
	@echo "━━━ Kubernetes ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make k8s-diff        kustomize diff against cluster (K8S_ENV=dev|prod)"
	@echo "  make k8s-apply       kustomize apply (K8S_ENV=dev|prod)"
	@echo "  make k8s-status      kubectl get pods -n bodh"
	@echo "  make k8s-logs        kubectl logs for bodh-care pods"
	@echo "  make k8s-validate    validate kustomize manifests (no cluster needed)"
	@echo ""
	@echo "━━━ Observability — Laminar (lmnr.ai) ━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make test-otel       go test -tags otel ./pkg/observability/... (Laminar + OTLP)"
	@echo "  make laminar-eval    run the lmnr.evaluate() Go equivalent (needs LMNR_PROJECT_API_KEY)"
	@echo "  Env: LMNR_PROJECT_API_KEY  — Laminar project API key"
	@echo "       LMNR_BASE_URL         — override endpoint (self-hosted: http://localhost:8000)"
	@echo "       LMNR_GROUP_NAME       — eval group name (default: my_first_feature)"
	@echo "       BODH_SERVICE_NAME     — OTel service.name (default: bodh)"
	@echo "  Build: go build -tags otel ./cmd/care     enables Laminar OTLP export"
	@echo ""
	@echo "━━━ Variables ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  DIAGNOSTICIAN    $(DIAGNOSTICIAN)  (rule|llm-ollama|llm-anthropic|llm-openai|llm-kronk)"
	@echo "  OLLAMA_HOST      $(OLLAMA_HOST)"
	@echo "  OLLAMA_MODEL     $(OLLAMA_MODEL)"
	@echo "  OLLAMA_EMBED     $(OLLAMA_EMBED)"
	@echo "  EVALS_PROVIDER   $(EVALS_PROVIDER)  (ollama|anthropic)"
	@echo "  K8S_ENV          $(K8S_ENV)   (dev|prod)"
	@echo "  IMG_CARE         $(IMG_CARE)"
	@echo "  ARGS             extra flags passed to the target binary"

# ===========================================================================
# BUILD & TEST
# ===========================================================================

.PHONY: all
all: vet test build

.PHONY: build
build:
	$(GO) build $(PKG)

.PHONY: test
test:
	$(GO) test $(PKG)

.PHONY: test-otel
## Run observability tests that require the OTel SDK (Laminar + OTLP spans).
## These are excluded from `make test` to keep the default build tag-free.
test-otel:
	$(GO) test -tags otel ./pkg/observability/...

LMNR_GROUP_NAME ?= my_first_feature

.PHONY: laminar-eval
## Run the lmnr.evaluate() Go equivalent against your Laminar instance.
## Requires LMNR_PROJECT_API_KEY. For self-hosted set LMNR_BASE_URL=http://localhost:8000.
##   make laminar-eval
##   make laminar-eval LMNR_GROUP_NAME=experiment-v2
laminar-eval:
	LMNR_PROJECT_API_KEY=$(LMNR_PROJECT_API_KEY) \
	LMNR_BASE_URL=$(LMNR_BASE_URL) \
	LMNR_GROUP_NAME=$(LMNR_GROUP_NAME) \
	$(GO) run ./cmd/laminar-eval/... $(ARGS)

.PHONY: race
race:
	$(GO) test -race $(PKG)

.PHONY: vet
vet:
	$(GO) vet $(PKG)

.PHONY: lint
lint:
	$(GO) vet $(PKG)
	@command -v staticcheck >/dev/null 2>&1 \
		|| (echo "installing staticcheck…" && $(GO) install honnef.co/go/tools/cmd/staticcheck@latest)
	staticcheck $(PKG)

.PHONY: cover
cover:
	$(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -20

.PHONY: ci
ci: vet test
	@command -v govulncheck >/dev/null 2>&1 \
		|| (echo "installing govulncheck…" && $(GO) install golang.org/x/vuln/cmd/govulncheck@latest)
	govulncheck $(PKG)

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: fmt
fmt:
	$(GO) fmt $(PKG)

.PHONY: clean
clean:
	$(GO) clean
	rm -f coverage.out

# ===========================================================================
# RUNNABLES
# ===========================================================================

ARGS ?=

# ===========================================================================
# LESSONS — agent course
# ===========================================================================

# Lesson 01 variables (all override-able on the command line).
INTRO_PROVIDER ?= ollama
INTRO_MODEL    ?=
INTRO_MESSAGE  ?= What is an AI agent?

.PHONY: lesson-01
## 01-Intro-to-Agents: the simplest possible agent — a single LLM call.
## Exercises: change INTRO_MESSAGE, INTRO_MODEL, or INTRO_PROVIDER.
##   make lesson-01 INTRO_MESSAGE="What is the ReAct pattern?"
##   make lesson-01 INTRO_MODEL=llama3.2
##   make lesson-01 INTRO_PROVIDER=anthropic   # needs ANTHROPIC_API_KEY
lesson-01:
	OLLAMA_HOST=$(OLLAMA_HOST) INTRO_PROVIDER=$(INTRO_PROVIDER) \
	$(GO) run ./cmd/intro/... \
	  -provider=$(INTRO_PROVIDER) \
	  -ollama-host=$(OLLAMA_HOST) \
	  $(if $(INTRO_MODEL),-model=$(INTRO_MODEL),) \
	  -message=$(INTRO_MESSAGE) \
	  -verbose \
	  $(ARGS)

# ===========================================================================
# RUNNABLES
# ===========================================================================

.PHONY: care
care:
	$(LLM_ENV) $(GO) run ./cmd/care -diagnostician=$(DIAGNOSTICIAN) $(ARGS)

.PHONY: demo
demo:
	$(LLM_ENV) $(GO) run ./cmd/demo -diagnostician=$(DIAGNOSTICIAN) $(ARGS)

.PHONY: caredemo
caredemo:
	$(LLM_ENV) $(GO) run ./cmd/caredemo $(ARGS)

.PHONY: bench
bench:
	$(LLM_ENV) $(GO) run ./cmd/bench -providers=$(DIAGNOSTICIAN) $(ARGS)

.PHONY: bench-compare
bench-compare:
	$(LLM_ENV) $(GO) run ./cmd/bench -providers=rule,$(DIAGNOSTICIAN) $(ARGS)

.PHONY: bodhadmin
bodhadmin:
	$(GO) install ./cmd/bodhadmin

.PHONY: bodhctl
bodhctl:
	$(GO) install ./cmd/bodhctl

.PHONY: export-hitl
export-hitl:
	$(GO) run ./cmd/export-hitl-feedback -in data/hitl-sample.json -out-dir /tmp/bodh-ft

# ===========================================================================
# EVALS — multi-turn agent evaluation
# ===========================================================================

# Base eval invocation: honours all EVALS_* variables.
_eval_run:
	$(EVALS_ENV) $(GO) run ./cmd/evals $(ARGS)

.PHONY: evals
evals:
	$(EVALS_ENV) $(GO) run ./cmd/evals $(ARGS)

.PHONY: evals-fast
evals-fast:
	$(EVALS_ENV) EVALS_NO_JUDGE=1 $(GO) run ./cmd/evals $(ARGS)

.PHONY: evals-anthropic
evals-anthropic:
	@test -n "$(ANTHROPIC_API_KEY)" \
		|| (echo "ANTHROPIC_API_KEY not set. Export it before running this target." >&2; exit 1)
	EVALS_PROVIDER=anthropic ANTHROPIC_API_KEY=$(ANTHROPIC_API_KEY) \
	EVALS_AGENT_MODEL=$${EVALS_AGENT_MODEL:-claude-haiku-4-5} \
	EVALS_JUDGE_MODEL=$${EVALS_JUDGE_MODEL:-claude-sonnet-4-6} \
	$(GO) run ./cmd/evals $(ARGS)

.PHONY: evals-save
evals-save:
	$(EVALS_ENV) $(GO) run ./cmd/evals -output /tmp/bodh-evals.json $(ARGS)
	@echo "Scores written to /tmp/bodh-evals.json"

# ===========================================================================
# OLLAMA
# ===========================================================================

.PHONY: ollama-check
ollama-check:
	@curl -sf $(OLLAMA_HOST)/api/tags >/dev/null \
		&& echo "✓ Ollama reachable at $(OLLAMA_HOST)" \
		|| (echo "✗ Ollama not reachable at $(OLLAMA_HOST)" >&2; \
		    echo "  Install:  https://ollama.com" >&2; \
		    echo "  Then run: ollama serve" >&2; \
		    exit 1)
	@curl -sf $(OLLAMA_HOST)/api/tags 2>/dev/null \
		| python3 -c 'import json,sys; d=json.load(sys.stdin); ms=d.get("models",[]); \
		  print(f"  {len(ms)} model(s) installed:"); [print(f"    - {m[\"name\"]}") for m in ms]' \
		2>/dev/null || true

.PHONY: ollama-pull
ollama-pull:
	@command -v ollama >/dev/null 2>&1 \
		|| (echo "ollama CLI not installed. Get it at https://ollama.com" >&2; exit 1)
	ollama pull $(OLLAMA_MODEL)
	ollama pull $(OLLAMA_EMBED)

.PHONY: ollama-ps
ollama-ps:
	@curl -sf $(OLLAMA_HOST)/api/ps 2>/dev/null \
		| python3 -c 'import json,sys; d=json.load(sys.stdin); ms=d.get("models",[]); \
		  [print(f"  {m[\"name\"]} ({m.get(\"size_vram\",0)//1024//1024} MB VRAM)") for m in ms] \
		  or print("  (no models loaded)")' 2>/dev/null \
		|| echo "  (ollama not reachable)"

# ===========================================================================
# OPEN POLICY AGENT (OPA)
# Rego sources live in pkg/governance/rego/ and pkg/auth/rego/.
# opa test runs in-process; no OPA server is needed.
# ===========================================================================

OPA_DIRS := pkg/governance/rego pkg/auth/rego

# Resolve the opa CLI: prefer the one already on PATH; fall back to the
# binary installed by `go install` in GOPATH/bin.
OPA ?= $(shell command -v opa 2>/dev/null || echo $(shell $(GO) env GOPATH)/bin/opa)

# _opa_install is an internal helper that installs the opa CLI when missing.
_opa_install:
	@if ! command -v opa >/dev/null 2>&1 && [ ! -x "$(shell $(GO) env GOPATH)/bin/opa" ]; then \
		echo "opa not found — installing via go install ..."; \
		GOFLAGS="" $(GO) install github.com/open-policy-agent/opa@latest; \
	fi

.PHONY: opa-test
## Run opa test on all embedded Rego policies (governance + auth).
opa-test: _opa_install
	$(OPA) test --v1-compatible $(OPA_DIRS)

.PHONY: opa-fmt
## Format all .rego files in-place with opa fmt.
opa-fmt: _opa_install
	$(OPA) fmt --write --v1-compatible $(OPA_DIRS)

.PHONY: opa-check
## Parse + type-check all .rego files (no evaluation).
opa-check: _opa_install
	$(OPA) check --v1-compatible $(OPA_DIRS)

# ===========================================================================
# CLINICIAN CONSOLE SPA
# ===========================================================================

.PHONY: web-install
web-install:
	cd $(WEB) && $(NPM) install

.PHONY: web-build
web-build:
	cd $(WEB) && $(NPM) run build

.PHONY: web-dev
web-dev:
	@echo "Vite dev server → http://localhost:5173  (API proxied to :8088)"
	@echo "Run 'make care' in another terminal for the backend."
	cd $(WEB) && $(NPM) run dev

.PHONY: web-clean
web-clean:
	rm -rf $(WEB)/node_modules $(WEB)/dist $(WEB)/tsconfig.tsbuildinfo

.PHONY: test-web
test-web:
	cd $(WEB) && $(NPM) test

.PHONY: test-web-watch
test-web-watch:
	cd $(WEB) && $(NPM) run test:watch

.PHONY: test-e2e
test-e2e:
	cd $(WEB) && $(NPM) run test:e2e

.PHONY: test-e2e-install
test-e2e-install:
	cd $(WEB) && $(NPM) run test:e2e:install

.PHONY: test-all
test-all: test test-web
	@echo ""
	@echo "(run 'make test-e2e' separately — needs Chromium via test-e2e-install)"

# ===========================================================================
# DOCKER COMPOSE — local dev stack
# ===========================================================================

.PHONY: compose-up
compose-up:
	$(COMPOSE) up -d
	@echo ""
	@echo "Stack is up:"
	@echo "  care       → http://localhost:8088/ui/"
	@echo "  prometheus → http://localhost:9090"
	@echo "  grafana    → http://localhost:3000  (admin/admin)"
	@echo "  tempo      → http://localhost:3200"
	@echo "  loki       → http://localhost:3100"
	@echo "  postgres   → localhost:5432  (postgres/postgres)"

.PHONY: compose-down
compose-down:
	$(COMPOSE) down

.PHONY: compose-ps
compose-ps:
	$(COMPOSE) ps

.PHONY: compose-logs
compose-logs:
	$(COMPOSE) logs -f $(ARGS)

.PHONY: compose-build
compose-build:
	$(COMPOSE) build bodh-care

.PHONY: compose-restart
compose-restart:
	$(COMPOSE) restart bodh-care

.PHONY: compose-clean
compose-clean:
	@echo "WARNING: this deletes all named volumes (Postgres data, Ollama models, audit logs)."
	@printf "Continue? [y/N] " && read ans && [ "$${ans:-N}" = "y" ]
	$(COMPOSE) down -v

# ===========================================================================
# DATABASE MIGRATIONS
# ===========================================================================

DSN ?= $(or $(BODH_DB_DSN),postgres://postgres:postgres@localhost:5432/bodh?sslmode=disable)

.PHONY: migrate
migrate:
	$(GO) run ./cmd/bodhadmin migrate -dsn "$(DSN)" $(ARGS)

.PHONY: migrate-status
migrate-status:
	$(GO) run ./cmd/bodhadmin migrate -dsn "$(DSN)" -status $(ARGS)

# ===========================================================================
# CONTAINER IMAGES
# ===========================================================================

.PHONY: docker
docker:
	$(DOCKER) build -t $(IMG_CARE) -f zarf/docker/Dockerfile.care .

.PHONY: docker-admin
docker-admin:
	$(DOCKER) build -t $(IMG_ADMIN) -f zarf/docker/Dockerfile.bodhadmin \
		--build-arg TAGS=kronk .

.PHONY: docker-run
docker-run:
	$(DOCKER) run --rm -p 8088:8088 \
		-e BODH_DIAGNOSTICIAN=$(DIAGNOSTICIAN) \
		-e OLLAMA_HOST=$(OLLAMA_HOST) \
		$(IMG_CARE)

# ===========================================================================
# KUBERNETES
# ===========================================================================

_k8s_ctx := $(if $(K8S_CONTEXT),--context $(K8S_CONTEXT),)

.PHONY: k8s-validate
k8s-validate:
	@command -v kustomize >/dev/null 2>&1 \
		|| (echo "kustomize not found. Install: https://kubectl.docs.kubernetes.io/installation/kustomize/" >&2; exit 1)
	kustomize build zarf/k8s/$(K8S_ENV) | $(KUBECTL) $(_k8s_ctx) apply --dry-run=client -f -

.PHONY: k8s-diff
k8s-diff:
	@command -v kustomize >/dev/null 2>&1 \
		|| (echo "kustomize not found." >&2; exit 1)
	kustomize build zarf/k8s/$(K8S_ENV) | $(KUBECTL) $(_k8s_ctx) diff -f -

.PHONY: k8s-apply
k8s-apply:
	@command -v kustomize >/dev/null 2>&1 \
		|| (echo "kustomize not found." >&2; exit 1)
	kustomize build zarf/k8s/$(K8S_ENV) | $(KUBECTL) $(_k8s_ctx) apply -f -

.PHONY: k8s-status
k8s-status:
	$(KUBECTL) $(_k8s_ctx) get pods,svc,ingress -n bodh

.PHONY: k8s-logs
k8s-logs:
	$(KUBECTL) $(_k8s_ctx) logs -n bodh -l app=bodh-care -f $(ARGS)
