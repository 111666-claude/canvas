package canvas

import (
	"image/color"
	"math"
	"slices"
)

// RGB returns a color given by red, green, and blue ∈ [0,1].
func RGB(r, g, b float64) color.RGBA {
	return color.RGBA{
		uint8(r * 255.0),
		uint8(g * 255.0),
		uint8(b * 255.0),
		uint8(255.0),
	}
}

// RGBA returns a color given by red, green, blue, and alpha ∈ [0,1] (non alpha premultiplied).
func RGBA(r, g, b, a float64) color.RGBA {
	return color.RGBA{
		uint8(a * r * 255.0),
		uint8(a * g * 255.0),
		uint8(a * b * 255.0),
		uint8(a * 255.0),
	}
}

func ToOpacity(col color.Color, a float64) color.RGBA {
	R, G, B, A := col.RGBA()
	return color.RGBA{
		R: uint8(float64(R) / float64(A) * a * 255.0),
		G: uint8(float64(G) / float64(A) * a * 255.0),
		B: uint8(float64(B) / float64(A) * a * 255.0),
		A: uint8(a * 255.0),
	}
}

// Hex parses a CSS hexadecimal color such as e.g. #ff0000 or F00.
func Hex(s string) color.RGBA {
	if 0 < len(s) && s[0] == '#' {
		s = s[1:]
	}
	h := make([]uint8, len(s))
	for i, c := range s {
		if '0' <= c && c <= '9' {
			h[i] = uint8(c - '0')
		} else if 'a' <= c && c <= 'f' {
			h[i] = 10 + uint8(c-'a')
		} else if 'A' <= c && c <= 'F' {
			h[i] = 10 + uint8(c-'A')
		}
	}
	if len(s) == 3 {
		return color.RGBA{h[0]*16 + h[0], h[1]*16 + h[1], h[2]*16 + h[2], 0xff}
	} else if len(s) == 4 {
		a := float64(h[3]*16+h[0]) / 255.0
		return color.RGBA{
			uint8(a * float64(h[0]*16+h[0])),
			uint8(a * float64(h[1]*16+h[1])),
			uint8(a * float64(h[2]*16+h[2])),
			h[3]*16 + h[3],
		}
	} else if len(s) == 6 {
		return color.RGBA{h[0]*16 + h[1], h[2]*16 + h[3], h[4]*16 + h[5], 0xff}
	} else if len(s) == 8 {
		a := float64(h[6]*16+h[7]) / 255.0
		return color.RGBA{
			uint8(a * float64(h[0]*16+h[1])),
			uint8(a * float64(h[2]*16+h[3])),
			uint8(a * float64(h[4]*16+h[5])),
			h[6]*16 + h[7],
		}
	}
	return Black
}

// Gradient is a gradient pattern for filling.
type Gradient interface {
	SetColorSpace(ColorSpace) Gradient
	At(float64, float64) color.RGBA
}

// GradientInterpolation selects the color space in which the RGB channels of two
// gradient stops are interpolated. The default, GradientSRGB, interpolates the
// encoded channel values directly and matches the behavior of most PDF viewers
// and browsers. GradientLinearLight converts the stops to linear light before
// interpolating, which is physically correct and avoids the dark band that
// appears when interpolating between saturated or semi-transparent sRGB stops.
type GradientInterpolation int

const (
	// GradientSRGB interpolates the sRGB-encoded channel values directly. This is
	// the default and preserves the historical pixel-for-pixel behavior.
	GradientSRGB GradientInterpolation = iota
	// GradientLinearLight converts stops to linear light before interpolating.
	GradientLinearLight
)

// GradientAlpha selects how the alpha channel participates in interpolation.
// The default, GradientPremultiplied, interpolates premultiplied color values
// (c*a and a together), which is robust against non-finite colors and keeps the
// premultiplied invariant r,g,b <= a at every t. GradientStraight interpolates
// the unassociated (straight) color and alpha independently, the semantics used
// by SVG and PDF; it is the correct choice when a fully transparent stop still
// carries a meaningful color.
type GradientAlpha int

