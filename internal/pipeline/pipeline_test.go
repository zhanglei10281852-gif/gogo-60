package pipeline

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"CaveLoop/internal/config"
	"CaveLoop/internal/model"
	"CaveLoop/internal/store"
)

func loopSurvey() model.Survey {
	return model.Survey{
		Cave:   "Pipeline Cave",
		Region: "Test Region",
		Trips: []model.Trip{
			{
				ID: "T1", Date: "2031-06-01", LengthUnit: "m", AngleUnit: "deg",
				Stations: []model.Station{
					{Name: "A", Flags: []string{"fixed", "entrance"}, Fixed: &model.FixedCoordinate{Unit: "m"}},
					{Name: "B"}, {Name: "C"}, {Name: "D"},
				},
				Shots: []model.Shot{
					{ID: "S1", From: "A", To: "B", Distance: 10, Azimuth: 90, Inclination: 0},
					{ID: "S2", From: "B", To: "C", Distance: 10, Azimuth: 0, Inclination: 0},
					{ID: "S3", From: "C", To: "D", Distance: 10, Azimuth: 270, Inclination: 0},
					{ID: "S4", From: "D", To: "A", Distance: 10.4, Azimuth: 180, Inclination: 0},
				},
			},
		},
	}
}

func TestRunProducesEveryStage(t *testing.T) {
	outcome, err := Run(loopSurvey(), config.Default(), Options{})
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if len(outcome.Reduced.Shots) != 4 {
		t.Fatalf("reduction produced %d legs", len(outcome.Reduced.Shots))
	}
	if len(outcome.Graph.Edges) != 4 || len(outcome.Analysis.Components) != 1 {
		t.Fatalf("graph is %d legs in %d components", len(outcome.Graph.Edges), len(outcome.Analysis.Components))
	}
	if len(outcome.Layout.Positions) != 4 {
		t.Fatalf("layout holds %d positions", len(outcome.Layout.Positions))
	}
	if len(outcome.Loops.Loops) != 1 {
		t.Fatalf("loops are %+v", outcome.Loops.Loops)
	}
	if outcome.Adjusted {
		t.Fatal("adjustment ran without being requested")
	}
	if outcome.Metrics.TotalLengthMeters <= 0 {
		t.Fatal("metrics were not computed")
	}
	if outcome.FinalLayout().Positions[0].Station != outcome.Layout.Positions[0].Station {
		t.Fatal("FinalLayout should return the unadjusted layout")
	}
	if len(outcome.FinalLoops().Loops) != 1 {
		t.Fatal("FinalLoops should return the unadjusted loops")
	}
}

func TestRunWithAdjustmentClosesTheLoop(t *testing.T) {
	outcome, err := Run(loopSurvey(), config.Default(), Options{Adjust: true, DetectBlunders: true})
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if !outcome.Adjusted {
		t.Fatal("the adjustment stage did not run")
	}
	before := outcome.Loops.Loops[0].TotalErrorMeters
	after := outcome.AdjustedLoops.Loops[0].TotalErrorMeters
	if before <= 0.3 {
		t.Fatalf("the fixture misclosure is only %v", before)
	}
	if after > 1e-6 {
		t.Fatalf("the adjusted loop closes to %v", after)
	}
	if outcome.Adjustment.AdjustedLegs != 4 {
		t.Fatalf("adjusted legs are %d", outcome.Adjustment.AdjustedLegs)
	}
	if !outcome.Blunders.Enabled {
		t.Fatal("blunder detection did not run")
	}
	if outcome.FinalLoops().Loops[0].TotalErrorMeters != after {
		t.Fatal("FinalLoops should return the adjusted loops")
	}
}

func TestRunRejectsInvalidSurvey(t *testing.T) {
	survey := loopSurvey()
	survey.Cave = ""
	if _, err := Run(survey, config.Default(), Options{}); err == nil {
		t.Fatal("an invalid survey was accepted")
	}
}

func TestRunRejectsConflictingControl(t *testing.T) {
	survey := loopSurvey()
	survey.Trips = append(survey.Trips, model.Trip{
		ID: "T2", LengthUnit: "m", AngleUnit: "deg",
		Stations: []model.Station{
			{Name: "A", Flags: []string{"fixed"}, Fixed: &model.FixedCoordinate{East: 500, Unit: "m"}},
			{Name: "B"},
		},
		Shots: []model.Shot{{ID: "S9", From: "A", To: "B", Distance: 1, Azimuth: 0, Inclination: 0}},
	})
	if _, err := Run(survey, config.Default(), Options{}); err == nil {
		t.Fatal("conflicting control coordinates were accepted")
	}
}

