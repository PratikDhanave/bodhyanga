# Bodh — common dev targets.
# Override variables on the command line: make test PKG=./pkg/medical/...
# Forward args to runnables with ARGS="...": make caredemo ARGS="-workflow=cdm"

GO        ?= go
PKG       ?= ./...
DOCKER    ?= docker
IMG_TAG   ?= bodh/care:dev
ARGS      ?=

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

# Runnables ----------------------------------------------------------------

.PHONY: demo
demo:
	$(GO) run ./cmd/demo $(ARGS)

.PHONY: caredemo
caredemo:
	$(GO) run ./cmd/caredemo $(ARGS)

.PHONY: care
care:
	$(GO) run ./cmd/care $(ARGS)

.PHONY: bench
bench:
	$(GO) run ./cmd/bench $(ARGS)

.PHONY: bodhctl
bodhctl:
	$(GO) install ./cmd/bodhctl

# Container ----------------------------------------------------------------

.PHONY: docker
docker:
	$(DOCKER) build -t $(IMG_TAG) -f Dockerfile .

.PHONY: docker-run
docker-run:
	$(DOCKER) run --rm -p 8088:8088 $(IMG_TAG)

# House-keeping -------------------------------------------------------------

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
