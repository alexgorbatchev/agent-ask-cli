---
name: agent-ask-cli
description: Use agent-ask-cli to submit requests through installed Claude Code or Codex harnesses.
author: alexgorbatchev
metadata:
  created_on: 2026-10-23 11:18
  last_modified: 2026-10-23 12:47
  status: current
---

Run one fresh request using the selected installed harness and its existing authentication. Native tool calls and internal retries may occur. The process runs with native permissions and may modify files through authorized tools. Choose a working directory appropriate to the task.

## `agent-ask-cli`

Print help when invoked without a command. Request output is identical regardless of `AGENT`. Set `AGENT=1`, `true`, or `yes` for compact help; agent help begins with a skill alert. Explicit help writes stdout; request errors use the error contract below. Version output is the raw version plus a newline.

| Flag | Short | Type | Default | Meaning |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print help |
| `--version` | `-v` | `bool` | `false` | Print version |

## `agent-ask-cli request`

Print request command help. Accepts no positional arguments.

## `agent-ask-cli request run`

Submit one user request to `claude` or `codex`. Accepts no positional arguments. Supply exactly one nonempty prompt source. Each remaining source group is optional and accepts one source. Only one group may read stdin. Literal flags always contain content; file flags always contain paths, including a literal filename `-`. All file and directory flags resolve relative to the caller's directory, independently of `--working-dir`.

| Flag | Short | Type | Default | Meaning |
| :--- | :--- | :--- | :--- | :--- |
| `--harness` | — | `string` | `""` | Required: claude or codex |
| `--prompt` | — | `string` | `""` | Literal instructions |
| `--prompt-file` | — | `string` | `""` | Instructions file |
| `--prompt-stdin` | — | `bool` | `false` | Instructions from stdin |
| `--input` | — | `string` | `""` | Literal input data |
| `--input-file` | — | `string` | `""` | Input file |
| `--input-stdin` | — | `bool` | `false` | Input from stdin |
| `--input-format` | — | `string` | `text` | text or json |
| `--input-schema` | — | `string` | `""` | Literal JSON Schema |
| `--input-schema-file` | — | `string` | `""` | Input schema file |
| `--input-schema-stdin` | — | `bool` | `false` | Input schema from stdin |
| `--output-schema` | — | `string` | `""` | Literal JSON Schema |
| `--output-schema-file` | — | `string` | `""` | Output schema file |
| `--output-schema-stdin` | — | `bool` | `false` | Output schema from stdin |
| `--harness-config-mode` | — | `string` | `inherit` | inherit or clean |
| `--harness-config-file` | — | `string` | `""` | Claude JSON or Codex TOML overlay |
| `--harness-home-dir` | — | `string` | `""` | Alternate existing native home, including authentication |
| `--model` | — | `string` | `""` | Native default when omitted |
| `--effort` | — | `string` | `""` | Native default when omitted |
| `--thinking` | — | `string` | `auto` | auto, on, off |
| `--timeout` | — | `duration` | `5m0s` | Positive duration, e.g. 90s or 5m |
| `--working-dir` | — | `string` | `""` | Caller directory when omitted |
| `--harness-executable-file` | — | `string` | `""` | Resolve harness on PATH when omitted |
| `--output-json` | — | `bool` | `false` | Buffered JSON envelope; effective default |
| `--output-plain` | — | `bool` | `false` | Answer only |
| `--output-stream` | — | `bool` | `false` | NDJSON events and terminal result/error |

Input is UTF-8, at most 16 MiB per source. `text` validates as a JSON string. `json` requires exactly one JSON value and preserves integer precision. An input schema requires input. JSON Schema drafts 4, 6, 7, 2019-09 and 2020-12 are supported by local validation; local file references resolve relative to a schema file, or the caller's directory for inline/stdin schemas. Network resolution is disabled. Native constrained-output support may accept a smaller schema subset and can reject a locally valid schema.

Input data is appended to instructions as a JSON encoded value, labelled with its original text/json format. Schemas are optional. Output schemas are supplied to the native harness and independently checked after a successful process exit. Without an output schema the answer is always a string, even if it looks like JSON.

Configuration precedence is selected native baseline, explicit native file overlay, then explicit model/effort/thinking. Codex overlays are passed through native `-c` overrides; paths inside them resolve as native CLI overrides relative to `--working-dir`. Native strict config validation is enabled. Claude settings use native `--settings` and are validated before launch against a bundled offline native settings schema, rejecting unknown root settings. Newly introduced native settings may need a snapshot update. Clean mode retains authentication and administrator policy, suppressing automatically discovered personal/project customizations; its suppression controls take precedence over overlay switches that would enable those customizations. Built-in tools remain available. Claude print mode denies actions that would require an interactive approval. Alternate homes must already exist and supply their own authentication. Credential files are never read or copied by this wrapper.

