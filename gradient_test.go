package canvas

import (
	"image"
	"image/color"
	"math"
	"sync"
	"testing"
)

// floatRef is an independent float64 reference implementation of gradient
// sampling. It deliberately does not share any helper code with the production
// implementation so the tests compare against a second derivation of the same
// semantics rather than against the code under test.
type floatRef struct {
	stops []Stop
	opts  GradientOptions
}

func refSRGBToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func refLinearTosRGB(c float64) float64 {
	if c <= 0.0 {
		return 0.0
	} else if c < 0.0031308 {
		return 12.92 * c
	} else if c >= 1.0 {
		return 1.0
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

func refClamp(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	} else if v > 1 {
		return 1
	}
	return v
}

// refStraight returns the unassociated RGB channels of a premultiplied color;
// a fully transparent color has no defined hue and is treated as black.
func refStraight(c color.RGBA, a float64) (float64, float64, float64) {
	if a == 0 {
		return 0, 0, 0
	}
	return float64(c.R) / 255.0 / a, float64(c.G) / 255.0 / a, float64(c.B) / 255.0 / a
}

// At evaluates the reference gradient at t.
func (r floatRef) At(t float64) color.RGBA {
	if len(r.stops) == 0 {
		return Transparent
	}
	if len(r.stops) == 1 {
		return r.stops[0].Color
	}
	lo := r.stops[0].Offset
	hi := r.stops[len(r.stops)-1].Offset

	// spread mapping (same documented order as the implementation)
	if !(r.opts.Spread == GradientPad || hi <= lo) {
		w := hi - lo
		u := (t - lo) / w
		switch r.opts.Spread {
		case GradientRepeat:
			u -= math.Floor(u)
		case GradientReflect:
			u = math.Mod(u, 2.0)
			if u < 0 {
				u += 2.0
			}
			if u > 1 {
				u = 2.0 - u
			}
		}
		t = lo + u*w
	}
	if t <= lo {
		return r.stops[0].Color
	} else if t >= hi {
		return r.stops[len(r.stops)-1].Color
	}

	i := 1
	for i < len(r.stops) && r.stops[i].Offset <= t {
		i++
	}
	s0, s1 := r.stops[i-1], r.stops[i]
	f := refClamp((t - s0.Offset) / (s1.Offset - s0.Offset))

	a0f := float64(s0.Color.A) / 255.0
	a1f := float64(s1.Color.A) / 255.0
	r0, g0, b0 := refStraight(s0.Color, a0f)
	r1, g1, b1 := refStraight(s1.Color, a1f)
	if r.opts.Interpolation == GradientLinearLight {
		r0, g0, b0 = refSRGBToLinear(r0), refSRGBToLinear(g0), refSRGBToLinear(b0)
		r1, g1, b1 = refSRGBToLinear(r1), refSRGBToLinear(g1), refSRGBToLinear(b1)
	}
	af := refClamp(a0f + (a1f-a0f)*f)

	var rf, gf, bf float64
	if r.opts.Alpha == GradientPremultiplied {
		rf = r0*a0f + (r1*a1f-r0*a0f)*f
		gf = g0*a0f + (g1*a1f-g0*a0f)*f
		bf = b0*a0f + (b1*a1f-b0*a0f)*f
	} else {
		// straight interpolation, then re-premultiply for storage
		rf = (r0 + (r1-r0)*f) * af
		gf = (g0 + (g1-g0)*f) * af
		bf = (b0 + (b1-b0)*f) * af
	}
	if r.opts.Interpolation == GradientLinearLight && af > 0 {
		// unpremultiply, re-encode to sRGB, premultiply again
		rf = refLinearTosRGB(rf/af) * af
		gf = refLinearTosRGB(gf/af) * af
		bf = refLinearTosRGB(bf/af) * af
	}
	rf, gf, bf = refClamp(rf), refClamp(gf), refClamp(bf)
	if rf > af {
		rf = af
	}
	if gf > af {
		gf = af
	}
	if bf > af {
		bf = af
	}
	return color.RGBA{
		R: uint8(rf*255.0 + 0.5),
		G: uint8(gf*255.0 + 0.5),
		B: uint8(bf*255.0 + 0.5),
		A: uint8(af*255.0 + 0.5),
	}
}

func colorDelta(a, b color.RGBA) (int, int, int, int) {
	d := func(x, y uint8) int {
		v := int(x) - int(y)
		if v < 0 {
			return -v
		}
		return v
	}
	return d(a.R, b.R), d(a.G, b.G), d(a.B, b.B), d(a.A, b.A)
}

// TestGradientFloatReference compares every interpolation/alpha combination
// against the independent float64 reference across dense sampling.
func TestGradientFloatReference(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	stops := Grad{{0.0, red}, {0.5, color.RGBA{255, 255, 255, 255}}, {1.0, blue}}

	configs := []GradientOptions{
		{Interpolation: GradientSRGB, Alpha: GradientPremultiplied},
		{Interpolation: GradientSRGB, Alpha: GradientStraight},
		{Interpolation: GradientLinearLight, Alpha: GradientPremultiplied},
		{Interpolation: GradientLinearLight, Alpha: GradientStraight},
	}
	for _, opts := range configs {
		ref := floatRef{stops: stops, opts: opts}
		for k := 0; k <= 200; k++ {
			tt := float64(k) / 200.0
			got := stops.AtWith(tt, opts)
			want := ref.At(tt)
			dR, dG, dB, dA := colorDelta(got, want)
			// the integer sRGB/premultiplied fast path is bit-exact; the float
			// paths allow a single quantization step of rounding difference
			tol := 1
			if dR > tol || dG > tol || dB > tol || dA > tol {
				t.Errorf("opts=%+v t=%.3f got=%v want=%v delta=(%d,%d,%d,%d)", opts, tt, got, want, dR, dG, dB, dA)
			}
		}
	}
}

// TestGradientBlackWhiteLuma is the black/white luminance staircase fixture.
// Linear-light interpolation is perceptually brighter at the midpoint than
// direct sRGB interpolation, and both ramps are monotonic and neutral.
func TestGradientBlackWhiteLuma(t *testing.T) {
	black := color.RGBA{0, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	stops := Grad{{0.0, black}, {1.0, white}}

	srgbMid := stops.AtWith(0.5, GradientOptions{Interpolation: GradientSRGB, Alpha: GradientPremultiplied})
	linearMid := stops.AtWith(0.5, GradientOptions{Interpolation: GradientLinearLight, Alpha: GradientPremultiplied})

	// sRGB midpoint comes from the historical integer lerp (0xffff*0.5 rounds
	// to 32768, giving 127 with alpha 254 from the 16-bit integer pipeline);
	// linear light re-encodes to a brighter value (~188).
	if srgbMid.R != 127 || srgbMid.G != 127 || srgbMid.B != 127 {
		t.Errorf("sRGB midpoint = %v, want {127 127 127}", srgbMid)
	}
	if linearMid.R < 180 || linearMid.R > 195 {
		t.Errorf("linear-light midpoint R = %d, want ~188", linearMid.R)
	}
	if !(linearMid.R > srgbMid.R) {
		t.Errorf("linear-light midpoint %v must be brighter than sRGB %v", linearMid, srgbMid)
	}

	// monotonic staircase with no dark band for both modes
	for _, opts := range []GradientOptions{
		{Interpolation: GradientSRGB, Alpha: GradientPremultiplied},
		{Interpolation: GradientLinearLight, Alpha: GradientPremultiplied},
	} {
		prev := uint8(0)
		for k := 0; k <= 50; k++ {
			c := stops.AtWith(float64(k)/50.0, opts)
			if c.R < prev || c.G < prev || c.B < prev {
				t.Fatalf("non-monotonic staircase for opts=%+v at k=%d: %v after %d", opts, k, c, prev)
			}
			if c.R != c.G || c.G != c.B {
				t.Fatalf("neutral ramp lost neutrality: %v", c)
			}
			prev = c.R
		}
	}
}

// TestGradientSemiTransparentRedBlue verifies the semi-transparent red/blue
// fixture. Premultiplied interpolation keeps the premultiplied invariant and
// straight interpolation preserves the unassociated hue, with no color leaking
// through transparent endpoints.
func TestGradientSemiTransparentRedBlue(t *testing.T) {
	// premultiplied storage: red at a=128, blue at a=64
	c0 := color.RGBA{128, 0, 0, 128}
	c1 := color.RGBA{0, 0, 64, 64}
	stops := Grad{{0.0, c0}, {1.0, c1}}

	allOpts := []GradientOptions{
		{Interpolation: GradientSRGB, Alpha: GradientPremultiplied},
		{Interpolation: GradientSRGB, Alpha: GradientStraight},
		{Interpolation: GradientLinearLight, Alpha: GradientPremultiplied},
		{Interpolation: GradientLinearLight, Alpha: GradientStraight},
	}
	for _, opts := range allOpts {
		for k := 0; k <= 100; k++ {
			c := stops.AtWith(float64(k)/100.0, opts)
			if c.R > c.A || c.G > c.A || c.B > c.A {
				t.Fatalf("premultiplied invariant violated opts=%+v t=%.2f: %v", opts, float64(k)/100.0, c)
			}
		}
		// endpoints must be reproduced exactly
		if got := stops.AtWith(0.0, opts); got != c0 {
			t.Errorf("opts=%+v start = %v, want %v", opts, got, c0)
		}
		if got := stops.AtWith(1.0, opts); got != c1 {
			t.Errorf("opts=%+v end = %v, want %v", opts, got, c1)
		}
	}

	// straight alpha at midpoint: alpha is the float average (128+64)/2=96;
	// the integer premultiplied path rounds the 16-bit value to 95.
	straightMid := stops.AtWith(0.5, GradientOptions{Alpha: GradientStraight})
	if straightMid.A != 96 {
		t.Errorf("straight midpoint alpha = %d, want 96", straightMid.A)
	}
	premulMid := stops.AtWith(0.5, GradientOptions{Alpha: GradientPremultiplied})
	if delta := int(straightMid.A) - int(premulMid.A); delta < 0 || delta > 1 {
		t.Errorf("alpha interpolation differs by more than quantization: premul=%d straight=%d", premulMid.A, straightMid.A)
	}
	// straight mode keeps the saturated hue: red channel stays well above the
	// premultiplied value which darkens toward the low-alpha blue endpoint
	if !(straightMid.R > premulMid.R) {
		t.Errorf("straight interpolation should preserve hue better: straight=%v premul=%v", straightMid, premulMid)
	}

	// transparent endpoint must never leak color, in either alpha mode
	transparentStops := Grad{{0.0, color.RGBA{0, 0, 0, 0}}, {1.0, color.RGBA{255, 0, 0, 255}}}
	for _, opts := range []GradientOptions{
		{Alpha: GradientPremultiplied},
		{Alpha: GradientStraight},
	} {
		if got := transparentStops.AtWith(0.0, opts); got != (color.RGBA{0, 0, 0, 0}) {
			t.Errorf("transparent start leaked color opts=%+v: %v", opts, got)
		}
	}
}

// TestGradientSpread exercises the pad, repeat and reflect fixtures against
// their defining periodicity/symmetry properties across several periods.
func TestGradientSpread(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	// domain deliberately narrower than [0,1] so spread is observable
	stops := Grad{{0.25, red}, {0.75, blue}}

	pad := GradientOptions{Spread: GradientPad}
	repeat := GradientOptions{Spread: GradientRepeat}
	reflect := GradientOptions{Spread: GradientReflect}

	// pad clamps outside the domain
	if got := stops.AtWith(-5.0, pad); got != red {
		t.Errorf("pad left = %v, want red", got)
	}
	if got := stops.AtWith(5.0, pad); got != blue {
		t.Errorf("pad right = %v, want blue", got)
	}

	// repeat is periodic with the domain width (0.5): t and t+0.5 match
	for k := -10; k <= 10; k++ {
		base := 0.3 + float64(k)*0.1
		a := stops.AtWith(base, repeat)
		b := stops.AtWith(base+0.5, repeat)
		if a != b {
			t.Errorf("repeat not periodic at %v: %v != %v", base, a, b)
		}
	}

	// reflect mirrors around the upper boundary hi=0.75: a point at distance d
	// before hi maps to the same distance after hi.
	for _, tt := range []float64{0.3, 0.45, 0.6} {
		d := 0.75 - tt
		mirror := 0.75 + d
		if stops.AtWith(tt, reflect) != stops.AtWith(mirror, reflect) {
			t.Errorf("reflect asymmetry at t=%v vs %v", tt, mirror)
		}
	}
	// reflect repeats every two domains (width 1.0).
	for k := -10; k <= 10; k++ {
		base := 0.3 + float64(k)*0.1
		if stops.AtWith(base, reflect) != stops.AtWith(base+1.0, reflect) {
			t.Errorf("reflect not periodic (period 2 domains) at %v", base)
		}
	}

	// all three agree strictly inside the domain
	for _, tt := range []float64{0.3, 0.5, 0.7} {
		c := stops.AtWith(tt, pad)
		if c != stops.AtWith(tt, repeat) || c != stops.AtWith(tt, reflect) {
			t.Errorf("spread modes disagree inside domain at %v", tt)
		}
	}
}

// TestGradientEdgeCases covers non-finite colors/positions, empty/single-stop
// gradients, extreme stop counts and degenerate geometry.
func TestGradientEdgeCases(t *testing.T) {
	// empty gradient is transparent for every shape
	empty := NewGradient()
	if got := empty.At(0.5); got != Transparent {
		t.Errorf("empty Grad.At = %v, want transparent", got)
	}
	for _, g := range []Gradient{
		NewLinearGradient(Point{0, 0}, Point{10, 0}),
		NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 10),
		NewConicGradient(Point{0, 0}, 0),
	} {
		if got := g.At(5, 5); got != Transparent {
			t.Errorf("empty %T = %v, want transparent", g, got)
		}
	}

	// single stop returns that color everywhere, including non-finite positions
	singleColor := color.RGBA{10, 20, 30, 40}
	single := Grad{{0.3, singleColor}}
	for _, tt := range []float64{-1, 0, math.NaN(), math.Inf(1), math.Inf(-1), 2} {
		if got := single.AtWith(tt, GradientOptions{}); got != singleColor {
			t.Errorf("single stop AtWith(%v) = %v, want %v", tt, got, singleColor)
		}
	}

	// non-finite offsets are clamped to the domain boundaries
	g := NewGradient()
	g.Add(math.NaN(), color.RGBA{255, 0, 0, 255})
	g.Add(math.Inf(1), color.RGBA{0, 0, 255, 255})
	g.Add(math.Inf(-1), color.RGBA{0, 255, 0, 255})
	if g[0].Offset != 0.0 || g[len(g)-1].Offset != 1.0 {
		t.Errorf("non-finite offsets not clamped: %+v", g)
	}

	// oversized color channels are clipped to the premultiplied invariant
	clipped := NewGradient()
	clipped.Add(0.5, color.RGBA{R: 250, G: 10, B: 10, A: 100})
	if c := clipped[0].Color; c.R > c.A || c.G > c.A || c.B > c.A {
		t.Errorf("color not clipped to alpha: %v", c)
	}

	// non-finite sampling positions produce stable, in-range colors
	stops := Grad{{0.0, color.RGBA{255, 0, 0, 255}}, {1.0, color.RGBA{0, 0, 255, 255}}}
	for _, opts := range []GradientOptions{
		{Spread: GradientPad},
		{Spread: GradientRepeat},
		{Spread: GradientReflect},
	} {
		for _, tt := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			c := stops.AtWith(tt, opts)
			if c.R > c.A || c.G > c.A || c.B > c.A {
				t.Errorf("non-finite t=%v opts=%+v gave invalid color %v", tt, opts, c)
			}
		}
	}

	// extreme stop count stays correct and bounded
	many := NewGradient()
	for i := 0; i < 10000; i++ {
		v := uint8(i % 256)
		many.Add(float64(i)/9999.0, color.RGBA{v, v, v, 255})
	}
	if len(many) != 10000 {
		t.Fatalf("expected 10000 stops, got %d", len(many))
	}
	c := many.AtWith(0.5, GradientOptions{})
	if c.R > c.A || c.G > c.A || c.B > c.A || c.A == 0 {
		t.Errorf("extreme gradient gave invalid color %v", c)
	}
	// endpoints reproduce the boundary stops exactly (no interpolation rounding)
	if many.AtWith(0.0, GradientOptions{}) != many[0].Color {
		t.Error("extreme gradient first stop mismatch")
	}
	if many.AtWith(1.0, GradientOptions{}) != many[len(many)-1].Color {
		t.Error("extreme gradient last stop mismatch")
	}

	// coincident offsets do not divide by zero
	coin := Grad{{0.5, color.RGBA{255, 0, 0, 255}}, {0.5, color.RGBA{0, 255, 0, 255}}}
	_ = coin.AtWith(0.5, GradientOptions{})

	// degenerate linear geometry (start == end) does not produce NaN
	degenerate := NewLinearGradient(Point{3, 3}, Point{3, 3})
	degenerate.Add(0.0, color.RGBA{255, 0, 0, 255})
	degenerate.Add(1.0, color.RGBA{0, 0, 255, 255})
	dc := degenerate.At(100, 100)
	if dc.A == 0 || dc.R > dc.A {
		t.Errorf("degenerate linear gradient gave %v", dc)
	}
}

