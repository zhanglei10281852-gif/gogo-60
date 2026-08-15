package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const surveyFixture = `{
  "cave": "CLI Cave",
  "region": "Test Region",
  "instruments": [
    {"id": "set-a", "lengthUnit": "m", "angleUnit": "deg", "declination": 1.5}
  ],
  "trips": [
    {
      "id": "T1",
      "date": "2031-07-04",
      "lengthUnit": "m",
      "angleUnit": "deg",
      "instrument": "set-a",
      "stations": [
        {"name": "A", "flags": ["entrance", "fixed"], "fixed": {"east": 0, "north": 0, "up": 0, "unit": "m"}},
        {"name": "B"}, {"name": "C"}, {"name": "D"}
      ],
      "shots": [
        {"id": "S1", "from": "A", "to": "B", "distance": 10, "azimuth": 90, "inclination": 0,
         "backAzimuth": 270.4, "backInclination": 0.1},
        {"id": "S2", "from": "B", "to": "C", "distance": 10, "azimuth": 0, "inclination": -5},
        {"id": "S3", "from": "C", "to": "D", "distance": 10, "azimuth": 270, "inclination": 0},
        {"id": "S4", "from": "D", "to": "A", "distance": 10.4, "azimuth": 180, "inclination": 5}
      ]
    }
  ]
}`

const followupFixture = `{"kind":"trip","cave":"CLI Cave","trip":{"id":"T2","lengthUnit":"m","angleUnit":"deg","stations":[{"name":"D"},{"name":"E"}],"shots":[{"id":"S5","from":"D","to":"E","distance":7.5,"azimuth":45,"inclination":-12}]}}
`

const unanchoredFixture = `{
  "cave": "Floating Cave",
  "trips": [
    {"id": "T1", "lengthUnit": "m", "angleUnit": "deg",
     "stations": [{"name": "A"}, {"name": "B"}],
     "shots": [{"id": "S1", "from": "A", "to": "B", "distance": 5, "azimuth": 10, "inclination": 0}]}
  ]
}`

type harness struct {
	t      *testing.T
	dir    string
	data   string
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "survey.json"), surveyFixture)
	write(t, filepath.Join(dir, "followup.jsonl"), followupFixture)
	write(t, filepath.Join(dir, "floating.json"), unanchoredFixture)
	return &harness{
		t:      t,
		dir:    dir,
		data:   filepath.Join(dir, "store"),
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func (h *harness) run(args ...string) int {
	h.t.Helper()
	h.stdout.Reset()
	h.stderr.Reset()
	return Run(args, h.stdout, h.stderr)
}

func (h *harness) path(name string) string { return filepath.Join(h.dir, name) }

func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	if code := h.run(args...); code != ExitOK {
		h.t.Fatalf("%v exited with %d\nstdout:\n%s\nstderr:\n%s", args, code, h.stdout.String(), h.stderr.String())
	}
	return h.stdout.String()
}

func TestUsageAndVersion(t *testing.T) {
	h := newHarness(t)
	if code := h.run(); code != ExitError {
		t.Fatalf("running without a subcommand exited with %d", code)
	}
	if !strings.Contains(h.stderr.String(), "usage: caveloop") {
		t.Fatalf("stderr is\n%s", h.stderr.String())
	}
	output := h.mustRun("help")
	for _, want := range []string{"validate", "import", "reduce", "adjust", "network", "loops", "blunders", "report", "verify"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help does not list %q:\n%s", want, output)
		}
	}
	if !strings.Contains(h.mustRun("version"), "CaveLoop ") {
		t.Fatalf("version output is %q", h.stdout.String())
	}
	if code := h.run("dig"); code != ExitError {
		t.Fatalf("an unknown subcommand exited with %d", code)
	}
	if !strings.Contains(h.stderr.String(), "unknown subcommand") {
		t.Fatalf("stderr is\n%s", h.stderr.String())
	}
}

