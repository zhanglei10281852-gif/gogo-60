// Package units provides deterministic parsing, conversion and normalization of
// the measurement units used in speleological survey data reduction.
//
// All internal computation in CaveLoop is performed in meters and decimal
// degrees. Conversions in this package are pure functions with no hidden state,
// so repeated runs on identical inputs produce byte-identical results.
package units

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// LengthUnit identifies a linear measurement unit used by a survey trip.
type LengthUnit string

// AngleUnit identifies an angular measurement unit used by a survey trip.
type AngleUnit string

// Supported length units.
const (
	Meters LengthUnit = "m"
	Feet   LengthUnit = "ft"
)

// Supported angle units.
const (
	Degrees AngleUnit = "deg"
	Grads   AngleUnit = "grad"
)

// Conversion constants. MetersPerFoot is the exact international foot.
const (
	MetersPerFoot   = 0.3048
	DegreesPerGrad  = 0.9
	FullCircleDeg   = 360.0
	FullCircleGrad  = 400.0
	MaxInclination  = 90.0
	angleTolerance  = 1e-9
	lengthTolerance = 1e-9
)

// compassPoints holds the 16 point compass rose used for human readable output.
var compassPoints = [16]string{
	"N", "NNE", "NE", "ENE",
	"E", "ESE", "SE", "SSE",
	"S", "SSW", "SW", "WSW",
	"W", "WNW", "NW", "NNW",
}

// ParseLengthUnit resolves a user supplied spelling to a canonical LengthUnit.
func ParseLengthUnit(raw string) (LengthUnit, error) {
	switch normalizeToken(raw) {
	case "m", "meter", "meters", "metre", "metres":
		return Meters, nil
	case "ft", "feet", "foot":
		return Feet, nil
	case "":
		return "", fmt.Errorf("length unit is empty")
	default:
		return "", fmt.Errorf("unsupported length unit %q (want m or ft)", raw)
	}
}

// ParseAngleUnit resolves a user supplied spelling to a canonical AngleUnit.
func ParseAngleUnit(raw string) (AngleUnit, error) {
	switch normalizeToken(raw) {
	case "deg", "degree", "degrees", "d":
		return Degrees, nil
	case "grad", "grads", "gradian", "gradians", "gon", "gons":
		return Grads, nil
	case "":
		return "", fmt.Errorf("angle unit is empty")
	default:
		return "", fmt.Errorf("unsupported angle unit %q (want deg or grad)", raw)
	}
}

