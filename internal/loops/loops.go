// Package loops derives the independent loop basis of a cave survey and
// measures how well each loop closes.
//
// The basis is the classical fundamental cycle basis: every leg that is not part
// of the shortest path spanning forest closes exactly one independent loop
// together with the tree path between its endpoints. The number of loops equals
// the cyclomatic number of the graph, so the basis is neither redundant nor
// incomplete.
package loops

import (
	"fmt"
	"sort"

	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/traverse"
	"CaveLoop/internal/units"
)

// Leg is one traversal of a survey edge inside a loop circuit.
type Leg struct {
	EdgeIndex    int         `json:"edgeIndex"`
	ShotKey      string      `json:"shot"`
	From         string      `json:"from"`
	To           string      `json:"to"`
	Reversed     bool        `json:"reversed"`
	LengthMeters float64     `json:"lengthMeters"`
	Vector       geom.Vector `json:"vector"`
	Chord        bool        `json:"chord"`
}

// Loop is one independent circuit with its closure error.
type Loop struct {
	ID                    string      `json:"id"`
	Component             int         `json:"component"`
	ChordShot             string      `json:"chordShot"`
	Stations              []string    `json:"stations"`
	Legs                  []Leg       `json:"legs"`
	LengthMeters          float64     `json:"lengthMeters"`
	Closure               geom.Vector `json:"closure"`
	HorizontalErrorMeters float64     `json:"horizontalErrorMeters"`
	VerticalErrorMeters   float64     `json:"verticalErrorMeters"`
	TotalErrorMeters      float64     `json:"totalErrorMeters"`
	ErrorPPM              float64     `json:"errorPpm"`
	WithinTolerance       bool        `json:"withinTolerance"`
	Failures              []string    `json:"failures,omitempty"`
}

// EdgeIndices lists the graph edges used by the loop in ascending order.
func (l Loop) EdgeIndices() []int {
	out := make([]int, 0, len(l.Legs))
	for _, leg := range l.Legs {
		out = append(out, leg.EdgeIndex)
	}
	sort.Ints(out)
	return out
}

// Result is the loop analysis of a survey.
type Result struct {
	Loops            []Loop       `json:"loops"`
	IndependentCount int          `json:"independentCount"`
	WorstLoop        string       `json:"worstLoop,omitempty"`
	WorstErrorMeters float64      `json:"worstErrorMeters"`
	FailingCount     int          `json:"failingCount"`
	Issues           model.Issues `json:"issues,omitempty"`
}

// LoopByID looks up a loop by identifier.
func (r Result) LoopByID(id string) (Loop, bool) {
	for _, loop := range r.Loops {
		if loop.ID == id {
			return loop, true
		}
	}
	return Loop{}, false
}

// treeLink is the parent relation of one station inside the spanning forest.
type treeLink struct {
	parent    string
	edgeIndex int
}

// Detect builds the loop basis and evaluates every closure against the
// configured tolerances. When vectors is non nil it overrides the edge
// displacements, allowing the same analysis to run on an adjusted network.
func Detect(graph network.Graph, analysis network.Analysis, layout traverse.Result, tolerances config.Tolerances, vectors []geom.Vector) (Result, error) {
	if vectors != nil && len(vectors) != len(graph.Edges) {
		return Result{}, fmt.Errorf("displacement override has %d entries but the graph has %d legs", len(vectors), len(graph.Edges))
	}
	links, err := buildTree(graph, layout)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		IndependentCount: len(graph.Edges) - len(graph.Stations) + len(analysis.Components),
	}
	if result.IndependentCount < 0 {
		result.IndependentCount = 0
	}
	displacement := func(edgeIndex int) geom.Vector {
		if vectors != nil {
			return vectors[edgeIndex]
		}
		return graph.Edges[edgeIndex].Vector
	}
	chords := layout.ChordIndices()
	built := make([]Loop, 0, len(chords))
	for _, chordIndex := range chords {
		chord := graph.Edges[chordIndex]
		loop, err := buildLoop(graph, links, chord, displacement)
		if err != nil {
			return Result{}, err
		}
		if position, ok := layout.Position(chord.From); ok {
			loop.Component = position.Component
		}
		built = append(built, loop)
	}
	sort.SliceStable(built, func(a, b int) bool { return built[a].ChordShot < built[b].ChordShot })
	issues := model.Issues{}
	for position := range built {
		built[position].ID = fmt.Sprintf("L%03d", position+1)
		evaluate(&built[position], tolerances)
		if !built[position].WithinTolerance {
			result.FailingCount++
			issues = append(issues, model.Issue{
				Severity: model.SeverityWarning,
				Code:     "loop-closure-out-of-tolerance",
				Path:     "loops[" + built[position].ID + "]",
				Message: fmt.Sprintf("loop %s over %s m of passage closes to %s m (%s ppm): %s",
					built[position].ID,
					units.Format(built[position].LengthMeters, 3),
					units.Format(built[position].TotalErrorMeters, 4),
					units.Format(built[position].ErrorPPM, 1),
					joinFailures(built[position].Failures)),
			})
		}
		if built[position].TotalErrorMeters > result.WorstErrorMeters {
			result.WorstErrorMeters = built[position].TotalErrorMeters
			result.WorstLoop = built[position].ID
		}
	}
	result.Loops = built
	result.Issues = issues.Sorted()
	return result, nil
}

