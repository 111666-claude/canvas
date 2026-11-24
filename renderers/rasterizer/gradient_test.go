package rasterizer

import (
	"image/color"
	"testing"

	"github.com/tdewolff/canvas"
)

// renderGradient fills a w x h (mm) rectangle with the given gradient and
// returns the rasterized image at one pixel per millimeter.
func renderGradient(t *testing.T, w, h float64, gradient canvas.Gradient, colorSpace canvas.ColorSpace) *color.RGBA {
	t.Helper()
	res := canvas.Resolution(1.0) // 1 px/mm
	r := New(w, h, res, colorSpace)
	path := canvas.Rectangle(w, h)
	r.RenderPath(path, canvas.Style{Fill: canvas.Paint{Gradient: gradient}}, canvas.Identity)
	r.Close()
	img := r.Image.(interface {
		RGBAAt(x, y int) color.RGBA
	})
	c := img.RGBAAt(int(w/2), int(h/2))
	return &c
}

// TestRasterizerGradientSemantics verifies that the pixels produced by the
// rasterizer match the shared Gradient.At sampling semantics for every
// interpolation/alpha combination, for both the default (linear) color space
// and the sRGB color space pipeline.
func TestRasterizerGradientSemantics(t *testing.T) {
	const w, h = 10.0, 10.0
	red := color.RGBA{255, 0, 0, 200}
	blue := color.RGBA{0, 0, 255, 100}

	configs := []canvas.GradientOptions{
		{Interpolation: canvas.GradientSRGB, Alpha: canvas.GradientPremultiplied},
		{Interpolation: canvas.GradientSRGB, Alpha: canvas.GradientStraight},
		{Interpolation: canvas.GradientLinearLight, Alpha: canvas.GradientPremultiplied},
		{Interpolation: canvas.GradientLinearLight, Alpha: canvas.GradientStraight},
	}
	colorSpaces := []canvas.ColorSpace{canvas.LinearColorSpace{}, canvas.SRGBColorSpace{}}

	for _, opts := range configs {
		for _, cs := range colorSpaces {
			gradient := canvas.NewLinearGradient(canvas.Point{0, h / 2}, canvas.Point{w, h / 2})
			gradient.GradientOptions = opts
			gradient.Add(0.0, red)
			gradient.Add(1.0, blue)

			got := renderGradient(t, w, h, gradient, cs)

			// the rasterizer samples the transformed pixel center (x+0.5, y+0.5);
			// ask the gradient directly at the same canvas coordinate. With a
			// non-linear color space the rasterizer gamma-compresses the blended
			// result on Close, so apply the matching FromLinear step here.
			want := gradient.At(w/2+0.5, h/2)
			if _, linear := cs.(canvas.LinearColorSpace); !linear {
				want = cs.FromLinear(want)
			}
			if *got != want {
				t.Errorf("opts=%+v cs=%T: rasterized %v != pipeline %v", opts, cs, *got, want)
			}
			if got.R > got.A || got.G > got.A || got.B > got.A {
				t.Errorf("opts=%+v cs=%T: premultiplied invariant violated: %v", opts, cs, got)
			}
		}
	}
}

// TestRasterizerGradientSpread renders repeat and reflect gradients and
// confirms pixels outside the geometric start/end follow the spread function
// rather than being clamped.
func TestRasterizerGradientSpread(t *testing.T) {
	const w, h = 10.0, 2.0
	makeGrad := func(spread canvas.GradientSpread) *canvas.LinearGradient {
		// gradient only spans the middle half of the rectangle
		g := canvas.NewLinearGradient(canvas.Point{w * 0.25, h / 2}, canvas.Point{w * 0.75, h / 2})
		g.Spread = spread
		g.Add(0.0, color.RGBA{255, 0, 0, 255})
		g.Add(1.0, color.RGBA{0, 0, 255, 255})
		return g
	}

	pad := makeGrad(canvas.GradientPad)
	repeat := makeGrad(canvas.GradientRepeat)
	reflect := makeGrad(canvas.GradientReflect)

	// sample near the left edge, which lies before the gradient start
	probe := func(g canvas.Gradient) color.RGBA {
		res := canvas.Resolution(1.0)
		r := New(w, h, res, canvas.LinearColorSpace{})
		r.RenderPath(canvas.Rectangle(w, h), canvas.Style{Fill: canvas.Paint{Gradient: g}}, canvas.Identity)
		r.Close()
		return r.Image.(interface {
			RGBAAt(x, y int) color.RGBA
		}).RGBAAt(0, 1)
	}

	padPixel := probe(pad)
	repeatPixel := probe(repeat)
	reflectPixel := probe(reflect)

	// pad before the start is the solid first stop
	if padPixel != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("pad edge = %v, want solid red", padPixel)
	}
	// repeat/reflect must not be the clamped first stop: they continue the
	// pattern, so they differ from pad at this coordinate
	if repeatPixel == padPixel {
		t.Errorf("repeat edge %v must differ from pad %v", repeatPixel, padPixel)
	}
	if reflectPixel == padPixel {
		t.Errorf("reflect edge %v must differ from pad %v", reflectPixel, padPixel)
	}
}

// TestRasterizerEmptyAndTransparent ensures empty gradients render nothing and
// transparent endpoints never write color into the destination.
func TestRasterizerEmptyAndTransparent(t *testing.T) {
	const w, h = 4.0, 4.0
	res := canvas.Resolution(1.0)

	// empty gradient must not paint anything (stays zero-valued/transparent)
	r := New(w, h, res, canvas.LinearColorSpace{})
	empty := canvas.NewLinearGradient(canvas.Point{0, 0}, canvas.Point{w, 0})
	r.RenderPath(canvas.Rectangle(w, h), canvas.Style{Fill: canvas.Paint{Gradient: empty}}, canvas.Identity)
	r.Close()
	if c := r.Image.(interface {
		RGBAAt(x, y int) color.RGBA
	}).RGBAAt(2, 2); c.A != 0 {
		t.Errorf("empty gradient painted alpha %d", c.A)
	}

	// gradient starting fully transparent must leave its first-stop region
	// clear. The first gradient stop spans [0,2mm], so an interior pixel whose
	// center lies within that region must be fully transparent (no color leak).
	r2 := New(w, h, res, canvas.LinearColorSpace{})
	transparent := canvas.NewLinearGradient(canvas.Point{0, 0}, canvas.Point{w, 0})
	transparent.Add(0.0, color.RGBA{0, 0, 0, 0})
	transparent.Add(0.5, color.RGBA{0, 0, 0, 0})
	transparent.Add(1.0, color.RGBA{255, 0, 0, 255})
	r2.RenderPath(canvas.Rectangle(w, h), canvas.Style{Fill: canvas.Paint{Gradient: transparent}}, canvas.Identity)
	r2.Close()
	// pixel (1,2) center is at x=1.5mm, inside the transparent [0,2mm] region
	if c := r2.Image.(interface {
		RGBAAt(x, y int) color.RGBA
	}).RGBAAt(1, 2); c.A != 0 || c.R != 0 {
		t.Errorf("transparent start painted %v", c)
	}
}
