// Package request validates and executes a single fresh native harness request.
package request

import (
	"errors"
	"io"
	"time"

	parser "github.com/alexgorbatchev/agent-parser"
)

type Source struct {
	Value string
	File  string
	Stdin bool
	Set   bool
	// FileSet and StdinSet report that the caller wrote the flag, which separates
	// an explicitly empty value from an omitted one.
	FileSet  bool
	StdinSet bool
}

type Options struct {
	Harness                                  string
	Prompt, Input, InputSchema, OutputSchema Source
	InputFormat                              string
	ConfigMode, ConfigFile, HomeDir          string
	Model, Effort, Thinking                  string
	WorkingDir, Executable                   string
	Timeout                                  time.Duration
	OutputMode                               string
}

type Result struct {
	Harness    string             `json:"harness"`
	OutputKind string             `json:"output_kind"`
	Output     any                `json:"output"`
	Model      string             `json:"model,omitempty"`
	SessionID  string             `json:"session_id,omitempty"`
	Usage      *parser.TokenUsage `json:"usage,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string           { return e.Message }
func failure(code, message string) error { return &Error{Code: code, Message: message} }

// OutputFailure is the error for output that could not be written to stdout.
func OutputFailure() error { return failure("output_failed", "Cannot write output") }

func ErrorValue(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: "invalid_request", Message: "Invalid command or arguments; use --help for the accepted interface"}
}
func WriteError(w io.Writer, err error) error {
	return encode(w, map[string]any{"error": ErrorValue(err)})
}