const (
	// GradientPremultiplied interpolates premultiplied colors. It is the default
	// and preserves the historical pixel-for-pixel behavior.
	GradientPremultiplied GradientAlpha = iota
	// GradientStraight interpolates straight (unassociated) colors and alpha.
	GradientStraight
)

// GradientSpread determines how the gradient is painted outside the domain
// spanned by its stops (the [first offset, last offset] interval).
type GradientSpread int

const (
	// GradientPad extends the colors of the first and last stops. It is the
	// default and matches the SVG pad and PDF Extend behavior.
	GradientPad GradientSpread = iota
	// GradientRepeat repeats the gradient periodically.
	GradientRepeat
	// GradientReflect mirrors the gradient alternately on every repetition.
	GradientReflect
)

// GradientOptions holds the sampling configuration shared by all gradient
// types. The zero value reproduces the historical behavior exactly: sRGB
// interpolation, premultiplied alpha, and pad spread.
type GradientOptions struct {
	// Interpolation is the color space used for interpolating RGB channels.
	Interpolation GradientInterpolation
	// Alpha selects premultiplied or straight (unassociated) alpha interpolation.
	Alpha GradientAlpha
	// Spread selects the behavior outside the stops' domain.
	Spread GradientSpread
}

// Stop is a color and offset for gradient patterns.
type Stop struct {
	Offset float64
	Color  color.RGBA
}

// Grad are the colors and offsets for gradient patterns, sorted by offset.
type Grad []Stop

func NewGradient() Grad {
	return Grad{}
}

// sanitizeOffset maps any float64, including non-finite values, onto [0,1].
// NaN and negative infinity map to 0, positive infinity maps to 1.
func sanitizeOffset(t float64) float64 {
	if math.IsNaN(t) || t < 0.0 {
		return 0.0
	} else if t > 1.0 {
		return 1.0
	}
	return t
}

// sanitizeColor returns a valid premultiplied color.RGBA by enforcing the
// premultiplied invariant r,g,b <= a, so a transparent endpoint can never
// carry or overflow into color. Inputs arrive through rgbaColor, which already
// rejects non-finite float channels.
func sanitizeColor(c color.RGBA) color.RGBA {
	if c.R > c.A {
		c.R = c.A
	}
	if c.G > c.A {
		c.G = c.A
	}
	if c.B > c.A {
		c.B = c.A
	}
	return c
}

// Add adds a new color stop to a gradient. The offset is clamped to [0,1]
// (non-finite values map to the nearest boundary) and the color is sanitized to
// a valid premultiplied color. The caller's data is never retained by reference
// beyond the value being added.
func (g *Grad) Add(t float64, col color.RGBA) {
	stop := Stop{sanitizeOffset(t), sanitizeColor(rgbaColor(col))}
	// insert or replace stop and keep sort order
	for i := range *g {
		if Equal((*g)[i].Offset, stop.Offset) {
			(*g)[i] = stop
			return
		} else if stop.Offset < (*g)[i].Offset {
			*g = append((*g)[:i], append(Grad{stop}, (*g)[i:]...)...)
			return
		}
	}
	*g = append(*g, stop)
}

// spreadPosition maps a position t onto the stops' domain [lo, hi] according to
// the spread method. The returned position always lies within [lo, hi].
func spreadPosition(t, lo, hi float64, spread GradientSpread) float64 {
	if spread == GradientPad || hi <= lo {
		if t < lo {
			return lo
		} else if t > hi {
			return hi
		}
		return t
	}
	w := hi - lo
	u := (t - lo) / w
	switch spread {
	case GradientRepeat:
		u = u - math.Floor(u)
	case GradientReflect:
		// period of two domains; fold every second domain back onto the first.
		u = math.Mod(u, 2.0)
		if u < 0.0 {
			u += 2.0
		}
		if u > 1.0 {
			u = 2.0 - u
		}
	}
	return lo + u*w
}