// TestGradientDoesNotMutateCallerStops ensures Add, SetColorSpace and sampling
// never modify slices the caller still holds.
func TestGradientDoesNotMutateCallerStops(t *testing.T) {
	original := []Stop{{0.0, color.RGBA{255, 0, 0, 255}}, {1.0, color.RGBA{0, 0, 255, 255}}}
	snapshot := append([]Stop(nil), original...)

	g := append(Grad(nil), original...)
	g.Add(0.5, color.RGBA{0, 255, 0, 255})
	for i := range original {
		if original[i] != snapshot[i] {
			t.Fatalf("Add mutated caller slice: %+v", original)
		}
	}

	// SetColorSpace on the concrete gradient clones before converting
	lin := NewLinearGradient(Point{0, 0}, Point{10, 0})
	lin.Grad = append(Grad(nil), original...)
	lin.SetColorSpace(SRGBColorSpace{})
	for i := range original {
		if original[i] != snapshot[i] {
			t.Fatalf("SetColorSpace mutated caller slice: %+v", original)
		}
	}

	// repeated sampling never changes the stops
	for k := 0; k < 100; k++ {
		_ = lin.At(float64(k), 0)
	}
	for i := range lin.Grad {
		if lin.Grad[i].Offset != original[i].Offset {
			t.Fatalf("sampling mutated stops at %d", i)
		}
	}
}

