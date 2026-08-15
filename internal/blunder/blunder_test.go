package blunder

import (
	"fmt"
	"testing"

	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/loops"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/reduce"
	"CaveLoop/internal/traverse"
)

// polarLeg builds a reduced leg whose vector matches its polar readings.
func polarLeg(shotID, from, to string, distance, azimuth, inclination float64) reduce.Shot {
	return reduce.Shot{
		TripID:            "T1",
		ShotID:            shotID,
		From:              from,
		To:                to,
		DistanceMeters:    distance,
		AzimuthDeg:        azimuth,
		InclinationDeg:    inclination,
		RawDistanceMeters: distance,
		RawAzimuthDeg:     azimuth,
		HorizontalMeters:  geom.HorizontalProjection(distance, inclination),
		Vector:            geom.FromPolar(distance, azimuth, inclination),
	}
}

func withAnchor(shots []reduce.Shot, extra ...reduce.Station) reduce.Result {
	origin := geom.Vector{}
	stations := []reduce.Station{{Name: "A", Flags: []string{model.FlagFixed}, Fixed: &origin}}
	seen := map[string]bool{"A": true}
	for _, shot := range shots {
		for _, name := range []string{shot.From, shot.To} {
			if seen[name] {
				continue
			}
			seen[name] = true
			stations = append(stations, reduce.Station{Name: name})
		}
	}
	stations = append(stations, extra...)
	return reduce.Result{Cave: "Blunder Cave", Stations: stations, Shots: shots}
}

func analyse(t *testing.T, result reduce.Result, cfg config.Config) (network.Graph, loops.Result) {
	t.Helper()
	graph := network.Build(result)
	analysis := network.Analyze(graph)
	layout, err := traverse.Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	loopResult, err := loops.Detect(graph, analysis, layout, cfg.Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	return graph, loopResult
}

func findingsByCode(result Result) map[string][]Finding {
	out := make(map[string][]Finding)
	for _, finding := range result.Findings {
		out[finding.Code] = append(out[finding.Code], finding)
	}
	return out
}

func TestDetectDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Blunders.Enabled = false
	reduced := withAnchor([]reduce.Shot{polarLeg("S1", "A", "B", 10, 0, 0)})
	graph, loopResult := analyse(t, reduced, cfg)
	result := Detect(reduced, graph, loopResult, cfg)
	if result.Enabled || len(result.Findings) != 0 {
		t.Fatalf("a disabled scan produced %+v", result)
	}
}

func TestDetectReversedReading(t *testing.T) {
	cfg := config.Default()
	shot := polarLeg("S1", "A", "B", 10, 90, 0)
	shot.Reconciliation = reduce.Reconciliation{
		HasBacksight:           true,
		AzimuthDisagreementDeg: 179.4,
	}
	reduced := withAnchor([]reduce.Shot{shot})
	graph, loopResult := analyse(t, reduced, cfg)
	grouped := findingsByCode(Detect(reduced, graph, loopResult, cfg))
	found := grouped[CodeReversedReading]
	if len(found) != 1 {
		t.Fatalf("findings are %+v", grouped)
	}
	if found[0].Severity != model.SeverityError || found[0].Subject != "T1/S1" {
		t.Fatalf("finding is %+v", found[0])
	}
	if found[0].Suggestion == "" || len(found[0].Evidence) != 2 {
		t.Fatalf("the finding carries no usable detail: %+v", found[0])
	}
}

func TestDetectIgnoresAcceptableBacksight(t *testing.T) {
	cfg := config.Default()
	shot := polarLeg("S1", "A", "B", 10, 90, 0)
	shot.Reconciliation = reduce.Reconciliation{HasBacksight: true, AzimuthDisagreementDeg: 1.2}
	reduced := withAnchor([]reduce.Shot{shot})
	graph, loopResult := analyse(t, reduced, cfg)
	if findings := findingsByCode(Detect(reduced, graph, loopResult, cfg))[CodeReversedReading]; len(findings) != 0 {
		t.Fatalf("a good backsight was flagged: %+v", findings)
	}
}

func TestDetectGrossLengthOutlier(t *testing.T) {
	cfg := config.Default()
	cfg.Blunders.LengthOutlierSigma = 2.0
	cfg.Blunders.LengthOutlierMinimum = 6
	lengths := []float64{6.0, 5.5, 7.2, 6.4, 5.9, 6.8, 7.1, 90.0}
	shots := make([]reduce.Shot, 0, len(lengths))
	for index, length := range lengths {
		from := fmt.Sprintf("P%02d", index)
		if index == 0 {
			from = "A"
		}
		shots = append(shots, polarLeg(fmt.Sprintf("S%02d", index), from, fmt.Sprintf("P%02d", index+1), length, 90, 0))
	}
	reduced := withAnchor(shots)
	graph, loopResult := analyse(t, reduced, cfg)
	found := findingsByCode(Detect(reduced, graph, loopResult, cfg))[CodeLengthOutlier]
	if len(found) != 1 {
		t.Fatalf("expected one outlier, got %+v", found)
	}
	if found[0].Subject != "T1/S07" {
		t.Fatalf("the flagged leg is %q", found[0].Subject)
	}
	if found[0].Score < cfg.Blunders.LengthOutlierSigma {
		t.Fatalf("the score %v is below the threshold", found[0].Score)
	}
	if len(found[0].Evidence) != 3 {
		t.Fatalf("evidence is %v", found[0].Evidence)
	}
}

