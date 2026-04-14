BINARY      := mbr
MODULE      := github.com/anuragsarkar97/mbr
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS     := -s -w \
               -X $(MODULE)/internal/version.Version=$(VERSION) \
               -X $(MODULE)/internal/version.Commit=$(COMMIT) \
               -X $(MODULE)/internal/version.Date=$(BUILD_DATE)
GOPATH_BIN  := $(shell go env GOPATH)/bin

.PHONY: build run test lint clean cross install help

## build: Compile for the current OS/arch → bin/mbr
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/mbr

## run: Build and launch the TUI
run: build
	./bin/$(BINARY)

## test: Run all unit tests with race detector
test:
	go test -race -count=1 ./...

## lint: Run golangci-lint (install: brew install golangci-lint)
lint:
	golangci-lint run ./...

## cross: Cross-compile for all supported platforms → dist/
cross:
	@mkdir -p dist
	GOOS=linux   GOARCH=amd64  CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_linux_amd64       ./cmd/mbr
	GOOS=linux   GOARCH=arm64  CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_linux_arm64       ./cmd/mbr
	GOOS=darwin  GOARCH=amd64  CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_darwin_amd64      ./cmd/mbr
	GOOS=darwin  GOARCH=arm64  CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_darwin_arm64      ./cmd/mbr
	GOOS=windows GOARCH=amd64  CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_windows_amd64.exe ./cmd/mbr
	@echo "Built binaries:"
	@ls -lh dist/

## install: Install to GOPATH/bin
install:
	CGO_ENABLED=0 go install -ldflags "$(LDFLAGS)" ./cmd/mbr

## release: Run GoReleaser (requires GITHUB_TOKEN env var)
release:
	goreleaser release --clean

## clean: Remove build artifacts
clean:
	rm -rf bin/ dist/

## help: Print this help message
help:
	@grep -E '^## ' Makefile | sed 's/^## /  /'
