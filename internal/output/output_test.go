package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestRendererWritesHumanOutput(t *testing.T) {
	var destination bytes.Buffer
	renderer := Renderer{Writer: &destination}
	if err := renderer.Write(map[string]string{"ignored": "in text mode"}, "hello %s\n", "world"); err != nil {
		t.Fatal(err)
	}
	if got := destination.String(); got != "hello world\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRendererWritesJSON(t *testing.T) {
	var destination bytes.Buffer
	renderer := Renderer{Writer: &destination, Mode: JSON, Type: "server"}
	if err := renderer.Write(map[string]string{"name": "report"}, "ignored"); err != nil {
		t.Fatal(err)
	}
	if got := destination.String(); !strings.Contains(got, `"schemaVersion": "1"`) ||
		!strings.Contains(got, `"type": "server"`) || !strings.Contains(got, `"name": "report"`) {
		t.Fatalf("unexpected JSON: %s", got)
	}
}

func TestRendererWritesOneJSONLObjectPerSliceItem(t *testing.T) {
	var destination bytes.Buffer
	renderer := Renderer{Writer: &destination, Mode: JSONL, Type: "item"}
	if err := renderer.Write([]string{"one", "two"}, "ignored"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(destination.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: got %d, want 2: %q", len(lines), destination.String())
	}
	for _, line := range lines {
		if !strings.Contains(line, `"schemaVersion":"1"`) || !strings.Contains(line, `"type":"item"`) {
			t.Fatalf("unexpected JSONL line: %s", line)
		}
	}
}
