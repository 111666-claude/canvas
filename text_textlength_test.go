package canvas

import (
	"image"
	"math"
	"strings"
	"testing"

	"github.com/tdewolff/canvas/text"
	"github.com/tdewolff/test"
)

// pathRecorder is a Renderer that records all rendered paths.
type pathRecorder struct {
	paths []*Path
	ms    []Matrix
}

func (r *pathRecorder) Size() (float64, float64) { return 0.0, 0.0 }
func (r *pathRecorder) RenderPath(p *Path, _ Style, m Matrix) {
	r.paths = append(r.paths, p)
	r.ms = append(r.ms, m)
}
func (r *pathRecorder) RenderText(t *Text, m Matrix)    { t.RenderTo(r, m, 0.0) }
func (r *pathRecorder) RenderImage(image.Image, Matrix) {}

func (r *pathRecorder) bounds() Rect {
	rect := Rect{}
	for i, p := range r.paths {
		rect = rect.Add(p.Transform(r.ms[i]).Bounds())
	}
	return rect
}

func textLengthFace(t *testing.T, args ...any) *FontFace {
	t.Helper()
	family := NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("resources/DejaVuSerif.ttf", FontRegular); err != nil {
		test.Error(t, err)
	}
	return family.Face(12.0*ptPerMm, append([]any{Black, FontRegular, FontNormal}, args...)...)
}

func glyphAdvances(face *FontFace, glyphs []text.Glyph) float64 {
	return face.textWidth(glyphs)
}

func TestTextLengthAdjustString(t *testing.T) {
	test.String(t, AdjustSpacing.String(), "spacing")
	test.String(t, AdjustSpacingAndGlyphs.String(), "spacingAndGlyphs")
	test.String(t, TextLengthAdjust(42).String(), "Invalid(42)")
}

func TestTextLengthSpacing(t *testing.T) {
	face := textLengthFace(t)
	s := "Text length"
	orig := face.Glyphs(s)

	txt := NewTextLine(face, s, Left)
	natural := txt.Width
	target := natural * 1.5
	txt.SetTextLength(target, AdjustSpacing)

	// final width is exactly the target
	test.Float(t, txt.Width, target)
	test.T(t, len(txt.lines), 1)
	test.T(t, len(txt.lines[0].spans), 1)
	span := txt.lines[0].spans[0]
	test.Float(t, span.Width, target)
	test.Float(t, span.TextLength, target)
	test.T(t, span.LengthAdjust, AdjustSpacing)

	// the sum of glyph advances equals the target up to font-units rounding
	test.FloatDiff(t, glyphAdvances(face, span.Glyphs), target, face.MmPerEm)

	// the adjustment is distributed evenly over all glyphs
	test.T(t, len(span.Glyphs), len(orig))
	perGlyph := (target - natural) / float64(len(orig)) // mm
	for i, glyph := range span.Glyphs {
		delta := face.MmPerEm * float64(glyph.XAdvance-orig[i].XAdvance)
		test.FloatDiff(t, delta, perGlyph, 2.0*face.MmPerEm) // within two font units
	}

	// setting the same length again is a no-op
	txt.SetTextLength(target, AdjustSpacing)
	test.Float(t, txt.Width, target)
	test.FloatDiff(t, glyphAdvances(face, txt.lines[0].spans[0].Glyphs), target, face.MmPerEm)

	// shrinking back to the natural width restores the advances
	txt.SetTextLength(natural, AdjustSpacing)
	test.Float(t, txt.Width, natural)
	test.FloatDiff(t, glyphAdvances(face, txt.lines[0].spans[0].Glyphs), natural, face.MmPerEm)
}

func TestTextLengthSpacingAndGlyphs(t *testing.T) {
	face := textLengthFace(t)
	s := "Hello"

	txt := NewTextLine(face, s, Left)
	natural := txt.Width
	target := natural * 2.0
	txt.SetTextLength(target, AdjustSpacingAndGlyphs)

	test.Float(t, txt.Width, target)
	span := txt.lines[0].spans[0]
	test.Float(t, span.Width, target)
	test.Float(t, span.TextLength, target)
	test.T(t, span.LengthAdjust, AdjustSpacingAndGlyphs)

	// glyph advances are untouched, outlines are scaled at render time
	test.Float(t, glyphAdvances(face, span.Glyphs), natural)

	// the rendered path equals the natural path scaled by target/natural
	rec := &pathRecorder{}
	txt.RenderTo(rec, Identity, 0.0)
	test.T(t, len(rec.paths), 1)
	p0, _ := face.ToPath(s)
	want := p0.Transform(Identity.Scale(2.0, 1.0)).Bounds()
	got := rec.paths[0].Bounds()
	test.FloatDiff(t, got.X0, want.X0, 0.01)
	test.FloatDiff(t, got.X1, want.X1, 0.01)
	test.FloatDiff(t, got.Y0, want.Y0, 0.01)
	test.FloatDiff(t, got.Y1, want.Y1, 0.01)
}