func TestValidateSubcommand(t *testing.T) {
	h := newHarness(t)
	output := h.mustRun("validate", "-input", h.path("survey.json"))
	for _, want := range []string{"CaveLoop validation", "CLI Cave", "verdict      ok"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not mention %q:\n%s", want, output)
		}
	}
	if code := h.run("validate"); code != ExitError {
		t.Fatalf("validate without -input exited with %d", code)
	}
	if code := h.run("validate", "-input", h.path("missing.json")); code != ExitRefused {
		t.Fatalf("validate on a missing file exited with %d", code)
	}
	if code := h.run("validate", "-input", h.path("floating.json"), "-strict"); code != ExitRefused {
		t.Fatalf("strict validate on a warning exited with %d", code)
	}
	if !strings.Contains(h.stderr.String(), "-strict") {
		t.Fatalf("stderr is\n%s", h.stderr.String())
	}
	if code := h.run("validate", "-input", h.path("floating.json")); code != ExitOK {
		t.Fatalf("non strict validate on a warning exited with %d", code)
	}
	if code := h.run("validate", "-input", h.path("survey.json"), "-input-format", "csv"); code != ExitError {
		t.Fatalf("an unsupported input format exited with %d", code)
	}
	if code := h.run("validate", "-input", h.path("survey.json"), "extra"); code != ExitError {
		t.Fatalf("a positional argument exited with %d", code)
	}
}

func TestValidateRejectsBrokenDocument(t *testing.T) {
	h := newHarness(t)
	write(t, h.path("broken.json"), `{"cave":"x","trips":[],"depth":1}`)
	if code := h.run("validate", "-input", h.path("broken.json")); code != ExitRefused {
		t.Fatalf("a document with an unknown field exited with %d", code)
	}
	write(t, h.path("invalid.json"), `{"cave":"","trips":[]}`)
	if code := h.run("validate", "-input", h.path("invalid.json")); code != ExitRefused {
		t.Fatalf("an invalid survey exited with %d", code)
	}
}

func TestFullWorkflow(t *testing.T) {
	h := newHarness(t)
	if code := h.run("verify", "-data", h.data); code != ExitRefused {
		t.Fatalf("verify on an empty store exited with %d", code)
	}
	if code := h.run("reduce", "-data", h.data); code != ExitRefused {
		t.Fatalf("reduce on an empty store exited with %d", code)
	}
	importOutput := h.mustRun("import", "-input", h.path("survey.json"), "-data", h.data)
	if !strings.Contains(importOutput, "appended records  2") {
		t.Fatalf("import output is\n%s", importOutput)
	}
	h.mustRun("import", "-input", h.path("followup.jsonl"), "-data", h.data)

	reduceOutput := h.mustRun("reduce", "-data", h.data)
	for _, want := range []string{"CaveLoop reduction", "stations", "legs", "T1/S1", "T2/S5"} {
		if !strings.Contains(reduceOutput, want) {
			t.Fatalf("reduce output does not mention %q:\n%s", want, reduceOutput)
		}
	}
	networkOutput := h.mustRun("network", "-data", h.data)
	if !strings.Contains(networkOutput, "network topology") || !strings.Contains(networkOutput, "junctions") {
		t.Fatalf("network output is\n%s", networkOutput)
	}
	loopsOutput := h.mustRun("loops", "-data", h.data)
	if !strings.Contains(loopsOutput, "L001") {
		t.Fatalf("loops output is\n%s", loopsOutput)
	}
	adjustOutput := h.mustRun("adjust", "-data", h.data)
	if !strings.Contains(adjustOutput, "closure adjustment") || !strings.Contains(adjustOutput, "leg corrections") {
		t.Fatalf("adjust output is\n%s", adjustOutput)
	}
	adjustedLoops := h.mustRun("loops", "-data", h.data, "-adjusted")
	if !strings.Contains(adjustedLoops, "adjusted          yes") {
		t.Fatalf("adjusted loops output is\n%s", adjustedLoops)
	}
	blunderOutput := h.mustRun("blunders", "-data", h.data)
	if !strings.Contains(blunderOutput, "blunder scan") {
		t.Fatalf("blunders output is\n%s", blunderOutput)
	}
	reportOutput := h.mustRun("report", "-data", h.data)
	for _, want := range []string{"survey report", "CLI Cave", "topology", "closure", "trips"} {
		if !strings.Contains(reportOutput, want) {
			t.Fatalf("report output does not mention %q:\n%s", want, reportOutput)
		}
	}
	verifyOutput := h.mustRun("verify", "-data", h.data)
	if !strings.Contains(verifyOutput, "verdict           ok") {
		t.Fatalf("verify output is\n%s", verifyOutput)
	}
	for _, name := range []string{"ledger.jsonl", "network.json", "metadata.json", "audit.jsonl"} {
		if _, err := os.Stat(filepath.Join(h.data, name)); err != nil {
			t.Fatalf("artefact %s is missing: %v", name, err)
		}
	}
}

