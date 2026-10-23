`ask-llm-cli` lets agents submit one fresh request through an installed, authenticated Claude Code or Codex harness. Pass instructions, optional data, and optional JSON schemas directly on the command line, from files, or through stdin.

# What It Does

- **Reuse existing login:** Use the selected harness's configured authentication and model selection.
- **Validate transformations:** Check input before launch and structured output before success.
- **Choose output:** Return a JSON envelope, the answer alone, or a stream of normalized events.
- **Control configuration:** Inherit native settings, select a clean baseline, or supply an explicit native overlay and home directory.

# How It Works

- Select `claude` or `codex` and supply a prompt.
- Add text or JSON input, optionally validating it against an input schema.
- Add an output schema for a structured answer, or receive text.
- Consume the final answer or watch provisional events while the harness works.

# How it Really Works

- Each call creates a fresh session. The harness can use its tools and make internal model calls or retries; native permissions remain applicable.
- Authentication belongs to the selected harness. Calls consume the account's normal subscription usage or configured API billing.
- Clean mode suppresses discovered customizations while preserving authentication and administrator policy. Explicit model, effort and native configuration options customize the request.
- Buffered output reaches stdout only after a successful process exit and output validation. Failures use a stable JSON error on stderr and exit 1. Streaming writes sequenced NDJSON with one terminal result or error.
- Schema resolution is local. Native constrained output may accept a smaller JSON Schema subset than local validation.
- Disposable schemas and merged Claude settings use an owned OS temporary directory and are removed after the request. Cancellation and timeout terminate the harness and its spawned tools.

# Installation

This project is available in the local workspace; a binary release has not been published.

# Setup

- Install and authenticate [Claude Code](https://code.claude.com/docs/en/quickstart) or [Codex](https://learn.chatgpt.com/docs/quickstart). The selected executable must be available on `PATH`, or supplied through `--harness-executable-file`.
- Use a harness version with the documented native options. The integration targets Claude Code 2.1.292 and Codex 0.163.0; older builds may reject unsupported native flags.

# Quick Start

```sh
ask-llm-cli request run --harness claude \
  --prompt 'Reply with exactly OK' --output-plain
```

Sample Output:

```text
OK
```

```sh
ask-llm-cli request run --harness codex --effort low \
  --prompt 'Remove the artist prefix from the title' \
  --input-format json --input '{"title":"Artist - Song","artist":"Artist"}' \
  --output-schema '{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}'

printf '%s' 'Text to summarize' | ask-llm-cli request run \
  --harness claude --prompt 'Summarize' --input-stdin --output-stream
```

# Options & Flags

`ask-llm-cli`

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--help` | `-h` | false | Print help |
| `--version` | `-v` | false | Print raw version |

`ask-llm-cli request run`

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--harness` | | required | claude or codex |
| `--prompt`, `--prompt-file`, `--prompt-stdin` | | one required | Instructions as literal content, file, or stdin |
| `--input`, `--input-file`, `--input-stdin` | | absent | Optional input through one source |
| `--input-format` | | text | text or json |
| `--input-schema`, `--input-schema-file`, `--input-schema-stdin` | | absent | Optional input JSON Schema through one source |
| `--output-schema`, `--output-schema-file`, `--output-schema-stdin` | | absent | Optional output JSON Schema through one source |
| `--harness-config-mode` | | inherit | inherit or clean |
| `--harness-config-file` | | absent | Claude JSON or Codex TOML native overlay |
| `--harness-home-dir` | | native home | Existing configuration, state and authentication directory |
| `--model` | | native default | Native model name |
| `--effort` | | native default | Native effort level |
| `--thinking` | | auto | auto, on, or supported off |
| `--timeout` | | 5m | Positive execution timeout |
| `--working-dir` | | caller directory | Directory for native tools |
| `--harness-executable-file` | | lookup on PATH | Explicit executable |
| `--output-json` | | effective default | Validated JSON result envelope |
| `--output-plain` | | false | Answer only |
| `--output-stream` | | false | NDJSON events and terminal result or error |

Only one content source may read stdin. Output modes are mutually exclusive. See the [CLI contract](docs/cli-contract.md) and [operational skill](skills/ask-llm-cli/SKILL.md) for complete validation, native option mappings, configuration precedence, output schemas, error codes, and examples.

# License

[MIT](LICENSE).
