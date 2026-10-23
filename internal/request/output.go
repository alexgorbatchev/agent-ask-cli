package request

import (
	"encoding/json"
	"io"
)

func encode(w io.Writer, v any) error {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return failure("output_failed", "Cannot write output")
	}
	return nil
}

type emitter struct {
	w         io.Writer
	sequence  int
	streaming bool
}

func (e *emitter) send(kind string, data any) error {
	if !e.streaming {
		return nil
	}
	e.sequence++
	return encode(e.w, struct {
		Sequence int    `json:"sequence"`
		Type     string `json:"type"`
		Data     any    `json:"data"`
	}{e.sequence, kind, data})
}
func (e *emitter) result(mode string, r Result) error {
	if e.streaming {
		return e.send("result", r)
	}
	if mode == "plain" && r.OutputKind == "text" {
		if _, err := io.WriteString(e.w, r.Output.(string)); err != nil {
			return failure("output_failed", "Cannot write output")
		}
		return nil
	}
	if mode == "plain" {
		return encode(e.w, r.Output)
	}
	return encode(e.w, r)
}
