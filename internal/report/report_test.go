package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"CaveLoop/internal/adjust"
	"CaveLoop/internal/blunder"
	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/loops"
	"CaveLoop/internal/metrics"
	"CaveLoop/internal/model"
	"CaveLoop/internal/network"
	"CaveLoop/internal/pipeline"
	"CaveLoop/internal/store"
)

func textRenderer() (*Renderer, *bytes.Buffer) {
	buffer := &bytes.Buffer{}
	return New(buffer, config.Default()), buffer
}

func jsonRenderer() (*Renderer, *bytes.Buffer) {
	cfg := config.Default()
	cfg.Output.Format = config.OutputJSON
	buffer := &bytes.Buffer{}
	return New(buffer, cfg), buffer
}

func sampleIssues() model.Issues {
	return model.Issues{
		{Severity: model.SeverityError, Code: "boom", Path: "survey", Message: "something broke"},
		{Severity: model.SeverityWarning, Code: "hmm", Path: "survey.trips[T1]", Message: "worth a look"},
	}
}

func TestRendererFormatting(t *testing.T) {
	renderer, _ := textRenderer()
	if renderer.IsJSON() {
		t.Fatal("the default renderer should emit text")
	}
	if got := renderer.Length(1.23456); got != "1.235" {
		t.Fatalf("Length = %q", got)
	}
	if got := renderer.Angle(1.23456); got != "1.23" {
		t.Fatalf("Angle = %q", got)
	}
	jsonRender, _ := jsonRenderer()
	if !jsonRender.IsJSON() {
		t.Fatal("the JSON renderer should report JSON")
	}
}

func TestValidateText(t *testing.T) {
	renderer, buffer := textRenderer()
	payload := ValidateReport{
		Command: "validate", Source: "survey.json", Cave: "Test Cave", Region: "Region",
		Instruments: 1, Trips: 2, Stations: 5, Shots: 4, Valid: false,
		Counts: Counts(sampleIssues()), Issues: sampleIssues().Sorted(),
	}
	if err := renderer.Validate(payload); err != nil {
		t.Fatalf("Validate returned %v", err)
	}
	output := buffer.String()
	for _, want := range []string{"CaveLoop validation", "Test Cave", "verdict      failed", "boom", "worth a look"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
	if payload.Counts.Errors != 1 || payload.Counts.Warnings != 1 {
		t.Fatalf("counts are %+v", payload.Counts)
	}
}

func TestValidateJSON(t *testing.T) {
	renderer, buffer := jsonRenderer()
	payload := ValidateReport{Command: "validate", Cave: "Test Cave", Valid: true, Issues: model.Issues{}}
	if err := renderer.Validate(payload); err != nil {
		t.Fatalf("Validate returned %v", err)
	}
	var decoded ValidateReport
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("the JSON payload does not decode: %v", err)
	}
	if decoded.Cave != "Test Cave" || !decoded.Valid {
		t.Fatalf("decoded %+v", decoded)
	}
}

func TestNoFindingsMessage(t *testing.T) {
	renderer, buffer := textRenderer()
	if err := renderer.Validate(ValidateReport{Command: "validate", Valid: true}); err != nil {
		t.Fatalf("Validate returned %v", err)
	}
	if !strings.Contains(buffer.String(), "no findings") {
		t.Fatalf("output is\n%s", buffer.String())
	}
}

func TestImportText(t *testing.T) {
	renderer, buffer := textRenderer()
	payload := ImportReport{
		Command: "import", Source: "survey.json", Store: "/tmp/store", Cave: "Test Cave",
		Appended: 3, TotalRecords: 7, PayloadDigest: "aa", LedgerDigest: "bb",
		AuditSeq: 2, AuditHash: "cc",
		Metadata: store.Metadata{InstrumentCount: 1, TripCount: 2, StationCount: 5, ShotCount: 4},
	}
	if err := renderer.Import(payload); err != nil {
		t.Fatalf("Import returned %v", err)
	}
	for _, want := range []string{"CaveLoop import", "appended records  3", "ledger records    7", "audit head        cc"} {
		if !strings.Contains(buffer.String(), want) {
			t.Fatalf("output does not mention %q:\n%s", want, buffer.String())
		}
	}
}

