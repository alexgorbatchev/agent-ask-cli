use crate::{
    input::Prepared,
    output::Error,
    request::{Harness, Options, Thinking},
};
use serde_json::{Map, Value, json};
use std::{
    ffi::OsString,
    fs,
    io::{self, Write},
    path::{Path, PathBuf, absolute},
    sync::OnceLock,
};

const CLAUDE_SCHEMA: &str = include_str!("schemas/claude-settings.json");
const AGY_SCHEMA: &str = include_str!("schemas/agy-settings.json");

pub struct Invocation {
    pub executable: PathBuf,
    pub args: Vec<OsString>,
    pub work_dir: PathBuf,
    pub home: Option<(&'static str, PathBuf)>,
    pub stdin: String,
}

pub fn build(
    options: &Options,
    prepared: &Prepared,
    workspace: &Path,
) -> Result<Invocation, Error> {
    let Ok(work_dir) = absolute(options.work_dir.as_deref().unwrap_or(Path::new("."))) else {
        return Err(Error::invalid_request("Cannot resolve working directory"));
    };
    if !work_dir.is_dir() {
        return Err(Error::invalid_request("Working directory must exist"));
    }
    let target = options
        .executable
        .as_deref()
        .unwrap_or(Path::new(options.harness.name()));
    let unavailable = |_: which::Error| {
        Error::new(
            "harness_unavailable",
            "Selected harness executable is unavailable",
        )
    };
    let Ok(executable) = absolute(which::which(target).map_err(unavailable)?) else {
        return Err(Error::new(
            "harness_unavailable",
            "Cannot resolve harness executable",
        ));
    };
    let home = if let Some(path) = &options.home_dir {
        let Ok(dir) = absolute(path) else {
            return Err(Error::configuration(
                "Cannot resolve harness home directory",
            ));
        };
        if !dir.is_dir() {
            return Err(Error::configuration("Harness home directory must exist"));
        }
        let key = match options.harness {
            Harness::Claude => "CLAUDE_CONFIG_DIR",
            Harness::Codex => "CODEX_HOME",
            Harness::Agy => "HOME",
        };
        Some((key, dir))
    } else {
        None
    };
    let schema = prepared.raw_output_schema();
    let args = match options.harness {
        Harness::Claude => claude(options, schema, workspace)?,
        Harness::Codex => codex(options, schema, workspace, &work_dir)?,
        Harness::Agy => agy(options, schema, workspace)?,
    };
    Ok(Invocation {
        executable,
        args,
        work_dir,
        home,
        stdin: prepared.native_stdin(options.harness),
    })
}

fn read_configuration(options: &Options) -> Result<Option<String>, Error> {
    options
        .config_file
        .as_ref()
        .map(|path| {
            fs::read_to_string(path)
                .map_err(|_| Error::configuration("Cannot read harness configuration file"))
        })
        .transpose()
}

fn json_settings(options: &Options, error: &'static str) -> Result<Map<String, Value>, Error> {
    let Some(config) = read_configuration(options)? else {
        return Ok(Map::new());
    };
    serde_json::from_str(&config).map_err(|_| Error::configuration(error))
}

fn stage_file(
    workspace: &Path,
    name: &str,
    bytes: &[u8],
    error: &'static str,
) -> Result<OsString, Error> {
    let path = workspace.join(name);
    let staged = (|| -> io::Result<()> {
        let mut file = tempfile::NamedTempFile::new_in(workspace)?;
        file.write_all(bytes)?;
        file.persist(&path)?;
        Ok(())
    })();
    if staged.is_err() {
        return Err(Error::configuration(error));
    }
    Ok(path.into_os_string())
}

fn stage_settings(
    options: &Options,
    config: Map<String, Value>,
    workspace: &Path,
    args: &mut Vec<OsString>,
) -> Result<(), Error> {
    static CLAUDE: OnceLock<Option<jsonschema::Validator>> = OnceLock::new();
    static AGY: OnceLock<Option<jsonschema::Validator>> = OnceLock::new();
    let (name, raw_schema, cache, initialize, invalid, stage) = match options.harness {
        Harness::Claude => (
            "claude-settings.json",
            CLAUDE_SCHEMA,
            &CLAUDE,
            "Cannot initialize Claude settings validation",
            "Claude settings do not match the bundled native settings schema",
            "Cannot stage Claude settings",
        ),
        Harness::Agy => (
            "agy-settings.json",
            AGY_SCHEMA,
            &AGY,
            "Cannot initialize Antigravity settings validation",
            "Antigravity settings do not match the bundled native settings schema",
            "Cannot stage Antigravity settings",
        ),
        Harness::Codex => unreachable!("Codex uses native TOML overrides"),
    };
    if config.is_empty() {
        return Ok(());
    }
    let validator = cache.get_or_init(|| {
        let mut schema: Value = serde_json::from_str(raw_schema).ok()?;
        schema["additionalProperties"] = json!(false);
        jsonschema::validator_for(&schema).ok()
    });
    let Some(validator) = validator else {
        return Err(Error::configuration(initialize));
    };
    let config = Value::Object(config);
    if !validator.is_valid(&config) {
        return Err(Error::configuration(invalid));
    }
    let path = stage_file(workspace, name, config.to_string().as_bytes(), stage)?;
    args.extend(["--settings".into(), path]);
    Ok(())
}

fn push_model(args: &mut Vec<OsString>, options: &Options) {
    if let Some(model) = &options.model {
        args.extend(["--model".into(), model.into()]);
    }
}

fn claude(
    options: &Options,
    schema: Option<&str>,
    workspace: &Path,
) -> Result<Vec<OsString>, Error> {
    let mut config = json_settings(options, "Claude configuration must be a JSON object")?;
    let mut args = [
        "-p",
        "--output-format",
        "stream-json",
        "--verbose",
        "--include-partial-messages",
        "--no-session-persistence",
        "--permission-prompts",
        "none",
    ]
    .map(OsString::from)
    .to_vec();
    if options.clean {
        args.extend(["--safe-mode".into(), "--setting-sources".into(), "".into()]);
    }
    push_model(&mut args, options);
    if let Some(effort) = &options.effort {
        if !["low", "medium", "high", "xhigh", "max"].contains(&effort.as_str()) {
            return Err(Error::unsupported(
                "Claude effort must be low, medium, high, xhigh, or max",
            ));
        }
        args.extend(["--effort".into(), effort.into()]);
    }
    if options.thinking != Thinking::Auto {
        if options.thinking == Thinking::Off
            && !options.model.as_deref().is_some_and(claude_can_disable)
        {
            return Err(Error::unsupported(
                "Claude thinking off requires an explicit supported Claude 4 or 4.5 model; use effort for newer models",
            ));
        }
        config.insert(
            "alwaysThinkingEnabled".into(),
            json!(options.thinking == Thinking::On),
        );
    }
    stage_settings(options, config, workspace, &mut args)?;
    if let Some(schema) = schema {
        args.extend(["--json-schema".into(), schema.into()]);
    }
    Ok(args)
}

fn claude_can_disable(model: &str) -> bool {
    [
        "claude-sonnet-4-20250514",
        "claude-opus-4-20250514",
        "claude-opus-4-1-20250805",
        "claude-sonnet-4-5",
        "claude-opus-4-5",
    ]
    .contains(&model)
        || model.starts_with("claude-sonnet-4-5-")
        || model.starts_with("claude-opus-4-5-")
}

fn codex(
    options: &Options,
    schema: Option<&str>,
    workspace: &Path,
    work_dir: &Path,
) -> Result<Vec<OsString>, Error> {
    let config = read_configuration(options)?
        .map(|config| {
            toml::from_str::<toml::Table>(&config)
                .map_err(|_| Error::configuration("Codex configuration must be valid TOML"))
        })
        .transpose()?
        .unwrap_or_default();
    let mut args = ["exec", "--json", "--ephemeral", "--skip-git-repo-check"]
        .map(OsString::from)
        .to_vec();
    for (key, value) in config {
        push_override(&mut args, &key, value)?;
    }
    if options.clean {
        args.extend(
            [
                "--ignore-user-config",
                "--ignore-rules",
                "-c",
                "project_doc_max_bytes=0",
                "-c",
                "skills.include_instructions=false",
                "-c",
                "features.skip_host_skill_discovery=true",
                "-c",
                "features.plugins=false",
                "-c",
                "features.hooks=false",
                "-c",
                "features.memories=false",
            ]
            .map(OsString::from),
        );
        let Some(work_dir) = work_dir.to_str() else {
            return Err(Error::configuration("Cannot encode Codex configuration"));
        };
        let project = toml::Value::Table(toml::Table::from_iter([(
            "trust_level".into(),
            toml::Value::String("untrusted".into()),
        )]));
        push_override(
            &mut args,
            "projects",
            toml::Value::Table(toml::Table::from_iter([(work_dir.into(), project)])),
        )?;
    }
    push_model(&mut args, options);
    let effort = options.effort.as_deref();
    if effort.is_some_and(|value| {
        !["none", "minimal", "low", "medium", "high", "xhigh"].contains(&value)
    }) {
        return Err(Error::unsupported(
            "Codex effort must be none, minimal, low, medium, high, or xhigh",
        ));
    }
    let effort = match (options.thinking, effort) {
        (Thinking::Off, Some(value)) if value != "none" => {
            return Err(Error::invalid_request(
                "Thinking off conflicts with a reasoning effort",
            ));
        }
        (Thinking::Off, _) => Some("none"),
        (Thinking::On, Some("none")) => {
            return Err(Error::invalid_request(
                "Thinking on conflicts with effort none",
            ));
        }
        (Thinking::On, None) => Some("medium"),
        (_, effort) => effort,
    };
    if let Some(effort) = effort {
        push_override(
            &mut args,
            "model_reasoning_effort",
            toml::Value::String(effort.into()),
        )?;
    }
    if let Some(schema) = schema {
        let path = stage_file(
            workspace,
            "output.schema.json",
            schema.as_bytes(),
            "Cannot stage output schema",
        )?;
        args.extend(["--output-schema".into(), path]);
    }
    args.push("-".into());
    Ok(args)
}

fn push_override(args: &mut Vec<OsString>, key: &str, value: toml::Value) -> Result<(), Error> {
    // Override paths are split at dots before TOML value parsing. Nested keys
    // stay inside the TOML value so their quoting remains meaningful.
    if key.is_empty() || key.contains(['.', '"', '\'', '[', ']', '=', ' ', '\t', '\r', '\n']) {
        return Err(Error::unsupported(
            "Codex configuration contains an unsupported root key",
        ));
    }
    args.extend(["-c".into(), format!("{key}={value}").into()]);
    Ok(())
}

fn agy(options: &Options, schema: Option<&str>, workspace: &Path) -> Result<Vec<OsString>, Error> {
    let mut config = json_settings(options, "Antigravity configuration must be a JSON object")?;
    let mut args = [
        "--input-format",
        "stream-json",
        "--output-format",
        "stream-json",
        "--disable-slash-commands",
    ]
    .map(OsString::from)
    .to_vec();
    if options.clean {
        args.extend(["--mode".into(), "default".into()]);
        config.insert("disableSlashCommands".into(), json!(true));
    }
    push_model(&mut args, options);
    let effort = options.effort.as_deref();
    if effort.is_some_and(|value| !["low", "medium", "high", "xhigh", "max"].contains(&value)) {
        return Err(Error::unsupported(
            "Antigravity effort must be low, medium, high, xhigh, or max",
        ));
    }
    if options.thinking == Thinking::Off {
        return Err(Error::unsupported(
            "Antigravity thinking off is unsupported; use --effort low",
        ));
    }
    if let Some(effort) = effort.or((options.thinking == Thinking::On).then_some("medium")) {
        args.extend(["--effort".into(), effort.into()]);
    }
    stage_settings(options, config, workspace, &mut args)?;
    if let Some(schema) = schema {
        let path = stage_file(
            workspace,
            "output.schema.json",
            schema.as_bytes(),
            "Cannot stage output schema",
        )?;
        args.extend(["--json-schema".into(), path]);
    }
    Ok(args)
}
