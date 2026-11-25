use agent_parser::{LiveResult, TokenUsage};
use serde::Serialize;
use serde_json::{Value, json};
use std::io::{self, Write};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum OutputMode {
    Json,
    Plain,
    Stream,
}

#[derive(Debug, Serialize)]
pub struct Error {
    pub code: &'static str,
    pub message: &'static str,
}

impl Error {
    pub const fn new(code: &'static str, message: &'static str) -> Self {
        Self { code, message }
    }

    pub const fn invalid_request(message: &'static str) -> Self {
        Self::new("invalid_request", message)
    }

    pub const fn configuration(message: &'static str) -> Self {
        Self::new("configuration_failed", message)
    }

    pub const fn unsupported(message: &'static str) -> Self {
        Self::new("unsupported_option", message)
    }

    pub const fn harness(message: &'static str) -> Self {
        Self::new("harness_failed", message)
    }

    pub const fn invalid_output(message: &'static str) -> Self {
        Self::new("output_invalid", message)
    }
}

impl From<io::Error> for Error {
    fn from(_: io::Error) -> Self {
        Self::new("output_failed", "Cannot write output")
    }
}

pub struct Failure {
    pub error: Error,
    pub delivered: bool,
}

impl From<Error> for Failure {
    fn from(error: Error) -> Self {
        Self {
            error,
            delivered: false,
        }
    }
}

#[derive(Serialize)]
pub struct Answer<'a> {
    harness: &'a str,
    output_kind: &'static str,
    pub output: Value,
    #[serde(skip_serializing_if = "str::is_empty")]
    model: &'a str,
    #[serde(skip_serializing_if = "str::is_empty")]
    session_id: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    usage: Option<&'a TokenUsage>,
}

impl<'a> Answer<'a> {
    pub fn new(harness: &'a str, native: &'a LiveResult, output: Value, structured: bool) -> Self {
        Self {
            harness,
            output_kind: if structured { "json" } else { "text" },
            output,
            model: &native.model,
            session_id: &native.session_id,
            usage: native.usage.as_ref(),
        }
    }
}

pub fn write_error(out: &mut dyn Write, error: &Error) -> Result<(), Error> {
    write_json(out, &json!({ "error": error }))
}

pub fn write_json(out: &mut dyn Write, value: &impl Serialize) -> Result<(), Error> {
    let mut bytes = serde_json::to_vec(value).map_err(io::Error::other)?;
    bytes.push(b'\n');
    out.write_all(&bytes)?;
    out.flush()?;
    Ok(())
}

pub struct Emitter<'a> {
    out: &'a mut dyn Write,
    mode: OutputMode,
    sequence: u64,
}

#[derive(Serialize)]
struct Record<'a, T> {
    sequence: u64,
    #[serde(rename = "type")]
    kind: &'static str,
    data: &'a T,
}

impl<'a> Emitter<'a> {
    pub fn new(out: &'a mut dyn Write, mode: OutputMode) -> Self {
        Self {
            out,
            mode,
            sequence: 0,
        }
    }

    pub fn send(&mut self, kind: &'static str, data: &impl Serialize) -> Result<(), Error> {
        if self.mode != OutputMode::Stream {
            return Ok(());
        }
        self.sequence += 1;
        write_json(
            self.out,
            &Record {
                sequence: self.sequence,
                kind,
                data,
            },
        )
    }

    pub fn failure(&mut self, error: Error) -> Failure {
        if self.mode != OutputMode::Stream {
            return error.into();
        }
        match self.send("error", &error) {
            Ok(()) => Failure {
                error,
                delivered: true,
            },
            Err(write_error) => write_error.into(),
        }
    }

    pub fn answer(&mut self, answer: &Answer<'_>) -> Result<(), Error> {
        match self.mode {
            OutputMode::Stream => self.send("result", answer),
            OutputMode::Json => write_json(self.out, answer),
            OutputMode::Plain => {
                if answer.output_kind == "text"
                    && let Some(text) = answer.output.as_str()
                {
                    self.out.write_all(text.as_bytes())?;
                    self.out.flush()?;
                    return Ok(());
                }
                write_json(self.out, &answer.output)
            }
        }
    }
}