// At returns the color at position t using the historical sampling semantics:
// sRGB interpolation of premultiplied colors with pad spread. It is equivalent
// to AtWith(t, GradientOptions{}).
func (g Grad) At(t float64) color.RGBA {
	return g.AtWith(t, GradientOptions{})
}

// AtWith returns the color at position t under the given sampling options. The
// evaluation order is: sanitize and sort the stops, map t through the spread
// function onto the stops' domain, locate the enclosing stops, and interpolate
// in the configured color space with the configured alpha treatment. An empty
// gradient returns Transparent and a single-stop gradient returns that stop.
func (g Grad) AtWith(t float64, opts GradientOptions) color.RGBA {
	n := len(g)
	if n == 0 {
		return Transparent
	} else if n == 1 {
		return g[0].Color
	}
	lo, hi := g[0].Offset, g[n-1].Offset
	if math.IsNaN(t) {
		return g[0].Color
	}
	t = spreadPosition(t, lo, hi, opts.Spread)
	if t <= lo || hi <= lo {
		return g[0].Color
	} else if t >= hi {
		return g[n-1].Color
	}

	// binary search for the first stop whose offset is greater than t
	i, j := 0, n
	for i < j {
		h := int(uint(i+j) >> 1)
		if g[h].Offset <= t {
			i = h + 1
		} else {
			j = h
		}
	}
	if i == 0 || i >= n {
		// all offsets coincident or t exactly on a boundary
		return g[n-1].Color
	}
	after := g[i]
	before := g[i-1]
	if after.Offset <= before.Offset {
		// coincident offsets: no interval to interpolate across
		return after.Color
	}
	f := (t - before.Offset) / (after.Offset - before.Offset)
	if f < 0.0 {
		f = 0.0
	} else if f > 1.0 {
		f = 1.0
	}
	return interpolateStops(before.Color, after.Color, f, opts)
}

// interpolateStops blends two premultiplied 8-bit colors at fraction f ∈ [0,1].
// The default configuration (sRGB, premultiplied) uses the original integer
// pipeline so output is bit-identical to the historical implementation; every
// other configuration uses a clamped float64 pipeline that cannot overflow.
func interpolateStops(c0, c1 color.RGBA, f float64, opts GradientOptions) color.RGBA {
	if opts.Interpolation == GradientSRGB && opts.Alpha == GradientPremultiplied {
		// historical fast path: integer interpolation of premultiplied channels
		T := uint32(f*65535.0 + 0.5)
		return color.RGBA{
			lerp(uint32(c0.R)<<8, uint32(c1.R)<<8, T),
			lerp(uint32(c0.G)<<8, uint32(c1.G)<<8, T),
			lerp(uint32(c0.B)<<8, uint32(c1.B)<<8, T),
			lerp(uint32(c0.A)<<8, uint32(c1.A)<<8, T),
		}
	}

	a0 := float64(c0.A) / 255.0
	a1 := float64(c1.A) / 255.0
	// straight (unassociated) colors, guarded against division by zero alpha
	unpremul := func(c color.RGBA, a float64) (r, g, b float64) {
		if a == 0.0 {
			return 0.0, 0.0, 0.0
		}
		return float64(c.R) / 255.0 / a, float64(c.G) / 255.0 / a, float64(c.B) / 255.0 / a
	}
	r0, g0, b0 := unpremul(c0, a0)
	r1, g1, b1 := unpremul(c1, a1)

	if opts.Interpolation == GradientLinearLight {
		r0, g0, b0 = sRGBToLinear(r0), sRGBToLinear(g0), sRGBToLinear(b0)
		r1, g1, b1 = sRGBToLinear(r1), sRGBToLinear(g1), sRGBToLinear(b1)
	}

	a := a0 + (a1-a0)*f
	mix := func(x, y float64) float64 { return x + (y-x)*f }
	r, g, b := mix(r0, r1), mix(g0, g1), mix(b0, b1)

	if opts.Alpha == GradientPremultiplied {
		// premultiplied interpolation: scale the straight colors by their alphas
		r, g, b = r0*a0+(r1*a1-r0*a0)*f, g0*a0+(g1*a1-g0*a0)*f, b0*a0+(b1*a1-b0*a0)*f
	}

	if opts.Interpolation == GradientLinearLight {
		r, g, b = linearTosRGB(r), linearTosRGB(g), linearTosRGB(b)
	}
	return clampColor(r, g, b, a)
}

