// Package blunder implements heuristic detection of the gross errors that
// typically spoil a cave survey.
//
// Four families of blunder are covered:
//
//   - a reversed reading, where a backsight was written down as if it were a
//     foresight, which shows up as a reciprocal disagreement close to 180 deg;
//   - transposed digits in a compass bearing, tested by re-closing every failing
//     loop with each plausible digit swap of each candidate leg;
//   - a gross tape length outlier inside a trip, measured in standard
//     deviations from the trip mean;
//   - a loop that closes outside the configured tolerance, reported together
//     with the share of the error that each leg would have to absorb.
//
// Every detector is deterministic: candidate legs are ranked by length and then
// by identifier, and the reported findings are sorted before being returned.
package blunder

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/loops"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/reduce"
	"CaveLoop/internal/units"
)

// Finding codes.
const (
	CodeReversedReading  = "reversed-reading"
	CodeReversedLeg      = "reversed-leg"
	CodeTransposedDigits = "transposed-azimuth-digits"
	CodeLengthOutlier    = "gross-length-outlier"
	CodeLoopClosure      = "loop-closure-exceeded"
	CodeVerticalDominant = "vertical-error-dominant"
)

// Finding is one suspected blunder.
type Finding struct {
	Code       string   `json:"code"`
	Severity   string   `json:"severity"`
	Subject    string   `json:"subject"`
	Loop       string   `json:"loop,omitempty"`
	Trip       string   `json:"trip,omitempty"`
	Score      float64  `json:"score"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
}

// Result is the outcome of blunder detection.
type Result struct {
	Enabled  bool           `json:"enabled"`
	Findings []Finding      `json:"findings"`
	Counts   map[string]int `json:"counts"`
	Issues   model.Issues   `json:"issues,omitempty"`
}

// Detect runs every enabled heuristic over the survey.
func Detect(reduced reduce.Result, graph network.Graph, loopResult loops.Result, cfg config.Config) Result {
	result := Result{Enabled: cfg.Blunders.Enabled, Counts: map[string]int{}}
	if !cfg.Blunders.Enabled {
		result.Findings = []Finding{}
		return result
	}
	shots := indexShots(reduced)
	findings := make([]Finding, 0, 16)
	findings = append(findings, reversedReadings(reduced, cfg.Blunders)...)
	findings = append(findings, lengthOutliers(reduced, cfg.Blunders)...)
	findings = append(findings, loopFindings(graph, loopResult, shots, cfg)...)
	sort.SliceStable(findings, func(a, b int) bool {
		left, right := findings[a], findings[b]
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Subject != right.Subject {
			return left.Subject < right.Subject
		}
		if left.Loop != right.Loop {
			return left.Loop < right.Loop
		}
		return left.Message < right.Message
	})
	issues := model.Issues{}
	for _, finding := range findings {
		result.Counts[finding.Code]++
		issues = append(issues, model.Issue{
			Severity: finding.Severity,
			Code:     finding.Code,
			Path:     "blunders[" + finding.Subject + "]",
			Message:  finding.Message,
		})
	}
	result.Findings = findings
	result.Issues = issues.Sorted()
	return result
}

// indexShots maps shot keys to their reduced measurement.
func indexShots(reduced reduce.Result) map[string]reduce.Shot {
	index := make(map[string]reduce.Shot, len(reduced.Shots))
	for _, shot := range reduced.Shots {
		index[shot.Key()] = shot
	}
	return index
}

// reversedReadings flags shots whose reciprocal reading looks like a foresight.
func reversedReadings(reduced reduce.Result, settings config.Blunders) []Finding {
	findings := make([]Finding, 0)
	for _, shot := range reduced.ActiveShots() {
		reconciliation := shot.Reconciliation
		if !reconciliation.HasBacksight {
			continue
		}
		disagreement := reconciliation.AzimuthDisagreementDeg
		if disagreement < 180-settings.ReversedWindowDeg {
			continue
		}
		findings = append(findings, Finding{
			Code:     CodeReversedReading,
			Severity: model.SeverityError,
			Subject:  shot.Key(),
			Trip:     shot.TripID,
			Score:    disagreement,
			Message: fmt.Sprintf("shot %s has a reciprocal azimuth disagreement of %s deg, which is close to a full reversal",
				shot.Key(), units.Format(disagreement, 2)),
			Suggestion: fmt.Sprintf("check whether the backsight %s deg was recorded as a foresight",
				units.Format(units.OppositeAzimuth(shot.AzimuthDeg), 2)),
			Evidence: []string{
				"foresight=" + units.Format(shot.AzimuthDeg, 2),
				"disagreement=" + units.Format(disagreement, 2),
			},
		})
	}
	return findings
}

// lengthOutliers flags tape lengths that stand out inside their own trip.
func lengthOutliers(reduced reduce.Result, settings config.Blunders) []Finding {
	byTrip := make(map[string][]reduce.Shot)
	for _, shot := range reduced.ActiveShots() {
		byTrip[shot.TripID] = append(byTrip[shot.TripID], shot)
	}
	tripIDs := make([]string, 0, len(byTrip))
	for tripID := range byTrip {
		tripIDs = append(tripIDs, tripID)
	}
	sort.Strings(tripIDs)
	findings := make([]Finding, 0)
	for _, tripID := range tripIDs {
		shots := byTrip[tripID]
		if len(shots) < settings.LengthOutlierMinimum {
			continue
		}
		lengths := make([]float64, 0, len(shots))
		for _, shot := range shots {
			lengths = append(lengths, shot.DistanceMeters)
		}
		mean := geom.Mean(lengths)
		deviation := geom.StdDev(lengths)
		median := geom.Median(lengths)
		if deviation <= 1e-9 {
			continue
		}
		sorted := make([]reduce.Shot, len(shots))
		copy(sorted, shots)
		sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].ShotID < sorted[b].ShotID })
		for _, shot := range sorted {
			sigmas := math.Abs(shot.DistanceMeters-mean) / deviation
			if sigmas < settings.LengthOutlierSigma {
				continue
			}
			findings = append(findings, Finding{
				Code:     CodeLengthOutlier,
				Severity: model.SeverityWarning,
				Subject:  shot.Key(),
				Trip:     tripID,
				Score:    sigmas,
				Message: fmt.Sprintf("shot %s is %s m long, %s standard deviations from the trip mean of %s m",
					shot.Key(), units.Format(shot.DistanceMeters, 3), units.Format(sigmas, 2), units.Format(mean, 3)),
				Suggestion: fmt.Sprintf("confirm the tape reading against the trip median of %s m", units.Format(median, 3)),
				Evidence: []string{
					"mean=" + units.Format(mean, 3),
					"median=" + units.Format(median, 3),
					"stddev=" + units.Format(deviation, 3),
				},
			})
		}
	}
	return findings
}

// loopFindings evaluates failing loops and searches for a single leg that would
// explain the misclosure.
func loopFindings(graph network.Graph, loopResult loops.Result, shots map[string]reduce.Shot, cfg config.Config) []Finding {
	findings := make([]Finding, 0)
	for _, loop := range loopResult.Loops {
		if loop.WithinTolerance {
			continue
		}
		findings = append(findings, Finding{
			Code:     CodeLoopClosure,
			Severity: model.SeverityWarning,
			Subject:  loop.ID,
			Loop:     loop.ID,
			Score:    loop.TotalErrorMeters,
			Message: fmt.Sprintf("loop %s closes to %s m over %s m of passage (%s ppm)",
				loop.ID, units.Format(loop.TotalErrorMeters, 4),
				units.Format(loop.LengthMeters, 3), units.Format(loop.ErrorPPM, 1)),
			Suggestion: fmt.Sprintf("each leg would absorb about %s m of the misclosure",
				units.Format(loop.TotalErrorMeters/math.Max(1, float64(len(loop.Legs))), 4)),
			Evidence: []string{
				"horizontal=" + units.Format(loop.HorizontalErrorMeters, 4),
				"vertical=" + units.Format(loop.VerticalErrorMeters, 4),
				"legs=" + strconv.Itoa(len(loop.Legs)),
			},
		})
		if loop.VerticalErrorMeters > loop.HorizontalErrorMeters && loop.VerticalErrorMeters > cfg.Tolerances.VerticalClosureMeters {
			findings = append(findings, Finding{
				Code:     CodeVerticalDominant,
				Severity: model.SeverityWarning,
				Subject:  loop.ID,
				Loop:     loop.ID,
				Score:    loop.VerticalErrorMeters,
				Message: fmt.Sprintf("loop %s misses vertically by %s m, more than its horizontal error of %s m",
					loop.ID, units.Format(loop.VerticalErrorMeters, 4), units.Format(loop.HorizontalErrorMeters, 4)),
				Suggestion: "review the clinometer readings and any vertical shot in this loop",
			})
		}
		findings = append(findings, searchSingleLeg(graph, loop, shots, cfg.Blunders)...)
	}
	return findings
}

// searchSingleLeg tries reversing and digit swapping each candidate leg of a
// failing loop and reports the substitutions that would close it.
func searchSingleLeg(graph network.Graph, loop loops.Loop, shots map[string]reduce.Shot, settings config.Blunders) []Finding {
	findings := make([]Finding, 0)
	baseline := loop.TotalErrorMeters
	if baseline <= 1e-9 {
		return findings
	}
	for _, leg := range candidateLegs(loop, settings.MaxCandidates) {
		edge := graph.Edges[leg.EdgeIndex]
		shot, ok := shots[edge.Key()]
		if !ok {
			continue
		}
		reversedClosure := replaceLeg(loop, leg.EdgeIndex, edge.Vector.Negate()).Length()
		if improvement(baseline, reversedClosure) >= settings.TransposeImprovement {
			findings = append(findings, Finding{
				Code:     CodeReversedLeg,
				Severity: model.SeverityWarning,
				Subject:  edge.Key(),
				Loop:     loop.ID,
				Trip:     shot.TripID,
				Score:    improvement(baseline, reversedClosure),
				Message: fmt.Sprintf("reversing leg %s would reduce the misclosure of loop %s from %s m to %s m",
					edge.Key(), loop.ID, units.Format(baseline, 4), units.Format(reversedClosure, 4)),
				Suggestion: fmt.Sprintf("check whether stations %q and %q were swapped in the field notes", edge.From, edge.To),
			})
		}
		best := -1.0
		bestAzimuth := 0.0
		bestClosure := baseline
		for _, candidate := range transpositions(shot.RawAzimuthDeg) {
			correction := units.AzimuthDelta(shot.RawAzimuthDeg, shot.AzimuthDeg)
			candidateReduced := units.NormalizeAzimuth(candidate + correction)
			vector := geom.FromPolar(shot.DistanceMeters, candidateReduced, shot.InclinationDeg)
			closure := replaceLeg(loop, leg.EdgeIndex, vector).Length()
			gain := improvement(baseline, closure)
			if gain <= best {
				continue
			}
			best = gain
			bestAzimuth = candidate
			bestClosure = closure
		}
		if best >= settings.TransposeImprovement {
			findings = append(findings, Finding{
				Code:     CodeTransposedDigits,
				Severity: model.SeverityWarning,
				Subject:  edge.Key(),
				Loop:     loop.ID,
				Trip:     shot.TripID,
				Score:    best,
				Message: fmt.Sprintf("reading the azimuth of leg %s as %s deg instead of %s deg would reduce the misclosure of loop %s from %s m to %s m",
					edge.Key(), units.Format(bestAzimuth, 1), units.Format(shot.RawAzimuthDeg, 1),
					loop.ID, units.Format(baseline, 4), units.Format(bestClosure, 4)),
				Suggestion: "compare the written bearing with the instrument reading, digits may have been transposed",
				Evidence: []string{
					"recorded=" + units.Format(shot.RawAzimuthDeg, 1),
					"candidate=" + units.Format(bestAzimuth, 1),
					"improvement=" + units.Format(best, 3),
				},
			})
		}
	}
	return findings
}

// candidateLegs ranks the legs of a loop by length and caps the search width.
func candidateLegs(loop loops.Loop, limit int) []loops.Leg {
	ordered := make([]loops.Leg, len(loop.Legs))
	copy(ordered, loop.Legs)
	sort.SliceStable(ordered, func(a, b int) bool {
		if !units.NearlyEqual(ordered[a].LengthMeters, ordered[b].LengthMeters, 1e-9) {
			return ordered[a].LengthMeters > ordered[b].LengthMeters
		}
		return ordered[a].ShotKey < ordered[b].ShotKey
	})
	if limit > 0 && len(ordered) > limit {
		ordered = ordered[:limit]
	}
	return ordered
}

// replaceLeg recomputes the closure of a loop with one leg substituted.
func replaceLeg(loop loops.Loop, edgeIndex int, replacement geom.Vector) geom.Vector {
	closure := geom.Zero()
	for _, leg := range loop.Legs {
		vector := leg.Vector
		if leg.EdgeIndex == edgeIndex {
			vector = replacement
			if leg.Reversed {
				vector = vector.Negate()
			}
		}
		closure = closure.Add(vector)
	}
	return closure
}

// improvement returns the fraction of the misclosure removed by a substitution.
func improvement(baseline, candidate float64) float64 {
	if baseline <= 1e-12 {
		return 0
	}
	gain := (baseline - candidate) / baseline
	if gain < 0 {
		return 0
	}
	return gain
}

// transpositions lists the plausible digit swaps of a recorded bearing. The
// bearing is rounded to whole degrees because transposition is a bookkeeping
// error made on the written number.
func transpositions(azimuthDeg float64) []float64 {
	whole := int(math.Round(units.NormalizeAzimuth(azimuthDeg)))
	if whole < 0 {
		whole = 0
	}
	text := strconv.Itoa(whole)
	seen := map[string]bool{text: true}
	candidates := make([]float64, 0, 4)
	digits := []byte(text)
	for first := 0; first < len(digits); first++ {
		for second := first + 1; second < len(digits); second++ {
			swapped := make([]byte, len(digits))
			copy(swapped, digits)
			swapped[first], swapped[second] = swapped[second], swapped[first]
			candidate := strings.TrimLeft(string(swapped), "0")
			if candidate == "" {
				candidate = "0"
			}
			if seen[candidate] {
				continue
			}
			seen[candidate] = true
			value, err := strconv.Atoi(candidate)
			if err != nil {
				continue
			}
			candidates = append(candidates, units.NormalizeAzimuth(float64(value)))
		}
	}
	sort.Float64s(candidates)
	return candidates
}
