package reduce

import (
	"math"
	"reflect"
	"testing"

	"CaveLoop/internal/config"
	"CaveLoop/internal/model"
)

func floatPtr(value float64) *float64 { return &value }

func baseSurvey(shots []model.Shot, instruments []model.Instrument, trip model.Trip) model.Survey {
	trip.Shots = shots
	return model.Survey{Cave: "Reduce Cave", Instruments: instruments, Trips: []model.Trip{trip}}
}

func TestReducePlainForesight(t *testing.T) {
	survey := baseSurvey(
		[]model.Shot{{ID: "S1", From: "A", To: "B", Distance: 10, Azimuth: 90, Inclination: 0}},
		nil,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg", Stations: []model.Station{{Name: "A"}, {Name: "B"}}},
	)
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	if len(result.Shots) != 1 {
		t.Fatalf("Reduce produced %d shots", len(result.Shots))
	}
	shot := result.Shots[0]
	if math.Abs(shot.Vector.East-10) > 1e-9 || math.Abs(shot.Vector.North) > 1e-9 {
		t.Fatalf("displacement is %+v", shot.Vector)
	}
	if shot.Key() != "T1/S1" {
		t.Fatalf("shot key is %q", shot.Key())
	}
	if shot.Pair() != "A\x00B" {
		t.Fatalf("shot pair is %q", shot.Pair())
	}
	if shot.Reconciliation.HasBacksight {
		t.Fatal("a shot without backsight reported one")
	}
}

func TestReduceConvertsFeetAndGrads(t *testing.T) {
	survey := baseSurvey(
		[]model.Shot{{ID: "S1", From: "A", To: "B", Distance: 100, Azimuth: 100, Inclination: 0}},
		nil,
		model.Trip{ID: "T1", LengthUnit: "ft", AngleUnit: "grad", Stations: []model.Station{{Name: "A"}, {Name: "B"}}},
	)
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	shot := result.Shots[0]
	if math.Abs(shot.DistanceMeters-30.48) > 1e-9 {
		t.Fatalf("distance is %v m, want 30.48", shot.DistanceMeters)
	}
	if math.Abs(shot.AzimuthDeg-90) > 1e-9 {
		t.Fatalf("azimuth is %v deg, want 90", shot.AzimuthDeg)
	}
}

func TestReduceAppliesInstrumentCorrections(t *testing.T) {
	instruments := []model.Instrument{{
		ID:                    "set-a",
		LengthUnit:            "m",
		AngleUnit:             "deg",
		TapeCorrection:        -0.05,
		TapeScale:             floatPtr(1.01),
		AzimuthCorrection:     1.5,
		InclinationCorrection: -0.5,
		Declination:           floatPtr(2.0),
	}}
	survey := baseSurvey(
		[]model.Shot{{ID: "S1", From: "A", To: "B", Distance: 20, Azimuth: 10, Inclination: 5}},
		instruments,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg", Instrument: "set-a",
			Stations: []model.Station{{Name: "A"}, {Name: "B"}}},
	)
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	shot := result.Shots[0]
	if math.Abs(shot.DistanceMeters-(20*1.01-0.05)) > 1e-9 {
		t.Fatalf("corrected distance is %v", shot.DistanceMeters)
	}
	if math.Abs(shot.AzimuthDeg-13.5) > 1e-9 {
		t.Fatalf("corrected azimuth is %v, want 13.5", shot.AzimuthDeg)
	}
	if math.Abs(shot.InclinationDeg-4.5) > 1e-9 {
		t.Fatalf("corrected inclination is %v, want 4.5", shot.InclinationDeg)
	}
	if shot.InstrumentID != "set-a" {
		t.Fatalf("instrument is %q", shot.InstrumentID)
	}
	if math.Abs(shot.DeclinationDeg-2) > 1e-9 {
		t.Fatalf("declination is %v", shot.DeclinationDeg)
	}
}

