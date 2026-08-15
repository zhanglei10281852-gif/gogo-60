// Package reduce turns raw field notes into reduced survey legs.
//
// Reduction resolves the instrument set of every shot, applies tape, compass
// and clinometer corrections plus magnetic declination, converts every reading
// into meters and decimal degrees, reconciles foresights against backsights and
// finally expresses each leg as a displacement vector in the east / north / up
// frame.
package reduce

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"CaveLoop/internal/config"
	"CaveLoop/internal/geom"
	"CaveLoop/internal/model"
	"CaveLoop/internal/units"
)

// Note codes attached to a reduced shot.
const (
	NoteBacksightAzimuthOutOfTolerance     = "backsight-azimuth-out-of-tolerance"
	NoteBacksightInclinationOutOfTolerance = "backsight-inclination-out-of-tolerance"
	NoteBacksightDistanceOutOfTolerance    = "backsight-distance-out-of-tolerance"
	NoteBacksightAveraged                  = "backsight-averaged"
	NoteAzimuthWrapped                     = "azimuth-wrapped"
	NoteVerticalShot                       = "vertical-shot"
	NoteZeroLength                         = "zero-length"
	NoteExcluded                           = "excluded-by-input"
)

// Reconciliation records how a foresight and a backsight were combined.
type Reconciliation struct {
	HasBacksight               bool    `json:"hasBacksight"`
	AzimuthAveraged            bool    `json:"azimuthAveraged"`
	InclinationAveraged        bool    `json:"inclinationAveraged"`
	DistanceAveraged           bool    `json:"distanceAveraged"`
	AzimuthDisagreementDeg     float64 `json:"azimuthDisagreementDeg"`
	InclinationDisagreementDeg float64 `json:"inclinationDisagreementDeg"`
	DistanceDisagreementMeters float64 `json:"distanceDisagreementMeters"`
	WithinTolerance            bool    `json:"withinTolerance"`
}

// Shot is a fully reduced survey leg.
type Shot struct {
	TripID            string         `json:"tripId"`
	ShotID            string         `json:"shotId"`
	From              string         `json:"from"`
	To                string         `json:"to"`
	InstrumentID      string         `json:"instrumentId,omitempty"`
	DistanceMeters    float64        `json:"distanceMeters"`
	AzimuthDeg        float64        `json:"azimuthDeg"`
	InclinationDeg    float64        `json:"inclinationDeg"`
	RawDistanceMeters float64        `json:"rawDistanceMeters"`
	RawAzimuthDeg     float64        `json:"rawAzimuthDeg"`
	RawInclinationDeg float64        `json:"rawInclinationDeg"`
	DeclinationDeg    float64        `json:"declinationDeg"`
	HorizontalMeters  float64        `json:"horizontalMeters"`
	Vector            geom.Vector    `json:"vector"`
	Reconciliation    Reconciliation `json:"reconciliation"`
	Excluded          bool           `json:"excluded"`
	Notes             []string       `json:"notes,omitempty"`
}

// Key is the stable identity of a reduced shot.
func (s Shot) Key() string { return s.TripID + "/" + s.ShotID }

// Pair returns the unordered station pair of the shot as a canonical string.
func (s Shot) Pair() string {
	if s.From <= s.To {
		return s.From + "\x00" + s.To
	}
	return s.To + "\x00" + s.From
}

// Station is a reduced station with merged flags and control coordinates.
type Station struct {
	Name    string       `json:"name"`
	Flags   []string     `json:"flags,omitempty"`
	Trips   []string     `json:"trips"`
	Fixed   *geom.Vector `json:"fixed,omitempty"`
	Note    string       `json:"note,omitempty"`
	Implied bool         `json:"implied"`
}

// HasFlag reports whether the reduced station carries the given flag.
func (s Station) HasFlag(flag string) bool {
	for _, candidate := range s.Flags {
		if candidate == flag {
			return true
		}
	}
	return false
}

