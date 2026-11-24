package canvas_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	svgr "github.com/tdewolff/canvas/renderers/svg"
	"github.com/tdewolff/test"
)

// rasterizeSVG parses an SVG and rasterizes it at 1 pixel per user unit, so that image pixel
// (x, y) corresponds to SVG user coordinate (x, y).
func rasterizeSVG(t *testing.T, s string) (*canvas.Canvas, *image.RGBA) {
	t.Helper()
	c, err := canvas.ParseSVG(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return c, rasterizer.Draw(c, canvas.DPMM(1.0), canvas.DefaultColorSpace)
}

func alphaAt(img *image.RGBA, x, y int) int {
	return int(img.RGBAAt(x, y).A)
}

func inRange(t *testing.T, got, min, max int, msg string) {
	t.Helper()
	if got < min || max < got {
		t.Fatalf("%s: got %d, want in [%d, %d]", msg, got, min, max)
	}
}

// TestSVGMaskSoftEdgeCircle tests that a radial gradient in a mask gives soft (anti-aliased
// transparency) edges instead of a hard clip.
func TestSVGMaskSoftEdgeCircle(t *testing.T) {
	s := `<svg width="100" height="100">
	<defs>
		<radialGradient id="fade">
			<stop offset="0" stop-color="#ffffff" stop-opacity="1"/>
			<stop offset="1" stop-color="#ffffff" stop-opacity="0"/>
		</radialGradient>
		<mask id="soft" mask-type="alpha">
			<circle cx="50" cy="50" r="40" fill="url(#fade)"/>
		</mask>
	</defs>
	<rect width="100" height="100" fill="#ff0000" mask="url(#soft)"/>
</svg>`
	_, img := rasterizeSVG(t, s)

	// center is fully opaque
	inRange(t, alphaAt(img, 50, 50), 240, 255, "center alpha")
	// halfway to the edge the alpha is about half: a soft edge, not a hard one
	inRange(t, alphaAt(img, 50, 70), 90, 170, "mid alpha")
	// near the circle edge the alpha fades to zero
	inRange(t, alphaAt(img, 50, 89), 0, 20, "edge alpha")
	// outside the circle and in the corners nothing is drawn
	inRange(t, alphaAt(img, 50, 91), 0, 2, "outside alpha")
	inRange(t, alphaAt(img, 2, 2), 0, 2, "corner alpha")

	// the remaining color is the red fill, premultiplied by the mask
	mid := img.RGBAAt(50, 70)
	inRange(t, int(mid.R), int(mid.A)-3, int(mid.A)+3, "mid red premultiplied")
	inRange(t, int(mid.G), 0, 3, "mid green")
	inRange(t, int(mid.B), 0, 3, "mid blue")
}

// TestSVGMaskLuminanceVsAlpha tests both mask-type modes: luminance uses the color's luminance
// times its alpha, alpha uses only the alpha channel.
func TestSVGMaskLuminanceVsAlpha(t *testing.T) {
	s := `<svg width="100" height="100">
	<defs>
		<mask id="lum">
			<rect width="100" height="100" fill="lime"/>
		</mask>
		<mask id="alp" mask-type="alpha">
			<rect width="100" height="100" fill="lime"/>
		</mask>
	</defs>
	<rect width="100" height="50" fill="red" mask="url(#lum)"/>
	<rect y="50" width="100" height="50" fill="red" mask="url(#alp)"/>
</svg>`
	_, img := rasterizeSVG(t, s)

	// luminance of lime (0,255,0) is 0.7152, so alpha is about 182
	inRange(t, alphaAt(img, 50, 25), 174, 190, "luminance mask alpha")
	// alpha mask ignores the color and keeps full opacity
	inRange(t, alphaAt(img, 50, 75), 250, 255, "alpha mask alpha")
}

// TestSVGMaskSemiTransparentImage tests that a semi-transparent image inside a mask modulates
// the masked content's opacity.
func TestSVGMaskSemiTransparentImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			src.SetRGBA(x, y, color.RGBA{255, 255, 255, 128})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	s := `<svg width="100" height="100">
	<defs>
		<mask id="im" mask-type="alpha">
			<image x="0" y="0" width="100" height="100" href="` + uri + `"/>
		</mask>
	</defs>
	<rect width="100" height="100" fill="red" mask="url(#im)"/>
</svg>`
	_, img := rasterizeSVG(t, s)
	inRange(t, alphaAt(img, 50, 50), 118, 138, "image mask alpha")
	inRange(t, alphaAt(img, 10, 10), 118, 138, "image mask alpha corner")
}

