package rasterizer

import (
	"image"
	"image/color"
	"math"

	"github.com/srwiley/rasterx"
	"github.com/srwiley/scanx"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"

	"github.com/tdewolff/canvas"
)

// TODO: add ASM optimized version for NRGBA images, since those are much faster to write as PNG

// Draw draws the canvas on a new image with given resolution (in dots-per-millimeter). Higher resolution will result in larger images.
func Draw(c *canvas.Canvas, resolution canvas.Resolution, colorSpace canvas.ColorSpace) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, int(c.W*resolution.DPMM()+0.5), int(c.H*resolution.DPMM()+0.5)))
	ras := FromImage(img, resolution, colorSpace)
	c.RenderTo(ras)
	ras.Close()
	return img
}

// Rasterizer is a rasterizing renderer.
type Rasterizer struct {
	draw.Image
	resolution canvas.Resolution
	colorSpace canvas.ColorSpace
	mask       *canvas.Mask

	spanner *scanx.ImgSpanner
	scanner *scanx.Scanner
}

// New returns a renderer that draws to a rasterized image. The final width and height of the image is the width and height (mm) multiplied by the resolution (px/mm), thus a higher resolution results in larger images. By default the linear color space is used, which assumes input and output colors are in linearRGB. If the sRGB color space is used for drawing with an average of gamma=2.2, the input and output colors are assumed to be in sRGB (a common assumption) and blending happens in linearRGB. Be aware that for text this results in thin stems for black-on-white (but wide stems for white-on-black).
func New(width, height float64, resolution canvas.Resolution, colorSpace canvas.ColorSpace) *Rasterizer {
	img := image.NewRGBA(image.Rect(0, 0, int(width*resolution.DPMM()+0.5), int(height*resolution.DPMM()+0.5)))
	return FromImage(img, resolution, colorSpace)
}

// FromImage returns a renderer that draws to an existing image. Resolution is in pixels per unit of canvas coordinates (millimeters). A higher resolution will give a larger and more detailed image.
func FromImage(img draw.Image, resolution canvas.Resolution, colorSpace canvas.ColorSpace) *Rasterizer {
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		panic("raster size is zero, increase resolution")
	} else if math.MaxInt32/bounds.Dx() < bounds.Dy() {
		panic("raster size overflow, decrease resolution")
		//} else if _, ok := img.(*image.RGBA); !ok {
		//	panic(fmt.Errorf("invalid image type: %T != *image.RGBA", img))
	}

	if colorSpace == nil {
		colorSpace = canvas.DefaultColorSpace
	}
	spanner := scanx.NewImgSpanner(img)
	return &Rasterizer{
		Image:      img,
		resolution: resolution,
		colorSpace: colorSpace,

		spanner: spanner,
		scanner: scanx.NewScanner(spanner, bounds.Dx(), bounds.Dy()),
	}
}

func (r *Rasterizer) Close() {
	if _, ok := r.colorSpace.(canvas.LinearColorSpace); !ok {
		// gamma compress
		changeColorSpace(r.Image, r.Image, r.colorSpace.FromLinear)
	}
}

// SetOp sets the drawing operation. Either draw.Src or draw.Over.
func (r *Rasterizer) SetOp(op draw.Op) {
	r.spanner.Op = op
}

// SetMask sets the mask to be applied to subsequently rendered layers. A nil mask clears the current mask. It implements the canvas.MaskRenderer interface.
func (r *Rasterizer) SetMask(mask *canvas.Mask) {
	r.mask = mask
}

// renderMasked renders f (a path, text or image) to an offscreen layer and composites it onto the target image through the current mask. The masked content is thus rendered as an independent transparency group before the mask is applied.
func (r *Rasterizer) renderMasked(f func(*Rasterizer)) {
	mask := r.mask
	size := r.Bounds().Size()
	dpmm := r.resolution.DPMM()

	// render the masked content to an offscreen layer
	srcImg := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	srcRas := FromImage(srcImg, r.resolution, r.colorSpace)
	f(srcRas)

	// mask region in pixels: intersection of the regions of all chained masks
	hmm := float64(size.Y) / dpmm
	x0, y0, x1, y1 := 0, 0, size.X, size.Y
	for mk := mask; mk != nil; mk = mk.Next {
		mx0 := int(math.Floor(mk.Rect.X0 * dpmm))
		mx1 := int(math.Ceil(mk.Rect.X1 * dpmm))
		my0 := int(math.Floor((hmm - mk.Rect.Y1) * dpmm))
		my1 := int(math.Ceil((hmm - mk.Rect.Y0) * dpmm))
		x0 = max(x0, mx0)
		y0 = max(y0, my0)
		x1 = min(x1, mx1)
		y1 = min(y1, my1)
	}
	x0 = min(max(x0, 0), size.X)
	y0 = min(max(y0, 0), size.Y)
	x1 = min(max(x1, 0), size.X)
	y1 = min(max(y1, 0), size.Y)
	if x1 <= x0 || y1 <= y0 {
		return // mask region is empty or outside the canvas
	}

	// accumulate the mask values of all chained masks
	w, h := x1-x0, y1-y0
	acc := make([]uint32, w*h)
	for i := range acc {
		acc[i] = 255
	}
	for mk := mask; mk != nil; mk = mk.Next {
		maskImg := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
		maskRas := FromImage(maskImg, r.resolution, r.colorSpace)
		if mk.Canvas != nil {
			mk.Canvas.RenderViewTo(maskRas, canvas.Identity)
		}
		i := 0
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				acc[i] = acc[i] * r.maskValueAt(maskImg, x, y, mk.Luminance) / 255
				i++
			}
		}
	}

	// apply the mask to the offscreen layer and composite over the target
	i := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			a := acc[i]
			i++
			j := srcImg.PixOffset(x, y)
			srcImg.Pix[j+0] = uint8((uint32(srcImg.Pix[j+0])*a + 127) / 255)
			srcImg.Pix[j+1] = uint8((uint32(srcImg.Pix[j+1])*a + 127) / 255)
			srcImg.Pix[j+2] = uint8((uint32(srcImg.Pix[j+2])*a + 127) / 255)
			srcImg.Pix[j+3] = uint8((uint32(srcImg.Pix[j+3])*a + 127) / 255)
		}
	}
	draw.Draw(r.Image, image.Rect(x0, y0, x1, y1), srcImg, image.Point{X: x0, Y: y0}, draw.Over)
}

