package request

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareInputAndSources(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "tracks.json")
	schemaPath := filepath.Join(dir, "schema.json")
	for name, contents := range map[string]string{inputPath: `{"title":"raw"}`, schemaPath: titleSchema} {
		if err := os.WriteFile(name, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name                string
		input, inputSchema  Source
		format, stdin, want string
	}{
		{"literal", Source{Set: true, Value: "notes.txt"}, Source{}, "text", "", `"notes.txt"`},
		{"stdin", Source{Set: true, Stdin: true}, Source{}, "json", `{"title":"raw"}`, `{"title":"raw"}`},
		{"file", Source{Set: true, File: inputPath}, Source{Set: true, File: schemaPath}, "json", "", `{"title":"raw"}`},
		{"schema stdin", Source{Set: true, Value: "hi"}, Source{Set: true, Stdin: true}, "text", `{"type":"string"}`, `"hi"`},
		{"empty", Source{Set: true, Value: ""}, Source{}, "text", "", `""`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o := options(t, "codex")
			o.Input = tt.input
			o.InputSchema = tt.inputSchema
			o.InputFormat = tt.format
			p, err := prepare(o, strings.NewReader(tt.stdin))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(p.prompt, tt.want) {
				t.Fatalf("prompt %q", p.prompt)
			}
		})
	}
	o := options(t, "codex")
	o.Prompt = Source{Set: true, Stdin: true}
	p, err := prepare(o, strings.NewReader("hello"))
	if err != nil || p.prompt != "hello" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	o.Prompt = Source{Set: true, File: inputPath}
	if _, err := prepare(o, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidInputStopsBeforeLaunch(t *testing.T) {
	cases := []struct {
		name, code string
		change     func(*Options)
	}{
		{"harness", "invalid_request", func(o *Options) { o.Harness = "unknown" }},
		{"prompt missing", "invalid_request", func(o *Options) { o.Prompt.Set = false }},
		{"empty prompt", "invalid_request", func(o *Options) { o.Prompt.Value = " \n" }},
		{"timeout", "invalid_request", func(o *Options) { o.Timeout = 0 }},
		{"config mode", "invalid_request", func(o *Options) { o.ConfigMode = "weird" }},
		{"input format", "invalid_request", func(o *Options) { o.InputFormat = "yaml" }},
		{"output mode", "invalid_request", func(o *Options) { o.OutputMode = "yaml" }},
		{"thinking", "invalid_request", func(o *Options) { o.Thinking = "maybe" }},
		{"stdin conflict", "invalid_request", func(o *Options) { o.Prompt.Stdin = true; o.Input = Source{Set: true, Stdin: true} }},
		{"schema without input", "invalid_request", func(o *Options) { o.InputSchema = Source{Set: true, Value: titleSchema} }},
		{"missing file", "invalid_input", func(o *Options) { o.Prompt = Source{Set: true, File: "does-not-exist"} }},
		{"bad json", "input_invalid", func(o *Options) { o.InputFormat = "json"; o.Input = Source{Set: true, Value: "["} }},
		{"two values", "input_invalid", func(o *Options) { o.InputFormat = "json"; o.Input = Source{Set: true, Value: "1 2"} }},
		{"bad schema", "invalid_schema", func(o *Options) { o.OutputSchema = Source{Set: true, Value: "["} }},
		{"invalid schema", "invalid_schema", func(o *Options) { o.OutputSchema = Source{Set: true, Value: `{"type":"wrong"}`} }},
		{"remote ref", "invalid_schema", func(o *Options) {
			o.OutputSchema = Source{Set: true, Value: `{"$ref":"https://example.invalid/no.json"}`}
		}},
		{"bad input schema", "invalid_schema", func(o *Options) {
			o.Input = Source{Set: true, Value: "x"}
			o.InputSchema = Source{Set: true, Value: "invalid"}
		}},
		{"missing input schema file", "invalid_input", func(o *Options) { o.Input = Source{Set: true}; o.InputSchema = Source{Set: true, File: "missing"} }},
		{"missing output schema file", "invalid_input", func(o *Options) { o.OutputSchema = Source{Set: true, File: "missing"} }},
		{"mismatched input", "input_invalid", func(o *Options) {
			o.Input = Source{Set: true, Value: "x"}
			o.InputSchema = Source{Set: true, Value: titleSchema}
		}},
		{"invalid UTF8", "invalid_input", func(o *Options) { o.Prompt.Value = string([]byte{0xff}) }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o := options(t, "codex")
			o.Executable = "this-must-not-launch"
			tt.change(&o)
			var out bytes.Buffer
			err := Run(context.Background(), o, strings.NewReader("x"), &out)
			if err == nil || ErrorValue(err).Code != tt.code || out.Len() != 0 {
				t.Fatalf("err=%v out=%q", err, out.String())
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestReadSourceFailures(t *testing.T) {
	for _, r := range []io.Reader{failingReader{}, strings.NewReader(strings.Repeat("x", maxInputBytes+1))} {
		if _, err := readSource(Source{Stdin: true}, r); err == nil {
			t.Fatal("expected read failure")
		}
	}
}
func TestLocalSchemaReferences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(filepath.Join(dir, "title.json"), []byte(`{"type":"string"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := compileSchema(Source{File: path}, []byte(`{"$ref":"title.json"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate("hi"); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(1); err == nil {
		t.Fatal("schema reference was not applied")
	}
}
func TestErrors(t *testing.T) {
	var out bytes.Buffer
	if err := WriteError(&out, failure("harness_failed", "failed")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"error\":{\"code\":\"harness_failed\",\"message\":\"failed\"}}\n" {
		t.Fatalf("%q", out.String())
	}
	if ErrorValue(errors.New("secret")).Code != "invalid_request" {
		t.Fatal("unexpected fallback")
	}
	if err := failure("input_invalid", "invalid input"); err.Error() != "invalid input" {
		t.Fatalf("error text %q", err.Error())
	}
	if err := WriteError(brokenWriter{}, errors.New("bad")); err == nil {
		t.Fatal("missing write error")
	}
}