// TestSVGMaskObjectBoundingBox tests maskContentUnits="objectBoundingBox" on an element with a
// non-zero bounds offset, i.e. the mask content is aligned to the element's bounding box.
func TestSVGMaskObjectBoundingBox(t *testing.T) {
	s := `<svg width="100" height="100">
	<defs>
		<mask id="obb" maskContentUnits="objectBoundingBox" mask-type="alpha">
			<rect x="0" y="0" width="0.5" height="1" fill="white"/>
		</mask>
	</defs>
	<rect x="40" y="40" width="20" height="20" fill="red" mask="url(#obb)"/>
</svg>`
	_, img := rasterizeSVG(t, s)

	// left half of the target rectangle is visible
	inRange(t, alphaAt(img, 45, 50), 240, 255, "left half alpha")
	// right half is masked out
	inRange(t, alphaAt(img, 55, 50), 0, 10, "right half alpha")
	// outside the target rectangle nothing is drawn
	inRange(t, alphaAt(img, 45, 30), 0, 2, "above target alpha")
	inRange(t, alphaAt(img, 30, 50), 0, 2, "left of target alpha")
}

// TestSVGMaskNested tests a mask referenced from within another mask's content.
func TestSVGMaskNested(t *testing.T) {
	s := `<svg width="100" height="100">
	<defs>
		<mask id="inner" mask-type="alpha">
			<rect width="100" height="100" fill="white" fill-opacity="0.5"/>
		</mask>
		<mask id="outer" mask-type="alpha">
			<rect width="100" height="100" fill="white" mask="url(#inner)"/>
		</mask>
	</defs>
	<rect width="100" height="100" fill="red" mask="url(#outer)"/>
</svg>`
	_, img := rasterizeSVG(t, s)
	inRange(t, alphaAt(img, 50, 50), 112, 144, "nested mask alpha")
}

// TestSVGMaskGroupChaining tests that a mask on a group multiplies with a mask on the element.
func TestSVGMaskGroupChaining(t *testing.T) {
	s := `<svg width="100" height="100">
	<defs>
		<mask id="half" mask-type="alpha">
			<rect width="100" height="100" fill="white" fill-opacity="0.5"/>
		</mask>
	</defs>
	<g mask="url(#half)">
		<rect width="100" height="100" fill="red" mask="url(#half)"/>
	</g>
</svg>`
	_, img := rasterizeSVG(t, s)
	// 0.5 * 0.5 = 0.25, so alpha is about 64
	inRange(t, alphaAt(img, 50, 50), 56, 72, "chained mask alpha")
}

// TestSVGMaskTransform tests that transforms on the masked element and inside the mask content
// are both respected.
func TestSVGMaskTransform(t *testing.T) {
	s := `<svg width="100" height="100">
	<defs>
		<mask id="m" mask-type="alpha">
			<rect x="0" y="0" width="50" height="50" fill="white" transform="translate(10,10)"/>
		</mask>
	</defs>
	<g transform="translate(20,0)">
		<rect width="100" height="100" fill="red" mask="url(#m)"/>
	</g>
</svg>`
	_, img := rasterizeSVG(t, s)
	// mask content rect is at user (10,10)-(60,60), the group shifts the user space by (20,0),
	// so the visible region is (30,10)-(80,60)
	inRange(t, alphaAt(img, 40, 40), 240, 255, "inside transformed mask")
	inRange(t, alphaAt(img, 15, 40), 0, 2, "left of transformed mask")
	inRange(t, alphaAt(img, 40, 80), 0, 2, "below transformed mask")
}

