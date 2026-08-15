// Package report renders CaveLoop results either as deterministic indented JSON
// or as fixed width text tables.
//
// Both renderings are pure functions of the values passed in: numeric fields are
// rounded to the configured precision before formatting, collections arrive
// already sorted from the analysis packages, and no timestamps or hostnames ever
// appear. Two runs over the same store therefore produce identical bytes.
package report

import (
	"fmt"
	"io"
	"strings"

	"CaveLoop/internal/adjust"
	"CaveLoop/internal/blunder"
	"CaveLoop/internal/config"
	"CaveLoop/internal/jsonx"
	"CaveLoop/internal/loops"
	"CaveLoop/internal/metrics"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/pipeline"
	"CaveLoop/internal/store"
	"CaveLoop/internal/units"
)

// Renderer writes command results in the configured output format.
type Renderer struct {
	out io.Writer
	cfg config.Config
}

// New builds a renderer for the given writer and configuration.
func New(out io.Writer, cfg config.Config) *Renderer {
	return &Renderer{out: out, cfg: cfg}
}

// IsJSON reports whether the renderer emits JSON.
func (r *Renderer) IsJSON() bool { return r.cfg.Output.Format == config.OutputJSON }

// Length formats a linear value with the configured precision.
func (r *Renderer) Length(value float64) string {
	return units.Format(value, r.cfg.Output.LengthPrecision)
}

// Angle formats an angular value with the configured precision.
func (r *Renderer) Angle(value float64) string {
	return units.Format(value, r.cfg.Output.AnglePrecision)
}

// writeJSON emits an indented JSON document.
func (r *Renderer) writeJSON(payload any) error {
	encoded, err := jsonx.MarshalIndent(payload)
	if err != nil {
		return err
	}
	if _, err := r.out.Write(encoded); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return nil
}

// printf writes a formatted text line.
func (r *Renderer) printf(format string, args ...any) error {
	if _, err := fmt.Fprintf(r.out, format, args...); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return nil
}

// IssueCounts groups issues by severity.
type IssueCounts struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

// countIssues tallies a list of issues.
func countIssues(issues model.Issues) IssueCounts {
	return IssueCounts{
		Errors:   len(issues.Filter(model.SeverityError)),
		Warnings: len(issues.Filter(model.SeverityWarning)),
	}
}

// ValidateReport is the payload of the validate command.
type ValidateReport struct {
	Command     string       `json:"command"`
	Source      string       `json:"source"`
	Cave        string       `json:"cave"`
	Region      string       `json:"region,omitempty"`
	Instruments int          `json:"instruments"`
	Trips       int          `json:"trips"`
	Stations    int          `json:"stations"`
	Shots       int          `json:"shots"`
	Valid       bool         `json:"valid"`
	Counts      IssueCounts  `json:"counts"`
	Issues      model.Issues `json:"issues"`
}

// ImportReport is the payload of the import command.
type ImportReport struct {
	Command       string         `json:"command"`
	Source        string         `json:"source"`
	Store         string         `json:"store"`
	Cave          string         `json:"cave"`
	Appended      int            `json:"appendedRecords"`
	TotalRecords  int            `json:"totalRecords"`
	PayloadDigest string         `json:"payloadDigest"`
	LedgerDigest  string         `json:"ledgerDigest"`
	AuditSeq      int            `json:"auditSeq"`
	AuditHash     string         `json:"auditHash"`
	Metadata      store.Metadata `json:"metadata"`
}

// StationLine is one row of the reduced station table.
type StationLine struct {
	Station     string   `json:"station"`
	East        float64  `json:"east"`
	North       float64  `json:"north"`
	Up          float64  `json:"up"`
	DepthMeters float64  `json:"depthMeters"`
	Component   int      `json:"component"`
	Flags       []string `json:"flags,omitempty"`
}

// LegLine is one row of the reduced leg table.
type LegLine struct {
	Shot            string  `json:"shot"`
	From            string  `json:"from"`
	To              string  `json:"to"`
	DistanceMeters  float64 `json:"distanceMeters"`
	AzimuthDeg      float64 `json:"azimuthDeg"`
	InclinationDeg  float64 `json:"inclinationDeg"`
	Compass         string  `json:"compass"`
	Backsight       bool    `json:"backsight"`
	WithinTolerance bool    `json:"withinTolerance"`
}