func TestReduceText(t *testing.T) {
	renderer, buffer := textRenderer()
	payload := ReduceReport{
		Command: "reduce", Store: "/tmp/store", Cave: "Test Cave",
		Metrics: metrics.Summary{
			StationCount: 2, ShotCount: 1, ActiveShotCount: 1, TotalLengthMeters: 10,
			HorizontalLengthMeters: 9.8, VerticalRangeMeters: 1.2,
			Deepest:     metrics.StationExtreme{Station: "B", Meters: -1.2},
			LongestShot: "T1/S1", LongestShotMeters: 10,
		},
		Stations: []StationLine{{Station: "A", Component: 1, Flags: []string{"fixed"}}, {Station: "B", Up: -1.2, DepthMeters: 1.2, Component: 1}},
		Legs: []LegLine{{Shot: "T1/S1", From: "A", To: "B", DistanceMeters: 10,
			AzimuthDeg: 90, InclinationDeg: -7, Compass: "E", Backsight: true, WithinTolerance: true}},
		Issues:       model.Issues{},
		SnapshotHash: "deadbeef",
	}
	if err := renderer.Reduce(payload); err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	output := buffer.String()
	for _, want := range []string{"CaveLoop reduction", "STATION", "SHOT", "T1/S1", "deadbeef", "fixed"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
	payload.Adjusted = true
	renderer, buffer = textRenderer()
	if err := renderer.Reduce(payload); err != nil {
		t.Fatalf("Reduce returned %v", err)
	}
	if !strings.Contains(buffer.String(), "(adjusted)") {
		t.Fatalf("the adjusted title is missing:\n%s", buffer.String())
	}
}

func TestAdjustText(t *testing.T) {
	renderer, buffer := textRenderer()
	payload := AdjustReport{
		Command: "adjust", Store: "/tmp/store", Cave: "Test Cave",
		Adjustment: adjust.Result{
			Enabled: true, Passes: 3, Converged: true, AdjustedLegs: 2,
			TotalCorrectionMeters: 0.2, VerticalDistributed: true,
			Adjustments: []adjust.ShotAdjustment{{
				ShotKey: "T1/S1", From: "A", To: "B", LengthMeters: 10,
				Correction: geom.Vector{East: 0.1}, MagnitudeMeters: 0.1,
				RelativePPM: 10000, Loops: []string{"L001"},
			}},
			Residuals: []adjust.LoopResidual{{LoopID: "L001", BeforeMeters: 0.2, AfterMeters: 0}},
		},
		LoopsBefore: loops.Result{Loops: []loops.Loop{{ID: "L001"}}, FailingCount: 1},
		LoopsAfter:  loops.Result{Loops: []loops.Loop{{ID: "L001"}}},
		Issues:      model.Issues{},
	}
	if err := renderer.Adjust(payload); err != nil {
		t.Fatalf("Adjust returned %v", err)
	}
	output := buffer.String()
	for _, want := range []string{"closure adjustment", "loop residuals", "leg corrections", "L001", "T1/S1"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
}

func TestNetworkText(t *testing.T) {
	renderer, buffer := textRenderer()
	analysis := network.Analysis{
		StationCount: 3, EdgeCount: 3, TotalLength: 30,
		Components: []network.Component{{ID: 1, Anchor: "A", Anchored: true, Stations: []string{"A", "B", "C"}, EdgeCount: 3, LengthMeters: 30, ControlPoints: []string{"A"}}},
		Junctions:  []network.Junction{{Station: "B", Degree: 3, Passages: []string{"T1/S1", "T1/S2", "T1/S3"}}},
		DeadEnds:   []network.DeadEnd{{Station: "C", FromStation: "B", ViaShot: "T1/S3", LengthMeters: 5, Entrance: false}},
		Duplicates: []network.DuplicateShot{{From: "A", To: "B", Shots: []string{"T1/S1", "T2/S9"}, SpreadM: 0.4}},
		Issues:     model.Issues{},
	}
	if err := renderer.Network(NetworkReport{Command: "network", Store: "/tmp", Cave: "Test Cave", Analysis: analysis, Issues: model.Issues{}}); err != nil {
		t.Fatalf("Network returned %v", err)
	}
	output := buffer.String()
	for _, want := range []string{"network topology", "components", "junctions", "dangling passages", "duplicate legs", "T2/S9"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
}

func TestLoopsText(t *testing.T) {
	renderer, buffer := textRenderer()
	payload := LoopReport{
		Command: "loops", Store: "/tmp", Cave: "Test Cave",
		Loops: loops.Result{
			IndependentCount: 1, FailingCount: 1, WorstLoop: "L001", WorstErrorMeters: 0.9,
			Loops: []loops.Loop{{
				ID: "L001", ChordShot: "T1/S4", Stations: []string{"A", "B", "A"},
				Legs: []loops.Leg{{ShotKey: "T1/S1"}}, LengthMeters: 40,
				TotalErrorMeters: 0.9, ErrorPPM: 22500, Failures: []string{"total-error"},
			}},
		},
		Issues: model.Issues{},
	}
	if err := renderer.Loops(payload); err != nil {
		t.Fatalf("Loops returned %v", err)
	}
	output := buffer.String()
	for _, want := range []string{"loop closures", "L001", "loop circuits", "A -> B -> A", "total-error"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
	renderer, buffer = textRenderer()
	if err := renderer.Loops(LoopReport{Command: "loops"}); err != nil {
		t.Fatalf("Loops returned %v", err)
	}
	if !strings.Contains(buffer.String(), "no closed loop") {
		t.Fatalf("output is\n%s", buffer.String())
	}
}

func TestBlundersText(t *testing.T) {
	renderer, buffer := textRenderer()
	payload := BlunderReport{
		Command: "blunders", Store: "/tmp", Cave: "Test Cave", Enabled: true,
		Findings: BuildFindings([]blunder.Finding{{
			Code: blunder.CodeReversedReading, Severity: model.SeverityError, Subject: "T1/S1",
			Score: 179, Message: "looks reversed", Suggestion: "check the notes",
			Evidence: []string{"foresight=90"},
		}}),
		Counts: map[string]int{blunder.CodeReversedReading: 1},
	}
	if err := renderer.Blunders(payload); err != nil {
		t.Fatalf("Blunders returned %v", err)
	}
	output := buffer.String()
	for _, want := range []string{"blunder scan", "counts by code", "looks reversed", "check the notes", "foresight=90"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
	renderer, buffer = textRenderer()
	if err := renderer.Blunders(BlunderReport{Command: "blunders", Enabled: true, Counts: map[string]int{}}); err != nil {
		t.Fatalf("Blunders returned %v", err)
	}
	if !strings.Contains(buffer.String(), "no suspected blunder") {
		t.Fatalf("output is\n%s", buffer.String())
	}
}

func TestFullText(t *testing.T) {
	renderer, buffer := textRenderer()
	payload := FullReport{
		Command: "report", Store: "/tmp", Cave: "Test Cave", Region: "Region", Adjusted: true,
		Metrics: metrics.Summary{
			StationCount: 4, ShotCount: 4, ActiveShotCount: 4, TotalLengthMeters: 40,
			HorizontalLengthMeters: 39, MeanShotMeters: 10, MedianShotMeters: 10,
			VerticalRangeMeters: 3, MaxDepthMeters: 3, BacksightCoverage: 0.5,
		},
		Topology: TopologySummary{Stations: 4, Legs: 4, Components: 1, Junctions: 1, DeadEnds: 1},
		Closure:  ClosureSummary{Loops: 1, WorstLoop: "L001", Converged: true, AdjustedLegs: 4},
		Blunders: map[string]int{"gross-length-outlier": 1},
		Issues:   model.Issues{},
		Trips: []metrics.TripStats{{
			TripID: "T1", Date: "2031-01-01", ShotCount: 4, StationCount: 4,
			LengthMeters: 40, LongestShot: "T1/S4",
		}},
		Extremes:   map[string]string{"deepestStation": "D", "longestShot": "T1/S4"},
		Tolerances: config.Default().Tolerances,
	}
	if err := renderer.Full(payload); err != nil {
		t.Fatalf("Full returned %v", err)
	}
	output := buffer.String()
	for _, want := range []string{"survey report", "topology", "closure", "extremes", "trips", "blunder counts", "50.0%"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
}

func TestVerifyText(t *testing.T) {
	renderer, buffer := textRenderer()
	payload := VerifyReport{
		Command: "verify", Store: "/tmp",
		Audit:          store.AuditVerification{EntryCount: 2, Head: "abc", Valid: false, BrokenAt: 2, Reason: "link mismatch"},
		LedgerDigest:   "aa",
		SnapshotDigest: "bb",
		Problems:       []string{"audit chain is broken"},
	}
	if err := renderer.Verify(payload); err != nil {
		t.Fatalf("Verify returned %v", err)
	}
	output := buffer.String()
	for _, want := range []string{"store verification", "audit chain       failed", "link mismatch", "1. audit chain is broken"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
	renderer, buffer = textRenderer()
	if err := renderer.Verify(VerifyReport{Command: "verify", Valid: true, Audit: store.AuditVerification{Valid: true}}); err != nil {
		t.Fatalf("Verify returned %v", err)
	}
	if !strings.Contains(buffer.String(), "no problem detected") {
		t.Fatalf("output is\n%s", buffer.String())
	}
}

func TestBuildersFromSnapshot(t *testing.T) {
	snapshot := pipeline.Snapshot{
		Stations: []pipeline.StationSnapshot{{
			Name: "A", Coordinate: geom.Vector{East: 1, North: 2, Up: 3},
			DepthMeters: 1, Component: 1, Flags: []string{"fixed"},
		}},
		Legs: []pipeline.LegSnapshot{{
			Shot: "T1/S1", From: "A", To: "B", DistanceMeters: 10,
			AzimuthDeg: 90, InclinationDeg: -5, Backsight: true, WithinTolerance: true,
		}},
	}
	stations := BuildStationLines(snapshot)
	if len(stations) != 1 || stations[0].East != 1 || stations[0].Flags[0] != "fixed" {
		t.Fatalf("station lines are %+v", stations)
	}
	legs := BuildLegLines(snapshot)
	if len(legs) != 1 || legs[0].Compass != "E" {
		t.Fatalf("leg lines are %+v", legs)
	}
}

func TestBuildSummaries(t *testing.T) {
	analysis := network.Analysis{
		StationCount: 3, EdgeCount: 4,
		Components:     []network.Component{{ID: 1}},
		Junctions:      []network.Junction{{Station: "A"}},
		DeadEnds:       []network.DeadEnd{{Station: "C"}},
		Isolated:       []string{"Z"},
		Duplicates:     []network.DuplicateShot{{From: "A", To: "B"}},
		NameCollisions: []network.NameCollision{{Normalized: "a"}},
	}
	topology := BuildTopology(analysis)
	if topology.Stations != 3 || topology.Legs != 4 || topology.Isolated != 1 || topology.NameCollisions != 1 {
		t.Fatalf("topology summary is %+v", topology)
	}
	closure := BuildClosure(
		loops.Result{Loops: []loops.Loop{{ID: "L001"}}, FailingCount: 1, WorstLoop: "L001", WorstErrorMeters: 0.4},
		adjust.Result{AdjustedLegs: 2, TotalCorrectionMeters: 0.4, Converged: true},
	)
	if closure.Loops != 1 || closure.Failing != 1 || !closure.Converged || closure.AdjustedLegs != 2 {
		t.Fatalf("closure summary is %+v", closure)
	}
}

func TestTextHelpers(t *testing.T) {
	if orDash("  ") != "-" || orDash("x") != "x" {
		t.Fatal("orDash misbehaved")
	}
	if verdict(true) != "ok" || verdict(false) != "failed" {
		t.Fatal("verdict misbehaved")
	}
	if yesNo(true) != "yes" || yesNo(false) != "no" {
		t.Fatal("yesNo misbehaved")
	}
	if joinList(nil) != "-" || joinList([]string{"a", "b"}) != "a,b" {
		t.Fatal("joinList misbehaved")
	}
	if percent(0.125) != "12.5%" {
		t.Fatalf("percent = %q", percent(0.125))
	}
	keys := sortedKeys(map[string]int{"b": 1, "a": 2})
	if keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("sortedKeys = %v", keys)
	}
	stringKeys := sortedStringKeys(map[string]string{"b": "1", "a": "2"})
	if stringKeys[0] != "a" {
		t.Fatalf("sortedStringKeys = %v", stringKeys)
	}
}
