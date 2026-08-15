package units

import (
	"math"
	"testing"
)

func TestParseLengthUnit(t *testing.T) {
	cases := map[string]LengthUnit{
		"m": Meters, "M": Meters, " meters ": Meters, "metre": Meters,
		"ft": Feet, "FEET": Feet, "foot": Feet,
	}
	for input, want := range cases {
		got, err := ParseLengthUnit(input)
		if err != nil {
			t.Fatalf("ParseLengthUnit(%q) returned %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseLengthUnit(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParseLengthUnit("fathom"); err == nil {
		t.Fatal("ParseLengthUnit accepted an unsupported unit")
	}
	if _, err := ParseLengthUnit("  "); err == nil {
		t.Fatal("ParseLengthUnit accepted an empty unit")
	}
}

func TestParseAngleUnit(t *testing.T) {
	for _, input := range []string{"deg", "DEGREES", "d"} {
		got, err := ParseAngleUnit(input)
		if err != nil || got != Degrees {
			t.Fatalf("ParseAngleUnit(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"grad", "gons", "gradians"} {
		got, err := ParseAngleUnit(input)
		if err != nil || got != Grads {
			t.Fatalf("ParseAngleUnit(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := ParseAngleUnit("mils"); err == nil {
		t.Fatal("ParseAngleUnit accepted an unsupported unit")
	}
}

func TestLengthConversionRoundTrip(t *testing.T) {
	meters, err := LengthToMeters(100, Feet)
	if err != nil {
		t.Fatalf("LengthToMeters returned %v", err)
	}
	if !NearlyEqual(meters, 30.48, 1e-9) {
		t.Fatalf("100 ft = %v m, want 30.48", meters)
	}
	back, err := LengthFromMeters(meters, Feet)
	if err != nil {
		t.Fatalf("LengthFromMeters returned %v", err)
	}
	if !NearlyEqual(back, 100, 1e-9) {
		t.Fatalf("round trip produced %v, want 100", back)
	}
	if _, err := LengthToMeters(1, LengthUnit("chain")); err == nil {
		t.Fatal("LengthToMeters accepted an unknown unit")
	}
}

func TestAngleConversion(t *testing.T) {
	degrees, err := AngleToDegrees(400, Grads)
	if err != nil {
		t.Fatalf("AngleToDegrees returned %v", err)
	}
	if !NearlyEqual(degrees, 360, 1e-9) {
		t.Fatalf("400 grad = %v deg, want 360", degrees)
	}
	grads, err := AngleFromDegrees(90, Grads)
	if err != nil {
		t.Fatalf("AngleFromDegrees returned %v", err)
	}
	if !NearlyEqual(grads, 100, 1e-9) {
		t.Fatalf("90 deg = %v grad, want 100", grads)
	}
	if FullCircle(Grads) != 400 || FullCircle(Degrees) != 360 {
		t.Fatal("FullCircle returned the wrong revolution size")
	}
}

func TestNormalizeAzimuth(t *testing.T) {
	cases := map[float64]float64{
		0: 0, 360: 0, 720: 0, -90: 270, 450: 90, 359.9999999999: 0, 12.5: 12.5,
	}
	for input, want := range cases {
		if got := NormalizeAzimuth(input); !NearlyEqual(got, want, 1e-6) {
			t.Fatalf("NormalizeAzimuth(%v) = %v, want %v", input, got, want)
		}
	}
	if !math.IsNaN(NormalizeAzimuth(math.NaN())) {
		t.Fatal("NormalizeAzimuth should propagate NaN")
	}
}

func TestAzimuthDeltaAndAverage(t *testing.T) {
	if got := AzimuthDelta(350, 10); !NearlyEqual(got, 20, 1e-9) {
		t.Fatalf("AzimuthDelta(350,10) = %v, want 20", got)
	}
	if got := AzimuthDelta(10, 350); !NearlyEqual(got, -20, 1e-9) {
		t.Fatalf("AzimuthDelta(10,350) = %v, want -20", got)
	}
	if got := AzimuthSeparation(10, 350); !NearlyEqual(got, 20, 1e-9) {
		t.Fatalf("AzimuthSeparation(10,350) = %v, want 20", got)
	}
	if got := AverageAzimuth(350, 10); !NearlyEqual(got, 0, 1e-9) {
		t.Fatalf("AverageAzimuth(350,10) = %v, want 0", got)
	}
	if got := AverageAzimuth(0, 180); !NearlyEqual(got, 90, 1e-9) {
		t.Fatalf("AverageAzimuth(0,180) = %v, want 90", got)
	}
	if got := OppositeAzimuth(300); !NearlyEqual(got, 120, 1e-9) {
		t.Fatalf("OppositeAzimuth(300) = %v, want 120", got)
	}
}

func TestValidateAndClampInclination(t *testing.T) {
	if err := ValidateInclination(90); err != nil {
		t.Fatalf("ValidateInclination(90) returned %v", err)
	}
	if err := ValidateInclination(-90); err != nil {
		t.Fatalf("ValidateInclination(-90) returned %v", err)
	}
	if err := ValidateInclination(90.5); err == nil {
		t.Fatal("ValidateInclination accepted an impossible inclination")
	}
	if err := ValidateInclination(math.Inf(1)); err == nil {
		t.Fatal("ValidateInclination accepted an infinite inclination")
	}
	if got := ClampInclination(91); got != 90 {
		t.Fatalf("ClampInclination(91) = %v, want 90", got)
	}
	if got := ClampInclination(-91); got != -90 {
		t.Fatalf("ClampInclination(-91) = %v, want -90", got)
	}
}

func TestCompassPoint(t *testing.T) {
	cases := map[float64]string{0: "N", 45: "NE", 90: "E", 180: "S", 270: "W", 350: "N", 339: "NNW"}
	for input, want := range cases {
		if got := CompassPoint(input); got != want {
			t.Fatalf("CompassPoint(%v) = %q, want %q", input, got, want)
		}
	}
}

func TestRoundAndFormat(t *testing.T) {
	if got := RoundTo(1.2345, 2); !NearlyEqual(got, 1.23, 1e-12) {
		t.Fatalf("RoundTo(1.2345,2) = %v", got)
	}
	if got := RoundTo(-1.25, 1); !NearlyEqual(got, -1.3, 1e-12) {
		t.Fatalf("RoundTo(-1.25,1) = %v, want -1.3 with half away from zero", got)
	}
	if got := RoundTo(1.25, 1); !NearlyEqual(got, 1.3, 1e-12) {
		t.Fatalf("RoundTo(1.25,1) = %v, want 1.3 with half away from zero", got)
	}
	if got := Format(-0.0004, 3); got != "0.000" {
		t.Fatalf("Format(-0.0004,3) = %q, want %q", got, "0.000")
	}
	if got := Format(12.5, 1); got != "12.5" {
		t.Fatalf("Format(12.5,1) = %q", got)
	}
}

func TestPartsPerMillion(t *testing.T) {
	if got := PartsPerMillion(0.5, 100); !NearlyEqual(got, 5000, 1e-9) {
		t.Fatalf("PartsPerMillion(0.5,100) = %v, want 5000", got)
	}
	if got := PartsPerMillion(0.5, 0); got != 0 {
		t.Fatalf("PartsPerMillion with zero length = %v, want 0", got)
	}
}

func TestDegRadRoundTrip(t *testing.T) {
	for _, degrees := range []float64{0, 30, 45, 123.456, -75} {
		if got := RadToDeg(DegToRad(degrees)); !NearlyEqual(got, degrees, 1e-9) {
			t.Fatalf("degree round trip of %v produced %v", degrees, got)
		}
	}
}
