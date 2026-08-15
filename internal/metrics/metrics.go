// Package metrics derives the descriptive statistics of a reduced cave survey:
// surveyed length, vertical range, per trip figures and the extreme passages.
package metrics

import (
	"math"
	"sort"

	"CaveLoop/internal/geom"
	"CaveLoop/internal/loops"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/reduce"
	"CaveLoop/internal/traverse"
)

// TripStats summarises a single survey trip.
type TripStats struct {
	TripID              string   `json:"tripId"`
	Name                string   `json:"name,omitempty"`
	Date                string   `json:"date,omitempty"`
	Surveyors           []string `json:"surveyors,omitempty"`
	ShotCount           int      `json:"shotCount"`
	ExcludedShots       int      `json:"excludedShots"`
	StationCount        int      `json:"stationCount"`
	LengthMeters        float64  `json:"lengthMeters"`
	HorizontalMeters    float64  `json:"horizontalMeters"`
	VerticalGainMeters  float64  `json:"verticalGainMeters"`
	VerticalDropMeters  float64  `json:"verticalDropMeters"`
	MinInclinationDeg   float64  `json:"minInclinationDeg"`
	MaxInclinationDeg   float64  `json:"maxInclinationDeg"`
	MeanShotMeters      float64  `json:"meanShotMeters"`
	MedianShotMeters    float64  `json:"medianShotMeters"`
	LongestShot         string   `json:"longestShot,omitempty"`
	LongestShotMeters   float64  `json:"longestShotMeters"`
	BacksightCoverage   float64  `json:"backsightCoverage"`
	OutOfToleranceShots int      `json:"outOfToleranceShots"`
}

// StationExtreme names a station together with the value that makes it extreme.
type StationExtreme struct {
	Station string  `json:"station"`
	Meters  float64 `json:"meters"`
}

// Summary is the descriptive statistic block of a whole survey.
type Summary struct {
	Cave                   string           `json:"cave"`
	Region                 string           `json:"region,omitempty"`
	StationCount           int              `json:"stationCount"`
	ShotCount              int              `json:"shotCount"`
	ActiveShotCount        int              `json:"activeShotCount"`
	ExcludedShotCount      int              `json:"excludedShotCount"`
	TotalLengthMeters      float64          `json:"totalLengthMeters"`
	HorizontalLengthMeters float64          `json:"horizontalLengthMeters"`
	MeanShotMeters         float64          `json:"meanShotMeters"`
	MedianShotMeters       float64          `json:"medianShotMeters"`
	VerticalRangeMeters    float64          `json:"verticalRangeMeters"`
	MaxDepthMeters         float64          `json:"maxDepthMeters"`
	Deepest                StationExtreme   `json:"deepest"`
	Highest                StationExtreme   `json:"highest"`
	LongestShot            string           `json:"longestShot,omitempty"`
	LongestShotMeters      float64          `json:"longestShotMeters"`
	LongestTrip            string           `json:"longestTrip,omitempty"`
	LongestTripMeters      float64          `json:"longestTripMeters"`
	DeepestTrip            string           `json:"deepestTrip,omitempty"`
	BoundingBox            geom.BoundingBox `json:"boundingBox"`
	Extent                 geom.Vector      `json:"extent"`
	ComponentCount         int              `json:"componentCount"`
	JunctionCount          int              `json:"junctionCount"`
	DeadEndCount           int              `json:"deadEndCount"`
	LoopCount              int              `json:"loopCount"`
	FailingLoopCount       int              `json:"failingLoopCount"`
	BacksightCoverage      float64          `json:"backsightCoverage"`
	Trips                  []TripStats      `json:"trips"`
}

// Inputs bundles everything the summary needs.
type Inputs struct {
	Survey   model.Survey
	Reduced  reduce.Result
	Graph    network.Graph
	Analysis network.Analysis
	Layout   traverse.Result
	Loops    loops.Result
}

// Summarise computes the descriptive statistics of a reduced survey.
func Summarise(in Inputs) Summary {
	summary := Summary{
		Cave:              in.Reduced.Cave,
		Region:            in.Reduced.Region,
		StationCount:      len(in.Reduced.Stations),
		ShotCount:         len(in.Reduced.Shots),
		ExcludedShotCount: in.Reduced.ExcludedShot,
		ComponentCount:    len(in.Analysis.Components),
		JunctionCount:     len(in.Analysis.Junctions),
		DeadEndCount:      len(in.Analysis.DeadEnds),
		LoopCount:         len(in.Loops.Loops),
		FailingLoopCount:  in.Loops.FailingCount,
	}
	active := in.Reduced.ActiveShots()
	summary.ActiveShotCount = len(active)
	lengths := make([]float64, 0, len(active))
	withBacksight := 0
	for _, shot := range active {
		summary.TotalLengthMeters += shot.DistanceMeters
		summary.HorizontalLengthMeters += shot.HorizontalMeters
		lengths = append(lengths, shot.DistanceMeters)
		if shot.Reconciliation.HasBacksight {
			withBacksight++
		}
		if shot.DistanceMeters > summary.LongestShotMeters {
			summary.LongestShotMeters = shot.DistanceMeters
			summary.LongestShot = shot.Key()
		}
	}
	summary.MeanShotMeters = geom.Mean(lengths)
	summary.MedianShotMeters = geom.Median(lengths)
	if len(active) > 0 {
		summary.BacksightCoverage = float64(withBacksight) / float64(len(active))
	}
	summary.applyGeometry(in.Layout)
	summary.Trips = tripStats(in)
	for _, trip := range summary.Trips {
		if trip.LengthMeters > summary.LongestTripMeters {
			summary.LongestTripMeters = trip.LengthMeters
			summary.LongestTrip = trip.TripID
		}
	}
	summary.DeepestTrip = deepestTrip(in, summary.Deepest.Station)
	return summary
}

