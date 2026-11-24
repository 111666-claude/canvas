package rasterizer

import (
	"context"
	"errors"
	"image"
	"image/color"
	"runtime"
	"testing"

	"golang.org/x/image/draw"

	"github.com/tdewolff/canvas"
)

// fullReference renders the canvas with the original full-canvas Rasterizer,
// the pixel-compatibility ground truth for every tiled test.
func fullReference(t *testing.T, c *canvas.Canvas, res canvas.Resolution, cs canvas.ColorSpace) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, int(c.W*res.DPMM()+0.5), int(c.H*res.DPMM()+0.5)))
	ras := FromImage(img, res, cs)
	c.RenderTo(ras)
	ras.Close()
	return img
}

// renderTiles renders every tile of the full canvas and stitches them into a
// single image of the full raster size.
func renderTiles(t *testing.T, c *canvas.Canvas, tileW, tileH int, res canvas.Resolution, cs canvas.ColorSpace, op draw.Op) (*image.RGBA, *Tiler) {
	t.Helper()
	tiler, err := NewTiler(c, tileW, tileH, res, cs, WithDrawOp(op), WithMaxCachedTiles(0), WithMaxCacheBytes(0))
	if err != nil {
		t.Fatalf("NewTiler: %v", err)
	}
	t.Cleanup(tiler.Close)
	fullW, fullH := tiler.FullSize()
	rects, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := image.NewRGBA(image.Rect(0, 0, fullW, fullH))
	for _, rect := range rects {
		img, _, ok := tiler.Tile(rect.Min.X, rect.Min.Y)
		if !ok {
			t.Fatalf("tile at %v missing", rect.Min)
		}
		draw.Draw(out, rect, img, image.Point{}, draw.Src)
	}
	return out, tiler
}

func diffPixels(a, b *image.RGBA) (n int, first image.Point) {
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				if n == 0 {
					first = image.Point{x, y}
				}
				n++
			}
		}
	}
	return
}

// buildScene constructs a canvas exercising paths, thin lines, translucent
// edges, gradients, hatch patterns, text and transformed images, with shapes
// deliberately straddling the tile boundaries.
func buildScene(t *testing.T, W, H float64) *canvas.Canvas {
	t.Helper()
	c := canvas.New(W, H)
	ctx := canvas.NewContext(c)

	// checkerboard background (opaque), cells 8mm so it crosses many tiles
	for j := 0; j*8 < int(H+8); j++ {
		for i := 0; i*8 < int(W+8); i++ {
			if (i+j)%2 == 0 {
				ctx.SetFillColor(canvas.Lightgray)
			} else {
				ctx.SetFillColor(canvas.White)
			}
			ctx.DrawPath(float64(i*8), float64(j*8), canvas.Rectangle(8.0, 8.0))
		}
	}

	// translucent overlapping circles with half-transparent edges
	ctx.SetFillColor(color.RGBA{R: 255, G: 0, B: 0, A: 128})
	ctx.DrawPath(20, 20, canvas.Circle(12))
	ctx.SetFillColor(color.RGBA{R: 0, G: 0, B: 255, A: 128})
	ctx.DrawPath(30, 25, canvas.Circle(12))

	// thin hairlines in several directions, placed across tile seams
	ctx.SetFillColor(canvas.Transparent)
	ctx.SetStrokeColor(canvas.Black)
	ctx.SetStrokeWidth(0.3)
	for _, a := range []float64{0, 30, 45, 90, 135} {
		p := &canvas.Path{}
		p.MoveTo(0, 0)
		p.LineTo(W, H)
		ctx.DrawPath(5, 5, p.Transform(canvas.Identity.Rotate(a)))
	}

	// rotated semi-transparent rectangle (non 90-degree rotation)
	ctx.ResetStyle()
	ctx.SetFillColor(color.RGBA{R: 0, G: 180, B: 0, A: 200})
	ctx.SetView(canvas.Identity.RotateAbout(23, W/2, H/2))
	ctx.DrawPath(W/2-15, H/2-8, canvas.Rectangle(30, 16))
	ctx.SetView(canvas.Identity)

	// linear gradient straddling a boundary
	grad := canvas.NewLinearGradient(canvas.Point{0, 0}, canvas.Point{40, 40})
	grad.Add(0, canvas.Red)
	grad.Add(1, canvas.Yellow)
	ctx.SetFillGradient(grad)
	ctx.DrawPath(45, 45, canvas.Rectangle(35, 20))

	// hatch pattern
	hatch := canvas.NewLineHatch(canvas.Magenta, 45, 3, 0.5)
	ctx.SetFillPattern(hatch)
	ctx.DrawPath(5, 55, canvas.Rectangle(25, 25))
	ctx.ResetStyle()

	// scaled path under a non-trivial matrix
	ctx.SetView(canvas.Identity.ScaleAbout(1.5, 0.8, 70, 30).RotateAbout(-15, 70, 30))
	ctx.SetFillColor(canvas.Teal)
	ctx.DrawPath(60, 20, canvas.Circle(8))
	ctx.SetView(canvas.Identity)

	// text
	family := canvas.NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("../../resources/DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		t.Fatalf("load font: %v", err)
	}
	face := family.Face(6.0, canvas.Black, canvas.FontRegular, canvas.FontNormal)
	text := canvas.NewTextLine(face, "Tile boundary 0123456789", canvas.Left)
	ctx.DrawText(2, H-4, text)

	// transformed image: small RGBA picture with a translucent edge and
	// rotation so the margin/catmull-rom path is exercised.
	src := image.NewRGBA(image.Rect(0, 0, 6, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			a := uint8(255)
			if x == 0 || y == 0 || x == 5 || y == 5 {
				a = 90 // translucent border
			}
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 40), G: uint8(y * 40), B: 120, A: a})
		}
	}
	ctx.SetView(canvas.Identity.RotateAbout(37, 80, 70))
	ctx.DrawImage(70, 60, src, canvas.DPMM(2.0))
	ctx.SetView(canvas.Identity)

	return c
}

