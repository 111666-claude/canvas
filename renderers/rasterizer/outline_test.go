package rasterizer

import (
	"image"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/font"
	"github.com/tdewolff/test"
)

const ptPerMm = 72.0 / 25.4

func loadOutlineFace(t *testing.T) *canvas.FontFace {
	t.Helper()
	family := canvas.NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("../../resources/DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		test.Error(t, err)
	}
	face := family.Face(12.0*ptPerMm, canvas.Black)
	// disable grid alignment so the text and outline pipelines are exact matches
	face.Hinting = font.NoHinting
	return face
}

func rasterize(t *testing.T, c *canvas.Canvas) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, int(c.W*20.0+0.5), int(c.H*20.0+0.5)))
	r := FromImage(img, 20.0, canvas.DefaultColorSpace)
	c.RenderTo(r)
	r.Close()
	return img
}

func TestRasterizerOutlineText(t *testing.T) {
	face := loadOutlineFace(t)
	txt := canvas.NewTextLine(face, "raster outline\nsecond line", canvas.Left)

	// ordinary text rendering
	cText := canvas.New(txt.Width, txt.Height)
	cText.RenderText(txt, canvas.Identity)
	imgText := rasterize(t, cText)

	// outline-only rendering: the canvas holds only path layers, no text
	cOutline, err := txt.ToCanvas()
	test.Error(t, err)
	test.That(t, cOutline.Empty() == false)
	imgOutline := rasterize(t, cOutline)

	test.T(t, imgOutline.Bounds(), imgText.Bounds())
	b := imgText.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			test.T(t, imgOutline.RGBAAt(x, y), imgText.RGBAAt(x, y))
		}
	}
}

func TestRasterizerOutlineTextTransform(t *testing.T) {
	face := loadOutlineFace(t)
	txt := canvas.NewTextLine(face, "transformed", canvas.Left)
	m := canvas.Identity.Translate(3.0, 2.0).Rotate(0.25).Scale(1.2, 1.2)

	cText := canvas.New(40.0, 40.0)
	cText.RenderText(txt, m)
	imgText := rasterize(t, cText)

	cOutline := canvas.New(40.0, 40.0)
	test.Error(t, txt.RenderPathsTo(cOutline, m, 0.0))
	imgOutline := rasterize(t, cOutline)

	b := imgText.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			test.T(t, imgOutline.RGBAAt(x, y), imgText.RGBAAt(x, y))
		}
	}
}