Codex effort accepts `none|minimal|low|medium|high|xhigh`; thinking `off` maps to effort `none`, and `on` defaults to `medium` when effort is omitted. Contradictory thinking and effort fail. Claude effort accepts `low|medium|high|xhigh|max`; `on` sets `alwaysThinkingEnabled`. Claude `off` requires an explicit supported Claude 4/4.5 model identifier; newer always-thinking model families and unspecified models reject it. Model-specific native rejection remains possible.

Buffered JSON has `harness`, `output_kind` (`text|json`), and `output`, with optional native-reported `model`, `session_id` and `usage`. Plain text is unchanged, with no added newline; plain structured JSON has one trailing newline. Buffered modes emit nothing before process success and output validation.

Streaming writes newline-delimited `{sequence,type,data}` records with increasing sequence starting at 1. Types are `started`, `event`, `result`, `error`. `event` holds a normalized agent-parser event. Native deltas have `data.data.delta`; complete snapshots use `data.data.content`. Deltas and snapshots are distinct; partial content is provisional. Exactly one wrapper `result` or `error` terminates a writable stream. A result contains the same envelope as buffered JSON. Usage snapshots replace one another. Native diagnostic errors are sanitized. Native records above 32 MiB fail.

Buffered failures write `{"error":{"code":"...","message":"..."}}` to stderr and exit 1; successful runs exit 0. Request streaming failures produce a terminal stdout error; command/flag parsing failures use stderr. Output write failures can prevent a terminal record. Error codes: `invalid_request`, `invalid_input`, `invalid_schema`, `input_invalid`, `harness_unavailable`, `unsupported_option`, `configuration_failed`, `harness_failed`, `output_invalid`, `timed_out`, `cancelled`, `output_failed`. Timeout or cancellation terminates the harness and its tool descendants. Production temporary schema files are removed on every path.

The wrapper uses `PATH`, native `CODEX_HOME`/`CLAUDE_CONFIG_DIR`, and the selected harness's authentication/provider environment. `--harness-home-dir` overrides the selected native home variable. Native API keys are optional when the harness is already authenticated with a subscription. Merged explicit Claude settings are staged in a private OS temporary file and removed after the request; native authentication stores are never read or copied by the wrapper. Harness stderr is drained privately. There is no wrapper configuration file, request-envelope input, conversation resume or automatic installation.

Examples:

```sh
agent-ask-cli request run --harness claude --prompt 'Summarize' --input 'some notes' --output-plain
printf '%s' 'some notes' | agent-ask-cli request run --harness codex --prompt 'Summarize' --input-stdin --output-stream
agent-ask-cli request run --harness codex --prompt 'Clean the title' --input '{"title":"Artist - Title"}' --input-format json --output-schema '{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}'
agent-ask-cli request run --harness claude --harness-config-mode clean --harness-config-file settings.json --prompt 'Reply OK' --output-plain
```

## `agent-ask-cli skill`

Print Skills CLI installation instructions and the operational skill URL. Accepts no positional arguments or command-specific flags. Works offline from any directory.

## `agent-ask-cli help [command]`

Print help for a command path. `command` is optional and may contain nested command names.

## `agent-ask-cli completion`

Generate shell completion scripts using a shell subcommand. Invoked without a shell, prints completion help.

## `agent-ask-cli completion bash`

Print Bash completion script.

| Flag | Short | Type | Default | Meaning |
| :--- | :--- | :--- | :--- | :--- |
| `--no-descriptions` | — | `bool` | `false` | Omit descriptions |

## `agent-ask-cli completion fish`

Print Fish completion script.

| Flag | Short | Type | Default | Meaning |
| :--- | :--- | :--- | :--- | :--- |
| `--no-descriptions` | — | `bool` | `false` | Omit descriptions |

## `agent-ask-cli completion powershell`

Print PowerShell completion script.

| Flag | Short | Type | Default | Meaning |
| :--- | :--- | :--- | :--- | :--- |
| `--no-descriptions` | — | `bool` | `false` | Omit descriptions |

## `agent-ask-cli completion zsh`

Print Zsh completion script.

| Flag | Short | Type | Default | Meaning |
| :--- | :--- | :--- | :--- | :--- |
| `--no-descriptions` | — | `bool` | `false` | Omit descriptions |
