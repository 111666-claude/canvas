package canvas

import (
	"fmt"
	"math"
	"testing"

	"github.com/tdewolff/test"
)

// offsetSignedArea returns the signed area of a path by flattening and applying the shoelace
// formula to each subpath. Filling (CCW) contours have a positive area.
func offsetSignedArea(p *Path) float64 {
	area := 0.0
	for _, pi := range p.Split() {
		coords := pi.Flatten(0.0001).Coords()
		if len(coords) == 0 {
			continue
		}
		prev := coords[len(coords)-1]
		for _, c := range coords {
			area += prev.X*c.Y - c.X*prev.Y
			prev = c
		}
	}
	return area / 2.0
}

// offsetFixtureRoundedRect returns a closed rounded rectangle at (x,y) of size w x h with corner
// radius r, consisting of straight lines and elliptical arcs.
func offsetFixtureRoundedRect(x, y, w, h, r float64) *Path {
	p := &Path{}
	p.MoveTo(x+r, y)
	p.LineTo(x+w-r, y)
	p.ArcTo(r, r, 0.0, false, true, x+w, y+r)
	p.LineTo(x+w, y+h-r)
	p.ArcTo(r, r, 0.0, false, true, x+w-r, y+h)
	p.LineTo(x+r, y+h)
	p.ArcTo(r, r, 0.0, false, true, x, y+h-r)
	p.LineTo(x, y+r)
	p.ArcTo(r, r, 0.0, false, true, x+r, y)
	p.Close()
	return p
}

