package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/alexgorbatchev/agent-ask-cli/internal/request"
)

func executeCommand(args ...string) (string, error) {
	var out bytes.Buffer
	cmd := newRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// runCommand runs args through run and returns the exit code and both channels.
func runCommand(t *testing.T, stdout io.Writer, args ...string) (int, string) {
	t.Helper()
	var stderr bytes.Buffer
	cmd := newRootCommand()
	cmd.SetContext(context.Background())
	cmd.SetOut(stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("input text"))
	cmd.SetArgs(args)
	return run(cmd), stderr.String()
}

// errorCode parses the single JSON error record stderr must hold.
func errorCode(t *testing.T, stderr string) string {
	t.Helper()
	var envelope struct {
		Error request.Error `json:"error"`
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Fatalf("want one record, got %q", stderr)
	}
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
		t.Fatalf("stderr %q: %v", stderr, err)
	}
	return envelope.Error.Code
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// flakyWriter fails only its first write.
type flakyWriter struct {
	bytes.Buffer
	failed bool
}

func (w *flakyWriter) Write(p []byte) (int, error) {
	if !w.failed {
		w.failed = true
		return 0, errors.New("disk full")
	}
	return w.Buffer.Write(p)
}

func TestRun(t *testing.T) {
	const stream, stderrChannel = "stdout stream record", "stderr"
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			for _, tt := range []struct {
				args    []string
				channel string
			}{
				{[]string{"unknown"}, stderrChannel},
				{[]string{"request", "run"}, stderrChannel},
				{[]string{"request", "run", "--harness", "codex", "--prompt", "hi", "--prompt-file", "file"}, stderrChannel},
				{[]string{"request", "run", "--harness", "claude", "--prompt-file="}, stderrChannel},
				{[]string{"request", "run", "--harness", "claude", "--prompt-stdin=false"}, stderrChannel},
				{[]string{"request", "run", "--harness", "codex", "--prompt", "hi", "--output-stream", "--output-plain"}, stderrChannel},
				{[]string{"request", "run", "--harness", "codex", "--prompt", "hi", "--output-stream", "--bogus"}, stderrChannel},
				{[]string{"request", "run", "--harness", "claude", "--output-stream"}, stream},
				{[]string{"request", "run", "--prompt", "hi", "--output-stream"}, stream},
				{[]string{"request", "run", "--harness", "claude", "--prompt-file=", "--output-stream"}, stream},
				{[]string{"request", "run", "--harness", "claude", "--prompt-stdin=false", "--output-stream"}, stream},
			} {
				t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
					var out bytes.Buffer
					code, stderr := runCommand(t, &out, tt.args...)
					if code != 1 {
						t.Fatalf("exit %d", code)
					}
					if tt.channel == stderrChannel {
						if out.Len() != 0 {
							t.Fatalf("stdout %q", out.String())
						}
						if got := errorCode(t, stderr); got != "invalid_request" {
							t.Fatalf("code %q", got)
						}
						return
					}
					if stderr != "" {
						t.Fatalf("stderr %q", stderr)
					}
					var record struct {
						Sequence int    `json:"sequence"`
						Type     string `json:"type"`
					}
					if err := json.Unmarshal(out.Bytes(), &record); err != nil {
						t.Fatalf("stdout %q: %v", out.String(), err)
					}
					if record.Sequence != 1 || record.Type != "error" {
						t.Fatalf("record %+v", record)
					}
				})
			}
		})
	}
}

func TestVersionOutput(t *testing.T) {
	var out bytes.Buffer
	code, stderr := runCommand(t, &out, "--version")
	if code != 0 || out.String() != version+"\n" || stderr != "" {
		t.Fatal(code, out.String(), stderr)
	}
}

func TestStdoutFailuresReportOutputFailed(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			for _, args := range [][]string{
				nil,
				{"--help"},
				{"help"},
				{"help", "request", "run"},
				{"request", "run", "--help"},
				{"skill"},
				{"--version"},
				{"completion", "bash"},
				{"request", "run", "--harness", "claude", "--output-stream"},
			} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					code, stderr := runCommand(t, failingWriter{}, args...)
					if code != 1 {
						t.Fatalf("exit %d", code)
					}
					if got := errorCode(t, stderr); got != "output_failed" {
						t.Fatalf("code %q", got)
					}
				})
			}
		})
	}
}