// maskValueAt returns the mask value (0-255) of the mask image at (x,y). For luminance masks the luminance of the (unpremultiplied) color is multiplied by its alpha, otherwise only the alpha channel is used.
func (r *Rasterizer) maskValueAt(img *image.RGBA, x, y int, luminance bool) uint32 {
	i := img.PixOffset(x, y)
	a := uint32(img.Pix[i+3])
	if !luminance || a == 0 {
		return a
	}
	// unpremultiply and compute the luminance in the working color space
	R := uint32(img.Pix[i+0]) * 255 / a
	G := uint32(img.Pix[i+1]) * 255 / a
	B := uint32(img.Pix[i+2]) * 255 / a
	lum := (2126*R + 7152*G + 722*B + 5000) / 10000
	return lum * a / 255
}

// Size returns the size of the canvas in millimeters.
func (r *Rasterizer) Size() (float64, float64) {
	size := r.Bounds().Size()
	return float64(size.X) / r.resolution.DPMM(), float64(size.Y) / r.resolution.DPMM()
}

// RenderPath renders a path to the canvas using a style and a transformation matrix.
func (r *Rasterizer) RenderPath(path *canvas.Path, style canvas.Style, m canvas.Matrix) {
	if r.mask != nil {
		r.renderMasked(func(sub *Rasterizer) {
			sub.RenderPath(path, style, m)
		})
		return
	}
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

	r.scanner.SetWinding(style.FillRule == canvas.NonZero)

	size := r.Bounds().Size()
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
				p := canvas.Point{(float64(x) + 0.5) / float64(r.resolution), (float64(size.Y-y) - 0.5) / float64(r.resolution)}
				p = mInv.Dot(p)
				return gradient.At(p.X, p.Y)
			}))
			fill.ToScanxScanner(r.scanner, float64(size.Y), r.resolution)
			r.scanner.Draw()
		} else if style.Fill.IsColor() {
			c := r.colorSpace.ToLinear(style.Fill.Color)
			r.scanner.Clear()
			r.scanner.SetColor(color.Color(r.Image.ColorModel().Convert(c)))
			fill.ToScanxScanner(r.scanner, float64(size.Y), r.resolution)
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
				p := canvas.Point{(float64(x) + 0.5) / float64(r.resolution), (float64(size.Y-y) - 0.5) / float64(r.resolution)}
				p = mInv.Dot(p)
				return gradient.At(p.X, p.Y)
			}))
			stroke.ToScanxScanner(r.scanner, float64(size.Y), r.resolution)
			r.scanner.Draw()
		} else if style.Stroke.IsColor() {
			c := r.colorSpace.ToLinear(style.Stroke.Color)
			r.scanner.Clear()
			r.scanner.SetColor(color.Color(r.Image.ColorModel().Convert(c)))
			stroke.ToScanxScanner(r.scanner, float64(size.Y), r.resolution)
			r.scanner.Draw()
		}
	}
}

// RenderText renders a text object to the canvas using a transformation matrix.
func (r *Rasterizer) RenderText(text *canvas.Text, m canvas.Matrix) {
	if r.mask != nil {
		r.renderMasked(func(sub *Rasterizer) {
			sub.RenderText(text, m)
		})
		return
	}
	text.RenderTo(r, m, r.resolution)
}

// RenderImage renders an image to the canvas using a transformation matrix.
func (r *Rasterizer) RenderImage(img image.Image, m canvas.Matrix) {
	if r.mask != nil {
		r.renderMasked(func(sub *Rasterizer) {
			sub.RenderImage(img, m)
		})
		return
	}
	// add transparent margin to image for smooth borders when rotating
	// TODO: optimize when transformation is only translation or stretch (if optimizing, dont overwrite original img when gamma correcting)
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

	// draw to destination image
	// note that we need to correct for the added margin in origin and m
	dpmm := r.resolution.DPMM()
	origin := m.Dot(canvas.Point{-float64(margin), float64(img.Bounds().Size().Y - margin)}).Mul(dpmm)
	m = m.Scale(dpmm, dpmm)

	h := float64(r.Bounds().Size().Y)
	aff3 := f64.Aff3{m[0][0], -m[0][1], origin.X, -m[1][0], m[1][1], h - origin.Y}
	draw.CatmullRom.Transform(r, aff3, img, img.Bounds(), draw.Over, nil)
}
