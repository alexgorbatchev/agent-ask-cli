use crate::{
    input::{InputFormat, Source},
    output::{Error, Failure, OutputMode},
    request::{self, Harness, Options, Thinking},
};
use clap::{
    Arg, ArgAction, ArgGroup, ArgMatches, Command,
    builder::{StyledStr, TypedValueParser},
};
use cli_command_kit::{HelpOptions, Mode, TechInfo};
use std::{
    ffi::OsString,
    io::{Read, Write},
    path::PathBuf,
    time::Duration,
};

const CLI: &str = "agent-ask";
const VERSION: &str = env!("CARGO_PKG_VERSION");
const TARGET_REPO: &str = "alexgorbatchev/agent-ask-cli-rust";
const RUN_LONG: &str = "Exactly one prompt source is required.
Each of prompt, input, input schema and output schema accepts at most one of its literal, file and stdin forms.
At most one of them may read stdin.";

struct SourceFlags {
    literal: &'static str,
    file: &'static str,
    stdin: &'static str,
    description: &'static str,
}

const PROMPT: SourceFlags = SourceFlags {
    literal: "prompt",
    file: "prompt-file",
    stdin: "prompt-stdin",
    description: "Instructions",
};
const INPUT: SourceFlags = SourceFlags {
    literal: "input",
    file: "input-file",
    stdin: "input-stdin",
    description: "Input data",
};
const INPUT_SCHEMA: SourceFlags = SourceFlags {
    literal: "input-schema",
    file: "input-schema-file",
    stdin: "input-schema-stdin",
    description: "Input JSON Schema",
};
const OUTPUT_SCHEMA: SourceFlags = SourceFlags {
    literal: "output-schema",
    file: "output-schema-file",
    stdin: "output-schema-stdin",
    description: "Output JSON Schema",
};

impl SourceFlags {
    fn add(&self, command: Command) -> Command {
        command
            .arg(string_flag(
                self.literal,
                format!("{} as literal content", self.description),
            ))
            .arg(path_flag(
                self.file,
                format!("{} from a UTF-8 file", self.description),
            ))
            .arg(bool_flag(
                self.stdin,
                format!("{} from stdin", self.description),
            ))
            .group(ArgGroup::new(format!("{}-source", self.literal)).args([
                self.literal,
                self.file,
                self.stdin,
            ]))
    }

    fn read(&self, matches: &ArgMatches) -> Option<Source> {
        if let Some(value) = matches.get_one::<String>(self.literal) {
            return Some(Source::Literal(value.clone()));
        }
        if let Some(path) = matches.get_one::<PathBuf>(self.file) {
            return Some(Source::File(path.clone()));
        }
        matches
            .get_one::<bool>(self.stdin)
            .copied()
            .map(Source::Stdin)
    }
}

fn string_flag(name: &'static str, help: impl Into<StyledStr>) -> Arg {
    Arg::new(name)
        .long(name)
        .value_name("string")
        .allow_hyphen_values(true)
        .help(help)
}

fn path_flag(name: &'static str, help: impl Into<StyledStr>) -> Arg {
    Arg::new(name)
        .long(name)
        .value_name("path")
        .allow_hyphen_values(true)
        .value_parser(clap::builder::OsStringValueParser::new().map(PathBuf::from))
        .help(help)
}

fn bool_flag(name: &'static str, help: impl Into<StyledStr>) -> Arg {
    Arg::new(name)
        .long(name)
        .value_name("bool")
        .num_args(0..=1)
        .require_equals(true)
        .default_missing_value("true")
        .value_parser(parse_bool)
        .help(help)
}

fn parse_bool(value: &str) -> Result<bool, &'static str> {
    match value {
        "1" | "t" | "T" | "true" | "TRUE" | "True" => Ok(true),
        "0" | "f" | "F" | "false" | "FALSE" | "False" => Ok(false),
        _ => Err("Expected a Boolean value"),
    }
}

