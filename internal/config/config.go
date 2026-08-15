// Package config holds the CaveLoop runtime configuration: default units,
// reconciliation and closure tolerances, adjustment behaviour, blunder
// detection thresholds and output formatting.
//
// Configuration is decoded strictly: unknown fields and trailing content are
// rejected so a typo in a tolerance name can never be silently ignored.
package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"CaveLoop/internal/jsonx"
	"CaveLoop/internal/model"
	"CaveLoop/internal/units"
)

// SchemaVersion is the configuration schema understood by this build.
const SchemaVersion = 1

// Output formats supported by the CLI.
const (
	OutputText = "text"
	OutputJSON = "json"
)

// Defaults describes the fallback measurement conventions.
type Defaults struct {
	LengthUnit  string  `json:"lengthUnit"`
	AngleUnit   string  `json:"angleUnit"`
	Declination float64 `json:"declination"`
}

// Tolerances holds the acceptance thresholds of the reduction pipeline. All
// linear values are in meters and all angular values in decimal degrees,
// independent of the units used in the field notes.
type Tolerances struct {
	BacksightAzimuthDeg     float64 `json:"backsightAzimuthDeg"`
	BacksightInclinationDeg float64 `json:"backsightInclinationDeg"`
	BacksightDistanceMeters float64 `json:"backsightDistanceMeters"`
	BacksightDistanceRatio  float64 `json:"backsightDistanceRatio"`
	LoopClosureMeters       float64 `json:"loopClosureMeters"`
	LoopClosurePPM          float64 `json:"loopClosurePpm"`
	VerticalClosureMeters   float64 `json:"verticalClosureMeters"`
}

// Adjustment controls the loop closure distribution stage.
type Adjustment struct {
	Enabled        bool    `json:"enabled"`
	MaxPasses      int     `json:"maxPasses"`
	Convergence    float64 `json:"convergenceMeters"`
	MinShotWeight  float64 `json:"minShotWeightMeters"`
	AdjustVertical bool    `json:"adjustVertical"`
}

// Blunders controls the heuristic blunder detectors.
type Blunders struct {
	Enabled              bool    `json:"enabled"`
	ReversedWindowDeg    float64 `json:"reversedWindowDeg"`
	LengthOutlierSigma   float64 `json:"lengthOutlierSigma"`
	LengthOutlierMinimum int     `json:"lengthOutlierMinimumShots"`
	TransposeImprovement float64 `json:"transposeImprovementRatio"`
	MaxCandidates        int     `json:"maxCandidatesPerLoop"`
}

// Output controls report rendering.
type Output struct {
	Format          string `json:"format"`
	LengthPrecision int    `json:"lengthPrecision"`
	AnglePrecision  int    `json:"anglePrecision"`
}

// Config is the complete runtime configuration.
type Config struct {
	Version    int        `json:"version"`
	DataDir    string     `json:"dataDir"`
	Defaults   Defaults   `json:"defaults"`
	Tolerances Tolerances `json:"tolerances"`
	Adjustment Adjustment `json:"adjustment"`
	Blunders   Blunders   `json:"blunders"`
	Output     Output     `json:"output"`
}

// Default returns the built in configuration. Every field is populated so a
// zero length configuration file is still a complete configuration.
func Default() Config {
	return Config{
		Version: SchemaVersion,
		DataDir: filepath.FromSlash("./caveloop-data"),
		Defaults: Defaults{
			LengthUnit:  string(units.Meters),
			AngleUnit:   string(units.Degrees),
			Declination: 0,
		},
		Tolerances: Tolerances{
			BacksightAzimuthDeg:     2.0,
			BacksightInclinationDeg: 2.0,
			BacksightDistanceMeters: 0.10,
			BacksightDistanceRatio:  0.02,
			LoopClosureMeters:       0.50,
			LoopClosurePPM:          20000,
			VerticalClosureMeters:   0.30,
		},
		Adjustment: Adjustment{
			Enabled:        true,
			MaxPasses:      24,
			Convergence:    0.0005,
			MinShotWeight:  0.05,
			AdjustVertical: true,
		},
		Blunders: Blunders{
			Enabled:              true,
			ReversedWindowDeg:    12.0,
			LengthOutlierSigma:   3.0,
			LengthOutlierMinimum: 6,
			TransposeImprovement: 0.60,
			MaxCandidates:        32,
		},
		Output: Output{
			Format:          OutputText,
			LengthPrecision: 3,
			AnglePrecision:  2,
		},
	}
}

