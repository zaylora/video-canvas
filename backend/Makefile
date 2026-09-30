APP       := server
PKG       := video-canvas/pkg/version
VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILDTIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).GitCommit=$(COMMIT) -X $(PKG).BuildTime=$(BUILDTIME)
# golangci-lint 版本与 CI 保持一致；安装：make tools
LINT         ?= golangci-lint
LINT_VERSION := v2.14.0
LINT_BASE    ?= master

.PHONY: run build test test-race lint lint-all fmt tools tidy clean

run:
	go run ./cmd/server -c configs/config.yaml

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/server

test:
	go test ./...

test-race:
	go test -race ./...

# 只检查相对 $(LINT_BASE) 的增量（含未提交改动），提交前必须通过
lint:
	$(LINT) run --new-from-merge-base=$(LINT_BASE) ./...

# 全量检查，用来跟踪存量问题（docs/standards/存量问题清单.md）
lint-all:
	$(LINT) run ./...

fmt:
	$(LINT) fmt ./...

tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION)

tidy:
	go mod tidy

clean:
	rm -rf bin
