// Package network builds the undirected survey graph from reduced legs and
// derives its topology: connected components, junctions, dangling passages,
// duplicate legs and suspicious station naming.
//
// The graph keeps parallel edges because two independent legs between the same
// pair of stations are exactly what creates a closed loop in a cave survey.
package network

import (
	"fmt"
	"sort"
	"strings"

	"CaveLoop/internal/geom"
	"CaveLoop/internal/model"
	"CaveLoop/internal/reduce"
	"CaveLoop/internal/units"
)

// Edge is one leg of the survey graph.
type Edge struct {
	Index        int         `json:"index"`
	TripID       string      `json:"tripId"`
	ShotID       string      `json:"shotId"`
	From         string      `json:"from"`
	To           string      `json:"to"`
	LengthMeters float64     `json:"lengthMeters"`
	Vector       geom.Vector `json:"vector"`
}

// Key returns the stable identity of the edge.
func (e Edge) Key() string { return e.TripID + "/" + e.ShotID }

// Other returns the endpoint of the edge that is not station.
func (e Edge) Other(station string) string {
	if station == e.From {
		return e.To
	}
	return e.From
}

// Directed returns the displacement of the edge when traversed starting from
// the given station, together with a flag telling whether it was reversed.
func (e Edge) Directed(from string) (geom.Vector, bool) {
	if from == e.From {
		return e.Vector, false
	}
	return e.Vector.Negate(), true
}

// Graph is the undirected multigraph of the survey.
type Graph struct {
	Stations  []string
	Edges     []Edge
	stations  map[string]reduce.Station
	adjacency map[string][]int
}