func TestTextLengthInvalid(t *testing.T) {
	face := textLengthFace(t)
	for _, length := range []float64{0.0, -10.0, math.NaN(), math.Inf(1), math.Inf(-1)} {
		txt := NewTextLine(face, "Hello", Left)
		natural := txt.Width
		txt.SetTextLength(length, AdjustSpacing)
		test.Float(t, txt.Width, natural)
		test.Float(t, txt.lines[0].spans[0].TextLength, 0.0)

		txt.SetTextLength(length, AdjustSpacingAndGlyphs)
		test.Float(t, txt.Width, natural)
		test.Float(t, txt.lines[0].spans[0].TextLength, 0.0)
	}
}

func TestTextLengthEdgeCases(t *testing.T) {
	face := textLengthFace(t)

	// empty text
	txt := NewTextLine(face, "", Left)
	txt.SetTextLength(10.0, AdjustSpacing)
	test.Float(t, txt.Width, 0.0)

	// object spans are not adjusted
	span := TextSpan{Objects: []TextSpanObject{{Width: 5.0, Height: 5.0}}}
	span.SetTextLength(10.0, AdjustSpacing)
	test.Float(t, span.TextLength, 0.0)

	// spans with zero natural advance cannot be scaled with spacingAndGlyphs
	zero := TextSpan{
		Face:   face,
		Width:  0.0,
		Glyphs: []text.Glyph{{SFNT: face.Font.SFNT, Size: face.Size}},
	}
	zero.SetTextLength(10.0, AdjustSpacingAndGlyphs)
	test.Float(t, zero.TextLength, 0.0)
	test.Float(t, zero.Width, 0.0)

	// ...but spacing can still distribute the length
	zero.SetTextLength(10.0, AdjustSpacing)
	test.Float(t, zero.Width, 10.0)
	test.Float(t, zero.TextLength, 10.0)

	// very large glyph counts
	txt = NewTextLine(face, strings.Repeat("a", 100000), Left)
	txt.SetTextLength(100.0, AdjustSpacing)
	test.Float(t, txt.Width, 100.0)
	txt.SetTextLength(100.0, AdjustSpacingAndGlyphs)
	test.Float(t, txt.Width, 100.0)
}

func TestTextLengthMultiline(t *testing.T) {
	face := textLengthFace(t)
	txt := NewTextLine(face, "ab\ncdefg", Left)
	target := 50.0
	txt.SetTextLength(target, AdjustSpacing)
	test.T(t, len(txt.lines), 2)
	for _, line := range txt.lines {
		last := line.spans[len(line.spans)-1]
		test.Float(t, last.X+last.Width-line.spans[0].X, target)
	}
	test.Float(t, txt.Width, target)
}

func TestTextLengthRTL(t *testing.T) {
	face := textLengthFace(t)
	s := "שלום" // Hebrew, right-to-left
	txt := NewTextLine(face, s, Left)
	test.T(t, len(txt.lines), 1)
	span := txt.lines[0].spans[0]
	test.T(t, span.Direction, text.RightToLeft)

	natural := txt.Width
	target := natural * 1.5
	txt.SetTextLength(target, AdjustSpacing)
	test.Float(t, txt.Width, target)
	span = txt.lines[0].spans[0]
	test.Float(t, span.Width, target)
	test.FloatDiff(t, glyphAdvances(face, span.Glyphs), target, face.MmPerEm)

	// logical order of clusters is preserved
	clusters := []uint32{}
	for _, glyph := range span.Glyphs {
		clusters = append(clusters, glyph.Cluster)
	}
	txt2 := NewTextLine(face, s, Left)
	orig := []uint32{}
	for _, glyph := range txt2.lines[0].spans[0].Glyphs {
		orig = append(orig, glyph.Cluster)
	}
	test.T(t, clusters, orig)
}

