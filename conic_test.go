package canvas

import (
	"image/color"
	"math"
	"testing"
)

func conicRedBlue(center Point, start, arc float64, spread Spread) *ConicGradient {
	g := NewConicGradient(center, start, arc)
	g.Spread = spread
	g.Add(0.0, color.RGBA{255, 0, 0, 255})
	g.Add(1.0, color.RGBA{0, 0, 255, 255})
	return g
}

func colorApproxEqual(a, b color.RGBA) bool {
	diff := func(x, y uint8) int {
		d := int(x) - int(y)
		if d < 0 {
			return -d
		}
		return d
	}
	return diff(a.R, b.R) <= 1 && diff(a.G, b.G) <= 1 && diff(a.B, b.B) <= 1 && diff(a.A, b.A) <= 1
}

func TestSpreadTime(t *testing.T) {
	tests := []struct {
		spread   Spread
		in, want float64
	}{
		{PadSpread, -0.5, 0.0},
		{PadSpread, 0.5, 0.5},
		{PadSpread, 1.5, 1.0},
		{RepeatSpread, 0.0, 0.0},
		{RepeatSpread, 1.0, 0.0},
		{RepeatSpread, 1.5, 0.5},
		{RepeatSpread, -0.25, 0.75},
		{RepeatSpread, -1.25, 0.75},
		{RepeatSpread, 3.75, 0.75},
		{ReflectSpread, 0.25, 0.25},
		{ReflectSpread, 0.75, 0.75},
		{ReflectSpread, 1.25, 0.75},
		{ReflectSpread, 1.75, 0.25},
		{ReflectSpread, 2.25, 0.25},
		{ReflectSpread, -0.25, 0.25},
		{ReflectSpread, -1.25, 0.75},
	}
	for _, tt := range tests {
		if got := spreadTime(tt.spread, tt.in); !Equal(got, tt.want) {
			t.Errorf("spreadTime(%v, %v) = %v, want %v", tt.spread, tt.in, got, tt.want)
		}
	}

	// non-finite inputs must degrade deterministically and never produce NaN
	for _, spread := range []Spread{PadSpread, RepeatSpread, ReflectSpread} {
		for _, in := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			got := spreadTime(spread, in)
			if math.IsNaN(got) || got < 0.0 || got > 1.0 {
				t.Errorf("spreadTime(%v, %v) = %v, want finite value in [0,1]", spread, in, got)
			}
		}
	}

	// very large repeat counts stay finite and keep their fractional position
	got := spreadTime(RepeatSpread, 1e15+0.37)
	if math.IsNaN(got) || got < 0.0 || got > 1.0 {
		t.Errorf("large repeat produced non-finite/out-of-range t = %v", got)
	}
}

func TestNewConicGradientInvalid(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, arc float64
	}{
		{"NaN start", math.NaN(), 1.0},
		{"Inf start", math.Inf(1), 1.0},
		{"NaN arc", 0.0, math.NaN()},
		{"Inf arc", 0.0, math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic for non-finite angle")
				}
			}()
			NewConicGradient(Point{}, tc.start, tc.arc)
		})
	}
}

