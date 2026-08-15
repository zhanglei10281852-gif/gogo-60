// Package adjust distributes loop closure error across the legs of a survey.
//
// The adjustment is a deterministic iterative proportional distribution: for
// every independent loop the residual closure vector is shared out over the legs
// of that loop in proportion to their tape length, and the passes repeat until
// the largest residual falls under the convergence threshold. Loops that share
// legs therefore settle together without any random or order dependent choice,
// because loops are always visited in identifier order.
//
// Tape lengths are never modified. Only the displacement vectors, and therefore
// the station coordinates, move.
package adjust

import (
	"fmt"
	"sort"

	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/loops"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/units"
)

// ShotAdjustment is the correction applied to a single leg.
type ShotAdjustment struct {
	EdgeIndex       int         `json:"edgeIndex"`
	ShotKey         string      `json:"shot"`
	From            string      `json:"from"`
	To              string      `json:"to"`
	LengthMeters    float64     `json:"lengthMeters"`
	Correction      geom.Vector `json:"correction"`
	MagnitudeMeters float64     `json:"magnitudeMeters"`
	RelativePPM     float64     `json:"relativePpm"`
	AzimuthShiftDeg float64     `json:"azimuthShiftDeg"`
	Loops           []string    `json:"loops,omitempty"`
}

// LoopResidual records the closure of one loop before and after adjustment.
type LoopResidual struct {
	LoopID       string  `json:"loopId"`
	BeforeMeters float64 `json:"beforeMeters"`
	AfterMeters  float64 `json:"afterMeters"`
	BeforePPM    float64 `json:"beforePpm"`
	AfterPPM     float64 `json:"afterPpm"`
}

// Result is the outcome of an adjustment run.
type Result struct {
	Enabled                bool             `json:"enabled"`
	Passes                 int              `json:"passes"`
	Converged              bool             `json:"converged"`
	MaxResidualMeters      float64          `json:"maxResidualMeters"`
	TotalCorrectionMeters  float64          `json:"totalCorrectionMeters"`
	AdjustedLegs           int              `json:"adjustedLegs"`
	WorstShot              string           `json:"worstShot,omitempty"`
	WorstMagnitudeMeters   float64          `json:"worstMagnitudeMeters"`
	Adjustments            []ShotAdjustment `json:"adjustments"`
	Residuals              []LoopResidual   `json:"residuals"`
	VerticalDistributed    bool             `json:"verticalDistributed"`
	Vectors                []geom.Vector    `json:"-"`
	Issues                 model.Issues     `json:"issues,omitempty"`
	UnadjustableLoopCount  int              `json:"unadjustableLoopCount"`
	UnadjustableTotalError float64          `json:"unadjustableTotalErrorMeters"`
}

// negligible is the magnitude below which a correction is not worth reporting.
const negligible = 1e-9