func TestTextLengthMixedScripts(t *testing.T) {
	face := textLengthFace(t)
	// Latin, CJK (missing glyphs), Hebrew (RTL) and emoji (missing glyphs)
	s := "AB中文xyאב🎉!"
	txt := NewTextLine(face, s, Left)
	test.That(t, 1 < len(txt.lines[0].spans), "expected multiple spans")

	target := 100.0
	txt.SetTextLength(target, AdjustSpacing)
	test.Float(t, txt.Width, target)

	// UTF-8 content and cluster boundaries are untouched
	all := ""
	notdef := 0
	for _, line := range txt.lines {
		total := 0.0
		for _, span := range line.spans {
			all += span.Text
			total += span.Width
			for _, glyph := range span.Glyphs {
				test.That(t, glyph.Cluster < uint32(len(s)), "cluster out of bounds")
				if glyph.ID == 0 {
					notdef++
				}
			}
		}
		test.Float(t, total, target)
	}
	test.String(t, all, s)
	test.That(t, notdef != 0, "expected missing glyphs (.notdef) for CJK/emoji")

	// missing glyphs participate in the adjustment like any other glyph
	txt2 := NewTextLine(face, s, Left)
	txt2.SetTextLength(target, AdjustSpacingAndGlyphs)
	test.Float(t, txt2.Width, target)
	for _, line := range txt2.lines {
		total := 0.0
		for _, span := range line.spans {
			total += span.Width
		}
		test.Float(t, total, target)
	}
}

func TestTextLengthClusterPositions(t *testing.T) {
	face := textLengthFace(t)
	s := "positions"
	txt := NewTextLine(face, s, Left)
	target := 80.0
	txt.SetTextLength(target, AdjustSpacing)
	span := txt.lines[0].spans[0]

	// the position of each cluster is the sum of the adjusted advances before it
	ppem := face.PPEM(0.0)
	x := 0.0
	for i := range span.Glyphs {
		_, w := face.toPath(span.Glyphs[i:i+1], ppem)
		adv := face.MmPerEm * float64(span.Glyphs[i].XAdvance)
		test.FloatDiff(t, w, adv, 1e-9)
		x += adv
	}
	test.FloatDiff(t, x, target, face.MmPerEm)

	// cumulative widths of glyph prefixes match the cumulative advances
	acc := 0.0
	for i := 1; i <= len(span.Glyphs); i++ {
		acc += face.MmPerEm * float64(span.Glyphs[i-1].XAdvance)
		_, w := face.toPath(span.Glyphs[:i], ppem)
		test.FloatDiff(t, w, acc, 1e-9)
	}
}

func TestTextLengthDecoration(t *testing.T) {
	face := textLengthFace(t, FontUnderline)
	txt := NewTextLine(face, "underlined", Left)
	target := 60.0
	txt.SetTextLength(target, AdjustSpacing)

	// the decoration line spans the adjusted width
	rec := &pathRecorder{}
	txt.RenderDecorationsTo(rec, Identity, 0.0)
	test.T(t, len(rec.paths), 1)
	bounds := rec.paths[0].Bounds()
	test.FloatDiff(t, bounds.X1-bounds.X0, target, 0.01)

	// same for spacingAndGlyphs
	txt = NewTextLine(face, "underlined", Left)
	txt.SetTextLength(target, AdjustSpacingAndGlyphs)
	rec = &pathRecorder{}
	txt.RenderDecorationsTo(rec, Identity, 0.0)
	test.T(t, len(rec.paths), 1)
	bounds = rec.paths[0].Bounds()
	test.FloatDiff(t, bounds.X1-bounds.X0, target, 0.01)
}

func TestTextLengthConsistency(t *testing.T) {
	// the adjusted text renders identically through Text.RenderTo (used by the
	// rasterizer and canvas text paths) and through the canvas layer
	face := textLengthFace(t)
	newText := func() *Text {
		txt := NewTextLine(face, "consistent", Left)
		txt.SetTextLength(70.0, AdjustSpacingAndGlyphs)
		return txt
	}

	rec1 := &pathRecorder{}
	newText().RenderTo(rec1, Identity, 0.0)

	c := New(100.0, 20.0)
	c.RenderText(newText(), Identity)
	rec2 := &pathRecorder{}
	c.RenderTo(rec2)

	test.T(t, len(rec1.paths), len(rec2.paths))
	for i := range rec1.paths {
		test.String(t, rec1.paths[i].Transform(rec1.ms[i]).String(), rec2.paths[i].Transform(rec2.ms[i]).String())
	}
}

func TestTextLengthSpansShare(t *testing.T) {
	// a line with several spans (mixed scripts) shares the target length proportionally
	face := textLengthFace(t)
	txt := NewTextLine(face, "abאבcd", Left)
	test.That(t, 1 < len(txt.lines[0].spans), "expected multiple spans")

	target := 90.0
	txt.SetTextLength(target, AdjustSpacingAndGlyphs)
	test.Float(t, txt.Width, target)

	// spans are contiguous and add up to the target
	x := txt.lines[0].spans[0].X
	for _, span := range txt.lines[0].spans {
		test.Float(t, span.X, x)
		x += span.Width
		test.Float(t, span.TextLength, span.Width)
	}
	test.Float(t, x-txt.lines[0].spans[0].X, target)
}
