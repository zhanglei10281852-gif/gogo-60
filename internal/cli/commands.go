package cli

import (
	"fmt"

	"CaveLoop/internal/model"
	"CaveLoop/internal/pipeline"
	"CaveLoop/internal/report"
	"CaveLoop/internal/store"
)

// runValidate implements the validate subcommand.
func runValidate(env *Environment, args []string) int {
	flags := newFlagSet(env, "validate")
	common := commonFlags{}
	common.bind(flags)
	input := flags.String("input", "", "survey document to validate")
	inputFormat := flags.String("input-format", "auto", "survey input format: json, jsonl or auto")
	strict := flags.Bool("strict", false, "treat warnings as a failure")
	code, proceed := parseFlags(env, flags, args)
	if !proceed {
		return code
	}
	if *input == "" {
		return fail(env, ExitError, fmt.Errorf("validate requires -input"))
	}
	format, err := model.ParseInputFormat(*inputFormat)
	if err != nil {
		return fail(env, ExitError, err)
	}
	current, code := openSession(env, common, false)
	if current == nil {
		return code
	}
	defer func() { _ = current.close() }()
	survey, err := model.LoadSurveyFile(*input, format)
	if err != nil {
		return fail(env, ExitRefused, err)
	}
	issues := model.Validate(survey, current.cfg.ModelDefaults())
	counts := report.Counts(issues)
	payload := report.ValidateReport{
		Command:     "validate",
		Source:      *input,
		Cave:        survey.Cave,
		Region:      survey.Region,
		Instruments: len(survey.Instruments),
		Trips:       len(survey.Trips),
		Stations:    len(survey.StationNames()),
		Shots:       survey.ShotCount(),
		Valid:       !issues.HasErrors(),
		Counts:      counts,
		Issues:      issues,
	}
	if err := current.renderer.Validate(payload); err != nil {
		return fail(env, ExitError, err)
	}
	if issues.HasErrors() {
		return ExitRefused
	}
	if *strict && counts.Warnings > 0 {
		fmt.Fprintf(env.Stderr, "caveloop: %d warning(s) rejected because -strict is set\n", counts.Warnings)
		return ExitRefused
	}
	return ExitOK
}

// runImport implements the import subcommand.
func runImport(env *Environment, args []string) int {
	flags := newFlagSet(env, "import")
	common := commonFlags{}
	common.bind(flags)
	input := flags.String("input", "", "survey document to append to the ledger")
	inputFormat := flags.String("input-format", "auto", "survey input format: json, jsonl or auto")
	code, proceed := parseFlags(env, flags, args)
	if !proceed {
		return code
	}
	if *input == "" {
		return fail(env, ExitError, fmt.Errorf("import requires -input"))
	}
	format, err := model.ParseInputFormat(*inputFormat)
	if err != nil {
		return fail(env, ExitError, err)
	}
	current, code := openSession(env, common, true)
	if current == nil {
		return code
	}
	defer func() { _ = current.close() }()
	survey, err := model.LoadSurveyFile(*input, format)
	if err != nil {
		return fail(env, ExitRefused, err)
	}
	issues := model.Validate(survey, current.cfg.ModelDefaults())
	if issues.HasErrors() {
		return fail(env, ExitRefused, fmt.Errorf("refusing to import an invalid survey: %w", issues))
	}
	records := survey.Records()
	appendReport, err := current.store.AppendRecords(records)
	if err != nil {
		return fail(env, ExitError, err)
	}
	entry, err := current.store.Record("import", store.LedgerFile,
		fmt.Sprintf("records=%d source=%s", appendReport.Appended, *input), appendReport.PayloadDigest)
	if err != nil {
		return fail(env, ExitError, err)
	}
	merged, err := current.store.LoadSurvey()
	if err != nil {
		return fail(env, ExitError, err)
	}
	verification, err := current.store.VerifyAudit()
	if err != nil {
		return fail(env, ExitError, err)
	}
	metadata := store.Metadata{
		Cave:            merged.Cave,
		Region:          merged.Region,
		RecordCount:     appendReport.TotalRecords,
		InstrumentCount: len(merged.Instruments),
		TripCount:       len(merged.Trips),
		StationCount:    len(merged.StationNames()),
		ShotCount:       merged.ShotCount(),
		LedgerDigest:    appendReport.LedgerDigest,
		AuditHead:       verification.Head,
		AuditEntries:    verification.EntryCount,
		LastAction:      "import",
	}
	if previous, err := current.store.LoadMetadata(); err == nil {
		metadata.SnapshotDigest = previous.SnapshotDigest
	}
	if err := current.store.WriteMetadata(metadata); err != nil {
		return fail(env, ExitError, err)
	}
	payload := report.ImportReport{
		Command:       "import",
		Source:        *input,
		Store:         current.store.Root(),
		Cave:          merged.Cave,
		Appended:      appendReport.Appended,
		TotalRecords:  appendReport.TotalRecords,
		PayloadDigest: appendReport.PayloadDigest,
		LedgerDigest:  appendReport.LedgerDigest,
		AuditSeq:      entry.Seq,
		AuditHash:     entry.Hash,
		Metadata:      metadata,
	}
	if err := current.renderer.Import(payload); err != nil {
		return fail(env, ExitError, err)
	}
	return ExitOK
}

