// Package geom implements the small amount of three dimensional geometry that
// cave traverse reduction needs: a local cartesian vector in the east / north /
// up frame plus the polar to cartesian conversion used for a single shot.
package geom

import (
	"math"
	"sort"

	"CaveLoop/internal/units"
)

// Vector is a displacement in the local cave frame. East and North are
// horizontal, Up is positive towards the surface, so cave depth is negative Up.
type Vector struct {
	East  float64 `json:"east"`
	North float64 `json:"north"`
	Up    float64 `json:"up"`
}

// Zero is the additive identity vector.
func Zero() Vector { return Vector{} }

// Add returns v + other.
func (v Vector) Add(other Vector) Vector {
	return Vector{East: v.East + other.East, North: v.North + other.North, Up: v.Up + other.Up}
}

// Sub returns v - other.
func (v Vector) Sub(other Vector) Vector {
	return Vector{East: v.East - other.East, North: v.North - other.North, Up: v.Up - other.Up}
}

// Scale multiplies every component by factor.
func (v Vector) Scale(factor float64) Vector {
	return Vector{East: v.East * factor, North: v.North * factor, Up: v.Up * factor}
}

// Negate flips the direction of the vector.
func (v Vector) Negate() Vector { return v.Scale(-1) }

// Length is the euclidean magnitude of the vector.
func (v Vector) Length() float64 {
	return math.Sqrt(v.East*v.East + v.North*v.North + v.Up*v.Up)
}

// HorizontalLength is the magnitude of the horizontal projection.
func (v Vector) HorizontalLength() float64 {
	return math.Sqrt(v.East*v.East + v.North*v.North)
}

// VerticalLength is the absolute vertical extent.
func (v Vector) VerticalLength() float64 { return math.Abs(v.Up) }

// IsFinite reports whether every component is a finite number.
func (v Vector) IsFinite() bool {
	for _, component := range [3]float64{v.East, v.North, v.Up} {
		if math.IsNaN(component) || math.IsInf(component, 0) {
			return false
		}
	}
	return true
}

// Round returns a copy with every component rounded to digits decimals.
func (v Vector) Round(digits int) Vector {
	return Vector{
		East:  units.RoundTo(v.East, digits),
		North: units.RoundTo(v.North, digits),
		Up:    units.RoundTo(v.Up, digits),
	}
}

// Azimuth returns the horizontal bearing of the vector in degrees. A vector
// with no horizontal component reports zero, matching the convention that a
// purely vertical shot carries no usable bearing.
func (v Vector) Azimuth() float64 {
	if v.HorizontalLength() < 1e-12 {
		return 0
	}
	return units.NormalizeAzimuth(units.RadToDeg(math.Atan2(v.East, v.North)))
}

// Inclination returns the vertical angle of the vector in degrees.
func (v Vector) Inclination() float64 {
	horizontal := v.HorizontalLength()
	if horizontal < 1e-12 {
		if v.Up > 0 {
			return units.MaxInclination
		}
		if v.Up < 0 {
			return -units.MaxInclination
		}
		return 0
	}
	return units.RadToDeg(math.Atan2(v.Up, horizontal))
}

// FromPolar converts a reduced shot measurement into a displacement vector.
// slopeDistance is in meters, azimuth and inclination in decimal degrees.
func FromPolar(slopeDistance, azimuthDeg, inclinationDeg float64) Vector {
	inclinationRad := units.DegToRad(inclinationDeg)
	azimuthRad := units.DegToRad(units.NormalizeAzimuth(azimuthDeg))
	horizontal := slopeDistance * math.Cos(inclinationRad)
	return Vector{
		East:  horizontal * math.Sin(azimuthRad),
		North: horizontal * math.Cos(azimuthRad),
		Up:    slopeDistance * math.Sin(inclinationRad),
	}
}

// HorizontalProjection returns the horizontal distance covered by a shot.
func HorizontalProjection(slopeDistance, inclinationDeg float64) float64 {
	return math.Abs(slopeDistance * math.Cos(units.DegToRad(inclinationDeg)))
}

// Sum accumulates a slice of vectors in the order supplied, which keeps the
// floating point result reproducible.
func Sum(vectors []Vector) Vector {
	total := Zero()
	for _, vector := range vectors {
		total = total.Add(vector)
	}
	return total
}

// BoundingBox describes the extent of a set of coordinates.
type BoundingBox struct {
	Min   Vector `json:"min"`
	Max   Vector `json:"max"`
	Empty bool   `json:"empty"`
}

// NewBoundingBox builds the axis aligned extent of the supplied points.
func NewBoundingBox(points []Vector) BoundingBox {
	if len(points) == 0 {
		return BoundingBox{Empty: true}
	}
	box := BoundingBox{Min: points[0], Max: points[0]}
	for _, point := range points[1:] {
		box.Min.East = math.Min(box.Min.East, point.East)
		box.Min.North = math.Min(box.Min.North, point.North)
		box.Min.Up = math.Min(box.Min.Up, point.Up)
		box.Max.East = math.Max(box.Max.East, point.East)
		box.Max.North = math.Max(box.Max.North, point.North)
		box.Max.Up = math.Max(box.Max.Up, point.Up)
	}
	return box
}

// Extent returns the size of the bounding box along each axis.
func (b BoundingBox) Extent() Vector {
	if b.Empty {
		return Zero()
	}
	return b.Max.Sub(b.Min)
}

// Median returns the median of the supplied samples without mutating the input.
func Median(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := make([]float64, len(samples))
	copy(sorted, samples)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

// Mean returns the arithmetic mean of the samples, or zero when empty.
func Mean(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	total := 0.0
	for _, sample := range samples {
		total += sample
	}
	return total / float64(len(samples))
}

// StdDev returns the population standard deviation of the samples.
func StdDev(samples []float64) float64 {
	if len(samples) < 2 {
		return 0
	}
	mean := Mean(samples)
	accumulator := 0.0
	for _, sample := range samples {
		delta := sample - mean
		accumulator += delta * delta
	}
	return math.Sqrt(accumulator / float64(len(samples)))
}
