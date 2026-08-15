// Package pipeline wires the reduction stages together and produces the
// persistent network snapshot.
//
// The stage order is fixed: validate, reduce, build the graph, analyse the
// topology, lay out coordinates, detect the loop basis, optionally distribute
// closure error, look for blunders and finally summarise. Each stage only reads
// the output of the previous ones, so the whole run is a pure function of the
// survey and the configuration.
package pipeline

import (
	"fmt"

	"CaveLoop/internal/adjust"
	"CaveLoop/internal/blunder"
	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/loops"
	"CaveLoop/internal/metrics"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/reduce"
	"CaveLoop/internal/store"
	"CaveLoop/internal/traverse"
)

// SnapshotSchema is the schema version of the network snapshot artefact.
const SnapshotSchema = 1

// Options selects which optional stages run.
type Options struct {
	Adjust         bool
	DetectBlunders bool
}

// Outcome is the complete result of a pipeline run.
type Outcome struct {
	Survey         model.Survey
	Validation     model.Issues
	Reduced        reduce.Result
	Graph          network.Graph
	Analysis       network.Analysis
	Layout         traverse.Result
	Loops          loops.Result
	Adjustment     adjust.Result
	AdjustedLayout traverse.Result
	AdjustedLoops  loops.Result
	Blunders       blunder.Result
	Metrics        metrics.Summary
	Issues         model.Issues
	Adjusted       bool
}

// FinalLayout returns the coordinates that the report should present.
func (o Outcome) FinalLayout() traverse.Result {
	if o.Adjusted {
		return o.AdjustedLayout
	}
	return o.Layout
}

// FinalLoops returns the loop analysis that matches the final coordinates.
func (o Outcome) FinalLoops() loops.Result {
	if o.Adjusted {
		return o.AdjustedLoops
	}
	return o.Loops
}

// Run executes the pipeline over a survey.
func Run(survey model.Survey, cfg config.Config, options Options) (Outcome, error) {
	outcome := Outcome{Survey: survey.Canonical()}
	outcome.Validation = model.Validate(outcome.Survey, cfg.ModelDefaults())
	if outcome.Validation.HasErrors() {
		return outcome, fmt.Errorf("survey is not valid: %w", outcome.Validation)
	}
	reduced, err := reduce.Reduce(outcome.Survey, cfg)
	if err != nil {
		return outcome, fmt.Errorf("reduction failed: %w", err)
	}
	outcome.Reduced = reduced
	if reduced.Issues.HasErrors() {
		return outcome, fmt.Errorf("reduction rejected the survey: %w", reduced.Issues)
	}
	outcome.Graph = network.Build(reduced)
	outcome.Analysis = network.Analyze(outcome.Graph)
	layout, err := traverse.Compute(outcome.Graph, outcome.Analysis, nil)
	if err != nil {
		return outcome, fmt.Errorf("traverse failed: %w", err)
	}
	outcome.Layout = layout
	loopResult, err := loops.Detect(outcome.Graph, outcome.Analysis, layout, cfg.Tolerances, nil)
	if err != nil {
		return outcome, fmt.Errorf("loop detection failed: %w", err)
	}
	outcome.Loops = loopResult
	if options.Adjust && cfg.Adjustment.Enabled {
		adjustment, err := adjust.Apply(outcome.Graph, loopResult, cfg.Adjustment)
		if err != nil {
			return outcome, fmt.Errorf("adjustment failed: %w", err)
		}
		outcome.Adjustment = adjustment
		adjustedLayout, err := traverse.Compute(outcome.Graph, outcome.Analysis, adjustment.Vectors)
		if err != nil {
			return outcome, fmt.Errorf("adjusted traverse failed: %w", err)
		}
		outcome.AdjustedLayout = adjustedLayout
		adjustedLoops, err := loops.Detect(outcome.Graph, outcome.Analysis, adjustedLayout, cfg.Tolerances, adjustment.Vectors)
		if err != nil {
			return outcome, fmt.Errorf("adjusted loop detection failed: %w", err)
		}
		outcome.AdjustedLoops = adjustedLoops
		outcome.Adjusted = true
	}
	if options.DetectBlunders {
		outcome.Blunders = blunder.Detect(reduced, outcome.Graph, loopResult, cfg)
	}
	outcome.Metrics = metrics.Summarise(metrics.Inputs{
		Survey:   outcome.Survey,
		Reduced:  reduced,
		Graph:    outcome.Graph,
		Analysis: outcome.Analysis,
		Layout:   outcome.FinalLayout(),
		Loops:    outcome.FinalLoops(),
	})
	outcome.Issues = collectIssues(outcome)
	return outcome, nil
}

// collectIssues merges the findings of every stage into one sorted list.
func collectIssues(outcome Outcome) model.Issues {
	issues := model.Issues{}
	issues = append(issues, outcome.Validation...)
	issues = append(issues, outcome.Reduced.Issues...)
	issues = append(issues, outcome.Analysis.Issues...)
	issues = append(issues, outcome.FinalLayout().Issues...)
	issues = append(issues, outcome.FinalLoops().Issues...)
	issues = append(issues, outcome.Adjustment.Issues...)
	issues = append(issues, outcome.Blunders.Issues...)
	return dedupeIssues(issues.Sorted())
}

