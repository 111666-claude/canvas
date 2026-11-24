package svg

import (
	"bytes"
	"image/color"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
)

func renderSVG(t *testing.T, gradient canvas.Gradient) string {
	t.Helper()
	c := canvas.New(100, 100)
	c.RenderPath(canvas.Rectangle(100, 100), canvas.Style{Fill: canvas.Paint{Gradient: gradient}}, canvas.Identity)

	buf := &bytes.Buffer{}
	svg := New(buf, 100, 100, nil)
	c.RenderTo(svg)
	if err := svg.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.String()
}

// TestSVGGradientSpread checks that the spread method and linear-light color
// interpolation are emitted, while the defaults preserve historical output.
func TestSVGGradientSpread(t *testing.T) {
	base := canvas.NewLinearGradient(canvas.Point{10, 50}, canvas.Point{90, 50})
	base.Add(0.0, color.RGBA{255, 0, 0, 255})
	base.Add(1.0, color.RGBA{0, 0, 255, 255})

	// default: no spread or interpolation attributes
	if s := renderSVG(t, base); strings.Contains(s, "spreadMethod") || strings.Contains(s, "color-interpolation") {
		t.Errorf("default gradient emitted non-default attributes: %s", s)
	}

	repeat := *base
	repeat.Spread = canvas.GradientRepeat
	if s := renderSVG(t, &repeat); !strings.Contains(s, `spreadMethod="repeat"`) {
		t.Errorf("repeat spread missing: %s", s)
	}

	reflect := *base
	reflect.Spread = canvas.GradientReflect
	if s := renderSVG(t, &reflect); !strings.Contains(s, `spreadMethod="reflect"`) {
		t.Errorf("reflect spread missing: %s", s)
	}

	linear := *base
	linear.Interpolation = canvas.GradientLinearLight
	if s := renderSVG(t, &linear); !strings.Contains(s, `color-interpolation="linearRGB"`) {
		t.Errorf("linear-light interpolation missing: %s", s)
	}
}

// TestSVGGradientStraightAlpha checks that straight-alpha stops emit an
// explicit stop-opacity while premultiplied defaults stay attribute-free.
func TestSVGGradientStraightAlpha(t *testing.T) {
	premul := canvas.NewLinearGradient(canvas.Point{0, 50}, canvas.Point{100, 50})
	premul.Add(0.0, color.RGBA{128, 0, 0, 128})
	premul.Add(1.0, color.RGBA{0, 0, 128, 128})
	if s := renderSVG(t, premul); strings.Contains(s, "stop-opacity") {
		t.Errorf("premultiplied default should not emit stop-opacity: %s", s)
	}

	straight := canvas.NewLinearGradient(canvas.Point{0, 50}, canvas.Point{100, 50})
	straight.Alpha = canvas.GradientStraight
	straight.Add(0.0, color.RGBA{128, 0, 0, 128})
	straight.Add(1.0, color.RGBA{0, 0, 128, 128})
	if s := renderSVG(t, straight); !strings.Contains(s, "stop-opacity") {
		t.Errorf("straight-alpha gradient should emit stop-opacity: %s", s)
	}
}

// TestSVGConicGradientFallback verifies a conic gradient degrades to a valid
// radial gradient definition rather than producing an empty or broken paint.
func TestSVGConicGradientFallback(t *testing.T) {
	conic := canvas.NewConicGradient(canvas.Point{50, 50}, 0.0)
	conic.Add(0.0, color.RGBA{255, 0, 0, 255})
	conic.Add(1.0, color.RGBA{0, 0, 255, 255})

	s := renderSVG(t, conic)
	if !strings.Contains(s, "<radialGradient") {
		t.Errorf("conic should fall back to radialGradient: %s", s)
	}
	if !strings.Contains(s, "url(#d0)") {
		t.Errorf("conic paint reference missing: %s", s)
	}
}

// TestSVGRadialGradientSpread ensures radial gradients carry spread attributes.
func TestSVGRadialGradientSpread(t *testing.T) {
	radial := canvas.NewRadialGradient(canvas.Point{50, 50}, 0, canvas.Point{50, 50}, 40)
	radial.Spread = canvas.GradientRepeat
	radial.Add(0.0, color.RGBA{255, 0, 0, 255})
	radial.Add(1.0, color.RGBA{0, 0, 255, 255})

	s := renderSVG(t, radial)
	if !strings.Contains(s, "<radialGradient") || !strings.Contains(s, `spreadMethod="repeat"`) {
		t.Errorf("radial repeat spread missing: %s", s)
	}
}