// runReduce implements the reduce subcommand.
func runReduce(env *Environment, args []string) int {
	return runComputation(env, args, "reduce")
}

// runAdjust implements the adjust subcommand.
func runAdjust(env *Environment, args []string) int {
	return runComputation(env, args, "adjust")
}

// runComputation implements the two subcommands that write a snapshot.
func runComputation(env *Environment, args []string, action string) int {
	flags := newFlagSet(env, action)
	common := commonFlags{}
	common.bind(flags)
	noWrite := flags.Bool("no-write", false, "compute without updating the store snapshot")
	code, proceed := parseFlags(env, flags, args)
	if !proceed {
		return code
	}
	current, code := openSession(env, common, true)
	if current == nil {
		return code
	}
	defer func() { _ = current.close() }()
	options := pipeline.Options{Adjust: action == "adjust", DetectBlunders: false}
	outcome, err := loadAndRun(current, options)
	if err != nil {
		return fail(env, ExitRefused, err)
	}
	snapshot := pipeline.BuildSnapshot(outcome, current.cfg)
	snapshotDigest := ""
	auditHash := ""
	if !*noWrite {
		metadata, entry, err := pipeline.Persist(current.store, outcome, current.cfg, action)
		if err != nil {
			return fail(env, ExitError, err)
		}
		snapshotDigest = metadata.SnapshotDigest
		auditHash = entry.Hash
	}
	if action == "adjust" {
		payload := report.AdjustReport{
			Command:      action,
			Store:        current.store.Root(),
			Cave:         outcome.Reduced.Cave,
			Adjustment:   outcome.Adjustment,
			LoopsBefore:  outcome.Loops,
			LoopsAfter:   outcome.AdjustedLoops,
			Counts:       report.Counts(outcome.Issues),
			Issues:       outcome.Issues,
			SnapshotHash: snapshotDigest,
			AuditHash:    auditHash,
		}
		if err := current.renderer.Adjust(payload); err != nil {
			return fail(env, ExitError, err)
		}
		return ExitOK
	}
	payload := report.ReduceReport{
		Command:      action,
		Store:        current.store.Root(),
		Cave:         outcome.Reduced.Cave,
		Adjusted:     outcome.Adjusted,
		Metrics:      outcome.Metrics,
		Stations:     report.BuildStationLines(snapshot),
		Legs:         report.BuildLegLines(snapshot),
		Counts:       report.Counts(outcome.Issues),
		Issues:       outcome.Issues,
		SnapshotHash: snapshotDigest,
		AuditHash:    auditHash,
	}
	if err := current.renderer.Reduce(payload); err != nil {
		return fail(env, ExitError, err)
	}
	return ExitOK
}

