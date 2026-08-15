package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"CaveLoop/internal/model"
	"CaveLoop/internal/units"
)

// tableWidth settings shared by every text table.
const (
	tabMinWidth = 2
	tabPadding  = 2
)

// newTable creates a tab writer bound to the renderer output.
func (r *Renderer) newTable() *tabwriter.Writer {
	return tabwriter.NewWriter(r.out, tabMinWidth, 0, tabPadding, ' ', 0)
}

// section writes a titled section header.
func (r *Renderer) section(title string) error {
	if err := r.printf("%s\n", title); err != nil {
		return err
	}
	return r.printf("%s\n", strings.Repeat("-", len(title)))
}

// Validate renders the validate command result.
func (r *Renderer) Validate(payload ValidateReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	if err := r.section("CaveLoop validation"); err != nil {
		return err
	}
	if err := r.printf("source       %s\ncave         %s\n", payload.Source, orDash(payload.Cave)); err != nil {
		return err
	}
	if payload.Region != "" {
		if err := r.printf("region       %s\n", payload.Region); err != nil {
			return err
		}
	}
	if err := r.printf("instruments  %d\ntrips        %d\nstations     %d\nshots        %d\nverdict      %s\n",
		payload.Instruments, payload.Trips, payload.Stations, payload.Shots, verdict(payload.Valid)); err != nil {
		return err
	}
	if err := r.printf("errors       %d\nwarnings     %d\n", payload.Counts.Errors, payload.Counts.Warnings); err != nil {
		return err
	}
	return r.issues(payload.Issues)
}