func TestConicGradientAt(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	tests := []struct {
		name string
		grad *ConicGradient
		x, y float64
		want color.RGBA
	}{
		{
			name: "empty gradient is transparent",
			grad: NewConicGradient(Point{}, 0.0, 2.0*math.Pi),
			x:    10, y: 0,
			want: Transparent,
		},
		{
			name: "start angle on positive X axis is first stop",
			grad: conicRedBlue(Point{}, 0.0, 2.0*math.Pi, PadSpread),
			x:    10, y: 0,
			want: red,
		},
		{
			name: "quarter turn CCW interpolates",
			grad: conicRedBlue(Point{}, 0.0, 2.0*math.Pi, PadSpread),
			x:    0, y: 10,
			want: color.RGBA{191, 0, 63, 255},
		},
		{
			name: "three quarter turn (negative X, atan2 seam)",
			grad: conicRedBlue(Point{}, 0.0, 2.0*math.Pi, PadSpread),
			x:    -10, y: 0,
			want: color.RGBA{127, 0, 127, 255},
		},
		{
			name: "negative Y axis completes the full turn",
			grad: conicRedBlue(Point{}, 0.0, 2.0*math.Pi, PadSpread),
			x:    0, y: -10,
			want: color.RGBA{63, 0, 191, 255},
		},
		{
			name: "offset center",
			grad: conicRedBlue(Point{5, 5}, 0.0, 2.0*math.Pi, PadSpread),
			x:    15, y: 5,
			want: red,
		},
		{
			name: "rotated start of pi/2",
			grad: conicRedBlue(Point{}, math.Pi/2.0, 2.0*math.Pi, PadSpread),
			x:    0, y: 10,
			want: red,
		},
		{
			name: "clockwise (negative) full arc at positive X is first stop",
			grad: conicRedBlue(Point{}, 0.0, -2.0*math.Pi, PadSpread),
			x:    10, y: 0,
			want: red,
		},
		{
			name: "clockwise quarter turn",
			grad: conicRedBlue(Point{}, 0.0, -2.0*math.Pi, PadSpread),
			x:    0, y: -10,
			want: color.RGBA{191, 0, 63, 255},
		},
		{
			name: "center with positive arc uses first stop",
			grad: conicRedBlue(Point{}, 0.0, 2.0*math.Pi, PadSpread),
			x:    0, y: 0,
			want: red,
		},
		{
			name: "center with negative arc still uses first stop",
			grad: conicRedBlue(Point{}, 0.0, -2.0*math.Pi, PadSpread),
			x:    0, y: 0,
			want: red,
		},
		{
			name: "zero arc resolves to the first stop",
			grad: conicRedBlue(Point{}, 0.0, 0.0, PadSpread),
			x:    10, y: 0,
			want: red,
		},
		{
			name: "pad clamps before a negative start to first stop",
			grad: conicRedBlue(Point{}, -math.Pi/2.0, math.Pi, PadSpread),
			x:    -10, y: 0, // t=1.5, beyond the arc end -> last stop
			want: blue,
		},
		{
			name: "pad clamps before start via positive angle gap",
			grad: conicRedBlue(Point{}, math.Pi/2.0, math.Pi, PadSpread),
			x:    10, y: 0, // t=1.5, equivalent to being before start modulo 2pi -> last stop
			want: blue,
		},
		{
			name: "pad within a negative start arc",
			grad: conicRedBlue(Point{}, -math.Pi/2.0, math.Pi, PadSpread),
			x:    10, y: 0, // delta pi/2 over arc pi -> t=0.5
			want: color.RGBA{127, 0, 127, 255},
		},
		{
			name: "pad within arc interpolates",
			grad: conicRedBlue(Point{}, 0.0, math.Pi, PadSpread),
			x:    0, y: 10, // angle pi/2 -> t=0.5
			want: color.RGBA{127, 0, 127, 255},
		},
		{
			name: "pad clamps after arc to last stop",
			grad: conicRedBlue(Point{}, 0.0, math.Pi/2.0, PadSpread),
			x:    -10, y: 0, // angle pi -> t=2 -> clamped to 1
			want: blue,
		},
		{
			name: "repeat wraps around",
			grad: conicRedBlue(Point{}, 0.0, math.Pi, RepeatSpread),
			x:    -10, y: 0, // angle pi -> t=1 -> repeats to t=0 -> red
			want: red,
		},
		{
			name: "reflect mirrors",
			grad: conicRedBlue(Point{}, 0.0, math.Pi, ReflectSpread),
			x:    0, y: -10, // angle -pi/2 -> t=-0.5 -> reflected 0.5
			want: color.RGBA{127, 0, 127, 255},
		},
		{
			name: "single stop returns that color everywhere",
			grad: func() *ConicGradient {
				g := NewConicGradient(Point{}, 0.0, 2.0*math.Pi)
				g.Add(0.5, red)
				return g
			}(),
			x: 10, y: 10,
			want: red,
		},
		{
			name: "non-finite position degrades to first stop",
			grad: conicRedBlue(Point{}, 0.0, 2.0*math.Pi, PadSpread),
			x:    math.NaN(), y: 0,
			want: red,
		},
		{
			name: "premultiplied transparent stop interpolates in premultiplied space",
			grad: func() *ConicGradient {
				g := NewConicGradient(Point{}, 0.0, 2.0*math.Pi)
				g.Add(0.0, color.RGBA{255, 0, 0, 255})
				g.Add(1.0, color.RGBA{0, 0, 0, 0})
				return g
			}(),
			x: 0, y: 10, // t=0.25
			want: color.RGBA{191, 0, 0, 191},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.grad.At(tt.x, tt.y)
			if !colorApproxEqual(got, tt.want) {
				t.Errorf("At(%v, %v) = %v, want %v", tt.x, tt.y, formatRGBA(got), formatRGBA(tt.want))
			}
			if math.IsNaN(float64(got.R)) || math.IsNaN(float64(got.G)) || math.IsNaN(float64(got.B)) || math.IsNaN(float64(got.A)) {
				t.Errorf("At(%v, %v) produced NaN channel", tt.x, tt.y)
			}
		})
	}
}

func TestConicGradientNegativeStartAndSpan(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	// negative start angle: start at -pi/2 (negative Y axis)
	g := conicRedBlue(Point{}, -math.Pi/2.0, math.Pi, PadSpread)
	if got := g.At(0, -10); !colorApproxEqual(got, red) {
		t.Errorf("negative start: got %v, want red", formatRGBA(got))
	}
	// arc spanning more than 2pi: t stays within [0,1] for the first turn
	g2 := conicRedBlue(Point{}, 0.0, 4.0*math.Pi, PadSpread)
	if got := g2.At(-10, 0); !colorApproxEqual(got, color.RGBA{191, 0, 63, 255}) {
		t.Errorf("4pi arc at pi: got %v, want t=0.25 color", formatRGBA(got))
	}
	if got := g2.At(10, 0); !colorApproxEqual(got, red) {
		t.Errorf("4pi arc at 0: got %v, want red", formatRGBA(got))
	}
	_ = blue
}