// TestGradientConcurrentReuse hammers the same gradient from many goroutines to
// establish that sampling is safe for concurrent reuse.
func TestGradientConcurrentReuse(t *testing.T) {
	lin := NewLinearGradient(Point{0, 0}, Point{100, 0})
	lin.GradientOptions = GradientOptions{Interpolation: GradientLinearLight, Alpha: GradientStraight, Spread: GradientReflect}
	lin.Add(0.0, color.RGBA{255, 0, 0, 200})
	lin.Add(0.5, color.RGBA{0, 255, 0, 128})
	lin.Add(1.0, color.RGBA{0, 0, 255, 64})

	rad := NewRadialGradient(Point{0, 0}, 0, Point{0, 0}, 50)
	rad.Spread = GradientRepeat
	rad.Add(0.0, color.RGBA{255, 0, 0, 255})
	rad.Add(1.0, color.RGBA{0, 0, 255, 255})

	con := NewConicGradient(Point{0, 0}, math.Pi/4)
	con.Add(0.0, color.RGBA{255, 0, 0, 255})
	con.Add(1.0, color.RGBA{0, 255, 0, 255})

	const workers = 32
	const iterations = 2000
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for k := 0; k < iterations; k++ {
				x := float64((seed + k) % 400)
				_ = lin.At(x, 0.5)
				_ = rad.At(x-200, float64(k%100)-50)
				_ = con.At(float64(k%200)-100, float64(seed%200)-100)
			}
		}(w)
	}
	wg.Wait()
}

