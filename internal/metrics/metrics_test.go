package metrics

import (
	"math"
	"testing"

	"CaveLoop/internal/config"
	"CaveLoop/internal/loops"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/reduce"
	"CaveLoop/internal/traverse"
)

func sampleSurvey() model.Survey {
	return model.Survey{
		Cave:   "Metrics Cave",
		Region: "Test Region",
		Trips: []model.Trip{
			{
				ID: "T1", Name: "entrance series", Date: "2031-03-01",
				Surveyors: []string{"A. Rivers"}, LengthUnit: "m", AngleUnit: "deg",
				Stations: []model.Station{
					{Name: "A", Flags: []string{"fixed", "entrance"}, Fixed: &model.FixedCoordinate{Unit: "m"}},
					{Name: "B"}, {Name: "C"}, {Name: "D"},
				},
				Shots: []model.Shot{
					{ID: "S1", From: "A", To: "B", Distance: 10, Azimuth: 90, Inclination: 0},
					{ID: "S2", From: "B", To: "C", Distance: 20, Azimuth: 0, Inclination: -30},
					{ID: "S3", From: "C", To: "D", Distance: 5, Azimuth: 180, Inclination: 10,
						BackAzimuth: floatPtr(0.4), BackInclination: floatPtr(-9.8)},
				},
			},
			{
				ID: "T2", Date: "2031-03-08", LengthUnit: "m", AngleUnit: "deg",
				Stations: []model.Station{{Name: "D"}, {Name: "E"}},
				Shots: []model.Shot{
					{ID: "S4", From: "D", To: "E", Distance: 8, Azimuth: 270, Inclination: 0},
					{ID: "S5", From: "E", To: "F", Distance: 100, Azimuth: 45, Inclination: 0, Excluded: true},
				},
			},
		},
	}
}

func floatPtr(value float64) *float64 { return &value }

