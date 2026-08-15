// Package jsonx centralises the strict JSON handling rules used by CaveLoop.
//
// Every document read from disk is decoded with unknown fields rejected and
// with trailing content rejected, so a malformed or unexpected input fails
// loudly instead of being silently ignored. Encoding is always performed with
// HTML escaping disabled so that the emitted bytes are a faithful, stable
// representation of the values.
package jsonx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// maxLineBytes bounds a single JSONL record so a corrupt file cannot exhaust
// memory. Survey records are small; one megabyte is generous.
const maxLineBytes = 1 << 20

// DecodeStrict decodes exactly one JSON value from r into target.
func DecodeStrict(r io.Reader, target any) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decodeSingle(decoder, target); err != nil {
		return err
	}
	return nil
}

// DecodeStrictBytes decodes exactly one JSON value from data into target.
func DecodeStrictBytes(data []byte, target any) error {
	return DecodeStrict(bytes.NewReader(data), target)
}

// decodeSingle performs the decode and then asserts the stream is exhausted.
func decodeSingle(decoder *json.Decoder, target any) error {
	if err := decodeValue(decoder, target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("unexpected trailing JSON content after the first value")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON value in document")
		}
		return fmt.Errorf("trailing content is not valid JSON: %w", err)
	}
	return nil
}

// decodeValue decodes a single value and normalises the error message.
func decodeValue(decoder *json.Decoder, target any) error {
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("document is empty")
		}
		return normalizeDecodeError(err)
	}
	return nil
}

// normalizeDecodeError rewrites decoder errors into stable, readable messages.
func normalizeDecodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := typeErr.Field
		if field == "" {
			field = "(root)"
		}
		return fmt.Errorf("field %s: cannot decode JSON %s into %s", field, typeErr.Value, typeErr.Type.String())
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf("invalid JSON at byte offset %d: %s", syntaxErr.Offset, syntaxErr.Error())
	}
	message := err.Error()
	if strings.HasPrefix(message, "json: unknown field ") {
		return fmt.Errorf("unknown field %s is not allowed", strings.TrimPrefix(message, "json: unknown field "))
	}
	return err
}

// Line is one decoded record of a JSON Lines stream.
type Line struct {
	Number int
	Raw    []byte
}

// ReadLines splits a JSON Lines stream into non empty records. Blank lines are
// skipped, every other line must be a complete JSON value.
func ReadLines(r io.Reader) ([]Line, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	lines := make([]Line, 0, 16)
	number := 0
	for scanner.Scan() {
		number++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		if !json.Valid(raw) {
			return nil, fmt.Errorf("line %d: not a valid JSON value", number)
		}
		copied := make([]byte, len(raw))
		copy(copied, raw)
		lines = append(lines, Line{Number: number, Raw: copied})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading JSON lines: %w", err)
	}
	return lines, nil
}

// DecodeLine decodes a single JSONL record with the strict rules applied.
func DecodeLine(line Line, target any) error {
	if err := DecodeStrictBytes(line.Raw, target); err != nil {
		return fmt.Errorf("line %d: %w", line.Number, err)
	}
	return nil
}

// Marshal encodes value compactly with HTML escaping disabled.
func Marshal(value any) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encoding JSON: %w", err)
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// MarshalIndent encodes value with two space indentation and a trailing
// newline, which is the shape used for every file CaveLoop writes.
func MarshalIndent(value any) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encoding JSON: %w", err)
	}
	return buffer.Bytes(), nil
}

// MarshalLine encodes value as a single JSONL record including the newline.
func MarshalLine(value any) ([]byte, error) {
	compact, err := Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(compact, '\n'), nil
}