// issues renders an issue table.
func (r *Renderer) issues(issues model.Issues) error {
	if len(issues) == 0 {
		return r.printf("\nno findings\n")
	}
	if err := r.printf("\nfindings\n"); err != nil {
		return err
	}
	table := r.newTable()
	if _, err := fmt.Fprintln(table, "SEVERITY\tCODE\tPATH\tMESSAGE"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, issue := range issues {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", issue.Severity, issue.Code, issue.Path, issue.Message); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	return flush(table)
}

// Import renders the import command result.
func (r *Renderer) Import(payload ImportReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	if err := r.section("CaveLoop import"); err != nil {
		return err
	}
	return r.printf(""+
		"source            %s\n"+
		"store             %s\n"+
		"cave              %s\n"+
		"appended records  %d\n"+
		"ledger records    %d\n"+
		"payload digest    %s\n"+
		"ledger digest     %s\n"+
		"audit sequence    %d\n"+
		"audit head        %s\n"+
		"instruments       %d\n"+
		"trips             %d\n"+
		"stations          %d\n"+
		"shots             %d\n",
		payload.Source, payload.Store, orDash(payload.Cave), payload.Appended, payload.TotalRecords,
		payload.PayloadDigest, payload.LedgerDigest, payload.AuditSeq, payload.AuditHash,
		payload.Metadata.InstrumentCount, payload.Metadata.TripCount,
		payload.Metadata.StationCount, payload.Metadata.ShotCount)
}

// Reduce renders the reduce command result.
func (r *Renderer) Reduce(payload ReduceReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	title := "CaveLoop reduction"
	if payload.Adjusted {
		title = "CaveLoop reduction (adjusted)"
	}
	if err := r.section(title); err != nil {
		return err
	}
	summary := payload.Metrics
	if err := r.printf(""+
		"store             %s\n"+
		"cave              %s\n"+
		"stations          %d\n"+
		"legs              %d active of %d\n"+
		"surveyed length   %s m\n"+
		"horizontal length %s m\n"+
		"vertical range    %s m\n"+
		"deepest station   %s at %s m\n"+
		"longest leg       %s at %s m\n",
		payload.Store, orDash(payload.Cave), summary.StationCount,
		summary.ActiveShotCount, summary.ShotCount,
		r.Length(summary.TotalLengthMeters), r.Length(summary.HorizontalLengthMeters),
		r.Length(summary.VerticalRangeMeters),
		orDash(summary.Deepest.Station), r.Length(summary.Deepest.Meters),
		orDash(summary.LongestShot), r.Length(summary.LongestShotMeters)); err != nil {
		return err
	}
	if payload.SnapshotHash != "" {
		if err := r.printf("snapshot digest   %s\n", payload.SnapshotHash); err != nil {
			return err
		}
	}
	if err := r.printf("\nstations\n"); err != nil {
		return err
	}
	table := r.newTable()
	if _, err := fmt.Fprintln(table, "STATION\tEAST\tNORTH\tUP\tDEPTH\tCOMPONENT\tFLAGS"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, station := range payload.Stations {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			station.Station, r.Length(station.East), r.Length(station.North), r.Length(station.Up),
			r.Length(station.DepthMeters), station.Component, joinList(station.Flags)); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	if err := flush(table); err != nil {
		return err
	}
	if err := r.printf("\nlegs\n"); err != nil {
		return err
	}
	legs := r.newTable()
	if _, err := fmt.Fprintln(legs, "SHOT\tFROM\tTO\tLENGTH\tAZIMUTH\tCOMPASS\tINCLINATION\tBACKSIGHT\tOK"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, leg := range payload.Legs {
		if _, err := fmt.Fprintf(legs, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			leg.Shot, leg.From, leg.To, r.Length(leg.DistanceMeters), r.Angle(leg.AzimuthDeg),
			leg.Compass, r.Angle(leg.InclinationDeg), yesNo(leg.Backsight), yesNo(leg.WithinTolerance)); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	if err := flush(legs); err != nil {
		return err
	}
	return r.issues(payload.Issues)
}

// Adjust renders the adjust command result.
func (r *Renderer) Adjust(payload AdjustReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	if err := r.section("CaveLoop closure adjustment"); err != nil {
		return err
	}
	adjustment := payload.Adjustment
	if err := r.printf(""+
		"store              %s\n"+
		"cave               %s\n"+
		"enabled            %s\n"+
		"passes             %d\n"+
		"converged          %s\n"+
		"largest residual   %s m\n"+
		"adjusted legs      %d\n"+
		"total correction   %s m\n"+
		"vertical adjusted  %s\n"+
		"loops before       %d failing of %d\n"+
		"loops after        %d failing of %d\n",
		payload.Store, orDash(payload.Cave), yesNo(adjustment.Enabled), adjustment.Passes,
		yesNo(adjustment.Converged), r.Length(adjustment.MaxResidualMeters), adjustment.AdjustedLegs,
		r.Length(adjustment.TotalCorrectionMeters), yesNo(adjustment.VerticalDistributed),
		payload.LoopsBefore.FailingCount, len(payload.LoopsBefore.Loops),
		payload.LoopsAfter.FailingCount, len(payload.LoopsAfter.Loops)); err != nil {
		return err
	}
	if payload.SnapshotHash != "" {
		if err := r.printf("snapshot digest    %s\n", payload.SnapshotHash); err != nil {
			return err
		}
	}
	if len(adjustment.Residuals) > 0 {
		if err := r.printf("\nloop residuals\n"); err != nil {
			return err
		}
		table := r.newTable()
		if _, err := fmt.Fprintln(table, "LOOP\tBEFORE\tAFTER\tBEFORE PPM\tAFTER PPM"); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		for _, residual := range adjustment.Residuals {
			if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", residual.LoopID,
				r.Length(residual.BeforeMeters), r.Length(residual.AfterMeters),
				units.Format(residual.BeforePPM, 1), units.Format(residual.AfterPPM, 1)); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}
		}
		if err := flush(table); err != nil {
			return err
		}
	}
	if len(adjustment.Adjustments) > 0 {
		if err := r.printf("\nleg corrections\n"); err != nil {
			return err
		}
		table := r.newTable()
		if _, err := fmt.Fprintln(table, "SHOT\tFROM\tTO\tLENGTH\tCORRECTION\tPPM\tAZIMUTH SHIFT\tLOOPS"); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		for _, correction := range adjustment.Adjustments {
			if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				correction.ShotKey, correction.From, correction.To, r.Length(correction.LengthMeters),
				r.Length(correction.MagnitudeMeters), units.Format(correction.RelativePPM, 1),
				r.Angle(correction.AzimuthShiftDeg), joinList(correction.Loops)); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}
		}
		if err := flush(table); err != nil {
			return err
		}
	}
	return r.issues(payload.Issues)
}

// Network renders the network command result.
func (r *Renderer) Network(payload NetworkReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	if err := r.section("CaveLoop network topology"); err != nil {
		return err
	}
	analysis := payload.Analysis
	if err := r.printf(""+
		"store            %s\n"+
		"cave             %s\n"+
		"stations         %d\n"+
		"legs             %d\n"+
		"total length     %s m\n"+
		"components       %d\n"+
		"junctions        %d\n"+
		"dangling ends    %d\n"+
		"isolated         %d\n"+
		"duplicate legs   %d\n"+
		"name collisions  %d\n",
		payload.Store, orDash(payload.Cave), analysis.StationCount, analysis.EdgeCount,
		r.Length(analysis.TotalLength), len(analysis.Components), len(analysis.Junctions),
		len(analysis.DeadEnds), len(analysis.Isolated), len(analysis.Duplicates),
		len(analysis.NameCollisions)); err != nil {
		return err
	}
	if err := r.printf("\ncomponents\n"); err != nil {
		return err
	}
	table := r.newTable()
	if _, err := fmt.Fprintln(table, "ID\tANCHOR\tANCHORED\tSTATIONS\tLEGS\tLENGTH\tCONTROL"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, component := range analysis.Components {
		if _, err := fmt.Fprintf(table, "%d\t%s\t%s\t%d\t%d\t%s\t%s\n", component.ID, component.Anchor,
			yesNo(component.Anchored), len(component.Stations), component.EdgeCount,
			r.Length(component.LengthMeters), joinList(component.ControlPoints)); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	if err := flush(table); err != nil {
		return err
	}
	if len(analysis.Junctions) > 0 {
		if err := r.printf("\njunctions\n"); err != nil {
			return err
		}
		junctions := r.newTable()
		if _, err := fmt.Fprintln(junctions, "STATION\tDEGREE\tPASSAGES"); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		for _, junction := range analysis.Junctions {
			if _, err := fmt.Fprintf(junctions, "%s\t%d\t%s\n", junction.Station, junction.Degree, joinList(junction.Passages)); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}
		}
		if err := flush(junctions); err != nil {
			return err
		}
	}
	if len(analysis.DeadEnds) > 0 {
		if err := r.printf("\ndangling passages\n"); err != nil {
			return err
		}
		deadEnds := r.newTable()
		if _, err := fmt.Fprintln(deadEnds, "STATION\tFROM\tVIA\tLENGTH\tENTRANCE"); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		for _, deadEnd := range analysis.DeadEnds {
			if _, err := fmt.Fprintf(deadEnds, "%s\t%s\t%s\t%s\t%s\n", deadEnd.Station, deadEnd.FromStation,
				deadEnd.ViaShot, r.Length(deadEnd.LengthMeters), yesNo(deadEnd.Entrance)); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}
		}
		if err := flush(deadEnds); err != nil {
			return err
		}
	}
	if len(analysis.Duplicates) > 0 {
		if err := r.printf("\nduplicate legs\n"); err != nil {
			return err
		}
		duplicates := r.newTable()
		if _, err := fmt.Fprintln(duplicates, "FROM\tTO\tSPREAD\tSHOTS"); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		for _, duplicate := range analysis.Duplicates {
			if _, err := fmt.Fprintf(duplicates, "%s\t%s\t%s\t%s\n", duplicate.From, duplicate.To,
				r.Length(duplicate.SpreadM), joinList(duplicate.Shots)); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}
		}
		if err := flush(duplicates); err != nil {
			return err
		}
	}
	return r.issues(payload.Issues)
}

