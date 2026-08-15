package network

import (
	"testing"

	"CaveLoop/internal/geom"
	"CaveLoop/internal/model"
	"CaveLoop/internal/reduce"
)

// leg builds a reduced shot with a horizontal displacement.
func leg(tripID, shotID, from, to string, east, north float64) reduce.Shot {
	vector := geom.Vector{East: east, North: north}
	return reduce.Shot{
		TripID:         tripID,
		ShotID:         shotID,
		From:           from,
		To:             to,
		DistanceMeters: vector.Length(),
		Vector:         vector,
	}
}

func sampleResult() reduce.Result {
	origin := geom.Vector{}
	return reduce.Result{
		Cave: "Network Cave",
		Stations: []reduce.Station{
			{Name: "A", Flags: []string{model.FlagFixed, model.FlagEntrance}, Fixed: &origin, Trips: []string{"T1"}},
			{Name: "B", Trips: []string{"T1"}},
			{Name: "C", Trips: []string{"T1"}},
			{Name: "D", Trips: []string{"T1"}},
			{Name: "E", Flags: []string{model.FlagEntrance}, Trips: []string{"T1"}},
			{Name: "a", Trips: []string{"T2"}},
			{Name: "Z", Trips: []string{"T2"}},
		},
		Shots: []reduce.Shot{
			leg("T1", "S1", "A", "B", 10, 0),
			leg("T1", "S2", "B", "C", 0, 10),
			leg("T1", "S3", "C", "A", -10, -10),
			leg("T1", "S4", "C", "D", 5, 5),
			leg("T1", "S5", "A", "E", -4, 0),
			leg("T2", "S6", "a", "Z", 1, 1),
			leg("T2", "S7", "a", "Z", 1.2, 1.1),
		},
	}
}

func TestBuildSkipsExcludedAndDegenerateLegs(t *testing.T) {
	result := sampleResult()
	excluded := leg("T1", "S8", "A", "D", 1, 1)
	excluded.Excluded = true
	selfLoop := leg("T1", "S9", "D", "D", 1, 1)
	orphan := leg("T1", "S10", "", "D", 1, 1)
	result.Shots = append(result.Shots, excluded, selfLoop, orphan)
	graph := Build(result)
	if len(graph.Edges) != 7 {
		t.Fatalf("graph holds %d legs, want 7", len(graph.Edges))
	}
	for _, edge := range graph.Edges {
		if edge.ShotID == "S8" || edge.ShotID == "S9" || edge.ShotID == "S10" {
			t.Fatalf("leg %s should have been skipped", edge.Key())
		}
	}
}

func TestGraphAccessors(t *testing.T) {
	graph := Build(sampleResult())
	if len(graph.Stations) != 7 {
		t.Fatalf("graph holds %d stations", len(graph.Stations))
	}
	if graph.Stations[0] != "A" || graph.Stations[len(graph.Stations)-1] != "a" {
		t.Fatalf("stations are ordered %v", graph.Stations)
	}
	if graph.Degree("A") != 3 {
		t.Fatalf("degree of A is %d, want 3", graph.Degree("A"))
	}
	if neighbors := graph.Neighbors("A"); len(neighbors) != 3 || neighbors[0] != "B" {
		t.Fatalf("neighbors of A are %v", neighbors)
	}
	if !graph.HasControl("A") || graph.HasControl("B") {
		t.Fatal("HasControl misreported the control stations")
	}
	if _, ok := graph.Station("A"); !ok {
		t.Fatal("Station did not find a declared station")
	}
	incident := graph.Incident("A")
	if len(incident) != 3 || incident[0] > incident[1] {
		t.Fatalf("incident legs are %v", incident)
	}
	edge := graph.Edges[0]
	if edge.Other("A") != "B" || edge.Other("B") != "A" {
		t.Fatal("Other did not resolve the opposite endpoint")
	}
	forward, reversed := edge.Directed("A")
	if reversed || forward != edge.Vector {
		t.Fatalf("forward traversal produced %+v reversed=%v", forward, reversed)
	}
	backward, reversed := edge.Directed("B")
	if !reversed || backward != edge.Vector.Negate() {
		t.Fatalf("reverse traversal produced %+v reversed=%v", backward, reversed)
	}
	if graph.TotalLength() <= 0 {
		t.Fatal("total length should be positive")
	}
}