func TestReduceAveragesBacksightWithinTolerance(t *testing.T) {
	survey := baseSurvey(
		[]model.Shot{{
			ID: "S1", From: "A", To: "B", Distance: 10, Azimuth: 100, Inclination: -10,
			BackAzimuth: floatPtr(281), BackInclination: floatPtr(11), BackDistance: floatPtr(10.04),
		}},
		nil,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg", Stations: []model.Station{{Name: "A"}, {Name: "B"}}},
	)
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	shot := result.Shots[0]
	reconciliation := shot.Reconciliation
	if !reconciliation.HasBacksight || !reconciliation.AzimuthAveraged ||
		!reconciliation.InclinationAveraged || !reconciliation.DistanceAveraged {
		t.Fatalf("reconciliation is %+v", reconciliation)
	}
	if !reconciliation.WithinTolerance {
		t.Fatal("readings within tolerance were flagged")
	}
	if math.Abs(shot.AzimuthDeg-100.5) > 1e-9 {
		t.Fatalf("averaged azimuth is %v, want 100.5", shot.AzimuthDeg)
	}
	if math.Abs(shot.InclinationDeg+10.5) > 1e-9 {
		t.Fatalf("averaged inclination is %v, want -10.5", shot.InclinationDeg)
	}
	if math.Abs(shot.DistanceMeters-10.02) > 1e-9 {
		t.Fatalf("averaged distance is %v, want 10.02", shot.DistanceMeters)
	}
	if !hasNote(shot.Notes, NoteBacksightAveraged) {
		t.Fatalf("notes are %v", shot.Notes)
	}
}

func TestReduceKeepsForesightWhenBacksightDisagrees(t *testing.T) {
	survey := baseSurvey(
		[]model.Shot{{
			ID: "S1", From: "A", To: "B", Distance: 10, Azimuth: 100, Inclination: -10,
			BackAzimuth: floatPtr(300), BackInclination: floatPtr(30), BackDistance: floatPtr(14),
		}},
		nil,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg", Stations: []model.Station{{Name: "A"}, {Name: "B"}}},
	)
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	shot := result.Shots[0]
	if shot.AzimuthDeg != 100 || shot.InclinationDeg != -10 || shot.DistanceMeters != 10 {
		t.Fatalf("foresight was not preserved: %+v", shot)
	}
	if shot.Reconciliation.WithinTolerance {
		t.Fatal("a large disagreement was accepted")
	}
	for _, note := range []string{
		NoteBacksightAzimuthOutOfTolerance,
		NoteBacksightInclinationOutOfTolerance,
		NoteBacksightDistanceOutOfTolerance,
	} {
		if !hasNote(shot.Notes, note) {
			t.Fatalf("note %q missing from %v", note, shot.Notes)
		}
	}
	if len(result.Issues) != 3 {
		t.Fatalf("expected three warnings, got %v", result.Issues)
	}
}

func TestReduceMergesStationsAcrossTrips(t *testing.T) {
	survey := model.Survey{
		Cave: "Reduce Cave",
		Trips: []model.Trip{
			{
				ID: "T1", LengthUnit: "m", AngleUnit: "deg",
				Stations: []model.Station{
					{Name: "A", Flags: []string{"entrance", "fixed"}, Fixed: &model.FixedCoordinate{East: 10, North: 20, Up: 30, Unit: "m"}},
					{Name: "B"},
				},
				Shots: []model.Shot{{ID: "S1", From: "A", To: "B", Distance: 5, Azimuth: 0, Inclination: 0}},
			},
			{
				ID: "T2", LengthUnit: "m", AngleUnit: "deg",
				Stations: []model.Station{{Name: "A", Flags: []string{"surface"}}, {Name: "C"}},
				Shots:    []model.Shot{{ID: "S2", From: "A", To: "C", Distance: 5, Azimuth: 180, Inclination: 0}},
			},
		},
	}
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	if len(result.Stations) != 3 {
		t.Fatalf("expected three stations, got %d", len(result.Stations))
	}
	anchor := result.Stations[0]
	if anchor.Name != "A" || anchor.Fixed == nil {
		t.Fatalf("first station is %+v", anchor)
	}
	if anchor.Fixed.East != 10 || anchor.Fixed.Up != 30 {
		t.Fatalf("control coordinate is %+v", anchor.Fixed)
	}
	if !anchor.HasFlag(model.FlagEntrance) || !anchor.HasFlag(model.FlagSurface) || !anchor.HasFlag(model.FlagFixed) {
		t.Fatalf("merged flags are %v", anchor.Flags)
	}
	if len(anchor.Trips) != 2 || anchor.Trips[0] != "T1" || anchor.Trips[1] != "T2" {
		t.Fatalf("trip references are %v", anchor.Trips)
	}
	if index := result.StationIndex(); index["C"] != 2 {
		t.Fatalf("station index is %v", index)
	}
}

