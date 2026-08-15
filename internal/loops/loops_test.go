package loops

import (
	"math"
	"testing"

	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/reduce"
	"CaveLoop/internal/traverse"
)

func leg(shotID, from, to string, vector geom.Vector) reduce.Shot {
	return reduce.Shot{
		TripID:         "T1",
		ShotID:         shotID,
		From:           from,
		To:             to,
		DistanceMeters: vector.Length(),
		Vector:         vector,
	}
}

// square builds a four leg circuit that closes with the supplied error vector.
func square(closureError geom.Vector) reduce.Result {
	origin := geom.Vector{}
	return reduce.Result{
		Cave: "Loop Cave",
		Stations: []reduce.Station{
			{Name: "A", Flags: []string{model.FlagFixed}, Fixed: &origin},
			{Name: "B"}, {Name: "C"}, {Name: "D"}, {Name: "E"},
		},
		Shots: []reduce.Shot{
			leg("S1", "A", "B", geom.Vector{East: 10}),
			leg("S2", "B", "C", geom.Vector{North: 10}),
			leg("S3", "C", "D", geom.Vector{East: -10}),
			leg("S4", "D", "A", geom.Vector{North: -10}.Add(closureError)),
			leg("S5", "C", "E", geom.Vector{North: 4}),
		},
	}
}

func build(t *testing.T, result reduce.Result) (network.Graph, network.Analysis, traverse.Result) {
	t.Helper()
	graph := network.Build(result)
	analysis := network.Analyze(graph)
	layout, err := traverse.Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	return graph, analysis, layout
}

