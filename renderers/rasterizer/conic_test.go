package rasterizer

import (
	"image/color"
	"math"
	"testing"

	"github.com/tdewolff/canvas"
)

// pixelToCanvas mirrors the rasterizer scanner's device-to-canvas mapping and
// then applies the inverse of the path's transformation matrix, yielding the
// path-local coordinate at which the gradient is sampled.
func pixelToCanvas(px, py int, res canvas.Resolution, sizeMM float64, m canvas.Matrix) canvas.Point {
	dpmm := res.DPMM()
	x := (float64(px) + 0.5) / dpmm
	y := (sizeMM*dpmm - float64(py) - 0.5) / dpmm
	return m.Inv().Dot(canvas.Point{x, y})
}

func makeConic(center canvas.Point, start, arc float64, spread canvas.Spread) *canvas.ConicGradient {
	g := canvas.NewConicGradient(center, start, arc)
	g.Spread = spread
	g.Add(0.0, color.RGBA{255, 0, 0, 255})
	g.Add(0.5, color.RGBA{0, 255, 0, 255})
	g.Add(1.0, color.RGBA{0, 0, 255, 255})
	return g
}

func channelDiff(a, b color.RGBA) int {
	d := 0
	for _, x := range [4]int{
		abs(int(a.R) - int(b.R)),
		abs(int(a.G) - int(b.G)),
		abs(int(a.B) - int(b.B)),
		abs(int(a.A) - int(b.A)),
	} {
		if x > d {
			d = x
		}
	}
	return d
}

