package jsonx

import (
	"strings"
	"testing"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestDecodeStrictAcceptsExactDocument(t *testing.T) {
	var target sample
	if err := DecodeStrictBytes([]byte(`{"name":"gallery","count":3}`), &target); err != nil {
		t.Fatalf("DecodeStrictBytes returned %v", err)
	}
	if target.Name != "gallery" || target.Count != 3 {
		t.Fatalf("decoded %+v", target)
	}
}

func TestDecodeStrictRejectsUnknownField(t *testing.T) {
	var target sample
	err := DecodeStrictBytes([]byte(`{"name":"a","depth":4}`), &target)
	if err == nil {
		t.Fatal("an unknown field was accepted")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestDecodeStrictRejectsTrailingValue(t *testing.T) {
	var target sample
	err := DecodeStrictBytes([]byte(`{"name":"a"} {"name":"b"}`), &target)
	if err == nil {
		t.Fatal("trailing content was accepted")
	}
	if !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestDecodeStrictRejectsTrailingGarbage(t *testing.T) {
	var target sample
	if err := DecodeStrictBytes([]byte(`{"name":"a"} oops`), &target); err == nil {
		t.Fatal("trailing garbage was accepted")
	}
}

func TestDecodeStrictRejectsEmptyDocument(t *testing.T) {
	var target sample
	err := DecodeStrict(strings.NewReader("   "), &target)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestDecodeStrictReportsTypeErrors(t *testing.T) {
	var target sample
	err := DecodeStrictBytes([]byte(`{"count":"three"}`), &target)
	if err == nil || !strings.Contains(err.Error(), "count") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestDecodeStrictReportsSyntaxErrors(t *testing.T) {
	var target sample
	err := DecodeStrictBytes([]byte(`{"count":`), &target)
	if err == nil {
		t.Fatal("a truncated document was accepted")
	}
}

func TestReadLinesSkipsBlankLines(t *testing.T) {
	input := "{\"name\":\"a\"}\n\n   \n{\"name\":\"b\"}\n"
	lines, err := ReadLines(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ReadLines returned %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("ReadLines produced %d records", len(lines))
	}
	if lines[0].Number != 1 || lines[1].Number != 4 {
		t.Fatalf("line numbers are %d and %d", lines[0].Number, lines[1].Number)
	}
	var decoded sample
	if err := DecodeLine(lines[1], &decoded); err != nil {
		t.Fatalf("DecodeLine returned %v", err)
	}
	if decoded.Name != "b" {
		t.Fatalf("decoded %+v", decoded)
	}
}

func TestReadLinesRejectsInvalidRecord(t *testing.T) {
	_, err := ReadLines(strings.NewReader("{\"name\":\"a\"}\nnot json\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestDecodeLineReportsLineNumber(t *testing.T) {
	var target sample
	err := DecodeLine(Line{Number: 7, Raw: []byte(`{"depth":1}`)}, &target)
	if err == nil || !strings.Contains(err.Error(), "line 7") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestMarshalVariants(t *testing.T) {
	value := sample{Name: "a<b", Count: 1}
	compact, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal returned %v", err)
	}
	if string(compact) != `{"name":"a<b","count":1}` {
		t.Fatalf("Marshal produced %s", compact)
	}
	indented, err := MarshalIndent(value)
	if err != nil {
		t.Fatalf("MarshalIndent returned %v", err)
	}
	if !strings.HasSuffix(string(indented), "}\n") || !strings.Contains(string(indented), "\n  \"name\"") {
		t.Fatalf("MarshalIndent produced %s", indented)
	}
	line, err := MarshalLine(value)
	if err != nil {
		t.Fatalf("MarshalLine returned %v", err)
	}
	if string(line) != `{"name":"a<b","count":1}`+"\n" {
		t.Fatalf("MarshalLine produced %s", line)
	}
}
