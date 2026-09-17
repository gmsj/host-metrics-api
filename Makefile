BINARY   := hostmetrics
PKG      := ./cmd/hostmetrics
DIST     := dist

# Git tag when the commit is tagged, otherwise the short SHA (plus -dirty when
# the tree has uncommitted changes). Falls back to "dev" outside a git repo.
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# -s -w strip the symbol table and DWARF debug info (smaller binary, no effect
# on stack traces). -X sets the package-level `version` variable in main.
LDFLAGS  := -s -w -X main.version=$(VERSION)
GOFLAGS  := -trimpath
GOEXE    := $(shell go env GOEXE)

.DEFAULT_GOAL := help
.PHONY: help run build build-linux build-windows build-all test cover vet lint fmt tidy clean

help: ## Lista os alvos disponíveis
	@awk 'BEGIN {FS = ":.*## "; printf "Uso: make <alvo>\n\n"} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

run: ## Roda localmente (ARGS="--port 9901" para passar flags)
	go run -ldflags "$(LDFLAGS)" $(PKG) $(ARGS)

build: ## Binário para a plataforma atual
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY)$(GOEXE) $(PKG)

build-linux: ## Binário Linux amd64 em dist/
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-amd64 $(PKG)

build-windows: ## Binário Windows amd64 em dist/
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-windows-amd64.exe $(PKG)

build-all: build-linux build-windows ## Ambos os binários em dist/

test: ## go test com detector de data race
	go test -race ./...

cover: ## Cobertura com relatório HTML (coverage.html)
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "relatório: coverage.html"

vet: ## go vet para Linux e para Windows (compila os arquivos com build tag windows)
	go vet ./...
	GOOS=windows go vet ./...

lint: vet ## golangci-lint
	golangci-lint run ./...

fmt: ## Formata o código (gofumpt se instalado, senão gofmt)
	@if command -v gofumpt >/dev/null 2>&1; then gofumpt -l -w .; else gofmt -l -w .; fi

tidy: ## go mod tidy
	go mod tidy

clean: ## Remove binários e relatórios
	rm -rf $(DIST) $(BINARY) $(BINARY).exe coverage.out coverage.html
