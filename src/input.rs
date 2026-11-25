use crate::{
    output::Error,
    request::{Harness, Options},
};
use agent_parser::LiveResult;
use jsonschema::Validator;
use serde_json::Value;
use std::{
    fs::File,
    io::{self, BufReader, Read},
    path::{Path, PathBuf},
};
use url::Url;

const MAX_INPUT: usize = 16 << 20;

#[derive(Debug)]
pub enum Source {
    Literal(String),
    File(PathBuf),
    Stdin(bool),
}

#[derive(Clone, Copy, Debug)]
pub enum InputFormat {
    Text,
    Json,
}

impl InputFormat {
    fn decode(self, raw: String) -> Result<Value, Error> {
        match self {
            Self::Text => Ok(Value::String(raw)),
            Self::Json => serde_json::from_str(&raw).map_err(|_| {
                Error::new("input_invalid", "Input must contain exactly one JSON value")
            }),
        }
    }

    fn label(self) -> &'static str {
        match self {
            Self::Text => "text",
            Self::Json => "json",
        }
    }
}

pub struct Prepared {
    pub prompt: String,
    output_schema: Option<(String, Validator)>,
}

impl Prepared {
    pub fn native_stdin(&self, harness: Harness) -> String {
        if harness == Harness::Agy {
            let mut input = serde_json::json!({
                "event": "user",
                "message": { "content": self.prompt },
            })
            .to_string();
            input.push('\n');
            input
        } else {
            self.prompt.clone()
        }
    }

    pub fn raw_output_schema(&self) -> Option<&str> {
        self.output_schema.as_ref().map(|(raw, _)| raw.as_str())
    }

    pub fn answer_output(&self, native: &LiveResult) -> Result<(Value, bool), Error> {
        let Some((_, schema)) = &self.output_schema else {
            return if native.has_text {
                Ok((Value::String(native.text.clone()), false))
            } else {
                Err(Error::invalid_output(
                    "Harness returned no final text answer",
                ))
            };
        };
        let value = match &native.structured_output {
            Some(value) => value.clone(),
            None if !native.has_text => {
                return Err(Error::invalid_output(
                    "Harness returned no structured answer",
                ));
            }
            None => serde_json::from_str(&native.text).map_err(|_| {
                Error::invalid_output("Final answer must contain exactly one JSON value")
            })?,
        };
        if !schema.is_valid(&value) {
            return Err(Error::invalid_output(
                "Final answer does not match its JSON Schema",
            ));
        }
        Ok((value, true))
    }
}

pub fn prepare(options: &Options, stdin: &mut dyn Read) -> Result<Prepared, Error> {
    let Some(prompt_source) = &options.prompt else {
        return Err(Error::invalid_request("Supply one prompt source"));
    };
    let mut stdin_count = 0;
    for source in [
        &options.prompt,
        &options.input,
        &options.input_schema,
        &options.output_schema,
    ]
    .into_iter()
    .flatten()
    {
        match source {
            Source::File(path) if path.as_os_str().is_empty() => {
                return Err(Error::invalid_request(
                    "File source paths must not be empty",
                ));
            }
            Source::Stdin(false) => {
                return Err(Error::invalid_request("Stdin source flags must be true"));
            }
            Source::Stdin(true) => stdin_count += 1,
            _ => (),
        }
    }
    if stdin_count > 1 {
        return Err(Error::invalid_request(
            "Only one content source may read stdin",
        ));
    }
    if options.input_schema.is_some() && options.input.is_none() {
        return Err(Error::invalid_request("An input schema requires input"));
    }
    let mut prompt = read_source(prompt_source, stdin)?;
    if prompt.trim().is_empty() {
        return Err(Error::invalid_request("Prompt must not be empty"));
    }
    if let Some(source) = &options.input {
        let raw = read_source(source, stdin)?;
        let input = options.input_format.decode(raw)?;
        if let Some(source) = &options.input_schema {
            let (raw, schema) = read_schema(source, stdin)?;
            if !schema.is_valid(&input) {
                return Err(Error::new(
                    "input_invalid",
                    "Input does not match its JSON Schema",
                ));
            }
            prompt.push_str("\n\nInput JSON Schema:\n");
            prompt.push_str(&raw);
        }
        let wrapped = input.to_string();
        prompt.push_str("\n\nInput data (JSON encoded ");
        prompt.push_str(options.input_format.label());
        prompt.push_str(" value):\n");
        prompt.push_str(&wrapped);
    }
    let output_schema = options
        .output_schema
        .as_ref()
        .map(|source| read_schema(source, stdin))
        .transpose()?;
    Ok(Prepared {
        prompt,
        output_schema,
    })
}

fn read_source(source: &Source, stdin: &mut dyn Read) -> Result<String, Error> {
    let mut bytes = Vec::new();
    let result = match source {
        Source::Literal(value) if value.len() <= MAX_INPUT => return Ok(value.clone()),
        Source::Literal(_) => return Err(invalid_input()),
        Source::Stdin(_) => stdin.take(MAX_INPUT as u64 + 1).read_to_end(&mut bytes),
        Source::File(path) => File::open(path)
            .map_err(|_| Error::new("invalid_input", "Cannot open the requested input file"))?
            .take(MAX_INPUT as u64 + 1)
            .read_to_end(&mut bytes),
    };
    if result.is_err() || bytes.len() > MAX_INPUT {
        return Err(invalid_input());
    }
    String::from_utf8(bytes).map_err(|_| invalid_input())
}

const fn invalid_input() -> Error {
    Error::new(
        "invalid_input",
        "Input must be readable UTF-8 and at most 16 MiB",
    )
}

fn read_schema(source: &Source, stdin: &mut dyn Read) -> Result<(String, Validator), Error> {
    let raw = read_source(source, stdin)?;
    let schema: Value = serde_json::from_str(&raw).map_err(|_| {
        Error::new(
            "invalid_schema",
            "Schema must contain exactly one valid JSON value",
        )
    })?;
    let path = match source {
        Source::File(path) => path.as_path(),
        _ => Path::new("agent-ask-inline.schema.json"),
    };
    let location = std::path::absolute(path)
        .and_then(|absolute| {
            Url::from_file_path(absolute).map_err(|()| io::Error::other("not a file URL"))
        })
        .map_err(|_| Error::new("invalid_schema", "Cannot resolve schema location"))?;
    let validator = jsonschema::options()
        .with_base_uri(location.as_str())
        .with_retriever(LocalSchemaFiles)
        .build(&schema)
        .map_err(|_| Error::new("invalid_schema", "Invalid JSON Schema or unavailable local reference; network references are disabled"))?;
    Ok((raw, validator))
}

struct LocalSchemaFiles;

impl jsonschema::Retrieve for LocalSchemaFiles {
    fn retrieve(
        &self,
        uri: &jsonschema::Uri<String>,
    ) -> Result<Value, Box<dyn std::error::Error + Send + Sync>> {
        let url = Url::parse(uri.as_str())?;
        if url.scheme() != "file" || url.host_str().is_some() {
            return Err("Schema references must resolve to local files".into());
        }
        let path = url.to_file_path().map_err(|()| "Invalid schema file URL")?;
        Ok(serde_json::from_reader(BufReader::new(File::open(path)?))?)
    }
}
