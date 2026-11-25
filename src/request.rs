use crate::{
    harness,
    input::{self, InputFormat, Source},
    output::{Answer, Emitter, Error, Failure, OutputMode},
    process,
};
use agent_parser::LiveParser;
use std::{
    io::{Read, Write},
    path::PathBuf,
    time::Duration,
};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Harness {
    Claude,
    Codex,
    Agy,
}

impl Harness {
    pub fn parse(name: &str) -> Result<Self, Error> {
        match name {
            "claude" => Ok(Self::Claude),
            "codex" => Ok(Self::Codex),
            "agy" | "antigravity" => Ok(Self::Agy),
            _ => Err(Error::invalid_request(
                "--harness must be claude, codex, or agy",
            )),
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Self::Claude => "claude",
            Self::Codex => "codex",
            Self::Agy => "agy",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Thinking {
    Auto,
    On,
    Off,
}

pub struct Options {
    pub harness: Harness,
    pub prompt: Option<Source>,
    pub input: Option<Source>,
    pub input_schema: Option<Source>,
    pub output_schema: Option<Source>,
    pub input_format: InputFormat,
    pub clean: bool,
    pub config_file: Option<PathBuf>,
    pub home_dir: Option<PathBuf>,
    pub model: Option<String>,
    pub effort: Option<String>,
    pub thinking: Thinking,
    pub work_dir: Option<PathBuf>,
    pub executable: Option<PathBuf>,
    pub timeout: Duration,
}

pub fn run(
    options: Result<Options, Error>,
    stdin: &mut dyn Read,
    stdout: &mut dyn Write,
    mode: OutputMode,
) -> Result<(), Failure> {
    let mut emitter = Emitter::new(stdout, mode);
    run_inner(options, stdin, &mut emitter).map_err(|error| emitter.failure(error))
}

fn run_inner(
    options: Result<Options, Error>,
    stdin: &mut dyn Read,
    emitter: &mut Emitter<'_>,
) -> Result<(), Error> {
    let options = options?;
    let prepared = input::prepare(&options, stdin)?;
    let workspace = tempfile::Builder::new()
        .prefix("agent-ask-cli-")
        .tempdir()
        .map_err(|_| Error::configuration("Cannot create request workspace"))?;
    let invocation = harness::build(&options, &prepared, workspace.path())?;
    let harness = options.harness.name();
    let mut live =
        LiveParser::new(harness).map_err(|_| Error::invalid_request("Unsupported harness"))?;
    process::run(invocation, options.timeout, harness, &mut live, emitter)?;
    let native = live
        .result()
        .filter(|result| !result.is_error)
        .ok_or_else(|| Error::harness("Harness did not return a successful final result"))?;
    let (output, structured) = prepared.answer_output(native)?;
    let answer = Answer::new(harness, native, output, structured);
    // Complete fallible cleanup before publishing the terminal success.
    workspace
        .close()
        .map_err(|_| Error::configuration("Cannot remove request workspace"))?;
    emitter.answer(&answer)
}