// Apply distributes the closure error of every loop over its legs.
func Apply(graph network.Graph, loopResult loops.Result, settings config.Adjustment) (Result, error) {
	result := Result{
		Enabled:             settings.Enabled,
		VerticalDistributed: settings.AdjustVertical,
		Vectors:             make([]geom.Vector, len(graph.Edges)),
	}
	for index, edge := range graph.Edges {
		result.Vectors[index] = edge.Vector
	}
	if !settings.Enabled || len(loopResult.Loops) == 0 {
		result.Converged = true
		result.Adjustments = []ShotAdjustment{}
		result.Residuals = []LoopResidual{}
		return result, nil
	}
	if settings.MaxPasses < 1 {
		return Result{}, fmt.Errorf("adjustment requires at least one pass, got %d", settings.MaxPasses)
	}
	corrections := make([]geom.Vector, len(graph.Edges))
	ordered := make([]loops.Loop, len(loopResult.Loops))
	copy(ordered, loopResult.Loops)
	sort.SliceStable(ordered, func(a, b int) bool { return ordered[a].ID < ordered[b].ID })

	before := make(map[string]float64, len(ordered))
	beforePPM := make(map[string]float64, len(ordered))
	for _, loop := range ordered {
		closure := loopClosure(graph, loop, corrections)
		before[loop.ID] = closure.Length()
		beforePPM[loop.ID] = units.PartsPerMillion(closure.Length(), loop.LengthMeters)
	}

	loopsByEdge := make(map[int][]string)
	for _, loop := range ordered {
		for _, leg := range loop.Legs {
			loopsByEdge[leg.EdgeIndex] = append(loopsByEdge[leg.EdgeIndex], loop.ID)
		}
	}

	maxResidual := 0.0
	for pass := 1; pass <= settings.MaxPasses; pass++ {
		result.Passes = pass
		maxResidual = 0
		for _, loop := range ordered {
			closure := loopClosure(graph, loop, corrections)
			if !settings.AdjustVertical {
				closure.Up = 0
			}
			magnitude := closure.Length()
			if magnitude > maxResidual {
				maxResidual = magnitude
			}
			if magnitude <= negligible {
				continue
			}
			totalWeight := 0.0
			for _, leg := range loop.Legs {
				totalWeight += legWeight(leg.LengthMeters, settings.MinShotWeight)
			}
			if totalWeight <= 0 {
				continue
			}
			for _, leg := range loop.Legs {
				share := legWeight(leg.LengthMeters, settings.MinShotWeight) / totalWeight
				delta := closure.Scale(-share)
				corrections[leg.EdgeIndex] = corrections[leg.EdgeIndex].Add(delta)
			}
		}
		if maxResidual <= settings.Convergence {
			result.Converged = true
			break
		}
	}
	result.MaxResidualMeters = maxResidual

	for index := range result.Vectors {
		result.Vectors[index] = graph.Edges[index].Vector.Add(corrections[index])
	}
	result.Adjustments = buildAdjustments(graph, corrections, loopsByEdge)
	for _, adjustment := range result.Adjustments {
		result.TotalCorrectionMeters += adjustment.MagnitudeMeters
		if adjustment.MagnitudeMeters > result.WorstMagnitudeMeters {
			result.WorstMagnitudeMeters = adjustment.MagnitudeMeters
			result.WorstShot = adjustment.ShotKey
		}
	}
	result.AdjustedLegs = len(result.Adjustments)
	result.Residuals = make([]LoopResidual, 0, len(ordered))
	issues := model.Issues{}
	for _, loop := range ordered {
		closure := loopClosure(graph, loop, corrections)
		residual := LoopResidual{
			LoopID:       loop.ID,
			BeforeMeters: before[loop.ID],
			AfterMeters:  closure.Length(),
			BeforePPM:    beforePPM[loop.ID],
			AfterPPM:     units.PartsPerMillion(closure.Length(), loop.LengthMeters),
		}
		result.Residuals = append(result.Residuals, residual)
		if residual.AfterMeters > settings.Convergence*10 {
			result.UnadjustableLoopCount++
			result.UnadjustableTotalError += residual.AfterMeters
			issues = append(issues, model.Issue{
				Severity: model.SeverityWarning,
				Code:     "adjustment-residual",
				Path:     "adjust.loops[" + loop.ID + "]",
				Message: fmt.Sprintf("loop %s still closes to %s m after %d passes",
					loop.ID, units.Format(residual.AfterMeters, 5), result.Passes),
			})
		}
	}
	if !result.Converged {
		issues = append(issues, model.Issue{
			Severity: model.SeverityWarning,
			Code:     "adjustment-not-converged",
			Path:     "adjust",
			Message: fmt.Sprintf("adjustment stopped after %d passes with a largest residual of %s m",
				result.Passes, units.Format(result.MaxResidualMeters, 5)),
		})
	}
	result.Issues = issues.Sorted()
	return result, nil
}

// legWeight returns the distribution weight of a leg, never below the floor.
func legWeight(lengthMeters, minimum float64) float64 {
	if lengthMeters < minimum {
		return minimum
	}
	return lengthMeters
}

// loopClosure recomputes the closure of a loop with the current corrections.
func loopClosure(graph network.Graph, loop loops.Loop, corrections []geom.Vector) geom.Vector {
	closure := geom.Zero()
	for _, leg := range loop.Legs {
		vector := graph.Edges[leg.EdgeIndex].Vector.Add(corrections[leg.EdgeIndex])
		if leg.Reversed {
			vector = vector.Negate()
		}
		closure = closure.Add(vector)
	}
	return closure
}

// buildAdjustments turns the correction table into a sorted report.
func buildAdjustments(graph network.Graph, corrections []geom.Vector, loopsByEdge map[int][]string) []ShotAdjustment {
	out := make([]ShotAdjustment, 0, len(corrections))
	for index, correction := range corrections {
		magnitude := correction.Length()
		if magnitude <= negligible {
			continue
		}
		edge := graph.Edges[index]
		adjusted := edge.Vector.Add(correction)
		membership := loopsByEdge[index]
		unique := make([]string, 0, len(membership))
		seen := make(map[string]bool, len(membership))
		for _, id := range membership {
			if seen[id] {
				continue
			}
			seen[id] = true
			unique = append(unique, id)
		}
		sort.Strings(unique)
		out = append(out, ShotAdjustment{
			EdgeIndex:       index,
			ShotKey:         edge.Key(),
			From:            edge.From,
			To:              edge.To,
			LengthMeters:    edge.LengthMeters,
			Correction:      correction,
			MagnitudeMeters: magnitude,
			RelativePPM:     units.PartsPerMillion(magnitude, edge.LengthMeters),
			AzimuthShiftDeg: units.AzimuthDelta(edge.Vector.Azimuth(), adjusted.Azimuth()),
			Loops:           unique,
		})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].ShotKey < out[b].ShotKey })
	return out
}
