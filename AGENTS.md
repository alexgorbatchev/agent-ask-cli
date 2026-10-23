---
created_on: 2026-10-23 11:18
last_modified: 2026-10-23 11:49
status: current
---

# ask-llm-cli

Go wrapper for one fresh request through an installed Claude Code or Codex harness.

## Commands

- `just setup`: download development dependencies.
- `just build`: compile `bin/ask-llm-cli`.
- `just run [args...]` / `just run-ai [args...]`: run the CLI.
- `just test`, `just vet`, `just check`: offline tests, static checks, and coverage.
- `just fmt`: format Go sources.

## Contract

- Maintain `docs/cli-contract.md` as the CLI contract. Literal values use unqualified flags, paths use `-file` or `-dir`, and stdin uses `-stdin`. All output selection flags have the `--output-` prefix.
- Request output is machine oriented in every environment: buffered JSON by default, answer-only `--output-plain`, or NDJSON `--output-stream`. Keep stderr separate and return stable error codes.
- Both schemas are optional. Validate input before launching a harness and output before publishing success. Never guess that JSON-looking text is structured output without an output schema.
- Use `agent-parser` live parsing rather than adding harness protocol parsers here. Preserve native metadata and usage; never synthesize deltas or sum overlapping usage snapshots.
- Validate supplied Claude settings against the bundled native schema: print mode can silently ignore invalid settings. Keep schema provenance and licensing with the asset. Codex override paths use native dotted names without TOML key quoting; nested arbitrary keys belong inside encoded TOML values.
- Invoke native harness executables directly. Reuse their authentication, configuration and permission systems. Clean mode suppresses customizations while retaining native authentication and administrator policy. Never copy credentials into temporary homes.
- One fresh request may include native tool calls and internal retries. Drain output and wait for process exit before selecting the final native result. Own cancellation and terminate the complete process tree.
- Production disposable files use an owned OS temporary directory and are removed on completion. Agent development artifacts may use `.tmp/`.
- Keep `skills/ask-llm-cli/SKILL.md` synchronized in the same change as every public command, option, default, environment variable, output or side-effect change. The top-level `skill` command prints the fixed Skills CLI install instructions and URL; it never embeds or reads the skill at runtime.
- Use Cobra and cobra-help-tree/v2; agent help begins with the operational skill alert. Raw `--version` output contains only the version and a newline.
- Keep tests offline and hermetic. Behavioral changes include meaningful tests. Maintain at least 90% statement coverage and run `just check` before committing.
- Ignore binaries in `bin/`, coverage artifacts, development worktrees and `.tmp/`. Never publish releases, tags or packages without explicit user authorization.
- Preserve unrelated workspace changes. Ask before registering a workspace submodule. Inspect destination hosts and staged changes for private domains before committing or pushing.