func TestDetectSkipsShortTripsForOutliers(t *testing.T) {
	cfg := config.Default()
	shots := []reduce.Shot{
		polarLeg("S1", "A", "B", 5, 90, 0),
		polarLeg("S2", "B", "C", 200, 90, 0),
	}
	reduced := withAnchor(shots)
	graph, loopResult := analyse(t, reduced, cfg)
	if found := findingsByCode(Detect(reduced, graph, loopResult, cfg))[CodeLengthOutlier]; len(found) != 0 {
		t.Fatalf("a two leg trip produced outliers: %+v", found)
	}
}

func TestDetectTransposedAzimuthDigits(t *testing.T) {
	cfg := config.Default()
	shots := []reduce.Shot{
		polarLeg("S1", "A", "B", 30, 90, 0),
		polarLeg("S2", "B", "C", 30, 120, 0), // the field note should read 210
		polarLeg("S3", "C", "A", 30, 330, 0),
	}
	reduced := withAnchor(shots)
	graph, loopResult := analyse(t, reduced, cfg)
	grouped := findingsByCode(Detect(reduced, graph, loopResult, cfg))
	if len(grouped[CodeLoopClosure]) != 1 {
		t.Fatalf("the failing loop was not reported: %+v", grouped)
	}
	found := grouped[CodeTransposedDigits]
	if len(found) != 1 {
		t.Fatalf("expected one transposition finding, got %+v", found)
	}
	if found[0].Subject != "T1/S2" {
		t.Fatalf("the flagged leg is %q", found[0].Subject)
	}
	if found[0].Score < cfg.Blunders.TransposeImprovement {
		t.Fatalf("improvement score is %v", found[0].Score)
	}
}

func TestDetectReversedLegInLoop(t *testing.T) {
	cfg := config.Default()
	shots := []reduce.Shot{
		polarLeg("S1", "A", "B", 30, 90, 0),
		polarLeg("S2", "B", "C", 30, 30, 0), // the correct bearing is 210
		polarLeg("S3", "C", "A", 30, 330, 0),
	}
	reduced := withAnchor(shots)
	graph, loopResult := analyse(t, reduced, cfg)
	grouped := findingsByCode(Detect(reduced, graph, loopResult, cfg))
	found := grouped[CodeReversedLeg]
	if len(found) != 1 || found[0].Subject != "T1/S2" {
		t.Fatalf("reversed leg findings are %+v", found)
	}
	if found[0].Suggestion == "" {
		t.Fatal("the reversed leg finding carries no suggestion")
	}
}

func TestDetectVerticalDominantClosure(t *testing.T) {
	cfg := config.Default()
	shots := []reduce.Shot{
		polarLeg("S1", "A", "B", 20, 0, 0),
		polarLeg("S2", "B", "C", 20, 90, 0),
		polarLeg("S3", "C", "D", 20, 180, 0),
		polarLeg("S4", "D", "A", 20, 270, -3),
	}
	reduced := withAnchor(shots)
	graph, loopResult := analyse(t, reduced, cfg)
	grouped := findingsByCode(Detect(reduced, graph, loopResult, cfg))
	if len(grouped[CodeVerticalDominant]) != 1 {
		t.Fatalf("vertical findings are %+v", grouped)
	}
	if grouped[CodeVerticalDominant][0].Loop != "L001" {
		t.Fatalf("the finding is attached to loop %q", grouped[CodeVerticalDominant][0].Loop)
	}
}

func TestDetectCountsAndOrdering(t *testing.T) {
	cfg := config.Default()
	shots := []reduce.Shot{
		polarLeg("S1", "A", "B", 30, 90, 0),
		polarLeg("S2", "B", "C", 30, 120, 0),
		polarLeg("S3", "C", "A", 30, 330, 0),
	}
	reduced := withAnchor(shots)
	graph, loopResult := analyse(t, reduced, cfg)
	result := Detect(reduced, graph, loopResult, cfg)
	total := 0
	for _, count := range result.Counts {
		total += count
	}
	if total != len(result.Findings) {
		t.Fatalf("counts %v do not match %d findings", result.Counts, len(result.Findings))
	}
	previous := ""
	for _, finding := range result.Findings {
		key := finding.Code + "|" + finding.Subject
		if key < previous {
			t.Fatalf("findings are not sorted: %q after %q", key, previous)
		}
		previous = key
	}
	if len(result.Issues) != len(result.Findings) {
		t.Fatalf("issues are %v", result.Issues)
	}
}

func TestTranspositions(t *testing.T) {
	got := transpositions(120)
	want := map[float64]bool{21: true, 102: true, 210: true}
	if len(got) != len(want) {
		t.Fatalf("transpositions(120) = %v", got)
	}
	for _, candidate := range got {
		if !want[candidate] {
			t.Fatalf("unexpected candidate %v in %v", candidate, got)
		}
	}
	previous := -1.0
	for _, candidate := range got {
		if candidate <= previous {
			t.Fatalf("candidates are not sorted: %v", got)
		}
		previous = candidate
	}
	if len(transpositions(7)) != 0 {
		t.Fatalf("a single digit bearing produced %v", transpositions(7))
	}
}

func TestImprovement(t *testing.T) {
	if got := improvement(10, 2); got != 0.8 {
		t.Fatalf("improvement(10,2) = %v", got)
	}
	if got := improvement(10, 12); got != 0 {
		t.Fatalf("a worse candidate scored %v", got)
	}
	if got := improvement(0, 1); got != 0 {
		t.Fatalf("improvement over a perfect baseline = %v", got)
	}
}
