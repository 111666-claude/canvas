package canvas

import (
	"image/color"
	"math"
)

// Spread is the method by which a gradient is extended beyond its domain.
type Spread uint8

const (
	// PadSpread extends the boundary colors beyond the gradient's domain.
	PadSpread Spread = iota
	// RepeatSpread repeats the gradient's colors beyond the gradient's domain.
	RepeatSpread
	// ReflectSpread mirrors the gradient's colors beyond the gradient's domain.
	ReflectSpread
)

// spreadTime maps a time t, whose domain [0,1] corresponds to the gradient's
// domain, back into [0,1] according to the spread method. The result is always
// finite and in [0,1].
func spreadTime(spread Spread, t float64) float64 {
	if math.IsNaN(t) {
		return 0.0
	}
	if math.IsInf(t, 0) {
		// every point at infinity is equivalent modulo the period
		if spread == ReflectSpread {
			return 1.0
		}
		return 0.0
	}
	switch spread {
	case RepeatSpread:
		t = math.Mod(t, 1.0)
		if t < 0.0 {
			t += 1.0
		}
		return t
	case ReflectSpread:
		// fold the real line into zig-zagging periods of length 2
		t = math.Mod(t, 2.0)
		if t < 0.0 {
			t += 2.0
		}
		if t > 1.0 {
			t = 2.0 - t
		}
		return t
	default: // PadSpread
		if t < 0.0 {
			return 0.0
		} else if t > 1.0 {
			return 1.0
		}
		return t
	}
}

// ConicGradient is a conic (angular) gradient pattern that sweeps colors
// counterclockwise (or clockwise for a negative arc) around a center point.
// The color at offset 0 corresponds to the angle start and offset 1 to the
// angle start plus arc. Angles are in radians measured from the positive
// X-axis in the canvas's coordinate system. Center, start and arc are in the
// canvas's coordinate system. The gradient supports any (possibly negative or
// spanning more than 2π) angular arc and pad, repeat and reflect spreads.
type ConicGradient struct {
	Grad
	Center Point
	Start  float64
	Arc    float64
	Spread Spread
}

// NewConicGradient returns a new conic gradient pattern centered at center,
// starting at angle start (radians) and sweeping over arc (radians). A
// negative arc sweeps clockwise. It panics when start or arc is not finite.
func NewConicGradient(center Point, start, arc float64) *ConicGradient {
	if math.IsNaN(start) || math.IsInf(start, 0) {
		panic("conic gradient: start angle must be finite")
	}
	if math.IsNaN(arc) || math.IsInf(arc, 0) {
		panic("conic gradient: arc angle must be finite")
	}
	return &ConicGradient{
		Center: center,
		Start:  start,
		Arc:    arc,
		Spread: PadSpread,
	}
}

// SetColorSpace sets the color space. Automatically called by the rasterizer.
func (g *ConicGradient) SetColorSpace(colorSpace ColorSpace) Gradient {
	g.Grad.SetColorSpace(colorSpace)
	return g
}

// At returns the color at position (x,y). The result is always a finite color
// and never contains NaN: the center and points with a degenerate (zero) arc
// resolve to the boundary colors, empty gradients are transparent and
// non-finite positions degrade deterministically.
func (g *ConicGradient) At(x, y float64) color.RGBA {
	if len(g.Grad) == 0 {
		return Transparent
	}

	dx := x - g.Center.X
	dy := y - g.Center.Y
	if math.IsNaN(dx) || math.IsNaN(dy) {
		return g.Grad.At(0)
	}
	if (Equal(dx, 0.0) && Equal(dy, 0.0)) || Equal(g.Arc, 0.0) {
		// the angle is undefined at the center and for a zero-length sweep;
		// resolve deterministically to the first stop's color
		return g.Grad.At(0)
	}

	theta := math.Atan2(dy, dx)
	if math.IsNaN(theta) {
		return g.Grad.At(0)
	}
	t := g.timeAtAngle(theta)
	return g.Grad.At(spreadTime(g.Spread, t))
}

// timeAtAngle maps an absolute angle theta (radians from the positive X-axis)
// to a gradient time using the same wrapped angular-distance rule as At.
func (g *ConicGradient) timeAtAngle(theta float64) float64 {
	delta := angleNorm(theta) - angleNorm(g.Start)
	if g.Arc > 0.0 && delta < 0.0 {
		delta += 2.0 * math.Pi
	} else if g.Arc < 0.0 && delta > 0.0 {
		delta -= 2.0 * math.Pi
	}
	return delta / g.Arc
}

// conicWedgeSegments is the default number of color segments per full turn
// used when tessellating the gradient for vector output.
const conicWedgeSegments = 360

// ConicMaxWedges is the maximum number of wedge paths emitted when
// tessellating the gradient for vector output, guarding against pathological
// inputs such as extremely small arcs combined with repeated spreads.
const ConicMaxWedges = 100000

// Wedges tessellates the conic gradient over the angular span [theta0, theta1)
// into solid-colored wedge paths and their corresponding colors. The span is
// given in radians in the same convention as At: angles measured from the
// positive X-axis. Each returned wedge path is a closed pie slice rooted at
// the gradient's center with the given radius, which must cover the shape
// being painted. The number of wedges is bounded by ConicMaxWedges.
func (g *ConicGradient) Wedges(radius, theta0, theta1 float64) ([]*Path, []color.RGBA) {
	if len(g.Grad) == 0 || radius <= 0.0 || Equal(theta0, theta1) || Equal(g.Arc, 0.0) {
		return nil, nil
	}
	// sample at a fixed angular resolution so that the seam is independent of
	// the requested span
	step := 2.0 * math.Pi / conicWedgeSegments
	n := int(math.Ceil(math.Abs(theta1-theta0)/step)) + 1
	if n < 1 {
		n = 1
	} else if n > ConicMaxWedges {
		n = ConicMaxWedges
	}
	dtheta := (theta1 - theta0) / float64(n)

	paths := make([]*Path, 0, n)
	colors := make([]color.RGBA, 0, n)
	prev := theta0
	for i := 0; i < n; i++ {
		next := theta0 + float64(i+1)*dtheta
		mid := 0.5 * (prev + next)
		col := g.colorAtAngle(mid)
		paths = append(paths, wedgePath(g.Center, radius, prev, next))
		colors = append(colors, col)
		prev = next
	}
	return paths, colors
}

// colorAtAngle returns the gradient color for an absolute angle in radians.
func (g *ConicGradient) colorAtAngle(theta float64) color.RGBA {
	return g.Grad.At(spreadTime(g.Spread, g.timeAtAngle(theta)))
}

// wedgePath returns a closed pie slice from center with radius r between
// angles a and b.
func wedgePath(center Point, r, a, b float64) *Path {
	p0 := center.Add(Point{r * math.Cos(a), r * math.Sin(a)})
	p1 := center.Add(Point{r * math.Cos(b), r * math.Sin(b)})
	sweep := b > a
	large := math.Abs(b-a) > math.Pi

	p := &Path{}
	p.MoveTo(center.X, center.Y)
	p.LineTo(p0.X, p0.Y)
	p.ArcTo(r, r, 0.0, large, sweep, p1.X, p1.Y)
	p.Close()
	return p
}
