package model

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"CaveLoop/internal/units"
)

// Issue severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Issue is a single validation finding with a stable machine readable code and
// a path pointing at the offending element.
type Issue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

// String renders the issue on one line.
func (i Issue) String() string {
	return fmt.Sprintf("%s %s at %s: %s", strings.ToUpper(i.Severity), i.Code, i.Path, i.Message)
}

// Issues is an ordered collection of findings.
type Issues []Issue

// Sorted returns the issues in a deterministic order: errors first, then by
// path, code and message.
func (issues Issues) Sorted() Issues {
	out := make(Issues, len(issues))
	copy(out, issues)
	sort.SliceStable(out, func(a, b int) bool {
		left, right := out[a], out[b]
		if left.Severity != right.Severity {
			return left.Severity == SeverityError
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})
	return out
}

// Filter returns the subset of issues with the given severity.
func (issues Issues) Filter(severity string) Issues {
	out := make(Issues, 0, len(issues))
	for _, issue := range issues {
		if issue.Severity == severity {
			out = append(out, issue)
		}
	}
	return out
}

// HasErrors reports whether any issue is fatal.
func (issues Issues) HasErrors() bool {
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Error implements the error interface so a validation result can be returned
// directly from functions that must fail on invalid data.
func (issues Issues) Error() string {
	failures := issues.Filter(SeverityError).Sorted()
	if len(failures) == 0 {
		return "no validation errors"
	}
	parts := make([]string, 0, len(failures))
	for _, issue := range failures {
		parts = append(parts, issue.String())
	}
	return strings.Join(parts, "; ")
}

// Defaults carries the fallback units and declination used when a trip omits
// them. It is passed in so that the model package stays free of configuration
// dependencies.
type Defaults struct {
	LengthUnit  units.LengthUnit
	AngleUnit   units.AngleUnit
	Declination float64
}

// collector accumulates issues while validating.
type collector struct {
	issues Issues
}

// fail records a fatal finding.
func (c *collector) fail(code, path, format string, args ...any) {
	c.issues = append(c.issues, Issue{
		Severity: SeverityError,
		Code:     code,
		Path:     path,
		Message:  fmt.Sprintf(format, args...),
	})
}

// warn records a non fatal finding.
func (c *collector) warn(code, path, format string, args ...any) {
	c.issues = append(c.issues, Issue{
		Severity: SeverityWarning,
		Code:     code,
		Path:     path,
		Message:  fmt.Sprintf(format, args...),
	})
}

// Validate checks the structural and physical consistency of a survey. It never
// mutates the input and always returns findings in a deterministic order.
func Validate(survey Survey, defaults Defaults) Issues {
	collect := &collector{}
	if strings.TrimSpace(survey.Cave) == "" {
		collect.fail("cave-missing", "survey.cave", "the survey must name the cave it describes")
	}
	if len(survey.Trips) == 0 {
		collect.fail("survey-empty", "survey.trips", "the survey contains no trips")
	}
	instruments := validateInstruments(collect, survey)
	seenTrips := make(map[string]bool, len(survey.Trips))
	fixedStations := 0
	for index, trip := range survey.Trips {
		path := fmt.Sprintf("survey.trips[%s]", tripLabel(trip, index))
		fixedStations += validateTrip(collect, path, trip, instruments, defaults, seenTrips)
	}
	if fixedStations == 0 && len(survey.Trips) > 0 {
		collect.warn("control-missing", "survey.trips",
			"no station is flagged fixed, traverses will be anchored at a synthetic origin")
	}
	return collect.issues.Sorted()
}

// tripLabel produces a stable path fragment for a trip.
func tripLabel(trip Trip, index int) string {
	if strings.TrimSpace(trip.ID) != "" {
		return trip.ID
	}
	return fmt.Sprintf("#%d", index)
}

// validateInstruments checks the instrument catalogue and returns the index of
// well formed instruments.
func validateInstruments(collect *collector, survey Survey) InstrumentIndex {
	index := make(InstrumentIndex, len(survey.Instruments))
	for position, instrument := range survey.Instruments {
		path := fmt.Sprintf("survey.instruments[%d]", position)
		if strings.TrimSpace(instrument.ID) == "" {
			collect.fail("instrument-id-empty", path, "instrument identifier must not be empty")
			continue
		}
		path = fmt.Sprintf("survey.instruments[%s]", instrument.ID)
		if _, exists := index[instrument.ID]; exists {
			collect.fail("instrument-id-duplicate", path, "instrument identifier %q is declared more than once", instrument.ID)
			continue
		}
		if instrument.LengthUnit != "" {
			if _, err := units.ParseLengthUnit(instrument.LengthUnit); err != nil {
				collect.fail("instrument-length-unit", path+".lengthUnit", "%v", err)
			}
		}
		if instrument.AngleUnit != "" {
			if _, err := units.ParseAngleUnit(instrument.AngleUnit); err != nil {
				collect.fail("instrument-angle-unit", path+".angleUnit", "%v", err)
			}
		}
		if instrument.TapeScale != nil {
			scale := *instrument.TapeScale
			if !isFinite(scale) || scale <= 0 {
				collect.fail("instrument-tape-scale", path+".tapeScale", "tape scale must be a positive finite number, got %s", units.Format(scale, 6))
			} else if scale < 0.5 || scale > 2 {
				collect.warn("instrument-tape-scale-unusual", path+".tapeScale", "tape scale %s is far from unity", units.Format(scale, 6))
			}
		}
		for name, value := range map[string]float64{
			".tapeCorrection":        instrument.TapeCorrection,
			".azimuthCorrection":     instrument.AzimuthCorrection,
			".inclinationCorrection": instrument.InclinationCorrection,
		} {
			if !isFinite(value) {
				collect.fail("instrument-correction-not-finite", path+name, "correction must be a finite number")
			}
		}
		if instrument.Declination != nil && !isFinite(*instrument.Declination) {
			collect.fail("instrument-declination-not-finite", path+".declination", "declination must be a finite number")
		}
		index[instrument.ID] = instrument
	}
	return index
}

// validateTrip checks one trip and returns the number of fixed stations found.
func validateTrip(collect *collector, path string, trip Trip, instruments InstrumentIndex, defaults Defaults, seenTrips map[string]bool) int {
	if strings.TrimSpace(trip.ID) == "" {
		collect.fail("trip-id-empty", path+".id", "trip identifier must not be empty")
	} else if seenTrips[trip.ID] {
		collect.fail("trip-id-duplicate", path+".id", "trip identifier %q is declared more than once", trip.ID)
	} else {
		seenTrips[trip.ID] = true
	}
	if trip.Date != "" {
		if _, err := time.Parse("2006-01-02", trip.Date); err != nil {
			collect.fail("trip-date-format", path+".date", "date %q must use the YYYY-MM-DD form", trip.Date)
		}
	}
	lengthUnit := defaults.LengthUnit
	if trip.LengthUnit != "" {
		parsed, err := units.ParseLengthUnit(trip.LengthUnit)
		if err != nil {
			collect.fail("trip-length-unit", path+".lengthUnit", "%v", err)
		} else {
			lengthUnit = parsed
		}
	}
	angleUnit := defaults.AngleUnit
	if trip.AngleUnit != "" {
		parsed, err := units.ParseAngleUnit(trip.AngleUnit)
		if err != nil {
			collect.fail("trip-angle-unit", path+".angleUnit", "%v", err)
		} else {
			angleUnit = parsed
		}
	}
	if trip.Declination != nil && !isFinite(*trip.Declination) {
		collect.fail("trip-declination-not-finite", path+".declination", "declination must be a finite number")
	}
	if trip.Instrument != "" {
		if _, ok := instruments[trip.Instrument]; !ok {
			collect.fail("trip-instrument-unknown", path+".instrument", "instrument %q is not declared", trip.Instrument)
		}
	}
	if len(trip.Shots) == 0 {
		collect.warn("trip-no-shots", path+".shots", "trip %q declares no shots", trip.ID)
	}
	declared, fixedCount := validateStations(collect, path, trip, lengthUnit)
	validateShots(collect, path, trip, instruments, declared, angleUnit)
	return fixedCount
}

// validateStations checks station declarations and returns the declared names
// together with the number of control stations.
func validateStations(collect *collector, path string, trip Trip, lengthUnit units.LengthUnit) (map[string]bool, int) {
	declared := make(map[string]bool, len(trip.Stations))
	normalized := make(map[string]string, len(trip.Stations))
	fixedCount := 0
	for position, station := range trip.Stations {
		stationPath := fmt.Sprintf("%s.stations[%d]", path, position)
		name := strings.TrimSpace(station.Name)
		if name == "" {
			collect.fail("station-name-empty", stationPath+".name", "station name must not be empty")
			continue
		}
		if name != station.Name {
			collect.warn("station-name-padded", stationPath+".name", "station name %q has surrounding whitespace", station.Name)
		}
		stationPath = fmt.Sprintf("%s.stations[%s]", path, name)
		if declared[station.Name] {
			collect.fail("station-duplicate", stationPath, "station %q is declared more than once in trip %q", station.Name, trip.ID)
			continue
		}
		declared[station.Name] = true
		folded := strings.ToLower(name)
		if previous, ok := normalized[folded]; ok && previous != station.Name {
			collect.warn("station-case-collision", stationPath,
				"station %q differs from %q only by case or padding", station.Name, previous)
		} else if !ok {
			normalized[folded] = station.Name
		}
		for _, flag := range station.Flags {
			canonical := strings.ToLower(strings.TrimSpace(flag))
			if canonical == "" {
				collect.fail("station-flag-empty", stationPath+".flags", "station flag must not be empty")
				continue
			}
			if !isKnownFlag(canonical) {
				collect.fail("station-flag-unknown", stationPath+".flags",
					"unknown station flag %q (known flags: %s)", flag, strings.Join(knownFlags, ", "))
			}
		}
		hasFixedFlag := station.HasFlag(FlagFixed)
		if hasFixedFlag && station.Fixed == nil {
			collect.fail("station-fixed-missing-coordinates", stationPath+".fixed",
				"station %q is flagged fixed but carries no control coordinate", station.Name)
		}
		if station.Fixed == nil {
			continue
		}
		if !hasFixedFlag {
			collect.warn("station-fixed-flag-missing", stationPath+".flags",
				"station %q carries a control coordinate and is treated as fixed", station.Name)
		}
		fixedCount++
		unit := lengthUnit
		if station.Fixed.Unit != "" {
			parsed, err := units.ParseLengthUnit(station.Fixed.Unit)
			if err != nil {
				collect.fail("station-fixed-unit", stationPath+".fixed.unit", "%v", err)
			} else {
				unit = parsed
			}
		}
		if !unit.Valid() {
			collect.fail("station-fixed-unit", stationPath+".fixed.unit", "control coordinate unit is unresolved")
		}
		for axis, value := range map[string]float64{
			"east":  station.Fixed.East,
			"north": station.Fixed.North,
			"up":    station.Fixed.Up,
		} {
			if !isFinite(value) {
				collect.fail("station-fixed-not-finite", stationPath+".fixed."+axis, "control coordinate must be finite")
			}
		}
	}
	return declared, fixedCount
}

// validateShots checks every shot of a trip.
func validateShots(collect *collector, path string, trip Trip, instruments InstrumentIndex, declared map[string]bool, angleUnit units.AngleUnit) {
	seenShots := make(map[string]bool, len(trip.Shots))
	for position, shot := range trip.Shots {
		shotPath := fmt.Sprintf("%s.shots[%d]", path, position)
		if strings.TrimSpace(shot.ID) == "" {
			collect.fail("shot-id-empty", shotPath+".id", "shot identifier must not be empty")
		} else {
			shotPath = fmt.Sprintf("%s.shots[%s]", path, shot.ID)
			if seenShots[shot.ID] {
				collect.fail("shot-id-duplicate", shotPath, "shot identifier %q is declared more than once in trip %q", shot.ID, trip.ID)
			}
			seenShots[shot.ID] = true
		}
		if strings.TrimSpace(shot.From) == "" {
			collect.fail("shot-from-empty", shotPath+".from", "shot origin station must not be empty")
		}
		if strings.TrimSpace(shot.To) == "" {
			collect.fail("shot-to-empty", shotPath+".to", "shot target station must not be empty")
		}
		if shot.From != "" && shot.From == shot.To {
			collect.fail("shot-self-loop", shotPath, "shot connects station %q to itself", shot.From)
		}
		for _, endpoint := range [2]string{shot.From, shot.To} {
			if endpoint == "" || declared[endpoint] {
				continue
			}
			collect.warn("shot-station-undeclared", shotPath,
				"station %q is used by a shot but not declared in trip %q", endpoint, trip.ID)
		}
		if shot.Instrument != "" {
			if _, ok := instruments[shot.Instrument]; !ok {
				collect.fail("shot-instrument-unknown", shotPath+".instrument", "instrument %q is not declared", shot.Instrument)
			}
		}
		validateShotMeasurements(collect, shotPath, shot, angleUnit)
	}
}

// validateShotMeasurements checks the numeric readings of one shot.
func validateShotMeasurements(collect *collector, shotPath string, shot Shot, angleUnit units.AngleUnit) {
	if !isFinite(shot.Distance) {
		collect.fail("shot-distance-not-finite", shotPath+".distance", "distance must be a finite number")
	} else if shot.Distance < 0 {
		collect.fail("shot-distance-negative", shotPath+".distance", "distance %s must not be negative", units.Format(shot.Distance, 4))
	} else if shot.Distance == 0 {
		collect.warn("shot-distance-zero", shotPath+".distance", "shot has zero length and contributes no displacement")
	}
	if !isFinite(shot.Azimuth) {
		collect.fail("shot-azimuth-not-finite", shotPath+".azimuth", "azimuth must be a finite number")
	} else if shot.Azimuth < 0 || shot.Azimuth > units.FullCircle(angleUnit) {
		collect.warn("shot-azimuth-out-of-range", shotPath+".azimuth",
			"azimuth %s is outside [0, %s] and will be wrapped", units.Format(shot.Azimuth, 4), units.Format(units.FullCircle(angleUnit), 0))
	}
	checkInclination(collect, shotPath+".inclination", "shot-inclination-range", shot.Inclination, angleUnit)
	if shot.BackDistance != nil {
		value := *shot.BackDistance
		if !isFinite(value) {
			collect.fail("shot-back-distance-not-finite", shotPath+".backDistance", "backsight distance must be finite")
		} else if value < 0 {
			collect.fail("shot-back-distance-negative", shotPath+".backDistance", "backsight distance must not be negative")
		}
	}
	if shot.BackAzimuth != nil && !isFinite(*shot.BackAzimuth) {
		collect.fail("shot-back-azimuth-not-finite", shotPath+".backAzimuth", "backsight azimuth must be finite")
	}
	if shot.BackInclination != nil {
		checkInclination(collect, shotPath+".backInclination", "shot-back-inclination-range", *shot.BackInclination, angleUnit)
	}
}

// checkInclination validates one inclination reading expressed in angleUnit.
func checkInclination(collect *collector, path, code string, value float64, angleUnit units.AngleUnit) {
	if !isFinite(value) {
		collect.fail(code, path, "inclination must be a finite number")
		return
	}
	degrees, err := units.AngleToDegrees(value, angleUnit)
	if err != nil {
		collect.fail(code, path, "%v", err)
		return
	}
	if err := units.ValidateInclination(degrees); err != nil {
		collect.fail(code, path, "%v", err)
	}
}

// isFinite reports whether value is a usable measurement.
func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
