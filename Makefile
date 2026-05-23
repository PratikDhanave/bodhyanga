# Bodh — common dev targets.
#
# Default LLM provider is llm-ollama (local, free, no API key). Override on
# the command line:
#   make care DIAGNOSTICIAN=llm-anthropic
#   make care DIAGNOSTICIAN=llm-openai
#   make care DIAGNOSTICIAN=rule
#
# Forward args to runnables with ARGS="...":
#   make caredemo ARGS="-workflow=cdm"
#
# Show every target:
#   make help

GO        ?= go
PKG       ?= ./...
DOCKER    ?= docker
IMG_TAG   ?= bodh/care:dev
ARGS      ?=
NPM       ?= npm

# ----- LLM / RAG defaults ---------------------------------------------------

# llm-ollama is the local-only default — no API key needed. Switch to
# llm-anthropic / llm-openai / rule on the command line.
DIAGNOSTICIAN     ?= llm-ollama
OLLAMA_HOST       ?= http://localhost:11434
OLLAMA_MODEL      ?= llama3.2
OLLAMA_EMBED      ?= nomic-embed-text
ANTHROPIC_MODEL   ?= claude-sonnet-4-6
OPENAI_MODEL      ?= gpt-4o

# RAG is on by default with the shipped corpus. Disable with KNOWLEDGE_CORPUS=.
KNOWLEDGE_CORPUS  ?= ./data/knowledge/corpus.json

# Environment block exported to every runnable target. ANTHROPIC_API_KEY /
# OPENAI_API_KEY / VOYAGE_API_KEY are pulled from the parent shell.
LLM_ENV := \
	OLLAMA_HOST=$(OLLAMA_HOST) \
	BODH_OLLAMA_MODEL=$(OLLAMA_MODEL) \
	BODH_ANTHROPIC_MODEL=$(ANTHROPIC_MODEL) \
	BODH_OPENAI_MODEL=$(OPENAI_MODEL) \
	BODH_KNOWLEDGE_CORPUS=$(KNOWLEDGE_CORPUS)

WEB := cmd/care/web

# ----- Help (default target) -----------------------------------------------

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "Bodh Makefile (Go $$($(GO) version | awk '{print $$3}'))"
	@echo ""
	@echo "Build / test"
	@echo "  make all             vet + test + build"
	@echo "  make build           go build ./..."
	@echo "  make test            go test ./..."
	@echo "  make race            go test -race ./..."
	@echo "  make vet             go vet ./..."
	@echo "  make cover           go test -coverprofile + summary"
	@echo "  make ci              vet + test + govulncheck"
	@echo ""
	@echo "Runnables (default DIAGNOSTICIAN=$(DIAGNOSTICIAN), RAG corpus=$(notdir $(KNOWLEDGE_CORPUS)))"
	@echo "  make care            cmd/care HTTP API + SPA at http://localhost:8088/ui/"
	@echo "  make demo            cmd/demo (sequential diagnosis)"
	@echo "  make caredemo        cmd/caredemo (CDM/TCM/Panel/...)"
	@echo "  make bench           cmd/bench (provider comparison)"
	@echo "  make bench-compare   cmd/bench across rule + llm-ollama"
	@echo "  make bodhctl         go install ./cmd/bodhctl"
	@echo "  make export-hitl     run cmd/export-hitl-feedback on data/hitl-sample.json"
	@echo ""
	@echo "Ollama (local LLM)"
	@echo "  make ollama-check    verify ollama is reachable at $(OLLAMA_HOST)"
	@echo "  make ollama-pull     pull $(OLLAMA_MODEL) + $(OLLAMA_EMBED)"
	@echo ""
	@echo "Clinician console SPA"
	@echo "  make web-install     npm install in $(WEB)"
	@echo "  make web-build       npm run build (regenerates dist/ used by go:embed)"
	@echo "  make web-dev         npm run dev (Vite hot-reload on :5173)"
	@echo "  make web-clean       remove node_modules / dist / tsbuildinfo"
	@echo ""
	@echo "Container"
	@echo "  make docker          docker build -t $(IMG_TAG)"
	@echo "  make docker-run      run container on :8088"
	@echo ""
	@echo "Variables (override on the command line)"
	@echo "  DIAGNOSTICIAN  ($(DIAGNOSTICIAN))   rule | llm-anthropic | llm-openai | llm-ollama"
	@echo "  OLLAMA_HOST    ($(OLLAMA_HOST))"
	@echo "  OLLAMA_MODEL   ($(OLLAMA_MODEL))"
	@echo "  OLLAMA_EMBED   ($(OLLAMA_EMBED))"
	@echo "  KNOWLEDGE_CORPUS ($(KNOWLEDGE_CORPUS))   set empty to disable RAG"
	@echo "  ARGS           passed through to runnable targets"