// Result is the outcome of reducing a whole survey.
type Result struct {
	Cave         string       `json:"cave"`
	Region       string       `json:"region,omitempty"`
	Stations     []Station    `json:"stations"`
	Shots        []Shot       `json:"shots"`
	Issues       model.Issues `json:"issues,omitempty"`
	ExcludedShot int          `json:"excludedShots"`
}

// StationIndex maps station names to their position in Result.Stations.
func (r Result) StationIndex() map[string]int {
	index := make(map[string]int, len(r.Stations))
	for position, station := range r.Stations {
		index[station.Name] = position
	}
	return index
}

// ActiveShots returns the shots that participate in the network, i.e. the ones
// that are neither excluded nor of zero length.
func (r Result) ActiveShots() []Shot {
	active := make([]Shot, 0, len(r.Shots))
	for _, shot := range r.Shots {
		if shot.Excluded {
			continue
		}
		active = append(active, shot)
	}
	return active
}

// resolvedUnits holds the units and corrections in effect for a single shot.
type resolvedUnits struct {
	lengthUnit            units.LengthUnit
	angleUnit             units.AngleUnit
	instrumentID          string
	tapeCorrectionMeters  float64
	tapeScale             float64
	azimuthCorrectionDeg  float64
	clinoCorrectionDeg    float64
	declinationDeg        float64
	declinationSourceTrip bool
}

// Reduce converts a survey into reduced legs and stations.
func Reduce(survey model.Survey, cfg config.Config) (Result, error) {
	canonical := survey.Canonical()
	defaults := cfg.ModelDefaults()
	instruments := canonical.Index()
	result := Result{Cave: canonical.Cave, Region: canonical.Region}
	builder := newStationBuilder()
	issues := model.Issues{}
	for _, trip := range canonical.Trips {
		tripLength, tripAngle, err := tripUnits(trip, defaults)
		if err != nil {
			return Result{}, fmt.Errorf("trip %q: %w", trip.ID, err)
		}
		for _, station := range trip.Stations {
			stationIssues := builder.addDeclared(trip, station, tripLength)
			issues = append(issues, stationIssues...)
		}
		for _, shot := range trip.Shots {
			resolved, err := resolveShotUnits(trip, shot, instruments, defaults, tripLength, tripAngle)
			if err != nil {
				return Result{}, fmt.Errorf("trip %q shot %q: %w", trip.ID, shot.ID, err)
			}
			reduced, shotIssues, err := reduceShot(trip, shot, resolved, cfg.Tolerances)
			if err != nil {
				return Result{}, fmt.Errorf("trip %q shot %q: %w", trip.ID, shot.ID, err)
			}
			issues = append(issues, shotIssues...)
			builder.addImplied(trip.ID, shot.From)
			builder.addImplied(trip.ID, shot.To)
			if reduced.Excluded {
				result.ExcludedShot++
			}
			result.Shots = append(result.Shots, reduced)
		}
	}
	sort.SliceStable(result.Shots, func(a, b int) bool {
		if result.Shots[a].TripID != result.Shots[b].TripID {
			return result.Shots[a].TripID < result.Shots[b].TripID
		}
		return result.Shots[a].ShotID < result.Shots[b].ShotID
	})
	result.Stations = builder.build()
	result.Issues = issues.Sorted()
	return result, nil
}

// tripUnits resolves the units in effect for a trip.
func tripUnits(trip model.Trip, defaults model.Defaults) (units.LengthUnit, units.AngleUnit, error) {
	lengthUnit := defaults.LengthUnit
	if trip.LengthUnit != "" {
		parsed, err := units.ParseLengthUnit(trip.LengthUnit)
		if err != nil {
			return "", "", err
		}
		lengthUnit = parsed
	}
	if !lengthUnit.Valid() {
		lengthUnit = units.Meters
	}
	angleUnit := defaults.AngleUnit
	if trip.AngleUnit != "" {
		parsed, err := units.ParseAngleUnit(trip.AngleUnit)
		if err != nil {
			return "", "", err
		}
		angleUnit = parsed
	}
	if !angleUnit.Valid() {
		angleUnit = units.Degrees
	}
	return lengthUnit, angleUnit, nil
}