// runNetwork implements the network subcommand.
func runNetwork(env *Environment, args []string) int {
	flags := newFlagSet(env, "network")
	common := commonFlags{}
	common.bind(flags)
	code, proceed := parseFlags(env, flags, args)
	if !proceed {
		return code
	}
	current, code := openSession(env, common, true)
	if current == nil {
		return code
	}
	defer func() { _ = current.close() }()
	outcome, err := loadAndRun(current, pipeline.Options{})
	if err != nil {
		return fail(env, ExitRefused, err)
	}
	payload := report.NetworkReport{
		Command:  "network",
		Store:    current.store.Root(),
		Cave:     outcome.Reduced.Cave,
		Analysis: outcome.Analysis,
		Counts:   report.Counts(outcome.Analysis.Issues),
		Issues:   outcome.Analysis.Issues,
	}
	if err := current.renderer.Network(payload); err != nil {
		return fail(env, ExitError, err)
	}
	return ExitOK
}

// runLoops implements the loops subcommand.
func runLoops(env *Environment, args []string) int {
	flags := newFlagSet(env, "loops")
	common := commonFlags{}
	common.bind(flags)
	adjusted := flags.Bool("adjusted", false, "report the closures of the adjusted network")
	code, proceed := parseFlags(env, flags, args)
	if !proceed {
		return code
	}
	current, code := openSession(env, common, true)
	if current == nil {
		return code
	}
	defer func() { _ = current.close() }()
	outcome, err := loadAndRun(current, pipeline.Options{Adjust: *adjusted})
	if err != nil {
		return fail(env, ExitRefused, err)
	}
	loopResult := outcome.FinalLoops()
	payload := report.LoopReport{
		Command:  "loops",
		Store:    current.store.Root(),
		Cave:     outcome.Reduced.Cave,
		Adjusted: outcome.Adjusted,
		Loops:    loopResult,
		Counts:   report.Counts(loopResult.Issues),
		Issues:   loopResult.Issues,
	}
	if err := current.renderer.Loops(payload); err != nil {
		return fail(env, ExitError, err)
	}
	return ExitOK
}

// runBlunders implements the blunders subcommand.
func runBlunders(env *Environment, args []string) int {
	flags := newFlagSet(env, "blunders")
	common := commonFlags{}
	common.bind(flags)
	failOnFinding := flags.Bool("fail-on-finding", false, "exit with code 2 when a blunder is suspected")
	code, proceed := parseFlags(env, flags, args)
	if !proceed {
		return code
	}
	current, code := openSession(env, common, true)
	if current == nil {
		return code
	}
	defer func() { _ = current.close() }()
	outcome, err := loadAndRun(current, pipeline.Options{DetectBlunders: true})
	if err != nil {
		return fail(env, ExitRefused, err)
	}
	payload := report.BlunderReport{
		Command:  "blunders",
		Store:    current.store.Root(),
		Cave:     outcome.Reduced.Cave,
		Enabled:  outcome.Blunders.Enabled,
		Findings: report.BuildFindings(outcome.Blunders.Findings),
		Counts:   outcome.Blunders.Counts,
	}
	if err := current.renderer.Blunders(payload); err != nil {
		return fail(env, ExitError, err)
	}
	if *failOnFinding && len(payload.Findings) > 0 {
		fmt.Fprintf(env.Stderr, "caveloop: %d suspected blunder(s): %s\n", len(payload.Findings), summariseCodes(payload.Counts))
		return ExitRefused
	}
	return ExitOK
}

// runReport implements the report subcommand.
func runReport(env *Environment, args []string) int {
	flags := newFlagSet(env, "report")
	common := commonFlags{}
	common.bind(flags)
	adjusted := flags.Bool("adjusted", true, "report the adjusted network")
	code, proceed := parseFlags(env, flags, args)
	if !proceed {
		return code
	}
	current, code := openSession(env, common, true)
	if current == nil {
		return code
	}
	defer func() { _ = current.close() }()
	outcome, err := loadAndRun(current, pipeline.Options{Adjust: *adjusted, DetectBlunders: true})
	if err != nil {
		return fail(env, ExitRefused, err)
	}
	snapshot := pipeline.BuildSnapshot(outcome, current.cfg)
	payload := report.FullReport{
		Command:    "report",
		Store:      current.store.Root(),
		Cave:       outcome.Reduced.Cave,
		Region:     outcome.Reduced.Region,
		Adjusted:   outcome.Adjusted,
		Metrics:    outcome.Metrics,
		Topology:   report.BuildTopology(outcome.Analysis),
		Closure:    report.BuildClosure(outcome.FinalLoops(), outcome.Adjustment),
		Blunders:   outcome.Blunders.Counts,
		Counts:     report.Counts(outcome.Issues),
		Issues:     outcome.Issues,
		Trips:      outcome.Metrics.Trips,
		Extremes:   snapshot.Extremes,
		Tolerances: current.cfg.Tolerances,
	}
	if payload.Blunders == nil {
		payload.Blunders = map[string]int{}
	}
	if err := current.renderer.Full(payload); err != nil {
		return fail(env, ExitError, err)
	}
	return ExitOK
}

