package rasterizer

import (
	"bytes"
	"image"
	"testing"

	"github.com/tdewolff/canvas"
)

// dashedCurve returns a long curve with all segment types.
func dashedCurve() *canvas.Path {
	p := &canvas.Path{}
	p.MoveTo(5.0, 50.0)
	for i := 0; i < 20; i++ {
		x := 5.0 + float64(i)*4.5
		p.CubeTo(x+1.5, 20.0, x+3.0, 80.0, x+4.5, 50.0)
	}
	p.LineTo(95.0, 50.0)
	p.ArcTo(5.0, 8.0, 20.0, false, true, 90.0, 60.0)
	p.QuadTo(50.0, 90.0, 5.0, 50.0)
	p.Close()
	return p
}

// renderDashedFrame renders one frame of a long dashed curve with the given
// view transformation, simulating an editor frame while dragging/zooming.
func renderDashedFrame(p *canvas.Path, view canvas.Matrix) *image.RGBA {
	c := canvas.New(100.0, 100.0)
	ctx := canvas.NewContext(c)
	ctx.SetView(view)
	ctx.SetStrokeColor(canvas.Black)
	ctx.SetStrokeWidth(0.8)
	ctx.SetDashes(-1.3, 3.0, 1.5, 0.7)
	ctx.DrawPath(0.0, 0.0, p)

	ras := New(100.0, 100.0, canvas.DPMM(4.0), canvas.DefaultColorSpace)
	c.RenderTo(ras)
	ras.Close()
	return ras.RGBA
}

func TestDashedStrokePixelConsistency(t *testing.T) {
	p := dashedCurve()
	views := []canvas.Matrix{
		canvas.Identity,
		canvas.Identity.Scale(1.01, 1.01).Translate(0.1, 0.0),
		canvas.Identity.Scale(1.07, 1.07).Translate(0.7, 0.0),
	}

	// render each view with a cold cache (no reuse possible) and with a warm
	// cache (reusing the dashed and stroked outline from previous frames)
	cold := make([]*image.RGBA, len(views))
	warm := make([]*image.RGBA, len(views))
	for i, view := range views {
		canvas.ClearDashCache()
		cold[i] = renderDashedFrame(p, view)
	}
	canvas.ClearDashCache()
	for rep := 0; rep < 3; rep++ {
		for i, view := range views {
			warm[i] = renderDashedFrame(p, view)
		}
	}

	blank := image.NewRGBA(cold[0].Bounds())
	for i := range views {
		if bytes.Equal(cold[i].Pix, blank.Pix) {
			t.Fatalf("frame %d is blank", i)
		}
		if !bytes.Equal(cold[i].Pix, warm[i].Pix) {
			t.Fatalf("frame %d: cached render differs from cold-cache render", i)
		}
	}
}

func BenchmarkDashedRenderUncached(b *testing.B) {
	p := dashedCurve()
	view := canvas.Identity
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		canvas.ClearDashCache()
		renderDashedFrame(p, view)
	}
}

func BenchmarkDashedRenderCached(b *testing.B) {
	p := dashedCurve()
	view := canvas.Identity
	canvas.ClearDashCache()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderDashedFrame(p, view)
	}
}

func BenchmarkDashedRenderCachedTransform(b *testing.B) {
	p := dashedCurve()
	canvas.ClearDashCache()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// simulate zooming/panning: the view changes every frame
		view := canvas.Identity.Scale(1.0+0.001*float64(i), 1.0+0.001*float64(i)).Translate(0.01*float64(i), 0.0)
		renderDashedFrame(p, view)
	}
}