fn run_command() -> Command {
    let command = Command::new("run")
        .about("Submit one request and return its validated answer")
        .long_about(RUN_LONG)
        .args_override_self(true)
        .arg(string_flag(
            "harness",
            "Installed harness: claude, codex, or agy (alias: antigravity) (required)",
        ))
        .arg(
            string_flag("input-format", "Input interpretation: text or json").default_value("text"),
        )
        .arg(
            string_flag(
                "harness-config-mode",
                "Native configuration baseline: inherit or clean",
            )
            .default_value("inherit"),
        )
        .arg(path_flag(
            "harness-config-file",
            "Native configuration overlay file: Claude JSON, Codex TOML, or Antigravity JSON",
        ))
        .arg(path_flag(
            "harness-home-dir",
            "Alternate native configuration/state/authentication directory",
        ))
        .arg(string_flag(
            "model",
            "Native model name; omitted uses harness default",
        ))
        .arg(string_flag(
            "effort",
            "Native reasoning effort; omitted uses harness default",
        ))
        .arg(string_flag("thinking", "Thinking selection: auto, on, or off").default_value("auto"))
        .arg(path_flag(
            "working-dir",
            "Harness working directory; omitted uses caller directory",
        ))
        .arg(path_flag(
            "harness-executable-file",
            "Harness executable file; omitted resolves harness on PATH",
        ))
        .arg(
            string_flag("timeout", "Positive request timeout")
                .value_name("duration")
                .default_value("5m0s")
                .value_parser(|value: &str| {
                    go_parse_duration::parse_duration(value)
                        .map(|nanos| Duration::from_nanos(u64::try_from(nanos).unwrap_or_default()))
                        .map_err(|error| format!("{error:?}"))
                }),
        )
        .arg(bool_flag(
            "output-json",
            "Print one validated JSON result envelope (default)",
        ))
        .arg(bool_flag(
            "output-plain",
            "Print only the validated final answer",
        ))
        .arg(bool_flag(
            "output-stream",
            "Stream sequenced NDJSON events and one terminal result or error",
        ))
        .group(ArgGroup::new("output-mode").args(["output-json", "output-plain", "output-stream"]));
    [PROMPT, INPUT, INPUT_SCHEMA, OUTPUT_SCHEMA]
        .iter()
        .fold(command, |command, source| source.add(command))
}

fn help_options() -> HelpOptions {
    HelpOptions {
        skill: Some(
            cli_command_kit::SkillSource::new(TARGET_REPO, "main")
                .expect("the fixed repository and its branch are valid"),
        ),
        catalog: [(
            "agent-ask request run".into(),
            TechInfo {
                metadata: [
                    ("stdout".into(), "json|plain|ndjson".into()),
                    ("authentication".into(), "native harness".into()),
                ]
                .into(),
                ..TechInfo::default()
            },
        )]
        .into(),
        ..HelpOptions::default()
    }
}

fn command(mode: Mode, options: &HelpOptions) -> Command {
    let root = Command::new(CLI)
        .about("Run one request through an installed coding harness")
        .version(VERSION)
        .disable_version_flag(true)
        .disable_help_subcommand(true)
        .arg(
            Arg::new("version")
                .long("version")
                .short('v')
                .action(ArgAction::SetTrue)
                .exclusive(true)
                .help("Print version"),
        )
        .subcommand(
            Command::new("request")
                .about("Submit fresh harness requests")
                .disable_help_subcommand(true)
                .subcommand(run_command()),
        )
        .subcommand(
            Command::new("completion")
                .about("Generate the autocompletion script for the specified shell")
                .disable_help_subcommand(true)
                .subcommands(["bash", "fish", "powershell", "zsh"].map(|name| {
                    Command::new(name)
                        .about(format!("Generate the autocompletion script for {name}"))
                        .arg(bool_flag("no-descriptions", "Omit descriptions"))
                })),
        )
        .subcommand(
            Command::new("help")
                .about("Print help for a command")
                .hide(true)
                .arg(
                    Arg::new("command")
                        .num_args(0..)
                        .action(ArgAction::Append)
                        .help("Command path"),
                ),
        );
    cli_command_kit::setup(root, mode, options).expect("the static command tree is valid")
}

pub fn run(
    args: impl IntoIterator<Item = impl Into<OsString> + Clone>,
    mode: Mode,
    stdin: &mut dyn Read,
    mut stdout: &mut dyn Write,
) -> Result<(), Failure> {
    let help = help_options();
    let mut root = command(mode, &help);
    let matches = match root.clone().try_get_matches_from(args) {
        Ok(matches) => matches,
        Err(error) if error.kind() == clap::error::ErrorKind::DisplayHelp => {
            return write_text(stdout, &error.to_string()).map_err(Into::into);
        }
        Err(_) => {
            return Err(Error::invalid_request(
                "Invalid command or arguments; use --help for the accepted interface",
            )
            .into());
        }
    };
    if matches.get_flag("version") {
        return write_text(stdout, &format!("{VERSION}\n")).map_err(Into::into);
    }
    if cli_command_kit::write_skill(&root, &matches, &help, &mut stdout).map_err(Error::from)? {
        return stdout.flush().map_err(Error::from).map_err(Into::into);
    }
    match matches.subcommand() {
        Some(("help", flags)) => command_help(
            &mut root,
            flags
                .get_many::<String>("command")
                .into_iter()
                .flatten()
                .map(String::as_str),
            stdout,
        ),
        Some(("request", group)) => {
            let Some(("run", flags)) = group.subcommand() else {
                return command_help(&mut root, ["request"], stdout);
            };
            let output = if flag(flags, "output-stream") {
                OutputMode::Stream
            } else if flag(flags, "output-plain") {
                OutputMode::Plain
            } else {
                OutputMode::Json
            };
            request::run(options(flags), stdin, stdout, output)
        }
        Some(("completion", group)) => {
            let Some((name, flags)) = group.subcommand() else {
                return command_help(&mut root, ["completion"], stdout);
            };
            let shell = name
                .parse::<clap_complete::Shell>()
                .map_err(|_| Error::invalid_request("Unsupported completion shell"))?;
            if flag(flags, "no-descriptions") {
                root = strip_descriptions(root);
            }
            let mut output = Vec::new();
            clap_complete::generate(shell, &mut root, CLI, &mut output);
            stdout
                .write_all(&output)
                .and_then(|()| stdout.flush())
                .map_err(Error::from)
                .map_err(Into::into)
        }
        _ => write_text(stdout, &root.render_help().to_string()).map_err(Into::into),
    }
}