// dedupeIssues removes exact duplicates from a sorted issue list.
func dedupeIssues(sorted model.Issues) model.Issues {
	out := make(model.Issues, 0, len(sorted))
	for index, issue := range sorted {
		if index > 0 {
			previous := sorted[index-1]
			if previous == issue {
				continue
			}
		}
		out = append(out, issue)
	}
	return out
}

// StationSnapshot is the persisted description of one station.
type StationSnapshot struct {
	Name             string      `json:"name"`
	Coordinate       geom.Vector `json:"coordinate"`
	DepthMeters      float64     `json:"depthMeters"`
	Component        int         `json:"component"`
	Parent           string      `json:"parent,omitempty"`
	ViaShot          string      `json:"viaShot,omitempty"`
	PathLengthMeters float64     `json:"pathLengthMeters"`
	Flags            []string    `json:"flags,omitempty"`
	Control          bool        `json:"control"`
	Anchor           bool        `json:"anchor"`
	Trips            []string    `json:"trips,omitempty"`
}

// LegSnapshot is the persisted description of one reduced leg.
type LegSnapshot struct {
	Shot             string      `json:"shot"`
	From             string      `json:"from"`
	To               string      `json:"to"`
	DistanceMeters   float64     `json:"distanceMeters"`
	AzimuthDeg       float64     `json:"azimuthDeg"`
	InclinationDeg   float64     `json:"inclinationDeg"`
	HorizontalMeters float64     `json:"horizontalMeters"`
	Vector           geom.Vector `json:"vector"`
	AdjustedVector   geom.Vector `json:"adjustedVector"`
	Backsight        bool        `json:"backsight"`
	WithinTolerance  bool        `json:"withinTolerance"`
	Notes            []string    `json:"notes,omitempty"`
}

// Snapshot is the persisted computed network.
type Snapshot struct {
	Schema     int                `json:"schema"`
	Generator  string             `json:"generator"`
	Cave       string             `json:"cave"`
	Region     string             `json:"region,omitempty"`
	Adjusted   bool               `json:"adjusted"`
	Stations   []StationSnapshot  `json:"stations"`
	Legs       []LegSnapshot      `json:"legs"`
	Network    network.Analysis   `json:"network"`
	Loops      loops.Result       `json:"loops"`
	Adjustment adjust.Result      `json:"adjustment"`
	Metrics    metrics.Summary    `json:"metrics"`
	Blunders   []blunder.Finding  `json:"blunders"`
	Issues     model.Issues       `json:"issues"`
	Datum      map[int]float64    `json:"datumUpMeters"`
	Residuals  []ControlResidual  `json:"controlResiduals"`
	Tolerances config.Tolerances  `json:"tolerances"`
	Defaults   config.Defaults    `json:"defaults"`
	Settings   config.Adjustment  `json:"adjustmentSettings"`
	Counts     map[string]int     `json:"blunderCounts"`
	Extremes   map[string]string  `json:"extremes"`
	Summary    map[string]float64 `json:"summaryMeters"`
}

// ControlResidual mirrors the traverse residual in the snapshot.
type ControlResidual struct {
	Station     string      `json:"station"`
	Residual    geom.Vector `json:"residual"`
	TotalMeters float64     `json:"totalMeters"`
}

