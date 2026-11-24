package rasterizer

import (
	"context"
	"image"
	"image/color"
	"runtime"
	"testing"

	"golang.org/x/image/draw"

	"github.com/tdewolff/canvas"
)

// benchScene builds a wide, tall canvas with many vector layers so a full
// raster is large while a single viewport covers only a few tiles.
func benchScene(t testing.TB, W, H float64) *canvas.Canvas {
	t.Helper()
	c := canvas.New(W, H)
	ctx := canvas.NewContext(c)
	// checkerboard
	for j := 0; float64(j)*4 < H; j++ {
		for i := 0; float64(i)*4 < W; i++ {
			if (i+j)%2 == 0 {
				ctx.SetFillColor(canvas.Lightgray)
			} else {
				ctx.SetFillColor(canvas.White)
			}
			ctx.DrawPath(float64(i*4), float64(j*4), canvas.Rectangle(4, 4))
		}
	}
	// translucent circles and hairlines spread across the whole canvas
	for k := 0; float64(k)*20 < W; k++ {
		ctx.SetFillColor(color.RGBA{R: 200, A: 120})
		ctx.DrawPath(float64(k*20)+5, float64(k%8)*20+5, canvas.Circle(6))
		ctx.SetFillColor(canvas.Transparent)
		ctx.SetStrokeColor(canvas.Black)
		ctx.SetStrokeWidth(0.3)
		p := &canvas.Path{}
		p.MoveTo(0, 0)
		p.LineTo(W, H)
		ctx.DrawPath(float64(k*20), 0, p)
	}
	return c
}

func allocMB() float64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	return float64(m.TotalAlloc) / 1e6
}

// BenchmarkFullRaster measures the original whole-canvas rasterizer on a
// large canvas.
func BenchmarkFullRaster(b *testing.B) {
	res := canvas.DPMM(2.0)
	c := benchScene(b, 400, 400)
	b.ReportAllocs()
	b.ResetTimer()
	var img *image.RGBA
	for i := 0; i < b.N; i++ {
		img = Draw(c, res, canvas.LinearColorSpace{})
	}
	_ = img
}

