// Package traverse converts the survey graph into three dimensional station
// coordinates.
//
// Each connected component is anchored on a control station when one exists and
// on a synthetic origin otherwise. Positions are propagated along a shortest
// path spanning tree so that a station is always reached through the least
// amount of accumulated tape, which keeps the traverse both stable and
// physically sensible.
package traverse

import (
	"fmt"
	"math"
	"sort"

	"CaveLoop/internal/geom"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/units"
)

// Position is the computed location of one station.
type Position struct {
	Station          string      `json:"station"`
	Coordinate       geom.Vector `json:"coordinate"`
	Component        int         `json:"component"`
	Parent           string      `json:"parent,omitempty"`
	ViaShot          string      `json:"viaShot,omitempty"`
	PathLengthMeters float64     `json:"pathLengthMeters"`
	DepthMeters      float64     `json:"depthMeters"`
	Control          bool        `json:"control"`
	Anchor           bool        `json:"anchor"`
}

// ControlResidual is the mismatch between a computed position and a control
// coordinate that was not used as the component anchor.
type ControlResidual struct {
	Station          string      `json:"station"`
	Component        int         `json:"component"`
	Residual         geom.Vector `json:"residual"`
	HorizontalMeters float64     `json:"horizontalMeters"`
	VerticalMeters   float64     `json:"verticalMeters"`
	TotalMeters      float64     `json:"totalMeters"`
}

// Result is the outcome of a traverse computation.
type Result struct {
	Positions        []Position        `json:"positions"`
	TreeEdges        []string          `json:"treeEdges"`
	ChordEdges       []string          `json:"chordEdges"`
	ControlResiduals []ControlResidual `json:"controlResiduals,omitempty"`
	DatumUpMeters    map[int]float64   `json:"datumUpMeters"`
	Issues           model.Issues      `json:"issues,omitempty"`

	treeEdgeSet  map[int]bool
	chordIndices []int
	byStation    map[string]int
}

// Position looks up the computed position of a station.
func (r Result) Position(station string) (Position, bool) {
	index, ok := r.byStation[station]
	if !ok {
		return Position{}, false
	}
	return r.Positions[index], true
}

// Coordinate returns the coordinate of a station or the zero vector.
func (r Result) Coordinate(station string) geom.Vector {
	position, ok := r.Position(station)
	if !ok {
		return geom.Zero()
	}
	return position.Coordinate
}

// IsTreeEdge reports whether the edge belongs to the spanning forest.
func (r Result) IsTreeEdge(index int) bool { return r.treeEdgeSet[index] }

// ChordIndices lists the edges that are not part of the spanning forest, in
// ascending order. Each chord closes exactly one independent loop.
func (r Result) ChordIndices() []int {
	out := make([]int, len(r.chordIndices))
	copy(out, r.chordIndices)
	return out
}

// Coordinates returns every coordinate in station order.
func (r Result) Coordinates() []geom.Vector {
	out := make([]geom.Vector, 0, len(r.Positions))
	for _, position := range r.Positions {
		out = append(out, position.Coordinate)
	}
	return out
}

// Compute lays out the graph. When vectors is non nil it overrides the edge
// displacements, which is how the adjusted network is recomputed after closure
// error has been distributed.
func Compute(graph network.Graph, analysis network.Analysis, vectors []geom.Vector) (Result, error) {
	if vectors != nil && len(vectors) != len(graph.Edges) {
		return Result{}, fmt.Errorf("displacement override has %d entries but the graph has %d legs", len(vectors), len(graph.Edges))
	}
	result := Result{
		treeEdgeSet:   make(map[int]bool, len(graph.Edges)),
		byStation:     make(map[string]int, len(graph.Stations)),
		DatumUpMeters: make(map[int]float64),
	}
	displacement := func(edge network.Edge, from string) geom.Vector {
		vector := edge.Vector
		if vectors != nil {
			vector = vectors[edge.Index]
		}
		if from == edge.From {
			return vector
		}
		return vector.Negate()
	}
	positions := make(map[string]Position, len(graph.Stations))
	issues := model.Issues{}
	for _, component := range analysis.Components {
		componentPositions, componentIssues := layoutComponent(graph, component, displacement)
		issues = append(issues, componentIssues...)
		for name, position := range componentPositions {
			positions[name] = position
		}
	}
	names := make([]string, 0, len(positions))
	for name := range positions {
		names = append(names, name)
	}
	sort.Strings(names)
	result.Positions = make([]Position, 0, len(names))
	for _, name := range names {
		result.byStation[name] = len(result.Positions)
		result.Positions = append(result.Positions, positions[name])
	}
	for _, component := range analysis.Components {
		datum := math.Inf(-1)
		for _, station := range component.Stations {
			position, ok := positions[station]
			if !ok {
				continue
			}
			if position.Coordinate.Up > datum {
				datum = position.Coordinate.Up
			}
		}
		if math.IsInf(datum, -1) {
			datum = 0
		}
		result.DatumUpMeters[component.ID] = datum
	}
	for index := range result.Positions {
		datum := result.DatumUpMeters[result.Positions[index].Component]
		result.Positions[index].DepthMeters = datum - result.Positions[index].Coordinate.Up
	}
	for _, position := range result.Positions {
		if position.ViaShot != "" {
			result.TreeEdges = append(result.TreeEdges, position.ViaShot)
		}
	}
	sort.Strings(result.TreeEdges)
	treeKeys := make(map[string]bool, len(result.TreeEdges))
	for _, key := range result.TreeEdges {
		treeKeys[key] = true
	}
	for _, edge := range graph.Edges {
		if treeKeys[edge.Key()] {
			result.treeEdgeSet[edge.Index] = true
			continue
		}
		result.chordIndices = append(result.chordIndices, edge.Index)
		result.ChordEdges = append(result.ChordEdges, edge.Key())
	}
	sort.Ints(result.chordIndices)
	sort.Strings(result.ChordEdges)
	result.ControlResiduals = controlResiduals(graph, analysis, positions)
	for _, residual := range result.ControlResiduals {
		issues = append(issues, model.Issue{
			Severity: model.SeverityWarning,
			Code:     "control-residual",
			Path:     "traverse.control[" + residual.Station + "]",
			Message: fmt.Sprintf("computed position of control station %q differs from its given coordinate by %s m",
				residual.Station, units.Format(residual.TotalMeters, 4)),
		})
	}
	result.Issues = issues.Sorted()
	return result, nil
}

