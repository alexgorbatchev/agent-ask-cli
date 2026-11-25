#![cfg(unix)]

use serde_json::Value;
use std::{
    fs,
    io::{BufRead, BufReader, Write},
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
    process::{Command, Output, Stdio},
};
use tempfile::TempDir;

const CODEX: &str = r#"{"type":"thread.started","thread_id":"session"}
{"type":"item.completed","item":{"id":"tool","type":"command_execution","exit_code":0,"aggregated_output":"ok"}}
{"type":"item.completed","item":{"type":"agent_message","text":"answer\n"}}
{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":2,"cached_input_tokens":1}}
"#;
const CLAUDE: &str = r#"{"type":"system","subtype":"init","model":"native-model","session_id":"session"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}}
{"type":"result","subtype":"success","result":"earlier","usage":{"input_tokens":2}}
{"type":"result","subtype":"success","result":"answer\n","usage":{"input_tokens":5,"output_tokens":2}}
"#;
const AGY: &str = r#"{"event":"init","conversation_id":"session","init":{"cwd":"/repo","model":"gemini-4-neon-high"}}
{"event":"step_update","step_update":{"conversation_id":"session","step_index":0,"state":"DONE","step_type":"user_input"}}
{"event":"step_update","step_update":{"conversation_id":"session","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"answer"}}
{"event":"result","result":{"conversation_id":"session","status":"SUCCESS","response":"answer\n","usage":{"input_tokens":5,"output_tokens":2,"thinking_tokens":1,"cache_read_tokens":0,"total_tokens":7}}}
"#;

fn cli() -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_agent-ask"));
    command.env_remove("AGENT");
    command
}

fn invoke(mut command: Command, bytes: &[u8]) -> Output {
    let mut child = command
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    let mut stdin = child.stdin.take().unwrap();
    std::thread::scope(|scope| {
        scope.spawn(move || {
            if let Err(error) = stdin.write_all(bytes) {
                assert_eq!(error.kind(), std::io::ErrorKind::BrokenPipe);
            }
        });
        child.wait_with_output().unwrap()
    })
}

fn text(bytes: &[u8]) -> &str {
    std::str::from_utf8(bytes).unwrap()
}

fn json_bytes(bytes: &[u8]) -> Value {
    serde_json::from_slice(bytes).unwrap()
}

fn success(output: &Output) {
    assert!(output.status.success(), "{}", text(&output.stderr));
    assert!(output.stderr.is_empty(), "{}", text(&output.stderr));
}

fn error(output: &Output, code: &str) {
    assert_eq!(output.status.code(), Some(1));
    assert!(output.stdout.is_empty(), "{}", text(&output.stdout));
    assert_eq!(json_bytes(&output.stderr)["error"]["code"], code);
}

fn stream(output: &Output, terminal: &str) -> Vec<Value> {
    assert!(output.stderr.is_empty(), "{}", text(&output.stderr));
    assert_eq!(output.status.code(), Some(i32::from(terminal == "error")));
    let records: Vec<Value> = text(&output.stdout)
        .lines()
        .map(|line| serde_json::from_str(line).unwrap())
        .collect();
    assert_eq!(records.last().unwrap()["type"], terminal);
    for (index, record) in records.iter().enumerate() {
        assert_eq!(record["sequence"], index + 1);
        if index + 1 < records.len() {
            assert!(matches!(record["type"].as_str(), Some("started" | "event")));
        }
    }
    records
}

struct Fixture {
    dir: TempDir,
    executable: PathBuf,
}

impl Fixture {
    fn script(body: &str) -> Self {
        let dir = tempfile::Builder::new()
            .prefix("agent-ask-test-")
            .tempdir()
            .unwrap();
        let executable = dir.path().join("harness-fixture");
        fs::write(&executable, format!("#!/bin/sh\nset -eu\n{body}\n")).unwrap();
        fs::set_permissions(&executable, fs::Permissions::from_mode(0o700)).unwrap();
        Self { dir, executable }
    }

    fn native(records: &str, ending: &str) -> Self {
        Self::script(&format!(
            "printf '%s\\n' \"$@\" > \"$FIXTURE_DIR/args\"\n\
             printf '%s\\n' \"$PWD\" > \"$FIXTURE_DIR/cwd\"\n\
             printf '%s\\n' \"${{CODEX_HOME:-}}\" \"${{CLAUDE_CONFIG_DIR:-}}\" \"$HOME\" > \"$FIXTURE_DIR/homes\"\n\
             cat > \"$FIXTURE_DIR/prompt\"\n\
             for flag in \"$@\"; do\n\
                 case \"${{previous:-}}\" in\n\
                     --settings) cp \"$flag\" \"$FIXTURE_DIR/staged-settings\"; printf '%s' \"$flag\" > \"$FIXTURE_DIR/staged-path\" ;;\n\
                     --json-schema|--output-schema) if test -f \"$flag\"; then cp \"$flag\" \"$FIXTURE_DIR/staged-schema\"; fi ;;\n\
                 esac\n\
                 previous=$flag\n\
             done\n\
             printf '%s\\n' 'private native stderr' >&2\n\
             cat \"$FIXTURE_DIR/records\"\n\
             {ending}"
        ))
        .with_records(records)
    }

    fn with_records(self, records: &str) -> Self {
        fs::write(self.dir.path().join("records"), records).unwrap();
        self
    }

    fn command(&self, harness: &str) -> Command {
        let mut command = cli();
        command
            .current_dir(self.dir.path())
            .env("FIXTURE_DIR", self.dir.path())
            .env_remove("CODEX_HOME")
            .env_remove("CLAUDE_CONFIG_DIR")
            .args(["request", "run", "--harness", harness])
            .arg("--harness-executable-file")
            .arg(&self.executable);
        command
    }

    fn run(&self, harness: &str, args: &[&str], stdin: &[u8]) -> Output {
        let mut command = self.command(harness);
        command.args(args);
        invoke(command, stdin)
    }

    fn read(&self, name: &str) -> String {
        fs::read_to_string(self.dir.path().join(name)).unwrap()
    }
}

#[test]
fn auxiliary_commands_and_modes() {
    for agent in ["0", "1", "true", "yes"] {
        for (args, content) in [
            (&[][..], "request"),
            (&["--help"][..], "completion"),
            (&["request"][..], "run"),
            (&["request", "run", "--help"][..], "--prompt-stdin"),
            (&["help", "request", "run"][..], "--output-stream"),
            (&["completion"][..], "bash"),
            (&["completion", "zsh", "--help"][..], "--no-descriptions"),
        ] {
            let output = cli().env("AGENT", agent).args(args).output().unwrap();
            success(&output);
            let help = text(&output.stdout);
            assert!(help.contains(content), "{help}");
            assert_eq!(help.starts_with("ALERT:"), agent != "0", "{help}");
        }
    }
    for flag in ["--version", "-v"] {
        let output = cli().arg(flag).output().unwrap();
        success(&output);
        assert_eq!(
            text(&output.stdout),
            concat!(env!("CARGO_PKG_VERSION"), "\n")
        );
    }
    let output = cli().arg("skill").output().unwrap();
    success(&output);
    assert!(text(&output.stdout).contains("alexgorbatchev/agent-ask-cli"));
    for shell in ["bash", "fish", "powershell", "zsh"] {
        for args in [
            vec![],
            vec!["--no-descriptions"],
            vec!["--no-descriptions=false"],
        ] {
            let output = cli()
                .args(["completion", shell])
                .args(args)
                .output()
                .unwrap();
            success(&output);
            assert!(text(&output.stdout).contains("agent-ask"));
        }
    }
    for args in [
        &["missing", "--help"][..],
        &["help", "request", "missing"][..],
        &["request", "--bogus"][..],
        &["skill", "extra"][..],
        &["completion", "include"][..],
        &["request", "run", "--timeout", "bad"][..],
        &["request", "run", "--prompt", "one", "--prompt-stdin"][..],
        &["request", "run", "--output-json", "--output-plain"][..],
    ] {
        error(&cli().args(args).output().unwrap(), "invalid_request");
    }
}

#[test]
fn native_results_and_streams() {
    for (harness, expected, native) in [
        ("claude", "claude", CLAUDE),
        ("codex", "codex", CODEX),
        ("antigravity", "agy", AGY),
    ] {
        let fixture = Fixture::native(native, "exit 0");
        let result = fixture.run(harness, &["--prompt", "-"], &[]);
        success(&result);
        let answer = json_bytes(&result.stdout);
        assert_eq!(answer["harness"], expected);
        assert_eq!(answer["output"], "answer\n");
        assert_eq!(answer["output_kind"], "text");
        assert_eq!(answer["session_id"], "session");
        assert_eq!(answer["usage"]["input"], 5);
        if harness == "antigravity" {
            assert_eq!(
                serde_json::from_str::<Value>(&fixture.read("prompt")).unwrap()["message"]["content"],
                "-"
            );
        } else {
            assert_eq!(fixture.read("prompt"), "-");
        }
        let plain = fixture.run(harness, &["--prompt", "p", "--output-plain"], &[]);
        success(&plain);
        assert_eq!(plain.stdout, b"answer\n");
        let stream_output = fixture.run(harness, &["--prompt", "p", "--output-stream"], &[]);
        let records = stream(&stream_output, "result");
        assert_eq!(records[0]["data"]["harness"], expected);
        assert_eq!(records.last().unwrap()["data"], answer);
        assert!(records.iter().any(|record| record["type"] == "event"));
        let mut command = fixture.command(harness);
        command
            .env("AGENT", "1")
            .args(["--prompt", "p", "--output-json"]);
        assert_eq!(invoke(command, &[]).stdout, result.stdout);
    }
}

#[test]
fn source_and_schema_validation() {
    let fixture = Fixture::native(CODEX, "exit 0");
    let file = |name: &str, bytes: &[u8]| fs::write(fixture.dir.path().join(name), bytes).unwrap();
    file("-", b"from the file called dash");
    file("bad-text", &[255]);
    file(
        "integer schema.json",
        br#"{"type":"integer","minimum":9007199254740993}"#,
    );
    file("root schema.json", br#"{"$ref":"integer%20schema.json"}"#);
    let result = fixture.run(
        "codex",
        &[
            "--prompt-file",
            "-",
            "--input-stdin",
            "--input-format",
            "json",
            "--input-schema-file",
            "root schema.json",
        ],
        b"9007199254740993",
    );
    success(&result);
    assert_eq!(
        fixture.read("prompt"),
        "from the file called dash\n\nInput JSON Schema:\n{\"$ref\":\"integer%20schema.json\"}\n\nInput data (JSON encoded json value):\n9007199254740993"
    );
    let result = fixture.run(
        "codex",
        &[
            "--prompt-stdin",
            "--input",
            "",
            "--input-schema",
            "{\"type\":\"string\"}",
        ],
        b"read from stdin",
    );
    success(&result);
    assert!(fixture.read("prompt").ends_with("\n\"\""));
    let result = fixture.run("codex", &["--prompt", "first", "--prompt", "-literal"], &[]);
    success(&result);
    assert_eq!(fixture.read("prompt"), "-literal");
    fs::remove_file(fixture.dir.path().join("prompt")).unwrap();
    for (args, code) in [
        (&[][..], "invalid_request"),
        (&["--prompt", "  "][..], "invalid_request"),
        (&["--prompt-file="][..], "invalid_request"),
        (&["--prompt-stdin=false"][..], "invalid_request"),
        (&["--prompt-stdin", "--input-stdin"][..], "invalid_request"),
        (
            &["--prompt", "p", "--input-schema", "{}"][..],
            "invalid_request",
        ),
        (&["--prompt-file", "missing"][..], "invalid_input"),
        (&["--prompt-file", "bad-text"][..], "invalid_input"),
        (
            &[
                "--prompt",
                "p",
                "--input",
                "[] {}",
                "--input-format",
                "json",
            ][..],
            "input_invalid",
        ),
        (
            &["--prompt", "p", "--input", "1", "--input-schema", "false"][..],
            "input_invalid",
        ),
        (
            &["--prompt", "p", "--output-schema", "[]"][..],
            "invalid_schema",
        ),
        (
            &["--prompt", "p", "--output-schema", "true false"][..],
            "invalid_schema",
        ),
        (
            &[
                "--prompt",
                "p",
                "--output-schema",
                r#"{"$ref":"https://example.invalid/schema.json"}"#,
            ][..],
            "invalid_schema",
        ),
        (
            &["--prompt", "p", "--input-format", "other"][..],
            "invalid_request",
        ),
    ] {
        error(&fixture.run("codex", args, &[]), code);
        assert!(!fixture.dir.path().join("prompt").exists());
    }
    let oversized = fs::File::create(fixture.dir.path().join("oversized")).unwrap();
    oversized.set_len((16 << 20) + 1).unwrap();
    error(
        &fixture.run("codex", &["--prompt-file", "oversized"], &[]),
        "invalid_input",
    );
}

#[test]
fn structured_results_keep_exact_values() {
    let cases = [
        (
            "codex",
            CODEX.replace("answer\\n", "9007199254740993"),
            r#"{"type":"integer"}"#,
            "9007199254740993\n",
        ),
        (
            "claude",
            r#"{"type":"result","subtype":"success","result":"text","structured_output":null}"#.into(),
            r#"{"type":"null"}"#,
            "null\n",
        ),
        (
            "agy",
            r#"{"event":"result","result":{"status":"SUCCESS","response":"text","structured_output":{"title":"clean"}}}"#.into(),
            r#"{"type":"object","required":["title"]}"#,
            "{\"title\":\"clean\"}\n",
        ),
    ];
    for (harness, native, schema, expected) in cases {
        let fixture = Fixture::native(&native, "exit 0");
        let result = fixture.run(
            harness,
            &["--prompt", "p", "--output-schema-stdin", "--output-plain"],
            schema.as_bytes(),
        );
        success(&result);
        assert_eq!(text(&result.stdout), expected);
    }
    let fixture = Fixture::native(CODEX, "exit 0");
    error(
        &fixture.run("codex", &["--prompt", "p", "--output-schema", "true"], &[]),
        "output_invalid",
    );
    let fixture = Fixture::native(&CODEX.replace("answer\\n", "1"), "exit 0");
    error(
        &fixture.run(
            "codex",
            &["--prompt", "p", "--output-schema", r#"{"type":"string"}"#],
            &[],
        ),
        "output_invalid",
    );
    let fixture = Fixture::native(r#"{"type":"result","subtype":"success"}"#, "exit 0");
    for args in [
        &["--prompt", "p"][..],
        &["--prompt", "p", "--output-schema", "true"][..],
    ] {
        error(&fixture.run("claude", args, &[]), "output_invalid");
    }
    assert_eq!(
        fixture
            .run("claude", &["--prompt", "p", "--output-json=false"], &[])
            .status
            .code(),
        Some(1),
    );
}

#[test]
fn configuration_preserves_native_ownership_and_precedence() {
    for (harness, native, overlay, args, expected_home) in [
        (
            "claude",
            CLAUDE,
            r#"{"alwaysThinkingEnabled":true}"#,
            vec![
                "--thinking",
                "off",
                "--model",
                "claude-sonnet-4-5-20250929",
                "--effort",
                "high",
            ],
            1,
        ),
        (
            "codex",
            CODEX,
            "[features]\nplugins = true\n[mcp_servers.fixture]\ncommand = './relative-tool'\n",
            vec![
                "--thinking",
                "off",
                "--model",
                "fixture-model",
                "--effort",
                "none",
            ],
            0,
        ),
        (
            "agy",
            AGY,
            r#"{"disableSlashCommands":false}"#,
            vec!["--thinking", "on", "--model", "fixture-model"],
            2,
        ),
    ] {
        let fixture = Fixture::native(native, "exit 0");
        fs::create_dir(fixture.dir.path().join("request-dir")).unwrap();
        fs::create_dir(fixture.dir.path().join("native-home")).unwrap();
        fs::write(fixture.dir.path().join("settings"), overlay).unwrap();
        let mut flags = vec![
            "--prompt",
            "p",
            "--harness-config-mode",
            "clean",
            "--working-dir",
            "request-dir",
            "--harness-home-dir",
            "native-home",
            "--harness-config-file",
            "settings",
        ];
        flags.extend(args);
        success(&fixture.run(harness, &flags, &[]));
        let cwd = fixture.dir.path().join("request-dir");
        assert_eq!(Path::new(fixture.read("cwd").trim()), cwd);
        let home = fixture.dir.path().join("native-home");
        assert_eq!(
            Path::new(fixture.read("homes").lines().nth(expected_home).unwrap()),
            home
        );
        let argv = fixture.read("args");
        match harness {
            "claude" => {
                assert!(argv.contains("--safe-mode\n--setting-sources\n\n"));
                assert!(argv.contains("--effort\nhigh"));
                assert_eq!(
                    fixture.read("staged-settings"),
                    r#"{"alwaysThinkingEnabled":false}"#
                );
            }
            "codex" => {
                assert!(argv.contains("--ignore-user-config\n--ignore-rules"));
                assert!(argv.contains("./relative-tool"));
                assert!(argv.contains("features.hooks=false"));
                assert!(argv.contains("untrusted"));
                assert!(argv.contains("model_reasoning_effort=\"none\""));
                assert!(!argv.contains("--strict-config"));
            }
            "agy" => {
                assert!(argv.contains("--mode\ndefault"));
                assert!(argv.contains("--effort\nmedium"));
                assert_eq!(
                    fixture.read("staged-settings"),
                    r#"{"disableSlashCommands":true}"#
                );
            }
            _ => unreachable!(),
        }
        if harness != "codex" {
            assert!(!Path::new(&fixture.read("staged-path")).exists());
        }
    }
}

#[test]
fn rejects_unsupported_native_configuration() {
    let fixture = Fixture::native(CODEX, "exit 0");
    let cases = [
        ("claude", &["--effort", "none"][..], "unsupported_option"),
        ("claude", &["--thinking", "off"][..], "unsupported_option"),
        (
            "claude",
            &["--thinking", "off", "--model", "claude-opus-4-7"][..],
            "unsupported_option",
        ),
        ("codex", &["--effort", "max"][..], "unsupported_option"),
        (
            "codex",
            &["--thinking", "off", "--effort", "minimal"][..],
            "invalid_request",
        ),
        (
            "codex",
            &["--thinking", "on", "--effort", "none"][..],
            "invalid_request",
        ),
        ("codex", &["--thinking", "yes"][..], "invalid_request"),
        ("agy", &["--effort", "minimal"][..], "unsupported_option"),
        ("agy", &["--thinking", "off"][..], "unsupported_option"),
        ("other", &[][..], "invalid_request"),
        ("codex", &["--timeout=-1s"][..], "invalid_request"),
        (
            "codex",
            &["--harness-config-mode", "unknown"][..],
            "invalid_request",
        ),
        (
            "codex",
            &["--working-dir", "missing"][..],
            "invalid_request",
        ),
        (
            "codex",
            &["--harness-home-dir", "missing"][..],
            "configuration_failed",
        ),
        (
            "codex",
            &["--harness-config-file", "missing"][..],
            "configuration_failed",
        ),
    ];
    for (harness, flags, code) in cases {
        let mut args = vec!["--prompt", "p"];
        args.extend(flags);
        error(&fixture.run(harness, &args, &[]), code);
    }
    for (harness, settings, code) in [
        ("claude", "null", "configuration_failed"),
        (
            "claude",
            r#"{"unexpectedSetting":true}"#,
            "configuration_failed",
        ),
        (
            "claude",
            r#"{"alwaysThinkingEnabled":"yes"}"#,
            "configuration_failed",
        ),
        ("agy", "[]", "configuration_failed"),
        (
            "agy",
            r#"{"unexpectedSetting":true}"#,
            "configuration_failed",
        ),
        (
            "agy",
            r#"{"disableSlashCommands":"yes"}"#,
            "configuration_failed",
        ),
        ("codex", "=", "configuration_failed"),
        (
            "codex",
            r#""unusable.root" = "value""#,
            "unsupported_option",
        ),
    ] {
        fs::write(fixture.dir.path().join("settings"), settings).unwrap();
        let args = ["--prompt", "p", "--harness-config-file", "settings"];
        error(&fixture.run(harness, &args, &[]), code);
    }
    assert!(!fixture.dir.path().join("prompt").exists());
    for file in ["missing", "bad-executable"] {
        let path = fixture.dir.path().join(file);
        if file == "bad-executable" {
            fs::write(&path, "#!/nonexistent/interpreter\n").unwrap();
            fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        }
        error(
            &cli()
                .args([
                    "request",
                    "run",
                    "--harness",
                    "codex",
                    "--prompt",
                    "p",
                    "--harness-executable-file",
                ])
                .arg(path)
                .output()
                .unwrap(),
            "harness_unavailable",
        );
    }
}

#[test]
fn failures_are_terminal_and_redacted() {
    let oversized = "x".repeat((32 << 20) + 1);
    for (harness, native, ending) in [
        ("codex", CODEX, "exit 2"),
        ("codex", r#"{"type":"turn.started"}"#, "exit 0"),
        ("codex", "broken", "exit 0"),
        ("codex", oversized.as_str(), "exit 0"),
        (
            "codex",
            "{\"type\":\"error\",\"message\":\"secret\"}\n{\"type\":\"turn.failed\",\"error\":{\"message\":\"secret\"}}",
            "exit 0",
        ),
        (
            "claude",
            r#"{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["secret"]}"#,
            "exit 0",
        ),
        (
            "agy",
            r#"{"event":"result","result":{"status":"ERROR","error":"secret"}}"#,
            "exit 0",
        ),
    ] {
        let fixture = Fixture::native(native, ending);
        error(
            &fixture.run(harness, &["--prompt", "p"], &[]),
            "harness_failed",
        );
        let output = fixture.run(harness, &["--prompt", "p", "--output-stream"], &[]);
        let records = stream(&output, "error");
        assert_eq!(records.last().unwrap()["data"]["code"], "harness_failed");
        assert!(!text(&output.stdout).contains("secret"));
        assert!(!text(&output.stdout).contains("private native stderr"));
    }
    let fixture = Fixture::native(CODEX, "exit 0");
    let records = stream(&fixture.run("codex", &["--output-stream"], &[]), "error");
    assert_eq!(records.len(), 1);
    assert_eq!(records[0]["data"]["code"], "invalid_request");
}

#[test]
fn reports_an_unavailable_temporary_directory_before_starting_the_harness() {
    let fixture = Fixture::native(CODEX, "exit 0");
    let mut command = fixture.command("codex");
    command
        .env("TMPDIR", fixture.dir.path().join("absent"))
        .args(["--prompt", "p"]);
    let output = invoke(command, &[]);
    error(&output, "configuration_failed");
    assert!(!fixture.dir.path().join("args").exists());
}

#[test]
fn request_timeout_accepts_go_microseconds_and_rejects_overflow() {
    let fixture = Fixture::native(CODEX, "exit 0");
    success(&fixture.run("codex", &["--prompt", "p", "--timeout", "1s1μs"], &[]));
    error(
        &fixture.run(
            "codex",
            &["--prompt", "p", "--timeout", "9223372036854775808ns"],
            &[],
        ),
        "invalid_request",
    );
}

#[test]
fn timeout_and_signal_stop_the_owned_process() {
    let fixture = Fixture::script(
        "cat >/dev/null\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"ready\"}'\nexec sleep 30",
    );
    error(
        &fixture.run("codex", &["--prompt", "p", "--timeout", "200ms"], &[]),
        "timed_out",
    );

    let mut child = fixture
        .command("codex")
        .args(["--prompt", "p", "--output-stream", "--timeout", "3s"])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    let mut reader = BufReader::new(child.stdout.take().unwrap());
    let mut line = String::new();
    reader.read_line(&mut line).unwrap();
    assert_eq!(json_bytes(line.as_bytes())["type"], "started");
    line.clear();
    reader.read_line(&mut line).unwrap();
    assert_eq!(json_bytes(line.as_bytes())["type"], "event");
    let sent = Command::new("kill")
        .args(["-TERM", &child.id().to_string()])
        .status()
        .unwrap();
    assert!(sent.success());
    line.clear();
    reader.read_line(&mut line).unwrap();
    assert_eq!(json_bytes(line.as_bytes())["data"]["code"], "cancelled");
    let output = child.wait_with_output().unwrap();
    assert_eq!(output.status.code(), Some(1));
    assert!(output.stderr.is_empty());
}

#[cfg(target_os = "linux")]
#[test]
fn reaps_a_tree_that_keeps_the_stream_pipe_open() {
    let fixture = Fixture::native(
        CODEX,
        "sleep 30 &\nprintf '%s' \"$!\" > \"$FIXTURE_DIR/descendant\"\nexit 0",
    );
    let output = fixture.run("codex", &["--prompt", "p", "--timeout", "3s"], &[]);
    let pid = fixture.read("descendant");
    let proc = Path::new("/proc").join(&pid);
    let exited =
        || fs::read_to_string(proc.join("stat")).map_or(true, |stat| stat.contains(") Z "));
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(1);
    while !exited() && std::time::Instant::now() < deadline {
        std::thread::sleep(std::time::Duration::from_millis(10));
    }
    let stopped = exited();
    if !stopped {
        Command::new("kill").args(["-KILL", &pid]).status().unwrap();
    }
    assert!(stopped, "Harness descendant {pid} outlived request");
    success(&output);
}

#[cfg(target_os = "linux")]
#[test]
fn reports_broken_stdout_for_every_output_path() {
    let full = || {
        fs::OpenOptions::new()
            .write(true)
            .open("/dev/full")
            .unwrap()
    };
    for args in [
        &["--version"][..],
        &["--help"][..],
        &["skill"][..],
        &["request"][..],
        &["completion", "bash"][..],
    ] {
        let output = cli().args(args).stdout(full()).output().unwrap();
        error(&output, "output_failed");
    }
    let fixture = Fixture::native(CODEX, "exit 0");
    for flag in ["--output-json", "--output-plain", "--output-stream"] {
        let output = fixture
            .command("codex")
            .args(["--prompt", "p", flag])
            .stdout(full())
            .output()
            .unwrap();
        error(&output, "output_failed");
    }
}
