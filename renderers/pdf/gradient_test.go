package pdf

import (
	"bytes"
	"image/color"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
)

func renderPDF(t *testing.T, gradient canvas.Gradient) string {
	t.Helper()
	c := canvas.New(100, 100)
	c.RenderPath(canvas.Rectangle(100, 100), canvas.Style{Fill: canvas.Paint{Gradient: gradient}}, canvas.Identity)

	buf := &bytes.Buffer{}
	pdf := New(buf, 100, 100, nil)
	c.RenderTo(pdf)
	if err := pdf.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.String()
}

// TestPDFTransparentStops guards against the historical division by zero when
// a stop had alpha 0, which emitted NaN into the shading function colors.
func TestPDFTransparentStops(t *testing.T) {
	gradient := canvas.NewLinearGradient(canvas.Point{0, 50}, canvas.Point{100, 50})
	gradient.Add(0.0, color.RGBA{0, 0, 0, 0})
	gradient.Add(0.5, color.RGBA{128, 0, 0, 128})
	gradient.Add(1.0, color.RGBA{0, 0, 0, 0})

	s := renderPDF(t, gradient)
	// match only standalone numeric tokens, not substrings of names such as /Info
	for _, bad := range []string{"NaN", "+Inf", "-Inf", " Infinity", "n/a"} {
		if strings.Contains(s, bad) {
			t.Errorf("PDF shading contains invalid number %q", bad)
		}
	}
	if !strings.Contains(s, "/PatternType 2") {
		t.Error("expected a shading pattern in the PDF")
	}
}

// TestPDFConicGradientFallback verifies a conic gradient still emits a valid
// shading pattern (axial fallback) rather than an empty resource.
func TestPDFConicGradientFallback(t *testing.T) {
	conic := canvas.NewConicGradient(canvas.Point{50, 50}, 0.0)
	conic.Add(0.0, color.RGBA{255, 0, 0, 255})
	conic.Add(1.0, color.RGBA{0, 0, 255, 255})

	s := renderPDF(t, conic)
	if !strings.Contains(s, "/ShadingType 2") {
		t.Errorf("conic should fall back to an axial (type 2) shading: %s", s)
	}
	if strings.Contains(s, "NaN") || strings.Contains(s, "+Inf") || strings.Contains(s, "-Inf") {
		t.Error("conic fallback emitted invalid numbers")
	}
}

// TestPDFRadialGradient verifies radial gradients still emit type 3 shadings.
func TestPDFRadialGradient(t *testing.T) {
	radial := canvas.NewRadialGradient(canvas.Point{50, 50}, 0, canvas.Point{50, 50}, 40)
	radial.Add(0.0, color.RGBA{255, 0, 0, 255})
	radial.Add(1.0, color.RGBA{0, 0, 255, 255})

	s := renderPDF(t, radial)
	if !strings.Contains(s, "/ShadingType 3") {
		t.Errorf("radial gradient should emit a type 3 shading: %s", s)
	}
}

// TestPDFSingleStopGradient verifies a degenerate single-stop gradient does not
// crash the function builder.
func TestPDFSingleStopGradient(t *testing.T) {
	gradient := canvas.NewLinearGradient(canvas.Point{0, 50}, canvas.Point{100, 50})
	gradient.Add(0.5, color.RGBA{255, 0, 0, 255})

	s := renderPDF(t, gradient)
	if strings.Contains(s, "NaN") || strings.Contains(s, "+Inf") || strings.Contains(s, "-Inf") {
		t.Error("single-stop gradient emitted invalid numbers")
	}
}