func TestTilerPixelCompatLinear(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 100, 90)
	ref := fullReference(t, c, res, canvas.LinearColorSpace{})

	for _, tile := range [][2]int{{7, 7}, {16, 13}, {32, 32}, {200, 180}, {37, 29}} {
		out, _ := renderTiles(t, c, tile[0], tile[1], res, canvas.LinearColorSpace{}, draw.Over)
		if out.Bounds() != ref.Bounds() {
			t.Fatalf("tile %v: bounds %v != %v", tile, out.Bounds(), ref.Bounds())
		}
		if n, at := diffPixels(out, ref); n != 0 {
			t.Fatalf("tile %v: %d differing pixels, first at %v ref=%v got=%v", tile, n, at, ref.RGBAAt(at.X, at.Y), out.RGBAAt(at.X, at.Y))
		}
	}
}

func TestTilerPixelCompatSRGB(t *testing.T) {
	// non-linear color space exercises gamma decompress/compress per tile
	res := canvas.DPMM(1.7)
	c := buildScene(t, 80, 70)
	ref := fullReference(t, c, res, canvas.DefaultColorSpace)
	out, _ := renderTiles(t, c, 23, 19, res, canvas.DefaultColorSpace, draw.Over)
	if n, at := diffPixels(out, ref); n != 0 {
		t.Fatalf("%d differing pixels, first at %v ref=%v got=%v", n, at, ref.RGBAAt(at.X, at.Y), out.RGBAAt(at.X, at.Y))
	}
}

func TestTilerPixelCompatSrcOp(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := canvas.New(40, 40)
	ctx := canvas.NewContext(c)
	ctx.SetFillColor(color.RGBA{R: 255, A: 128})
	ctx.DrawPath(5, 5, canvas.Circle(15))
	ctx.SetFillColor(color.RGBA{B: 255, A: 128})
	ctx.DrawPath(18, 18, canvas.Circle(15))

	refImg := image.NewRGBA(image.Rect(0, 0, int(40*res.DPMM()), int(40*res.DPMM())))
	refRas := FromImage(refImg, res, canvas.LinearColorSpace{})
	refRas.SetOp(draw.Src)
	c.RenderTo(refRas)
	refRas.Close()

	tiler, err := NewTiler(c, 13, 11, res, canvas.LinearColorSpace{}, WithDrawOp(draw.Src), WithMaxCachedTiles(0))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	fullW, fullH := tiler.FullSize()
	rects, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH))
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewRGBA(image.Rect(0, 0, fullW, fullH))
	for _, rect := range rects {
		img, _, ok := tiler.Tile(rect.Min.X, rect.Min.Y)
		if !ok {
			t.Fatal("missing tile")
		}
		draw.Draw(out, rect, img, image.Point{}, draw.Src)
	}
	if n, at := diffPixels(out, refImg); n != 0 {
		t.Fatalf("draw.Src: %d differing pixels at %v", n, at)
	}
}

