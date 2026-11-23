package canvas

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/tdewolff/test"
)

// highPrecisionLength integrates the segment speed on [0,1] with composite 15-point
// Gauss-Legendre quadrature over 200 subintervals, used as the reference arc length.
func highPrecisionLength(speed func(float64) float64) float64 {
	const N = 200
	d := 0.0
	for k := 0; k < N; k++ {
		a := float64(k) / N
		b := float64(k+1) / N
		q, _ := gaussLegendre15(speed, a, b)
		d += q
	}
	return d
}

// referenceLength is the high-accuracy composite-GL15 length over all drawable segments.
// It parses the path's segments independently (no loop-variable capturing in closures).
func referenceLength(p *Path) float64 {
	d := 0.0
	var start, end Point
	for i := 0; i < len(p.d); {
		cmd := p.d[i]
		switch cmd {
		case MoveToCmd:
			end = Point{p.d[i+1], p.d[i+2]}
		case LineToCmd, CloseCmd:
			end = Point{p.d[i+1], p.d[i+2]}
			d += end.Sub(start).Length()
		case QuadToCmd:
			cp := Point{p.d[i+1], p.d[i+2]}
			end = Point{p.d[i+3], p.d[i+4]}
			p0, p1, p2 := start, cp, end
			d += highPrecisionLength(func(t float64) float64 {
				return quadraticBezierDeriv(p0, p1, p2, t).Length()
			})
		case CubeToCmd:
			cp1 := Point{p.d[i+1], p.d[i+2]}
			cp2 := Point{p.d[i+3], p.d[i+4]}
			end = Point{p.d[i+5], p.d[i+6]}
			p0, p1, p2, p3 := start, cp1, cp2, end
			d += highPrecisionLength(func(t float64) float64 {
				return cubicBezierDeriv(p0, p1, p2, p3, t).Length()
			})
		case ArcToCmd:
			rx, ry, phi := p.d[i+1], p.d[i+2], p.d[i+3]
			large, sweep := toArcFlags(p.d[i+4])
			end = Point{p.d[i+5], p.d[i+6]}
			pStart := start
			_, _, theta0, theta1 := ellipseToCenter(pStart.X, pStart.Y, rx, ry, phi, large, sweep, end.X, end.Y)
			dt := math.Abs(theta1 - theta0)
			d += highPrecisionLength(func(t float64) float64 {
				theta := theta0 + t*(theta1-theta0)
				return ellipseDeriv(rx, ry, phi, sweep, theta).Length() * dt
			})
		}
		i += cmdLen(cmd)
		start = end
	}
	return d
}

func TestPathSamplerErrors(t *testing.T) {
	// empty and zero-length-only paths
	for _, p := range []*Path{
		nil,
		{},
		MustParseSVGPath("M10 10"),       // MoveTo only
		MustParseSVGPath("M10 10z"),      // MoveTo + point Close
		MustParseSVGPath("M0 0M5 5M3 3"), // multiple MoveTos
	} {
		_, err := NewPathSampler(p)
		test.That(t, errors.Is(err, ErrEmptyPath), "expected ErrEmptyPath for", p)
	}

	// NaN / Inf inside path data
	p := NewPathFromData([]float64{MoveToCmd, 0.0, 0.0, MoveToCmd, LineToCmd, math.NaN(), 1.0, LineToCmd})
	_, err := NewPathSampler(p)
	test.That(t, errors.Is(err, ErrInvalidDistance))

	// invalid query distances
	s, err := NewPathSampler(MustParseSVGPath("L10 0"))
	test.That(t, err == nil)
	for _, d := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, _, _, _, err := s.At(d)
		test.That(t, errors.Is(err, ErrInvalidDistance), "d=", d)
		_, err = s.PositionAt(d)
		test.That(t, errors.Is(err, ErrInvalidDistance))
		_, _, _, _, err = s.AtT(math.NaN())
		test.That(t, errors.Is(err, ErrInvalidDistance))
	}
	for _, d := range []float64{-0.01, -1.0, 10.01, math.MaxFloat64} {
		_, _, _, _, err := s.At(d)
		test.That(t, errors.Is(err, ErrDistanceOutOfRange), "d=", d)
		_, err = s.SegmentIndexAt(d)
		test.That(t, errors.Is(err, ErrDistanceOutOfRange))
	}

	// clamping never errors on finite out-of-range distances
	pos, _, _, idx, err := s.AtClamped(-5.0)
	test.That(t, err == nil)
	test.T(t, pos, Point{0.0, 0.0})
	test.T(t, idx, 1) // first drawable segment, MoveTo is index 0
	pos, _, _, _, err = s.AtClamped(100.0)
	test.That(t, err == nil)
	test.T(t, pos, Point{10.0, 0.0})
	_, _, _, _, err = s.AtClamped(math.NaN())
	test.That(t, errors.Is(err, ErrInvalidDistance))
}