func TestRunIsDeterministic(t *testing.T) {
	first, err := Run(loopSurvey(), config.Default(), Options{Adjust: true, DetectBlunders: true})
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	second, err := Run(loopSurvey(), config.Default(), Options{Adjust: true, DetectBlunders: true})
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	left := BuildSnapshot(first, config.Default())
	right := BuildSnapshot(second, config.Default())
	if !reflect.DeepEqual(left, right) {
		t.Fatal("two identical runs produced different snapshots")
	}
}

func TestBuildSnapshotContent(t *testing.T) {
	cfg := config.Default()
	outcome, err := Run(loopSurvey(), cfg, Options{Adjust: true, DetectBlunders: true})
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	snapshot := BuildSnapshot(outcome, cfg)
	if snapshot.Schema != SnapshotSchema || snapshot.Generator != store.Generator {
		t.Fatalf("snapshot header is %+v", snapshot)
	}
	if snapshot.Cave != "Pipeline Cave" || !snapshot.Adjusted {
		t.Fatalf("snapshot identity is %q adjusted=%v", snapshot.Cave, snapshot.Adjusted)
	}
	if len(snapshot.Stations) != 4 || len(snapshot.Legs) != 4 {
		t.Fatalf("snapshot holds %d stations and %d legs", len(snapshot.Stations), len(snapshot.Legs))
	}
	anchor := snapshot.Stations[0]
	if anchor.Name != "A" || !anchor.Anchor || !anchor.Control {
		t.Fatalf("anchor station is %+v", anchor)
	}
	if len(anchor.Flags) == 0 || len(anchor.Trips) == 0 {
		t.Fatalf("anchor metadata is %+v", anchor)
	}
	leg := snapshot.Legs[0]
	if leg.Shot != "T1/S1" || leg.DistanceMeters != 10 {
		t.Fatalf("first leg is %+v", leg)
	}
	if leg.Vector == leg.AdjustedVector {
		t.Fatal("the adjusted vector should differ after distribution")
	}
	if math.Abs(leg.AzimuthDeg-90) > 1e-9 {
		t.Fatalf("leg azimuth is %v", leg.AzimuthDeg)
	}
	if snapshot.Extremes["longestShot"] != "T1/S4" {
		t.Fatalf("extremes are %v", snapshot.Extremes)
	}
	if snapshot.Summary["totalLength"] <= 0 {
		t.Fatalf("summary is %v", snapshot.Summary)
	}
	if snapshot.Tolerances != cfg.Tolerances || snapshot.Settings != cfg.Adjustment {
		t.Fatal("the snapshot did not record the settings in force")
	}
	if snapshot.Counts == nil || snapshot.Blunders == nil {
		t.Fatal("blunder fields should never be nil in a snapshot")
	}
}

func TestPersistWritesSnapshotAndAudit(t *testing.T) {
	cfg := config.Default()
	handle, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("Open returned %v", err)
	}
	survey := loopSurvey()
	if _, err := handle.AppendRecords(survey.Records()); err != nil {
		t.Fatalf("AppendRecords returned %v", err)
	}
	outcome, err := Run(survey, cfg, Options{Adjust: true})
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	metadata, entry, err := Persist(handle, outcome, cfg, "adjust")
	if err != nil {
		t.Fatalf("Persist returned %v", err)
	}
	if metadata.SnapshotDigest == "" || metadata.LedgerDigest == "" {
		t.Fatalf("metadata is %+v", metadata)
	}
	if metadata.LastAction != "adjust" || metadata.TripCount != 1 {
		t.Fatalf("metadata is %+v", metadata)
	}
	if entry.Seq != 1 || entry.Action != "adjust" {
		t.Fatalf("audit entry is %+v", entry)
	}
	if metadata.AuditHead != entry.Hash {
		t.Fatal("metadata does not point at the audit head")
	}
	var reloaded Snapshot
	if err := handle.ReadJSON(store.SnapshotFile, &reloaded); err != nil {
		t.Fatalf("the snapshot cannot be decoded strictly: %v", err)
	}
	if reloaded.Cave != "Pipeline Cave" || len(reloaded.Stations) != 4 {
		t.Fatalf("reloaded snapshot is %+v", reloaded)
	}
	verification, err := handle.VerifyAudit()
	if err != nil || !verification.Valid {
		t.Fatalf("audit verification is %+v, %v", verification, err)
	}
	digest, err := handle.FileDigest(store.SnapshotFile)
	if err != nil {
		t.Fatalf("FileDigest returned %v", err)
	}
	if digest != metadata.SnapshotDigest {
		t.Fatal("the stored snapshot digest does not match the file")
	}
}

func TestCollectIssuesDeduplicates(t *testing.T) {
	issue := model.Issue{Severity: model.SeverityWarning, Code: "x", Path: "p", Message: "m"}
	deduped := dedupeIssues(model.Issues{issue, issue}.Sorted())
	if len(deduped) != 1 {
		t.Fatalf("dedupeIssues produced %v", deduped)
	}
}