func TestWorkflowIsByteIdentical(t *testing.T) {
	h := newHarness(t)
	digests := make([]map[string]string, 0, 2)
	for run := 0; run < 2; run++ {
		data := filepath.Join(h.dir, "store-"+string(rune('a'+run)))
		h.mustRun("import", "-input", h.path("survey.json"), "-data", data)
		h.mustRun("import", "-input", h.path("followup.jsonl"), "-data", data)
		h.mustRun("adjust", "-data", data)
		snapshot := make(map[string]string)
		for _, name := range []string{"ledger.jsonl", "network.json", "metadata.json", "audit.jsonl"} {
			payload, err := os.ReadFile(filepath.Join(data, name))
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}
			snapshot[name] = string(payload)
		}
		digests = append(digests, snapshot)
	}
	for name, first := range digests[0] {
		if digests[1][name] != first {
			t.Fatalf("artefact %s differs between two identical runs", name)
		}
	}
}

func TestJSONOutputDecodes(t *testing.T) {
	h := newHarness(t)
	h.mustRun("import", "-input", h.path("survey.json"), "-data", h.data)
	h.mustRun("adjust", "-data", h.data)
	for _, command := range []string{"network", "loops", "blunders", "report", "verify", "reduce"} {
		output := h.mustRun(command, "-data", h.data, "-format", "json")
		var decoded map[string]any
		if err := json.Unmarshal([]byte(output), &decoded); err != nil {
			t.Fatalf("%s produced invalid JSON: %v\n%s", command, err, output)
		}
		if decoded["command"] != command {
			t.Fatalf("%s reported command %v", command, decoded["command"])
		}
	}
	if code := h.run("report", "-data", h.data, "-format", "xml"); code != ExitError {
		t.Fatalf("an unsupported output format exited with %d", code)
	}
}

