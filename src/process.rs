use crate::{
    harness::Invocation,
    output::{Emitter, Error},
};
use agent_parser::LiveParser;
use process_wrap::tokio::CommandWrap;
#[cfg(windows)]
use process_wrap::tokio::JobObject;
#[cfg(unix)]
use process_wrap::tokio::ProcessGroup;
use serde_json::json;
use std::{io, process::Stdio, time::Duration};
use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader},
    process::{ChildStdout, Command},
};

const MAX_RECORD: usize = 32 << 20;

pub fn run(
    invocation: Invocation,
    timeout: Duration,
    harness: &str,
    live: &mut LiveParser,
    emitter: &mut Emitter<'_>,
) -> Result<(), Error> {
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_io()
        .enable_time()
        .build()
        .map_err(|_| Error::harness("Cannot initialize process runtime"))?;
    runtime.block_on(execute(invocation, timeout, harness, live, emitter))
}

async fn execute(
    invocation: Invocation,
    timeout: Duration,
    harness: &str,
    live: &mut LiveParser,
    emitter: &mut Emitter<'_>,
) -> Result<(), Error> {
    let signal = Cancellation::new()
        .map_err(|_| Error::harness("Cannot listen for request cancellation"))?;
    let Invocation {
        executable,
        args,
        work_dir,
        home,
        stdin,
    } = invocation;
    let mut command = Command::new(executable);
    command
        .args(args)
        .current_dir(work_dir)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        // Native diagnostics may include secrets or prompt data.
        .stderr(Stdio::null())
        .kill_on_drop(true);
    if let Some((key, path)) = home {
        command.env(key, path);
    }
    let mut command = CommandWrap::from(command);
    #[cfg(unix)]
    command.wrap(ProcessGroup::leader());
    #[cfg(windows)]
    command.wrap(JobObject);
    let mut child = command.spawn().map_err(|_| {
        Error::new(
            "harness_unavailable",
            "Cannot start selected harness executable",
        )
    })?;
    let mut tree_signalled = false;
    let result = async {
        let mut child_stdin = child
            .stdin()
            .take()
            .ok_or_else(|| Error::harness("Cannot open harness input"))?;
        let stdout = child
            .stdout()
            .take()
            .ok_or_else(|| Error::harness("Cannot open harness output"))?;
        emitter.send("started", &json!({ "harness": harness }))?;
        let input = async move {
            match child_stdin.write_all(stdin.as_bytes()).await {
                Ok(()) => Ok(()),
                Err(error) if error.kind() == io::ErrorKind::BrokenPipe => Ok(()),
                Err(_) => Err(Error::harness("Cannot write harness input")),
            }
        };
        let parent = async {
            // Wait for the original process without waiting on descendants that
            // might keep the stdout pipe open. Kill the owned tree as soon as
            // the parent exits, then let the reader drain the buffered bytes.
            let status = child
                .inner_mut()
                .wait()
                .await
                .map_err(|_| Error::harness("Cannot wait for selected harness"))?;
            tree_signalled = child.start_kill().is_ok();
            Ok(status)
        };
        let (_, (), status) = tokio::try_join!(input, consume(stdout, live, emitter), parent)?;
        if !status.success() {
            return Err(Error::harness("Harness exited unsuccessfully"));
        }
        Ok(())
    };
    let result = tokio::select! {
        biased;
        _ = signal.wait() => Err(Error::new("cancelled", "Harness request was cancelled")),
        _ = tokio::time::sleep(timeout) => Err(Error::new("timed_out", "Harness request exceeded its timeout")),
        result = result => result,
    };
    // This also runs when malformed stdout or a transport failure interrupted
    // the reader before the parent could exit.
    if !tree_signalled {
        let _ = child.start_kill();
    }
    let _ = child.wait().await;
    result
}

async fn consume(
    stdout: ChildStdout,
    live: &mut LiveParser,
    emitter: &mut Emitter<'_>,
) -> Result<(), Error> {
    let mut reader = BufReader::with_capacity(64 << 10, stdout);
    let mut line = Vec::new();
    loop {
        let chunk = reader.fill_buf().await.map_err(|_| record_error())?;
        let end = chunk.iter().position(|&byte| byte == b'\n');
        let take = end.unwrap_or(chunk.len());
        if line.len() + take > MAX_RECORD {
            return Err(record_error());
        }
        if end.is_none() && chunk.is_empty() && line.is_empty() {
            return Ok(());
        }
        let eof = chunk.is_empty();
        line.extend_from_slice(&chunk[..take]);
        reader.consume(take + usize::from(end.is_some()));
        if end.is_none() && !eof {
            continue;
        }
        let events = live
            .parse_line(&line)
            .map_err(|_| Error::harness("Harness emitted invalid live JSON"))?;
        for mut event in events {
            if let Some(result) = &mut event.data.result {
                result.error_message.clear();
            }
            if event.event_type == "error" {
                event.data.content = Some("Harness reported an error".into());
                event.data.native = None;
            }
            emitter.send("event", &event)?;
        }
        line.clear();
    }
}

const fn record_error() -> Error {
    Error::harness("Cannot read harness output or native record exceeds 32 MiB")
}

struct Cancellation {
    #[cfg(unix)]
    interrupt: tokio::signal::unix::Signal,
    #[cfg(unix)]
    terminate: tokio::signal::unix::Signal,
    #[cfg(windows)]
    interrupt: tokio::signal::windows::CtrlC,
}

impl Cancellation {
    fn new() -> io::Result<Self> {
        #[cfg(unix)]
        {
            use tokio::signal::unix::{SignalKind, signal};
            Ok(Self {
                interrupt: signal(SignalKind::interrupt())?,
                terminate: signal(SignalKind::terminate())?,
            })
        }
        #[cfg(windows)]
        Ok(Self {
            interrupt: tokio::signal::windows::ctrl_c()?,
        })
    }

    async fn wait(mut self) {
        #[cfg(unix)]
        tokio::select! {
            _ = self.interrupt.recv() => (),
            _ = self.terminate.recv() => (),
        }
        #[cfg(windows)]
        self.interrupt.recv().await;
    }
}