// TestTilerNonZeroOrigin verifies tiles whose origin is not (0,0) reproduce
// the corresponding sub-rectangle of the full reference, including edge tiles
// smaller than the configured tile size.
func TestTilerNonZeroOrigin(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 60, 50)
	ref := fullReference(t, c, res, canvas.LinearColorSpace{})

	tiler, err := NewTiler(c, 32, 32, res, canvas.LinearColorSpace{}, WithMaxCachedTiles(0))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	fullW, fullH := tiler.FullSize()
	rects, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH))
	if err != nil {
		t.Fatal(err)
	}
	for _, rect := range rects {
		img, gotRect, ok := tiler.TileAt(rect.Min.X/32, rect.Min.Y/32)
		if !ok {
			t.Fatalf("missing tile at %v", rect.Min)
		}
		if gotRect != rect {
			t.Fatalf("rect mismatch %v != %v", gotRect, rect)
		}
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				got := img.RGBAAt(x-rect.Min.X, y-rect.Min.Y)
				if got != ref.RGBAAt(x, y) {
					t.Fatalf("tile origin %v mismatch at (%d,%d): %v != %v", rect.Min, x, y, got, ref.RGBAAt(x, y))
				}
			}
		}
	}
}

// TestTilerDirtyAndViewportMove checks that only marked tiles are repainted,
// clean tiles are byte-identical across repeated Renders, and a moving
// viewport reuses the cache.
func TestTilerDirtyAndViewportMove(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 64, 64)
	tiler, err := NewTiler(c, 16, 16, res, canvas.LinearColorSpace{}, WithMaxCachedTiles(0))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	fullW, fullH := tiler.FullSize()

	if _, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH)); err != nil {
		t.Fatal(err)
	}
	if s := tiler.Stats(); s.DirtyTiles == 0 {
		t.Fatal("first render should paint dirty tiles")
	}

	// snapshot every tile, then re-render: nothing should be repainted and
	// pixels must remain identical
	snap := map[tilerKey]color.RGBA{}
	for row := 0; row*16 < fullH; row++ {
		for col := 0; col*16 < fullW; col++ {
			img, _, ok := tiler.TileAt(col, row)
			if !ok {
				t.Fatal("missing tile")
			}
			snap[tilerKey{col, row}] = img.RGBAAt(0, 0)
		}
	}
	rects, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH))
	if err != nil {
		t.Fatal(err)
	}
	if s := tiler.Stats(); s.DirtyTiles != 0 {
		t.Fatalf("clean re-render repainted %d tiles", s.DirtyTiles)
	}
	for row := 0; row*16 < fullH; row++ {
		for col := 0; col*16 < fullW; col++ {
			img, _, _ := tiler.TileAt(col, row)
			if img.RGBAAt(0, 0) != snap[tilerKey{col, row}] {
				t.Fatalf("clean tile (%d,%d) changed", col, row)
			}
		}
	}

	// mark a single dirty rectangle covering exactly one tile and repaint
	tileRect := tiler.tileRect(2, 1)
	tiler.MarkDirty(tileRect)
	if _, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH)); err != nil {
		t.Fatal(err)
	}
	if s := tiler.Stats(); s.DirtyTiles != 1 {
		t.Fatalf("expected 1 dirty tile, got %d", s.DirtyTiles)
	}

	// moving viewport: overlapping tiles are served from cache (0 dirty),
	// newly revealed tiles are painted
	vp := image.Rect(16, 16, fullW, fullH)
	if _, err := tiler.Render(context.Background(), vp); err != nil {
		t.Fatal(err)
	}
	if s := tiler.Stats(); s.DirtyTiles != 0 {
		t.Fatalf("moving viewport repainted cached tiles: %d", s.DirtyTiles)
	}
	_ = rects
}

// TestTilerOverlappingTiles renders the same region through two independent
// Tilers with different tile grids and confirms identical output.
func TestTilerOverlappingTiles(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 50, 50)
	ref := fullReference(t, c, res, canvas.LinearColorSpace{})
	a, _ := renderTiles(t, c, 17, 17, res, canvas.LinearColorSpace{}, draw.Over)
	b, _ := renderTiles(t, c, 29, 13, res, canvas.LinearColorSpace{}, draw.Over)
	if n, at := diffPixels(a, ref); n != 0 {
		t.Fatalf("grid A: %d diffs at %v", n, at)
	}
	if n, at := diffPixels(b, ref); n != 0 {
		t.Fatalf("grid B: %d diffs at %v", n, at)
	}
}