func TestAnalyzeComponents(t *testing.T) {
	analysis := Analyze(Build(sampleResult()))
	if len(analysis.Components) != 2 {
		t.Fatalf("expected two components, got %d", len(analysis.Components))
	}
	first := analysis.Components[0]
	if first.Anchor != "A" || !first.Anchored {
		t.Fatalf("first component is %+v", first)
	}
	if len(first.Stations) != 5 || first.EdgeCount != 5 {
		t.Fatalf("first component holds %d stations and %d legs", len(first.Stations), first.EdgeCount)
	}
	second := analysis.Components[1]
	if second.Anchored {
		t.Fatalf("the second component should have no control: %+v", second)
	}
	if second.Anchor != "Z" && second.Anchor != "a" {
		t.Fatalf("second component anchor is %q", second.Anchor)
	}
	if !hasCode(analysis.Issues, "network-disconnected") || !hasCode(analysis.Issues, "component-unanchored") {
		t.Fatalf("topology issues are %v", analysis.Issues)
	}
}

func TestAnalyzeJunctionsAndDeadEnds(t *testing.T) {
	analysis := Analyze(Build(sampleResult()))
	if len(analysis.Junctions) != 2 {
		t.Fatalf("junctions are %+v", analysis.Junctions)
	}
	if analysis.Junctions[0].Station != "A" || analysis.Junctions[0].Degree != 3 {
		t.Fatalf("first junction is %+v", analysis.Junctions[0])
	}
	deadEnds := map[string]DeadEnd{}
	for _, deadEnd := range analysis.DeadEnds {
		deadEnds[deadEnd.Station] = deadEnd
	}
	if _, ok := deadEnds["A"]; ok {
		t.Fatal("a control station must not be reported as a dangling passage")
	}
	entrance, ok := deadEnds["E"]
	if !ok || !entrance.Entrance || entrance.FromStation != "A" {
		t.Fatalf("dangling entrance is %+v", entrance)
	}
	if plain, ok := deadEnds["D"]; !ok || plain.Entrance {
		t.Fatalf("dangling passage D is %+v", plain)
	}
}

func TestAnalyzeDuplicatesAndCollisions(t *testing.T) {
	analysis := Analyze(Build(sampleResult()))
	if len(analysis.Duplicates) != 1 {
		t.Fatalf("duplicates are %+v", analysis.Duplicates)
	}
	duplicate := analysis.Duplicates[0]
	if duplicate.From != "Z" || duplicate.To != "a" {
		t.Fatalf("duplicate pair is %q -> %q", duplicate.From, duplicate.To)
	}
	if len(duplicate.Shots) != 2 || duplicate.SpreadM <= 0 {
		t.Fatalf("duplicate legs are %+v", duplicate)
	}
	if len(analysis.NameCollisions) != 1 || analysis.NameCollisions[0].Normalized != "a" {
		t.Fatalf("name collisions are %+v", analysis.NameCollisions)
	}
	if len(analysis.NameCollisions[0].Names) != 2 {
		t.Fatalf("collision names are %v", analysis.NameCollisions[0].Names)
	}
	if !hasCode(analysis.Issues, "duplicate-leg") || !hasCode(analysis.Issues, "station-name-collision") {
		t.Fatalf("issues are %v", analysis.Issues)
	}
}

func TestAnalyzeIsolatedStation(t *testing.T) {
	result := sampleResult()
	result.Stations = append(result.Stations, reduce.Station{Name: "Q"})
	analysis := Analyze(Build(result))
	if len(analysis.Isolated) != 1 || analysis.Isolated[0] != "Q" {
		t.Fatalf("isolated stations are %v", analysis.Isolated)
	}
	if !hasCode(analysis.Issues, "station-isolated") {
		t.Fatalf("issues are %v", analysis.Issues)
	}
}

func TestAnalyzeIsDeterministic(t *testing.T) {
	first := Analyze(Build(sampleResult()))
	second := Analyze(Build(sampleResult()))
	if len(first.Components) != len(second.Components) {
		t.Fatal("component count differs between runs")
	}
	for index := range first.Components {
		if first.Components[index].Anchor != second.Components[index].Anchor {
			t.Fatalf("component %d anchor differs between runs", index)
		}
	}
	if len(first.Issues) != len(second.Issues) {
		t.Fatal("issue count differs between runs")
	}
	for index := range first.Issues {
		if first.Issues[index] != second.Issues[index] {
			t.Fatalf("issue %d differs between runs", index)
		}
	}
}

func hasCode(issues model.Issues, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