// Load reads a configuration document from disk. An empty path yields the
// default configuration, which keeps the CLI usable with no setup at all.
func Load(path string) (Config, error) {
	config := Default()
	if strings.TrimSpace(path) == "" {
		return config, nil
	}
	handle, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("opening configuration %s: %w", path, err)
	}
	defer func() { _ = handle.Close() }()
	overlay := documentOverlay{}
	if err := jsonx.DecodeStrict(handle, &overlay); err != nil {
		return Config{}, fmt.Errorf("configuration %s: %w", filepath.Base(path), err)
	}
	overlay.apply(&config)
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("configuration %s: %w", filepath.Base(path), err)
	}
	return config, nil
}

// documentOverlay mirrors Config with pointer fields so that omitted keys keep
// their default value instead of collapsing to the zero value.
type documentOverlay struct {
	Version    *int               `json:"version,omitempty"`
	DataDir    *string            `json:"dataDir,omitempty"`
	Defaults   *defaultsOverlay   `json:"defaults,omitempty"`
	Tolerances *tolerancesOverlay `json:"tolerances,omitempty"`
	Adjustment *adjustmentOverlay `json:"adjustment,omitempty"`
	Blunders   *blundersOverlay   `json:"blunders,omitempty"`
	Output     *outputOverlay     `json:"output,omitempty"`
}

type defaultsOverlay struct {
	LengthUnit  *string  `json:"lengthUnit,omitempty"`
	AngleUnit   *string  `json:"angleUnit,omitempty"`
	Declination *float64 `json:"declination,omitempty"`
}

type tolerancesOverlay struct {
	BacksightAzimuthDeg     *float64 `json:"backsightAzimuthDeg,omitempty"`
	BacksightInclinationDeg *float64 `json:"backsightInclinationDeg,omitempty"`
	BacksightDistanceMeters *float64 `json:"backsightDistanceMeters,omitempty"`
	BacksightDistanceRatio  *float64 `json:"backsightDistanceRatio,omitempty"`
	LoopClosureMeters       *float64 `json:"loopClosureMeters,omitempty"`
	LoopClosurePPM          *float64 `json:"loopClosurePpm,omitempty"`
	VerticalClosureMeters   *float64 `json:"verticalClosureMeters,omitempty"`
}

type adjustmentOverlay struct {
	Enabled        *bool    `json:"enabled,omitempty"`
	MaxPasses      *int     `json:"maxPasses,omitempty"`
	Convergence    *float64 `json:"convergenceMeters,omitempty"`
	MinShotWeight  *float64 `json:"minShotWeightMeters,omitempty"`
	AdjustVertical *bool    `json:"adjustVertical,omitempty"`
}

type blundersOverlay struct {
	Enabled              *bool    `json:"enabled,omitempty"`
	ReversedWindowDeg    *float64 `json:"reversedWindowDeg,omitempty"`
	LengthOutlierSigma   *float64 `json:"lengthOutlierSigma,omitempty"`
	LengthOutlierMinimum *int     `json:"lengthOutlierMinimumShots,omitempty"`
	TransposeImprovement *float64 `json:"transposeImprovementRatio,omitempty"`
	MaxCandidates        *int     `json:"maxCandidatesPerLoop,omitempty"`
}

type outputOverlay struct {
	Format          *string `json:"format,omitempty"`
	LengthPrecision *int    `json:"lengthPrecision,omitempty"`
	AnglePrecision  *int    `json:"anglePrecision,omitempty"`
}

