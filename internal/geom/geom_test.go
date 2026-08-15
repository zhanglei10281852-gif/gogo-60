package geom

import (
	"math"
	"testing"
)

func TestFromPolarCardinalDirections(t *testing.T) {
	north := FromPolar(10, 0, 0)
	if math.Abs(north.North-10) > 1e-9 || math.Abs(north.East) > 1e-9 || math.Abs(north.Up) > 1e-9 {
		t.Fatalf("north shot produced %+v", north)
	}
	east := FromPolar(10, 90, 0)
	if math.Abs(east.East-10) > 1e-9 || math.Abs(east.North) > 1e-9 {
		t.Fatalf("east shot produced %+v", east)
	}
	down := FromPolar(10, 123, -90)
	if math.Abs(down.Up+10) > 1e-9 || down.HorizontalLength() > 1e-9 {
		t.Fatalf("vertical shot produced %+v", down)
	}
}

func TestFromPolarRoundTrip(t *testing.T) {
	cases := []struct {
		distance    float64
		azimuth     float64
		inclination float64
	}{
		{12.5, 37.5, -8.25},
		{4, 300, 15},
		{25, 179.9, 0},
	}
	for _, sample := range cases {
		vector := FromPolar(sample.distance, sample.azimuth, sample.inclination)
		if math.Abs(vector.Length()-sample.distance) > 1e-9 {
			t.Fatalf("length of %+v = %v, want %v", sample, vector.Length(), sample.distance)
		}
		if math.Abs(vector.Azimuth()-sample.azimuth) > 1e-6 {
			t.Fatalf("azimuth of %+v = %v", sample, vector.Azimuth())
		}
		if math.Abs(vector.Inclination()-sample.inclination) > 1e-6 {
			t.Fatalf("inclination of %+v = %v", sample, vector.Inclination())
		}
	}
}

func TestVectorArithmetic(t *testing.T) {
	a := Vector{East: 1, North: 2, Up: 3}
	b := Vector{East: 4, North: 5, Up: 6}
	if sum := a.Add(b); sum != (Vector{East: 5, North: 7, Up: 9}) {
		t.Fatalf("Add produced %+v", sum)
	}
	if difference := b.Sub(a); difference != (Vector{East: 3, North: 3, Up: 3}) {
		t.Fatalf("Sub produced %+v", difference)
	}
	if scaled := a.Scale(2); scaled != (Vector{East: 2, North: 4, Up: 6}) {
		t.Fatalf("Scale produced %+v", scaled)
	}
	if negated := a.Negate(); negated != (Vector{East: -1, North: -2, Up: -3}) {
		t.Fatalf("Negate produced %+v", negated)
	}
	if total := Sum([]Vector{a, b}); total != (Vector{East: 5, North: 7, Up: 9}) {
		t.Fatalf("Sum produced %+v", total)
	}
	if !a.IsFinite() {
		t.Fatal("a finite vector reported as non finite")
	}
	if (Vector{East: math.NaN()}).IsFinite() {
		t.Fatal("a NaN vector reported as finite")
	}
	if rounded := (Vector{East: 1.23456789}).Round(3); rounded.East != 1.235 {
		t.Fatalf("Round produced %+v", rounded)
	}
	if Zero() != (Vector{}) {
		t.Fatal("Zero is not the additive identity")
	}
	if got := (Vector{Up: -4}).VerticalLength(); got != 4 {
		t.Fatalf("VerticalLength = %v, want 4", got)
	}
}

func TestHorizontalProjection(t *testing.T) {
	if got := HorizontalProjection(10, 0); math.Abs(got-10) > 1e-9 {
		t.Fatalf("HorizontalProjection(10,0) = %v", got)
	}
	if got := HorizontalProjection(10, 60); math.Abs(got-5) > 1e-9 {
		t.Fatalf("HorizontalProjection(10,60) = %v, want 5", got)
	}
}

func TestBoundingBox(t *testing.T) {
	empty := NewBoundingBox(nil)
	if !empty.Empty || empty.Extent() != Zero() {
		t.Fatalf("empty bounding box produced %+v", empty)
	}
	box := NewBoundingBox([]Vector{
		{East: -1, North: 2, Up: 5},
		{East: 4, North: -3, Up: -2},
	})
	if box.Min != (Vector{East: -1, North: -3, Up: -2}) {
		t.Fatalf("bounding box minimum is %+v", box.Min)
	}
	if box.Max != (Vector{East: 4, North: 2, Up: 5}) {
		t.Fatalf("bounding box maximum is %+v", box.Max)
	}
	if box.Extent() != (Vector{East: 5, North: 5, Up: 7}) {
		t.Fatalf("bounding box extent is %+v", box.Extent())
	}
}

func TestStatistics(t *testing.T) {
	samples := []float64{4, 1, 3, 2}
	if got := Mean(samples); got != 2.5 {
		t.Fatalf("Mean = %v, want 2.5", got)
	}
	if got := Median(samples); got != 2.5 {
		t.Fatalf("Median = %v, want 2.5", got)
	}
	if samples[0] != 4 {
		t.Fatal("Median mutated its input")
	}
	if got := Median([]float64{5, 1, 3}); got != 3 {
		t.Fatalf("Median of odd sample = %v, want 3", got)
	}
	if got := Mean(nil); got != 0 {
		t.Fatalf("Mean of empty sample = %v", got)
	}
	if got := StdDev([]float64{2}); got != 0 {
		t.Fatalf("StdDev of a single sample = %v", got)
	}
	if got := StdDev([]float64{2, 4, 4, 4, 5, 5, 7, 9}); math.Abs(got-2) > 1e-9 {
		t.Fatalf("StdDev = %v, want 2", got)
	}
}