// BuildSnapshot renders an outcome into the persistent snapshot form.
func BuildSnapshot(outcome Outcome, cfg config.Config) Snapshot {
	layout := outcome.FinalLayout()
	snapshot := Snapshot{
		Schema:     SnapshotSchema,
		Generator:  store.Generator,
		Cave:       outcome.Reduced.Cave,
		Region:     outcome.Reduced.Region,
		Adjusted:   outcome.Adjusted,
		Network:    outcome.Analysis,
		Loops:      outcome.FinalLoops(),
		Adjustment: outcome.Adjustment,
		Metrics:    outcome.Metrics,
		Issues:     outcome.Issues,
		Datum:      layout.DatumUpMeters,
		Tolerances: cfg.Tolerances,
		Defaults:   cfg.Defaults,
		Settings:   cfg.Adjustment,
		Counts:     outcome.Blunders.Counts,
	}
	if snapshot.Counts == nil {
		snapshot.Counts = map[string]int{}
	}
	snapshot.Blunders = outcome.Blunders.Findings
	if snapshot.Blunders == nil {
		snapshot.Blunders = []blunder.Finding{}
	}
	stationRecords := make(map[string]reduce.Station, len(outcome.Reduced.Stations))
	for _, station := range outcome.Reduced.Stations {
		stationRecords[station.Name] = station
	}
	snapshot.Stations = make([]StationSnapshot, 0, len(layout.Positions))
	for _, position := range layout.Positions {
		entry := StationSnapshot{
			Name:             position.Station,
			Coordinate:       position.Coordinate.Round(6),
			DepthMeters:      position.DepthMeters,
			Component:        position.Component,
			Parent:           position.Parent,
			ViaShot:          position.ViaShot,
			PathLengthMeters: position.PathLengthMeters,
			Control:          position.Control,
			Anchor:           position.Anchor,
		}
		if record, ok := stationRecords[position.Station]; ok {
			entry.Flags = record.Flags
			entry.Trips = record.Trips
		}
		snapshot.Stations = append(snapshot.Stations, entry)
	}
	adjusted := outcome.Adjustment.Vectors
	snapshot.Legs = make([]LegSnapshot, 0, len(outcome.Graph.Edges))
	for _, edge := range outcome.Graph.Edges {
		leg := LegSnapshot{
			Shot:           edge.Key(),
			From:           edge.From,
			To:             edge.To,
			DistanceMeters: edge.LengthMeters,
			Vector:         edge.Vector.Round(6),
			AdjustedVector: edge.Vector.Round(6),
		}
		if adjusted != nil && edge.Index < len(adjusted) {
			leg.AdjustedVector = adjusted[edge.Index].Round(6)
		}
		snapshot.Legs = append(snapshot.Legs, leg)
	}
	shotByKey := make(map[string]reduce.Shot, len(outcome.Reduced.Shots))
	for _, shot := range outcome.Reduced.Shots {
		shotByKey[shot.Key()] = shot
	}
	for index := range snapshot.Legs {
		shot, ok := shotByKey[snapshot.Legs[index].Shot]
		if !ok {
			continue
		}
		snapshot.Legs[index].AzimuthDeg = shot.AzimuthDeg
		snapshot.Legs[index].InclinationDeg = shot.InclinationDeg
		snapshot.Legs[index].HorizontalMeters = shot.HorizontalMeters
		snapshot.Legs[index].Backsight = shot.Reconciliation.HasBacksight
		snapshot.Legs[index].WithinTolerance = shot.Reconciliation.WithinTolerance
		snapshot.Legs[index].Notes = shot.Notes
	}
	snapshot.Residuals = make([]ControlResidual, 0, len(layout.ControlResiduals))
	for _, residual := range layout.ControlResiduals {
		snapshot.Residuals = append(snapshot.Residuals, ControlResidual{
			Station:     residual.Station,
			Residual:    residual.Residual.Round(6),
			TotalMeters: residual.TotalMeters,
		})
	}
	snapshot.Extremes = map[string]string{
		"deepestStation": outcome.Metrics.Deepest.Station,
		"highestStation": outcome.Metrics.Highest.Station,
		"longestShot":    outcome.Metrics.LongestShot,
		"longestTrip":    outcome.Metrics.LongestTrip,
		"deepestTrip":    outcome.Metrics.DeepestTrip,
		"worstLoop":      outcome.FinalLoops().WorstLoop,
	}
	snapshot.Summary = map[string]float64{
		"totalLength":      outcome.Metrics.TotalLengthMeters,
		"horizontalLength": outcome.Metrics.HorizontalLengthMeters,
		"verticalRange":    outcome.Metrics.VerticalRangeMeters,
		"maxDepth":         outcome.Metrics.MaxDepthMeters,
		"worstLoopError":   outcome.FinalLoops().WorstErrorMeters,
	}
	return snapshot
}

// Persist writes the snapshot and refreshed metadata into the store and records
// the action in the audit chain.
func Persist(target *store.Store, outcome Outcome, cfg config.Config, action string) (store.Metadata, store.AuditEntry, error) {
	snapshot := BuildSnapshot(outcome, cfg)
	digest, err := target.WriteJSON(store.SnapshotFile, snapshot)
	if err != nil {
		return store.Metadata{}, store.AuditEntry{}, err
	}
	ledgerDigest, err := target.FileDigest(store.LedgerFile)
	if err != nil {
		return store.Metadata{}, store.AuditEntry{}, err
	}
	records, err := target.LoadRecords()
	if err != nil {
		return store.Metadata{}, store.AuditEntry{}, err
	}
	entry, err := target.Record(action, store.SnapshotFile,
		fmt.Sprintf("stations=%d legs=%d loops=%d", len(snapshot.Stations), len(snapshot.Legs), len(snapshot.Loops.Loops)), digest)
	if err != nil {
		return store.Metadata{}, store.AuditEntry{}, err
	}
	verification, err := target.VerifyAudit()
	if err != nil {
		return store.Metadata{}, store.AuditEntry{}, err
	}
	metadata := store.Metadata{
		Cave:            outcome.Reduced.Cave,
		Region:          outcome.Reduced.Region,
		RecordCount:     len(records),
		InstrumentCount: len(outcome.Survey.Instruments),
		TripCount:       len(outcome.Survey.Trips),
		StationCount:    len(outcome.Reduced.Stations),
		ShotCount:       len(outcome.Reduced.Shots),
		LedgerDigest:    ledgerDigest,
		SnapshotDigest:  digest,
		AuditHead:       verification.Head,
		AuditEntries:    verification.EntryCount,
		LastAction:      action,
	}
	if err := target.WriteMetadata(metadata); err != nil {
		return store.Metadata{}, store.AuditEntry{}, err
	}
	return metadata, entry, nil
}