// offsetFixtureStar returns a closed concave star polygon with n spikes around (cx,cy) with
// outer radius R and inner radius r.
func offsetFixtureStar(cx, cy, R, r float64, n int) *Path {
	p := &Path{}
	for i := 0; i < 2*n; i++ {
		radius := R
		if i%2 == 1 {
			radius = r
		}
		angle := math.Pi/2.0 + float64(i)*math.Pi/float64(n)
		x := cx + radius*math.Cos(angle)
		y := cy + radius*math.Sin(angle)
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	p.Close()
	return p
}

// offsetFixtureHole returns a donut: an outer rounded rectangle with a rounded rectangular hole.
func offsetFixtureHole() *Path {
	outer := offsetFixtureRoundedRect(0.0, 0.0, 20.0, 20.0, 3.0)
	hole := offsetFixtureRoundedRect(6.0, 6.0, 8.0, 8.0, 1.0).Reverse()
	return outer.Append(hole)
}

// offsetFixtureLongCurve returns a long open cubic Bézier bending 90 degrees to the right.
func offsetFixtureLongCurve() *Path {
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.CubeTo(0.0, 100.0, 100.0, 200.0, 200.0, 200.0)
	return p
}

func TestPathOffsetWithOptionsLine(t *testing.T) {
	// open straight centerline becomes a closed, fillable boundary
	line := MustParseSVGPath("M0 0L100 0")
	var tts = []struct {
		w      float64
		capper Capper
		bounds Rect
		area   float64
	}{
		{2.0, ButtCap, Rect{0.0, -2.0, 100.0, 0.0}, 200.0},
		{2.0, SquareCap, Rect{-2.0, -2.0, 102.0, 0.0}, 208.0},
		{2.0, RoundCap, Rect{-2.0, -2.0, 102.0, 0.0}, 200.0 + 2.0*math.Pi}, // rectangle + two quarter circles
		{-2.0, ButtCap, Rect{0.0, 0.0, 100.0, 2.0}, 200.0},
		{-2.0, SquareCap, Rect{-2.0, 0.0, 102.0, 2.0}, 208.0},
		{-2.0, RoundCap, Rect{-2.0, 0.0, 102.0, 2.0}, 200.0 + 2.0*math.Pi},
	}
	for _, tt := range tts {
		t.Run(fmt.Sprintf("%v/%v", tt.w, tt.capper), func(t *testing.T) {
			q := line.OffsetWithOptions(tt.w, OffsetOptions{Cap: tt.capper, Tolerance: 0.001})
			test.FloatDiff(t, q.Bounds().X0, tt.bounds.X0, 0.01)
			test.FloatDiff(t, q.Bounds().Y0, tt.bounds.Y0, 0.01)
			test.FloatDiff(t, q.Bounds().W(), tt.bounds.W(), 0.01)
			test.FloatDiff(t, q.Bounds().H(), tt.bounds.H(), 0.01)
			test.FloatDiff(t, offsetSignedArea(q), tt.area, 0.01)
			test.T(t, q.CCW(), true, "offset region should be CCW")
		})
	}
}

func TestPathOffsetWithOptionsClosed(t *testing.T) {
	// closed 10x10 square
	square := MustParseSVGPath("M0 0L10 0L10 10L0 10z")
	var tts = []struct {
		w      float64
		join   Joiner
		bounds Rect
		area   float64
	}{
		// Steiner formula for dilation by a disk: A + P*w + pi*w^2
		{1.0, RoundJoin, Rect{-1.0, -1.0, 11.0, 11.0}, 100.0 + 40.0 + math.Pi},
		// miter joins extend the square corners
		{1.0, MiterJoin, Rect{-1.0, -1.0, 11.0, 11.0}, 144.0},
		// bevel joins cut off the four corners
		{1.0, BevelJoin, Rect{-1.0, -1.0, 11.0, 11.0}, 144.0 - 4.0*0.5},
		// negative offsets contract with sharp inner corners, as with Path.Offset
		{-1.0, RoundJoin, Rect{1.0, 1.0, 9.0, 9.0}, 64.0},
		{-1.0, MiterJoin, Rect{1.0, 1.0, 9.0, 9.0}, 64.0},
	}
	for _, tt := range tts {
		t.Run(fmt.Sprintf("%v/%v", tt.w, tt.join), func(t *testing.T) {
			q := square.OffsetWithOptions(tt.w, OffsetOptions{Join: tt.join, Tolerance: 0.001})
			test.FloatDiff(t, q.Bounds().X0, tt.bounds.X0, 0.01)
			test.FloatDiff(t, q.Bounds().Y0, tt.bounds.Y0, 0.01)
			test.FloatDiff(t, q.Bounds().W(), tt.bounds.W(), 0.01)
			test.FloatDiff(t, q.Bounds().H(), tt.bounds.H(), 0.01)
			test.FloatDiff(t, offsetSignedArea(q), tt.area, 0.01)
		})
	}

	// a square contracted beyond its half width vanishes
	q := square.OffsetWithOptions(-6.0, OffsetOptions{Tolerance: 0.001})
	test.T(t, q.Empty(), true, "over-contracted square should vanish")
}

func TestPathOffsetWithOptionsRoundedRect(t *testing.T) {
	// rounded rectangle at a non-zero position: lines and arcs offset exactly
	r := 5.0
	p := offsetFixtureRoundedRect(10.0, 20.0, 40.0, 30.0, r)
	area := 40.0*30.0 - (4.0-math.Pi)*r*r
	perimeter := 2.0*(40.0+30.0) - 8.0*r + 2.0*math.Pi*r
	test.FloatDiff(t, offsetSignedArea(p), area, 0.01, "fixture area")

	w := 2.0
	q := p.OffsetWithOptions(w, OffsetOptions{Join: RoundJoin, Tolerance: 0.001})
	test.FloatDiff(t, offsetSignedArea(q), area+perimeter*w+math.Pi*w*w, 0.05, "Steiner dilation")
	bounds := q.Bounds()
	test.FloatDiff(t, bounds.X0, 8.0, 0.01)
	test.FloatDiff(t, bounds.Y0, 18.0, 0.01)
	test.FloatDiff(t, bounds.W(), 44.0, 0.01)
	test.FloatDiff(t, bounds.H(), 34.0, 0.01)
	test.T(t, q.CCW(), true)

	// negative offset contracts; arcs keep an exact (smaller) radius
	q = p.OffsetWithOptions(-w, OffsetOptions{Join: RoundJoin, Tolerance: 0.001})
	bounds = q.Bounds()
	test.FloatDiff(t, bounds.X0, 12.0, 0.01)
	test.FloatDiff(t, bounds.Y0, 22.0, 0.01)
	test.FloatDiff(t, bounds.W(), 36.0, 0.01)
	test.FloatDiff(t, bounds.H(), 26.0, 0.01)
	test.T(t, q.CCW(), true)
}

func TestPathOffsetWithOptionsStar(t *testing.T) {
	// concave star: Steiner formula still holds for small offsets with round joins
	R, r := 10.0, 4.0
	n := 5
	star := offsetFixtureStar(0.0, 0.0, R, r, n)
	area := float64(n) * R * r * math.Sin(math.Pi/float64(n))
	edge := math.Sqrt(R*R + r*r - 2.0*R*r*math.Cos(math.Pi/float64(n)))
	perimeter := float64(2*n) * edge
	test.FloatDiff(t, offsetSignedArea(star), area, 0.01, "fixture area")

	w := 0.5
	q := star.OffsetWithOptions(w, OffsetOptions{Join: RoundJoin, Tolerance: 0.001})
	test.FloatDiff(t, offsetSignedArea(q), area+perimeter*w+math.Pi*w*w, 0.1, "Steiner dilation")
	test.T(t, q.CCW(), true)
	test.FloatDiff(t, q.Bounds().Y1, R+w, 0.01, "star top expands by w")

	// contraction: sharp inner corners, consistent with the existing Path.Offset semantics
	q = star.OffsetWithOptions(-w, OffsetOptions{Join: RoundJoin, Tolerance: 0.001})
	ref := star.Offset(-w, 0.001)
	test.FloatDiff(t, offsetSignedArea(q), offsetSignedArea(ref), 0.01, "consistent with Path.Offset")
	test.T(t, q.CCW(), true)

	// self-intersecting pentagram must not panic and give a sane, filling result
	pentagram := &Path{}
	for i := 0; i <= 5; i++ {
		angle := math.Pi/2.0 + float64(2*i%10)*math.Pi/5.0
		x, y := R*math.Cos(angle), R*math.Sin(angle)
		if i == 0 {
			pentagram.MoveTo(x, y)
		} else {
			pentagram.LineTo(x, y)
		}
	}
	pentagram.Close()
	q = pentagram.OffsetWithOptions(0.5, OffsetOptions{Join: RoundJoin, Tolerance: 0.001})
	test.T(t, q.Sane(), true)
	test.T(t, q.Empty(), false)
	test.T(t, q.CCW(), true)
}

func TestPathOffsetWithOptionsHole(t *testing.T) {
	// donut: outer contour expands and the hole contracts by the same distance
	donut := offsetFixtureHole()
	areaOuter := 20.0*20.0 - (4.0-math.Pi)*9.0
	perimOuter := 2.0*(20.0+20.0) - 8.0*3.0 + 2.0*math.Pi*3.0
	areaHole := 8.0*8.0 - (4.0-math.Pi)*1.0
	perimHole := 2.0*(8.0+8.0) - 8.0*1.0 + 2.0*math.Pi*1.0
	area := areaOuter - areaHole
	test.FloatDiff(t, offsetSignedArea(donut.Settle(NonZero)), area, 0.01, "fixture area")

	w := 0.5
	q := donut.OffsetWithOptions(w, OffsetOptions{Join: RoundJoin, Tolerance: 0.001})
	// the pi*w^2 terms of the dilation and the hole's erosion cancel
	expect := area + (perimOuter+perimHole)*w
	test.FloatDiff(t, offsetSignedArea(q), expect, 0.05, "donut area")

	filling := q.Filling(NonZero)
	test.T(t, len(filling), 2, "donut should keep two subpaths")
	test.T(t, filling[0], true, "outer contour fills")
	test.T(t, filling[1], false, "inner contour is a hole")

	subs := q.Split()
	test.FloatDiff(t, subs[0].Bounds().X0, -w, 0.01, "outer expands")
	test.FloatDiff(t, subs[1].Bounds().X0, 6.0+w, 0.01, "hole contracts")
}

func TestPathOffsetWithOptionsLongCurve(t *testing.T) {
	// open arc: the offset region is an exact annulus sector with butt caps
	theta := 2.0 * math.Pi / 3.0
	arc := &Path{}
	arc.MoveTo(50.0, 0.0)
	arc.ArcTo(50.0, 50.0, 0.0, false, true, 50.0*math.Cos(theta), 50.0*math.Sin(theta))
	w := 2.0
	length := 50.0 * theta
	q := arc.OffsetWithOptions(w, OffsetOptions{Cap: ButtCap, Tolerance: 0.001})
	test.FloatDiff(t, offsetSignedArea(q), length*w+theta*w*w/2.0, 0.01, "annulus sector, outer side")
	q = arc.OffsetWithOptions(-w, OffsetOptions{Cap: ButtCap, Tolerance: 0.001})
	test.FloatDiff(t, offsetSignedArea(q), length*w-theta*w*w/2.0, 0.01, "annulus sector, inner side")

	// long cubic bending 90 degrees to the right: area = L*w + dtheta*w^2/2
	curve := offsetFixtureLongCurve()
	length = curve.Length()
	dtheta := -math.Pi / 2.0 // tangents rotate from +y to +x
	q = curve.OffsetWithOptions(w, OffsetOptions{Cap: ButtCap, Tolerance: 0.001})
	area := offsetSignedArea(q)
	test.FloatDiff(t, area, length*w+dtheta*w*w/2.0, 0.02*length*w, "curve offset area")
	test.T(t, q.CCW(), true)
	test.T(t, q.Closed(), true, "open curve offset should be a closed region")

	// quadratic Bézier bending 90 degrees to the right
	quad := MustParseSVGPath("M0 0Q100 100 200 0")
	length = quad.Length()
	q = quad.OffsetWithOptions(w, OffsetOptions{Cap: ButtCap, Tolerance: 0.001})
	test.FloatDiff(t, offsetSignedArea(q), length*w+dtheta*w*w/2.0, 0.02*length*w, "quadratic offset area")
	test.T(t, q.CCW(), true)
}

func TestPathOffsetWithOptionsMixed(t *testing.T) {
	// a path with both closed and open subpaths offsets each appropriately
	p := MustParseSVGPath("M0 0L10 0L10 10L0 10zM20 0L30 0")
	q := p.OffsetWithOptions(1.0, OffsetOptions{Cap: ButtCap, Join: MiterJoin, Tolerance: 0.001})
	test.T(t, q.Sane(), true)
	subs := q.Split()
	test.T(t, len(subs), 2, "closed and open subpaths should each offset")
	test.FloatDiff(t, subs[0].Bounds().X0, -1.0, 0.01, "closed subpath expands")
	test.FloatDiff(t, subs[1].Bounds().X0, 20.0, 0.01, "open subpath offsets")
	test.FloatDiff(t, subs[1].Bounds().Y0, -1.0, 0.01, "open subpath offsets to the right")
	test.FloatDiff(t, offsetSignedArea(q), 144.0+10.0, 0.01)
}

func TestPathOffsetWithOptionsJoinConsistency(t *testing.T) {
	// sharp 90 degree corner: join behaviour must be consistent with Path.Stroke
	v := MustParseSVGPath("M0 0L10 0L10 10")
	w := 1.0
	probeMiter := Point{10.6, -0.6} // inside a miter join, outside a bevel join
	probeSide := Point{5.0, -0.5}   // always inside
	probeFar := Point{5.0, 0.5}     // always outside (other side of the centerline)

	var tts = []struct {
		join  Joiner
		miter bool
	}{
		{MiterJoin, true},
		{MiterJoiner{BevelJoin, 1.0}, false}, // limit exceeded, falls back to bevel
		{BevelJoin, false},
		{RoundJoin, true},
	}
	for _, tt := range tts {
		t.Run(fmt.Sprintf("%v", tt.join), func(t *testing.T) {
			q := v.OffsetWithOptions(w, OffsetOptions{Join: tt.join, Tolerance: 0.001})
			stroke := v.Stroke(2.0*w, ButtCap, tt.join, 0.001)
			test.T(t, q.ContainsPoint(probeSide.X, probeSide.Y, NonZero), true)
			test.T(t, q.ContainsPoint(probeFar.X, probeFar.Y, NonZero), false)
			test.T(t, q.ContainsPoint(probeMiter.X, probeMiter.Y, NonZero), tt.miter)
			// the offset region agrees with the stroke on the offset side
			test.T(t, q.ContainsPoint(probeMiter.X, probeMiter.Y, NonZero),
				stroke.ContainsPoint(probeMiter.X, probeMiter.Y, NonZero), "consistent with Stroke")
			test.T(t, q.ContainsPoint(probeSide.X, probeSide.Y, NonZero),
				stroke.ContainsPoint(probeSide.X, probeSide.Y, NonZero), "consistent with Stroke")
		})
	}

	// the MiterLimit option is used when no joiner is given
	q := v.OffsetWithOptions(w, OffsetOptions{MiterLimit: 1.0, Tolerance: 0.001})
	test.T(t, q.ContainsPoint(probeMiter.X, probeMiter.Y, NonZero), false, "miter limit bevels")
	q = v.OffsetWithOptions(w, OffsetOptions{MiterLimit: 10.0, Tolerance: 0.001})
	test.T(t, q.ContainsPoint(probeMiter.X, probeMiter.Y, NonZero), true, "miter limit miters")
}

func TestPathOffsetWithOptionsReversed(t *testing.T) {
	// reversed contours give the same offset region
	p := offsetFixtureRoundedRect(10.0, 20.0, 40.0, 30.0, 5.0)
	for _, w := range []float64{2.0, -2.0} {
		opts := OffsetOptions{Join: RoundJoin, Tolerance: 0.001}
		a := p.OffsetWithOptions(w, opts)
		b := p.Reverse().OffsetWithOptions(w, opts)
		test.FloatDiff(t, offsetSignedArea(a), offsetSignedArea(b), 0.01, fmt.Sprintf("w=%v", w))
		test.FloatDiff(t, a.Bounds().X0, b.Bounds().X0, 0.01)
		test.FloatDiff(t, a.Bounds().Y0, b.Bounds().Y0, 0.01)
		test.T(t, b.CCW(), true)
	}
}

func TestPathOffsetWithOptionsTransform(t *testing.T) {
	// offsetting commutes with similarity transforms when scaling the distance
	p := offsetFixtureRoundedRect(100.0, 50.0, 40.0, 30.0, 5.0)
	m := Identity.Translate(3.0, -2.0).Rotate(25.0).Scale(1.5, 1.5)
	opts := OffsetOptions{Join: RoundJoin, Tolerance: 0.001}

	a := p.OffsetWithOptions(1.0, opts).Transform(m)
	b := p.Copy().Transform(m).OffsetWithOptions(1.5, opts)
	test.FloatDiff(t, offsetSignedArea(a), offsetSignedArea(b), 0.05*math.Abs(offsetSignedArea(a)))
	ba, bb := a.Bounds(), b.Bounds()
	test.FloatDiff(t, ba.X0, bb.X0, 0.05)
	test.FloatDiff(t, ba.Y0, bb.Y0, 0.05)
	test.FloatDiff(t, ba.W(), bb.W(), 0.05)
	test.FloatDiff(t, ba.H(), bb.H(), 0.05)

	// non-zero bounds are preserved by the offset
	q := p.OffsetWithOptions(2.0, opts)
	test.FloatDiff(t, q.Bounds().X0, 98.0, 0.01)
	test.FloatDiff(t, q.Bounds().Y0, 48.0, 0.01)
}

func TestPathOffsetWithOptionsRobust(t *testing.T) {
	rect := offsetFixtureRoundedRect(0.0, 0.0, 10.0, 10.0, 2.0)
	ref := rect.OffsetWithOptions(1.0, OffsetOptions{Tolerance: 0.01}).String()

	// zero, negative and NaN tolerances fall back to the default tolerance
	for _, tol := range []float64{0.0, -1.0, math.NaN()} {
		q := rect.OffsetWithOptions(1.0, OffsetOptions{Tolerance: tol})
		test.T(t, q.String(), ref, fmt.Sprintf("tolerance=%v", tol))
	}

	// non-finite distances return an empty path
	for _, w := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		q := rect.OffsetWithOptions(w, OffsetOptions{})
		test.T(t, q.Empty(), true, fmt.Sprintf("w=%v", w))
	}

	// non-finite coordinates return an empty path
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		p := &Path{d: []float64{MoveToCmd, bad, 0.0, MoveToCmd, LineToCmd, 10.0, 10.0, LineToCmd}}
		test.T(t, p.OffsetWithOptions(1.0, OffsetOptions{}).Empty(), true, "non-finite move")
		p = &Path{d: []float64{MoveToCmd, 0.0, 0.0, MoveToCmd, LineToCmd, bad, 10.0, LineToCmd}}
		test.T(t, p.OffsetWithOptions(1.0, OffsetOptions{}).Empty(), true, "non-finite line")
		p = &Path{d: []float64{MoveToCmd, 0.0, 0.0, MoveToCmd, ArcToCmd, bad, 5.0, 0.0, fromArcFlags(false, true), 10.0, 0.0, ArcToCmd}}
		test.T(t, p.OffsetWithOptions(1.0, OffsetOptions{}).Empty(), true, "non-finite arc radius")
	}

	// empty and degenerate paths return an empty path
	test.T(t, (&Path{}).OffsetWithOptions(1.0, OffsetOptions{}).Empty(), true)
	test.T(t, MustParseSVGPath("M5 5").OffsetWithOptions(1.0, OffsetOptions{}).Empty(), true)
	test.T(t, MustParseSVGPath("M0 0L0 0").OffsetWithOptions(1.0, OffsetOptions{}).Empty(), true)
	test.T(t, MustParseSVGPath("M0 0L5 0z").OffsetWithOptions(-1.0, OffsetOptions{}).Empty(), true)

	// extremely large arc radii give a bounded, sane approximation
	hugeArc := &Path{}
	hugeArc.MoveTo(0.0, 0.0)
	hugeArc.ArcTo(1e300, 1e300, 0.0, false, true, 100.0, 0.0)
	q := hugeArc.OffsetWithOptions(2.0, OffsetOptions{Tolerance: 0.01})
	test.T(t, q.Sane(), true, "huge arc radius")

	// extremely large coordinates and distances
	huge := MustParseSVGPath("M0 0L1e8 0L1e8 1e8L0 1e8z")
	q = huge.OffsetWithOptions(1e6, OffsetOptions{Tolerance: 1.0})
	test.T(t, q.Sane(), true, "huge coordinates")
	test.T(t, q.Empty(), false, "huge coordinates")

	// tiny tolerance on a huge curve gives a bounded approximation instead of hanging
	long := MustParseSVGPath("M0 0C3e5 0 3e5 3e5 0 3e5")
	q = long.OffsetWithOptions(1.0, OffsetOptions{Tolerance: 1e-12})
	test.T(t, q.Sane(), true, "tiny tolerance on huge curve")
	if 100000 < q.Len() {
		t.Errorf("offset of huge curve is not bounded: %d segments", q.Len())
	}
}

func TestPathOffsetWithOptionsSourceUnchanged(t *testing.T) {
	fixtures := map[string]*Path{
		"rounded rect": offsetFixtureRoundedRect(10.0, 20.0, 40.0, 30.0, 5.0),
		"star":         offsetFixtureStar(0.0, 0.0, 10.0, 4.0, 5),
		"hole":         offsetFixtureHole(),
		"long curve":   offsetFixtureLongCurve(),
	}
	for name, p := range fixtures {
		t.Run(name, func(t *testing.T) {
			orig := p.String()
			p.OffsetWithOptions(1.0, OffsetOptions{Tolerance: 0.001})
			p.OffsetWithOptions(-1.0, OffsetOptions{Cap: RoundCap, Join: BevelJoin, Tolerance: 0.001})
			test.T(t, p.String(), orig, "source path was modified")

			// a zero offset returns an independent copy
			q := p.OffsetWithOptions(0.0, OffsetOptions{})
			test.T(t, q.String(), orig)
			q.LineTo(12345.0, 12345.0)
			test.T(t, p.String(), orig, "zero offset aliases the source path")
		})
	}
}
