package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
)

// Envelope is the stable machine-output boundary consumed by black-box tests.
type Envelope struct {
	SchemaVersion string          `json:"schemaVersion"`
	Type          string          `json:"type"`
	Data          json.RawMessage `json:"data"`
}

// DecodeEnvelope decodes and validates one JSON result.
func DecodeEnvelope(data []byte) (Envelope, error) {
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode CLI JSON envelope: %w", err)
	}
	if envelope.SchemaVersion != "1" || envelope.Type == "" ||
		len(envelope.Data) == 0 {
		return Envelope{}, fmt.Errorf("invalid CLI JSON envelope: %#v", envelope)
	}
	return envelope, nil
}

// DecodeJSONL decodes and validates every non-empty JSONL record.
func DecodeJSONL(data []byte) ([]Envelope, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var envelopes []Envelope
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		envelope, err := DecodeEnvelope(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan CLI JSONL output: %w", err)
	}
	return envelopes, nil
}

// DecodeData decodes the data member of an envelope.
func DecodeData[T any](envelope Envelope) (T, error) {
	var value T
	if err := json.Unmarshal(envelope.Data, &value); err != nil {
		return value, fmt.Errorf("decode %s envelope data: %w", envelope.Type, err)
	}
	return value, nil
}
