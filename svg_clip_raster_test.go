package canvas_test

import (
	"image"
	"strconv"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/tdewolff/test"
)

// raster renders an SVG canvas at one pixel per user unit.
func raster(t *testing.T, c *canvas.Canvas) *image.RGBA {
	t.Helper()
	return rasterizer.Draw(c, canvas.DPMM(1.0), canvas.DefaultColorSpace)
}

func parse(t *testing.T, svg string) *canvas.Canvas {
	t.Helper()
	c, err := canvas.ParseSVG(strings.NewReader(svg))
	test.Error(t, err)
	return c
}

// referenceCanvas renders a single filling path in canvas space, built
// independently of the SVG clipping machinery, as the geometric reference.
func referenceCanvas(w, h float64, path *canvas.Path, m canvas.Matrix, rule canvas.FillRule) *canvas.Canvas {
	c := canvas.New(w, h)
	c.RenderPath(path, canvas.Style{Fill: canvas.Paint{Color: canvas.Black}, FillRule: rule}, m)
	return c
}

// assertCoverageMatch compares a clipped render against an independently
// rendered reference. Pixels fully outside the reference must be empty in the
// clip result; pixels fully inside must be painted. Anti-aliased boundary
// pixels of the reference are ignored.
func assertCoverageMatch(t *testing.T, got, want *image.RGBA) {
	t.Helper()
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			wa := want.RGBAAt(x, y).A
			ga := got.RGBAAt(x, y).A
			if wa == 0 && ga != 0 {
				t.Errorf("paint outside reference at (%d,%d): alpha=%d", x, y, ga)
			} else if wa == 255 && ga == 0 {
				t.Errorf("missing paint inside reference at (%d,%d)", x, y)
			}
		}
	}
}

func TestClipRasterFillRule(t *testing.T) {
	// evenodd: overlapping region is a hole; nonzero: it is filled
	evenodd := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<clipPath id="c">
			<path d="M0,0 H40 V40 H0 Z M10,10 H30 V30 H10 Z" fill-rule="evenodd"/>
		</clipPath>
	</defs>
	<rect x="0" y="0" width="100" height="100" fill="red" clip-path="url(#c)"/>
</svg>`
	c := parse(t, evenodd)
	img := raster(t, c)

	// independent reference: ring path expressed directly in canvas space.
	// The reference canvas is Cartesian I (y-up); SVG rect 0..40,0..40 maps
	// to canvas y 60..100 and the SVG hole 10..30,10..30 to y 70..90.
	outer := canvas.Rectangle(40, 40).Transform(canvas.Identity.Translate(0, 60))
	hole := canvas.Rectangle(20, 20).Transform(canvas.Identity.Translate(10, 70))
	ring := outer.Append(hole)
	want := raster(t, referenceCanvas(100, 100, ring, canvas.Identity, canvas.EvenOdd))
	assertCoverageMatch(t, img, want)

	nonzero := strings.Replace(evenodd, `fill-rule="evenodd"`, `fill-rule="nonzero"`, 1)
	c2 := parse(t, nonzero)
	img2 := raster(t, c2)
	filled := canvas.Rectangle(40, 40).Transform(canvas.Identity.Translate(0, 60))
	want2 := raster(t, referenceCanvas(100, 100, filled, canvas.Identity, canvas.NonZero))
	assertCoverageMatch(t, img2, want2)
}

func TestClipRasterStrokedTarget(t *testing.T) {
	// strokes are clipped: nothing paints outside the clip rectangle
	svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<clipPath id="c">
			<rect x="10" y="40" width="20" height="20"/>
		</clipPath>
	</defs>
	<line x1="0" y1="50" x2="100" y2="50" stroke="black" stroke-width="10" clip-path="url(#c)"/>
</svg>`
	c := parse(t, svg)
	img := raster(t, c)

	// independent reference: clip rect (SVG x 10..30, y 40..60 -> canvas
	// y 40..60) intersected with the 10-wide stroke band (SVG y 45..55 ->
	// canvas y 45..55): canvas rect x 10..30, y 45..55
	ref := canvas.Rectangle(20, 10).Transform(canvas.Identity.Translate(10, 45))
	want := raster(t, referenceCanvas(100, 100, ref, canvas.Identity, canvas.NonZero))
	assertCoverageMatch(t, img, want)
}

func TestClipRasterRotatedTarget(t *testing.T) {
	// rotated target with objectBoundingBox: the clipped half-box must match
	// the independently constructed and transformed reference geometry
	svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<clipPath id="c" clipPathUnits="objectBoundingBox">
			<rect x="0" y="0" width="0.5" height="1"/>
		</clipPath>
	</defs>
	<rect x="20" y="20" width="60" height="60" fill="red" transform="rotate(45 50 50)" clip-path="url(#c)"/>
</svg>`
	c := parse(t, svg)
	img := raster(t, c)

	// reference: half of the 60x60 box in user space, through the element's
	// transform and the Cartesian IV flip the renderer applies
	targetM := canvas.Identity.ReflectYAbout(50.0).RotateAbout(45.0, 50.0, 50.0)
	refPath := canvas.Rectangle(30, 60).Transform(canvas.Identity.Translate(20, 20))
	want := raster(t, referenceCanvas(100, 100, refPath, targetM, canvas.NonZero))
	assertCoverageMatch(t, img, want)
}

func TestClipRasterText(t *testing.T) {
	// text under a clip is materialized to glyph paths and clipped: a clip
	// covering the whole canvas keeps the glyphs, a tiny clip removes them
	mk := func(clip string) string {
		return `<svg width="100" height="100">
	<defs><clipPath id="c">` + clip + `</clipPath></defs>
	<text x="10" y="50" font-size="20" fill="black" clip-path="url(#c)">Hi</text>
</svg>`
	}
	countPainted := func(img *image.RGBA) int {
		n := 0
		for y := 0; y < 100; y++ {
			for x := 0; x < 100; x++ {
				if img.RGBAAt(x, y).A != 0 {
					n++
				}
			}
		}
		return n
	}

	full := raster(t, parse(t, mk(`<rect x="0" y="0" width="100" height="100"/>`)))
	tiny := raster(t, parse(t, mk(`<rect x="0" y="0" width="2" height="2"/>`)))
	test.That(t, countPainted(full) > 100)
	test.That(t, countPainted(tiny) == 0)
}

func TestClipRasterSameGeometryAcrossTargetSizes(t *testing.T) {
	// identical objectBoundingBox geometry at different target sizes always
	// covers the same fractional region, verified against reference geometry
	mk := func(w, h string) string {
		return `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs><clipPath id="c" clipPathUnits="objectBoundingBox">
		<path d="M0,0 H1 V0.5 H0 Z"/>
	</clipPath></defs>
	<rect x="10" y="10" width="` + w + `" height="` + h + `" fill="red" clip-path="url(#c)"/>
</svg>`
	}
	targetM := canvas.Identity.ReflectYAbout(50.0)
	for _, size := range [][2]string{{"40", "40"}, {"80", "20"}, {"20", "80"}} {
		c := parse(t, mk(size[0], size[1]))
		img := raster(t, c)

		w, _ := strconv.ParseFloat(size[0], 64)
		h, _ := strconv.ParseFloat(size[1], 64)
		// upper SVG half of the target box: height h/2 starting at y=10
		refPath := canvas.Rectangle(w, h/2.0).Transform(canvas.Identity.Translate(10, 10))
		want := raster(t, referenceCanvas(100, 100, refPath, targetM, canvas.NonZero))
		assertCoverageMatch(t, img, want)
	}
}