func TestStreamDeliveredTerminalRecordKeepsStderrEmpty(t *testing.T) {
	t.Setenv("AGENT_ASK_COMMAND_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The started record is lost, but the terminal error still reaches stdout.
	var out flakyWriter
	code, stderr := runCommand(t, &out, "request", "run", "--harness", "codex", "--harness-executable-file", exe, "--prompt", "hi", "--output-stream")
	if code != 1 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(out.String(), `"type":"error"`) || !strings.Contains(out.String(), "output_failed") {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestHelpForUnknownCommandsFails(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			for _, args := range [][]string{
				{"bogus", "--help"},
				{"help", "bogus"},
				{"request", "bogus", "--help"},
				{"help", "request", "bogus"},
				{"request", "run", "extra", "--help"},
			} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					var out bytes.Buffer
					code, stderr := runCommand(t, &out, args...)
					if code != 1 || out.Len() != 0 {
						t.Fatalf("code=%d stdout=%q", code, out.String())
					}
					if got := errorCode(t, stderr); got != "invalid_request" {
						t.Fatalf("code %q", got)
					}
				})
			}
			for _, args := range [][]string{
				nil,
				{"--help"},
				{"help"},
				{"help", "--help"},
				{"help", "request", "run"},
				{"help", "completion", "bash"},
				{"request"},
				{"request", "--help"},
				{"request", "run", "--help"},
				{"completion", "--help"},
			} {
				t.Run("valid "+strings.Join(args, " "), func(t *testing.T) {
					var out bytes.Buffer
					code, stderr := runCommand(t, &out, args...)
					if code != 0 || out.Len() == 0 || stderr != "" {
						t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr)
					}
				})
			}
		})
	}
}

func TestAgentHelpScreens(t *testing.T) {
	t.Setenv("AGENT", "1")
	run, err := executeCommand("request", "run", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  stdout: json|plain|ndjson\n", "summary: Submit one request and return its validated answer\n"} {
		if !strings.Contains(run, want) {
			t.Errorf("missing %q in:\n%s", want, run)
		}
	}
	root, err := executeCommand("--help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(root, "description:") {
		t.Errorf("root help repeats its summary:\n%s", root)
	}
	if !strings.Contains(root, "  - run: Submit one request and return its validated answer\n") {
		t.Errorf("root help must list the run command:\n%s", root)
	}
}

func TestRunHelpStatesSourceRules(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			t.Setenv("COLUMNS", "500")
			out, err := executeCommand("request", "run", "--help")
			if err != nil {
				t.Fatal(err)
			}
			for _, rule := range []string{"Exactly one prompt source is required.", "accepts at most one of its literal, file and stdin forms.", "At most one of them may read stdin."} {
				if !strings.Contains(out, rule) {
					t.Errorf("missing %q in:\n%s", rule, out)
				}
			}
		})
	}
}

func TestHumanHelpContent(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	t.Setenv("AGENT", "0")
	for _, tt := range []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{"╰─ run", "├─", "completion", "skill"}},
		{[]string{"request", "--help"}, []string{"╰─ run"}},
	} {
		out, err := executeCommand(tt.args...)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range tt.want {
			if !strings.Contains(out, want) {
				t.Errorf("%v missing %q:\n%s", tt.args, want, out)
			}
		}
		for _, agentKey := range []string{"command:", "subcommands:"} {
			if strings.Contains(out, agentKey) {
				t.Errorf("%v contains agent key %q:\n%s", tt.args, agentKey, out)
			}
		}
	}
}

func TestAgentValueSelectsHelpMode(t *testing.T) {
	for _, tt := range []struct {
		value   string
		isAgent bool
	}{{"TRUE", true}, {" 1 ", true}, {"yes", true}, {"on", false}, {"0", false}, {"", false}} {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("AGENT", tt.value)
			out, err := executeCommand("--help")
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(out, "command: agent-ask-cli\n"); got != tt.isAgent {
				t.Errorf("agent help = %v, want %v:\n%s", got, tt.isAgent, out)
			}
		})
	}
}