// resolveShotUnits determines the corrections in effect for a single shot.
func resolveShotUnits(trip model.Trip, shot model.Shot, instruments model.InstrumentIndex, defaults model.Defaults, tripLength units.LengthUnit, tripAngle units.AngleUnit) (resolvedUnits, error) {
	resolved := resolvedUnits{
		lengthUnit: tripLength,
		angleUnit:  tripAngle,
		tapeScale:  1,
	}
	declinationDeg, err := units.AngleToDegrees(defaults.Declination, defaults.AngleUnit)
	if err != nil {
		return resolvedUnits{}, err
	}
	resolved.declinationDeg = declinationDeg
	if trip.Declination != nil {
		tripDeclination, err := units.AngleToDegrees(*trip.Declination, tripAngle)
		if err != nil {
			return resolvedUnits{}, err
		}
		resolved.declinationDeg = tripDeclination
		resolved.declinationSourceTrip = true
	}
	instrumentID := shot.Instrument
	if instrumentID == "" {
		instrumentID = trip.Instrument
	}
	if instrumentID == "" {
		return resolved, nil
	}
	instrument, ok := instruments[instrumentID]
	if !ok {
		return resolvedUnits{}, fmt.Errorf("instrument %q is not declared", instrumentID)
	}
	resolved.instrumentID = instrumentID
	instrumentLength := tripLength
	if instrument.LengthUnit != "" {
		parsed, err := units.ParseLengthUnit(instrument.LengthUnit)
		if err != nil {
			return resolvedUnits{}, err
		}
		instrumentLength = parsed
	}
	instrumentAngle := tripAngle
	if instrument.AngleUnit != "" {
		parsed, err := units.ParseAngleUnit(instrument.AngleUnit)
		if err != nil {
			return resolvedUnits{}, err
		}
		instrumentAngle = parsed
	}
	tapeMeters, err := units.LengthToMeters(instrument.TapeCorrection, instrumentLength)
	if err != nil {
		return resolvedUnits{}, err
	}
	resolved.tapeCorrectionMeters = tapeMeters
	if instrument.TapeScale != nil {
		if *instrument.TapeScale <= 0 {
			return resolvedUnits{}, fmt.Errorf("instrument %q has a non positive tape scale", instrumentID)
		}
		resolved.tapeScale = *instrument.TapeScale
	}
	azimuthCorrection, err := units.AngleToDegrees(instrument.AzimuthCorrection, instrumentAngle)
	if err != nil {
		return resolvedUnits{}, err
	}
	resolved.azimuthCorrectionDeg = azimuthCorrection
	clinoCorrection, err := units.AngleToDegrees(instrument.InclinationCorrection, instrumentAngle)
	if err != nil {
		return resolvedUnits{}, err
	}
	resolved.clinoCorrectionDeg = clinoCorrection
	if instrument.Declination != nil {
		instrumentDeclination, err := units.AngleToDegrees(*instrument.Declination, instrumentAngle)
		if err != nil {
			return resolvedUnits{}, err
		}
		resolved.declinationDeg = instrumentDeclination
	}
	return resolved, nil
}

