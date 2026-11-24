package rasterizer

import (
	"image"
	"testing"

	"github.com/tdewolff/canvas"
	"golang.org/x/image/draw"
)

func TestTilerClipPattern(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := canvas.New(40, 40)
	ctx := canvas.NewContext(c)
	hatch := canvas.NewLineHatch(canvas.Magenta, 45, 2, 0.6)
	ctx.SetFillPattern(hatch)
	// shape deliberately straddling many tile seams (13px tiles)
	ctx.DrawPath(5, 5, canvas.Circle(15))
	ctx.SetFillColor(canvas.Black)
	ctx.DrawPath(20, 20, canvas.Rectangle(18, 18))
	ref := fullReference(t, c, res, canvas.LinearColorSpace{})
	fullW, fullH := ref.Bounds().Dx(), ref.Bounds().Dy()
	out, _ := renderTiles(t, c, 13, 11, res, canvas.LinearColorSpace{}, draw.Over)
	if n, at := diffPixels(out, ref); n != 0 {
		t.Fatalf("clip/hatch tile mismatch: %d diffs first at %v", n, at)
	}
	_ = image.Rect(0, 0, fullW, fullH)
}
