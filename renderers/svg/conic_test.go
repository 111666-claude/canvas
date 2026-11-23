package svg

import (
	"bytes"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
)

func renderConicSVG(t *testing.T, g *canvas.ConicGradient) string {
	t.Helper()
	buf := &bytes.Buffer{}
	r := New(buf, 40.0, 40.0, nil)
	m := canvas.Identity.Translate(20.0, 20.0)
	r.RenderPath(canvas.Circle(10.0), canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.String()
}

func TestSVGConicGradient(t *testing.T) {
	g := canvas.NewConicGradient(canvas.Point{}, 0.0, 2.0*math.Pi)
	g.Add(0.0, color.RGBA{255, 0, 0, 255})
	g.Add(1.0, color.RGBA{0, 0, 255, 255})

	out := renderConicSVG(t, g)

	// SVG lacks native conic gradients: emit a clip path plus wedge paths
	if !strings.Contains(out, "<clipPath") {
		t.Error("output contains no clipPath for conic gradient")
	}
	if !strings.Contains(out, `clip-path="url(#`) {
		t.Error("output wedges are not clipped")
	}
	if n := strings.Count(out, "<path "); n < 100 {
		t.Errorf("expected many tessellated wedge paths, got %d", n)
	}
	if !strings.Contains(out, "</svg>") {
		t.Error("output is not a closed SVG document")
	}
}

func TestSVGConicGradientSpreadVariants(t *testing.T) {
	for _, spread := range []canvas.Spread{canvas.PadSpread, canvas.RepeatSpread, canvas.ReflectSpread} {
		g := canvas.NewConicGradient(canvas.Point{}, 0.0, math.Pi/2.0)
		g.Spread = spread
		g.Add(0.0, color.RGBA{255, 0, 0, 255})
		g.Add(1.0, color.RGBA{0, 255, 0, 255})

		out := renderConicSVG(t, g)
		if !strings.Contains(out, "<clipPath") {
			t.Errorf("spread %v: no clipPath emitted", spread)
		}
		if strings.Count(out, "<path ") < 100 {
			t.Errorf("spread %v: wedges missing", spread)
		}
	}
}

func TestSVGConicGradientStroke(t *testing.T) {
	g := canvas.NewConicGradient(canvas.Point{}, 0.0, 2.0*math.Pi)
	g.Add(0.0, color.RGBA{255, 0, 0, 255})
	g.Add(1.0, color.RGBA{0, 0, 255, 255})

	buf := &bytes.Buffer{}
	r := New(buf, 40.0, 40.0, nil)
	m := canvas.Identity.Translate(20.0, 20.0)
	style := canvas.Style{
		Stroke:      canvas.Paint{Gradient: g},
		StrokeWidth: 2.0,
	}
	r.RenderPath(canvas.Circle(8.0), style, m)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "<clipPath") {
		t.Error("stroked conic gradient emitted no clipPath")
	}
}

func TestSVGConicGradientEmpty(t *testing.T) {
	// an empty (stop-less) conic gradient degrades to nothing rather than
	// emitting an empty/broken clip group
	g := canvas.NewConicGradient(canvas.Point{}, 0.0, 2.0*math.Pi)
	out := renderConicSVG(t, g)
	if strings.Contains(out, "<clipPath") {
		t.Error("empty conic gradient should not emit a clipPath")
	}
	if !strings.Contains(out, "</svg>") {
		t.Error("output is not a closed SVG document")
	}
}

func TestSVGConicGradientUniqueClipIDs(t *testing.T) {
	g := canvas.NewConicGradient(canvas.Point{}, 0.0, 2.0*math.Pi)
	g.Add(0.0, color.RGBA{255, 0, 0, 255})
	g.Add(1.0, color.RGBA{0, 0, 255, 255})

	buf := &bytes.Buffer{}
	r := New(buf, 40.0, 40.0, nil)
	m := canvas.Identity.Translate(20.0, 20.0)
	r.RenderPath(canvas.Circle(5.0), canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
	r.RenderPath(canvas.Circle(8.0), canvas.Style{Fill: canvas.Paint{Gradient: g}}, m)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	out := buf.String()
	if n := strings.Count(out, "<clipPath"); n != 2 {
		t.Errorf("expected 2 unique clipPaths, got %d", n)
	}
}