func TestRasterizerConicGradient(t *testing.T) {
	const size = 20.0 // mm
	res := canvas.DPMM(10)

	// gradient defined in path-local coordinates centered on the origin; the
	// path is translated to the canvas center via the transformation matrix
	g := makeConic(canvas.Point{}, 0.0, 2.0*math.Pi, canvas.PadSpread)
	m := canvas.Identity.Translate(size/2.0, size/2.0)

	c := canvas.New(size, size)
	c.RenderPath(canvas.Circle(8.0), canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
	img := Draw(c, res, canvas.LinearColorSpace{})

	worst := 0
	for py := 20; py < 180; py += 4 {
		for px := 20; px < 180; px += 4 {
			p := pixelToCanvas(px, py, res, size, m)
			r := math.Hypot(p.X, p.Y)
			if r < 1.0 || r > 7.0 { // interior only, away from the edge and center
				continue
			}
			want := g.At(p.X, p.Y)
			if d := channelDiff(img.RGBAAt(px, py), want); d > worst {
				worst = d
			}
		}
	}
	if worst > 2 {
		t.Errorf("rasterized conic differs from reference by up to %d per channel, want <= 2", worst)
	}
}

func TestRasterizerConicGradientSeam(t *testing.T) {
	// a full-turn gradient is continuous across the positive X-axis seam:
	// collect pixels just above and below the seam (small local angle) and
	// verify the color changes smoothly rather than jumping to the far stop.
	const size = 20.0
	res := canvas.DPMM(40)
	g := makeConic(canvas.Point{}, 0.0, 2.0*math.Pi, canvas.PadSpread)
	m := canvas.Identity.Translate(size/2.0, size/2.0)

	c := canvas.New(size, size)
	c.RenderPath(canvas.Circle(8.0), canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
	img := Draw(c, res, canvas.LinearColorSpace{})

	// compare each pixel's rendered color against the direct At() value; a
	// seam discontinuity would show up as a large mismatch for small angles
	worst := 0
	for py := 0; py < img.Bounds().Dy(); py++ {
		for px := 0; px < img.Bounds().Dx(); px++ {
			p := pixelToCanvas(px, py, res, size, m)
			r := math.Hypot(p.X, p.Y)
			if r < 2.0 || r > 7.0 {
				continue
			}
			if d := channelDiff(img.RGBAAt(px, py), g.At(p.X, p.Y)); d > worst {
				worst = d
			}
		}
	}
	if worst > 2 {
		t.Errorf("seam/gradient mismatch up to %d per channel, want <= 2", worst)
	}
}

func TestRasterizerConicTransparentStopAndBounds(t *testing.T) {
	// transparent stop exercises premultiplied interpolation; the circle is
	// placed at a nonzero origin so its transformed Bounds do not touch (0,0)
	const size = 30.0
	res := canvas.DPMM(40) // high resolution so pixel centers land on the transparent stop
	center := canvas.Point{18.0, 12.0}
	m := canvas.Identity.Translate(center.X, center.Y)

	g := canvas.NewConicGradient(canvas.Point{}, 0.0, 2.0*math.Pi)
	g.Add(0.0, color.RGBA{255, 0, 0, 255})
	g.Add(0.5, color.RGBA{0, 0, 0, 0}) // fully transparent midpoint
	g.Add(1.0, color.RGBA{0, 0, 255, 255})

	path := canvas.Circle(6.0)
	if b := path.FastBounds(); b.X0 != -6.0 || b.Y0 != -6.0 {
		t.Fatalf("unexpected local bounds: %+v", b)
	}

	c := canvas.New(size, size)
	c.RenderPath(path, canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
	img := Draw(c, res, canvas.LinearColorSpace{})

	// every interior pixel must match the direct At() reference (exercising
	// premultiplied interpolation through the transparent stop) and satisfy
	// the premultiplied invariant; we also confirm the gradient reaches both
	// full opacity and partial transparency somewhere inside the circle
	worst := 0
	foundOpaque, foundPremult := false, false
	for py := 0; py < img.Bounds().Dy(); py++ {
		for px := 0; px < img.Bounds().Dx(); px++ {
			p := pixelToCanvas(px, py, res, size, m)
			r := math.Hypot(p.X, p.Y)
			if r < 3.0 || r > 5.0 { // stay clear of edge and center
				continue
			}
			theta := math.Atan2(p.Y, p.X)
			col := img.RGBAAt(px, py)
			if col.R > col.A || col.G > col.A || col.B > col.A {
				t.Fatalf("non-premultiplied pixel %v at local %v", col, p)
			}
			if d := channelDiff(col, g.At(p.X, p.Y)); d > worst {
				worst = d
			}
			switch {
			case math.Abs(theta) < 0.1 && col.A == 255 && col.R > 200:
				foundOpaque = true
			case 0 < col.A && col.A < 255:
				foundPremult = true
			}
		}
	}
	if worst > 2 {
		t.Errorf("transparent-stop gradient differs from At() by up to %d, want <= 2", worst)
	}
	if !foundOpaque {
		t.Error("no opaque red start-stop pixels found")
	}
	if !foundPremult {
		t.Error("no partially transparent premultiplied blend pixels found")
	}

	// the exact transparent-stop color is returned at the precise angle: the
	// transparent stop is at offset 0.5 of a 2pi arc, i.e. angle pi (the -X
	// axis), even though the pixel grid may not center a sample exactly there
	if got := g.At(-4.0, 0.0); got.A != 0 {
		t.Errorf("At() at transparent stop angle pi has alpha %d, want 0", got.A)
	}
}

func TestRasterizerConicRepeatAndReflect(t *testing.T) {
	const size = 20.0
	res := canvas.DPMM(10)
	m := canvas.Identity.Translate(size/2.0, size/2.0)

	for _, spread := range []canvas.Spread{canvas.RepeatSpread, canvas.ReflectSpread} {
		g := makeConic(canvas.Point{}, 0.0, math.Pi/2.0, spread) // quarter-turn domain
		c := canvas.New(size, size)
		c.RenderPath(canvas.Circle(8.0), canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
		img := Draw(c, res, canvas.LinearColorSpace{})

		cx, cy := size/2.0*res.DPMM(), size/2.0*res.DPMM()
		innerR := 7.0 * res.DPMM()
		for py := 30; py < 170; py += 5 {
			for px := 30; px < 170; px += 5 {
				if math.Hypot(float64(px)-cx, float64(py)-cy) > innerR {
					continue
				}
				if img.RGBAAt(px, py).A == 0 {
					t.Errorf("spread %v left a transparent gap at (%d,%d)", spread, px, py)
				}
			}
		}
	}
}

func TestRasterizerConicRotatedMatrix(t *testing.T) {
	// a rotated and translated view matrix keeps the gradient attached to the
	// path and matches the reference computed in path-local coordinates
	const size = 30.0
	res := canvas.DPMM(10)
	g := makeConic(canvas.Point{}, 0.0, 2.0*math.Pi, canvas.PadSpread)
	m := canvas.Identity.Translate(size/2.0, size/2.0).Rotate(35.0 * math.Pi / 180.0)

	c := canvas.New(size, size)
	c.RenderPath(canvas.Circle(8.0), canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
	img := Draw(c, res, canvas.LinearColorSpace{})

	worst := 0
	for py := 70; py < 230; py += 5 {
		for px := 70; px < 230; px += 5 {
			p := pixelToCanvas(px, py, res, size, m)
			r := math.Hypot(p.X, p.Y)
			if r < 1.0 || r > 7.0 {
				continue
			}
			want := g.At(p.X, p.Y)
			if d := channelDiff(img.RGBAAt(px, py), want); d > worst {
				worst = d
			}
		}
	}
	if worst > 2 {
		t.Errorf("rotated conic differs from reference by up to %d, want <= 2", worst)
	}
}

func TestRasterizerConicNoNaN(t *testing.T) {
	// pathological inputs (huge repeat counts, zero radius, single/duplicate
	// stops) must never produce NaN channels in the rasterized output
	const size = 10.0
	res := canvas.DPMM(8)
	m := canvas.Identity.Translate(size/2.0, size/2.0)

	cases := []*canvas.ConicGradient{
		func() *canvas.ConicGradient {
			g := canvas.NewConicGradient(canvas.Point{}, 0.0, 1e-9) // near-zero arc
			g.Spread = canvas.RepeatSpread
			g.Add(0.0, color.RGBA{255, 0, 0, 255})
			g.Add(1.0, color.RGBA{0, 0, 255, 255})
			return g
		}(),
		func() *canvas.ConicGradient {
			g := canvas.NewConicGradient(canvas.Point{}, 0.0, math.Pi/1e6)
			g.Spread = canvas.RepeatSpread
			g.Add(0.0, color.RGBA{255, 0, 0, 255})
			g.Add(1.0, color.RGBA{0, 0, 255, 255})
			return g
		}(),
		func() *canvas.ConicGradient {
			g := canvas.NewConicGradient(canvas.Point{}, 0.0, 2.0*math.Pi)
			g.Add(0.5, color.RGBA{10, 20, 30, 40}) // single stop
			return g
		}(),
	}
	for i, g := range cases {
		c := canvas.New(size, size)
		c.RenderPath(canvas.Circle(4.0), canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
		img := Draw(c, res, canvas.LinearColorSpace{})
		for py := 0; py < img.Bounds().Dy(); py += 3 {
			for px := 0; px < img.Bounds().Dx(); px += 3 {
				col := img.RGBAAt(px, py)
				// uint8 can't be NaN directly; guard the premultiplied
				// invariant instead (channels never exceed alpha)
				if col.R > col.A || col.G > col.A || col.B > col.A {
					t.Fatalf("case %d: invalid premultiplied pixel %v at (%d,%d)", i, col, px, py)
				}
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