func TestTilerErrors(t *testing.T) {
	c := canvas.New(10, 10)
	res := canvas.DPMM(2.0)

	if _, err := NewTiler(c, 0, 10, res, canvas.LinearColorSpace{}); !errors.Is(err, ErrEmptyViewport) {
		t.Fatalf("zero tile width: %v", err)
	}
	if _, err := NewTiler(c, 10, -5, res, canvas.LinearColorSpace{}); !errors.Is(err, ErrEmptyViewport) {
		t.Fatalf("negative tile height: %v", err)
	}
	if _, err := NewTiler(nil, 10, 10, res, canvas.LinearColorSpace{}); !errors.Is(err, ErrEmptyViewport) {
		t.Fatalf("nil canvas: %v", err)
	}

	// oversized tile rejected up front
	if _, err := NewTiler(c, 1<<20, 1<<20, res, canvas.LinearColorSpace{}); !errors.Is(err, ErrTileTooLarge) {
		t.Fatalf("huge tile: %v", err)
	}
	if _, err := NewTiler(c, 10, 10, res, canvas.LinearColorSpace{}, WithMaxTilePixels(50)); !errors.Is(err, ErrTileTooLarge) {
		t.Fatalf("tile above custom limit: %v", err)
	}

	tiler, err := NewTiler(c, 8, 8, res, canvas.LinearColorSpace{})
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	fullW, fullH := tiler.FullSize()

	// empty / negative / fully out-of-range viewports
	for _, vp := range []image.Rectangle{
		image.Rect(0, 0, 0, 0),
		image.Rect(5, 5, 5, 10), // zero width after canonicalization
		image.Rect(fullW+10, fullH+10, fullW+20, fullH+20),
		image.Rect(-20, -20, -10, -10),
	} {
		if _, err := tiler.Render(context.Background(), vp); !errors.Is(err, ErrEmptyViewport) {
			t.Fatalf("viewport %v: %v", vp, err)
		}
	}

	// partially out-of-range viewport clips safely and renders the overlap
	if _, err := tiler.Render(context.Background(), image.Rect(-5, -5, 8, 8)); err != nil {
		t.Fatalf("clipped viewport: %v", err)
	}

	// cancellation before and during rendering
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tiler.Render(ctx, image.Rect(0, 0, fullW, fullH)); !errors.Is(err, ErrCanceled) {
		t.Fatalf("pre-canceled: %v", err)
	}

	// close is idempotent-ish and renders afterwards fail cleanly
	tiler.Close()
	if _, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH)); !errors.Is(err, ErrEmptyViewport) {
		t.Fatalf("render after close: %v", err)
	}
	if img, _, ok := tiler.Tile(0, 0); ok || img != nil {
		t.Fatal("Tile after close must fail")
	}
}

// TestTilerOversized verifies extreme dimensions degrade to explicit errors
// instead of panicking or allocating unbounded memory.
func TestTilerOversized(t *testing.T) {
	res := canvas.DPMM(2.0)

	// an extremely wide canvas: full raster width overflows the backing guard
	// even though a single visible tile would be small.
	wide := canvas.New(2e9, 10)
	if _, err := NewTiler(wide, 64, 64, res, canvas.LinearColorSpace{}); !errors.Is(err, ErrTileTooLarge) {
		t.Fatalf("extremely wide canvas: %v", err)
	}

	// extremely tall canvas: raster height overflows int32
	tall := canvas.New(10, 2e9)
	if _, err := NewTiler(tall, 64, 64, res, canvas.LinearColorSpace{}); !errors.Is(err, ErrTileTooLarge) {
		t.Fatalf("extremely tall canvas: %v", err)
	}

	// a tile whose pixel count overflows int32
	normal := canvas.New(10, 10)
	if _, err := NewTiler(normal, 1<<20, 1<<20, res, canvas.LinearColorSpace{}); !errors.Is(err, ErrTileTooLarge) {
		t.Fatalf("overflowing tile: %v", err)
	}

	// custom per-tile pixel limit is honored
	if _, err := NewTiler(normal, 100, 100, res, canvas.LinearColorSpace{}, WithMaxTilePixels(1000)); !errors.Is(err, ErrTileTooLarge) {
		t.Fatalf("custom pixel limit: %v", err)
	}
}

