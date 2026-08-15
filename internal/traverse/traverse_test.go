package traverse

import (
	"math"
	"testing"

	"CaveLoop/internal/geom"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/reduce"
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

func fixture() (network.Graph, network.Analysis) {
	anchor := geom.Vector{East: 100, North: 200, Up: 300}
	control := geom.Vector{East: 110.05, North: 200, Up: 300}
	result := reduce.Result{
		Cave: "Traverse Cave",
		Stations: []reduce.Station{
			{Name: "A", Flags: []string{model.FlagFixed}, Fixed: &anchor},
			{Name: "B", Flags: []string{model.FlagFixed}, Fixed: &control},
			{Name: "C"},
			{Name: "D"},
			{Name: "X"},
			{Name: "Y"},
		},
		Shots: []reduce.Shot{
			leg("S1", "A", "B", geom.Vector{East: 10}),
			leg("S2", "B", "C", geom.Vector{North: 10, Up: -5}),
			leg("S3", "A", "C", geom.Vector{East: 10, North: 10, Up: -5}),
			leg("S4", "C", "D", geom.Vector{North: 4, Up: -3}),
			leg("S5", "X", "Y", geom.Vector{East: 7}),
		},
	}
	graph := network.Build(result)
	return graph, network.Analyze(graph)
}

func TestComputeAnchorsOnControlStation(t *testing.T) {
	graph, analysis := fixture()
	layout, err := Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	position, ok := layout.Position("A")
	if !ok || !position.Anchor || !position.Control {
		t.Fatalf("anchor position is %+v", position)
	}
	if position.Coordinate != (geom.Vector{East: 100, North: 200, Up: 300}) {
		t.Fatalf("anchor coordinate is %+v", position.Coordinate)
	}
	b := layout.Coordinate("B")
	if math.Abs(b.East-110) > 1e-9 || math.Abs(b.North-200) > 1e-9 {
		t.Fatalf("station B is at %+v", b)
	}
	d := layout.Coordinate("D")
	if math.Abs(d.North-214) > 1e-9 || math.Abs(d.Up-292) > 1e-9 {
		t.Fatalf("station D is at %+v", d)
	}
	if _, ok := layout.Position("missing"); ok {
		t.Fatal("Position invented a station")
	}
	if layout.Coordinate("missing") != geom.Zero() {
		t.Fatal("Coordinate should fall back to the origin")
	}
}

func TestComputeSelectsShortestPathParent(t *testing.T) {
	graph, analysis := fixture()
	layout, err := Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	position, ok := layout.Position("C")
	if !ok {
		t.Fatal("station C was not positioned")
	}
	// The direct leg A -> C is 15 m while A -> B -> C accumulates 21.18 m.
	if position.Parent != "A" || position.ViaShot != "T1/S3" {
		t.Fatalf("station C was reached through %q via %q", position.Parent, position.ViaShot)
	}
	if math.Abs(position.PathLengthMeters-15) > 1e-6 {
		t.Fatalf("path length to C is %v", position.PathLengthMeters)
	}
}

func TestComputeIdentifiesChords(t *testing.T) {
	graph, analysis := fixture()
	layout, err := Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	if len(layout.ChordEdges) != 1 || layout.ChordEdges[0] != "T1/S2" {
		t.Fatalf("chord legs are %v", layout.ChordEdges)
	}
	if len(layout.TreeEdges) != 4 {
		t.Fatalf("tree legs are %v", layout.TreeEdges)
	}
	chords := layout.ChordIndices()
	if len(chords) != 1 || layout.IsTreeEdge(chords[0]) {
		t.Fatalf("chord indices are %v", chords)
	}
}

func TestComputeDepthIsRelativeToComponentDatum(t *testing.T) {
	graph, analysis := fixture()
	layout, err := Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	anchor, _ := layout.Position("A")
	if anchor.DepthMeters != 0 {
		t.Fatalf("the highest station should have zero depth, got %v", anchor.DepthMeters)
	}
	deepest, _ := layout.Position("D")
	if math.Abs(deepest.DepthMeters-8) > 1e-9 {
		t.Fatalf("depth of D is %v, want 8", deepest.DepthMeters)
	}
	unanchored, _ := layout.Position("X")
	if unanchored.Coordinate != geom.Zero() {
		t.Fatalf("an unanchored component should start at the origin, got %+v", unanchored.Coordinate)
	}
	if len(layout.DatumUpMeters) != 2 {
		t.Fatalf("datum table is %v", layout.DatumUpMeters)
	}
}

func TestComputeReportsControlResidual(t *testing.T) {
	graph, analysis := fixture()
	layout, err := Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	if len(layout.ControlResiduals) != 1 {
		t.Fatalf("control residuals are %+v", layout.ControlResiduals)
	}
	residual := layout.ControlResiduals[0]
	if residual.Station != "B" || math.Abs(residual.TotalMeters-0.05) > 1e-9 {
		t.Fatalf("residual is %+v", residual)
	}
	if math.Abs(residual.HorizontalMeters-0.05) > 1e-9 || residual.VerticalMeters != 0 {
		t.Fatalf("residual split is %+v", residual)
	}
	found := false
	for _, issue := range layout.Issues {
		if issue.Code == "control-residual" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues are %v", layout.Issues)
	}
}

func TestComputeAppliesDisplacementOverride(t *testing.T) {
	graph, analysis := fixture()
	overrides := make([]geom.Vector, len(graph.Edges))
	for index, edge := range graph.Edges {
		overrides[index] = edge.Vector.Scale(2)
	}
	layout, err := Compute(graph, analysis, overrides)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	if got := layout.Coordinate("B").East; math.Abs(got-120) > 1e-9 {
		t.Fatalf("station B east is %v, want 120", got)
	}
	if _, err := Compute(graph, analysis, overrides[:1]); err == nil {
		t.Fatal("a mismatched override length was accepted")
	}
}

func TestComputeCoordinatesAreSortedByStation(t *testing.T) {
	graph, analysis := fixture()
	layout, err := Compute(graph, analysis, nil)
	if err != nil {
		t.Fatalf("Compute returned %v", err)
	}
	previous := ""
	for _, position := range layout.Positions {
		if position.Station <= previous {
			t.Fatalf("positions are not sorted: %q after %q", position.Station, previous)
		}
		previous = position.Station
	}
	if len(layout.Coordinates()) != len(layout.Positions) {
		t.Fatal("Coordinates and Positions disagree")
	}
}
