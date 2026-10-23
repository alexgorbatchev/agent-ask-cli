package request

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	parser "github.com/alexgorbatchev/agent-parser"
)

const maxRecordBytes = 32 << 20

// Run publishes exactly one validated answer, or a sequenced live stream.
func Run(ctx context.Context, o Options, stdin io.Reader, stdout io.Writer) (err error) {
	e := emitter{w: stdout, streaming: o.OutputMode == "stream"}
	defer func() {
		if err != nil && e.streaming {
			if writeErr := e.send("error", ErrorValue(err)); writeErr != nil {
				err = writeErr
			}
		}
	}()
	p, err := prepare(o, stdin)
	if err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "agent-ask-cli-")
	if err != nil {
		return failure("configuration_failed", "Cannot create request workspace")
	}
	defer func() {
		if temp == "" {
			return
		}
		if cleanupErr := os.RemoveAll(temp); cleanupErr != nil && err == nil {
			err = failure("configuration_failed", "Cannot remove request workspace")
		}
	}()
	v, err := buildInvocation(o, p, temp)
	if err != nil {
		return err
	}
	live, err := parser.NewLiveParser(o.Harness)
	if err != nil {
		return failure("invalid_request", "Unsupported harness")
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, v.executable, v.args...)
	cmd.Dir = v.dir
	cmd.Env = v.env
	cmd.Stdin = strings.NewReader(p.prompt)
	// Native stderr can contain config secrets, prompts and panic stacks. Drain it
	// without forwarding; structured failure codes are the public diagnostic API.
	cmd.Stderr = io.Discard
	owner, err := ownProcess(cmd)
	if err != nil {
		return failure("harness_failed", "Cannot establish harness process ownership")
	}
	defer owner.close()
	cmd.WaitDelay = time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return failure("harness_failed", "Cannot open harness output")
	}
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return contextFailure(ctx)
		}
		return failure("harness_unavailable", "Cannot start selected harness executable")
	}
	if err := owner.afterStart(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if ctx.Err() != nil {
			return contextFailure(ctx)
		}
		return failure("harness_failed", "Cannot own harness process tree")
	}
	if err := e.send("started", map[string]string{"harness": o.Harness}); err != nil {
		cancel()
		_ = cmd.Wait()
		return err
	}
	scan := bufio.NewScanner(pipe)
	scan.Buffer(make([]byte, 64<<10), maxRecordBytes)
	var streamErr error
	for scan.Scan() {
		events, err := live.ParseLine(scan.Bytes())
		if err != nil {
			streamErr = failure("harness_failed", "Harness emitted invalid live JSON")
			break
		}
		for _, event := range events {
			if event.Data.Result != nil {
				event.Data.Result.ErrorMessage = ""
			}
			if event.EventType == "error" {
				message := "Harness reported an error"
				event.Data.Content = &message
				event.Data.Native = nil
			}
			if err := e.send("event", event); err != nil {
				streamErr = err
				break
			}
		}
		if streamErr != nil {
			break
		}
	}
	if scan.Err() != nil {
		streamErr = failure("harness_failed", "Cannot read harness output or native record exceeds 32 MiB")
	}
	if streamErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	// Descendants retaining pipes or running tools cannot outlive this request.
	_ = owner.kill()
	if streamErr != nil {
		return streamErr
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failure("timed_out", "Harness request exceeded its timeout")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return failure("cancelled", "Harness request was cancelled")
	}
	if waitErr != nil {
		return failure("harness_failed", "Harness exited unsuccessfully")
	}
	native, done := live.Result()
	if !done || native.IsError {
		return failure("harness_failed", "Harness did not return a successful final result")
	}
	result, err := validatedResult(o, p, native)
	if err != nil {
		return err
	}
	// Cleanup precedes publishing success: a cleanup failure must not produce two
	// terminal events. The deferred cleanup remains for every failure path.
	if err := os.RemoveAll(temp); err != nil {
		return failure("configuration_failed", "Cannot remove request workspace")
	}
	temp = ""
	return e.result(o.OutputMode, result)
}

func contextFailure(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failure("timed_out", "Harness request exceeded its timeout")
	}
	return failure("cancelled", "Harness request was cancelled")
}

func validatedResult(o Options, p prepared, n parser.LiveResult) (Result, error) {
	r := Result{Harness: o.Harness, OutputKind: "text", Output: n.Text, Model: n.Model, SessionID: n.SessionID, Usage: n.Usage}
	if p.schema == nil {
		if !n.HasText {
			return r, failure("output_invalid", "Harness returned no final text answer")
		}
		return r, nil
	}
	data := n.StructuredOutput
	if len(data) == 0 {
		if !n.HasText {
			return r, failure("output_invalid", "Harness returned no structured answer")
		}
		data = []byte(n.Text)
	}
	output, err := decodeJSON(data)
	if err != nil {
		return r, failure("output_invalid", "Final answer must contain exactly one JSON value")
	}
	if err := p.schema.Validate(output); err != nil {
		return r, failure("output_invalid", "Final answer does not match its JSON Schema")
	}
	r.OutputKind = "json"
	r.Output = output
	return r, nil
}
