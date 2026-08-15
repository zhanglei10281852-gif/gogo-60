package adjust

import (
	"math"
	"testing"

	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/loops"
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

func squareWithError(closureError geom.Vector) reduce.Result {
	origin := geom.Vector{}
	return reduce.Result{
		Cave: "Adjust Cave",
		Stations: []reduce.Station{
			{Name: "A", Flags: []string{model.FlagFixed}, Fixed: &origin},
			{Name: "B"}, {Name: "C"}, {Name: "D"},
		},
		Shots: []reduce.Shot{
			leg("S1", "A", "B", geom.Vector{East: 10}),
			leg("S2", "B", "C", geom.Vector{North: 20}),
			leg("S3", "C", "D", geom.Vector{East: -10}),
			leg("S4", "D", "A", geom.Vector{North: -20}.Add(closureError)),
		},
	}
}

func pipeline(t *testing.T, result reduce.Result) (network.Graph, loops.Result) {
	t.Helper()
	graph := network.Build(result)
	analysis := network.Analyze(graph)
	layout, err := traverse.Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	loopResult, err := loops.Detect(graph, analysis, layout, config.Default().Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	return graph, loopResult
}

func TestApplyClosesTheLoop(t *testing.T) {
	graph, loopResult := pipeline(t, squareWithError(geom.Vector{East: 0.6, Up: 0.3}))
	result, err := Apply(graph, loopResult, config.Default().Adjustment)
	if err != nil {
		t.Fatalf("Apply returned %v", err)
	}
	if !result.Converged {
		t.Fatalf("the adjustment did not converge: %+v", result)
	}
	closure := geom.Zero()
	for _, leg := range loopResult.Loops[0].Legs {
		vector := result.Vectors[leg.EdgeIndex]
		if leg.Reversed {
			vector = vector.Negate()
		}
		closure = closure.Add(vector)
	}
	if closure.Length() > 1e-6 {
		t.Fatalf("the adjusted loop closes to %v", closure.Length())
	}
	if len(result.Residuals) != 1 || result.Residuals[0].AfterMeters > 1e-6 {
		t.Fatalf("residuals are %+v", result.Residuals)
	}
	if result.Residuals[0].BeforeMeters <= 0 || result.Residuals[0].BeforePPM <= 0 {
		t.Fatalf("the residual report lost the original error: %+v", result.Residuals[0])
	}
}

func TestApplyDistributesProportionallyToLength(t *testing.T) {
	graph, loopResult := pipeline(t, squareWithError(geom.Vector{East: 0.6}))
	result, err := Apply(graph, loopResult, config.Default().Adjustment)
	if err != nil {
		t.Fatalf("Apply returned %v", err)
	}
	byShot := make(map[string]ShotAdjustment, len(result.Adjustments))
	for _, adjustment := range result.Adjustments {
		byShot[adjustment.ShotKey] = adjustment
	}
	short := byShot["T1/S1"]
	long := byShot["T1/S2"]
	if short.MagnitudeMeters <= 0 || long.MagnitudeMeters <= 0 {
		t.Fatalf("corrections are %+v and %+v", short, long)
	}
	ratio := long.MagnitudeMeters / short.MagnitudeMeters
	if math.Abs(ratio-2) > 1e-6 {
		t.Fatalf("a 20 m leg absorbed %v times the correction of a 10 m leg, want 2", ratio)
	}
	if math.Abs(short.RelativePPM-long.RelativePPM) > 1e-6 {
		t.Fatalf("relative corrections differ: %v and %v", short.RelativePPM, long.RelativePPM)
	}
	total := 0.0
	for _, adjustment := range result.Adjustments {
		total += adjustment.MagnitudeMeters
	}
	if math.Abs(total-0.6) > 1e-6 {
		t.Fatalf("the distributed corrections sum to %v, want 0.6", total)
	}
	if result.AdjustedLegs != 4 {
		t.Fatalf("adjusted legs are %d", result.AdjustedLegs)
	}
	if result.WorstShot != "T1/S2" && result.WorstShot != "T1/S4" {
		t.Fatalf("worst shot is %q", result.WorstShot)
	}
}

func TestApplyCanSkipTheVerticalComponent(t *testing.T) {
	graph, loopResult := pipeline(t, squareWithError(geom.Vector{East: 0.4, Up: 0.9}))
	settings := config.Default().Adjustment
	settings.AdjustVertical = false
	result, err := Apply(graph, loopResult, settings)
	if err != nil {
		t.Fatalf("Apply returned %v", err)
	}
	if result.VerticalDistributed {
		t.Fatal("the vertical component should not have been distributed")
	}
	for _, adjustment := range result.Adjustments {
		if math.Abs(adjustment.Correction.Up) > 1e-12 {
			t.Fatalf("leg %s received a vertical correction of %v", adjustment.ShotKey, adjustment.Correction.Up)
		}
	}
	closure := geom.Zero()
	for _, leg := range loopResult.Loops[0].Legs {
		vector := result.Vectors[leg.EdgeIndex]
		if leg.Reversed {
			vector = vector.Negate()
		}
		closure = closure.Add(vector)
	}
	if closure.HorizontalLength() > 1e-6 {
		t.Fatalf("the horizontal misclosure remains at %v", closure.HorizontalLength())
	}
	if math.Abs(closure.Up-0.9) > 1e-9 {
		t.Fatalf("the vertical misclosure changed to %v", closure.Up)
	}
}

func TestApplyDisabledReturnsOriginalVectors(t *testing.T) {
	graph, loopResult := pipeline(t, squareWithError(geom.Vector{East: 0.5}))
	settings := config.Default().Adjustment
	settings.Enabled = false
	result, err := Apply(graph, loopResult, settings)
	if err != nil {
		t.Fatalf("Apply returned %v", err)
	}
	if len(result.Adjustments) != 0 || !result.Converged {
		t.Fatalf("a disabled adjustment produced %+v", result)
	}
	for index, edge := range graph.Edges {
		if result.Vectors[index] != edge.Vector {
			t.Fatalf("leg %s was modified while disabled", edge.Key())
		}
	}
}

func TestApplyWithoutLoopsIsANoOp(t *testing.T) {
	origin := geom.Vector{}
	chain := reduce.Result{
		Cave:     "Chain Cave",
		Stations: []reduce.Station{{Name: "A", Flags: []string{model.FlagFixed}, Fixed: &origin}, {Name: "B"}},
		Shots:    []reduce.Shot{leg("S1", "A", "B", geom.Vector{East: 5})},
	}
	graph, loopResult := pipeline(t, chain)
	result, err := Apply(graph, loopResult, config.Default().Adjustment)
	if err != nil {
		t.Fatalf("Apply returned %v", err)
	}
	if len(result.Adjustments) != 0 || len(result.Residuals) != 0 {
		t.Fatalf("a loop free network produced %+v", result)
	}
}

func TestApplyRejectsZeroPasses(t *testing.T) {
	graph, loopResult := pipeline(t, squareWithError(geom.Vector{East: 0.5}))
	settings := config.Default().Adjustment
	settings.MaxPasses = 0
	if _, err := Apply(graph, loopResult, settings); err == nil {
		t.Fatal("a zero pass adjustment was accepted")
	}
}

func TestApplyReportsNonConvergence(t *testing.T) {
	graph, loopResult := pipeline(t, squareWithError(geom.Vector{East: 5}))
	settings := config.Default().Adjustment
	settings.MaxPasses = 1
	settings.Convergence = 1e-12
	result, err := Apply(graph, loopResult, settings)
	if err != nil {
		t.Fatalf("Apply returned %v", err)
	}
	if result.Converged {
		t.Fatal("a single pass with a tiny threshold should not converge")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Code == "adjustment-not-converged" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues are %v", result.Issues)
	}
}

func TestLegWeightHonoursFloor(t *testing.T) {
	if got := legWeight(0.01, 0.05); got != 0.05 {
		t.Fatalf("legWeight = %v, want the floor 0.05", got)
	}
	if got := legWeight(3, 0.05); got != 3 {
		t.Fatalf("legWeight = %v, want 3", got)
	}
}