// layoutComponent positions every station of one component.
func layoutComponent(graph network.Graph, component network.Component, displacement func(network.Edge, string) geom.Vector) (map[string]Position, model.Issues) {
	issues := model.Issues{}
	positions := make(map[string]Position, len(component.Stations))
	if len(component.Stations) == 0 {
		return positions, issues
	}
	anchor := component.Anchor
	anchorCoordinate := geom.Zero()
	if station, ok := graph.Station(anchor); ok && station.Fixed != nil {
		anchorCoordinate = *station.Fixed
	}
	settled := make(map[string]bool, len(component.Stations))
	best := make(map[string]float64, len(component.Stations))
	for _, station := range component.Stations {
		best[station] = math.Inf(1)
	}
	best[anchor] = 0
	positions[anchor] = Position{
		Station:    anchor,
		Coordinate: anchorCoordinate,
		Component:  component.ID,
		Control:    graph.HasControl(anchor),
		Anchor:     true,
	}
	for range component.Stations {
		current := ""
		currentBest := math.Inf(1)
		for _, station := range component.Stations {
			if settled[station] {
				continue
			}
			distance := best[station]
			if math.IsInf(distance, 1) {
				continue
			}
			if distance < currentBest-1e-12 || (units.NearlyEqual(distance, currentBest, 1e-12) && (current == "" || station < current)) {
				current = station
				currentBest = distance
			}
		}
		if current == "" {
			break
		}
		settled[current] = true
		base := positions[current]
		for _, index := range graph.Incident(current) {
			edge := graph.Edges[index]
			neighbor := edge.Other(current)
			if settled[neighbor] {
				continue
			}
			candidate := base.PathLengthMeters + edge.LengthMeters
			existing, known := positions[neighbor]
			better := candidate < best[neighbor]-1e-9
			tie := units.NearlyEqual(candidate, best[neighbor], 1e-9) && known && edge.Key() < existing.ViaShot
			if !better && !tie {
				continue
			}
			best[neighbor] = candidate
			positions[neighbor] = Position{
				Station:          neighbor,
				Coordinate:       base.Coordinate.Add(displacement(edge, current)),
				Component:        component.ID,
				Parent:           current,
				ViaShot:          edge.Key(),
				PathLengthMeters: candidate,
				Control:          graph.HasControl(neighbor),
			}
		}
	}
	for _, station := range component.Stations {
		if settled[station] {
			continue
		}
		issues = append(issues, model.Issue{
			Severity: model.SeverityWarning,
			Code:     "station-unreachable",
			Path:     "traverse.stations[" + station + "]",
			Message:  fmt.Sprintf("station %q could not be reached from anchor %q", station, anchor),
		})
		positions[station] = Position{Station: station, Component: component.ID, Coordinate: geom.Zero()}
	}
	return positions, issues
}

// controlResiduals compares computed positions against non anchor controls.
func controlResiduals(graph network.Graph, analysis network.Analysis, positions map[string]Position) []ControlResidual {
	out := make([]ControlResidual, 0)
	for _, component := range analysis.Components {
		for _, name := range component.ControlPoints {
			if name == component.Anchor {
				continue
			}
			station, ok := graph.Station(name)
			if !ok || station.Fixed == nil {
				continue
			}
			position, ok := positions[name]
			if !ok {
				continue
			}
			residual := station.Fixed.Sub(position.Coordinate)
			out = append(out, ControlResidual{
				Station:          name,
				Component:        component.ID,
				Residual:         residual,
				HorizontalMeters: residual.HorizontalLength(),
				VerticalMeters:   residual.VerticalLength(),
				TotalMeters:      residual.Length(),
			})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Station < out[b].Station })
	return out
}