# ----- Build / test --------------------------------------------------------

.PHONY: all
all: vet test build

.PHONY: build
build:
	$(GO) build $(PKG)

.PHONY: test
test:
	$(GO) test $(PKG)

.PHONY: race
race:
	$(GO) test -race $(PKG)

.PHONY: vet
vet:
	$(GO) vet $(PKG)

.PHONY: cover
cover:
	$(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -20

.PHONY: ci
ci: vet test
	@command -v govulncheck >/dev/null 2>&1 && govulncheck $(PKG) || echo "govulncheck not installed (skipping)"

# ----- Runnables -----------------------------------------------------------

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

.PHONY: bodhctl
bodhctl:
	$(GO) install ./cmd/bodhctl

.PHONY: export-hitl
export-hitl:
	$(GO) run ./cmd/export-hitl-feedback -in data/hitl-sample.json -out-dir /tmp/bodh-ft

# ----- Ollama --------------------------------------------------------------

.PHONY: ollama-check
ollama-check:
	@curl -sf $(OLLAMA_HOST)/api/tags >/dev/null \
		&& echo "Ollama reachable at $(OLLAMA_HOST)" \
		|| (echo "Ollama not reachable at $(OLLAMA_HOST)." >&2; \
		    echo "  Install:  https://ollama.com" >&2; \
		    echo "  Then run: ollama serve" >&2; \
		    exit 1)
	@curl -sf $(OLLAMA_HOST)/api/tags 2>/dev/null \
		| python3 -c 'import json,sys; d=json.load(sys.stdin); ms=d.get("models",[]); print(f"{len(ms)} models installed:"); [print(" -", m["name"]) for m in ms]' \
		2>/dev/null || true

.PHONY: ollama-pull
ollama-pull:
	@command -v ollama >/dev/null 2>&1 || (echo "ollama CLI not installed. Get it at https://ollama.com" >&2; exit 1)
	ollama pull $(OLLAMA_MODEL)
	ollama pull $(OLLAMA_EMBED)

# ----- Clinician console (SPA) ---------------------------------------------

.PHONY: web-install
web-install:
	cd $(WEB) && $(NPM) install

.PHONY: web-build
web-build:
	cd $(WEB) && $(NPM) run build

.PHONY: web-dev
web-dev:
	@echo "Vite dev server at http://localhost:5173 (proxies /api to :8088)."
	@echo "Run 'make care' in another terminal for the backend."
	cd $(WEB) && $(NPM) run dev

.PHONY: web-clean
web-clean:
	rm -rf $(WEB)/node_modules $(WEB)/dist $(WEB)/tsconfig.tsbuildinfo

# ----- Container -----------------------------------------------------------

.PHONY: docker
docker:
	$(DOCKER) build -t $(IMG_TAG) -f Dockerfile .

.PHONY: docker-run
docker-run:
	$(DOCKER) run --rm -p 8088:8088 $(IMG_TAG)

# ----- House-keeping -------------------------------------------------------

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
