package pdf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/test"
)

func renderSquarePDF(t *testing.T, align canvas.StrokeAlignment) string {
	t.Helper()
	c := canvas.New(40.0, 40.0)
	ctx := canvas.NewContext(c)
	ctx.Style = pdfStrokeStyle()
	ctx.SetStrokeAlign(align)
	ctx.DrawPath(5.0, 5.0, canvas.Rectangle(30.0, 30.0))

	b := &bytes.Buffer{}
	p := New(b, 40.0, 40.0, &Options{Compress: false})
	c.RenderTo(p)
	test.Error(t, p.Close())
	return b.String()
}

func pdfStrokeStyle() canvas.Style {
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: canvas.Lightblue}
	style.Stroke = canvas.Paint{Color: canvas.Black}
	style.StrokeWidth = 4.0
	return style
}

func TestPDFStrokeAlignCenterUnchanged(t *testing.T) {
	// the center alignment must keep using the native PDF stroke operator
	out := renderSquarePDF(t, canvas.StrokeAlignmentCenter)
	test.That(t, strings.Contains(out, "4 w"), "default alignment must set a native line width")
	test.That(t, strings.Contains(out, " l b\n"), "default alignment must use a fill+stroke operator")
}

func TestPDFStrokeAlignInside(t *testing.T) {
	out := renderSquarePDF(t, canvas.StrokeAlignmentInside)
	// the aligned stroke is an explicit filled outline, not a native stroke
	test.That(t, !strings.Contains(out, "4 w"), "inside alignment must not use a native line width")
	// the original fill path is followed by the stroke outline path
	test.That(t, strings.Contains(out, "5 5 m 35 5 l 35 35 l 5 35 l f"), "fill path")
	test.That(t, strings.Contains(out, "5 5 m 35 5 l 35 35 l 5 35 l h 9 9 m 9 31 l 31 31 l 31 9 l h f"), "inside outline geometry")
}

func TestPDFStrokeAlignOutside(t *testing.T) {
	out := renderSquarePDF(t, canvas.StrokeAlignmentOutside)
	test.That(t, !strings.Contains(out, "4 w"), "outside alignment must not use a native line width")
	test.That(t, strings.Contains(out, "1 1 m 39 1 l 39 39 l 1 39 l h 5 5 m 5 35 l 35 35 l 35 5 l h f"), "outside outline geometry")
}
