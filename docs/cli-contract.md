---
created_on: 2026-10-23 11:12
last_modified: 2026-10-23 11:55
status: current
---

# ask-llm-cli contract

This Go CLI serves agents that submit one fresh request to an already installed,
authenticated Claude Code or Codex harness. Tools, internal model requests, and
native retries are allowed until that request finishes. There is no conversation
resume, API-key management, harness installation, or Engine-specific behavior.

## Commands

`ask-llm-cli request run --harness claude|codex [options]` executes a request.
`ask-llm-cli skill` prints the operational skill installation reference.
`--help`, `--version`, and framework completion commands expose CLI metadata.

## Content sources

| Content | Literal value | File | stdin |
| --- | --- | --- | --- |
| Prompt | `--prompt TEXT` | `--prompt-file PATH` | `--prompt-stdin` |
| Input | `--input VALUE` | `--input-file PATH` | `--input-stdin` |
| Input schema | `--input-schema JSON` | `--input-schema-file PATH` | `--input-schema-stdin` |
| Output schema | `--output-schema JSON` | `--output-schema-file PATH` | `--output-schema-stdin` |

Exactly one prompt source is required. Input and both schemas are optional. At
most one source may supply each field, and at most one field may consume stdin.
Explicitly supplied empty values count as sources; an empty prompt is invalid.
Literal options always contain content, including a literal `-`; they never
guess filenames or read stdin. File paths resolve against the caller's working
directory. No files are needed for inline or piped calls.

Every content source must be readable UTF-8 and at most 16 MiB. Explicit
`-stdin=false` and empty file paths are invalid. A file path of `-` names a file;
only a `-stdin` flag consumes stdin.

`--input-format text|json` defaults to `text`. Text input is a JSON string when
validating its input schema. JSON input must contain exactly one JSON value.
Schemas are JSON Schema documents, validated locally before launching a harness;
input validation failures also prevent launch. Schema references must resolve
locally; no remote schema downloads occur. Output schema constraints are sent to
the native harness and independently validated against the final JSON value.
Invalid or unsupported native schemas fail explicitly; they are not weakened.

The submitted prompt distinguishes trusted task instructions from the optional
input data and input schema. These fields are separate from the CLI's own result
envelope and never interpreted as command-line arguments or shell commands.

## Harness settings

| Option | Default | Behavior |
| --- | --- | --- |
| `--harness-config-mode inherit\|clean` | `inherit` | Load existing configuration, or suppress personal/project customizations while retaining authentication. Built-in tools and administrative policy remain applicable. |
| `--harness-config-file PATH` | absent | Apply native Claude JSON or Codex TOML configuration over the selected baseline. Explicit request flags win where administrative policy permits. |
| `--harness-home-dir PATH` | native default/environment | Select an alternate complete harness home, including its authentication and state. This is not a settings-only override. |
| `--model NAME` | native selection | Override the model. |
| `--effort LEVEL` | native selection | Override effort using a supported native value. |
| `--thinking auto\|on\|off` | `auto` | Leave native behavior unchanged, or explicitly request supported thinking behavior. Unsupported requests fail instead of being ignored. |
| `--timeout DURATION` | `5m` | Native execution deadline; must be positive. Cancellation terminates the owned process tree and joins workers. |
| `--working-dir PATH` | caller's current directory | Working directory for harness tools. Wrapper input/config paths still resolve against the caller's directory. |
| `--harness-executable-file PATH` | selected executable on PATH | Override the harness executable without invoking a shell. |

`--config-file` is reserved for a future configuration file belonging to this
CLI. It is not a public option. There are no public Claude settings or Codex
profile options; the selected harness determines the native configuration dialect.
Authentication is resolved by the harness. Clean mode must not use Claude's bare
mode, which bypasses OAuth/keychain login. Native authentication stores are
never read or copied by the wrapper, or exposed in its diagnostics.
Supplied native configuration errors are surfaced; options are not silently dropped.

Claude uses native print/stream-json mode, partial messages, no session
persistence, and no permission prompt handler: native actions that require an
interactive approval are denied. Clean mode uses `--safe-mode` and an empty
`--setting-sources`. Explicit settings are checked against the bundled native
settings schema before launch, with unknown root keys rejected. Merged explicit
settings are passed by private temporary file rather than as settings JSON in
the process arguments. That schema is
an offline snapshot; newly introduced native settings may require a schema update.

Codex uses `exec --json --ephemeral --skip-git-repo-check --strict-config`.
Native TOML overlays are encoded as native `-c` overrides; paths inside those
values follow native override semantics relative to `--working-dir`. Clean mode
ignores user config and user/project rules, suppresses instruction and skill
discovery, disables discovered plugins/hooks/memories, and marks the request
directory untrusted so project config does not load. Clean-mode suppression
controls take precedence over customization switches in an overlay; explicit
model and effort flags take precedence over corresponding overlay values.
Native administrative requirements remain effective.