// reduceShot applies corrections, reconciles the backsight and builds the
// displacement vector for one shot.
func reduceShot(trip model.Trip, shot model.Shot, resolved resolvedUnits, tolerances config.Tolerances) (Shot, model.Issues, error) {
	issues := model.Issues{}
	path := fmt.Sprintf("survey.trips[%s].shots[%s]", trip.ID, shot.ID)
	rawDistance, err := units.LengthToMeters(shot.Distance, resolved.lengthUnit)
	if err != nil {
		return Shot{}, nil, err
	}
	rawAzimuth, err := units.AngleToDegrees(shot.Azimuth, resolved.angleUnit)
	if err != nil {
		return Shot{}, nil, err
	}
	rawInclination, err := units.AngleToDegrees(shot.Inclination, resolved.angleUnit)
	if err != nil {
		return Shot{}, nil, err
	}
	foreDistance := correctedDistance(rawDistance, resolved)
	foreAzimuth := units.NormalizeAzimuth(rawAzimuth + resolved.azimuthCorrectionDeg + resolved.declinationDeg)
	foreInclination := rawInclination + resolved.clinoCorrectionDeg
	if err := units.ValidateInclination(foreInclination); err != nil {
		return Shot{}, nil, fmt.Errorf("corrected inclination is invalid: %w", err)
	}
	foreInclination = units.ClampInclination(foreInclination)

	reduced := Shot{
		TripID:            trip.ID,
		ShotID:            shot.ID,
		From:              shot.From,
		To:                shot.To,
		InstrumentID:      resolved.instrumentID,
		RawDistanceMeters: rawDistance,
		RawAzimuthDeg:     units.NormalizeAzimuth(rawAzimuth),
		RawInclinationDeg: rawInclination,
		DeclinationDeg:    resolved.declinationDeg,
		Excluded:          shot.Excluded,
	}
	notes := make([]string, 0, 4)
	if shot.Excluded {
		notes = append(notes, NoteExcluded)
	}
	if !units.NearlyEqual(units.NormalizeAzimuth(rawAzimuth), rawAzimuth, 1e-9) {
		notes = append(notes, NoteAzimuthWrapped)
	}

	distance, azimuth, inclination, reconciliation, backNotes, backIssues, err := reconcile(
		path, shot, resolved, tolerances, foreDistance, foreAzimuth, foreInclination)
	if err != nil {
		return Shot{}, nil, err
	}
	notes = append(notes, backNotes...)
	issues = append(issues, backIssues...)

	if distance <= 1e-9 {
		notes = append(notes, NoteZeroLength)
	}
	if math.Abs(math.Abs(inclination)-units.MaxInclination) < 1e-6 {
		notes = append(notes, NoteVerticalShot)
	}
	reduced.DistanceMeters = distance
	reduced.AzimuthDeg = azimuth
	reduced.InclinationDeg = inclination
	reduced.HorizontalMeters = geom.HorizontalProjection(distance, inclination)
	reduced.Vector = geom.FromPolar(distance, azimuth, inclination)
	reduced.Reconciliation = reconciliation
	sort.Strings(notes)
	reduced.Notes = dedupe(notes)
	if !reduced.Vector.IsFinite() {
		return Shot{}, nil, fmt.Errorf("reduced displacement is not finite")
	}
	return reduced, issues, nil
}

// correctedDistance applies the tape scale and the tape correction.
func correctedDistance(rawMeters float64, resolved resolvedUnits) float64 {
	corrected := rawMeters*resolved.tapeScale + resolved.tapeCorrectionMeters
	if corrected < 0 {
		return 0
	}
	return corrected
}