fn strip_descriptions(command: Command) -> Command {
    command
        .about(clap::builder::Resettable::Reset)
        .long_about(clap::builder::Resettable::Reset)
        .mut_args(|arg| {
            arg.help(clap::builder::Resettable::Reset)
                .long_help(clap::builder::Resettable::Reset)
        })
        .mut_subcommands(strip_descriptions)
}

fn command_help<'a>(
    mut command: &mut Command,
    path: impl IntoIterator<Item = &'a str>,
    stdout: &mut dyn Write,
) -> Result<(), Failure> {
    for name in path {
        command = command
            .find_subcommand_mut(name)
            .ok_or_else(|| Error::invalid_request("Unknown command in help path"))?;
    }
    write_text(stdout, &command.render_help().to_string()).map_err(Into::into)
}

fn write_text(out: &mut dyn Write, text: &str) -> Result<(), Error> {
    out.write_all(text.as_bytes())?;
    out.flush()?;
    Ok(())
}

fn flag(matches: &ArgMatches, name: &str) -> bool {
    matches.get_one::<bool>(name).copied().unwrap_or_default()
}

fn options(matches: &ArgMatches) -> Result<Options, Error> {
    let string = |name| matches.get_one::<String>(name).map_or("", String::as_str);
    let text = |name| {
        matches
            .get_one::<String>(name)
            .filter(|value| !value.is_empty())
            .cloned()
    };
    let path = |name| {
        matches
            .get_one::<PathBuf>(name)
            .filter(|value| !value.as_os_str().is_empty())
            .cloned()
    };
    let harness = Harness::parse(string("harness"))?;
    let timeout = matches
        .get_one::<Duration>("timeout")
        .copied()
        .filter(|value| !value.is_zero())
        .ok_or_else(|| Error::invalid_request("--timeout must be positive"))?;
    let clean = match string("harness-config-mode") {
        "clean" => true,
        "inherit" => false,
        _ => {
            return Err(Error::invalid_request(
                "--harness-config-mode must be inherit or clean",
            ));
        }
    };
    let input_format = match string("input-format") {
        "text" => InputFormat::Text,
        "json" => InputFormat::Json,
        _ => {
            return Err(Error::invalid_request(
                "--input-format must be text or json",
            ));
        }
    };
    let thinking = match string("thinking") {
        "auto" => Thinking::Auto,
        "on" => Thinking::On,
        "off" => Thinking::Off,
        _ => {
            return Err(Error::invalid_request(
                "--thinking must be auto, on, or off",
            ));
        }
    };
    Ok(Options {
        harness,
        prompt: PROMPT.read(matches),
        input: INPUT.read(matches),
        input_schema: INPUT_SCHEMA.read(matches),
        output_schema: OUTPUT_SCHEMA.read(matches),
        input_format,
        clean,
        config_file: path("harness-config-file"),
        home_dir: path("harness-home-dir"),
        model: text("model"),
        effort: text("effort"),
        thinking,
        work_dir: path("working-dir"),
        executable: path("harness-executable-file"),
        timeout,
    })
}

#[cfg(test)]
mod tests {
    #[test]
    fn skill_documents_the_command_tree() {
        let options = super::help_options();
        let command = super::command(super::Mode::Agent, &options);
        let skill = include_str!("../skills/agent-ask/SKILL.md");
        let missing = cli_command_kit::skill_omissions(&command, &options, skill);
        assert!(missing.is_empty(), "Skill omissions: {missing:?}");
    }
}