// Loops renders the loops command result.
func (r *Renderer) Loops(payload LoopReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	if err := r.section("CaveLoop loop closures"); err != nil {
		return err
	}
	if err := r.printf(""+
		"store             %s\n"+
		"cave              %s\n"+
		"adjusted          %s\n"+
		"independent loops %d\n"+
		"failing loops     %d\n"+
		"worst loop        %s at %s m\n",
		payload.Store, orDash(payload.Cave), yesNo(payload.Adjusted),
		payload.Loops.IndependentCount, payload.Loops.FailingCount,
		orDash(payload.Loops.WorstLoop), r.Length(payload.Loops.WorstErrorMeters)); err != nil {
		return err
	}
	if len(payload.Loops.Loops) == 0 {
		return r.printf("\nthe survey contains no closed loop\n")
	}
	if err := r.printf("\nloops\n"); err != nil {
		return err
	}
	table := r.newTable()
	if _, err := fmt.Fprintln(table, "LOOP\tCHORD\tLEGS\tLENGTH\tHORIZONTAL\tVERTICAL\tTOTAL\tPPM\tOK\tFAILURES"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, loop := range payload.Loops.Loops {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			loop.ID, loop.ChordShot, len(loop.Legs), r.Length(loop.LengthMeters),
			r.Length(loop.HorizontalErrorMeters), r.Length(loop.VerticalErrorMeters),
			r.Length(loop.TotalErrorMeters), units.Format(loop.ErrorPPM, 1),
			yesNo(loop.WithinTolerance), joinList(loop.Failures)); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	if err := flush(table); err != nil {
		return err
	}
	if err := r.printf("\nloop circuits\n"); err != nil {
		return err
	}
	circuits := r.newTable()
	if _, err := fmt.Fprintln(circuits, "LOOP\tSTATIONS"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, loop := range payload.Loops.Loops {
		if _, err := fmt.Fprintf(circuits, "%s\t%s\n", loop.ID, strings.Join(loop.Stations, " -> ")); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	if err := flush(circuits); err != nil {
		return err
	}
	return r.issues(payload.Issues)
}

// Blunders renders the blunders command result.
func (r *Renderer) Blunders(payload BlunderReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	if err := r.section("CaveLoop blunder scan"); err != nil {
		return err
	}
	if err := r.printf("store     %s\ncave      %s\nenabled   %s\nfindings  %d\n",
		payload.Store, orDash(payload.Cave), yesNo(payload.Enabled), len(payload.Findings)); err != nil {
		return err
	}
	if len(payload.Counts) > 0 {
		if err := r.printf("\ncounts by code\n"); err != nil {
			return err
		}
		table := r.newTable()
		if _, err := fmt.Fprintln(table, "CODE\tCOUNT"); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		for _, code := range sortedKeys(payload.Counts) {
			if _, err := fmt.Fprintf(table, "%s\t%d\n", code, payload.Counts[code]); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}
		}
		if err := flush(table); err != nil {
			return err
		}
	}
	if len(payload.Findings) == 0 {
		return r.printf("\nno suspected blunder found\n")
	}
	if err := r.printf("\nfindings\n"); err != nil {
		return err
	}
	table := r.newTable()
	if _, err := fmt.Fprintln(table, "CODE\tSEVERITY\tSUBJECT\tLOOP\tSCORE\tMESSAGE"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, finding := range payload.Findings {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", finding.Code, finding.Severity,
			finding.Subject, orDash(finding.Loop), units.Format(finding.Score, 3), finding.Message); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	if err := flush(table); err != nil {
		return err
	}
	if err := r.printf("\nsuggestions\n"); err != nil {
		return err
	}
	suggestions := r.newTable()
	if _, err := fmt.Fprintln(suggestions, "SUBJECT\tSUGGESTION\tEVIDENCE"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, finding := range payload.Findings {
		if finding.Suggestion == "" {
			continue
		}
		if _, err := fmt.Fprintf(suggestions, "%s\t%s\t%s\n", finding.Subject, finding.Suggestion, joinList(finding.Evidence)); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	return flush(suggestions)
}

// Full renders the report command result.
func (r *Renderer) Full(payload FullReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	if err := r.section("CaveLoop survey report"); err != nil {
		return err
	}
	summary := payload.Metrics
	if err := r.printf(""+
		"store              %s\n"+
		"cave               %s\n"+
		"region             %s\n"+
		"adjusted           %s\n"+
		"stations           %d\n"+
		"legs               %d active of %d\n"+
		"surveyed length    %s m\n"+
		"horizontal length  %s m\n"+
		"mean leg           %s m\n"+
		"median leg         %s m\n"+
		"vertical range     %s m\n"+
		"maximum depth      %s m\n"+
		"backsight coverage %s\n",
		payload.Store, orDash(payload.Cave), orDash(payload.Region), yesNo(payload.Adjusted),
		summary.StationCount, summary.ActiveShotCount, summary.ShotCount,
		r.Length(summary.TotalLengthMeters), r.Length(summary.HorizontalLengthMeters),
		r.Length(summary.MeanShotMeters), r.Length(summary.MedianShotMeters),
		r.Length(summary.VerticalRangeMeters), r.Length(summary.MaxDepthMeters),
		percent(summary.BacksightCoverage)); err != nil {
		return err
	}
	if err := r.printf("\ntopology\n"); err != nil {
		return err
	}
	topology := payload.Topology
	if err := r.printf(""+
		"components         %d\n"+
		"junctions          %d\n"+
		"dangling passages  %d\n"+
		"isolated stations  %d\n"+
		"duplicate legs     %d\n"+
		"name collisions    %d\n",
		topology.Components, topology.Junctions, topology.DeadEnds,
		topology.Isolated, topology.Duplicates, topology.NameCollisions); err != nil {
		return err
	}
	if err := r.printf("\nclosure\n"); err != nil {
		return err
	}
	closure := payload.Closure
	if err := r.printf(""+
		"independent loops  %d\n"+
		"failing loops      %d\n"+
		"worst loop         %s at %s m\n"+
		"adjusted legs      %d\n"+
		"total correction   %s m\n"+
		"converged          %s\n",
		closure.Loops, closure.Failing, orDash(closure.WorstLoop),
		r.Length(closure.WorstErrorMeters), closure.AdjustedLegs,
		r.Length(closure.TotalCorrectionMeters), yesNo(closure.Converged)); err != nil {
		return err
	}
	if err := r.printf("\nextremes\n"); err != nil {
		return err
	}
	extremes := r.newTable()
	if _, err := fmt.Fprintln(extremes, "SUBJECT\tVALUE"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, key := range sortedStringKeys(payload.Extremes) {
		if _, err := fmt.Fprintf(extremes, "%s\t%s\n", key, orDash(payload.Extremes[key])); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	if err := flush(extremes); err != nil {
		return err
	}
	if err := r.printf("\ntrips\n"); err != nil {
		return err
	}
	trips := r.newTable()
	if _, err := fmt.Fprintln(trips, "TRIP\tDATE\tSHOTS\tSTATIONS\tLENGTH\tGAIN\tDROP\tMIN INC\tMAX INC\tLONGEST"); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	for _, trip := range payload.Trips {
		if _, err := fmt.Fprintf(trips, "%s\t%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			trip.TripID, orDash(trip.Date), trip.ShotCount, trip.StationCount,
			r.Length(trip.LengthMeters), r.Length(trip.VerticalGainMeters), r.Length(trip.VerticalDropMeters),
			r.Angle(trip.MinInclinationDeg), r.Angle(trip.MaxInclinationDeg), orDash(trip.LongestShot)); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}
	if err := flush(trips); err != nil {
		return err
	}
	if len(payload.Blunders) > 0 {
		if err := r.printf("\nblunder counts\n"); err != nil {
			return err
		}
		table := r.newTable()
		if _, err := fmt.Fprintln(table, "CODE\tCOUNT"); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		for _, code := range sortedKeys(payload.Blunders) {
			if _, err := fmt.Fprintf(table, "%s\t%d\n", code, payload.Blunders[code]); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}
		}
		if err := flush(table); err != nil {
			return err
		}
	}
	return r.issues(payload.Issues)
}

// Verify renders the verify command result.
func (r *Renderer) Verify(payload VerifyReport) error {
	if r.IsJSON() {
		return r.writeJSON(payload)
	}
	if err := r.section("CaveLoop store verification"); err != nil {
		return err
	}
	if err := r.printf(""+
		"store             %s\n"+
		"audit entries     %d\n"+
		"audit head        %s\n"+
		"audit chain       %s\n"+
		"records readable  %s\n"+
		"ledger digest     %s\n"+
		"ledger matches    %s\n"+
		"snapshot digest   %s\n"+
		"snapshot matches  %s\n"+
		"verdict           %s\n",
		payload.Store, payload.Audit.EntryCount, orDash(payload.Audit.Head),
		verdict(payload.Audit.Valid), yesNo(payload.RecordsReadble),
		orDash(payload.LedgerDigest), yesNo(payload.LedgerMatches),
		orDash(payload.SnapshotDigest), yesNo(payload.SnapshotMatch),
		verdict(payload.Valid)); err != nil {
		return err
	}
	if payload.Audit.Reason != "" {
		if err := r.printf("audit problem     %s\n", payload.Audit.Reason); err != nil {
			return err
		}
	}
	if len(payload.Problems) == 0 {
		return r.printf("\nno problem detected\n")
	}
	if err := r.printf("\nproblems\n"); err != nil {
		return err
	}
	for index, problem := range payload.Problems {
		if err := r.printf("%s %s\n", strconv.Itoa(index+1)+".", problem); err != nil {
			return err
		}
	}
	return nil
}

// flush finalises a table and maps the error.
func flush(table *tabwriter.Writer) error {
	if err := table.Flush(); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return nil
}

// orDash renders an empty string as a dash.
func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

// verdict renders a boolean verdict.
func verdict(value bool) string {
	if value {
		return "ok"
	}
	return "failed"
}

// percent renders a ratio as a percentage with one decimal.
func percent(ratio float64) string {
	return units.Format(ratio*100, 1) + "%"
}

// sortedKeys returns the sorted keys of an integer map.
func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

// sortedStringKeys returns the sorted keys of a string map.
func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

// sortStrings orders keys lexicographically so map backed tables render in a
// stable sequence.
func sortStrings(values []string) { sort.Strings(values) }
