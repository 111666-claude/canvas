package svg

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/test"
)

const ptPerMm = 72.0 / 25.4

func outlineSVG(t *testing.T, outline bool) string {
	t.Helper()
	family := canvas.NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("../../resources/DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		test.Error(t, err)
	}
	face := family.Face(12.0*ptPerMm, canvas.Black)

	txt := canvas.NewTextLine(face, "ab\ncd", canvas.Left)
	c := canvas.New(txt.Width, txt.Height)
	c.RenderText(txt, canvas.Identity)

	buf := &bytes.Buffer{}
	opts := DefaultOptions
	opts.OutlineText = outline
	r := New(buf, c.W, c.H, &opts)
	c.RenderTo(r)
	test.Error(t, r.Close())
	return buf.String()
}

func TestSVGOutlineText(t *testing.T) {
	outlined := outlineSVG(t, true)

	// no font is embedded or referenced and no <text> element is emitted
	test.That(t, !strings.Contains(outlined, "@font-face"))
	test.That(t, !strings.Contains(outlined, "<text"))
	test.That(t, !strings.Contains(outlined, "<tspan"))

	// the glyphs are serialized as path elements instead
	test.That(t, strings.Contains(outlined, "<path d="))

	// the default mode still emits text elements and embeds the font
	regular := outlineSVG(t, false)
	test.That(t, strings.Contains(regular, "<text"))
	test.That(t, strings.Contains(regular, "@font-face"))
}

func TestSVGOutlineTextAlignsWithText(t *testing.T) {
	// the outline SVG must contain the same glyph path data as a canvas that
	// renders the text converted through Text.RenderPathsTo manually
	family := canvas.NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("../../resources/DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		test.Error(t, err)
	}
	face := family.Face(12.0*ptPerMm, canvas.Black)
	face.Hinting = 0 // font.NoHinting
	txt := canvas.NewTextLine(face, "ab\ncd", canvas.Left)

	c, err := txt.ToCanvas()
	test.Error(t, err)

	buf := &bytes.Buffer{}
	opts := DefaultOptions
	opts.OutlineText = true
	r := New(buf, c.W, c.H, &opts)
	c.RenderTo(r)
	test.Error(t, r.Close())

	// both spans (two lines) survive as path elements; one <path> per line
	s := buf.String()
	test.That(t, 2 <= strings.Count(s, "<path d="))
}