// BenchmarkTilerViewport measures the motivating workflow: a reusable Tiler
// that renders a small viewport of a large canvas. The first call paints four
// cold tiles; the sub-benchmark body then re-issues the same viewport, which is
// the steady-state cost a panning UI pays once its initial frame is drawn.
func BenchmarkTilerViewport(b *testing.B) {
	res := canvas.DPMM(2.0)
	c := benchScene(b, 400, 400)
	tileW, tileH := 128, 128
	tiler, err := NewTiler(c, tileW, tileH, res, canvas.LinearColorSpace{})
	if err != nil {
		b.Fatal(err)
	}
	defer tiler.Close()
	vp := image.Rect(0, 0, tileW*2, tileH*2)
	// prime the cache so the benchmark isolates the steady-state viewport cost
	if _, err := tiler.Render(context.Background(), vp); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tiler.Render(context.Background(), vp); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTilerColdViewport measures a single cold viewport render (cache
// miss on all four tiles) against the full-canvas rasterizer, using a fresh
// Tiler per iteration to avoid any cache reuse.
func BenchmarkTilerColdViewport(b *testing.B) {
	res := canvas.DPMM(2.0)
	c := benchScene(b, 400, 400)
	tileW, tileH := 128, 128
	vp := image.Rect(0, 0, tileW*2, tileH*2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tiler, err := NewTiler(c, tileW, tileH, res, canvas.LinearColorSpace{})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := tiler.Render(context.Background(), vp); err != nil {
			tiler.Close()
			b.Fatal(err)
		}
		tiler.Close()
	}
}

// BenchmarkTilerCacheHit measures a repeated Render of an unchanged viewport:
// every tile is served from cache, so no vector rasterization should occur.
func BenchmarkTilerCacheHit(b *testing.B) {
	res := canvas.DPMM(2.0)
	c := benchScene(b, 400, 400)
	tiler, err := NewTiler(c, 128, 128, res, canvas.LinearColorSpace{})
	if err != nil {
		b.Fatal(err)
	}
	defer tiler.Close()
	vp := image.Rect(0, 0, 256, 256)
	if _, err := tiler.Render(context.Background(), vp); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tiler.Render(context.Background(), vp); err != nil {
			b.Fatal(err)
		}
	}
}

// TestTilerMemoryReport reports peak memory and dirty-tile counts for the tile
// path against the full rasterizer, using the requested checkerboard, hairline
// and translucent-edge fixtures.
func TestTilerMemoryReport(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 200, 200)
	fullW, fullH := int(200*res.DPMM()+0.5), int(200*res.DPMM()+0.5)

	// original full-canvas rasterizer
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	full := Draw(c, res, canvas.LinearColorSpace{})
	var afterFull runtime.MemStats
	runtime.ReadMemStats(&afterFull)
	fullBytes := len(full.Pix)

	// tiler rendering a small moving viewport
	const tileW, tileH = 64, 64
	tiler, err := NewTiler(c, tileW, tileH, res, canvas.LinearColorSpace{},
		WithMaxCachedTiles(16))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()

	vp := image.Rect(0, 0, tileW*2, tileH*2)
	if _, err := tiler.Render(context.Background(), vp); err != nil {
		t.Fatal(err)
	}
	first := tiler.Stats()

	// move the viewport across the canvas several times; only newly revealed
	// (or evicted) tiles should be painted.
	totalDirty := first.DirtyTiles
	for step := 1; step <= 4; step++ {
		moved := image.Rect(step*tileW, step*tileH, step*tileW+tileW*2, step*tileH+tileH*2)
		if _, err := tiler.Render(context.Background(), moved); err != nil {
			t.Fatal(err)
		}
		totalDirty += tiler.Stats().DirtyTiles
	}
	stats := tiler.Stats()
	var afterTile runtime.MemStats
	runtime.ReadMemStats(&afterTile)

	t.Logf("full raster: %dx%d = %.1f MB RGBA", fullW, fullH, float64(fullBytes)/1e6)
	t.Logf("tile size: %dx%d; viewport tiles per render: 4", tileW, tileH)
	t.Logf("first render dirty tiles: %d; cumulative dirty over 5 viewports: %d", first.DirtyTiles, totalDirty)
	t.Logf("cache: %d tiles, %.2f MB current, %.2f MB peak, %d evictions",
		stats.CachedTiles, float64(stats.CachedBytes)/1e6, float64(stats.PeakBytes)/1e6, stats.Evictions)
	t.Logf("heap delta full raster: %.1f MB; heap delta tiler session: %.1f MB",
		float64(afterFull.TotalAlloc-before.TotalAlloc)/1e6,
		float64(afterTile.TotalAlloc-before.TotalAlloc)/1e6)

	// hard guarantees for the report to stay meaningful
	if stats.CachedTiles > 16 {
		t.Fatalf("cache exceeded limit: %d", stats.CachedTiles)
	}
	if stats.PeakBytes == 0 {
		t.Fatal("expected non-zero peak cache bytes")
	}
	if fullBytes <= stats.PeakBytes {
		t.Fatalf("tile peak (%.2f MB) should be below full raster (%.2f MB)",
			float64(stats.PeakBytes)/1e6, float64(fullBytes)/1e6)
	}
}

// TestTilerStitchFullCanvas renders every tile through the Tiler and stitches
// them, asserting the result is identical to a full-canvas render (end-to-end
// check of caching, dirty tracking and extract together).
func TestTilerStitchFullCanvas(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 60, 50)
	ref := fullReference(t, c, res, canvas.LinearColorSpace{})
	fullW, fullH := ref.Bounds().Dx(), ref.Bounds().Dy()

	tiler, err := NewTiler(c, 29, 23, res, canvas.LinearColorSpace{}, WithMaxCachedTiles(0))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	rects, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH))
	if err != nil {
		t.Fatal(err)
	}
	stitched := image.NewRGBA(image.Rect(0, 0, fullW, fullH))
	for _, rect := range rects {
		img, _, ok := tiler.Tile(rect.Min.X, rect.Min.Y)
		if !ok {
			t.Fatalf("missing tile at %v", rect.Min)
		}
		draw.Draw(stitched, rect, img, image.Point{}, draw.Src)
	}
	if n, at := diffPixels(stitched, ref); n != 0 {
		t.Fatalf("stitched canvas has %d diffs, first at %v", n, at)
	}
}
