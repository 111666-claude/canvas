package svg

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
)

func fallbackText(t *testing.T) *canvas.Text {
	t.Helper()
	dejavu := canvas.NewFontFamily("dejavu-serif")
	if err := dejavu.LoadFontFile("../../resources/DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	unifont := canvas.NewFontFamily("unifont")
	if err := unifont.LoadFontFile("../../resources/unifont-13.0.05.ttf", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	dejavu.SetFallbacks(unifont)
	face := dejavu.Face(12.0, canvas.Black, canvas.FontRegular, canvas.FontNormal)
	return canvas.NewTextLine(face, "A世☃", canvas.Left)
}

func TestSVGFontFallback(t *testing.T) {
	text := fallbackText(t)

	// spans with fallback glyphs are rendered as paths for consistent output
	var buf bytes.Buffer
	r := New(&buf, text.Width, text.Height, nil)
	r.RenderText(text, canvas.Identity)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "<path") {
		t.Fatal("expected fallback span to be rendered as path in SVG")
	}

	// single-font text keeps using native text elements
	dejavu := canvas.NewFontFamily("dejavu-serif")
	if err := dejavu.LoadFontFile("../../resources/DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	face := dejavu.Face(12.0, canvas.Black, canvas.FontRegular, canvas.FontNormal)
	single := canvas.NewTextLine(face, "Hello", canvas.Left)
	buf.Reset()
	r = New(&buf, single.Width, single.Height, nil)
	r.RenderText(single, canvas.Identity)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "<text") {
		t.Fatal("expected single-font text to use native text elements in SVG")
	}
}