func TestPathSamplerCancellation(t *testing.T) {
	// a path heavy enough that construction cannot finish before cancellation
	p := &Path{}
	p.MoveTo(0, 0)
	for i := 0; i < 100; i++ {
		p.CubeTo(float64(i)+0.3, 10.0, float64(i)+0.7, -10.0, float64(i)+1.0, 0.0)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewPathSamplerWithContext(ctx, p)
	test.That(t, errors.Is(err, context.Canceled), "err=", err)
}

func TestPathSamplerLine(t *testing.T) {
	p := MustParseSVGPath("M2 3L12 3")
	s, err := NewPathSampler(p)
	test.That(t, err == nil)
	test.FloatDiff(t, s.Length(), 10.0, 1e-12)
	test.T(t, s.SegmentLengths(), []float64{10.0})

	for i := 0; i <= 20; i++ {
		d := float64(i) / 2.0
		pos, tan, normal, idx, err := s.At(d)
		test.That(t, err == nil)
		test.FloatDiff(t, pos.X, 2.0+d, 1e-12)
		test.FloatDiff(t, pos.Y, 3.0, 1e-12)
		test.FloatDiff(t, tan.X, 1.0, 1e-12)
		test.FloatDiff(t, tan.Y, 0.0, 1e-12)
		// normal is the tangent rotated 90 degrees CW
		test.FloatDiff(t, normal.X, tan.Rot90CW().X, 1e-12)
		test.FloatDiff(t, normal.Y, tan.Rot90CW().Y, 1e-12)
		test.FloatDiff(t, normal.Length(), 1.0, 1e-12)
		test.That(t, idx == 1)
	}

	// endpoints
	pos, _, _, idx, _ := s.At(0.0)
	test.T(t, pos, Point{2.0, 3.0})
	test.That(t, idx == 1)
	pos, _, _, idx, _ = s.At(10.0)
	test.T(t, pos, Point{12.0, 3.0})
	test.That(t, idx == 1) // single segment owns both ends

	// normalized t equals d / length
	posT, _, _, _, _ := s.AtT(0.25)
	test.FloatDiff(t, posT.X, 4.5, 1e-12)
}

func TestPathSamplerArcLengthAccuracy(t *testing.T) {
	// one path exercising every segment type; compare the sampler's length against
	// high-precision composite GL15 integration of the same speed functions.
	p := MustParseSVGPath("M0 0L10 0Q20 10 30 0C35 -10 45 -10 50 0A5 10 30 0 0 60 0z")
	s, err := NewPathSampler(p)
	test.That(t, err == nil)

	want := referenceLength(p)
	relErr := math.Abs(s.Length()-want) / want
	test.That(t, relErr < 1e-10, "sampler length", s.Length(), "reference", want, "relErr", relErr)

	// segment lengths sum to the total and each matches its reference
	var sum float64
	for k, seg := range s.segments {
		sum += seg.length
		wantSeg := highPrecisionLength(seg.speed)
		rel := math.Abs(seg.length-wantSeg) / wantSeg
		test.That(t, rel < 1e-10, "segment", k, seg.length, wantSeg, rel)
	}
	test.FloatDiff(t, sum, s.Length(), 1e-12)
	test.FloatDiff(t, s.Length(), p.Length(), 0.01*s.Length()) // within Path.Length's own 1% accuracy
}

func TestPathSamplerArcLengthReparametrization(t *testing.T) {
	// walking arc length must map back to the true arc length: integrate the speed of
	// the reparametrized curve over t in [0,1] and compare with Length(), and verify
	// monotonicity of the returned positions.
	paths := []string{
		"M0 0Q100 0 10 10",                  // strongly non-uniform quadratic speed
		"M0 0C10 0 10 2 8 2",                // cubic
		"M0 0A10000 10 0 0 100 10",          // very eccentric ellipse
		"M0 0A5 5 0 1 0 10 0A5 5 0 1 0 0 0", // full circle as two arcs
		"M0 0L10 0Q20 10 30 0C35 -10 45 -10 50 0z",
	}
	for _, svg := range paths {
		t.Run(svg, func(t *testing.T) {
			p := MustParseSVGPath(svg)
			s, err := NewPathSampler(p)
			test.That(t, err == nil)

			const N = 500
			prev := Point{}
			prevD := -1.0
			chord := 0.0
			for i := 0; i <= N; i++ {
				d := s.Length() * float64(i) / N
				pos, tan, _, _, err := s.At(d)
				test.That(t, err == nil)
				test.FloatDiff(t, tan.Length(), 1.0, 1e-10)
				if 0 < i {
					chord += pos.Sub(prev).Length()
				}
				prev = pos

				// distance parameter is strictly increasing
				if 0 < i {
					test.That(t, d > prevD)
				}
				prevD = d
			}
			// chord length of a monotone arc-length walk cannot exceed the arc length
			test.That(t, chord <= s.Length()*(1.0+1e-9), chord, s.Length())
			// dense sampling of a smooth curve approaches the arc length
			test.That(t, math.Abs(chord-s.Length())/s.Length() < 1e-3, chord, s.Length())
		})
	}
}

func TestPathSamplerMonotonicityAndBounds(t *testing.T) {
	p := MustParseSVGPath("M0 0L5 0Q10 10 15 0C18 -10 22 -10 25 0A5 5 0 0 0 35 0")
	s, err := NewPathSampler(p)
	test.That(t, err == nil)

	const N = 2000
	var prevPos Point
	for i := 0; i <= N; i++ {
		d := s.Length() * float64(i) / N
		pos, tan, normal, _, err := s.At(d)
		test.That(t, err == nil)
		if math.Abs(tan.X)+math.Abs(tan.Y) > 0.0 {
			test.FloatDiff(t, tan.Length(), 1.0, 1e-9)
			test.FloatDiff(t, normal.Length(), 1.0, 1e-9)
			// normal is the tangent rotated 90 degrees CW: (ty, -tx)
			test.FloatDiff(t, normal.X, tan.Y, 1e-9)
			test.FloatDiff(t, normal.Y, -tan.X, 1e-9)
		}
		if 0 < i {
			// sampled chord steps are tiny (at most twice the average panel step)
			step := pos.Sub(prevPos)
			test.That(t, step.Length() <= 2.0*s.Length()/N+1e-9, "step too large:", step.Length())
		}
		prevPos = pos
	}
}

func TestPathSamplerCircleFixture(t *testing.T) {
	// analytical circle: r=5, full circle as two semicircle arcs, starting at (5,0)
	r := 5.0
	p := &Path{}
	p.MoveTo(r, 0.0)
	p.ArcTo(r, r, 0.0, true, true, -r, 0.0)
	p.ArcTo(r, r, 0.0, true, true, r, 0.0)
	s, err := NewPathSampler(p)
	test.That(t, err == nil)

	wantLength := 2.0 * math.Pi * r
	test.FloatDiff(t, s.Length(), wantLength, 1e-9)

	const N = 72
	for i := 0; i <= N; i++ {
		d := s.Length() * float64(i) / N
		pos, tan, normal, _, err := s.At(d)
		test.That(t, err == nil)

		// position lies on the circle
		test.FloatDiff(t, math.Hypot(pos.X, pos.Y), r, 1e-8)

		// analytical tangent for CCW circle: (-sin, cos)
		angle := d / r
		wantTan := Point{-math.Sin(angle), math.Cos(angle)}
		test.FloatDiff(t, tan.X, wantTan.X, 1e-8)
		test.FloatDiff(t, tan.Y, wantTan.Y, 1e-8)

		// normal points right of travel (Rot90CW); for a CCW circle that is the
		// outward radial direction
		radial := pos.Norm(1.0)
		test.FloatDiff(t, normal.X, radial.X, 1e-8)
		test.FloatDiff(t, normal.Y, radial.Y, 1e-8)
	}

	// closed wrap: position at full length equals the start position
	end, _, _, _, _ := s.At(s.Length())
	start, _, _, _, _ := s.At(0.0)
	test.FloatDiff(t, end.X, start.X, 1e-8)
	test.FloatDiff(t, end.Y, start.Y, 1e-8)
}

func TestPathSamplerEllipseFixture(t *testing.T) {
	// analytical ellipse rx=10, ry=4 over theta in [0, 3pi/2]
	rx, ry := 10.0, 4.0
	p := &Path{}
	p.MoveTo(rx, 0.0)
	p.Arc(rx, ry, 0.0, 0.0, 270.0) // CCW quarter-turns-and-a-half ending at (0,-4)
	s, err := NewPathSampler(p)
	test.That(t, err == nil)

	thetaMax := 1.5 * math.Pi
	speed := func(theta float64) float64 {
		return math.Hypot(-rx*math.Sin(theta), ry*math.Cos(theta))
	}
	wantLength := highPrecisionLength(func(t float64) float64 {
		return speed(t*thetaMax) * thetaMax
	})
	test.FloatDiff(t, s.Length(), wantLength, 1e-9)

	// invert the analytic arc length on a fine grid: thetaAt[d] gives the theta whose
	// accumulated arc length equals the query distance. Use 15-point quadrature per cell.
	const grid = 20000
	cum := make([]float64, grid+1)
	for k := 1; k <= grid; k++ {
		a := thetaMax * float64(k-1) / grid
		b := thetaMax * float64(k) / grid
		q, _ := gaussLegendre15(speed, a, b)
		cum[k] = cum[k-1] + q
	}
	thetaAt := func(d float64) float64 {
		lo, hi := 0, grid
		for lo+1 < hi {
			mid := (lo + hi) / 2
			if cum[mid] <= d {
				lo = mid
			} else {
				hi = mid
			}
		}
		frac := 0.0
		if cum[hi] > cum[lo] {
			frac = (d - cum[lo]) / (cum[hi] - cum[lo])
		}
		return thetaMax * (float64(lo) + frac) / grid
	}

	const N = 100
	for i := 0; i <= N; i++ {
		d := s.Length() * float64(i) / N
		pos, tan, _, _, err := s.At(d)
		test.That(t, err == nil)

		theta := thetaAt(d)
		wantPos := EllipsePos(rx, ry, 0.0, 0.0, 0.0, theta)
		test.FloatDiff(t, pos.X, wantPos.X, 1e-7)
		test.FloatDiff(t, pos.Y, wantPos.Y, 1e-7)

		wantTan := Point{-rx * math.Sin(theta), ry * math.Cos(theta)}.Norm(1.0)
		// tangent tolerance reflects the grid-based theta inversion reference error
		test.FloatDiff(t, tan.X, wantTan.X, 1e-5)
		test.FloatDiff(t, tan.Y, wantTan.Y, 1e-5)
	}
}

func TestPathSamplerBezierFixtures(t *testing.T) {
	// quadratic: position and tangent against the analytic polynomial
	quad := MustParseSVGPath("M0 0Q10 10 20 0")
	qs, err := NewPathSampler(quad)
	test.That(t, err == nil)
	p0, p1, p2 := Point{0, 0}, Point{10, 10}, Point{20, 0}
	for i := 0; i <= 50; i++ {
		u := float64(i) / 50.0
		// find d whose local parameter equals u using the segment LUT directly
		seg := qs.segments[0]
		d := 0.0
		for k := 0; k < samplerPanels; k++ {
			a, b := float64(k)/samplerPanels, float64(k+1)/samplerPanels
			if a <= u && u <= b {
				d += adaptiveIntegrate(seg.speed, a, u)
				break
			}
			d += seg.lut[k+1] - seg.lut[k]
		}
		pos, tan, _, _, err := qs.At(d)
		test.That(t, err == nil)
		wantPos := quadraticBezierPos(p0, p1, p2, u)
		test.FloatDiff(t, pos.X, wantPos.X, 1e-7)
		test.FloatDiff(t, pos.Y, wantPos.Y, 1e-7)
		wantTan := quadraticBezierDeriv(p0, p1, p2, u).Norm(1.0)
		test.FloatDiff(t, tan.X, wantTan.X, 1e-7)
		test.FloatDiff(t, tan.Y, wantTan.Y, 1e-7)
	}

	// cubic: sampled points must lie on the curve polynomial
	cube := MustParseSVGPath("M0 0C5 20 25 20 30 0")
	cs, err := NewPathSampler(cube)
	test.That(t, err == nil)
	c0, c1, c2, c3 := Point{0, 0}, Point{5, 20}, Point{25, 20}, Point{30, 0}
	for i := 0; i <= 100; i++ {
		d := cs.Length() * float64(i) / 100.0
		pos, tan, _, _, err := cs.At(d)
		test.That(t, err == nil)
		// recover the local parameter from the position via the segment's inverse map
		seg := cs.segments[0]
		u := seg.arcToU(d)
		wantPos := cubicBezierPos(c0, c1, c2, c3, u)
		test.FloatDiff(t, pos.X, wantPos.X, 1e-7)
		test.FloatDiff(t, pos.Y, wantPos.Y, 1e-7)
		wantTan := cubicBezierDeriv(c0, c1, c2, c3, u).Norm(1.0)
		test.FloatDiff(t, tan.X, wantTan.X, 1e-7)
		test.FloatDiff(t, tan.Y, wantTan.Y, 1e-7)
	}
}

func TestPathSamplerClosed(t *testing.T) {
	// closed triangle: Close is a drawable segment; last point equals first point
	p := MustParseSVGPath("M0 0L10 0L10 10z")
	s, err := NewPathSampler(p)
	test.That(t, err == nil)
	test.That(t, len(s.segments) == 3) // two lines plus the close segment
	// close runs from (10,10) back to (0,0)
	test.FloatDiff(t, s.SegmentLengths()[2], 10.0*math.Sqrt2, 1e-12)
	test.FloatDiff(t, s.Length(), 20.0+10.0*math.Sqrt2, 1e-12)

	first, _, _, _, _ := s.At(0.0)
	last, _, _, idx, _ := s.At(s.Length())
	test.T(t, first, Point{0.0, 0.0})
	test.FloatDiff(t, last.X, 0.0, 1e-10)
	test.FloatDiff(t, last.Y, 0.0, 1e-10)
	test.That(t, idx == 3) // close segment is command index 3

	// point-closed path (Close with zero length) keeps the other segments only
	pc := MustParseSVGPath("M0 0L10 0L10 10L0 0z")
	sc, err := NewPathSampler(pc)
	test.That(t, err == nil)
	test.That(t, len(sc.segments) == 3)
}

func TestPathSamplerSubpaths(t *testing.T) {
	p := MustParseSVGPath("M0 0L10 0M50 50Q60 60 70 50")
	s, err := NewPathSampler(p)
	test.That(t, err == nil)
	test.That(t, len(s.segments) == 2)
	test.FloatDiff(t, s.segments[0].length, 10.0, 1e-12)
	test.That(t, s.segments[0].segIndex == 1) // MoveTo=0, LineTo=1
	test.That(t, s.segments[1].segIndex == 3) // MoveTo=2, QuadTo=3

	// the subpath boundary: segment ending there is reported
	idx, err := s.SegmentIndexAt(10.0)
	test.That(t, err == nil)
	test.That(t, idx == 1)
	idx, err = s.SegmentIndexAt(10.0 + 1e-12)
	test.That(t, err == nil)
	test.That(t, idx == 3)

	// positions at the boundary and start of the second subpath
	pos, _ := s.PositionAt(10.0)
	test.T(t, pos, Point{10.0, 0.0})
	pos, _ = s.PositionAt(s.Length())
	test.T(t, pos, Point{70.0, 50.0})
	pos, _ = s.PositionAt(0.0)
	test.T(t, pos, Point{0.0, 0.0})
}

func TestPathSamplerNonZeroStart(t *testing.T) {
	// path not starting at the origin; positions must be absolute coordinates
	p := MustParseSVGPath("M100 200L110 200Q120 210 130 200")
	s, err := NewPathSampler(p)
	test.That(t, err == nil)
	pos, _ := s.PositionAt(0.0)
	test.T(t, pos, Point{100.0, 200.0})
	pos, _ = s.PositionAt(s.Length())
	test.T(t, pos, Point{130.0, 200.0})
}

func TestPathSamplerReverse(t *testing.T) {
	paths := []string{
		"M0 0L10 0Q20 10 30 0C35 -10 45 -10 50 0",
		"M0 0A5 10 30 0 0 20 5",
		"M0 0L10 0L0 10z",
		"M100 200L110 200Q120 210 130 200z",
	}
	for _, svg := range paths {
		t.Run(svg, func(t *testing.T) {
			p := MustParseSVGPath(svg)
			pr := p.Reverse()
			s, err := NewPathSampler(p)
			test.That(t, err == nil)
			sr, err := NewPathSampler(pr)
			test.That(t, err == nil)

			// lengths agree
			test.FloatDiff(t, s.Length(), sr.Length(), 1e-9)

			const N = 200
			for i := 0; i <= N; i++ {
				d := s.Length() * float64(i) / N
				pos, tan, _, _, err := s.At(d)
				test.That(t, err == nil)
				posR, tanR, _, _, err := sr.At(sr.Length() - d)
				test.That(t, err == nil)

				// same point at mirrored arc length
				test.FloatDiff(t, pos.X, posR.X, 1e-7)
				test.FloatDiff(t, pos.Y, posR.Y, 1e-7)
				// opposite tangents
				test.FloatDiff(t, tan.X, -tanR.X, 1e-7)
				test.FloatDiff(t, tan.Y, -tanR.Y, 1e-7)
			}
		})
	}
}

// linearDot applies the linear part of affine matrix m to a direction vector, ignoring
// its translation component.
func linearDot(m Matrix, v Point) Point {
	return Point{m[0][0]*v.X + m[0][1]*v.Y, m[1][0]*v.X + m[1][1]*v.Y}
}

func TestPathSamplerTransform(t *testing.T) {
	// Isometries and uniform scales preserve the arc-length fraction (uniform scaling
	// multiplies every segment's length by the same factor), so points sampled at the same
	// normalized t correspond exactly. This holds for every segment type.
	cases := []*Path{
		MustParseSVGPath("M1 2L11 -7"),
		MustParseSVGPath("M0 0Q10 10 20 0"),
		MustParseSVGPath("M0 0C5 20 25 20 30 0"),
		MustParseSVGPath("M0 0A5 10 30 0 0 20 5"),
	}
	isometries := []struct {
		name string
		m    Matrix
	}{
		{"translate", Identity.Translate(100.0, -50.0)},
		{"rotate", Identity.Rotate(37.0)},
		{"uniform-scale", Identity.Scale(2.5, 2.5)},
		{"rigid", Identity.Rotate(-23.0).Translate(7.0, 9.0)},
		{"mirror", Identity.Scale(1.0, -1.0)},
	}
	for ci, base := range cases {
		for _, tt := range isometries {
			t.Run(tt.name, func(t *testing.T) {
				p := base.Copy().Transform(tt.m)
				s0, err := NewPathSampler(base)
				test.That(t, err == nil)
				s1, err := NewPathSampler(p)
				test.That(t, err == nil)

				const N = 200
				for i := 0; i <= N; i++ {
					tpar := float64(i) / N
					pos0, tan0, _, _, err := s0.AtT(tpar)
					test.That(t, err == nil)
					pos1, tan1, nrm1, _, err := s1.AtT(tpar)
					test.That(t, err == nil)

					wantPos := tt.m.Dot(pos0)
					test.FloatDiff(t, pos1.X, wantPos.X, 1e-6, "case", ci, "t", tpar)
					test.FloatDiff(t, pos1.Y, wantPos.Y, 1e-6)

					wantTan := linearDot(tt.m, tan0).Norm(1.0)
					test.FloatDiff(t, tan1.X, wantTan.X, 1e-7)
					test.FloatDiff(t, tan1.Y, wantTan.Y, 1e-7)

					// normal is the tangent rotated 90 degrees CW
					wantNrm := wantTan.Rot90CW()
					test.FloatDiff(t, nrm1.X, wantNrm.X, 1e-7)
					test.FloatDiff(t, nrm1.Y, wantNrm.Y, 1e-7)
				}
			})
		}
	}

	// General affine transforms (non-uniform scale, shear) do not preserve arc-length
	// fractions, but they do preserve each segment's polynomial parameter u in [0,1].
	// Sampling both paths at the same u must still give transformed points and frames.
	generalAffines := []struct {
		name string
		m    Matrix
	}{
		{"nonuniform-scale", Identity.Scale(2.0, 0.5)},
		{"shear", Identity.Shear(0.3, -0.2)},
	}
	for ci, base := range cases[:3] { // line, quad, cube keep their polynomial form
		for _, tt := range generalAffines {
			t.Run(tt.name, func(t *testing.T) {
				p := base.Copy().Transform(tt.m)
				s0, err := NewPathSampler(base)
				test.That(t, err == nil)
				s1, err := NewPathSampler(p)
				test.That(t, err == nil)
				seg0, seg1 := s0.segments[0], s1.segments[0]

				const N = 200
				for i := 0; i <= N; i++ {
					u := float64(i) / N
					// clamp guards against sub-ULP integration overshoot at u=1
					d0 := math.Min(seg0.length, adaptiveIntegrate(seg0.speed, 0.0, u))
					d1 := math.Min(seg1.length, adaptiveIntegrate(seg1.speed, 0.0, u))
					pos0, tan0, _, _, err := s0.At(d0)
					test.That(t, err == nil)
					pos1, tan1, nrm1, _, err := s1.At(d1)
					test.That(t, err == nil)

					wantPos := tt.m.Dot(pos0)
					test.FloatDiff(t, pos1.X, wantPos.X, 1e-6, "case", ci, "u", u)
					test.FloatDiff(t, pos1.Y, wantPos.Y, 1e-6)

					wantTan := linearDot(tt.m, tan0).Norm(1.0)
					test.FloatDiff(t, tan1.X, wantTan.X, 1e-6)
					test.FloatDiff(t, tan1.Y, wantTan.Y, 1e-6)
					wantNrm := wantTan.Rot90CW()
					test.FloatDiff(t, nrm1.X, wantNrm.X, 1e-6)
					test.FloatDiff(t, nrm1.Y, wantNrm.Y, 1e-6)
				}
			})
		}
	}

	// length scaling: uniform scale multiplies, mirror and rigid transforms preserve
	base := MustParseSVGPath("M0 0L10 0Q20 10 30 0C35 -10 45 -10 50 0")
	s0, _ := NewPathSampler(base)
	su, _ := NewPathSampler(base.Copy().Transform(Identity.Scale(2.5, 2.5)))
	test.FloatDiff(t, su.Length(), 2.5*s0.Length(), 1e-9)
	sm, _ := NewPathSampler(base.Copy().Transform(Identity.Scale(1.0, -1.0)))
	test.FloatDiff(t, sm.Length(), s0.Length(), 1e-9)
	sr, _ := NewPathSampler(base.Copy().Transform(Identity.Rotate(42.0)))
	test.FloatDiff(t, sr.Length(), s0.Length(), 1e-9)
}

func TestPathSamplerCuspAndHighCurvature(t *testing.T) {
	// cubic with a cusp at t=0.5: p0=(0,0), control points pulling both sides onto the
	// same point, derivative vanishes at the cusp. Queries around it must stay finite and
	// the iteration must terminate (no infinite subdivision).
	p := MustParseSVGPath("M0 0C10 10 10 -10 0 0")
	s, err := NewPathSampler(p)
	test.That(t, err == nil)
	test.That(t, 0.0 < s.Length())
	for i := 0; i <= 400; i++ {
		d := s.Length() * float64(i) / 400.0
		pos, tan, normal, _, err := s.At(d)
		test.That(t, err == nil)
		test.That(t, !math.IsNaN(pos.X) && !math.IsInf(pos.X, 0.0))
		if tan.X != 0.0 || tan.Y != 0.0 {
			test.FloatDiff(t, tan.Length(), 1.0, 1e-9)
			test.FloatDiff(t, normal.Length(), 1.0, 1e-9)
		}
	}

	// near-zero radius-like arc: very small circle must still integrate and query
	circle := &Path{}
	circle.MoveTo(1e-8, 0.0)
	circle.ArcTo(1e-8, 1e-8, 0.0, true, true, -1e-8, 0.0)
	circle.ArcTo(1e-8, 1e-8, 0.0, true, true, 1e-8, 0.0)
	ss, err := NewPathSampler(circle)
	test.That(t, err == nil)
	test.FloatDiff(t, ss.Length(), 2.0*math.Pi*1e-8, 1e-20)
	pos, _, _, _, err := ss.At(ss.Length() / 2.0)
	test.That(t, err == nil)
	test.That(t, math.Abs(math.Hypot(pos.X, pos.Y)-1e-8) < 1e-15)
}

func TestPathSamplerSnapshot(t *testing.T) {
	// the sampler is a snapshot: mutating the path afterwards must not change queries
	p := MustParseSVGPath("L10 0")
	s, err := NewPathSampler(p)
	test.That(t, err == nil)
	before, _ := s.PositionAt(5.0)
	p.Scale(3.0, 3.0)
	after, _ := s.PositionAt(5.0)
	test.T(t, before, after)
	test.FloatDiff(t, s.Length(), 10.0, 1e-12)
}

// randomPathLocal builds a random path from a dedicated RNG so the test is deterministic
// and independent of the package-global random stream used by other tests.
func randomPathLocal(rng *rand.Rand, n int, closed bool) *Path {
	p := &Path{}
	if 0 < n {
		p.d = append(p.d, MoveToCmd, rng.NormFloat64(), rng.NormFloat64(), MoveToCmd)
		for i := 1; i < n; i++ {
			switch rng.IntN(4) {
			case 0:
				p.d = append(p.d, LineToCmd, rng.NormFloat64(), rng.NormFloat64(), LineToCmd)
			case 1:
				p.d = append(p.d, QuadToCmd, rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64(), QuadToCmd)
			case 2:
				p.d = append(p.d, CubeToCmd, rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64(), CubeToCmd)
			case 3:
				large, sweep := rng.IntN(2) == 0, rng.IntN(2) == 0
				p.d = append(p.d, ArcToCmd, rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64(), fromArcFlags(large, sweep), rng.NormFloat64(), rng.NormFloat64(), ArcToCmd)
			}
		}
		if closed {
			p.d = append(p.d, CloseCmd, p.d[1], p.d[2], CloseCmd)
		}
	}
	return p
}

func TestPathSamplerRandomPaths(t *testing.T) {
	// deterministic randomized property checks: length accuracy, unit frames, steps, NaN
	rng := rand.New(rand.NewPCG(1, 2))
	for n := 1; n <= 50; n++ {
		p := randomPathLocal(rng, 6, n%3 == 0)
		if p.Empty() {
			_, err := NewPathSampler(p)
			test.That(t, errors.Is(err, ErrEmptyPath))
			continue
		}
		s, err := NewPathSampler(p)
		if err != nil {
			continue // degenerate random data may be rejected
		}

		want := referenceLength(p)
		if 0.0 < want {
			test.That(t, math.Abs(s.Length()-want)/want < 1e-9, s.Length(), want)
		}

		const N = 200
		var prev Point
		prevSeg := -1
		for i := 0; i <= N; i++ {
			d := s.Length() * float64(i) / N
			pos, tan, normal, segIdx, err := s.At(d)
			test.That(t, err == nil)
			test.That(t, !math.IsNaN(pos.X) && !math.IsNaN(pos.Y))
			if tan.X != 0.0 || tan.Y != 0.0 {
				test.FloatDiff(t, tan.Length(), 1.0, 1e-8)
				test.FloatDiff(t, normal.Length(), 1.0, 1e-8)
			}
			// chord bound applies within a segment; reset at subpath/segment jumps
			if 0 < i && segIdx == prevSeg {
				test.That(t, pos.Sub(prev).Length() <= 2.0*s.Length()/N+1e-9)
			}
			prev = pos
			prevSeg = segIdx
		}
	}
}
