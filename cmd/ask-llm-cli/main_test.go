package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
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

func TestRun(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			for _, args := range [][]string{{"--version"}, {"unknown"}, {"request", "run"}, {"request", "run", "--harness", "codex", "--prompt", "hi", "--prompt-file", "file"}, {"request", "run", "--harness", "claude", "--prompt-file=", "--output-stream"}, {"request", "run", "--harness", "claude", "--prompt-stdin=false"}, {"request", "run", "--harness", "codex", "--prompt", "hi", "--output-stream", "--output-plain"}, {"request", "run", "--prompt", "hi", "--output-stream"}} {
				var out, stderr bytes.Buffer
				cmd := newRootCommand()
				cmd.SetContext(context.Background())
				cmd.SetOut(&out)
				cmd.SetErr(&stderr)
				cmd.SetArgs(args)
				code := run(cmd)
				if args[0] == "--version" {
					if code != 0 || out.String() != version+"\n" || stderr.Len() != 0 {
						t.Fatal(code, out.String(), stderr.String())
					}
					continue
				}
				if code != 1 {
					t.Fatalf("accepted args %v", args)
				}
				if out.Len() > 0 {
					if !strings.Contains(out.String(), `"type":"error"`) || stderr.Len() != 0 {
						t.Fatalf("out=%q stderr=%q", out.String(), stderr.String())
					}
				} else {
					var envelope map[string]any
					if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
						t.Fatalf("stderr %q: %v", stderr.String(), err)
					}
				}
			}
		})
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
		_, err := executeCommand("request", "run", "--harness", "codex", "--prompt", "hi", "--harness-executable-file", "ask-llm-missing", flag)
		if err == nil {
			t.Fatal("missing executable accepted")
		}
	}
}

func TestSuccessfulRequestCommand(t *testing.T) {
	t.Setenv("ASK_LLM_COMMAND_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"codex", "claude"} {
		for _, mode := range []string{"--output-plain", "--output-json", "--output-stream"} {
			t.Run(harness+mode, func(t *testing.T) {
				var out, stderr bytes.Buffer
				cmd := newRootCommand()
				cmd.SetOut(&out)
				cmd.SetErr(&stderr)
				cmd.SetIn(strings.NewReader("input text"))
				cmd.SetArgs([]string{"request", "run", "--harness", harness, "--harness-executable-file", exe, "--prompt", "summarize", "--input-stdin", mode})
				if code := run(cmd); code != 0 || stderr.Len() != 0 {
					t.Fatalf("code=%d stderr=%q", code, stderr.String())
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
	if os.Getenv("ASK_LLM_COMMAND_HELPER") == "1" {
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
	os.Args = []string{"ask-llm-cli", "--version"}
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