func TestRequestOutputIsIdenticalAcrossAgentModes(t *testing.T) {
	t.Setenv("AGENT_ASK_COMMAND_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := regexp.MustCompile(`"timestamp":\d+`)
	capture := func(t *testing.T, agent string, args []string) (int, string, string) {
		t.Helper()
		t.Setenv("AGENT", agent)
		var out bytes.Buffer
		code, stderr := runCommand(t, &out, args...)
		return code, timestamp.ReplaceAllString(out.String(), `"timestamp":0`), stderr
	}
	for _, harness := range []string{"codex", "claude"} {
		base := []string{"request", "run", "--harness", harness, "--harness-executable-file", exe, "--prompt", "summarize", "--input-stdin"}
		for name, args := range map[string][]string{
			"json":           append(slices.Clone(base), "--output-json"),
			"plain":          append(slices.Clone(base), "--output-plain"),
			"stream":         append(slices.Clone(base), "--output-stream"),
			"stream failure": {"request", "run", "--harness", harness, "--output-stream"},
			"json failure":   {"request", "run", "--harness", harness},
		} {
			t.Run(harness+" "+name, func(t *testing.T) {
				humanCode, humanOut, humanErr := capture(t, "0", args)
				agentCode, agentOut, agentErr := capture(t, "1", args)
				if humanCode != agentCode || humanOut != agentOut || humanErr != agentErr {
					t.Fatalf("modes differ:\nAGENT=0 %d %q %q\nAGENT=1 %d %q %q", humanCode, humanOut, humanErr, agentCode, agentOut, agentErr)
				}
			})
		}
	}
}

func TestHelpAndOutputSelection(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"request"}, {"request", "run", "--help"}, {"help", "request", "run"}, {"completion", "bash"}, {"completion", "fish"}, {"completion", "zsh"}, {"completion", "powershell"}} {
		out, err := executeCommand(args...)
		if err != nil || out == "" {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
	for _, flag := range []string{"--output-json", "--output-plain", "--output-stream"} {
		_, err := executeCommand("request", "run", "--harness", "codex", "--prompt", "hi", "--harness-executable-file", "agent-ask-missing", flag)
		if err == nil {
			t.Fatal("missing executable accepted")
		}
	}
}

func TestSuccessfulRequestCommand(t *testing.T) {
	t.Setenv("AGENT_ASK_COMMAND_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"codex", "claude"} {
		for _, mode := range []string{"--output-plain", "--output-json", "--output-stream"} {
			t.Run(harness+mode, func(t *testing.T) {
				var out bytes.Buffer
				code, stderr := runCommand(t, &out, "request", "run", "--harness", harness, "--harness-executable-file", exe, "--prompt", "summarize", "--input-stdin", mode)
				if code != 0 || stderr != "" {
					t.Fatalf("code=%d stderr=%q", code, stderr)
				}
				if mode == "--output-plain" && out.String() != "answer" {
					t.Fatalf("plain %q", out.String())
				}
				if !strings.Contains(out.String(), "answer") {
					t.Fatalf("answer %q", out.String())
				}
			})
		}
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("AGENT_ASK_COMMAND_HELPER") == "1" {
		input, err := io.ReadAll(os.Stdin)
		if err != nil || !strings.Contains(string(input), "input text") {
			os.Exit(2)
		}
		if len(os.Args) > 1 && os.Args[1] == "exec" {
			fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"answer"}}`)
			fmt.Println(`{"type":"turn.completed"}`)
		} else {
			fmt.Println(`{"type":"result","subtype":"success","result":"answer"}`)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestExecuteVersion(t *testing.T) {
	previousArgs, previousOut := os.Args, os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer func() { os.Args = previousArgs; os.Stdout = previousOut }()
	os.Args = []string{"agent-ask-cli", "--version"}
	os.Stdout = writer
	code := execute()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || string(data) != version+"\n" {
		t.Fatalf("code=%d output=%q", code, data)
	}
}