// ReduceReport is the payload of the reduce and adjust commands.
type ReduceReport struct {
	Command      string          `json:"command"`
	Store        string          `json:"store"`
	Cave         string          `json:"cave"`
	Adjusted     bool            `json:"adjusted"`
	Metrics      metrics.Summary `json:"metrics"`
	Stations     []StationLine   `json:"stations"`
	Legs         []LegLine       `json:"legs"`
	Counts       IssueCounts     `json:"counts"`
	Issues       model.Issues    `json:"issues"`
	SnapshotHash string          `json:"snapshotDigest,omitempty"`
	AuditHash    string          `json:"auditHash,omitempty"`
}

// AdjustReport is the payload of the adjust command.
type AdjustReport struct {
	Command      string        `json:"command"`
	Store        string        `json:"store"`
	Cave         string        `json:"cave"`
	Adjustment   adjust.Result `json:"adjustment"`
	LoopsBefore  loops.Result  `json:"loopsBefore"`
	LoopsAfter   loops.Result  `json:"loopsAfter"`
	Counts       IssueCounts   `json:"counts"`
	Issues       model.Issues  `json:"issues"`
	SnapshotHash string        `json:"snapshotDigest,omitempty"`
	AuditHash    string        `json:"auditHash,omitempty"`
}

// NetworkReport is the payload of the network command.
type NetworkReport struct {
	Command  string           `json:"command"`
	Store    string           `json:"store"`
	Cave     string           `json:"cave"`
	Analysis network.Analysis `json:"analysis"`
	Counts   IssueCounts      `json:"counts"`
	Issues   model.Issues     `json:"issues"`
}

// LoopReport is the payload of the loops command.
type LoopReport struct {
	Command  string       `json:"command"`
	Store    string       `json:"store"`
	Cave     string       `json:"cave"`
	Adjusted bool         `json:"adjusted"`
	Loops    loops.Result `json:"loops"`
	Counts   IssueCounts  `json:"counts"`
	Issues   model.Issues `json:"issues"`
}

// BlunderReport is the payload of the blunders command.
type BlunderReport struct {
	Command  string         `json:"command"`
	Store    string         `json:"store"`
	Cave     string         `json:"cave"`
	Enabled  bool           `json:"enabled"`
	Findings []Finding      `json:"findings"`
	Counts   map[string]int `json:"counts"`
}

// Finding mirrors a blunder finding for reporting.
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

// FullReport is the payload of the report command.
type FullReport struct {
	Command    string              `json:"command"`
	Store      string              `json:"store"`
	Cave       string              `json:"cave"`
	Region     string              `json:"region,omitempty"`
	Adjusted   bool                `json:"adjusted"`
	Metrics    metrics.Summary     `json:"metrics"`
	Topology   TopologySummary     `json:"topology"`
	Closure    ClosureSummary      `json:"closure"`
	Blunders   map[string]int      `json:"blunderCounts"`
	Counts     IssueCounts         `json:"counts"`
	Issues     model.Issues        `json:"issues"`
	Trips      []metrics.TripStats `json:"trips"`
	Extremes   map[string]string   `json:"extremes"`
	Tolerances config.Tolerances   `json:"tolerances"`
}

// TopologySummary condenses the graph analysis for the full report.
type TopologySummary struct {
	Stations       int `json:"stations"`
	Legs           int `json:"legs"`
	Components     int `json:"components"`
	Junctions      int `json:"junctions"`
	DeadEnds       int `json:"deadEnds"`
	Isolated       int `json:"isolated"`
	Duplicates     int `json:"duplicateLegs"`
	NameCollisions int `json:"nameCollisions"`
}