// applyGeometry fills the coordinate derived figures.
func (s *Summary) applyGeometry(layout traverse.Result) {
	if len(layout.Positions) == 0 {
		s.BoundingBox = geom.BoundingBox{Empty: true}
		return
	}
	points := layout.Coordinates()
	s.BoundingBox = geom.NewBoundingBox(points)
	s.Extent = s.BoundingBox.Extent()
	s.VerticalRangeMeters = s.BoundingBox.Max.Up - s.BoundingBox.Min.Up
	lowest := math.Inf(1)
	highest := math.Inf(-1)
	for _, position := range layout.Positions {
		if position.Coordinate.Up < lowest ||
			(position.Coordinate.Up == lowest && position.Station < s.Deepest.Station) {
			lowest = position.Coordinate.Up
			s.Deepest = StationExtreme{Station: position.Station, Meters: position.Coordinate.Up}
		}
		if position.Coordinate.Up > highest ||
			(position.Coordinate.Up == highest && position.Station < s.Highest.Station) {
			highest = position.Coordinate.Up
			s.Highest = StationExtreme{Station: position.Station, Meters: position.Coordinate.Up}
		}
	}
	maxDepth := 0.0
	for _, position := range layout.Positions {
		if position.DepthMeters > maxDepth {
			maxDepth = position.DepthMeters
		}
	}
	s.MaxDepthMeters = maxDepth
}

// tripStats builds the per trip statistics in trip identifier order.
func tripStats(in Inputs) []TripStats {
	metadata := make(map[string]model.Trip, len(in.Survey.Trips))
	for _, trip := range in.Survey.Trips {
		metadata[trip.ID] = trip
	}
	grouped := make(map[string][]reduce.Shot)
	for _, shot := range in.Reduced.Shots {
		grouped[shot.TripID] = append(grouped[shot.TripID], shot)
	}
	tripIDs := make([]string, 0, len(grouped))
	for tripID := range grouped {
		tripIDs = append(tripIDs, tripID)
	}
	for tripID := range metadata {
		if _, ok := grouped[tripID]; !ok {
			tripIDs = append(tripIDs, tripID)
		}
	}
	sort.Strings(tripIDs)
	out := make([]TripStats, 0, len(tripIDs))
	for _, tripID := range tripIDs {
		stats := TripStats{TripID: tripID}
		if trip, ok := metadata[tripID]; ok {
			stats.Name = trip.Name
			stats.Date = trip.Date
			stats.Surveyors = trip.Surveyors
		}
		shots := grouped[tripID]
		stats.ShotCount = len(shots)
		lengths := make([]float64, 0, len(shots))
		stations := make(map[string]bool, len(shots)*2)
		minimum := math.Inf(1)
		maximum := math.Inf(-1)
		withBacksight := 0
		for _, shot := range shots {
			stations[shot.From] = true
			stations[shot.To] = true
			if shot.Excluded {
				stats.ExcludedShots++
				continue
			}
			lengths = append(lengths, shot.DistanceMeters)
			stats.LengthMeters += shot.DistanceMeters
			stats.HorizontalMeters += shot.HorizontalMeters
			if shot.Vector.Up >= 0 {
				stats.VerticalGainMeters += shot.Vector.Up
			} else {
				stats.VerticalDropMeters += -shot.Vector.Up
			}
			minimum = math.Min(minimum, shot.InclinationDeg)
			maximum = math.Max(maximum, shot.InclinationDeg)
			if shot.Reconciliation.HasBacksight {
				withBacksight++
			}
			if !shot.Reconciliation.WithinTolerance {
				stats.OutOfToleranceShots++
			}
			if shot.DistanceMeters > stats.LongestShotMeters {
				stats.LongestShotMeters = shot.DistanceMeters
				stats.LongestShot = shot.Key()
			}
		}
		if trip, ok := metadata[tripID]; ok {
			for _, station := range trip.Stations {
				stations[station.Name] = true
			}
		}
		stats.StationCount = len(stations)
		stats.MeanShotMeters = geom.Mean(lengths)
		stats.MedianShotMeters = geom.Median(lengths)
		if len(lengths) > 0 {
			stats.MinInclinationDeg = minimum
			stats.MaxInclinationDeg = maximum
			stats.BacksightCoverage = float64(withBacksight) / float64(len(lengths))
		}
		out = append(out, stats)
	}
	return out
}

// deepestTrip names the trip that established the deepest station.
func deepestTrip(in Inputs, deepest string) string {
	if deepest == "" {
		return ""
	}
	for _, station := range in.Reduced.Stations {
		if station.Name != deepest {
			continue
		}
		if len(station.Trips) == 0 {
			return ""
		}
		return station.Trips[0]
	}
	return ""
}
