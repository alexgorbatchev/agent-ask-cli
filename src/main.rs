mod cli;
mod harness;
mod input;
mod output;
mod process;
mod request;

use std::{io, process::ExitCode};

fn main() -> ExitCode {
    let result = cli::run(
        std::env::args_os(),
        cli_command_kit::Mode::from_env(),
        &mut io::stdin().lock(),
        &mut io::stdout().lock(),
    );
    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(failure) => {
            if !failure.delivered {
                let _ = output::write_error(&mut io::stderr().lock(), &failure.error);
            }
            ExitCode::FAILURE
        }
    }
}