// apply folds the overlay onto a fully populated configuration.
func (o documentOverlay) apply(target *Config) {
	if o.Version != nil {
		target.Version = *o.Version
	}
	if o.DataDir != nil {
		target.DataDir = *o.DataDir
	}
	if o.Defaults != nil {
		if o.Defaults.LengthUnit != nil {
			target.Defaults.LengthUnit = *o.Defaults.LengthUnit
		}
		if o.Defaults.AngleUnit != nil {
			target.Defaults.AngleUnit = *o.Defaults.AngleUnit
		}
		if o.Defaults.Declination != nil {
			target.Defaults.Declination = *o.Defaults.Declination
		}
	}
	if o.Tolerances != nil {
		assignFloat(&target.Tolerances.BacksightAzimuthDeg, o.Tolerances.BacksightAzimuthDeg)
		assignFloat(&target.Tolerances.BacksightInclinationDeg, o.Tolerances.BacksightInclinationDeg)
		assignFloat(&target.Tolerances.BacksightDistanceMeters, o.Tolerances.BacksightDistanceMeters)
		assignFloat(&target.Tolerances.BacksightDistanceRatio, o.Tolerances.BacksightDistanceRatio)
		assignFloat(&target.Tolerances.LoopClosureMeters, o.Tolerances.LoopClosureMeters)
		assignFloat(&target.Tolerances.LoopClosurePPM, o.Tolerances.LoopClosurePPM)
		assignFloat(&target.Tolerances.VerticalClosureMeters, o.Tolerances.VerticalClosureMeters)
	}
	if o.Adjustment != nil {
		assignBool(&target.Adjustment.Enabled, o.Adjustment.Enabled)
		assignInt(&target.Adjustment.MaxPasses, o.Adjustment.MaxPasses)
		assignFloat(&target.Adjustment.Convergence, o.Adjustment.Convergence)
		assignFloat(&target.Adjustment.MinShotWeight, o.Adjustment.MinShotWeight)
		assignBool(&target.Adjustment.AdjustVertical, o.Adjustment.AdjustVertical)
	}
	if o.Blunders != nil {
		assignBool(&target.Blunders.Enabled, o.Blunders.Enabled)
		assignFloat(&target.Blunders.ReversedWindowDeg, o.Blunders.ReversedWindowDeg)
		assignFloat(&target.Blunders.LengthOutlierSigma, o.Blunders.LengthOutlierSigma)
		assignInt(&target.Blunders.LengthOutlierMinimum, o.Blunders.LengthOutlierMinimum)
		assignFloat(&target.Blunders.TransposeImprovement, o.Blunders.TransposeImprovement)
		assignInt(&target.Blunders.MaxCandidates, o.Blunders.MaxCandidates)
	}
	if o.Output != nil {
		if o.Output.Format != nil {
			target.Output.Format = *o.Output.Format
		}
		assignInt(&target.Output.LengthPrecision, o.Output.LengthPrecision)
		assignInt(&target.Output.AnglePrecision, o.Output.AnglePrecision)
	}
}

// assignFloat copies a present overlay value onto the target.
func assignFloat(target *float64, source *float64) {
	if source != nil {
		*target = *source
	}
}

// assignInt copies a present overlay value onto the target.
func assignInt(target *int, source *int) {
	if source != nil {
		*target = *source
	}
}

// assignBool copies a present overlay value onto the target.
func assignBool(target *bool, source *bool) {
	if source != nil {
		*target = *source
	}
}