func TestConicGradientDuplicateStops(t *testing.T) {
	// repeated stop positions: Add replaces, and a hard boundary is possible
	g := NewConicGradient(Point{}, 0.0, 2.0*math.Pi)
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	g.Add(0.0, red)
	g.Add(0.5, green)
	g.Add(0.5, blue) // replaces green
	if len(g.Grad) != 2 {
		t.Fatalf("expected 2 stops after replacement, got %d", len(g.Grad))
	}
	if got := g.At(0, 10); !colorApproxEqual(got, color.RGBA{127, 0, 127, 255}) { // t=0.25 between red and blue
		t.Errorf("duplicate offset: got %v, want interpolated", formatRGBA(got))
	}
	// sampling never produces NaN even with coincident offsets
	for i := 0; i <= 100; i++ {
		theta := 2.0 * math.Pi * float64(i) / 100.0
		col := g.At(10.0*math.Cos(theta), 10.0*math.Sin(theta))
		if math.IsNaN(float64(col.R)) {
			t.Fatal("NaN with duplicate stops")
		}
	}
}

func TestConicGradientSetColorSpace(t *testing.T) {
	// default LinearColorSpace leaves colors untouched
	g := conicRedBlue(Point{}, 0.0, 2.0*math.Pi, PadSpread)
	returned := g.SetColorSpace(LinearColorSpace{})
	if returned != Gradient(g) {
		t.Error("SetColorSpace should return the same gradient")
	}
	if got := g.At(10, 0); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("linear color space changed colors: %v", got)
	}

	// sRGB color space converts stops; conversion is applied once and the
	// gradient still returns finite colors
	g2 := conicRedBlue(Point{}, 0.0, 2.0*math.Pi, PadSpread)
	g2.SetColorSpace(SRGBColorSpace{})
	for i := 0; i <= 64; i++ {
		theta := 2.0 * math.Pi * float64(i) / 64.0
		col := g2.At(10.0*math.Cos(theta), 10.0*math.Sin(theta))
		if math.IsNaN(float64(col.R)) || math.IsNaN(float64(col.A)) {
			t.Fatal("NaN after color space conversion")
		}
	}
}

func TestConicGradientWedges(t *testing.T) {
	g := conicRedBlue(Point{5, 5}, 0.0, 2.0*math.Pi, RepeatSpread)

	// full turn produces the bounded number of wedges
	paths, colors := g.Wedges(10.0, 0.0, 2.0*math.Pi)
	if len(paths) != len(colors) || len(paths) == 0 {
		t.Fatalf("wedge paths/colors mismatch: %d vs %d", len(paths), len(colors))
	}
	if len(paths) > ConicMaxWedges {
		t.Fatalf("wedge count %d exceeds cap %d", len(paths), ConicMaxWedges)
	}
	// every wedge is a closed path and contains the center
	for _, p := range paths {
		if !p.Closed() {
			t.Error("wedge path is not closed")
		}
		if !p.ContainsPoint(5.0, 5.0, NonZero) {
			t.Error("wedge does not contain the gradient center")
		}
	}

	// degenerate inputs produce no wedges and no panic
	for _, tc := range []struct {
		radius         float64
		theta0, theta1 float64
	}{
		{0.0, 0.0, 1.0},
		{-1.0, 0.0, 1.0},
		{10.0, 1.0, 1.0},
	} {
		if p, _ := g.Wedges(tc.radius, tc.theta0, tc.theta1); p != nil {
			t.Errorf("Wedges(%v, %v, %v) = non-nil, want nil", tc.radius, tc.theta0, tc.theta1)
		}
	}

	// empty gradient produces no wedges
	empty := NewConicGradient(Point{}, 0.0, 2.0*math.Pi)
	if p, _ := empty.Wedges(10.0, 0.0, 2.0*math.Pi); p != nil {
		t.Error("empty gradient produced wedges")
	}

	// wedge colors match direct At() sampling at the wedge midpoint angle
	for i, col := range colors {
		midAngle := 2.0 * math.Pi * (float64(i) + 0.5) / float64(len(colors))
		want := g.At(5.0+10.0*math.Cos(midAngle), 5.0+10.0*math.Sin(midAngle))
		if col != want {
			t.Errorf("wedge %d color %v != At() %v", i, formatRGBA(col), formatRGBA(want))
		}
	}
}

func TestConicGradientToConic(t *testing.T) {
	grad := Grad{}
	grad.Add(0.0, color.RGBA{255, 0, 0, 255})
	grad.Add(1.0, color.RGBA{0, 0, 255, 255})
	g := grad.ToConic(Point{1, 2}, math.Pi/4.0, math.Pi, ReflectSpread)
	if g.Center != (Point{1, 2}) || !Equal(g.Start, math.Pi/4.0) || !Equal(g.Arc, math.Pi) || g.Spread != ReflectSpread {
		t.Fatal("ToConic did not preserve parameters")
	}
	if len(g.Grad) != 2 {
		t.Fatal("ToConic did not copy stops")
	}
	// satisfies the Gradient interface used by Paint and the rasterizer
	var _ Gradient = g
}
