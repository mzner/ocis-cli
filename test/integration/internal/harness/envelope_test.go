package harness

import (
	"strings"
	"testing"
)

func TestDecodeEnvelopeAndData(t *testing.T) {
	envelope, err := DecodeEnvelope([]byte(
		`{"schemaVersion":"1","type":"item","data":{"name":"report"}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	value, err := DecodeData[map[string]string](envelope)
	if err != nil {
		t.Fatal(err)
	}
	if value["name"] != "report" {
		t.Fatalf("name = %q, want report", value["name"])
	}
}

func TestDecodeJSONLRejectsInvalidEnvelope(t *testing.T) {
	_, err := DecodeJSONL([]byte(
		"{\"schemaVersion\":\"1\",\"type\":\"item\",\"data\":1}\n{}\n",
	))
	if err == nil || !strings.Contains(err.Error(), "invalid CLI JSON envelope") {
		t.Fatalf("error = %v, want invalid-envelope error", err)
	}
}
