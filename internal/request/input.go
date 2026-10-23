package request

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const maxInputBytes = 16 << 20

type prepared struct {
	prompt       string
	schema       *jsonschema.Schema
	outputSchema []byte
}

func readSource(s Source, stdin io.Reader) ([]byte, error) {
	var r io.Reader = strings.NewReader(s.Value)
	if s.Stdin {
		r = stdin
	}
	if s.File != "" {
		f, err := os.Open(s.File)
		if err != nil {
			return nil, failure("invalid_input", "Cannot open the requested input file")
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, maxInputBytes+1))
	if err != nil || len(b) > maxInputBytes || !utf8.Valid(b) {
		return nil, failure("invalid_input", "Input must be readable UTF-8 and at most 16 MiB")
	}
	return b, nil
}

func decodeJSON(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, failure("invalid_input", "Expected exactly one JSON value")
	}
	return v, nil
}

func compileSchema(s Source, data []byte) (*jsonschema.Schema, error) {
	doc, err := decodeJSON(data)
	if err != nil {
		return nil, failure("invalid_schema", "Schema must contain exactly one valid JSON value")
	}
	name := s.File
	if name == "" {
		name = "agent-ask-inline.schema.json"
	}
	path, err := filepath.Abs(name)
	if err != nil {
		return nil, failure("invalid_schema", "Cannot resolve schema location")
	}
	urlPath := filepath.ToSlash(path)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	loc := (&url.URL{Scheme: "file", Path: urlPath}).String()
	c := jsonschema.NewCompiler()
	c.UseLoader(jsonschema.FileLoader{})
	if err := c.AddResource(loc, doc); err != nil {
		return nil, failure("invalid_schema", "Cannot load JSON Schema")
	}
	schema, err := c.Compile(loc)
	if err != nil {
		return nil, failure("invalid_schema", "Invalid JSON Schema or unavailable local reference; network references are disabled")
	}
	return schema, nil
}

func prepare(o Options, stdin io.Reader) (prepared, error) {
	var p prepared
	if o.Harness != "claude" && o.Harness != "codex" {
		return p, failure("invalid_request", "--harness must be claude or codex")
	}
	if !o.Prompt.Set {
		return p, failure("invalid_request", "Supply one prompt source")
	}
	if o.Timeout <= 0 {
		return p, failure("invalid_request", "--timeout must be positive")
	}
	if o.ConfigMode != "inherit" && o.ConfigMode != "clean" {
		return p, failure("invalid_request", "--harness-config-mode must be inherit or clean")
	}
	if o.InputFormat != "text" && o.InputFormat != "json" {
		return p, failure("invalid_request", "--input-format must be text or json")
	}
	if o.OutputMode != "json" && o.OutputMode != "plain" && o.OutputMode != "stream" {
		return p, failure("invalid_request", "Invalid output mode")
	}
	if o.Thinking != "auto" && o.Thinking != "on" && o.Thinking != "off" {
		return p, failure("invalid_request", "--thinking must be auto, on, or off")
	}
	count := 0
	for _, s := range []Source{o.Prompt, o.Input, o.InputSchema, o.OutputSchema} {
		if s.Stdin {
			count++
		}
	}
	if count > 1 {
		return p, failure("invalid_request", "Only one content source may read stdin")
	}
	if o.InputSchema.Set && !o.Input.Set {
		return p, failure("invalid_request", "An input schema requires input")
	}
	b, err := readSource(o.Prompt, stdin)
	if err != nil {
		return p, err
	}
	if strings.TrimSpace(string(b)) == "" {
		return p, failure("invalid_request", "Prompt must not be empty")
	}
	p.prompt = string(b)
	if o.Input.Set {
		b, err := readSource(o.Input, stdin)
		if err != nil {
			return p, err
		}
		var input any = string(b)
		if o.InputFormat == "json" {
			input, err = decodeJSON(b)
			if err != nil {
				return p, failure("input_invalid", "Input must contain exactly one JSON value")
			}
		}
		if o.InputSchema.Set {
			raw, err := readSource(o.InputSchema, stdin)
			if err != nil {
				return p, err
			}
			schema, err := compileSchema(o.InputSchema, raw)
			if err != nil {
				return p, err
			}
			if err := schema.Validate(input); err != nil {
				return p, failure("input_invalid", "Input does not match its JSON Schema")
			}
			p.prompt += "\n\nInput JSON Schema:\n" + string(raw)
		}
		// JSON quoting keeps arbitrary input separate from the caller's instructions.
		wrapped, err := json.Marshal(input)
		if err != nil {
			return p, failure("input_invalid", "Cannot encode input")
		}
		p.prompt += "\n\nInput data (JSON encoded " + o.InputFormat + " value):\n" + string(wrapped)
	}
	if o.OutputSchema.Set {
		b, err := readSource(o.OutputSchema, stdin)
		if err != nil {
			return p, err
		}
		p.schema, err = compileSchema(o.OutputSchema, b)
		if err != nil {
			return p, err
		}
		p.outputSchema = b
	}
	return p, nil
}
