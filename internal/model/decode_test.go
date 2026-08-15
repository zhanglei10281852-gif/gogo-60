package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const surveyDocument = `{
  "cave": "Decode Cave",
  "trips": [
    {
      "id": "T1",
      "lengthUnit": "m",
      "angleUnit": "deg",
      "stations": [{"name": "A"}, {"name": "B"}],
      "shots": [{"id": "S1", "from": "A", "to": "B", "distance": 5, "azimuth": 10, "inclination": 0}]
    }
  ]
}`

func TestParseInputFormat(t *testing.T) {
	cases := map[string]InputFormat{
		"": FormatAuto, "auto": FormatAuto, "json": FormatJSON,
		"jsonl": FormatJSONL, "ndjson": FormatJSONL, "LINES": FormatJSONL,
	}
	for input, want := range cases {
		got, err := ParseInputFormat(input)
		if err != nil || got != want {
			t.Fatalf("ParseInputFormat(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := ParseInputFormat("csv"); err == nil {
		t.Fatal("an unsupported format was accepted")
	}
}

func TestDetectFormat(t *testing.T) {
	if DetectFormat("a.jsonl") != FormatJSONL || DetectFormat("a.ndjson") != FormatJSONL {
		t.Fatal("JSONL extensions were not detected")
	}
	if DetectFormat("a.json") != FormatJSON || DetectFormat("a.txt") != FormatJSON {
		t.Fatal("the default format should be JSON")
	}
}

func TestDecodeSurveyJSON(t *testing.T) {
	survey, err := DecodeSurvey(strings.NewReader(surveyDocument), FormatJSON)
	if err != nil {
		t.Fatalf("DecodeSurvey returned %v", err)
	}
	if survey.Cave != "Decode Cave" || len(survey.Trips) != 1 {
		t.Fatalf("decoded %+v", survey)
	}
}

func TestDecodeSurveyRejectsUnknownField(t *testing.T) {
	_, err := DecodeSurvey(strings.NewReader(`{"cave":"a","depth":3}`), FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestDecodeSurveyLines(t *testing.T) {
	stream := `{"kind":"instrument","cave":"Line Cave","instrument":{"id":"set-a"}}` + "\n" +
		`{"kind":"trip","trip":{"id":"T1","shots":[{"id":"S1","from":"A","to":"B","distance":1,"azimuth":0,"inclination":0}]}}` + "\n"
	survey, err := DecodeSurvey(strings.NewReader(stream), FormatJSONL)
	if err != nil {
		t.Fatalf("DecodeSurvey returned %v", err)
	}
	if survey.Cave != "Line Cave" || len(survey.Instruments) != 1 || len(survey.Trips) != 1 {
		t.Fatalf("decoded %+v", survey)
	}
}

func TestDecodeSurveyLinesRejectsWrongPayload(t *testing.T) {
	cases := []string{
		`{"kind":"instrument","trip":{"id":"T1"}}`,
		`{"kind":"trip","instrument":{"id":"set-a"}}`,
		`{"kind":"trip"}`,
		`{"instrument":{"id":"set-a"}}`,
		`{"kind":"station","instrument":{"id":"set-a"}}`,
	}
	for _, stream := range cases {
		if _, err := DecodeSurvey(strings.NewReader(stream), FormatJSONL); err == nil {
			t.Fatalf("record %s was accepted", stream)
		}
	}
}

func TestDecodeSurveyLinesRejectsEmptyStream(t *testing.T) {
	if _, err := DecodeSurvey(strings.NewReader("\n\n"), FormatJSONL); err == nil {
		t.Fatal("an empty stream was accepted")
	}
}

func TestDecodeSurveyRejectsUnsupportedFormat(t *testing.T) {
	if _, err := DecodeSurvey(strings.NewReader("{}"), InputFormat("csv")); err == nil {
		t.Fatal("an unsupported format was accepted")
	}
}

func TestLoadSurveyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "survey.json")
	if err := os.WriteFile(path, []byte(surveyDocument), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	survey, err := LoadSurveyFile(path, FormatAuto)
	if err != nil {
		t.Fatalf("LoadSurveyFile returned %v", err)
	}
	if survey.Cave != "Decode Cave" {
		t.Fatalf("loaded %+v", survey)
	}
	if _, err := LoadSurveyFile("", FormatAuto); err == nil {
		t.Fatal("an empty path was accepted")
	}
	if _, err := LoadSurveyFile(filepath.Join(dir, "missing.json"), FormatAuto); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestDecodeRecordsPreservesOrder(t *testing.T) {
	stream := `{"kind":"trip","trip":{"id":"T2"}}` + "\n" + `{"kind":"trip","trip":{"id":"T1"}}` + "\n"
	records, err := DecodeRecords(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("DecodeRecords returned %v", err)
	}
	if len(records) != 2 || records[0].Trip.ID != "T2" || records[1].Trip.ID != "T1" {
		t.Fatalf("DecodeRecords produced %+v", records)
	}
}
