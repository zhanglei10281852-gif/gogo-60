package model

import (
	"strings"
	"testing"

	"CaveLoop/internal/units"
)

func defaults() Defaults {
	return Defaults{LengthUnit: units.Meters, AngleUnit: units.Degrees}
}

func minimalSurvey() Survey {
	return Survey{
		Cave: "Test Cave",
		Instruments: []Instrument{
			{ID: "set-a", LengthUnit: "m", AngleUnit: "deg"},
		},
		Trips: []Trip{
			{
				ID:         "T1",
				Date:       "2031-01-02",
				LengthUnit: "m",
				AngleUnit:  "deg",
				Instrument: "set-a",
				Stations: []Station{
					{Name: "A", Flags: []string{"entrance", "fixed"}, Fixed: &FixedCoordinate{Unit: "m"}},
					{Name: "B"},
				},
				Shots: []Shot{
					{ID: "S1", From: "A", To: "B", Distance: 10, Azimuth: 45, Inclination: -5},
				},
			},
		},
	}
}

func TestValidateAcceptsMinimalSurvey(t *testing.T) {
	issues := Validate(minimalSurvey(), defaults())
	if issues.HasErrors() {
		t.Fatalf("valid survey rejected: %v", issues.Error())
	}
}

func TestValidateReportsMissingCaveAndEmptyTrips(t *testing.T) {
	issues := Validate(Survey{}, defaults())
	codes := codeSet(issues)
	for _, want := range []string{"cave-missing", "survey-empty"} {
		if !codes[want] {
			t.Fatalf("expected code %q in %v", want, issues.Error())
		}
	}
}

func TestValidateDetectsStructuralProblems(t *testing.T) {
	survey := minimalSurvey()
	survey.Trips[0].Shots = append(survey.Trips[0].Shots,
		Shot{ID: "S1", From: "B", To: "B", Distance: -1, Azimuth: 10, Inclination: 120},
		Shot{ID: "", From: "", To: "C", Distance: 5, Instrument: "missing"},
	)
	issues := Validate(survey, defaults())
	codes := codeSet(issues)
	for _, want := range []string{
		"shot-id-duplicate", "shot-self-loop", "shot-distance-negative",
		"shot-inclination-range", "shot-id-empty", "shot-from-empty", "shot-instrument-unknown",
	} {
		if !codes[want] {
			t.Fatalf("expected code %q in %v", want, issues.Error())
		}
	}
	if !issues.HasErrors() {
		t.Fatal("structural problems should be fatal")
	}
}

func TestValidateDetectsInstrumentProblems(t *testing.T) {
	scale := 0.0
	survey := minimalSurvey()
	survey.Instruments = append(survey.Instruments,
		Instrument{ID: "set-a"},
		Instrument{ID: "set-b", LengthUnit: "cubit"},
		Instrument{ID: "set-c", TapeScale: &scale},
		Instrument{ID: ""},
	)
	issues := Validate(survey, defaults())
	codes := codeSet(issues)
	for _, want := range []string{"instrument-id-duplicate", "instrument-length-unit", "instrument-tape-scale", "instrument-id-empty"} {
		if !codes[want] {
			t.Fatalf("expected code %q in %v", want, issues.Error())
		}
	}
}

func TestValidateDetectsStationProblems(t *testing.T) {
	survey := minimalSurvey()
	survey.Trips[0].Stations = append(survey.Trips[0].Stations,
		Station{Name: "B"},
		Station{Name: "C", Flags: []string{"fixed"}},
		Station{Name: "d", Flags: []string{"sump"}},
		Station{Name: "D"},
	)
	issues := Validate(survey, defaults())
	codes := codeSet(issues)
	for _, want := range []string{"station-duplicate", "station-fixed-missing-coordinates", "station-flag-unknown", "station-case-collision"} {
		if !codes[want] {
			t.Fatalf("expected code %q in %v", want, issues.Error())
		}
	}
}

func TestValidateWarnsWithoutControl(t *testing.T) {
	survey := minimalSurvey()
	survey.Trips[0].Stations[0] = Station{Name: "A"}
	issues := Validate(survey, defaults())
	if issues.HasErrors() {
		t.Fatalf("survey without control should still be valid: %v", issues.Error())
	}
	if !codeSet(issues)["control-missing"] {
		t.Fatalf("expected a control-missing warning, got %v", issues)
	}
}

func TestValidateRejectsBadDate(t *testing.T) {
	survey := minimalSurvey()
	survey.Trips[0].Date = "12/04/2031"
	if !codeSet(Validate(survey, defaults()))["trip-date-format"] {
		t.Fatal("an invalid date was accepted")
	}
}

func TestIssuesSortedPutsErrorsFirst(t *testing.T) {
	issues := Issues{
		{Severity: SeverityWarning, Code: "b", Path: "p1"},
		{Severity: SeverityError, Code: "a", Path: "p2"},
	}
	sorted := issues.Sorted()
	if sorted[0].Severity != SeverityError {
		t.Fatalf("sorted issues start with %v", sorted[0])
	}
	if len(issues.Filter(SeverityWarning)) != 1 {
		t.Fatal("Filter did not isolate warnings")
	}
	if (Issues{}).Error() != "no validation errors" {
		t.Fatal("empty issue list should report no errors")
	}
	if !strings.Contains(issues.Error(), "ERROR a") {
		t.Fatalf("Error() rendered %q", issues.Error())
	}
}