// Validate checks that the configuration is internally consistent.
func (c Config) Validate() error {
	if c.Version != SchemaVersion {
		return fmt.Errorf("unsupported configuration version %d (want %d)", c.Version, SchemaVersion)
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("dataDir must not be empty")
	}
	if _, err := units.ParseLengthUnit(c.Defaults.LengthUnit); err != nil {
		return fmt.Errorf("defaults.lengthUnit: %w", err)
	}
	if _, err := units.ParseAngleUnit(c.Defaults.AngleUnit); err != nil {
		return fmt.Errorf("defaults.angleUnit: %w", err)
	}
	if math.IsNaN(c.Defaults.Declination) || math.IsInf(c.Defaults.Declination, 0) {
		return fmt.Errorf("defaults.declination must be a finite number")
	}
	positives := []struct {
		name  string
		value float64
	}{
		{"tolerances.backsightAzimuthDeg", c.Tolerances.BacksightAzimuthDeg},
		{"tolerances.backsightInclinationDeg", c.Tolerances.BacksightInclinationDeg},
		{"tolerances.backsightDistanceMeters", c.Tolerances.BacksightDistanceMeters},
		{"tolerances.backsightDistanceRatio", c.Tolerances.BacksightDistanceRatio},
		{"tolerances.loopClosureMeters", c.Tolerances.LoopClosureMeters},
		{"tolerances.loopClosurePpm", c.Tolerances.LoopClosurePPM},
		{"tolerances.verticalClosureMeters", c.Tolerances.VerticalClosureMeters},
		{"adjustment.convergenceMeters", c.Adjustment.Convergence},
		{"adjustment.minShotWeightMeters", c.Adjustment.MinShotWeight},
		{"blunders.reversedWindowDeg", c.Blunders.ReversedWindowDeg},
		{"blunders.lengthOutlierSigma", c.Blunders.LengthOutlierSigma},
		{"blunders.transposeImprovementRatio", c.Blunders.TransposeImprovement},
	}
	for _, entry := range positives {
		if math.IsNaN(entry.value) || math.IsInf(entry.value, 0) {
			return fmt.Errorf("%s must be a finite number", entry.name)
		}
		if entry.value <= 0 {
			return fmt.Errorf("%s must be greater than zero, got %s", entry.name, units.Format(entry.value, 6))
		}
	}
	if c.Tolerances.BacksightAzimuthDeg > 90 {
		return fmt.Errorf("tolerances.backsightAzimuthDeg must stay at or below 90")
	}
	if c.Blunders.ReversedWindowDeg > 90 {
		return fmt.Errorf("blunders.reversedWindowDeg must stay at or below 90")
	}
	if c.Blunders.TransposeImprovement > 1 {
		return fmt.Errorf("blunders.transposeImprovementRatio must stay at or below 1")
	}
	if c.Adjustment.MaxPasses < 1 || c.Adjustment.MaxPasses > 1000 {
		return fmt.Errorf("adjustment.maxPasses must be between 1 and 1000, got %d", c.Adjustment.MaxPasses)
	}
	if c.Blunders.LengthOutlierMinimum < 2 {
		return fmt.Errorf("blunders.lengthOutlierMinimumShots must be at least 2")
	}
	if c.Blunders.MaxCandidates < 1 {
		return fmt.Errorf("blunders.maxCandidatesPerLoop must be at least 1")
	}
	if c.Output.Format != OutputText && c.Output.Format != OutputJSON {
		return fmt.Errorf("output.format must be %q or %q, got %q", OutputText, OutputJSON, c.Output.Format)
	}
	if c.Output.LengthPrecision < 0 || c.Output.LengthPrecision > 9 {
		return fmt.Errorf("output.lengthPrecision must be between 0 and 9")
	}
	if c.Output.AnglePrecision < 0 || c.Output.AnglePrecision > 9 {
		return fmt.Errorf("output.anglePrecision must be between 0 and 9")
	}
	return nil
}

// ModelDefaults converts the configured defaults into the model representation.
func (c Config) ModelDefaults() model.Defaults {
	lengthUnit, err := units.ParseLengthUnit(c.Defaults.LengthUnit)
	if err != nil {
		lengthUnit = units.Meters
	}
	angleUnit, err := units.ParseAngleUnit(c.Defaults.AngleUnit)
	if err != nil {
		angleUnit = units.Degrees
	}
	return model.Defaults{
		LengthUnit:  lengthUnit,
		AngleUnit:   angleUnit,
		Declination: c.Defaults.Declination,
	}
}

// ResolvedDataDir returns the data directory as an absolute path.
func (c Config) ResolvedDataDir() (string, error) {
	absolute, err := filepath.Abs(c.DataDir)
	if err != nil {
		return "", fmt.Errorf("resolving dataDir %q: %w", c.DataDir, err)
	}
	return absolute, nil
}

// WithDataDir returns a copy of the configuration pointing at another store.
func (c Config) WithDataDir(dir string) Config {
	out := c
	out.DataDir = dir
	return out
}

// WithFormat returns a copy of the configuration using another output format.
func (c Config) WithFormat(format string) (Config, error) {
	out := c
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "":
		return out, nil
	case OutputText:
		out.Output.Format = OutputText
	case OutputJSON:
		out.Output.Format = OutputJSON
	default:
		return Config{}, fmt.Errorf("unsupported output format %q (want %q or %q)", format, OutputText, OutputJSON)
	}
	return out, nil
}

// Describe renders the configuration as deterministic indented JSON, which the
// CLI uses to echo the effective settings.
func (c Config) Describe() ([]byte, error) {
	return jsonx.MarshalIndent(c)
}
