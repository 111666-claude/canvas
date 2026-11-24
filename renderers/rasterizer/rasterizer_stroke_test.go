package rasterizer

import (
	"image"
	"image/color"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/test"
)

var alignTestResolution = canvas.DPMM(10.0) // 10 pixels per mm

// renderAligned renders a 30x30 mm square filled with white and stroked with black at the given
// alignment, at origin (5,5), and returns the image.
func renderAligned(t *testing.T, align canvas.StrokeAlignment, width float64, strokeAlpha uint8) *image.RGBA {
	t.Helper()
	c := canvas.New(40.0, 40.0)
	ctx := canvas.NewContext(c)
	ctx.SetFillColor(canvas.White)
	ctx.DrawPath(0.0, 0.0, canvas.Rectangle(40.0, 40.0))

	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: canvas.Lightblue}
	style.Stroke = canvas.Paint{Color: color.RGBA{0, 0, 0, strokeAlpha}}
	style.StrokeWidth = width
	style.StrokeCapper = canvas.ButtCap
	style.StrokeJoiner = canvas.MiterJoin
	ctx.Style = style
	ctx.SetStrokeAlign(align)
	ctx.DrawPath(5.0, 5.0, canvas.Rectangle(30.0, 30.0))
	return Draw(c, alignTestResolution, canvas.LinearColorSpace{})
}

// isDark returns true when the pixel at (x,y) is close to black, i.e. is covered by the stroke.
func isDark(img *image.RGBA, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return r < 0x2000 && g < 0x2000 && b < 0x2000
}

func TestRasterizerStrokeAlignPixels(t *testing.T) {
	// at 10 px/mm the square spans x,y in [50,350) pixels with the boundary at 50 and 350.
	// A stroke of width 4mm is 40 pixels wide.
	center := renderAligned(t, canvas.StrokeAlignmentCenter, 4.0, 255)
	inside := renderAligned(t, canvas.StrokeAlignmentInside, 4.0, 255)
	outside := renderAligned(t, canvas.StrokeAlignmentOutside, 4.0, 255)

	// center: the stroke straddles the boundary at x=50, spanning x in [30,70], so pixels just
	// inside and outside the boundary are dark while x=25 is not
	test.That(t, isDark(center, 35, 200), "center stroke must extend outside the boundary")
	test.That(t, isDark(center, 65, 200), "center stroke must extend inside the boundary")
	test.That(t, !isDark(center, 25, 200), "center stroke must not reach far outside")

	// inside: nothing outside the boundary, the stroke lies within [50,90)
	test.That(t, !isDark(inside, 45, 200), "inside stroke must not extend outside the boundary")
	test.That(t, isDark(inside, 55, 200), "inside stroke must cover just inside the boundary")
	test.That(t, isDark(inside, 85, 200), "inside stroke must be the full width inside")
	test.That(t, !isDark(inside, 95, 200), "inside stroke must not extend deeper than its width")

	// outside: nothing inside the boundary, the stroke lies within (50,90] outside
	test.That(t, !isDark(outside, 55, 200), "outside stroke must not extend inside the boundary")
	test.That(t, isDark(outside, 45, 200), "outside stroke must cover just outside the boundary")
	test.That(t, isDark(outside, 15, 200), "outside stroke must be the full width outside")
	test.That(t, !isDark(outside, 5, 200), "outside stroke must not extend deeper than its width")

	// the interior of the fill is never covered by an inside stroke beyond its width
	test.That(t, !isDark(inside, 200, 200), "the fill must remain visible in the interior for inside strokes")
}

func TestRasterizerStrokeAlignTransparency(t *testing.T) {
	// a fully transparent stroke draws nothing for all alignments
	for _, align := range []canvas.StrokeAlignment{canvas.StrokeAlignmentCenter, canvas.StrokeAlignmentInside, canvas.StrokeAlignmentOutside} {
		img := renderAligned(t, align, 4.0, 0)
		test.That(t, !isDark(img, 55, 200), "transparent stroke must not draw inside for", align)
		test.That(t, !isDark(img, 45, 200), "transparent stroke must not draw outside for", align)
	}

	// a half-transparent stroke composites over the fill and background, so it is a blend rather
	// than solid black, for all alignments
	for _, align := range []canvas.StrokeAlignment{canvas.StrokeAlignmentCenter, canvas.StrokeAlignmentInside, canvas.StrokeAlignmentOutside} {
		img := renderAligned(t, align, 4.0, 128)
		r, _, _, a := img.At(55, 200).RGBA()
		test.That(t, a == 0xffff, "image must be opaque")
		// lightblue (173,216,230) blended with 50% black yields a mid-tone, not black and not the fill
		test.That(t, 0x2000 < r, "translucent stroke must blend rather than stay solid for", align)
	}
	test.That(t, true, "")
}