// reconcile combines the foresight with an optional backsight.
func reconcile(path string, shot model.Shot, resolved resolvedUnits, tolerances config.Tolerances,
	foreDistance, foreAzimuth, foreInclination float64) (float64, float64, float64, Reconciliation, []string, model.Issues, error) {

	reconciliation := Reconciliation{WithinTolerance: true}
	notes := make([]string, 0, 3)
	issues := model.Issues{}
	distance := foreDistance
	azimuth := foreAzimuth
	inclination := foreInclination
	if !shot.HasBacksight() {
		return distance, azimuth, inclination, reconciliation, notes, issues, nil
	}
	reconciliation.HasBacksight = true

	if shot.BackAzimuth != nil {
		rawBack, err := units.AngleToDegrees(*shot.BackAzimuth, resolved.angleUnit)
		if err != nil {
			return 0, 0, 0, reconciliation, nil, nil, err
		}
		backAsForward := units.OppositeAzimuth(rawBack + resolved.azimuthCorrectionDeg + resolved.declinationDeg)
		disagreement := units.AzimuthSeparation(foreAzimuth, backAsForward)
		reconciliation.AzimuthDisagreementDeg = disagreement
		if disagreement <= tolerances.BacksightAzimuthDeg {
			azimuth = units.AverageAzimuth(foreAzimuth, backAsForward)
			reconciliation.AzimuthAveraged = true
			notes = append(notes, NoteBacksightAveraged)
		} else {
			reconciliation.WithinTolerance = false
			notes = append(notes, NoteBacksightAzimuthOutOfTolerance)
			issues = append(issues, model.Issue{
				Severity: model.SeverityWarning,
				Code:     "backsight-azimuth-disagreement",
				Path:     path + ".backAzimuth",
				Message: fmt.Sprintf("foresight and backsight disagree by %s deg, tolerance is %s deg",
					units.Format(disagreement, 3), units.Format(tolerances.BacksightAzimuthDeg, 3)),
			})
		}
	}

	if shot.BackInclination != nil {
		rawBack, err := units.AngleToDegrees(*shot.BackInclination, resolved.angleUnit)
		if err != nil {
			return 0, 0, 0, reconciliation, nil, nil, err
		}
		backAsForward := -(rawBack + resolved.clinoCorrectionDeg)
		disagreement := math.Abs(foreInclination - backAsForward)
		reconciliation.InclinationDisagreementDeg = disagreement
		if disagreement <= tolerances.BacksightInclinationDeg {
			inclination = units.ClampInclination((foreInclination + backAsForward) / 2)
			reconciliation.InclinationAveraged = true
			notes = append(notes, NoteBacksightAveraged)
		} else {
			reconciliation.WithinTolerance = false
			notes = append(notes, NoteBacksightInclinationOutOfTolerance)
			issues = append(issues, model.Issue{
				Severity: model.SeverityWarning,
				Code:     "backsight-inclination-disagreement",
				Path:     path + ".backInclination",
				Message: fmt.Sprintf("foresight and backsight disagree by %s deg, tolerance is %s deg",
					units.Format(disagreement, 3), units.Format(tolerances.BacksightInclinationDeg, 3)),
			})
		}
	}

	if shot.BackDistance != nil {
		rawBack, err := units.LengthToMeters(*shot.BackDistance, resolved.lengthUnit)
		if err != nil {
			return 0, 0, 0, reconciliation, nil, nil, err
		}
		backDistance := correctedDistance(rawBack, resolved)
		disagreement := math.Abs(foreDistance - backDistance)
		reconciliation.DistanceDisagreementMeters = disagreement
		allowed := math.Max(tolerances.BacksightDistanceMeters, tolerances.BacksightDistanceRatio*foreDistance)
		if disagreement <= allowed {
			distance = (foreDistance + backDistance) / 2
			reconciliation.DistanceAveraged = true
			notes = append(notes, NoteBacksightAveraged)
		} else {
			reconciliation.WithinTolerance = false
			notes = append(notes, NoteBacksightDistanceOutOfTolerance)
			issues = append(issues, model.Issue{
				Severity: model.SeverityWarning,
				Code:     "backsight-distance-disagreement",
				Path:     path + ".backDistance",
				Message: fmt.Sprintf("foresight and backsight differ by %s m, tolerance is %s m",
					units.Format(disagreement, 4), units.Format(allowed, 4)),
			})
		}
	}
	return distance, azimuth, inclination, reconciliation, notes, issues, nil
}