// Build assembles the graph from a reduction result. Excluded legs and legs
// whose endpoints are empty are skipped.
func Build(result reduce.Result) Graph {
	graph := Graph{
		stations:  make(map[string]reduce.Station, len(result.Stations)),
		adjacency: make(map[string][]int),
	}
	for _, station := range result.Stations {
		graph.stations[station.Name] = station
	}
	names := make(map[string]bool, len(result.Stations))
	for _, station := range result.Stations {
		names[station.Name] = true
	}
	for _, shot := range result.ActiveShots() {
		if shot.From == "" || shot.To == "" || shot.From == shot.To {
			continue
		}
		edge := Edge{
			Index:        len(graph.Edges),
			TripID:       shot.TripID,
			ShotID:       shot.ShotID,
			From:         shot.From,
			To:           shot.To,
			LengthMeters: shot.DistanceMeters,
			Vector:       shot.Vector,
		}
		graph.Edges = append(graph.Edges, edge)
		graph.adjacency[edge.From] = append(graph.adjacency[edge.From], edge.Index)
		graph.adjacency[edge.To] = append(graph.adjacency[edge.To], edge.Index)
		names[edge.From] = true
		names[edge.To] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	graph.Stations = ordered
	return graph
}

// Station returns the reduced station record if it is known.
func (g Graph) Station(name string) (reduce.Station, bool) {
	station, ok := g.stations[name]
	return station, ok
}

// Incident returns the sorted edge indices touching a station.
func (g Graph) Incident(station string) []int {
	indices := g.adjacency[station]
	out := make([]int, len(indices))
	copy(out, indices)
	sort.Ints(out)
	return out
}

// Degree is the number of incident legs, counting parallel legs separately.
func (g Graph) Degree(station string) int { return len(g.adjacency[station]) }

// Neighbors returns the distinct adjacent stations in sorted order.
func (g Graph) Neighbors(station string) []string {
	seen := make(map[string]bool)
	for _, index := range g.adjacency[station] {
		seen[g.Edges[index].Other(station)] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TotalLength sums the length of every leg in the graph.
func (g Graph) TotalLength() float64 {
	total := 0.0
	for _, edge := range g.Edges {
		total += edge.LengthMeters
	}
	return total
}

// HasControl reports whether the station carries a control coordinate.
func (g Graph) HasControl(name string) bool {
	station, ok := g.stations[name]
	return ok && station.Fixed != nil
}

// Component is a connected part of the survey graph.
type Component struct {
	ID            int      `json:"id"`
	Anchor        string   `json:"anchor"`
	Stations      []string `json:"stations"`
	EdgeCount     int      `json:"edgeCount"`
	LengthMeters  float64  `json:"lengthMeters"`
	ControlPoints []string `json:"controlPoints,omitempty"`
	Anchored      bool     `json:"anchored"`
}

// Junction is a station where three or more distinct passages meet.
type Junction struct {
	Station  string   `json:"station"`
	Degree   int      `json:"degree"`
	Passages []string `json:"passages"`
}

// DeadEnd is a dangling passage: a station reached by exactly one leg that is
// neither an entrance nor a control point.
type DeadEnd struct {
	Station      string  `json:"station"`
	ViaShot      string  `json:"viaShot"`
	FromStation  string  `json:"fromStation"`
	LengthMeters float64 `json:"lengthMeters"`
	Entrance     bool    `json:"entrance"`
}

// DuplicateShot lists several legs measured between the same station pair.
type DuplicateShot struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Shots   []string `json:"shots"`
	SpreadM float64  `json:"lengthSpreadMeters"`
}

// NameCollision groups station names that differ only by case or padding.
type NameCollision struct {
	Normalized string   `json:"normalized"`
	Names      []string `json:"names"`
}

// Analysis is the complete topological description of the survey graph.
type Analysis struct {
	StationCount   int             `json:"stationCount"`
	EdgeCount      int             `json:"edgeCount"`
	TotalLength    float64         `json:"totalLengthMeters"`
	Components     []Component     `json:"components"`
	Junctions      []Junction      `json:"junctions"`
	DeadEnds       []DeadEnd       `json:"deadEnds"`
	Isolated       []string        `json:"isolatedStations"`
	Duplicates     []DuplicateShot `json:"duplicateShots"`
	NameCollisions []NameCollision `json:"nameCollisions"`
	Issues         model.Issues    `json:"issues,omitempty"`
}

// Analyze derives the topology of the graph.
func Analyze(graph Graph) Analysis {
	analysis := Analysis{
		StationCount: len(graph.Stations),
		EdgeCount:    len(graph.Edges),
		TotalLength:  graph.TotalLength(),
	}
	analysis.Components = components(graph)
	analysis.Junctions = junctions(graph)
	analysis.DeadEnds = deadEnds(graph)
	analysis.Isolated = isolated(graph)
	analysis.Duplicates = duplicateShots(graph)
	analysis.NameCollisions = nameCollisions(graph)
	analysis.Issues = topologyIssues(analysis).Sorted()
	return analysis
}

// components partitions the graph with a deterministic union find.
func components(graph Graph) []Component {
	parent := make(map[string]string, len(graph.Stations))
	for _, station := range graph.Stations {
		parent[station] = station
	}
	var find func(string) string
	find = func(node string) string {
		if parent[node] == node {
			return node
		}
		root := find(parent[node])
		parent[node] = root
		return root
	}
	union := func(a, b string) {
		rootA, rootB := find(a), find(b)
		if rootA == rootB {
			return
		}
		if rootA < rootB {
			parent[rootB] = rootA
			return
		}
		parent[rootA] = rootB
	}
	for _, edge := range graph.Edges {
		union(edge.From, edge.To)
	}
	grouped := make(map[string][]string)
	for _, station := range graph.Stations {
		root := find(station)
		grouped[root] = append(grouped[root], station)
	}
	roots := make([]string, 0, len(grouped))
	for root := range grouped {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	out := make([]Component, 0, len(roots))
	for position, root := range roots {
		members := grouped[root]
		sort.Strings(members)
		component := Component{ID: position + 1, Anchor: root, Stations: members}
		memberSet := make(map[string]bool, len(members))
		for _, member := range members {
			memberSet[member] = true
			if graph.HasControl(member) {
				component.ControlPoints = append(component.ControlPoints, member)
			}
		}
		for _, edge := range graph.Edges {
			if memberSet[edge.From] {
				component.EdgeCount++
				component.LengthMeters += edge.LengthMeters
			}
		}
		sort.Strings(component.ControlPoints)
		if len(component.ControlPoints) > 0 {
			component.Anchored = true
			component.Anchor = component.ControlPoints[0]
		} else if len(members) > 0 {
			component.Anchor = members[0]
		}
		out = append(out, component)
	}
	return out
}

// junctions lists the stations where at least three distinct passages meet.
func junctions(graph Graph) []Junction {
	out := make([]Junction, 0)
	for _, station := range graph.Stations {
		neighbors := graph.Neighbors(station)
		if len(neighbors) < 3 {
			continue
		}
		passages := make([]string, 0, len(graph.adjacency[station]))
		for _, index := range graph.Incident(station) {
			passages = append(passages, graph.Edges[index].Key())
		}
		sort.Strings(passages)
		out = append(out, Junction{Station: station, Degree: len(neighbors), Passages: passages})
	}
	return out
}

// deadEnds lists dangling passages.
func deadEnds(graph Graph) []DeadEnd {
	out := make([]DeadEnd, 0)
	for _, station := range graph.Stations {
		incident := graph.Incident(station)
		if len(incident) != 1 {
			continue
		}
		record, known := graph.Station(station)
		entrance := known && record.HasFlag(model.FlagEntrance)
		if known && record.Fixed != nil {
			continue
		}
		edge := graph.Edges[incident[0]]
		out = append(out, DeadEnd{
			Station:      station,
			ViaShot:      edge.Key(),
			FromStation:  edge.Other(station),
			LengthMeters: edge.LengthMeters,
			Entrance:     entrance,
		})
	}
	return out
}

// isolated lists stations that no leg touches.
func isolated(graph Graph) []string {
	out := make([]string, 0)
	for _, station := range graph.Stations {
		if graph.Degree(station) == 0 {
			out = append(out, station)
		}
	}
	return out
}

// duplicateShots groups parallel legs between the same station pair.
func duplicateShots(graph Graph) []DuplicateShot {
	grouped := make(map[string][]int)
	for _, edge := range graph.Edges {
		grouped[pairKey(edge.From, edge.To)] = append(grouped[pairKey(edge.From, edge.To)], edge.Index)
	}
	keys := make([]string, 0, len(grouped))
	for key, indices := range grouped {
		if len(indices) > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]DuplicateShot, 0, len(keys))
	for _, key := range keys {
		indices := grouped[key]
		sort.Ints(indices)
		shots := make([]string, 0, len(indices))
		minimum := graph.Edges[indices[0]].LengthMeters
		maximum := minimum
		for _, index := range indices {
			edge := graph.Edges[index]
			shots = append(shots, edge.Key())
			if edge.LengthMeters < minimum {
				minimum = edge.LengthMeters
			}
			if edge.LengthMeters > maximum {
				maximum = edge.LengthMeters
			}
		}
		sort.Strings(shots)
		parts := strings.SplitN(key, "\x00", 2)
		duplicate := DuplicateShot{Shots: shots, SpreadM: maximum - minimum}
		if len(parts) == 2 {
			duplicate.From, duplicate.To = parts[0], parts[1]
		}
		out = append(out, duplicate)
	}
	return out
}

// nameCollisions groups station names that only differ by case or padding.
func nameCollisions(graph Graph) []NameCollision {
	grouped := make(map[string][]string)
	for _, station := range graph.Stations {
		normalized := strings.ToLower(strings.TrimSpace(station))
		grouped[normalized] = append(grouped[normalized], station)
	}
	keys := make([]string, 0, len(grouped))
	for key, names := range grouped {
		if len(names) > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]NameCollision, 0, len(keys))
	for _, key := range keys {
		names := grouped[key]
		sort.Strings(names)
		out = append(out, NameCollision{Normalized: key, Names: names})
	}
	return out
}

// topologyIssues converts notable topology facts into reportable findings.
func topologyIssues(analysis Analysis) model.Issues {
	issues := model.Issues{}
	if len(analysis.Components) > 1 {
		issues = append(issues, model.Issue{
			Severity: model.SeverityWarning,
			Code:     "network-disconnected",
			Path:     "network.components",
			Message:  fmt.Sprintf("the survey splits into %d disconnected components", len(analysis.Components)),
		})
	}
	for _, component := range analysis.Components {
		if component.Anchored {
			continue
		}
		issues = append(issues, model.Issue{
			Severity: model.SeverityWarning,
			Code:     "component-unanchored",
			Path:     fmt.Sprintf("network.components[%d]", component.ID),
			Message: fmt.Sprintf("component anchored at %q has no control station, coordinates are relative to a synthetic origin (%s m of passage)",
				component.Anchor, units.Format(component.LengthMeters, 3)),
		})
	}
	for _, station := range analysis.Isolated {
		issues = append(issues, model.Issue{
			Severity: model.SeverityWarning,
			Code:     "station-isolated",
			Path:     "network.stations[" + station + "]",
			Message:  fmt.Sprintf("station %q is declared but no leg reaches it", station),
		})
	}
	for _, collision := range analysis.NameCollisions {
		issues = append(issues, model.Issue{
			Severity: model.SeverityWarning,
			Code:     "station-name-collision",
			Path:     "network.stations[" + collision.Normalized + "]",
			Message:  fmt.Sprintf("station names %s differ only by case or padding", strings.Join(collision.Names, ", ")),
		})
	}
	for _, duplicate := range analysis.Duplicates {
		issues = append(issues, model.Issue{
			Severity: model.SeverityWarning,
			Code:     "duplicate-leg",
			Path:     "network.pairs[" + duplicate.From + "-" + duplicate.To + "]",
			Message: fmt.Sprintf("legs %s connect the same pair of stations, lengths spread by %s m",
				strings.Join(duplicate.Shots, ", "), units.Format(duplicate.SpreadM, 3)),
		})
	}
	return issues
}

// pairKey builds the canonical unordered pair key of two stations.
func pairKey(a, b string) string {
	if a <= b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}
