package rasterizer

import (
	"errors"
	"image"
	"image/color"
	"math"

	"github.com/srwiley/rasterx"
	"github.com/srwiley/scanx"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"

	"github.com/tdewolff/canvas"
)

// ErrEmptyViewport is returned when a viewport or tile rectangle has zero or
// negative width or height.
var ErrEmptyViewport = errors.New("rasterizer: empty or negative viewport")

// ErrTileTooLarge is returned when a tile cannot be allocated because its
// pixel dimensions overflow or exceed the configured limit.
var ErrTileTooLarge = errors.New("rasterizer: tile dimensions too large")

// ErrCacheFull is returned when the tile cache has reached its memory limit and
// evicting least-recently-used tiles does not free enough space.
var ErrCacheFull = errors.New("rasterizer: tile cache memory limit reached")

// ErrCanceled is returned when the context passed to Render is canceled before
// or while rendering tiles.
var ErrCanceled = errors.New("rasterizer: rendering canceled")

// maxTilePixels is the hard upper bound on the number of pixels in a single
// tile unless explicitly overridden with WithMaxTilePixels. It keeps a
// misconfigured tile size from exhausting memory.
const maxTilePixels = 256 * 1024 * 1024 // 256 megapixels; 1 GiB as RGBA

// maxBackingPixels bounds the transient full-width row backing
// (virtualWidth*tileHeight pixels) used while rendering a tile. It prevents an
// extremely wide canvas from attempting an allocation that overflows or exhausts
// memory even when the visible tile itself is small.
const maxBackingPixels = 512 * 1024 * 1024 // 512 megapixels; 2 GiB as RGBA

// pixelCoverage is a safety margin (in pixels) added to layer bounds before
// deciding a layer does not touch a tile. It accounts for rasterized
// anti-aliasing, stroke joins/caps and image resampling fringes.
const pixelCoverage = 2.0

// rowSpanner sits between the full-coordinate scanx.Scanner and the tile's
// pixel buffer. The scanner accumulates coverage in global pixel coordinates
// (identical to a full-canvas render); the spanner maps each emitted span onto
// the tile's full-width row buffer, discarding spans whose scanline lies
// outside the tile. Span X coordinates are never clipped or shifted, which is
// what keeps anti-aliased coverage on and across tile seams bit-identical to
// the full-canvas rasterizer.
type rowSpanner struct {
	inner *scanx.ImgSpanner
	oy, h int
}

func (s *rowSpanner) SetColor(c interface{}) { s.inner.SetColor(c) }

func (s *rowSpanner) GetSpanFunc() scanx.SpanFunc {
	f := s.inner.GetSpanFunc()
	return func(yi, xi0, xi1 int, ma uint32) {
		ly := yi - s.oy
		if ly < 0 || ly >= s.h {
			return
		}
		// xi stays global: the backing row is as wide as the full raster, so
		// global column indices address it directly.
		f(ly, xi0, xi1, ma)
	}
}

// tileRenderer renders the whole canvas coordinate space into one tile of a
// virtual full-canvas raster.
//
// Memory layout. Instead of a full-canvas RGBA image (W*H pixels) the renderer
// allocates a backing buffer of only W*tileHeight pixels: full-width rows for
// the tile's scanline range. The scanx.Scanner keeps its normal full-height
// cellIndex (a single []int of length H, i.e. 8*H bytes, independent of width)
// and its sparse per-cell list, so coverage accumulation runs on the exact
// same global coordinates as Rasterizer. A rowSpanner redirects the resulting
// spans into the tile-height backing and drops scanlines above and below the
// tile. Because no coordinate is translated or rounded differently, every
// pixel is identical to rendering the full canvas at once.
type tileRenderer struct {
	// img is the zero-based, tile-sized image returned to callers.
	img *image.RGBA
	// backing is a full-width, tile-height row buffer. The tile window
	// [ox, ox+w) of every row is copied into img at the end.
	backing *image.RGBA

	resolution canvas.Resolution
	colorSpace canvas.ColorSpace

	// origin and size of the tile in virtual full-canvas pixel coordinates
	ox, oy int
	w, h   int
	// virtualW, virtualH are the full-canvas raster dimensions in pixels.
	virtualW, virtualH int

	spanner *rowSpanner
	scanner *scanx.Scanner

	op draw.Op
}