func TestDetectFindsOneIndependentLoop(t *testing.T) {
	graph, analysis, layout := build(t, square(geom.Vector{}))
	result, err := Detect(graph, analysis, layout, config.Default().Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	if result.IndependentCount != 1 {
		t.Fatalf("independent count is %d, want 1", result.IndependentCount)
	}
	if len(result.Loops) != 1 {
		t.Fatalf("loops are %+v", result.Loops)
	}
	loop := result.Loops[0]
	if loop.ID != "L001" {
		t.Fatalf("loop identifier is %q", loop.ID)
	}
	if len(loop.Legs) != 4 {
		t.Fatalf("loop uses %d legs, want 4", len(loop.Legs))
	}
	if math.Abs(loop.LengthMeters-40) > 1e-9 {
		t.Fatalf("loop length is %v, want 40", loop.LengthMeters)
	}
	if loop.TotalErrorMeters > 1e-9 {
		t.Fatalf("a perfect loop closed to %v", loop.TotalErrorMeters)
	}
	if !loop.WithinTolerance || len(loop.Failures) != 0 {
		t.Fatalf("a perfect loop was rejected: %+v", loop)
	}
	if loop.Stations[0] != loop.Stations[len(loop.Stations)-1] {
		t.Fatalf("the circuit does not close: %v", loop.Stations)
	}
	if len(loop.EdgeIndices()) != 4 {
		t.Fatalf("edge indices are %v", loop.EdgeIndices())
	}
	if _, ok := result.LoopByID("L001"); !ok {
		t.Fatal("LoopByID did not find the loop")
	}
	if _, ok := result.LoopByID("L999"); ok {
		t.Fatal("LoopByID invented a loop")
	}
}

func TestDetectMeasuresClosureError(t *testing.T) {
	graph, analysis, layout := build(t, square(geom.Vector{East: 0.3, North: 0.4, Up: 0.5}))
	result, err := Detect(graph, analysis, layout, config.Default().Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	loop := result.Loops[0]
	if math.Abs(loop.HorizontalErrorMeters-0.5) > 1e-9 {
		t.Fatalf("horizontal error is %v, want 0.5", loop.HorizontalErrorMeters)
	}
	if math.Abs(loop.VerticalErrorMeters-0.5) > 1e-9 {
		t.Fatalf("vertical error is %v, want 0.5", loop.VerticalErrorMeters)
	}
	expected := math.Sqrt(0.3*0.3 + 0.4*0.4 + 0.5*0.5)
	if math.Abs(loop.TotalErrorMeters-expected) > 1e-9 {
		t.Fatalf("total error is %v, want %v", loop.TotalErrorMeters, expected)
	}
	if loop.WithinTolerance {
		t.Fatal("a loop above tolerance was accepted")
	}
	// 0.707 m over 40.7 m is 17376 ppm, still under the relative tolerance, so
	// only the absolute and the vertical checks fail.
	if len(loop.Failures) != 2 || loop.Failures[0] != "total-error" || loop.Failures[1] != "vertical-error" {
		t.Fatalf("failure codes are %v", loop.Failures)
	}
	if result.FailingCount != 1 || result.WorstLoop != "L001" {
		t.Fatalf("result summary is %+v", result)
	}
	if math.Abs(result.WorstErrorMeters-expected) > 1e-9 {
		t.Fatalf("worst error is %v", result.WorstErrorMeters)
	}
	if len(result.Issues) != 1 || result.Issues[0].Code != "loop-closure-out-of-tolerance" {
		t.Fatalf("issues are %v", result.Issues)
	}
}

func TestDetectPartsPerMillion(t *testing.T) {
	graph, analysis, layout := build(t, square(geom.Vector{East: 0.04}))
	result, err := Detect(graph, analysis, layout, config.Default().Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	loop := result.Loops[0]
	// 0.04 m of error over roughly 40.04 m of passage is about 999 ppm.
	if loop.ErrorPPM < 950 || loop.ErrorPPM > 1050 {
		t.Fatalf("relative error is %v ppm", loop.ErrorPPM)
	}
	if !loop.WithinTolerance {
		t.Fatalf("a small misclosure was rejected: %+v", loop)
	}
}

func TestDetectWithoutLoops(t *testing.T) {
	origin := geom.Vector{}
	result := reduce.Result{
		Cave: "Chain Cave",
		Stations: []reduce.Station{
			{Name: "A", Flags: []string{model.FlagFixed}, Fixed: &origin}, {Name: "B"}, {Name: "C"},
		},
		Shots: []reduce.Shot{
			leg("S1", "A", "B", geom.Vector{East: 5}),
			leg("S2", "B", "C", geom.Vector{East: 5}),
		},
	}
	graph, analysis, layout := build(t, result)
	detected, err := Detect(graph, analysis, layout, config.Default().Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	if detected.IndependentCount != 0 || len(detected.Loops) != 0 {
		t.Fatalf("a tree should have no loop: %+v", detected)
	}
	if detected.WorstLoop != "" {
		t.Fatalf("worst loop is %q", detected.WorstLoop)
	}
}

func TestDetectHandlesTwoIndependentLoops(t *testing.T) {
	origin := geom.Vector{}
	result := reduce.Result{
		Cave: "Double Loop Cave",
		Stations: []reduce.Station{
			{Name: "A", Flags: []string{model.FlagFixed}, Fixed: &origin},
			{Name: "B"}, {Name: "C"}, {Name: "D"},
		},
		Shots: []reduce.Shot{
			leg("S1", "A", "B", geom.Vector{East: 10}),
			leg("S2", "B", "C", geom.Vector{North: 10}),
			leg("S3", "A", "C", geom.Vector{East: 10, North: 10}),
			leg("S4", "C", "D", geom.Vector{East: 10}),
			leg("S5", "B", "D", geom.Vector{East: 10, North: 10}),
		},
	}
	graph, analysis, layout := build(t, result)
	detected, err := Detect(graph, analysis, layout, config.Default().Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	if detected.IndependentCount != 2 || len(detected.Loops) != 2 {
		t.Fatalf("expected two loops, got %+v", detected.Loops)
	}
	if detected.Loops[0].ID != "L001" || detected.Loops[1].ID != "L002" {
		t.Fatalf("loop identifiers are %q and %q", detected.Loops[0].ID, detected.Loops[1].ID)
	}
	if detected.Loops[0].ChordShot >= detected.Loops[1].ChordShot {
		t.Fatalf("loops are not ordered by chord: %q, %q", detected.Loops[0].ChordShot, detected.Loops[1].ChordShot)
	}
}

func TestDetectAppliesDisplacementOverride(t *testing.T) {
	graph, analysis, layout := build(t, square(geom.Vector{East: 0.6}))
	overrides := make([]geom.Vector, len(graph.Edges))
	for index, edge := range graph.Edges {
		overrides[index] = edge.Vector
		if edge.ShotID == "S4" {
			overrides[index] = geom.Vector{North: -10}
		}
	}
	detected, err := Detect(graph, analysis, layout, config.Default().Tolerances, overrides)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	if detected.Loops[0].TotalErrorMeters > 1e-9 {
		t.Fatalf("the corrected loop closed to %v", detected.Loops[0].TotalErrorMeters)
	}
	if _, err := Detect(graph, analysis, layout, config.Default().Tolerances, overrides[:2]); err == nil {
		t.Fatal("a mismatched override length was accepted")
	}
}

func TestJoinFailures(t *testing.T) {
	if got := joinFailures(nil); got != "within tolerance" {
		t.Fatalf("joinFailures(nil) = %q", got)
	}
	if got := joinFailures([]string{"a", "b"}); got != "a, b" {
		t.Fatalf("joinFailures = %q", got)
	}
}