func buildInputs(t *testing.T, survey model.Survey) Inputs {
	t.Helper()
	cfg := config.Default()
	reduced, err := reduce.Reduce(survey, cfg)
	if err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	graph := network.Build(reduced)
	analysis := network.Analyze(graph)
	layout, err := traverse.Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	loopResult, err := loops.Detect(graph, analysis, layout, cfg.Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	return Inputs{
		Survey:   survey.Canonical(),
		Reduced:  reduced,
		Graph:    graph,
		Analysis: analysis,
		Layout:   layout,
		Loops:    loopResult,
	}
}

func TestSummariseTotals(t *testing.T) {
	summary := Summarise(buildInputs(t, sampleSurvey()))
	if summary.Cave != "Metrics Cave" || summary.Region != "Test Region" {
		t.Fatalf("summary identifies %q / %q", summary.Cave, summary.Region)
	}
	if summary.ShotCount != 5 || summary.ActiveShotCount != 4 || summary.ExcludedShotCount != 1 {
		t.Fatalf("shot counts are %d/%d/%d", summary.ShotCount, summary.ActiveShotCount, summary.ExcludedShotCount)
	}
	if math.Abs(summary.TotalLengthMeters-43) > 1e-9 {
		t.Fatalf("total length is %v, want 43", summary.TotalLengthMeters)
	}
	if summary.HorizontalLengthMeters >= summary.TotalLengthMeters {
		t.Fatalf("horizontal length %v should be shorter than the tape total", summary.HorizontalLengthMeters)
	}
	if summary.LongestShot != "T1/S2" || math.Abs(summary.LongestShotMeters-20) > 1e-9 {
		t.Fatalf("longest leg is %q at %v", summary.LongestShot, summary.LongestShotMeters)
	}
	if math.Abs(summary.MeanShotMeters-10.75) > 1e-9 {
		t.Fatalf("mean leg is %v, want 10.75", summary.MeanShotMeters)
	}
	if math.Abs(summary.MedianShotMeters-9) > 1e-9 {
		t.Fatalf("median leg is %v, want 9", summary.MedianShotMeters)
	}
	if math.Abs(summary.BacksightCoverage-0.25) > 1e-9 {
		t.Fatalf("backsight coverage is %v, want 0.25", summary.BacksightCoverage)
	}
}

func TestSummariseGeometry(t *testing.T) {
	summary := Summarise(buildInputs(t, sampleSurvey()))
	if summary.Deepest.Station != "C" && summary.Deepest.Station != "D" {
		t.Fatalf("deepest station is %q", summary.Deepest.Station)
	}
	if summary.Highest.Station != "A" {
		t.Fatalf("highest station is %q", summary.Highest.Station)
	}
	if summary.VerticalRangeMeters <= 9 {
		t.Fatalf("vertical range is %v, want more than 9", summary.VerticalRangeMeters)
	}
	if math.Abs(summary.MaxDepthMeters-summary.VerticalRangeMeters) > 1e-9 {
		t.Fatalf("max depth %v should equal the vertical range %v when the anchor is the datum",
			summary.MaxDepthMeters, summary.VerticalRangeMeters)
	}
	if summary.BoundingBox.Empty {
		t.Fatal("the bounding box should not be empty")
	}
	if summary.Extent.East <= 0 {
		t.Fatalf("extent is %+v", summary.Extent)
	}
}

func TestSummariseTopologyCounters(t *testing.T) {
	inputs := buildInputs(t, sampleSurvey())
	summary := Summarise(inputs)
	// Station F is only referenced by the excluded leg, so it stays isolated and
	// forms a component of its own.
	if summary.ComponentCount != 2 {
		t.Fatalf("component count is %d", summary.ComponentCount)
	}
	if len(inputs.Analysis.Isolated) != 1 || inputs.Analysis.Isolated[0] != "F" {
		t.Fatalf("isolated stations are %v", inputs.Analysis.Isolated)
	}
	if summary.LoopCount != 0 || summary.FailingLoopCount != 0 {
		t.Fatalf("a chain survey reported %d loops", summary.LoopCount)
	}
	if summary.DeadEndCount == 0 {
		t.Fatal("the chain should end in a dangling passage")
	}
}

func TestSummariseTripStatistics(t *testing.T) {
	summary := Summarise(buildInputs(t, sampleSurvey()))
	if len(summary.Trips) != 2 {
		t.Fatalf("trip statistics are %+v", summary.Trips)
	}
	first := summary.Trips[0]
	if first.TripID != "T1" || first.Name != "entrance series" || first.Date != "2031-03-01" {
		t.Fatalf("first trip metadata is %+v", first)
	}
	if first.ShotCount != 3 || first.StationCount != 4 {
		t.Fatalf("first trip counts are %d shots and %d stations", first.ShotCount, first.StationCount)
	}
	if math.Abs(first.LengthMeters-35) > 1e-9 {
		t.Fatalf("first trip length is %v, want 35", first.LengthMeters)
	}
	if first.VerticalDropMeters <= 0 || first.VerticalGainMeters <= 0 {
		t.Fatalf("first trip vertical figures are %+v", first)
	}
	if first.MinInclinationDeg > -29 || first.MaxInclinationDeg < 9 {
		t.Fatalf("first trip inclination range is %v to %v", first.MinInclinationDeg, first.MaxInclinationDeg)
	}
	if first.LongestShot != "T1/S2" {
		t.Fatalf("first trip longest leg is %q", first.LongestShot)
	}
	if math.Abs(first.BacksightCoverage-1.0/3.0) > 1e-9 {
		t.Fatalf("first trip backsight coverage is %v", first.BacksightCoverage)
	}
	second := summary.Trips[1]
	if second.ExcludedShots != 1 {
		t.Fatalf("second trip excluded %d shots", second.ExcludedShots)
	}
	if math.Abs(second.LengthMeters-8) > 1e-9 {
		t.Fatalf("second trip length is %v, want 8", second.LengthMeters)
	}
	if summary.LongestTrip != "T1" || math.Abs(summary.LongestTripMeters-35) > 1e-9 {
		t.Fatalf("longest trip is %q at %v", summary.LongestTrip, summary.LongestTripMeters)
	}
	if summary.DeepestTrip == "" {
		t.Fatal("the deepest trip was not identified")
	}
}

func TestSummariseEmptySurvey(t *testing.T) {
	summary := Summarise(Inputs{})
	if summary.StationCount != 0 || summary.TotalLengthMeters != 0 {
		t.Fatalf("an empty survey summarised to %+v", summary)
	}
	if !summary.BoundingBox.Empty {
		t.Fatal("an empty survey should have an empty bounding box")
	}
	if summary.DeepestTrip != "" {
		t.Fatalf("deepest trip is %q", summary.DeepestTrip)
	}
}
