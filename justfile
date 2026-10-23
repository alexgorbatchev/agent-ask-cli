set dotenv-load := false

default:
    @just --list

# Download development dependencies
setup:
    go mod download

# Run the CLI
run *args:
    go run ./cmd/ask-llm-cli {{args}}

# Run with compact agent help
run-ai *args:
    AGENT=1 go run ./cmd/ask-llm-cli {{args}}

# Build the local binary
build:
    mkdir -p bin
    go build -o bin/ask-llm-cli ./cmd/ask-llm-cli

# Run offline behavioral tests
test:
    go test -race ./...

vet:
    go vet ./...

lint: vet
    go mod tidy -diff

fmt:
    go fmt ./...

# Enforce 90% coverage in every Go package
coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    go test -race -coverprofile=coverage.out ./... | tee .tmp-coverage-summary
    trap 'rm -- .tmp-coverage-summary' EXIT
    awk '/^ok/ && /coverage:/ { value = $(NF-2); sub(/%/, "", value); if (value + 0 < 90) failed = 1; count++ } END { if (count == 0 || failed) exit 1 }' .tmp-coverage-summary
    go tool cover -func=coverage.out | tail -1

check: lint coverage

clean:
    go clean ./...
    if test -d bin; then rm -r -- bin; fi
