package svg

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/test"
)

func renderSquare(t *testing.T, align canvas.StrokeAlignment, style canvas.Style) string {
	t.Helper()
	c := canvas.New(40.0, 40.0)
	ctx := canvas.NewContext(c)
	ctx.Style = style
	ctx.SetStrokeAlign(align)
	ctx.DrawPath(5.0, 5.0, canvas.Rectangle(30.0, 30.0))

	b := &bytes.Buffer{}
	s := New(b, 40.0, 40.0, nil)
	c.RenderTo(s)
	return b.String()
}

func strokeStyle() canvas.Style {
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: canvas.Lightblue}
	style.Stroke = canvas.Paint{Color: canvas.Black}
	style.StrokeWidth = 4.0
	return style
}

func TestSVGStrokeAlignCenterUnchanged(t *testing.T) {
	// the center alignment must keep using the native SVG stroke, so the output has no outline path
	out := renderSquare(t, canvas.StrokeAlignmentCenter, strokeStyle())
	test.That(t, strings.Contains(out, `stroke-width:4`), "default alignment must use a native stroke")
	test.That(t, 1 == strings.Count(out, "<path "), "default alignment must emit a single path")
}

func TestSVGStrokeAlignInside(t *testing.T) {
	out := renderSquare(t, canvas.StrokeAlignmentInside, strokeStyle())
	// the aligned stroke is drawn as a filled outline instead of a native stroke
	test.That(t, !strings.Contains(out, "stroke-width"), "inside alignment must not use a native stroke")
	test.That(t, 2 == strings.Count(out, "<path "), "inside alignment must emit a separate outline path")
	// the outline is the boundary of the 30x30 square and an inset 4 (the stroke width) rectangle
	test.That(t, strings.Contains(out, "M5 35H35V5H5zM9 31V9H31V31z"), "inside outline geometry")
}

func TestSVGStrokeAlignOutside(t *testing.T) {
	out := renderSquare(t, canvas.StrokeAlignmentOutside, strokeStyle())
	test.That(t, !strings.Contains(out, "stroke-width"), "outside alignment must not use a native stroke")
	test.That(t, 2 == strings.Count(out, "<path "), "outside alignment must emit a separate outline path")
	test.That(t, strings.Contains(out, "M1 39H39V1H1zM5 35V5H35V35z"), "outside outline geometry")
}

func TestSVGStrokeAlignDashed(t *testing.T) {
	style := strokeStyle()
	style.Dashes = []float64{3.0, 2.0}
	style.StrokeCapper = canvas.ButtCap
	style.StrokeJoiner = canvas.MiterJoin
	out := renderSquare(t, canvas.StrokeAlignmentInside, style)
	test.That(t, !strings.Contains(out, "stroke-dasharray"), "aligned dashed stroke must be drawn as an outline")
	test.That(t, 2 == strings.Count(out, "<path "), "aligned dashed stroke must emit a separate outline path")
}