// buildTree derives the parent relation of the spanning forest from the layout.
func buildTree(graph network.Graph, layout traverse.Result) (map[string]treeLink, error) {
	byKey := make(map[string]int, len(graph.Edges))
	for _, edge := range graph.Edges {
		byKey[edge.Key()] = edge.Index
	}
	links := make(map[string]treeLink, len(layout.Positions))
	for _, position := range layout.Positions {
		if position.ViaShot == "" {
			continue
		}
		edgeIndex, ok := byKey[position.ViaShot]
		if !ok {
			return nil, fmt.Errorf("spanning tree references unknown leg %q", position.ViaShot)
		}
		links[position.Station] = treeLink{parent: position.Parent, edgeIndex: edgeIndex}
	}
	stations := make([]string, 0, len(links))
	for station := range links {
		stations = append(stations, station)
	}
	sort.Strings(stations)
	for _, station := range stations {
		if _, err := ancestry(links, station); err != nil {
			return nil, err
		}
	}
	return links, nil
}

// buildLoop assembles the circuit closed by one chord.
func buildLoop(graph network.Graph, links map[string]treeLink, chord network.Edge, displacement func(int) geom.Vector) (Loop, error) {
	loop := Loop{ChordShot: chord.Key()}
	loop.Legs = append(loop.Legs, Leg{
		EdgeIndex:    chord.Index,
		ShotKey:      chord.Key(),
		From:         chord.From,
		To:           chord.To,
		LengthMeters: chord.LengthMeters,
		Vector:       displacement(chord.Index),
		Chord:        true,
	})
	path, err := treePath(graph, links, chord.To, chord.From, displacement)
	if err != nil {
		return Loop{}, fmt.Errorf("loop through %s: %w", chord.Key(), err)
	}
	loop.Legs = append(loop.Legs, path...)
	loop.Stations = append(loop.Stations, chord.From)
	for _, leg := range loop.Legs {
		loop.Stations = append(loop.Stations, leg.To)
		loop.LengthMeters += leg.LengthMeters
	}
	closure := geom.Zero()
	for _, leg := range loop.Legs {
		closure = closure.Add(leg.Vector)
	}
	loop.Closure = closure
	loop.HorizontalErrorMeters = closure.HorizontalLength()
	loop.VerticalErrorMeters = closure.VerticalLength()
	loop.TotalErrorMeters = closure.Length()
	loop.ErrorPPM = units.PartsPerMillion(loop.TotalErrorMeters, loop.LengthMeters)
	return loop, nil
}

// treePath walks the spanning forest from one station to another and returns the
// directed legs of that walk.
func treePath(graph network.Graph, links map[string]treeLink, from, to string, displacement func(int) geom.Vector) ([]Leg, error) {
	if from == to {
		return nil, nil
	}
	fromChain, err := ancestry(links, from)
	if err != nil {
		return nil, err
	}
	toChain, err := ancestry(links, to)
	if err != nil {
		return nil, err
	}
	inToChain := make(map[string]int, len(toChain))
	for position, station := range toChain {
		inToChain[station] = position
	}
	meetIndex := -1
	meetPosition := -1
	for position, station := range fromChain {
		if other, ok := inToChain[station]; ok {
			meetIndex = position
			meetPosition = other
			break
		}
	}
	if meetIndex < 0 {
		return nil, fmt.Errorf("stations %q and %q are not connected in the spanning forest", from, to)
	}
	legs := make([]Leg, 0, meetIndex+meetPosition)
	for position := 0; position < meetIndex; position++ {
		station := fromChain[position]
		link := links[station]
		legs = append(legs, directedLeg(graph, link.edgeIndex, station, link.parent, displacement))
	}
	for position := meetPosition - 1; position >= 0; position-- {
		station := toChain[position]
		link := links[station]
		legs = append(legs, directedLeg(graph, link.edgeIndex, link.parent, station, displacement))
	}
	return legs, nil
}

// ancestry lists a station and all of its ancestors up to the component anchor.
func ancestry(links map[string]treeLink, station string) ([]string, error) {
	chain := []string{station}
	cursor := station
	for step := 0; step <= len(links)+1; step++ {
		link, ok := links[cursor]
		if !ok {
			return chain, nil
		}
		cursor = link.parent
		chain = append(chain, cursor)
	}
	return nil, fmt.Errorf("spanning forest walk from %q does not terminate", station)
}

// directedLeg builds a leg for traversing an edge from one station to another.
func directedLeg(graph network.Graph, edgeIndex int, from, to string, displacement func(int) geom.Vector) Leg {
	edge := graph.Edges[edgeIndex]
	vector := displacement(edgeIndex)
	reversed := from != edge.From
	if reversed {
		vector = vector.Negate()
	}
	return Leg{
		EdgeIndex:    edgeIndex,
		ShotKey:      edge.Key(),
		From:         from,
		To:           to,
		Reversed:     reversed,
		LengthMeters: edge.LengthMeters,
		Vector:       vector,
	}
}

// evaluate classifies a loop against the closure tolerances.
func evaluate(loop *Loop, tolerances config.Tolerances) {
	loop.Failures = nil
	if loop.TotalErrorMeters > tolerances.LoopClosureMeters {
		loop.Failures = append(loop.Failures, "total-error")
	}
	if loop.VerticalErrorMeters > tolerances.VerticalClosureMeters {
		loop.Failures = append(loop.Failures, "vertical-error")
	}
	if loop.ErrorPPM > tolerances.LoopClosurePPM {
		loop.Failures = append(loop.Failures, "relative-error")
	}
	sort.Strings(loop.Failures)
	loop.WithinTolerance = len(loop.Failures) == 0
}

// joinFailures renders the failure codes of a loop.
func joinFailures(failures []string) string {
	if len(failures) == 0 {
		return "within tolerance"
	}
	out := failures[0]
	for _, failure := range failures[1:] {
		out += ", " + failure
	}
	return out
}