// normalizeToken lowercases and trims a unit token.
func normalizeToken(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// Valid reports whether the length unit is one of the supported units.
func (u LengthUnit) Valid() bool {
	return u == Meters || u == Feet
}

// Valid reports whether the angle unit is one of the supported units.
func (u AngleUnit) Valid() bool {
	return u == Degrees || u == Grads
}

// String renders the canonical unit token.
func (u LengthUnit) String() string { return string(u) }

// String renders the canonical unit token.
func (u AngleUnit) String() string { return string(u) }

// LengthToMeters converts value expressed in u into meters.
func LengthToMeters(value float64, u LengthUnit) (float64, error) {
	switch u {
	case Meters:
		return value, nil
	case Feet:
		return value * MetersPerFoot, nil
	default:
		return 0, fmt.Errorf("cannot convert length: unknown unit %q", string(u))
	}
}

// LengthFromMeters converts a value in meters into the requested unit.
func LengthFromMeters(meters float64, u LengthUnit) (float64, error) {
	switch u {
	case Meters:
		return meters, nil
	case Feet:
		return meters / MetersPerFoot, nil
	default:
		return 0, fmt.Errorf("cannot convert length: unknown unit %q", string(u))
	}
}

// AngleToDegrees converts an angular value expressed in u into decimal degrees.
func AngleToDegrees(value float64, u AngleUnit) (float64, error) {
	switch u {
	case Degrees:
		return value, nil
	case Grads:
		return value * DegreesPerGrad, nil
	default:
		return 0, fmt.Errorf("cannot convert angle: unknown unit %q", string(u))
	}
}

// AngleFromDegrees converts decimal degrees into the requested angle unit.
func AngleFromDegrees(degrees float64, u AngleUnit) (float64, error) {
	switch u {
	case Degrees:
		return degrees, nil
	case Grads:
		return degrees / DegreesPerGrad, nil
	default:
		return 0, fmt.Errorf("cannot convert angle: unknown unit %q", string(u))
	}
}

// FullCircle returns the numeric value of a full revolution in unit u.
func FullCircle(u AngleUnit) float64 {
	if u == Grads {
		return FullCircleGrad
	}
	return FullCircleDeg
}

// NormalizeAzimuth wraps an azimuth in degrees into the half open range
// [0, 360). Values that land exactly on 360 after rounding collapse to 0 so the
// output domain never contains two representations of north.
func NormalizeAzimuth(deg float64) float64 {
	if math.IsNaN(deg) || math.IsInf(deg, 0) {
		return math.NaN()
	}
	wrapped := math.Mod(deg, FullCircleDeg)
	if wrapped < 0 {
		wrapped += FullCircleDeg
	}
	if FullCircleDeg-wrapped < angleTolerance {
		return 0
	}
	if wrapped < angleTolerance {
		return 0
	}
	return wrapped
}

// OppositeAzimuth returns the reciprocal bearing of deg.
func OppositeAzimuth(deg float64) float64 {
	return NormalizeAzimuth(deg + 180)
}

// AzimuthDelta returns the signed shortest rotation, in degrees, that moves
// from azimuth a to azimuth b. The result lies in (-180, 180].
func AzimuthDelta(a, b float64) float64 {
	diff := math.Mod(b-a, FullCircleDeg)
	if diff <= -180 {
		diff += FullCircleDeg
	}
	if diff > 180 {
		diff -= FullCircleDeg
	}
	return diff
}

// AzimuthSeparation returns the absolute shortest angle between two azimuths.
func AzimuthSeparation(a, b float64) float64 {
	return math.Abs(AzimuthDelta(a, b))
}

// AverageAzimuth returns the deterministic circular midpoint of two azimuths.
// When the two bearings are exactly opposed the midpoint is ambiguous, so the
// rotation is always taken in the positive direction from a.
func AverageAzimuth(a, b float64) float64 {
	delta := AzimuthDelta(NormalizeAzimuth(a), NormalizeAzimuth(b))
	if math.Abs(math.Abs(delta)-180) < angleTolerance {
		delta = 180
	}
	return NormalizeAzimuth(NormalizeAzimuth(a) + delta/2)
}

// ValidateInclination reports an error when an inclination in degrees falls
// outside the physically meaningful range [-90, +90].
func ValidateInclination(deg float64) error {
	if math.IsNaN(deg) || math.IsInf(deg, 0) {
		return fmt.Errorf("inclination is not a finite number")
	}
	if deg > MaxInclination+angleTolerance || deg < -MaxInclination-angleTolerance {
		return fmt.Errorf("inclination %s deg is outside [-90, +90]", Format(deg, 4))
	}
	return nil
}

// ClampInclination snaps inclinations that exceed the vertical by less than the
// angular tolerance back onto the vertical.
func ClampInclination(deg float64) float64 {
	if deg > MaxInclination {
		return MaxInclination
	}
	if deg < -MaxInclination {
		return -MaxInclination
	}
	return deg
}

// DegToRad converts decimal degrees into radians.
func DegToRad(deg float64) float64 { return deg * math.Pi / 180 }

// RadToDeg converts radians into decimal degrees.
func RadToDeg(rad float64) float64 { return rad * 180 / math.Pi }

// CompassPoint names the 16 point compass sector containing the azimuth.
func CompassPoint(deg float64) string {
	normalized := NormalizeAzimuth(deg)
	if math.IsNaN(normalized) {
		return "??"
	}
	sector := int(math.Floor(normalized/22.5 + 0.5))
	return compassPoints[sector%16]
}

// RoundTo rounds value to the requested number of decimal digits using
// half away from zero rounding, which keeps output stable across platforms.
func RoundTo(value float64, digits int) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	if digits < 0 {
		digits = 0
	}
	if digits > 12 {
		digits = 12
	}
	scale := math.Pow(10, float64(digits))
	scaled := value * scale
	rounded := math.Floor(math.Abs(scaled) + 0.5)
	if scaled < 0 {
		rounded = -rounded
	}
	result := rounded / scale
	if result == 0 {
		return 0
	}
	return result
}

// Format renders a float with a fixed number of decimals and without a signed
// zero, which makes textual diffs of reports stable.
func Format(value float64, digits int) string {
	rounded := RoundTo(value, digits)
	if rounded == 0 {
		rounded = 0
	}
	return strconv.FormatFloat(rounded, 'f', digits, 64)
}

// NearlyEqual compares two floats with an absolute epsilon.
func NearlyEqual(a, b, epsilon float64) bool {
	if epsilon <= 0 {
		epsilon = lengthTolerance
	}
	return math.Abs(a-b) <= epsilon
}

// PartsPerMillion expresses an error magnitude relative to a traverse length.
// A zero or negative length yields zero to avoid propagating infinities.
func PartsPerMillion(errorMagnitude, traverseLength float64) float64 {
	if traverseLength <= lengthTolerance {
		return 0
	}
	return errorMagnitude / traverseLength * 1e6
}