// sRGBToLinear converts a single sRGB-encoded channel in [0,1] to linear light.
func sRGBToLinear(c float64) float64 {
	if c <= 0.0 {
		return 0.0
	} else if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// linearTosRGB converts a single linear-light channel in [0,1] to sRGB.
func linearTosRGB(c float64) float64 {
	switch {
	case c <= 0.0:
		return 0.0
	case c < 0.0031308:
		return 12.92 * c
	case c < 1.0:
		return 1.055*math.Pow(c, 1.0/2.4) - 0.055
	}
	return 1.0
}

// clampColor converts straight/premultiplied float channels to a valid
// premultiplied color.RGBA. Non-finite values become zero and every channel is
// clamped to its representable range, enforcing r,g,b <= a.
func clampColor(r, g, b, a float64) color.RGBA {
	clean := func(v float64) float64 {
		if math.IsNaN(v) || v < 0.0 {
			return 0.0
		} else if v > 1.0 {
			return 1.0
		}
		return v
	}
	a = clean(a)
	cleanPremul := func(v float64) float64 {
		v = clean(v)
		if v > a {
			return a
		}
		return v
	}
	r, g, b = cleanPremul(r), cleanPremul(g), cleanPremul(b)
	return color.RGBA{
		R: uint8(r*255.0 + 0.5),
		G: uint8(g*255.0 + 0.5),
		B: uint8(b*255.0 + 0.5),
		A: uint8(a*255.0 + 0.5),
	}
}

func (g Grad) ToLinear(start, end Point) *LinearGradient {
	grad := NewLinearGradient(start, end)
	grad.Grad = g
	return grad
}

func (g Grad) ToRadial(c0 Point, r0 float64, c1 Point, r1 float64) *RadialGradient {
	grad := NewRadialGradient(c0, r0, c1, r1)
	grad.Grad = g
	return grad
}

func (g Grad) ToConic(center Point, angle float64) *ConicGradient {
	grad := NewConicGradient(center, angle)
	grad.Grad = g
	return grad
}

// SetColorSpace converts the gradient's stops to the given color space. The
// conversion is performed on a cloned slice so the caller's stop slice is
// never modified. LinearColorSpace is a no-op.
func (g *Grad) SetColorSpace(colorSpace ColorSpace) {
	if _, ok := colorSpace.(LinearColorSpace); ok {
		return
	}
	converted := slices.Clone(*g)
	for i := range converted {
		converted[i].Color = colorSpace.ToLinear(converted[i].Color)
	}
	*g = converted
}

func lerp(a, b, t uint32) uint8 {
	return uint8(((0xffff-t)*a + t*b) >> 24)
}

// LinearGradient is a linear gradient pattern between the given start and end points. The color at offset 0 corresponds to the start position, and offset 1 to the end position. Start and end points are in the canvas's coordinate system.
type LinearGradient struct {
	Grad
	GradientOptions
	Start, End Point
	d          Point
	d2         float64
}

// NewLinearGradient returns a new linear gradient pattern.
func NewLinearGradient(start, end Point) *LinearGradient {
	d := end.Sub(start)
	return &LinearGradient{
		Start: start,
		End:   end,
		d:     d,
		d2:    d.Dot(d),
	}
}

// SetColorSpace sets the color space. Automatically called by the rasterizer.
// A gradient configured for linear-light interpolation already encodes linear
// light in its stops, so no conversion is applied.
func (g *LinearGradient) SetColorSpace(colorSpace ColorSpace) Gradient {
	if g.Interpolation != GradientLinearLight {
		g.Grad.SetColorSpace(colorSpace)
	}
	return g
}

// At returns the color at position (x,y).
func (g *LinearGradient) At(x, y float64) color.RGBA {
	if len(g.Grad) == 0 {
		return Transparent
	}

	p := Point{x, y}.Sub(g.Start)
	var t float64
	if Equal(g.d.Y, 0.0) && !Equal(g.d.X, 0.0) {
		t = p.X / g.d.X // horizontal
	} else if !Equal(g.d.Y, 0.0) && Equal(g.d.X, 0.0) {
		t = p.Y / g.d.Y // vertical
	} else if g.d2 == 0.0 {
		t = 0.0
	} else {
		t = p.Dot(g.d) / g.d2
	}
	return g.Grad.AtWith(t, g.GradientOptions)
}

// RadialGradient is a radial gradient pattern between two circles defined by their center points and radii. Color stop at offset 0 corresponds to the first circle and offset 1 to the second circle.
type RadialGradient struct {
	Grad
	GradientOptions
	C0, C1 Point
	R0, R1 float64
	cd     Point
	dr, a  float64
}

// NewRadialGradient returns a new radial gradient pattern.
func NewRadialGradient(c0 Point, r0 float64, c1 Point, r1 float64) *RadialGradient {
	cd := c1.Sub(c0)
	dr := r1 - r0
	return &RadialGradient{
		C0: c0,
		R0: r0,
		C1: c1,
		R1: r1,
		cd: cd,
		dr: dr,
		a:  cd.Dot(cd) - dr*dr,
	}
}

// SetColorSpace sets the color space. Automatically called by the rasterizer.
// A gradient configured for linear-light interpolation already encodes linear
// light in its stops, so no conversion is applied.
func (g *RadialGradient) SetColorSpace(colorSpace ColorSpace) Gradient {
	if g.Interpolation != GradientLinearLight {
		g.Grad.SetColorSpace(colorSpace)
	}
	return g
}

// At returns the color at position (x,y).
func (g *RadialGradient) At(x, y float64) color.RGBA {
	if len(g.Grad) == 0 {
		return Transparent
	}

	// see reference implementation of pixman-radial-gradient
	// https://github.com/servo/pixman/blob/master/pixman/pixman-radial-gradient.c#L161
	pd := Point{x, y}.Sub(g.C0)
	b := pd.Dot(g.cd) + g.R0*g.dr
	c := pd.Dot(pd) - g.R0*g.R0
	t0, t1 := solveQuadraticFormula(g.a, -2.0*b, c)

	valid := func(t float64) bool {
		return !math.IsNaN(t) && t >= 0 && t <= 1 && g.R0+g.dr*t >= 0
	}
	hasPositive := func(t float64) bool {
		return !math.IsNaN(t) && t > 0 && g.R0+g.dr*t >= 0
	}

	// Pick the largest valid t (t1 >= t0 from solveQuadraticFormula).
	if valid(t1) {
		return g.Grad.AtWith(t1, g.GradientOptions)
	}
	if valid(t0) {
		return g.Grad.AtWith(t0, g.GradientOptions)
	}

	// No valid t in [0,1]. Extend boundary colors through the spread function.
	if hasPositive(t0) || hasPositive(t1) {
		return g.Grad.AtWith(1.0, g.GradientOptions)
	}
	return g.Grad.AtWith(0.0, g.GradientOptions)
}

// ConicGradient is a conic (sweep) gradient rotating around Center. Stop offset
// 0 corresponds to the direction Angle (in radians, measured counterclockwise
// from the positive X axis) and offset 1 to a full clockwise turn. At the
// center itself the first stop's color is returned. A full-turn conic gradient
// is periodic by construction; Spread still controls how partial domains (when
// the stops do not span the full [0,1] range) are extended.
type ConicGradient struct {
	Grad
	GradientOptions
	Center Point
	Angle  float64
}

// NewConicGradient returns a new conic gradient pattern with the given center
// and starting angle in radians.
func NewConicGradient(center Point, angle float64) *ConicGradient {
	return &ConicGradient{
		Center: center,
		Angle:  angle,
	}
}

// SetColorSpace sets the color space. Automatically called by the rasterizer.
// A gradient configured for linear-light interpolation already encodes linear
// light in its stops, so no conversion is applied.
func (g *ConicGradient) SetColorSpace(colorSpace ColorSpace) Gradient {
	if g.Interpolation != GradientLinearLight {
		g.Grad.SetColorSpace(colorSpace)
	}
	return g
}

// At returns the color at position (x,y).
func (g *ConicGradient) At(x, y float64) color.RGBA {
	if len(g.Grad) == 0 {
		return Transparent
	}
	p := Point{x, y}.Sub(g.Center)
	if p.X == 0.0 && p.Y == 0.0 {
		return g.Grad[0].Color
	}
	theta := math.Atan2(p.Y, p.X) - g.Angle
	// normalize to [0,1): offsets 0 and 1 describe the same starting ray
	t := theta / (2.0 * math.Pi)
	t -= math.Floor(t)
	if t == 1.0 {
		t = 0.0
	}
	return g.Grad.AtWith(t, g.GradientOptions)
}

// ColorSpace defines the color space within the RGB color model. All colors passed to this library are assumed to be in the sRGB color space, which is a ubiquitous assumption in most software. This works great for most applications, but fails when blending semi-transparent layers. See an elaborate explanation at https://blog.johnnovak.net/2016/09/21/what-every-coder-should-know-about-gamma/, which goes into depth of the problems of using sRGB for blending and the need for gamma correction. In short, we need to transform the colors, which are in the sRGB color space, to the linear color space, perform blending, and then transform them back to the sRGB color space.
// Unfortunately, almost all software does blending the wrong way (all PDF renderers and browsers I've tested), so by default this library will do the same by using LinearColorSpace which does no conversion from sRGB to linear and back but blends directly in sRGB. Or in other words, it assumes that colors are given in the linear color space and that the output image is expected to be in the linear color space as well. For technical correctness we should really be using the SRGBColorSpace, which will convert from sRGB to linear space, do blending in linear space, and then go back to sRGB space.
type ColorSpace interface {
	ToLinear(color.Color) color.RGBA
	FromLinear(color.Color) color.RGBA
}

// DefaultColorSpace is set to LinearColorSpace to match other renderers.
var DefaultColorSpace ColorSpace = LinearColorSpace{}

// LinearColorSpace is the default color space that does not do color space conversion for blending purposes. This is only correct if the input colors and output images are assumed to be in the linear color space so that blending is in linear space as well. In general though, we assume that input colors and output images are using the sRGB color space almost ubiquitously, resulting in blending in sRGB space which is wrong! Even though it is technically incorrect, many PDF viewers and browsers do this anyway.
type LinearColorSpace struct{}

// ToLinear encodes color to color space.
func (LinearColorSpace) ToLinear(col color.Color) color.RGBA {
	if rgba, ok := col.(color.RGBA); ok {
		return rgba
	}
	R, G, B, A := col.RGBA()
	return color.RGBA{uint8(R >> 8), uint8(G >> 8), uint8(B >> 8), uint8(A >> 8)}
}

// FromLinear decodes color from color space.
func (LinearColorSpace) FromLinear(col color.Color) color.RGBA {
	if rgba, ok := col.(color.RGBA); ok {
		return rgba
	}
	R, G, B, A := col.RGBA()
	return color.RGBA{uint8(R >> 8), uint8(G >> 8), uint8(B >> 8), uint8(A >> 8)}
}

// GammaColorSpace assumes that input colors and output images are gamma-corrected with the given gamma value. The sRGB space uses a gamma=2.4 for most of the curve, but will on average have a gamma=2.2 best approximating the sRGB curve. See https://en.wikipedia.org/wiki/SRGB#The_sRGB_transfer_function_(%22gamma%22). According to https://www.puredevsoftware.com/blog/2019/01/22/sub-pixel-gamma-correct-font-rendering/, a gamma=1.43 is recommended for fonts.
type GammaColorSpace struct {
	Gamma float64
}

// ToLinear encodes color to color space.
func (cs GammaColorSpace) ToLinear(col color.Color) color.RGBA {
	R, G, B, A := col.RGBA()
	r := math.Pow(float64(R)/float64(A), cs.Gamma)
	g := math.Pow(float64(G)/float64(A), cs.Gamma)
	b := math.Pow(float64(B)/float64(A), cs.Gamma)
	a := float64(A) / 0xffff
	return color.RGBA{
		uint8(r*a*255.0 + 0.5),
		uint8(g*a*255.0 + 0.5),
		uint8(b*a*255.0 + 0.5),
		uint8(a*255.0 + 0.5),
	}
}

// FromLinear decodes color from color space.
func (cs GammaColorSpace) FromLinear(col color.Color) color.RGBA {
	R, G, B, A := col.RGBA()
	r := math.Pow(float64(R)/float64(A), 1.0/cs.Gamma)
	g := math.Pow(float64(G)/float64(A), 1.0/cs.Gamma)
	b := math.Pow(float64(B)/float64(A), 1.0/cs.Gamma)
	a := float64(A) / 0xffff
	return color.RGBA{
		uint8(r*a*255.0 + 0.5),
		uint8(g*a*255.0 + 0.5),
		uint8(b*a*255.0 + 0.5),
		uint8(a*255.0 + 0.5),
	}
}

// SRGBColorSpace assumes that input colors and output images are in the sRGB color space (ubiquitous in almost all applications), which implies that for blending we need to convert to the linear color space, do blending, and then convert back to the sRGB color space. This will give technically correct blending, but may differ from common PDF viewer and browsers (which are wrong).
type SRGBColorSpace struct{}

// ToLinear encodes color to color space.
func (SRGBColorSpace) ToLinear(col color.Color) color.RGBA {
	sRGBToLinear := func(c float64) float64 {
		// Formula from EXT_sRGB.
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}

	R, G, B, A := col.RGBA()
	r := sRGBToLinear(float64(R) / float64(A))
	g := sRGBToLinear(float64(G) / float64(A))
	b := sRGBToLinear(float64(B) / float64(A))
	a := float64(A) / 0xffff
	return color.RGBA{
		uint8(r*a*255.0 + 0.5),
		uint8(g*a*255.0 + 0.5),
		uint8(b*a*255.0 + 0.5),
		uint8(a*255.0 + 0.5),
	}
}

// FromLinear decodes color from color space.
func (SRGBColorSpace) FromLinear(col color.Color) color.RGBA {
	linearTosRGB := func(c float64) float64 {
		// Formula from EXT_sRGB.
		switch {
		case c <= 0.0:
			return 0.0
		case 0 < c && c < 0.0031308:
			return 12.92 * c
		case 0.0031308 <= c && c < 1:
			return 1.055*math.Pow(c, 0.41666) - 0.055
		}
		return 1.0
	}

	R, G, B, A := col.RGBA()
	r := linearTosRGB(float64(R) / float64(A))
	g := linearTosRGB(float64(G) / float64(A))
	b := linearTosRGB(float64(B) / float64(A))
	a := float64(A) / 0xffff
	return color.RGBA{
		uint8(r*a*255.0 + 0.5),
		uint8(g*a*255.0 + 0.5),
		uint8(b*a*255.0 + 0.5),
		uint8(a*255.0 + 0.5),
	}
}
