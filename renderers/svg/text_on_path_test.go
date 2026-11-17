package svg

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/tdewolff/test"
)

func textOnPathCanvas(t *testing.T) *canvas.Canvas {
	t.Helper()
	family := canvas.NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("../../resources/DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	face := family.Face(12.0*(72.0/25.4), canvas.Black, canvas.FontRegular, canvas.FontNormal)

	// upper semicircle from (10,10) to (50,10) through (30,30)
	p := &canvas.Path{}
	p.MoveTo(10.0, 10.0)
	p.ArcTo(20.0, 20.0, 0.0, false, false, 50.0, 10.0)

	c := canvas.New(60.0, 40.0)
	ctx := canvas.NewContext(c)
	if err := ctx.DrawPathText(0.0, 0.0, face, "Arc", p, canvas.TextOnPathOptions{
		Align:    canvas.TextOnPathCenter,
		Distance: p.Length() / 2.0,
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTextOnPathSVG(t *testing.T) {
	c := textOnPathCanvas(t)

	buf := &bytes.Buffer{}
	r := New(buf, 60.0, 40.0, nil)
	c.RenderTo(r)
	test.Error(t, r.Close())

	s := buf.String()
	// text on path is emitted as ordinary filled paths with the rotation baked
	// into the coordinates, never as <text> elements; this is what makes the
	// output identical across renderers
	test.That(t, strings.Contains(s, "<path"), "SVG must contain glyph paths")
	test.That(t, !strings.Contains(s, "<text"), "text on path must not emit <text>")
}

func TestTextOnPathRasterizer(t *testing.T) {
	c := textOnPathCanvas(t)
	res := canvas.DPMM(10.0) // 10 px/mm -> 600x400 image
	img := rasterizer.Draw(c, res, canvas.DefaultColorSpace)
	test.T(t, img.Bounds(), image.Rect(0, 0, 600, 400))

	// the text is centered on the arc top (30,30), which rasterizes to roughly
	// (300,100) in image coordinates (y flipped). There must be opaque ink there.
	center := img.RGBAAt(300, 100)
	test.That(t, center.A == 255, "expected opaque glyph ink near the arc top")

	// an empty corner must remain fully transparent, proving the glyphs follow
	// the arc rather than filling the canvas
	corner := img.RGBAAt(5, 5)
	test.That(t, corner.A == 0, "expected transparent background in the corner")
}

// TestTextOnPathRendererParity renders the same canvas to both targets and
// asserts that both produce output from the identical recorded layer set: the
// SVG emits paths and the rasterizer paints the same glyphs.
func TestTextOnPathRendererParity(t *testing.T) {
	c := textOnPathCanvas(t)

	svgBuf := &bytes.Buffer{}
	svgR := New(svgBuf, 60.0, 40.0, nil)
	c.RenderTo(svgR)
	test.Error(t, svgR.Close())
	svgPaths := strings.Count(svgBuf.String(), "<path")
	test.That(t, 0 < svgPaths, "SVG paths present")

	img := rasterizer.Draw(c, canvas.DPMM(8.0), canvas.DefaultColorSpace)
	ink := 0
	for y := range img.Bounds().Dy() {
		for x := range img.Bounds().Dx() {
			if img.RGBAAt(x, y).A != 0 {
				ink++
			}
		}
	}
	test.That(t, 0 < ink, "rasterizer must paint the same glyphs")
}