func TestOutFileAndConfig(t *testing.T) {
	h := newHarness(t)
	configPath := h.path("config.json")
	write(t, configPath, `{"version":1,"dataDir":"`+strings.ReplaceAll(h.data, `\`, `\\`)+`","output":{"lengthPrecision":1}}`)
	h.mustRun("import", "-input", h.path("survey.json"), "-config", configPath)
	outPath := filepath.Join(h.dir, "reports", "reduce.txt")
	h.mustRun("reduce", "-config", configPath, "-out", outPath)
	payload, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading the report file: %v", err)
	}
	if !strings.Contains(string(payload), "CaveLoop reduction") {
		t.Fatalf("the report file holds\n%s", payload)
	}
	if h.stdout.Len() != 0 {
		t.Fatalf("stdout should stay empty, it holds\n%s", h.stdout.String())
	}
	if code := h.run("reduce", "-config", h.path("absent.json")); code != ExitError {
		t.Fatalf("a missing configuration exited with %d", code)
	}
	write(t, h.path("bad-config.json"), `{"version":2}`)
	if code := h.run("reduce", "-config", h.path("bad-config.json")); code != ExitError {
		t.Fatalf("an unsupported configuration version exited with %d", code)
	}
}

func TestNoWriteKeepsSnapshotUntouched(t *testing.T) {
	h := newHarness(t)
	h.mustRun("import", "-input", h.path("survey.json"), "-data", h.data)
	h.mustRun("reduce", "-data", h.data)
	before, err := os.ReadFile(filepath.Join(h.data, "network.json"))
	if err != nil {
		t.Fatalf("reading the snapshot: %v", err)
	}
	h.mustRun("adjust", "-data", h.data, "-no-write")
	after, err := os.ReadFile(filepath.Join(h.data, "network.json"))
	if err != nil {
		t.Fatalf("reading the snapshot: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("-no-write modified the stored snapshot")
	}
}

func TestBlundersFailOnFinding(t *testing.T) {
	h := newHarness(t)
	write(t, h.path("blunder.json"), `{
	  "cave": "Blunder Cave",
	  "trips": [
	    {"id": "T1", "lengthUnit": "m", "angleUnit": "deg",
	     "stations": [{"name":"A","flags":["fixed"],"fixed":{"east":0,"north":0,"up":0,"unit":"m"}},{"name":"B"}],
	     "shots": [{"id":"S1","from":"A","to":"B","distance":10,"azimuth":90,"inclination":0,
	                "backAzimuth":90,"backInclination":0}]}
	  ]
	}`)
	h.mustRun("import", "-input", h.path("blunder.json"), "-data", h.data)
	if code := h.run("blunders", "-data", h.data, "-fail-on-finding"); code != ExitRefused {
		t.Fatalf("blunders with -fail-on-finding exited with %d", code)
	}
	if !strings.Contains(h.stderr.String(), "suspected blunder") {
		t.Fatalf("stderr is\n%s", h.stderr.String())
	}
	if code := h.run("blunders", "-data", h.data); code != ExitOK {
		t.Fatalf("blunders without -fail-on-finding exited with %d", code)
	}
}

func TestImportRefusesInvalidSurvey(t *testing.T) {
	h := newHarness(t)
	write(t, h.path("invalid.json"), `{"cave":"x","trips":[{"id":"","shots":[]}]}`)
	if code := h.run("import", "-input", h.path("invalid.json"), "-data", h.data); code != ExitRefused {
		t.Fatalf("importing an invalid survey exited with %d", code)
	}
	if code := h.run("import", "-data", h.data); code != ExitError {
		t.Fatalf("import without -input exited with %d", code)
	}
}

func TestVerifyDetectsTamperedLedger(t *testing.T) {
	h := newHarness(t)
	h.mustRun("import", "-input", h.path("survey.json"), "-data", h.data)
	h.mustRun("reduce", "-data", h.data)
	ledger := filepath.Join(h.data, "ledger.jsonl")
	payload, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	write(t, ledger, string(payload)+`{"kind":"trip","trip":{"id":"T9"}}`+"\n")
	if code := h.run("verify", "-data", h.data); code != ExitRefused {
		t.Fatalf("verify on a tampered ledger exited with %d", code)
	}
	if !strings.Contains(h.stdout.String(), "ledger matches    no") {
		t.Fatalf("verify output is\n%s", h.stdout.String())
	}
}

func TestSummariseCodes(t *testing.T) {
	if got := summariseCodes(nil); got != "none" {
		t.Fatalf("summariseCodes(nil) = %q", got)
	}
	if got := summariseCodes(map[string]int{"b": 2, "a": 1}); got != "a=1 b=2" {
		t.Fatalf("summariseCodes = %q", got)
	}
}

func TestHelpFlagOnSubcommand(t *testing.T) {
	h := newHarness(t)
	if code := h.run("loops", "-h"); code != ExitOK {
		t.Fatalf("loops -h exited with %d", code)
	}
	if !strings.Contains(h.stderr.String(), "-adjusted") {
		t.Fatalf("subcommand help is\n%s", h.stderr.String())
	}
}