// ClosureSummary condenses the loop analysis for the full report.
type ClosureSummary struct {
	Loops                 int     `json:"loops"`
	Failing               int     `json:"failing"`
	WorstLoop             string  `json:"worstLoop,omitempty"`
	WorstErrorMeters      float64 `json:"worstErrorMeters"`
	AdjustedLegs          int     `json:"adjustedLegs"`
	TotalCorrectionMeters float64 `json:"totalCorrectionMeters"`
	Converged             bool    `json:"converged"`
}

// VerifyReport is the payload of the verify command.
type VerifyReport struct {
	Command        string                  `json:"command"`
	Store          string                  `json:"store"`
	Audit          store.AuditVerification `json:"audit"`
	Metadata       store.Metadata          `json:"metadata"`
	LedgerDigest   string                  `json:"ledgerDigest"`
	SnapshotDigest string                  `json:"snapshotDigest"`
	LedgerMatches  bool                    `json:"ledgerMatches"`
	SnapshotMatch  bool                    `json:"snapshotMatches"`
	RecordsReadble bool                    `json:"recordsReadable"`
	Valid          bool                    `json:"valid"`
	Problems       []string                `json:"problems"`
}

// BuildStationLines converts a snapshot into station rows.
func BuildStationLines(snapshot pipeline.Snapshot) []StationLine {
	lines := make([]StationLine, 0, len(snapshot.Stations))
	for _, station := range snapshot.Stations {
		lines = append(lines, StationLine{
			Station:     station.Name,
			East:        station.Coordinate.East,
			North:       station.Coordinate.North,
			Up:          station.Coordinate.Up,
			DepthMeters: station.DepthMeters,
			Component:   station.Component,
			Flags:       station.Flags,
		})
	}
	return lines
}

// BuildLegLines converts a snapshot into leg rows.
func BuildLegLines(snapshot pipeline.Snapshot) []LegLine {
	lines := make([]LegLine, 0, len(snapshot.Legs))
	for _, leg := range snapshot.Legs {
		lines = append(lines, LegLine{
			Shot:            leg.Shot,
			From:            leg.From,
			To:              leg.To,
			DistanceMeters:  leg.DistanceMeters,
			AzimuthDeg:      leg.AzimuthDeg,
			InclinationDeg:  leg.InclinationDeg,
			Compass:         units.CompassPoint(leg.AzimuthDeg),
			Backsight:       leg.Backsight,
			WithinTolerance: leg.WithinTolerance,
		})
	}
	return lines
}

// BuildFindings converts blunder findings for reporting.
func BuildFindings(findings []blunder.Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		out = append(out, Finding{
			Code:       finding.Code,
			Severity:   finding.Severity,
			Subject:    finding.Subject,
			Loop:       finding.Loop,
			Trip:       finding.Trip,
			Score:      finding.Score,
			Message:    finding.Message,
			Suggestion: finding.Suggestion,
			Evidence:   finding.Evidence,
		})
	}
	return out
}

// BuildTopology condenses a network analysis.
func BuildTopology(analysis network.Analysis) TopologySummary {
	return TopologySummary{
		Stations:       analysis.StationCount,
		Legs:           analysis.EdgeCount,
		Components:     len(analysis.Components),
		Junctions:      len(analysis.Junctions),
		DeadEnds:       len(analysis.DeadEnds),
		Isolated:       len(analysis.Isolated),
		Duplicates:     len(analysis.Duplicates),
		NameCollisions: len(analysis.NameCollisions),
	}
}

// BuildClosure condenses the loop and adjustment results.
func BuildClosure(loopResult loops.Result, adjustment adjust.Result) ClosureSummary {
	return ClosureSummary{
		Loops:                 len(loopResult.Loops),
		Failing:               loopResult.FailingCount,
		WorstLoop:             loopResult.WorstLoop,
		WorstErrorMeters:      loopResult.WorstErrorMeters,
		AdjustedLegs:          adjustment.AdjustedLegs,
		TotalCorrectionMeters: adjustment.TotalCorrectionMeters,
		Converged:             adjustment.Converged,
	}
}

// Counts exposes the issue tally of an issue list.
func Counts(issues model.Issues) IssueCounts { return countIssues(issues) }

// joinList renders a string slice for text output.
func joinList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

// yesNo renders a boolean in a fixed width friendly form.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