func TestTilerMemoryLimit(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 80, 80)
	tiler, err := NewTiler(c, 16, 16, res, canvas.LinearColorSpace{}, WithMaxCachedTiles(4))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	fullW, fullH := tiler.FullSize()

	rects, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH))
	if err != nil {
		t.Fatal(err)
	}
	stats := tiler.Stats()
	if stats.CachedTiles > 4 {
		t.Fatalf("cache grew beyond limit: %d", stats.CachedTiles)
	}
	if stats.Evictions == 0 {
		t.Fatal("expected LRU evictions")
	}
	// With a cache smaller than the grid, LRU retains only the most recently
	// rendered tiles (the tail of the row-major order); verify exactly the last
	// four requested tiles survive and earlier ones were evicted.
	wantSurvive := rects[len(rects)-4:]
	surviving := 0
	for _, rect := range rects {
		_, _, ok := tiler.Tile(rect.Min.X, rect.Min.Y)
		if ok {
			surviving++
		}
	}
	if surviving != 4 {
		t.Fatalf("expected 4 surviving tiles, got %d", surviving)
	}
	for _, rect := range wantSurvive {
		if _, _, ok := tiler.Tile(rect.Min.X, rect.Min.Y); !ok {
			t.Fatalf("most-recent tile %v should survive", rect.Min)
		}
	}

	// byte limit: a single tile larger than the budget is a hard error
	_, err = NewTiler(c, 64, 64, res, canvas.LinearColorSpace{}, WithMaxCacheBytes(100))
	if !errors.Is(err, ErrTileTooLarge) {
		t.Fatalf("tile exceeding byte budget: %v", err)
	}
}

func TestTilerClearReleasesMemory(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 40, 40)
	tiler, err := NewTiler(c, 16, 16, res, canvas.LinearColorSpace{}, WithMaxCachedTiles(0))
	if err != nil {
		t.Fatal(err)
	}
	fullW, fullH := tiler.FullSize()
	if _, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH)); err != nil {
		t.Fatal(err)
	}
	if s := tiler.Stats(); s.CachedBytes == 0 {
		t.Fatal("expected cached bytes")
	}
	tiler.Clear()
	if s := tiler.Stats(); s.CachedTiles != 0 || s.CachedBytes != 0 {
		t.Fatalf("Clear did not release cache: %+v", s)
	}
	// after Clear all tiles are dirty again and re-render works
	if _, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH)); err != nil {
		t.Fatal(err)
	}
	if s := tiler.Stats(); s.DirtyTiles == 0 {
		t.Fatal("tiles should be dirty after Clear")
	}
	tiler.Close()
	runtime.GC()
}

// TestTilerConcurrentRead exercises read-only Tile access while another
// goroutine re-renders dirty tiles; it must not race (run with -race) and must
// never hand back a nil image for a rendered tile.
func TestTilerConcurrentRead(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 48, 48)
	tiler, err := NewTiler(c, 12, 12, res, canvas.LinearColorSpace{}, WithMaxCachedTiles(8))
	if err != nil {
		t.Fatal(err)
	}
	defer tiler.Close()
	fullW, fullH := tiler.FullSize()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			tiler.MarkAllDirty()
			if _, err := tiler.Render(context.Background(), image.Rect(0, 0, fullW, fullH)); err != nil {
				return
			}
		}
	}()
	for i := 0; i < 200; i++ {
		for y := 0; y < fullH; y += 3 {
			for x := 0; x < fullW; x += 3 {
				if img, _, ok := tiler.Tile(x, y); ok && img == nil {
					t.Fatal("rendered tile returned nil image")
				}
			}
		}
	}
	<-done
}

// TestRenderTileDirect checks the low-level RenderTile entry point including
// tiles that do not start at the full raster origin.
func TestRenderTileDirect(t *testing.T) {
	res := canvas.DPMM(2.0)
	c := buildScene(t, 40, 40)
	ref := fullReference(t, c, res, canvas.LinearColorSpace{})
	fullW, fullH := ref.Bounds().Dx(), ref.Bounds().Dy()

	ox, oy := 17, 9
	tile := RenderTile(c, 15, 11, ox, oy, fullW, fullH, res, canvas.LinearColorSpace{}, draw.Over)
	for y := 0; y < 11; y++ {
		for x := 0; x < 15; x++ {
			if tile.RGBAAt(x, y) != ref.RGBAAt(ox+x, oy+y) {
				t.Fatalf("direct tile mismatch at local (%d,%d)", x, y)
			}
		}
	}
}
