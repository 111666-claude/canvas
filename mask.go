package canvas

// MaskRenderer is an optional interface implemented by renderers that support masking. Canvas.RenderViewTo will call SetMask before rendering a masked layer and SetMask(nil) afterwards. Renderers that do not implement this interface will render masked layers without their mask.
type MaskRenderer interface {
	Renderer
	// SetMask sets the mask to be applied to subsequently rendered layers. A nil mask clears the current mask.
	SetMask(*Mask)
}

// Mask defines a transparency mask for a canvas layer. The mask's canvas holds the mask content (paths, text, images, gradients) expressed in the same coordinate space as the canvas that contains the masked layers. Rect is the mask region in that same coordinate space; everything outside the region is fully masked out. When Luminance is set, the mask value of a pixel is its luminance multiplied by its alpha (the SVG default), otherwise only the alpha channel is used. Masks can be chained with Next, in which case the mask values multiply (e.g. a group mask on top of an element mask).
type Mask struct {
	Canvas    *Canvas // mask content in the coordinate space of the parent canvas
	Rect      Rect    // mask region in the coordinate space of the parent canvas
	Luminance bool    // use luminance-to-alpha instead of alpha only
	Next      *Mask   // chained masks multiply their mask values
}

// Transform returns a copy of the mask transformed by the given matrix. A nil mask or the identity matrix returns the mask itself.
func (mask *Mask) Transform(m Matrix) *Mask {
	if mask == nil || m.IsIdentity() || mask.Canvas == nil {
		return mask
	}
	c := New(mask.Canvas.W, mask.Canvas.H)
	for zindex, layers := range mask.Canvas.layers {
		for _, l := range layers {
			l.m = m.Mul(l.m)
			c.layers[zindex] = append(c.layers[zindex], l)
		}
	}
	return &Mask{
		Canvas:    c,
		Rect:      mask.Rect.Transform(m),
		Luminance: mask.Luminance,
		Next:      mask.Next.Transform(m),
	}
}
