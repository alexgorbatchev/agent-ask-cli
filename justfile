set dotenv-load := false

default:
    @just --list

setup:
    cargo fetch --locked

run *args:
    cargo run --locked -- {{args}}

run-ai *args:
    AGENT=1 cargo run --locked -- {{args}}

test:
    cargo test --locked

test-affected base="origin/main":
    #!/usr/bin/env bash
    set -euo pipefail
    candidate={{ quote(base) }}
    if git rev-parse --verify --quiet --end-of-options "$candidate^{commit}" >/dev/null; then
        base=$(git merge-base -- "$candidate" HEAD)
        files=$(git diff --name-only "$base" --; git ls-files --others --exclude-standard)
        affected=false
        while IFS= read -r file; do
            case "$file" in
                ""|README.md|docs/*|LICENSE) ;;
                *) affected=true ;;
            esac
        done <<< "$files"
        if ! "$affected"; then
            echo 'No agent-ask test inputs changed.'
            exit 0
        fi
    fi
    echo 'Selected agent-ask-cli: package or test inputs changed.'
    just test

fmt:
    cargo fmt --all

fmt-check:
    cargo fmt --all -- --check

lint:
    cargo clippy --locked --all-targets -- -D warnings

coverage:
    cargo llvm-cov --all-targets --locked --ignore-filename-regex '(^|/)tests/' --fail-under-lines 90 --fail-under-functions 90

check: fmt-check lint test coverage

build:
    cargo build --locked --release
    mkdir -p bin
    cp target/release/agent-ask bin/
