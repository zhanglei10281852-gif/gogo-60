package model

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"CaveLoop/internal/jsonx"
)

// InputFormat identifies how a survey document is laid out on disk.
type InputFormat string

// Supported survey input formats.
const (
	FormatJSON  InputFormat = "json"
	FormatJSONL InputFormat = "jsonl"
	FormatAuto  InputFormat = "auto"
)

// ParseInputFormat resolves a user supplied format token.
func ParseInputFormat(raw string) (InputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return FormatAuto, nil
	case "json":
		return FormatJSON, nil
	case "jsonl", "ndjson", "lines":
		return FormatJSONL, nil
	default:
		return "", fmt.Errorf("unsupported input format %q (want json, jsonl or auto)", raw)
	}
}

// DetectFormat picks a format from the file extension. Unknown extensions
// default to plain JSON.
func DetectFormat(path string) InputFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl", ".ndjson":
		return FormatJSONL
	default:
		return FormatJSON
	}
}

// LoadSurveyFile reads and strictly decodes a survey document from disk.
func LoadSurveyFile(path string, format InputFormat) (Survey, error) {
	if strings.TrimSpace(path) == "" {
		return Survey{}, fmt.Errorf("survey input path is empty")
	}
	if format == FormatAuto || format == "" {
		format = DetectFormat(path)
	}
	handle, err := os.Open(path)
	if err != nil {
		return Survey{}, fmt.Errorf("opening survey input %s: %w", path, err)
	}
	defer func() { _ = handle.Close() }()
	survey, err := DecodeSurvey(handle, format)
	if err != nil {
		return Survey{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return survey, nil
}

// DecodeSurvey decodes a survey document in the requested format.
func DecodeSurvey(r io.Reader, format InputFormat) (Survey, error) {
	switch format {
	case FormatJSONL:
		return decodeSurveyLines(r)
	case FormatJSON, FormatAuto, "":
		return decodeSurveyDocument(r)
	default:
		return Survey{}, fmt.Errorf("unsupported input format %q", string(format))
	}
}

// decodeSurveyDocument decodes a single strict JSON survey object.
func decodeSurveyDocument(r io.Reader) (Survey, error) {
	var survey Survey
	if err := jsonx.DecodeStrict(r, &survey); err != nil {
		return Survey{}, err
	}
	return survey.Canonical(), nil
}

// decodeSurveyLines decodes a JSON Lines record stream into a survey.
func decodeSurveyLines(r io.Reader) (Survey, error) {
	lines, err := jsonx.ReadLines(r)
	if err != nil {
		return Survey{}, err
	}
	if len(lines) == 0 {
		return Survey{}, fmt.Errorf("survey stream contains no records")
	}
	records := make([]Record, 0, len(lines))
	for _, line := range lines {
		var record Record
		if err := jsonx.DecodeLine(line, &record); err != nil {
			return Survey{}, err
		}
		if err := validateRecordShape(record, line.Number); err != nil {
			return Survey{}, err
		}
		records = append(records, record)
	}
	return SurveyFromRecords(records)
}

// validateRecordShape rejects records that carry the wrong payload for a kind.
func validateRecordShape(record Record, lineNumber int) error {
	switch record.Kind {
	case KindInstrument:
		if record.Instrument == nil {
			return fmt.Errorf("line %d: instrument record has no instrument payload", lineNumber)
		}
		if record.Trip != nil {
			return fmt.Errorf("line %d: instrument record must not carry a trip payload", lineNumber)
		}
	case KindTrip:
		if record.Trip == nil {
			return fmt.Errorf("line %d: trip record has no trip payload", lineNumber)
		}
		if record.Instrument != nil {
			return fmt.Errorf("line %d: trip record must not carry an instrument payload", lineNumber)
		}
	case "":
		return fmt.Errorf("line %d: record kind is missing", lineNumber)
	default:
		return fmt.Errorf("line %d: unsupported record kind %q", lineNumber, record.Kind)
	}
	return nil
}

// DecodeRecords decodes a JSON Lines stream into raw records without folding
// them into a survey. The ledger reader uses this to preserve record order.
func DecodeRecords(r io.Reader) ([]Record, error) {
	lines, err := jsonx.ReadLines(r)
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(lines))
	for _, line := range lines {
		var record Record
		if err := jsonx.DecodeLine(line, &record); err != nil {
			return nil, err
		}
		if err := validateRecordShape(record, line.Number); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}