// TestSVGMaskErrors tests that invalid masks return contextual errors or safe empty results.
func TestSVGMaskErrors(t *testing.T) {
	var errs = []struct {
		name string
		svg  string
		err  string
	}{
		{
			"circular reference",
			`<svg width="10" height="10"><defs><mask id="a"><rect width="10" height="10" mask="url(#a)"/></mask></defs><rect width="10" height="10" mask="url(#a)"/></svg>`,
			"circular mask reference",
		},
		{
			"invalid maskUnits",
			`<svg width="10" height="10"><defs><mask id="m" maskUnits="pixels"><rect width="10" height="10"/></mask></defs><rect width="10" height="10" mask="url(#m)"/></svg>`,
			"invalid maskUnits",
		},
		{
			"invalid maskContentUnits",
			`<svg width="10" height="10"><defs><mask id="m" maskContentUnits="pixels"><rect width="10" height="10"/></mask></defs><rect width="10" height="10" mask="url(#m)"/></svg>`,
			"invalid maskContentUnits",
		},
		{
			"invalid mask-type",
			`<svg width="10" height="10"><defs><mask id="m" mask-type="foo"><rect width="10" height="10"/></mask></defs><rect width="10" height="10" mask="url(#m)"/></svg>`,
			"invalid mask-type",
		},
		{
			"illegal dimension unit",
			`<svg width="10" height="10"><defs><mask id="m" maskUnits="userSpaceOnUse" width="10foo" height="10"><rect width="10" height="10"/></mask></defs><rect width="10" height="10" mask="url(#m)"/></svg>`,
			"unknown dimension",
		},
		{
			"non-finite transform",
			`<svg width="10" height="10"><defs><mask id="m"><rect width="10" height="10"/></mask></defs><rect width="10" height="10" transform="scale(nan)" mask="url(#m)"/></svg>`,
			"non-finite transform",
		},
		{
			"oversized mask",
			`<svg width="10" height="10"><defs><mask id="m" maskUnits="userSpaceOnUse" width="1e9" height="1e9"><rect width="10" height="10"/></mask></defs><rect width="10" height="10" mask="url(#m)"/></svg>`,
			"mask region too large",
		},
	}
	for _, tt := range errs {
		t.Run(tt.name, func(t *testing.T) {
			_, err := canvas.ParseSVG(strings.NewReader(tt.svg))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.err)
			}
			if !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("expected error containing %q, got %q", tt.err, err.Error())
			}
		})
	}

	var empties = []struct {
		name string
		svg  string
	}{
		{
			"negative size",
			`<svg width="10" height="10"><defs><mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="-10" height="10"><rect width="10" height="10"/></mask></defs><rect width="10" height="10" fill="red" mask="url(#m)"/></svg>`,
		},
		{
			"zero size",
			`<svg width="10" height="10"><defs><mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="0" height="10"><rect width="10" height="10"/></mask></defs><rect width="10" height="10" fill="red" mask="url(#m)"/></svg>`,
		},
		{
			"empty mask",
			`<svg width="10" height="10"><defs><mask id="m"></mask></defs><rect width="10" height="10" fill="red" mask="url(#m)"/></svg>`,
		},
	}
	for _, tt := range empties {
		t.Run(tt.name, func(t *testing.T) {
			c, err := canvas.ParseSVG(strings.NewReader(tt.svg))
			test.Error(t, err) // safe empty result, not an error
			img := rasterizer.Draw(c, canvas.DPMM(1.0), canvas.DefaultColorSpace)
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					if a := img.RGBAAt(x, y).A; a != 0 {
						t.Fatalf("expected fully transparent result, got alpha %d at (%d,%d)", a, x, y)
					}
				}
			}
		})
	}
}

// TestSVGMaskSVGOutput tests that masks are written out as native SVG mask elements and that
// the written SVG renders the same pixels when parsed and rasterized again.
func TestSVGMaskSVGOutput(t *testing.T) {
	s := `<svg width="100" height="100">
	<defs>
		<radialGradient id="fade">
			<stop offset="0" stop-color="#ffffff" stop-opacity="1"/>
			<stop offset="1" stop-color="#ffffff" stop-opacity="0"/>
		</radialGradient>
		<mask id="soft" mask-type="alpha">
			<circle cx="50" cy="50" r="40" fill="url(#fade)"/>
		</mask>
	</defs>
	<rect width="100" height="100" fill="#ff0000" mask="url(#soft)"/>
</svg>`
	c, img := rasterizeSVG(t, s)

	var buf bytes.Buffer
	w := svgr.New(&buf, c.W, c.H, &svgr.Options{})
	c.RenderTo(w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "<mask ") || !strings.Contains(out, `mask="url(#`) {
		t.Fatalf("expected SVG output to contain a mask element and reference, got:\n%s", out)
	}

	// re-parse the written SVG (defs come after the masked element here, testing deferred
	// mask application) and compare pixels
	c2, err := canvas.ParseSVG(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	img2 := rasterizer.Draw(c2, canvas.DPMM(1.0), canvas.DefaultColorSpace)
	if img2.Bounds() != img.Bounds() {
		t.Fatalf("bounds differ: %v != %v", img2.Bounds(), img.Bounds())
	}
	for _, p := range []image.Point{{50, 50}, {50, 70}, {50, 89}, {2, 2}, {25, 25}} {
		a1 := alphaAt(img, p.X, p.Y)
		a2 := alphaAt(img2, p.X, p.Y)
		if d := a1 - a2; d < -8 || 8 < d {
			t.Fatalf("alpha at %v differs: %d != %d", p, a1, a2)
		}
	}
}

// TestSVGMaskText tests that text inside a mask is rendered as part of the mask content.
func TestSVGMaskText(t *testing.T) {
	family := canvas.NewFontFamily("serif")
	if err := family.LoadSystemFont("serif", canvas.FontRegular); err != nil {
		t.Skip("no system serif font available")
	}

	s := `<svg width="100" height="100">
	<defs>
		<mask id="tm" mask-type="alpha">
			<text x="10" y="60" font-size="50" font-family="serif">H</text>
		</mask>
	</defs>
	<rect width="100" height="100" fill="red" mask="url(#tm)"/>
</svg>`
	_, img := rasterizeSVG(t, s)

	// the glyph covers only part of the canvas
	opaque := 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			if 200 < alphaAt(img, x, y) {
				opaque++
			}
		}
	}
	if opaque == 0 {
		t.Fatal("expected text in mask to be visible")
	}
	if 5000 < opaque {
		t.Fatalf("expected text to cover only part of the canvas, got %d opaque pixels", opaque)
	}
}