// newTileRenderer creates a renderer for a zero-based tile image covering the
// virtual pixel rectangle (ox, oy)-(ox+w, oy+h).
func newTileRenderer(w, h, ox, oy, virtualW, virtualH int, resolution canvas.Resolution, colorSpace canvas.ColorSpace) *tileRenderer {
	if colorSpace == nil {
		colorSpace = canvas.DefaultColorSpace
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// full-width rows, tile height; global column X addresses each row directly
	backing := image.NewRGBA(image.Rect(0, 0, virtualW, h))
	inner := scanx.NewImgSpanner(backing)
	spanner := &rowSpanner{inner: inner, oy: oy, h: h}
	spanner.inner.Op = draw.Over
	return &tileRenderer{
		img:        img,
		backing:    backing,
		resolution: resolution,
		colorSpace: colorSpace,
		ox:         ox,
		oy:         oy,
		w:          w,
		h:          h,
		virtualW:   virtualW,
		virtualH:   virtualH,
		spanner:    spanner,
		// full-width, full-height cellIndex so global coordinates accumulate
		// exactly as in the full-canvas scanner.
		scanner: scanx.NewScanner(spanner, virtualW, virtualH),
		op:      draw.Over,
	}
}

// SetOp sets the drawing operation. Either draw.Src or draw.Over.
func (r *tileRenderer) SetOp(op draw.Op) {
	r.op = op
	r.spanner.inner.Op = op
}

// Size returns the size of the virtual full canvas in millimeters.
func (r *tileRenderer) Size() (float64, float64) {
	return float64(r.virtualW) / r.resolution.DPMM(), float64(r.virtualH) / r.resolution.DPMM()
}

// tileRect is the tile rectangle in virtual full-canvas pixel coordinates.
func (r *tileRenderer) tileRect() image.Rectangle {
	return image.Rect(r.ox, r.oy, r.ox+r.w, r.oy+r.h)
}

// boundsTouchVirtual reports whether transformed canvas bounds (canvas
// millimeters, Y-up, global coordinates) intersect the tile rectangle after
// scaling to virtual full-canvas pixels (Y-down).
func (r *tileRenderer) boundsTouchVirtual(b canvas.Rect) bool {
	if b.Empty() {
		return true // unknown bounds; render to be safe
	}
	dpmm := r.resolution.DPMM()
	x0 := int(math.Floor(b.X0*dpmm - pixelCoverage))
	x1 := int(math.Ceil(b.X1*dpmm + pixelCoverage))
	y0 := int(math.Floor(float64(r.virtualH) - b.Y1*dpmm - pixelCoverage))
	y1 := int(math.Ceil(float64(r.virtualH) - b.Y0*dpmm + pixelCoverage))
	return image.Rect(x0, y0, x1, y1).Overlaps(r.tileRect())
}

// RenderPath renders a path to the tile using a style and a transformation
// matrix. It is identical to Rasterizer.RenderPath and shares its global
// coordinate system; only the spanner redirects output into the tile.
func (r *tileRenderer) RenderPath(path *canvas.Path, style canvas.Style, m canvas.Matrix) {
	bounds := canvas.Rect{}
	var fill, stroke *canvas.Path
	if style.HasFill() {
		fill = path.Copy().Transform(m)
		bounds = fill.FastBounds()
	}
	if style.HasStroke() {
		tolerance := canvas.PixelTolerance / r.resolution.DPMM()
		stroke = path
		if 0 < len(style.Dashes) {
			dashOffset, dashes := canvas.ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
			stroke = stroke.Dash(dashOffset, dashes...)
		}
		stroke = stroke.Stroke(style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, tolerance)
		stroke = stroke.Transform(m)
		if style.HasFill() {
			bounds = bounds.Add(stroke.FastBounds())
		} else {
			bounds = stroke.FastBounds()
		}
	}
	if !bounds.Empty() && !r.boundsTouchVirtual(bounds) {
		return
	}

	r.scanner.SetWinding(style.FillRule == canvas.NonZero)
	virtualH := float64(r.virtualH)

	if style.HasFill() {
		if style.Fill.IsPattern() {
			if hatch, ok := style.Fill.Pattern.(*canvas.HatchPattern); ok {
				style.Fill = hatch.Fill
				fill = hatch.Tile(fill)
			} else {
				pattern := style.Fill.Pattern.Transform(m).SetColorSpace(r.colorSpace)
				pattern.RenderTo(r, fill)
			}
		}
		if style.Fill.IsGradient() {
			mInv := m.Inv()
			gradient := style.Fill.Gradient.SetColorSpace(r.colorSpace)
			r.scanner.Clear()
			r.scanner.SetColor(rasterx.ColorFunc(func(x, y int) color.Color {
				// global virtual pixel coordinates, exactly like Rasterizer
				p := canvas.Point{(float64(x) + 0.5) / float64(r.resolution), (virtualH - float64(y+r.oy) - 0.5) / float64(r.resolution)}
				p = mInv.Dot(p)
				return gradient.At(p.X, p.Y)
			}))
			fill.ToScanxScanner(r.scanner, virtualH, r.resolution)
			r.scanner.Draw()
		} else if style.Fill.IsColor() {
			c := r.colorSpace.ToLinear(style.Fill.Color)
			r.scanner.Clear()
			r.scanner.SetColor(color.Color(r.backing.ColorModel().Convert(c)))
			fill.ToScanxScanner(r.scanner, virtualH, r.resolution)
			r.scanner.Draw()
		}
	}
	if style.HasStroke() {
		if style.Stroke.IsPattern() {
			if hatch, ok := style.Stroke.Pattern.(*canvas.HatchPattern); ok {
				style.Stroke = hatch.Fill
				stroke = hatch.Tile(stroke)
			} else {
				pattern := style.Stroke.Pattern.Transform(m).SetColorSpace(r.colorSpace)
				pattern.RenderTo(r, stroke)
			}
		}
		if style.Stroke.IsGradient() {
			mInv := m.Inv()
			gradient := style.Stroke.Gradient.SetColorSpace(r.colorSpace)
			r.scanner.Clear()
			r.scanner.SetColor(rasterx.ColorFunc(func(x, y int) color.Color {
				p := canvas.Point{(float64(x) + 0.5) / float64(r.resolution), (virtualH - float64(y+r.oy) - 0.5) / float64(r.resolution)}
				p = mInv.Dot(p)
				return gradient.At(p.X, p.Y)
			}))
			stroke.ToScanxScanner(r.scanner, virtualH, r.resolution)
			r.scanner.Draw()
		} else if style.Stroke.IsColor() {
			c := r.colorSpace.ToLinear(style.Stroke.Color)
			r.scanner.Clear()
			r.scanner.SetColor(color.Color(r.backing.ColorModel().Convert(c)))
			stroke.ToScanxScanner(r.scanner, virtualH, r.resolution)
			r.scanner.Draw()
		}
	}
}

// RenderText renders a text object to the tile using a transformation matrix.
func (r *tileRenderer) RenderText(text *canvas.Text, m canvas.Matrix) {
	if !text.Empty() && !r.boundsTouchVirtual(text.Bounds().Transform(m)) {
		return
	}
	text.RenderTo(r, m, r.resolution)
}

// RenderImage renders an image to the tile using a transformation matrix.
func (r *tileRenderer) RenderImage(img image.Image, m canvas.Matrix) {
	// add transparent margin to image for smooth borders when rotating
	margin := 0
	if (m[0][1] != 0.0 || m[1][0] != 0.0) && (m[0][0] != 0.0 || m[1][1] == 0.0) {
		// only add margin for shear transformation or rotations that are not 90/180/270 degrees
		margin = 4
		size := img.Bounds().Size()
		sp := img.Bounds().Min // starting point
		img2 := image.NewRGBA(image.Rect(0, 0, size.X+margin*2, size.Y+margin*2))
		draw.Draw(img2, image.Rect(margin, margin, size.X+margin, size.Y+margin), img, sp, draw.Over)
		img = img2
	}

	if _, ok := r.colorSpace.(canvas.LinearColorSpace); !ok {
		// gamma decompress
		changeColorSpace(img.(draw.Image), img, r.colorSpace.ToLinear)
	}

	// Draw into the zero-based full-width backing. Because every backing row
	// spans the full raster width, global column indices stay valid: the X
	// translation is therefore unchanged (b=origin.X). Only the Y translation
	// is reduced by oy so global row oy+y maps onto backing row y. The sample
	// at backing (x, y) is then exactly what the full-canvas rasterizer writes
	// at virtual (x, oy+y), and extract() picks the [ox, ox+w) column window.
	// This stays exact under rotation/scaling since only a translation term
	// changes.
	dpmm := r.resolution.DPMM()
	origin := m.Dot(canvas.Point{-float64(margin), float64(img.Bounds().Size().Y - margin)}).Mul(dpmm)
	mScaled := m.Scale(dpmm, dpmm)

	aff3 := f64.Aff3{
		mScaled[0][0], -mScaled[0][1], origin.X,
		-mScaled[1][0], mScaled[1][1], float64(r.virtualH) - origin.Y - float64(r.oy),
	}
	draw.CatmullRom.Transform(r.backing, aff3, img, img.Bounds(), r.op, nil)
}

// extract copies the tile window [ox, ox+w) x [0, h) out of the full-width
// backing into the zero-based tile image.
func (r *tileRenderer) extract() {
	for y := 0; y < r.h; y++ {
		srcOff := y*r.backing.Stride + r.ox*4
		dstOff := y * r.img.Stride
		copy(r.img.Pix[dstOff:dstOff+r.w*4], r.backing.Pix[srcOff:srcOff+r.w*4])
	}
}

// RenderTile renders the canvas layers (in z-order) into a single tile and
// returns the zero-based tile image. The returned image covers virtual pixel
// rectangle (ox, oy)-(ox+w, oy+h); edge tiles may be smaller than the nominal
// tile size. virtualW and virtualH are the full-canvas raster dimensions in
// pixels. The tile image is gamma compressed on return when a non-linear color
// space is in use, matching the full-canvas Rasterizer.Close behavior.
func RenderTile(c *canvas.Canvas, w, h, ox, oy, virtualW, virtualH int, resolution canvas.Resolution, colorSpace canvas.ColorSpace, op draw.Op) *image.RGBA {
	r := newTileRenderer(w, h, ox, oy, virtualW, virtualH, resolution, colorSpace)
	r.SetOp(op)
	// Canvas.RenderViewTo guarantees the same z-index and layer ordering as a
	// full-canvas render; tileRenderer implements canvas.Renderer.
	c.RenderViewTo(r, canvas.Identity)
	r.extract()
	if _, ok := r.colorSpace.(canvas.LinearColorSpace); !ok {
		// gamma compress
		changeColorSpace(r.img, r.img, r.colorSpace.FromLinear)
	}
	return r.img
}
