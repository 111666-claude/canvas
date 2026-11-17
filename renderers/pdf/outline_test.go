package pdf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/test"
)

func TestPDFOutlineText(t *testing.T) {
	dejaVuSerif := canvas.NewFontFamily("dejavu-serif")
	err := dejaVuSerif.LoadFontFile(fontDir+"DejaVuSerif.ttf", canvas.FontRegular)
	test.Error(t, err)
	face := dejaVuSerif.Face(12.0, canvas.Black, canvas.FontRegular, canvas.FontNormal, canvas.FontUnderline)

	txt := canvas.NewTextLine(face, "outline\ntext", canvas.Left)
	c := canvas.New(txt.Width, txt.Height)
	c.RenderText(txt, canvas.Identity)

	// outlined output: no embedded font program and no text operators
	buf := &bytes.Buffer{}
	opts := DefaultOptions
	opts.OutlineText = true
	opts.Compress = false // keep the content stream readable for the checks
	r := New(buf, c.W, c.H, &opts)
	c.RenderTo(r)
	test.Error(t, r.Close())
	out := buf.String()

	test.That(t, !strings.Contains(out, "FontFile2"))
	test.That(t, !strings.Contains(out, "Tf"))

	// regular output embeds the font and uses a text object
	bufRegular := &bytes.Buffer{}
	optsRegular := DefaultOptions
	optsRegular.Compress = false
	rRegular := New(bufRegular, c.W, c.H, &optsRegular)
	c.RenderTo(rRegular)
	test.Error(t, rRegular.Close())
	regular := bufRegular.String()
	test.That(t, strings.Contains(regular, "FontFile2"))
	test.That(t, strings.Contains(regular, "Tf"))
}