Codex effort values are `none|minimal|low|medium|high|xhigh`. Thinking `off`
selects effort `none`; thinking `on` selects `medium` if effort is omitted.
Contradictory thinking/effort combinations fail. Claude effort values are
`low|medium|high|xhigh|max`; thinking `on` sets `alwaysThinkingEnabled`.
Thinking `off` is accepted for explicit Claude 4/4.1 dated model identifiers
and Claude 4.5 identifiers supported by this adapter. Unspecified models,
aliases, and later families reject `off`. Models and native versions may reject
options their server does not support.

## Output

| Option | Purpose | stdout |
| --- | --- | --- |
| `--output-json` | Default agent response with metadata | One JSON result envelope after successful completion and validation. |
| `--output-plain` | Answer-only output requested by the caller | Final text unchanged, or the validated JSON value without the result envelope. No progress or metadata is printed. |
| `--output-stream` | Observe a running request and its tools | NDJSON events, ending with exactly one validated result or terminal error. |

These modes are mutually exclusive. Omitting all modes selects JSON. There is no
`--raw`, `--stream`, `--json`, or alternative JSON request-envelope input option.
`AGENT` does not change request/result bytes: every caller receives machine output.

The JSON result contains `harness`, `output_kind` (`text` or `json`), and `output`.
Without an output schema, output is always a string, including JSON-looking text.
With an output schema, output is the parsed JSON value. Optional `model`,
`session_id`, and `usage` come only from native reported metadata; unknown values
are omitted. Usage is the native run summary, not a sum of overlapping snapshots.

Stream envelopes contain `sequence` (strictly increasing, starting at 1), `type`,
and `data`. The wrapper emits `started`, `event`, and a terminal `result` or
`error`. An `event` contains a normalized `agent-parser` event, preserving source
event kinds such as text deltas and tool activity. The terminal result's data is
the same JSON result envelope returned in buffered mode. Streamed content is
provisional, and text deltas are emitted only when the harness supplies them.
Native end-of-turn events are not wrapper success: the subprocess must also exit
successfully and the final response must pass validation.

For buffered modes, failures leave stdout empty and put a JSON
`{"error":{"code":"...","message":"..."}}` diagnostic on stderr. In streaming
mode the terminal error carries that error object on stdout, and stderr is
reserved for diagnostic information. All failures exit nonzero. Successful JSON
and NDJSON records end in a newline; plain text is not decorated or rewritten.
Wrapper diagnostics do not forward panic dumps, credentials, or raw harness
stderr. Native answers and tool outputs remain data.

For example, a terminal stream error is
`{"sequence":3,"type":"error","data":{"code":"harness_failed","message":"..."}}`.
Command/flag parsing failures occur before request execution and use stderr.
An output-write failure can prevent delivery of a terminal record. Native
records exceeding 32 MiB fail instead of growing memory without a bound.
Plain structured output is a JSON value followed by a newline. Answer text is
left unchanged; home-directory display folding does not rewrite model answers.

Stable error codes: `invalid_request`, `invalid_input`, `invalid_schema`,
`input_invalid`, `harness_unavailable`, `unsupported_option`, `configuration_failed`,
`harness_failed`, `output_invalid`, `timed_out`, `cancelled`, and `output_failed`.
Initial exit codes are 0 for success and 1 for a reported failure.

## Examples

```sh
ask-llm-cli request run --harness claude \
  --prompt 'Summarize this text' --input 'The meeting covered...' --output-plain

ask-llm-cli request run --harness codex --effort low \
  --prompt 'Remove duplicated artist prefixes; preserve mix names.' \
  --input-format json --input '{"title":"Artist - Song","artist":"Artist"}' \
  --output-schema '{"type":"object","properties":{"title":{"type":"string"},"artist":{"type":"string"}},"required":["title","artist"],"additionalProperties":false}'

printf '%s' 'Text to summarize' | ask-llm-cli request run \
  --harness claude --prompt 'Summarize this text' --input-stdin --output-stream
```

## Ownership and verification

`agent-parser` owns live harness decoding, including Codex exec JSONL, Claude
stream deltas, tool activity, errors, and final structured results (D2).
`ask-llm-cli` owns process execution, lifecycle, configuration, schema validation,
and result delivery. Engine CLI retains its cleanup prompt and music-specific
selection/validation. Transcript/process discovery from `agent-watcher` is not
needed for a subprocess owned directly by this CLI.

Unit/integration tests use synthetic streams and local child-process fixtures.
They do not call paid models or inspect personal credentials. Behavioral tests
cover every content-source combination, schema rejection before launch,
configuration precedence, native result extraction, process failure after a
result, cancellation, output failures, ordering, and terminal event uniqueness.
Every Go package maintains at least 90% statement coverage.
