package request

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("AGENT_ASK_TEST_HARNESS"); mode != "" {
		helper(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(mode string) {
	if mode == "codex-ui-state" {
		settings, err := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "config.toml"))
		if err != nil {
			os.Exit(8)
		}
		if bytes.Contains(settings, []byte("screen_reader_detection_done")) && slices.Contains(os.Args, "--strict-config") {
			fmt.Fprintln(os.Stderr, "unknown configuration field screen_reader_detection_done")
			os.Exit(2)
		}
		mode = "codex"
	}
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(8)
	}
	if path := os.Getenv("AGENT_ASK_CAPTURE"); path != "" {
		cwd, err := os.Getwd()
		if err != nil {
			os.Exit(8)
		}
		b, err := json.Marshal(map[string]any{"args": os.Args[1:], "prompt": string(prompt), "cwd": cwd, "codex_home": os.Getenv("CODEX_HOME"), "claude_home": os.Getenv("CLAUDE_CONFIG_DIR")})
		if err != nil {
			os.Exit(8)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			os.Exit(8)
		}
	}
	if mode == "sleep" {
		time.Sleep(time.Minute)
		return
	}
	if mode == "tree" {
		exe, err := os.Executable()
		if err != nil {
			os.Exit(8)
		}
		child := exec.Command(exe)
		child.Env = replaceEnv(os.Environ(), "AGENT_ASK_TEST_HARNESS", "child")
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			os.Exit(8)
		}
		if err := os.WriteFile(os.Getenv("AGENT_ASK_CHILD_PID"), []byte(fmt.Sprint(child.Process.Pid)), 0600); err != nil {
			os.Exit(8)
		}
		time.Sleep(time.Minute)
		return
	}
	if mode == "child" {
		for {
			if err := os.WriteFile(os.Getenv("AGENT_ASK_HEARTBEAT"), []byte(time.Now().String()), 0600); err != nil {
				os.Exit(8)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	fmt.Fprintln(os.Stderr, "secret native stderr must not leak")
	switch mode {
	case "bad-json":
		fmt.Println("broken")
		return
	case "oversized":
		fmt.Print(strings.Repeat("x", maxRecordBytes+1))
		return
	case "codex-native-error":
		fmt.Println(`{"type":"error","message":"secret"}`)
		fmt.Println(`{"type":"turn.failed","error":{"message":"secret"}}`)
		return
	case "no-result":
		fmt.Println(`{"type":"turn.started"}`)
		return
	case "codex", "codex-fail", "codex-schema", "codex-invalid", "codex-bigint":
		fmt.Println(`{"type":"thread.started","thread_id":"session"}`)
		fmt.Println(`{"type":"item.completed","item":{"id":"tool","type":"command_execution","exit_code":0,"aggregated_output":"ok"}}`)
		text := "answer\n"
		if mode == "codex-schema" {
			text = `{"title":"clean"}`
		}
		if mode == "codex-invalid" {
			text = `{"title":1}`
		}
		if mode == "codex-bigint" {
			text = `9007199254740993`
		}
		b, err := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": text}})
		if err != nil {
			os.Exit(8)
		}
		fmt.Println(string(b))
		fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":2,"cached_input_tokens":1}}`)
		if mode == "codex-fail" {
			os.Exit(2)
		}
	case "claude":
		fmt.Println(`{"type":"system","subtype":"init","model":"native-model","session_id":"session"}`)
		fmt.Println(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}}`)
		fmt.Println(`{"type":"result","subtype":"success","result":"earlier","usage":{"input_tokens":2}}`)
		fmt.Println(`{"type":"result","subtype":"success","result":"answer\n","usage":{"input_tokens":5,"output_tokens":2}}`)
	case "claude-schema":
		fmt.Println(`{"type":"result","subtype":"success","structured_output":{"title":"clean"}}`)
	case "claude-error":
		fmt.Println(`{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["secret"]}`)
	case "claude-empty":
		fmt.Println(`{"type":"result","subtype":"success"}`)
	}
}

func options(t *testing.T, harness string) Options {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Options{Harness: harness, Prompt: Source{Value: "fix titles", Set: true}, InputFormat: "text", ConfigMode: "inherit", Thinking: "auto", Timeout: 5 * time.Second, OutputMode: "json", Executable: exe, WorkingDir: t.TempDir()}
}

const titleSchema = `{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`

func TestRunCodexInheritsNativeUIState(t *testing.T) {
	t.Setenv("AGENT_ASK_TEST_HARNESS", "codex-ui-state")
	o := options(t, "codex")
	o.HomeDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(o.HomeDir, "config.toml"), []byte("screen_reader_detection_done = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), o, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Output != "answer\n" {
		t.Fatalf("unexpected answer %v", result.Output)
	}
}

func TestRunHarnessResults(t *testing.T) {
	cases := []struct{ mode, harness, output, schema, wantCode string }{
		{"codex", "codex", "json", "", ""}, {"claude", "claude", "plain", "", ""}, {"claude", "claude", "stream", "", ""},
		{"codex-schema", "codex", "json", titleSchema, ""}, {"claude-schema", "claude", "plain", titleSchema, ""}, {"codex-bigint", "codex", "plain", `{"type":"integer"}`, ""},
		{"codex-invalid", "codex", "json", titleSchema, "output_invalid"}, {"codex", "codex", "plain", titleSchema, "output_invalid"},
		{"codex-fail", "codex", "json", "", "harness_failed"}, {"claude-error", "claude", "stream", "", "harness_failed"}, {"claude-empty", "claude", "plain", "", "output_invalid"},
		{"no-result", "codex", "json", "", "harness_failed"}, {"bad-json", "codex", "stream", "", "harness_failed"},
		{"claude-empty", "claude", "plain", titleSchema, "output_invalid"},
		{"codex-native-error", "codex", "stream", "", "harness_failed"},
		{"oversized", "codex", "json", "", "harness_failed"},
	}
	for _, tt := range cases {
		t.Run(tt.mode+tt.output, func(t *testing.T) {
			t.Setenv("AGENT_ASK_TEST_HARNESS", tt.mode)
			o := options(t, tt.harness)
			o.OutputMode = tt.output
			if tt.schema != "" {
				o.OutputSchema = Source{Set: true, Value: tt.schema}
			}
			var out bytes.Buffer
			err := Run(context.Background(), o, strings.NewReader(""), &out)
			if tt.wantCode != "" {
				if err == nil || ErrorValue(err).Code != tt.wantCode {
					t.Fatalf("err=%v", err)
				}
				if tt.output != "stream" && out.Len() != 0 {
					t.Fatalf("failed buffered output %q", out.String())
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "secret") {
				t.Fatal("native diagnostic leaked")
			}
			if tt.output == "stream" {
				lines := strings.Split(strings.TrimSpace(out.String()), "\n")
				terminal := 0
				for i, line := range lines {
					var event struct {
						Sequence int    `json:"sequence"`
						Type     string `json:"type"`
					}
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Fatal(err)
					}
					if event.Sequence != i+1 {
						t.Fatalf("sequence %+v", event)
					}
					if event.Type == "result" || event.Type == "error" {
						terminal++
						if i != len(lines)-1 {
							t.Fatal("terminal event was not last")
						}
					}
				}
				if terminal != 1 {
					t.Fatalf("terminal count %d", terminal)
				}
			} else if tt.wantCode == "" && tt.output == "plain" {
				want := "answer\n"
				if tt.schema == titleSchema {
					want = "{\"title\":\"clean\"}\n"
				}
				if tt.mode == "codex-bigint" {
					want = "9007199254740993\n"
				}
				if out.String() != want {
					t.Fatalf("plain %q", out.String())
				}
			} else if tt.wantCode == "" {
				var r Result
				if err := json.Unmarshal(out.Bytes(), &r); err != nil {
					t.Fatal(err)
				}
				if r.Harness != tt.harness || r.SessionID != "session" {
					t.Fatalf("result %+v", r)
				}
				if tt.schema == "" && (r.Output != "answer\n" || r.Usage.Input != 5) {
					t.Fatalf("result %+v", r)
				}
			}
		})
	}
}

func TestRunCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			t.Setenv("AGENT_ASK_TEST_HARNESS", "sleep")
			o := options(t, "codex")
			o.Timeout = 80 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				go func() { time.Sleep(30 * time.Millisecond); cancel() }()
			}
			var out bytes.Buffer
			start := time.Now()
			err := Run(ctx, o, strings.NewReader(""), &out)
			want := "timed_out"
			if cancelled {
				want = "cancelled"
			}
			if err == nil || ErrorValue(err).Code != want {
				t.Fatalf("err %v", err)
			}
			if time.Since(start) > 3*time.Second || out.Len() != 0 {
				t.Fatal("cancellation did not finish promptly")
			}
		})
	}
}

func TestRunKillsDescendants(t *testing.T) {
	t.Setenv("AGENT_ASK_TEST_HARNESS", "tree")
	dir := t.TempDir()
	heartbeat := filepath.Join(dir, "heartbeat")
	t.Setenv("AGENT_ASK_HEARTBEAT", heartbeat)
	t.Setenv("AGENT_ASK_CHILD_PID", filepath.Join(dir, "pid"))
	o := options(t, "codex")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { defer close(done); done <- Run(ctx, o, strings.NewReader(""), io.Discard) }()
	defer func() { cancel(); <-done }()
	readyBy := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(heartbeat); err == nil {
			break
		}
		if time.Now().After(readyBy) {
			t.Fatal("descendant did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	err := <-done
	if err == nil || ErrorValue(err).Code != "cancelled" {
		t.Fatalf("err=%v", err)
	}
	before, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	after, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("descendant survived cancellation")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestRunOutputFailure(t *testing.T) {
	for _, mode := range []string{"plain", "json", "stream"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT_ASK_TEST_HARNESS", "codex")
			o := options(t, "codex")
			o.OutputMode = mode
			err := Run(context.Background(), o, strings.NewReader(""), brokenWriter{})
			if err == nil || ErrorValue(err).Code != "output_failed" {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestAlreadyCancelled(t *testing.T) {
	o := options(t, "codex")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, o, strings.NewReader(""), io.Discard)
	if err == nil || ErrorValue(err).Code != "cancelled" {
		t.Fatalf("err=%v", err)
	}
	ctx, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if ErrorValue(contextFailure(ctx)).Code != "timed_out" {
		t.Fatal("deadline classification")
	}
}

type limitedWriter struct{ calls int }

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > 2 {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
func TestStreamWriteFailureDuringEvents(t *testing.T) {
	t.Setenv("AGENT_ASK_TEST_HARNESS", "codex")
	o := options(t, "codex")
	o.OutputMode = "stream"
	err := Run(context.Background(), o, strings.NewReader(""), &limitedWriter{})
	if err == nil || ErrorValue(err).Code != "output_failed" {
		t.Fatalf("err=%v", err)
	}
}
func TestExecutableStartFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec-format fixture is Unix-specific")
	}
	o := options(t, "codex")
	o.Executable = filepath.Join(t.TempDir(), "invalid-executable")
	if err := os.WriteFile(o.Executable, []byte("invalid executable"), 0700); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), o, strings.NewReader(""), io.Discard)
	if err == nil || ErrorValue(err).Code != "harness_unavailable" {
		t.Fatalf("err=%v", err)
	}
}
