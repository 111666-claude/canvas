package pdf

import (
	"bytes"
	"testing"

	"github.com/tdewolff/canvas"
)

func TestPDFFontFallback(t *testing.T) {
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
	text := canvas.NewTextLine(face, "A世☃", canvas.Left)

	// mixed-font spans are split into per-font runs, both fonts are embedded
	var buf bytes.Buffer
	r := New(&buf, text.Width, text.Height, nil)
	r.RenderText(text, canvas.Identity)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected PDF output")
	}
	if len(r.w.pdf.fontsH) != 2 {
		t.Fatalf("expected 2 fonts to be used, got %d", len(r.w.pdf.fontsH))
	}
}
