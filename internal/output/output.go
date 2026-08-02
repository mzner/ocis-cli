// Package output renders application results independently of Cobra and
// protocol clients.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

// Mode selects a command's output contract.
type Mode string

const (
	Human Mode = "human"
	JSON  Mode = "json"
	JSONL Mode = "jsonl"
)

// SchemaVersion is the version of the machine-readable output envelope.
const SchemaVersion = "1"

// Envelope is the stable top-level shape used by JSON and JSONL output.
type Envelope struct {
	SchemaVersion string `json:"schemaVersion"`
	Type          string `json:"type"`
	Data          any    `json:"data"`
}

// ErrorData is the stable machine-readable representation of a failed command.
type ErrorData struct {
	Code      int    `json:"code"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Operation string `json:"operation,omitempty"`
}

// Renderer writes human-readable or machine-readable command results.
type Renderer struct {
	Writer io.Writer
	Mode   Mode
	Type   string
}

// Write renders value in the selected mode or applies the human-readable format.
func (renderer Renderer) Write(value any, format string, args ...any) error {
	switch renderer.Mode {
	case JSON:
		return renderer.writeJSON(value, true)
	case JSONL:
		return renderer.WriteJSONL(value)
	}
	_, err := fmt.Fprintf(renderer.Writer, format, args...)
	return err
}

// WriteJSON renders one stable, indented JSON envelope.
func (renderer Renderer) WriteJSON(value any) error {
	return renderer.writeJSON(value, true)
}

func (renderer Renderer) writeJSON(value any, indent bool) error {
	encoder := json.NewEncoder(renderer.Writer)
	if indent {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(renderer.envelope(value))
}

// WriteJSONL renders one compact envelope per collection item. Scalar values
// produce one line.
func (renderer Renderer) WriteJSONL(value any) error {
	reflected := reflect.ValueOf(value)
	if reflected.IsValid() && (reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array) {
		for index := range reflected.Len() {
			if err := renderer.writeJSON(reflected.Index(index).Interface(), false); err != nil {
				return err
			}
		}
		return nil
	}
	return renderer.writeJSON(value, false)
}

func (renderer Renderer) envelope(value any) Envelope {
	kind := renderer.Type
	if kind == "" {
		kind = "result"
	}
	return Envelope{SchemaVersion: SchemaVersion, Type: kind, Data: value}
}