func TestReduceReportsConflictingControl(t *testing.T) {
	survey := model.Survey{
		Cave: "Reduce Cave",
		Trips: []model.Trip{
			{
				ID: "T1", LengthUnit: "m", AngleUnit: "deg",
				Stations: []model.Station{{Name: "A", Flags: []string{"fixed"}, Fixed: &model.FixedCoordinate{East: 1}}},
			},
			{
				ID: "T2", LengthUnit: "m", AngleUnit: "deg",
				Stations: []model.Station{{Name: "A", Flags: []string{"fixed"}, Fixed: &model.FixedCoordinate{East: 5}}},
			},
		},
	}
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	if !result.Issues.HasErrors() {
		t.Fatalf("conflicting control coordinates were accepted: %v", result.Issues)
	}
}

func TestReduceExcludesFlaggedShots(t *testing.T) {
	survey := baseSurvey(
		[]model.Shot{
			{ID: "S1", From: "A", To: "B", Distance: 10, Azimuth: 0, Inclination: 0},
			{ID: "S2", From: "B", To: "C", Distance: 10, Azimuth: 0, Inclination: 0, Excluded: true},
		},
		nil,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg"},
	)
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	if result.ExcludedShot != 1 {
		t.Fatalf("excluded count is %d", result.ExcludedShot)
	}
	if len(result.ActiveShots()) != 1 {
		t.Fatalf("active shots are %+v", result.ActiveShots())
	}
	if !hasNote(result.Shots[1].Notes, NoteExcluded) {
		t.Fatalf("notes are %v", result.Shots[1].Notes)
	}
}

func TestReduceMarksVerticalAndZeroLengthShots(t *testing.T) {
	survey := baseSurvey(
		[]model.Shot{
			{ID: "S1", From: "A", To: "B", Distance: 12, Azimuth: 0, Inclination: -90},
			{ID: "S2", From: "B", To: "C", Distance: 0, Azimuth: 0, Inclination: 0},
			{ID: "S3", From: "C", To: "D", Distance: 5, Azimuth: 370, Inclination: 0},
		},
		nil,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg"},
	)
	result, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	if !hasNote(result.Shots[0].Notes, NoteVerticalShot) {
		t.Fatalf("vertical shot notes are %v", result.Shots[0].Notes)
	}
	if !hasNote(result.Shots[1].Notes, NoteZeroLength) {
		t.Fatalf("zero length notes are %v", result.Shots[1].Notes)
	}
	if !hasNote(result.Shots[2].Notes, NoteAzimuthWrapped) {
		t.Fatalf("wrapped azimuth notes are %v", result.Shots[2].Notes)
	}
	if result.Shots[2].AzimuthDeg != 10 {
		t.Fatalf("wrapped azimuth is %v, want 10", result.Shots[2].AzimuthDeg)
	}
}

func TestReduceRejectsUnknownInstrument(t *testing.T) {
	survey := baseSurvey(
		[]model.Shot{{ID: "S1", From: "A", To: "B", Distance: 1, Azimuth: 0, Inclination: 0, Instrument: "ghost"}},
		nil,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg"},
	)
	if _, err := Reduce(survey, config.Default()); err == nil {
		t.Fatal("an unknown instrument reference was accepted")
	}
}

func TestReduceRejectsImpossibleCorrectedInclination(t *testing.T) {
	instruments := []model.Instrument{{ID: "set-a", InclinationCorrection: 10}}
	survey := baseSurvey(
		[]model.Shot{{ID: "S1", From: "A", To: "B", Distance: 1, Azimuth: 0, Inclination: 85}},
		instruments,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg", Instrument: "set-a"},
	)
	if _, err := Reduce(survey, config.Default()); err == nil {
		t.Fatal("an impossible corrected inclination was accepted")
	}
}

func TestReduceIsDeterministic(t *testing.T) {
	survey := baseSurvey(
		[]model.Shot{
			{ID: "S2", From: "B", To: "C", Distance: 7, Azimuth: 120, Inclination: 3},
			{ID: "S1", From: "A", To: "B", Distance: 9, Azimuth: 30, Inclination: -4},
		},
		nil,
		model.Trip{ID: "T1", LengthUnit: "m", AngleUnit: "deg"},
	)
	first, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	second, err := Reduce(survey, config.Default())
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	if first.Shots[0].ShotID != "S1" || first.Shots[1].ShotID != "S2" {
		t.Fatalf("shots are not sorted: %+v", first.Shots)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("two reductions of the same survey differ")
	}
}

func hasNote(notes []string, want string) bool {
	for _, note := range notes {
		if note == want {
			return true
		}
	}
	return false
}
