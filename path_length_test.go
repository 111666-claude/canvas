package canvas

import (
	"math"
	"testing"

	"github.com/tdewolff/test"
)

func TestPathLengthScale(t *testing.T) {
	// length scale maps the author-declared path length to the true geometry
	// length; every valid case returns a positive finite factor
	var tts = []struct {
		name       string
		pathLength float64
		length     float64
		expected   float64
	}{
		{"equal", 10.0, 10.0, 1.0},
		{"declared half", 5.0, 10.0, 2.0},
		{"declared double", 20.0, 10.0, 0.5},
		{"zero declared", 0.0, 10.0, 1.0},
		{"negative declared", -4.0, 10.0, 1.0},
		{"NaN declared", math.NaN(), 10.0, 1.0},
		{"+Inf declared", math.Inf(1), 10.0, 1.0},
		{"-Inf declared", math.Inf(-1), 10.0, 1.0},
		{"zero geometry", 10.0, 0.0, 1.0},
		{"NaN geometry", 10.0, math.NaN(), 1.0},
		{"huge ratio", 1e-300, 1e300, 1.0},
		{"tiny ratio", 1e300, 1e-300, 1.0},
	}
	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			scale := pathLengthScale(tt.pathLength, tt.length)
			test.That(t, math.IsNaN(scale) == false)
			test.That(t, math.IsInf(scale, 0) == false)
			test.That(t, 0.0 < scale)
			test.T(t, scale, tt.expected)
		})
	}
}

func TestPathNormalizeLength(t *testing.T) {
	// lines, quadratic/cubic Beziers and elliptic arcs all contribute to the
	// measured length, and the normalization factor stays finite for every
	// segment kind
	line := MustParseSVGPath("M0 0L10 0")
	test.T(t, line.Length(), 10.0)
	test.T(t, line.NormalizeLength(5.0), 2.0)
	test.T(t, line.NormalizeLength(0.0), 1.0)
	test.T(t, line.NormalizeLength(-1.0), 1.0)
	test.T(t, line.NormalizeLength(math.NaN()), 1.0)

	quad := MustParseSVGPath("M0 0Q5 10 10 0")
	test.That(t, quad.NormalizeLength(1.0) > 1.0) // true length is longer than 1

	cube := MustParseSVGPath("M0 0C0 10 10 10 10 0")
	test.That(t, cube.NormalizeLength(100.0) > 0.0)
	test.That(t, !math.IsNaN(cube.NormalizeLength(100.0)))

	arc := MustParseSVGPath("M0 5A5 5 0 0 1 10 5")
	length := arc.Length()
	test.FloatDiff(t, length, math.Pi*5.0, 1e-4) // arc length is a Gauss-Legendre approximation
	scale := arc.NormalizeLength(math.Pi * 2.5)
	test.FloatDiff(t, scale, 2.0, 1e-5)

	// a point-only path has zero length and falls back to a factor of one
	point := MustParseSVGPath("M3 3")
	test.T(t, point.Length(), 0.0)
	test.T(t, point.NormalizeLength(10.0), 1.0)
}

func TestPathNormalizeDashes(t *testing.T) {
	p := MustParseSVGPath("M0 0L100 0")

	// dash array and offset scale by length / pathLength
	offset, dashes := p.NormalizeDashes(50.0, 5.0, []float64{10.0, 5.0})
	test.T(t, offset, 10.0)
	test.T(t, dashes, []float64{20.0, 10.0})

	// negative offsets keep their sign through the scaling
	offset, dashes = p.NormalizeDashes(50.0, -5.0, []float64{10.0})
	test.T(t, offset, -10.0)
	test.T(t, dashes, []float64{20.0})

	// invalid pathLength leaves everything untouched
	offset, dashes = p.NormalizeDashes(0.0, 5.0, []float64{10.0})
	test.T(t, offset, 5.0)
	test.T(t, dashes, []float64{10.0})

	// a valid pathLength never produces NaN, even with extreme dash values
	offset, dashes = p.NormalizeDashes(50.0, math.Inf(1), []float64{math.MaxFloat64})
	test.That(t, !math.IsNaN(offset))
	for _, d := range dashes {
		test.That(t, !math.IsNaN(d))
	}
}

func TestPathPositionAt(t *testing.T) {
	// straight line
	line := MustParseSVGPath("M0 0L10 0")
	pos, dir := line.PositionAt(0.0)
	test.T(t, pos, Point{0.0, 0.0})
	test.T(t, dir, Point{1.0, 0.0})
	pos, dir = line.PositionAt(5.0)
	test.T(t, pos, Point{5.0, 0.0})
	test.T(t, dir, Point{1.0, 0.0})

	// clamping at and beyond the end
	pos, _ = line.PositionAt(100.0)
	test.T(t, pos, Point{10.0, 0.0})
	pos, _ = line.PositionAt(-5.0)
	test.T(t, pos, Point{0.0, 0.0})

	// cubic Bezier: endpoints and tangent directions
	cube := MustParseSVGPath("M0 0C0 10 10 10 10 0")
	pos, dir = cube.PositionAt(0.0)
	test.T(t, pos, Point{0.0, 0.0})
	test.T(t, dir, Point{0.0, 1.0})
	pos, dir = cube.PositionAt(cube.Length())
	test.T(t, pos, Point{10.0, 0.0})
	test.T(t, dir, Point{0.0, -1.0})

	// arc: midpoint of a semicircle in the y-down SVG frame sits at (5, 0),
	// moving in the +x direction at the top of the arc
	arc := MustParseSVGPath("M0 5A5 5 0 0 1 10 5")
	pos, dir = arc.PositionAt(arc.Length() / 2.0)
	test.FloatDiff(t, pos.X, 5.0, 1e-5)
	test.FloatDiff(t, pos.Y, 0.0, 1e-5)
	test.FloatDiff(t, dir.X, 1.0, 1e-5)
	test.FloatDiff(t, math.Abs(dir.Y), 0.0, 1e-5)

	// quadratic Bezier never produces NaN
	quad := MustParseSVGPath("M0 0Q5 10 10 0")
	for d := 0.0; d <= quad.Length(); d += quad.Length() / 7.0 {
		pos, dir = quad.PositionAt(d)
		test.That(t, !math.IsNaN(pos.X) && !math.IsNaN(pos.Y))
		test.FloatDiff(t, dir.Length(), 1.0, 1e-9)
	}
}

func TestPathPositionsAt(t *testing.T) {
	// closed ring: evenly spaced positions wrap the subpath and keep unit
	// tangent directions
	ring := MustParseSVGPath("M0 0L10 0L10 10L0 10z")
	length := ring.Length()
	distances := []float64{0.0, length / 4.0, length / 2.0, 3.0 * length / 4.0}
	positions, directions := ring.PositionsAt(distances...)
	test.T(t, len(positions), 4)
	expected := []Point{{0.0, 0.0}, {10.0, 0.0}, {10.0, 10.0}, {0.0, 10.0}}
	for i, want := range expected {
		test.That(t, positions[i].Equals(want))
		test.T(t, directions[i].Length(), 1.0)
	}
}
