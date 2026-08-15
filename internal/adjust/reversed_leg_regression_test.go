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

// TestApplyClosesLoopMeasuredAgainstTheCircuitDirection adjusts a circuit whose
// legs were not all surveyed in the direction the circuit is walked. The
// distribution must still remove the misclosure and converge.
func TestApplyClosesLoopMeasuredAgainstTheCircuitDirection(t *testing.T) {
	origin := geom.Vector{}
	survey := reduce.Result{
		Cave: "Reverse Cave",
		Stations: []reduce.Station{
			{Name: "A", Flags: []string{model.FlagFixed}, Fixed: &origin},
			{Name: "B"}, {Name: "C"}, {Name: "D"},
		},
		Shots: []reduce.Shot{
			reversedLeg("S1", "A", "B", geom.Vector{East: 10}),
			reversedLeg("S2", "B", "C", geom.Vector{North: 20}),
			reversedLeg("S3", "D", "C", geom.Vector{East: 10}),
			reversedLeg("S4", "D", "A", geom.Vector{North: -20, East: 0.6}),
		},
	}
	graph := network.Build(survey)
	analysis := network.Analyze(graph)
	layout, err := traverse.Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	loopResult, err := loops.Detect(graph, analysis, layout, config.Default().Tolerances, nil)
	if err != nil {
		t.Fatalf("Detect returned %v", err)
	}
	if len(loopResult.Loops) != 1 {
		t.Fatalf("the fixture produced %d loops, want 1", len(loopResult.Loops))
	}
	circuit := loopResult.Loops[0]
	reversedLegs := 0
	for _, leg := range circuit.Legs {
		if leg.Reversed {
			reversedLegs++
		}
	}
	if reversedLegs == 0 {
		t.Fatalf("the fixture no longer walks any leg backwards: %+v", circuit.Legs)
	}
	if circuit.TotalErrorMeters < 0.5 {
		t.Fatalf("the fixture misclosure is only %v m", circuit.TotalErrorMeters)
	}

	settings := config.Default().Adjustment
	result, err := Apply(graph, loopResult, settings)
	if err != nil {
		t.Fatalf("Apply returned %v", err)
	}
	if !result.Converged {
		t.Fatalf("the adjustment did not converge: passes=%d maxResidual=%v", result.Passes, result.MaxResidualMeters)
	}
	if result.MaxResidualMeters > settings.Convergence {
		t.Fatalf("the largest residual is %v m, the threshold is %v m", result.MaxResidualMeters, settings.Convergence)
	}
	if len(result.Residuals) != 1 {
		t.Fatalf("residuals are %+v", result.Residuals)
	}
	if result.Residuals[0].AfterMeters > settings.Convergence {
		t.Fatalf("the loop still closes to %v m after adjustment", result.Residuals[0].AfterMeters)
	}
	if result.Residuals[0].AfterMeters > result.Residuals[0].BeforeMeters {
		t.Fatalf("the misclosure grew from %v m to %v m", result.Residuals[0].BeforeMeters, result.Residuals[0].AfterMeters)
	}
	if result.UnadjustableLoopCount != 0 {
		t.Fatalf("the loop was reported as unadjustable: %+v", result.Issues)
	}

	closure := geom.Zero()
	for _, leg := range circuit.Legs {
		vector := result.Vectors[leg.EdgeIndex]
		if leg.Reversed {
			vector = vector.Negate()
		}
		closure = closure.Add(vector)
	}
	if closure.Length() > 1e-6 {
		t.Fatalf("the adjusted circuit closes to %v m", closure.Length())
	}
	total := 0.0
	for _, adjustment := range result.Adjustments {
		total += adjustment.MagnitudeMeters
	}
	if math.Abs(total-circuit.TotalErrorMeters) > 1e-6 {
		t.Fatalf("the distributed corrections sum to %v m, want the %v m misclosure",
			total, circuit.TotalErrorMeters)
	}
}

// reversedLeg builds a reduced leg for the regression fixture.
func reversedLeg(shotID, from, to string, vector geom.Vector) reduce.Shot {
	return reduce.Shot{
		TripID:         "T1",
		ShotID:         shotID,
		From:           from,
		To:             to,
		DistanceMeters: vector.Length(),
		Vector:         vector,
	}
}