// dedupe removes repeated entries from a sorted slice.
func dedupe(sorted []string) []string {
	if len(sorted) == 0 {
		return nil
	}
	out := make([]string, 0, len(sorted))
	previous := ""
	for index, value := range sorted {
		if index > 0 && value == previous {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}

// stationBuilder merges station declarations coming from several trips.
type stationBuilder struct {
	byName map[string]*Station
	fixed  map[string]geom.Vector
}

// newStationBuilder creates an empty builder.
func newStationBuilder() *stationBuilder {
	return &stationBuilder{
		byName: make(map[string]*Station),
		fixed:  make(map[string]geom.Vector),
	}
}

// addDeclared registers a station declaration from a trip.
func (b *stationBuilder) addDeclared(trip model.Trip, station model.Station, tripLength units.LengthUnit) model.Issues {
	issues := model.Issues{}
	entry := b.ensure(station.Name)
	entry.Implied = false
	entry.Flags = mergeFlags(entry.Flags, station.CanonicalFlags())
	if station.Note != "" && entry.Note == "" {
		entry.Note = station.Note
	}
	b.recordTrip(entry, trip.ID)
	if station.Fixed == nil {
		return issues
	}
	unit := tripLength
	if station.Fixed.Unit != "" {
		parsed, err := units.ParseLengthUnit(station.Fixed.Unit)
		if err == nil {
			unit = parsed
		}
	}
	east, _ := units.LengthToMeters(station.Fixed.East, unit)
	north, _ := units.LengthToMeters(station.Fixed.North, unit)
	up, _ := units.LengthToMeters(station.Fixed.Up, unit)
	coordinate := geom.Vector{East: east, North: north, Up: up}
	if existing, ok := b.fixed[station.Name]; ok {
		if !sameCoordinate(existing, coordinate) {
			issues = append(issues, model.Issue{
				Severity: model.SeverityError,
				Code:     "control-conflict",
				Path:     fmt.Sprintf("survey.trips[%s].stations[%s].fixed", trip.ID, station.Name),
				Message: fmt.Sprintf("station %q already has a different control coordinate (%s vs %s)",
					station.Name, formatVector(existing), formatVector(coordinate)),
			})
		}
		return issues
	}
	b.fixed[station.Name] = coordinate
	stored := coordinate
	entry.Fixed = &stored
	entry.Flags = mergeFlags(entry.Flags, []string{model.FlagFixed})
	return issues
}

// addImplied registers a station that is only referenced by a shot.
func (b *stationBuilder) addImplied(tripID, name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	entry := b.ensure(name)
	b.recordTrip(entry, tripID)
}

// ensure returns the station entry, creating it when unknown.
func (b *stationBuilder) ensure(name string) *Station {
	if entry, ok := b.byName[name]; ok {
		return entry
	}
	entry := &Station{Name: name, Implied: true}
	b.byName[name] = entry
	return entry
}

// recordTrip appends a trip reference to a station without duplicates.
func (b *stationBuilder) recordTrip(entry *Station, tripID string) {
	if tripID == "" {
		return
	}
	for _, existing := range entry.Trips {
		if existing == tripID {
			return
		}
	}
	entry.Trips = append(entry.Trips, tripID)
}

// build finalises the station list in deterministic order.
func (b *stationBuilder) build() []Station {
	names := make([]string, 0, len(b.byName))
	for name := range b.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	stations := make([]Station, 0, len(names))
	for _, name := range names {
		entry := b.byName[name]
		sort.Strings(entry.Trips)
		stations = append(stations, *entry)
	}
	return stations
}

// mergeFlags unions two flag lists preserving canonical order.
func mergeFlags(current, additional []string) []string {
	present := make(map[string]bool, len(current)+len(additional))
	for _, flag := range current {
		present[flag] = true
	}
	for _, flag := range additional {
		present[flag] = true
	}
	ordered := make([]string, 0, len(present))
	for _, flag := range model.KnownFlags() {
		if present[flag] {
			ordered = append(ordered, flag)
			delete(present, flag)
		}
	}
	remaining := make([]string, 0, len(present))
	for flag := range present {
		remaining = append(remaining, flag)
	}
	sort.Strings(remaining)
	return append(ordered, remaining...)
}

// sameCoordinate compares two control coordinates within one millimeter.
func sameCoordinate(a, b geom.Vector) bool {
	return units.NearlyEqual(a.East, b.East, 1e-3) &&
		units.NearlyEqual(a.North, b.North, 1e-3) &&
		units.NearlyEqual(a.Up, b.Up, 1e-3)
}

// formatVector renders a coordinate for diagnostics.
func formatVector(v geom.Vector) string {
	return fmt.Sprintf("(%s, %s, %s)", units.Format(v.East, 3), units.Format(v.North, 3), units.Format(v.Up, 3))
}