func TestCanonicalOrdersEverything(t *testing.T) {
	survey := Survey{
		Cave: "C",
		Trips: []Trip{
			{ID: "T2", Shots: []Shot{{ID: "S2"}, {ID: "S1"}}},
			{ID: "T1", Surveyors: []string{"z", "a"}, Stations: []Station{{Name: "B"}, {Name: "A", Flags: []string{"SURFACE", "fixed"}}}},
		},
	}
	canonical := survey.Canonical()
	if canonical.Trips[0].ID != "T1" || canonical.Trips[1].ID != "T2" {
		t.Fatalf("trips are ordered %s, %s", canonical.Trips[0].ID, canonical.Trips[1].ID)
	}
	if canonical.Trips[0].Stations[0].Name != "A" {
		t.Fatal("stations were not sorted")
	}
	flags := canonical.Trips[0].Stations[0].Flags
	if len(flags) != 2 || flags[0] != FlagFixed || flags[1] != FlagSurface {
		t.Fatalf("flags canonicalised to %v", flags)
	}
	if canonical.Trips[0].Surveyors[0] != "a" {
		t.Fatal("surveyors were not sorted")
	}
	if canonical.Trips[1].Shots[0].ID != "S1" {
		t.Fatal("shots were not sorted")
	}
	if survey.Trips[0].ID != "T2" {
		t.Fatal("Canonical mutated its input")
	}
}

func TestRecordsRoundTrip(t *testing.T) {
	survey := minimalSurvey()
	records := survey.Records()
	if len(records) != 2 {
		t.Fatalf("expected one instrument and one trip record, got %d", len(records))
	}
	if records[0].Kind != KindInstrument || records[1].Kind != KindTrip {
		t.Fatalf("record kinds are %q and %q", records[0].Kind, records[1].Kind)
	}
	rebuilt, err := SurveyFromRecords(records)
	if err != nil {
		t.Fatalf("SurveyFromRecords returned %v", err)
	}
	if rebuilt.Cave != survey.Cave || len(rebuilt.Trips) != 1 || len(rebuilt.Instruments) != 1 {
		t.Fatalf("rebuilt survey is %+v", rebuilt)
	}
}

func TestSurveyFromRecordsUpsertsByIdentifier(t *testing.T) {
	first := Trip{ID: "T1", Name: "first"}
	second := Trip{ID: "T1", Name: "second"}
	rebuilt, err := SurveyFromRecords([]Record{
		{Kind: KindTrip, Cave: "C", Trip: &first},
		{Kind: KindTrip, Trip: &second},
	})
	if err != nil {
		t.Fatalf("SurveyFromRecords returned %v", err)
	}
	if len(rebuilt.Trips) != 1 || rebuilt.Trips[0].Name != "second" {
		t.Fatalf("upsert produced %+v", rebuilt.Trips)
	}
}

func TestSurveyFromRecordsRejectsBadRecords(t *testing.T) {
	if _, err := SurveyFromRecords([]Record{{Kind: KindTrip}}); err == nil {
		t.Fatal("a trip record without payload was accepted")
	}
	if _, err := SurveyFromRecords([]Record{{Kind: "station"}}); err == nil {
		t.Fatal("an unsupported kind was accepted")
	}
}

func TestMergeKeepsReceiverCave(t *testing.T) {
	left := minimalSurvey()
	right := minimalSurvey()
	right.Cave = "Other"
	right.Trips[0].ID = "T2"
	merged, err := left.Merge(right)
	if err != nil {
		t.Fatalf("Merge returned %v", err)
	}
	if merged.Cave != "Test Cave" {
		t.Fatalf("merged cave is %q", merged.Cave)
	}
	if len(merged.Trips) != 2 {
		t.Fatalf("merged survey holds %d trips", len(merged.Trips))
	}
}

func TestStationHelpers(t *testing.T) {
	station := Station{Name: "A", Flags: []string{" Entrance ", "fixed", "fixed"}}
	if !station.HasFlag(FlagEntrance) || station.HasFlag(FlagSurface) {
		t.Fatal("HasFlag misreported the station flags")
	}
	flags := station.CanonicalFlags()
	if len(flags) != 2 || flags[0] != FlagFixed || flags[1] != FlagEntrance {
		t.Fatalf("CanonicalFlags produced %v", flags)
	}
	survey := minimalSurvey()
	if got := survey.StationNames(); len(got) != 2 || got[0] != "A" {
		t.Fatalf("StationNames produced %v", got)
	}
	if survey.ShotCount() != 1 {
		t.Fatal("ShotCount is wrong")
	}
	if _, ok := survey.Trips[0].StationByName("B"); !ok {
		t.Fatal("StationByName did not find a declared station")
	}
	if ids := survey.SortedInstrumentIDs(); len(ids) != 1 || ids[0] != "set-a" {
		t.Fatalf("SortedInstrumentIDs produced %v", ids)
	}
	if len(KnownFlags()) != 3 {
		t.Fatal("KnownFlags changed unexpectedly")
	}
}

func codeSet(issues Issues) map[string]bool {
	out := make(map[string]bool, len(issues))
	for _, issue := range issues {
		out[issue.Code] = true
	}
	return out
}
