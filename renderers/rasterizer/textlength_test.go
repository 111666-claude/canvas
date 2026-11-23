package rasterizer

import (
	"image"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/test"
)

type inkRect struct {
	x0, y0, x1, y1 float64
}

func (r inkRect) w() float64 { return r.x1 - r.x0 }

// inkBounds returns the bounding box of the non-transparent pixels in millimeters.
func inkBounds(img *image.RGBA, dpmm float64) inkRect {
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y).A != 0 {
				if x < minX {
					minX = x
				}
				if maxX < x {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if maxY < y {
					maxY = y
				}
			}
		}
	}
	return inkRect{
		float64(minX) / dpmm,
		float64(minY) / dpmm,
		float64(maxX+1) / dpmm,
		float64(maxY+1) / dpmm,
	}
}

func textLengthCanvas(t *testing.T, target float64, adjust canvas.TextLengthAdjust, underline bool) (*canvas.Canvas, *canvas.Text) {
	family := canvas.NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("../../resources/DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		test.Error(t, err)
	}
	args := []any{canvas.Black, canvas.FontRegular, canvas.FontNormal}
	if underline {
		args = append(args, canvas.FontUnderline)
	}
	face := family.Face(28.0, args...) // 28pt = 10mm

	c := canvas.New(120.0, 20.0)
	ctx := canvas.NewContext(c)
	text := canvas.NewTextLine(face, "Hello", canvas.Left)
	if 0.0 < target {
		text.SetTextLength(target, adjust)
	}
	ctx.DrawText(10.0, 15.0, text)
	return c, text
}

// The underline decoration spans the full advance of the text, so the rendered ink width
// must equal the target text length exactly (up to rasterization), for both adjustment modes.
func TestTextLengthPixels(t *testing.T) {
	dpmm := 4.0
	render := func(target float64, adjust canvas.TextLengthAdjust, underline bool) inkRect {
		c, _ := textLengthCanvas(t, target, adjust, underline)
		return inkBounds(Draw(c, canvas.DPMM(dpmm), canvas.DefaultColorSpace), dpmm)
	}

	const target = 80.0
	for _, adjust := range []canvas.TextLengthAdjust{
		canvas.AdjustSpacing,
		canvas.AdjustSpacingAndGlyphs,
	} {
		ink := render(target, adjust, true)
		test.FloatDiff(t, ink.x0, 10.0, 0.5)
		test.FloatDiff(t, ink.w(), target, 0.5) // underline fills the target length exactly
	}

	// without adjustment the text is much narrower than the target
	natural := render(0.0, canvas.AdjustSpacing, true)
	test.That(t, natural.w() < 40.0, "natural text too wide:", natural.w())

	// for spacingAndGlyphs the glyph ink itself is stretched by the same factor
	naturalInk := render(0.0, canvas.AdjustSpacing, false)
	stretchedInk := render(target, canvas.AdjustSpacingAndGlyphs, false)
	s := target / natural.w()
	test.FloatDiff(t, stretchedInk.w(), naturalInk.w()*s, 2.0)

	// for spacing the glyph ink never exceeds the target length
	spacingInk := render(target, canvas.AdjustSpacing, false)
	test.That(t, spacingInk.x1 <= 10.0+target+0.5, "ink exceeds target:", spacingInk.x1)
	test.That(t, naturalInk.w()+10.0 < spacingInk.w(), "ink not stretched:", spacingInk.w())
}