// runVerify implements the verify subcommand.
func runVerify(env *Environment, args []string) int {
	flags := newFlagSet(env, "verify")
	common := commonFlags{}
	common.bind(flags)
	code, proceed := parseFlags(env, flags, args)
	if !proceed {
		return code
	}
	current, code := openSession(env, common, true)
	if current == nil {
		return code
	}
	defer func() { _ = current.close() }()
	payload := report.VerifyReport{
		Command: "verify",
		Store:   current.store.Root(),
		Valid:   true,
	}
	problems := make([]string, 0, 4)
	verification, err := current.store.VerifyAudit()
	if err != nil {
		return fail(env, ExitError, err)
	}
	payload.Audit = verification
	if !verification.Valid {
		payload.Valid = false
		problems = append(problems, "audit chain is broken: "+verification.Reason)
	}
	metadata, err := current.store.LoadMetadata()
	if err != nil {
		payload.Valid = false
		problems = append(problems, "metadata is unusable: "+err.Error())
	} else {
		payload.Metadata = metadata
	}
	ledgerDigest, err := current.store.FileDigest(store.LedgerFile)
	if err != nil {
		return fail(env, ExitError, err)
	}
	payload.LedgerDigest = ledgerDigest
	snapshotDigest, err := current.store.FileDigest(store.SnapshotFile)
	if err != nil {
		return fail(env, ExitError, err)
	}
	payload.SnapshotDigest = snapshotDigest
	payload.LedgerMatches = metadata.LedgerDigest != "" && metadata.LedgerDigest == ledgerDigest
	if !payload.LedgerMatches {
		payload.Valid = false
		problems = append(problems, "ledger digest does not match the recorded metadata")
	}
	payload.SnapshotMatch = metadata.SnapshotDigest != "" && metadata.SnapshotDigest == snapshotDigest
	if !payload.SnapshotMatch {
		payload.Valid = false
		problems = append(problems, "snapshot digest does not match the recorded metadata, run reduce or adjust")
	}
	records, err := current.store.LoadRecords()
	if err != nil {
		payload.Valid = false
		problems = append(problems, "ledger records are unreadable: "+err.Error())
	} else {
		payload.RecordsReadble = true
		if metadata.RecordCount != 0 && metadata.RecordCount != len(records) {
			payload.Valid = false
			problems = append(problems, fmt.Sprintf("metadata counts %d records but the ledger holds %d", metadata.RecordCount, len(records)))
		}
	}
	if snapshotDigest != "" {
		var snapshot pipeline.Snapshot
		if err := current.store.ReadJSON(store.SnapshotFile, &snapshot); err != nil {
			payload.Valid = false
			problems = append(problems, "snapshot is unreadable: "+err.Error())
		} else if snapshot.Schema != pipeline.SnapshotSchema {
			payload.Valid = false
			problems = append(problems, fmt.Sprintf("snapshot declares schema %d, this build understands %d", snapshot.Schema, pipeline.SnapshotSchema))
		}
	}
	payload.Problems = problems
	if err := current.renderer.Verify(payload); err != nil {
		return fail(env, ExitError, err)
	}
	if !payload.Valid {
		return ExitRefused
	}
	return ExitOK
}

// loadAndRun folds the ledger into a survey and runs the pipeline over it.
func loadAndRun(current *session, options pipeline.Options) (pipeline.Outcome, error) {
	survey, err := current.store.LoadSurvey()
	if err != nil {
		return pipeline.Outcome{}, err
	}
	return pipeline.Run(survey, current.cfg, options)
}
