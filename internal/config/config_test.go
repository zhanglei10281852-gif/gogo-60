package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"CaveLoop/internal/units"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing configuration: %v", err)
	}
	return path
}

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("the built in configuration is invalid: %v", err)
	}
}

func TestLoadWithoutPathReturnsDefaults(t *testing.T) {
	loaded, err := Load("")
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if loaded != Default() {
		t.Fatalf("Load without a path returned %+v", loaded)
	}
}

func TestLoadOverlayKeepsUnsetDefaults(t *testing.T) {
	path := writeConfig(t, `{"version":1,"tolerances":{"loopClosureMeters":1.25},"output":{"format":"json"}}`)
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if loaded.Tolerances.LoopClosureMeters != 1.25 {
		t.Fatalf("overlay was not applied: %+v", loaded.Tolerances)
	}
	if loaded.Tolerances.BacksightAzimuthDeg != Default().Tolerances.BacksightAzimuthDeg {
		t.Fatal("an unset tolerance lost its default")
	}
	if loaded.Output.Format != OutputJSON {
		t.Fatalf("output format is %q", loaded.Output.Format)
	}
	if loaded.Output.LengthPrecision != Default().Output.LengthPrecision {
		t.Fatal("an unset precision lost its default")
	}
	if loaded.Adjustment != Default().Adjustment {
		t.Fatalf("adjustment defaults changed: %+v", loaded.Adjustment)
	}
}

func TestLoadAppliesEveryOverlaySection(t *testing.T) {
	path := writeConfig(t, `{
	  "version": 1,
	  "dataDir": "store",
	  "defaults": {"lengthUnit": "ft", "angleUnit": "grad", "declination": 3.5},
	  "tolerances": {"backsightAzimuthDeg": 3, "backsightInclinationDeg": 4,
	    "backsightDistanceMeters": 0.2, "backsightDistanceRatio": 0.05,
	    "loopClosureMeters": 2, "loopClosurePpm": 30000, "verticalClosureMeters": 1},
	  "adjustment": {"enabled": false, "maxPasses": 5, "convergenceMeters": 0.01,
	    "minShotWeightMeters": 0.5, "adjustVertical": false},
	  "blunders": {"enabled": false, "reversedWindowDeg": 20, "lengthOutlierSigma": 2,
	    "lengthOutlierMinimumShots": 4, "transposeImprovementRatio": 0.5, "maxCandidatesPerLoop": 8},
	  "output": {"format": "json", "lengthPrecision": 1, "anglePrecision": 0}
	}`)
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if loaded.DataDir != "store" || loaded.Defaults.LengthUnit != "ft" || loaded.Defaults.Declination != 3.5 {
		t.Fatalf("defaults section not applied: %+v", loaded)
	}
	if loaded.Adjustment.Enabled || loaded.Adjustment.MaxPasses != 5 || loaded.Adjustment.AdjustVertical {
		t.Fatalf("adjustment section not applied: %+v", loaded.Adjustment)
	}
	if loaded.Blunders.Enabled || loaded.Blunders.MaxCandidates != 8 {
		t.Fatalf("blunders section not applied: %+v", loaded.Blunders)
	}
	if loaded.Tolerances.LoopClosurePPM != 30000 {
		t.Fatalf("tolerances section not applied: %+v", loaded.Tolerances)
	}
	modelDefaults := loaded.ModelDefaults()
	if modelDefaults.LengthUnit != units.Feet || modelDefaults.AngleUnit != units.Grads {
		t.Fatalf("ModelDefaults produced %+v", modelDefaults)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, `{"version":1,"tolerance":{"loopClosureMeters":1}}`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestLoadRejectsTrailingContent(t *testing.T) {
	path := writeConfig(t, `{"version":1}{"version":1}`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("a missing configuration was accepted")
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := map[string]func(*Config){
		"version":       func(c *Config) { c.Version = 2 },
		"dataDir":       func(c *Config) { c.DataDir = "  " },
		"lengthUnit":    func(c *Config) { c.Defaults.LengthUnit = "cubit" },
		"angleUnit":     func(c *Config) { c.Defaults.AngleUnit = "mils" },
		"tolerance":     func(c *Config) { c.Tolerances.LoopClosureMeters = 0 },
		"azimuthWindow": func(c *Config) { c.Tolerances.BacksightAzimuthDeg = 120 },
		"reversed":      func(c *Config) { c.Blunders.ReversedWindowDeg = 120 },
		"transpose":     func(c *Config) { c.Blunders.TransposeImprovement = 1.5 },
		"passes":        func(c *Config) { c.Adjustment.MaxPasses = 0 },
		"minimumShots":  func(c *Config) { c.Blunders.LengthOutlierMinimum = 1 },
		"candidates":    func(c *Config) { c.Blunders.MaxCandidates = 0 },
		"format":        func(c *Config) { c.Output.Format = "yaml" },
		"lengthDigits":  func(c *Config) { c.Output.LengthPrecision = 12 },
		"angleDigits":   func(c *Config) { c.Output.AnglePrecision = -1 },
	}
	for name, mutate := range cases {
		candidate := Default()
		mutate(&candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatalf("case %q was accepted", name)
		}
	}
}

func TestWithFormat(t *testing.T) {
	base := Default()
	unchanged, err := base.WithFormat("")
	if err != nil || unchanged.Output.Format != OutputText {
		t.Fatalf("empty format changed the configuration: %+v, %v", unchanged, err)
	}
	asJSON, err := base.WithFormat(" JSON ")
	if err != nil || asJSON.Output.Format != OutputJSON {
		t.Fatalf("WithFormat produced %+v, %v", asJSON, err)
	}
	if _, err := base.WithFormat("xml"); err == nil {
		t.Fatal("an unsupported format was accepted")
	}
}

func TestWithDataDirAndResolution(t *testing.T) {
	base := Default().WithDataDir("store-a")
	if base.DataDir != "store-a" {
		t.Fatalf("WithDataDir produced %q", base.DataDir)
	}
	resolved, err := base.ResolvedDataDir()
	if err != nil {
		t.Fatalf("ResolvedDataDir returned %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("ResolvedDataDir produced %q", resolved)
	}
}

func TestDescribeIsIndentedJSON(t *testing.T) {
	described, err := Default().Describe()
	if err != nil {
		t.Fatalf("Describe returned %v", err)
	}
	if !strings.Contains(string(described), "\n  \"dataDir\"") {
		t.Fatalf("Describe produced %s", described)
	}
}
