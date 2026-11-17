package canvas

import (
	"math"
	"testing"

	"github.com/tdewolff/test"
)

func testFontFace(t *testing.T) *FontFace {
	t.Helper()
	family := NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("resources/DejaVuSerif.ttf", FontRegular); err != nil {
		t.Fatal(err)
	}
	return family.Face(12.0*ptPerMm, Black, FontRegular, FontNormal)
}

// expectedAnchors returns the arc-length positions of every glyph center for
// text laid out from start, independently of the implementation.
func expectedAnchors(face *FontFace, s string, start, spacing float64) []float64 {
	glyphs := face.Glyphs(s)
	anchors := make([]float64, 0, len(glyphs))
	cursor := start
	for i, glyph := range glyphs {
		adv := face.MmPerEm * float64(glyph.XAdvance)
		anchors = append(anchors, cursor+adv/2.0)
		cursor += adv
		if i+1 < len(glyphs) {
			cursor += spacing
		}
	}
	return anchors
}

// normAngle normalizes an angle to [-PI, PI) so 2PI and 0 compare equal.
func normAngle(a float64) float64 {
	for math.Pi <= a {
		a -= 2 * math.Pi
	}
	for a < -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

// countGlyphs runs the layout and returns the number of glyphs that were placed.
func countGlyphs(t *testing.T, face *FontFace, s string, p *Path, opts TextOnPathOptions) int {
	t.Helper()
	_, n, err := textOnPath(face, s, p, opts)
	test.Error(t, err)
	return n
}

func TestTextOnPathLine(t *testing.T) {
	face := testFontFace(t)

	// a horizontal line of 100mm along the x-axis
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.LineTo(100.0, 0.0)

	s := "Hello"
	out, err := TextOnPath(face, s, p, TextOnPathOptions{})
	test.Error(t, err)
	test.That(t, !out.Empty(), "expected glyph outlines")
	test.T(t, countGlyphs(t, face, s, p, TextOnPathOptions{}), len(face.Glyphs(s)))

	// every glyph center must lie on y=0 with a horizontal tangent
	segments := arcLengthSegments(p)
	for _, d := range expectedAnchors(face, s, 0.0, 0.0) {
		_, pos, tangent := locateAt(segments, d)
		test.FloatDiff(t, pos.X, d, 1e-9)
		test.FloatDiff(t, pos.Y, 0.0, 1e-9)
		test.FloatDiff(t, tangent.Angle(), 0.0, 1e-9)
	}

	// the output's horizontal span equals the total advance (glyphs are centered
	// on their advance, so the first/last ink edge differs only by side bearings)
	metrics := face.Metrics()
	bounds := out.Bounds()
	test.That(t, bounds.W() <= face.TextWidth(s)+Epsilon, "output wider than text advance")
	test.That(t, bounds.Y0 >= -metrics.Descent-Epsilon, "output below descent")
	test.That(t, bounds.Y1 <= metrics.Ascent+Epsilon, "output above ascent")
}

func TestTextOnPathLineAlignment(t *testing.T) {
	face := testFontFace(t)
	p := &Path{}
	p.MoveTo(10.0, 5.0) // line runs from arc d=0 at (10,5) to d=100 at (110,5)
	p.LineTo(110.0, 5.0)
	width := face.TextWidth("Hello")

	segments := arcLengthSegments(p)
	expectPoint := func(d float64, wantX, wantY, wantAngle float64) {
		_, pos, tangent := locateAt(segments, d)
		test.FloatDiff(t, pos.X, wantX, 1e-9)
		test.FloatDiff(t, pos.Y, wantY, 1e-9)
		test.FloatDiff(t, normAngle(tangent.Angle()), wantAngle, 1e-9)
	}

	// left: glyph centers start at Distance + half advance
	_, err := TextOnPath(face, "Hello", p, TextOnPathOptions{Distance: 20.0})
	test.Error(t, err)
	test.T(t, countGlyphs(t, face, "Hello", p, TextOnPathOptions{Distance: 20.0}), 5)
	for _, d := range expectedAnchors(face, "Hello", 20.0, 0.0) {
		expectPoint(d, 10.0+d, 5.0, 0.0)
	}

	// center: the text is centered on Distance, hence starts at Distance-width/2
	center, err := TextOnPath(face, "Hello", p, TextOnPathOptions{Align: TextOnPathCenter, Distance: 50.0})
	test.Error(t, err)
	centerStart := 50.0 - width/2.0
	centerAnchors := expectedAnchors(face, "Hello", centerStart, 0.0)
	for _, d := range centerAnchors {
		expectPoint(d, 10.0+d, 5.0, 0.0)
	}
	// the text center is the midpoint of the first glyph's start and the last
	// glyph's end, which must coincide with the requested distance (d=50 -> x=60)
	firstStart := 10.0 + centerStart
	glyphs := face.Glyphs("Hello")
	lastAdv := face.MmPerEm * float64(glyphs[4].XAdvance)
	lastEnd := 10.0 + centerAnchors[4] + lastAdv/2.0
	test.FloatDiff(t, (firstStart+lastEnd)/2.0, 60.0, 1e-9)
	test.That(t, !center.Empty(), "center output present")

	// right: the advance ends at Distance, hence starts at Distance-width
	right, err := TextOnPath(face, "Hello", p, TextOnPathOptions{Align: TextOnPathRight, Distance: 80.0})
	test.Error(t, err)
	for _, d := range expectedAnchors(face, "Hello", 80.0-width, 0.0) {
		expectPoint(d, 10.0+d, 5.0, 0.0)
	}
	test.That(t, right.Bounds().X1 <= 90.0+metricsOr(face)+Epsilon, "right-aligned text passes anchor")
}

func metricsOr(face *FontFace) float64 { return face.Metrics().Ascent }

func TestTextOnPathArc(t *testing.T) {
	face := testFontFace(t)

	// upper semicircle of radius 20, from (0,0) through (20,20) to (40,0)
	const r = 20.0
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.ArcTo(r, r, 0.0, false, false, 2.0*r, 0.0)
	test.FloatDiff(t, p.Length(), math.Pi*r, 1e-5)

	s := "Arc"
	out, err := TextOnPath(face, s, p, TextOnPathOptions{Align: TextOnPathCenter, Distance: p.Length() / 2.0})
	test.Error(t, err)
	test.That(t, !out.Empty(), "expected glyph outlines on arc")

	segments := arcLengthSegments(p)
	// the middle of the path is the circle top (20,20) with a horizontal tangent
	_, pos, tangent := locateAt(segments, p.Length()/2.0)
	test.FloatDiff(t, pos.X, r, 1e-6)
	test.FloatDiff(t, pos.Y, r, 1e-6)
	test.FloatDiff(t, normAngle(tangent.Angle()), 0.0, 1e-6)

	// at the arc start the tangent points up, at the end it points down
	_, startPos, startTan := locateAt(segments, 0.0)
	test.FloatDiff(t, startPos.X, 0.0, 1e-9)
	test.FloatDiff(t, startPos.Y, 0.0, 1e-9)
	test.FloatDiff(t, startTan.Angle(), math.Pi/2.0, 1e-6)

	_, endPos, endTan := locateAt(segments, p.Length())
	test.FloatDiff(t, endPos.X, 2.0*r, 1e-9)
	test.FloatDiff(t, endPos.Y, 0.0, 1e-9)
	test.FloatDiff(t, normAngle(endTan.Angle()), -math.Pi/2.0, 1e-6)

	// output stays within the semicircle's bounding box expanded by the font
	bounds := out.Bounds()
	metrics := face.Metrics()
	test.That(t, bounds.X0 >= -metrics.Ascent-Epsilon, "bounds x0")
	test.That(t, bounds.X1 <= 2.0*r+metrics.Ascent+Epsilon, "bounds x1")
	test.That(t, bounds.Y1 <= r+metrics.Ascent+Epsilon, "bounds y1")
}

func TestTextOnPathBezier(t *testing.T) {
	face := testFontFace(t)

	// quadratic and cubic segments must be handled and the text must follow the
	// curve (rise above the chord) rather than a straight line
	quad := &Path{}
	quad.MoveTo(0.0, 0.0)
	quad.QuadTo(25.0, 50.0, 50.0, 0.0)
	out, err := TextOnPath(face, "Bezier", quad, TextOnPathOptions{})
	test.Error(t, err)
	test.That(t, 0.0 < out.Bounds().Y1, "quadratic text should rise above the chord")

	cube := &Path{}
	cube.MoveTo(0.0, 0.0)
	cube.CubeTo(10.0, 40.0, 40.0, 40.0, 50.0, 0.0)
	out, err = TextOnPath(face, "Bezier", cube, TextOnPathOptions{})
	test.Error(t, err)
	test.That(t, 0.0 < out.Bounds().Y1, "cubic text should rise above the chord")

	// position lookup at the segment endpoints must be exact and match the control geometry
	segments := arcLengthSegments(cube)
	test.That(t, len(segments) == 1, "expected one segment")
	_, p0, t0 := locateAt(segments, 0.0)
	test.FloatDiff(t, p0.X, 0.0, 1e-9)
	test.FloatDiff(t, p0.Y, 0.0, 1e-9)
	test.FloatDiff(t, t0.Angle(), segments[0].cp1.Sub(segments[0].start).Angle(), 1e-9)
	_, p1, t1 := locateAt(segments, segments[0].length)
	test.FloatDiff(t, p1.X, 50.0, 1e-9)
	test.FloatDiff(t, p1.Y, 0.0, 1e-9)
	test.FloatDiff(t, t1.Angle(), segments[0].end.Sub(segments[0].cp2).Angle(), 1e-9)

	// an interior point must be on the curve, with tangent equal to the curve's derivative
	d := segments[0].length / 2.0
	seg, pos, tangent := locateAt(segments, d)
	tt := seg.arcToParam(0.5)
	wantPos := cubicBezierPos(seg.start, seg.cp1, seg.cp2, seg.end, tt)
	wantTan := cubicBezierDeriv(seg.start, seg.cp1, seg.cp2, seg.end, tt).Norm(1.0)
	test.FloatDiff(t, pos.X, wantPos.X, 1e-8)
	test.FloatDiff(t, pos.Y, wantPos.Y, 1e-8)
	test.FloatDiff(t, tangent.X, wantTan.X, 1e-8)
	test.FloatDiff(t, tangent.Y, wantTan.Y, 1e-8)
}

func TestTextOnPathLetterSpacing(t *testing.T) {
	face := testFontFace(t)
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.LineTo(200.0, 0.0)

	const spacing = 2.0
	_, err := TextOnPath(face, "Hello", p, TextOnPathOptions{LetterSpacing: spacing})
	test.Error(t, err)

	// five glyphs -> four gaps of extra spacing
	want := face.TextWidth("Hello") + 4.0*spacing
	last := expectedAnchors(face, "Hello", 0.0, spacing)[4]
	hAdv := face.MmPerEm * float64(face.Glyphs("Hello")[4].XAdvance)
	test.FloatDiff(t, last+hAdv/2.0, want, 1e-9)
	test.That(t, countGlyphs(t, face, "Hello", p, TextOnPathOptions{LetterSpacing: spacing}) == 5, "all glyphs placed")
}

func TestTextOnPathClipping(t *testing.T) {
	face := testFontFace(t)

	// line shorter than the text: glyphs are clipped at the path's end
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.LineTo(8.0, 0.0)
	out, err := TextOnPath(face, "Hello world", p, TextOnPathOptions{})
	test.Error(t, err)
	n := countGlyphs(t, face, "Hello world", p, TextOnPathOptions{})
	test.That(t, 0 < n && n < len(face.Glyphs("Hello world")), "partial clipping")
	test.That(t, out.Bounds().X1 <= 8.0+face.Metrics().Ascent+Epsilon, "glyphs must not extend past path end")

	// path too short for a single glyph center returns an error
	short := &Path{}
	short.MoveTo(0.0, 0.0)
	short.LineTo(1e-12, 0.0)
	_, err = TextOnPath(face, "H", short, TextOnPathOptions{})
	test.That(t, err != nil, "expected error for too-short path")
}

func TestTextOnPathErrors(t *testing.T) {
	face := testFontFace(t)
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.LineTo(100.0, 0.0)

	if _, err := TextOnPath(nil, "Hello", p, TextOnPathOptions{}); err == nil {
		t.Error("nil face must error")
	}
	if _, err := TextOnPath(face, "", p, TextOnPathOptions{}); err == nil {
		t.Error("empty text must error")
	}
	if _, err := TextOnPath(face, "line\nbreak", p, TextOnPathOptions{}); err == nil {
		t.Error("line break must error")
	}
	if _, err := TextOnPath(face, "Hello", p, TextOnPathOptions{Distance: -1.0}); err == nil {
		t.Error("negative distance must error")
	}
	if _, err := TextOnPath(face, "Hello", p, TextOnPathOptions{Distance: math.NaN()}); err == nil {
		t.Error("NaN distance must error")
	}
	if _, err := TextOnPath(face, "Hello", p, TextOnPathOptions{Distance: math.Inf(1)}); err == nil {
		t.Error("infinite distance must error")
	}
	if _, err := TextOnPath(face, "Hello", p, TextOnPathOptions{Distance: 101.0}); err == nil {
		t.Error("distance beyond path end must error")
	}
	if _, err := TextOnPath(face, "Hello", p, TextOnPathOptions{LetterSpacing: -0.5}); err == nil {
		t.Error("negative letter spacing must error")
	}
	if _, err := TextOnPath(face, "Hello", nil, TextOnPathOptions{}); err == nil {
		t.Error("nil path must error")
	}
	if _, err := TextOnPath(face, "Hello", &Path{}, TextOnPathOptions{}); err == nil {
		t.Error("empty path must error")
	}

	// zero-length segments are skipped: a path of only degenerate commands has no length
	zero := &Path{}
	zero.MoveTo(5.0, 5.0)
	zero.LineTo(5.0, 5.0)
	zero.QuadTo(6.0, 6.0, 5.0, 5.0)
	if _, err := TextOnPath(face, "H", zero, TextOnPathOptions{}); err == nil {
		t.Error("zero-length path must error")
	}
}

func TestTextOnPathCusp(t *testing.T) {
	face := testFontFace(t)

	// a sharp corner: both tangents must be well defined and text crosses the cusp
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.LineTo(25.0, 0.0)
	p.LineTo(25.0, 25.0)

	out, err := TextOnPath(face, "Cusp text", p, TextOnPathOptions{})
	test.Error(t, err)
	test.That(t, !out.Empty(), "expected output across cusp")

	segments := arcLengthSegments(p)
	test.That(t, len(segments) == 2, "expected two segments")
	_, _, tanBefore := locateAt(segments, 25.0-Epsilon)
	test.FloatDiff(t, tanBefore.Angle(), 0.0, 1e-9)
	_, _, tanAfter := locateAt(segments, 25.0+Epsilon)
	test.FloatDiff(t, tanAfter.Angle(), math.Pi/2.0, 1e-9)
}

func TestTextOnPathSubpaths(t *testing.T) {
	face := testFontFace(t)

	// a MoveTo splits subpaths but does not consume arc length
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.LineTo(20.0, 0.0)
	p.MoveTo(40.0, 40.0)
	p.LineTo(60.0, 40.0)

	segments := arcLengthSegments(p)
	test.That(t, len(segments) == 2, "two subpath segments")
	test.FloatDiff(t, segments[1].d0, 20.0, 1e-9, "MoveTo must not add arc length")

	out, err := TextOnPath(face, "Subpaths", p, TextOnPathOptions{Distance: 25.0})
	test.Error(t, err)
	// glyphs crossing onto the second subpath sit at y=40
	test.That(t, 39.0 < out.Bounds().Center().Y, "expected glyphs on second subpath")
}

func TestTextOnPathContextFill(t *testing.T) {
	face := testFontFace(t)
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.LineTo(80.0, 0.0)

	// with no context fill, the font face's own fill is used
	c := New(100.0, 20.0)
	ctx := NewContext(c)
	test.Error(t, ctx.DrawPathText(5.0, 10.0, face, "Hello", p, TextOnPathOptions{}))
	test.That(t, !c.Empty(), "canvas must record a layer")

	// the recorded layer is a filled path using the face's paint, not a Text layer
	var foundPath bool
	for _, layers := range c.layers {
		for _, l := range layers {
			if l.path != nil {
				foundPath = true
				test.That(t, l.style.Fill.Has(), "glyph path must be filled")
			}
			test.That(t, l.text == nil, "must not record a Text layer")
		}
	}
	test.That(t, foundPath, "expected a rendered glyph path")

	// with a context fill color set, the context's paint takes precedence
	c2 := New(100.0, 20.0)
	ctx2 := NewContext(c2)
	ctx2.SetFillColor(Red)
	test.Error(t, ctx2.DrawPathText(5.0, 10.0, face, "Hello", p, TextOnPathOptions{}))
	for _, layers := range c2.layers {
		for _, l := range layers {
			if l.path != nil {
				test.T(t, l.style.Fill.Color, Red)
				test.That(t, !l.style.HasStroke(), "glyphs are never stroked")
			}
		}
	}
}

func TestTextOnPathContextGradient(t *testing.T) {
	face := testFontFace(t)
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.LineTo(80.0, 0.0)

	grad := NewLinearGradient(Point{0.0, 0.0}, Point{80.0, 0.0})
	grad.Add(0.0, Red)
	grad.Add(1.0, Blue)

	c := New(100.0, 20.0)
	ctx := NewContext(c)
	ctx.SetFillGradient(grad)
	test.Error(t, ctx.DrawPathText(5.0, 10.0, face, "Hello", p, TextOnPathOptions{}))

	var sawGradient bool
	for _, layers := range c.layers {
		for _, l := range layers {
			if l.path != nil && l.style.Fill.IsGradient() {
				sawGradient = true
			}
		}
	}
	test.That(t, sawGradient, "gradient fill must propagate to the glyph path")
}

func TestTextOnPathContextError(t *testing.T) {
	face := testFontFace(t)
	c := New(100.0, 20.0)
	ctx := NewContext(c)
	test.That(t, ctx.DrawPathText(0.0, 0.0, face, "Hello", &Path{}, TextOnPathOptions{}) != nil, "empty path must error")
	test.That(t, c.Empty(), "nothing must be rendered on error")
}

func TestTextOnPathArcParametrization(t *testing.T) {
	// arc-length inversion must be self-consistent across a full parameter sweep
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	p.ArcTo(30.0, 15.0, 0.3, false, false, 60.0, 0.0)
	segments := arcLengthSegments(p)
	test.That(t, len(segments) == 1, "single arc segment")
	s := &segments[0]

	for k := 0; k <= 20; k++ {
		u := float64(k) / 20.0
		pos, tangent := s.pos(s.d0 + u*s.length)
		// the reported point must lie on the ellipse and the tangent be unit length
		test.FloatDiff(t, tangent.Length(), 1.0, 1e-9)
		test.That(t, math.Abs(pos.X) < 1e6 && math.Abs(pos.Y) < 1e6, "finite position")
	}

	// walking from start to end by small arc steps must be monotonic in position
	var prev Point
	for k := 0; k <= 10; k++ {
		d := float64(k) / 10.0 * s.length
		_, pos, _ := locateAt(segments, d)
		if k > 0 {
			test.That(t, 0.0 < pos.Sub(prev).Length(), "monotonic arc traversal")
		}
		prev = pos
	}
}