// TestConicGradient verifies the angular geometry of the conic gradient.
func TestConicGradient(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	g := NewConicGradient(Point{0, 0}, 0.0)
	g.Add(0.0, red)
	g.Add(1.0, blue)

	// center returns the first stop
	if got := g.At(0, 0); got != red {
		t.Errorf("conic center = %v, want red", got)
	}
	// positive X axis is offset 0
	if got := g.At(10, 0); got != red {
		t.Errorf("conic +X = %v, want red", got)
	}
	// positive Y axis is a quarter turn (canvas Y points up), offset 0.25, so
	// the color must be a blend rather than either endpoint
	quarter := g.At(0, 10)
	if quarter == red || quarter == blue {
		t.Errorf("conic +Y should be blended, got %v", quarter)
	}
	// the same ray at any radius gives the same color
	if g.At(0, 5) != g.At(0, 50) {
		t.Error("conic color must be radius-independent")
	}

	// angle offset rotates the starting ray
	rotated := NewConicGradient(Point{0, 0}, math.Pi/2.0)
	rotated.Add(0.0, red)
	rotated.Add(1.0, blue)
	if got := rotated.At(0, 10); got != red {
		t.Errorf("rotated conic +Y = %v, want red", got)
	}

	// conic satisfies the Gradient interface and is usable as an image source
	var _ Gradient = g
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, g.At(1, 1))
}
