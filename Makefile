APP       := server
PKG       := video-canvas/pkg/version
VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILDTIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).GitCommit=$(COMMIT) -X $(PKG).BuildTime=$(BUILDTIME)

.PHONY: run build test lint tidy clean

run:
	go run ./cmd/server -c configs/config.yaml

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/server

test:
	go test ./...

lint:
	gofmt -l . && go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin
