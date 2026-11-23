package canvas

import (
	"math"

	"github.com/tdewolff/parse/v2"
)

// clipPathDef holds the parsed contents of an SVG <clipPath> element. The
// contents are drawn in the user coordinate system in place at the time when
// the clipPath element is referenced (i.e. the user space of the element
// carrying the clip-path attribute), adjusted by the clipPath's own transform.
type clipPathDef struct {
	units    string // userSpaceOnUse (default) or objectBoundingBox
	clipPath []clipGeometry
	clipRef  string // clip-path reference on the clipPath element (nested clipping)
}

// clipGeometry is one rendered child of a clipPath: a path in the clipPath's
// local user space together with the transformation that maps that local space
// onto the user space of the referencing element, and the fill rule applying
// to this child.
type clipGeometry struct {
	path     *Path
	m        Matrix
	fillRule FillRule
}

// finiteMatrix returns true when all entries of the affine matrix are finite.
func finiteMatrix(m Matrix) bool {
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			if math.IsNaN(m[i][j]) || math.IsInf(m[i][j], 0) {
				return false
			}
		}
	}
	return true
}

// clipError records a contextual parse error related to clipping without
// overwriting an earlier error.
func (svg *svgParser) clipError(format string, args ...interface{}) {
	if svg.err == nil {
		svg.err = parse.NewErrorLexer(svg.z, format, args...)
	}
}

// parseClipPath parses an <clipPath> definition tag into a clipPathDef. Its
// child shapes are rendered to a temporary canvas using the same machinery as
// regular SVG content so that nested paths, groups and their transforms are
// handled uniformly. nil is returned when the definition is invalid (e.g. an
// unsupported clipPathUnits value); references to such a definition hide the
// clipped content just like an empty clip path.
func (svg *svgParser) parseClipPath(tag *svgTag) *clipPathDef {
	units := tag.attrs["clipPathUnits"]
	switch units {
	case "", "userSpaceOnUse":
		units = "userSpaceOnUse"
	case "objectBoundingBox":
	default:
		svg.clipError("invalid clipPathUnits: %s", units)
		return nil
	}

	def := &clipPathDef{
		units: units,
	}
	if v, ok := tag.attrs["clip-path"]; ok {
		def.clipRef = svg.parseUrlID(v)
	}

	// the clipPath's own transform applies to all of its children
	rootM := Identity
	if t := tag.attrs["transform"]; t != "" {
		rootM = svg.parseTransform(t)
		if !finiteMatrix(rootM) {
			svg.clipError("non-finite clipPath transform")
			return nil
		}
	}

	origCanvas := svg.svgCanvas
	origActiveDefs := svg.activeDefs
	clipCanvas := New(svg.c.W, svg.c.H)
	clipCtx := NewContext(clipCanvas)
	// clipPath contents are defined in the referencing element's user
	// coordinate system: no Cartesian axis flip and no view transform. The
	// target's coordinate system and view are applied when the clip is
	// resolved, so recorded layer matrices contain only clipPath and group
	// transforms expressed in user units.
	clipCtx.SetCoordSystem(CartesianI)
	clipCtx.SetView(Identity)
	svg.svgCanvas = svgCanvas{c: clipCanvas, ctx: clipCtx, width: svg.width, height: svg.height, diagonal: svg.diagonal}
	svg.activeDefs = map[string]svgDef{}

	svg.parseClipChildren(tag, rootM)

	svg.svgCanvas = origCanvas
	svg.activeDefs = origActiveDefs

	for _, l := range clipCanvas.layers[0] {
		if l.path == nil || !finiteMatrix(l.m) {
			continue
		}
		rule := NonZero
		if l.style.FillRule == EvenOdd {
			rule = EvenOdd
		}
		def.clipPath = append(def.clipPath, clipGeometry{
			path:     l.path.Copy(),
			m:        l.m,
			fillRule: rule,
		})
	}
	return def
}

// parseClipChildren renders the children of a clipPath element. The parent
// transform accumulates the clipPath's own transform and the transforms of
// nested <g> elements.
func (svg *svgParser) parseClipChildren(tag *svgTag, parent Matrix) {
	for _, child := range tag.content {
		switch child.name {
		case "clipPath", "defs", "title", "desc", "metadata":
			continue
		case "g":
			m := parent
			if t := child.attrs["transform"]; t != "" {
				tm := svg.parseTransform(t)
				if !finiteMatrix(tm) {
					svg.clipError("non-finite transform in clipPath")
					continue
				}
				m = m.Mul(tm)
			}
			svg.parseClipChildren(child, m)
			continue
		}

		svg.ctx.Push()
		svg.ctx.SetView(parent)
		// clip children never paint, but they need a fill so DrawPath does
		// not early-return; fill-rule is carried through the layer style.
		svg.ctx.SetFill(Paint{Color: Black})

		props := []cssProperty{}
		for _, key := range child.attrNames {
			props = append(props, cssProperty{key, child.attrs[key]})
		}
		svg.setStyling(props)
		svg.drawShape(child.name, child.attrs)
		svg.ctx.Pop()
	}
}

// clipResult distinguishes the three outcomes of resolving a clip-path:
// clipping applies with the given geometry, clipping is disabled (unknown
// reference or objectBoundingBox without a usable box), or the clip path is
// empty in which case the clipped content must not be rendered.
type clipResult int

const (
	clipDisabled clipResult = iota
	clipHasGeometry
	clipEmpty
)

// resolveClip returns the clipping geometry in the canvas coordinate system
// that applies to the element carrying a clip-path attribute.
//
// targetM is the transformation of the referencing element (its view matrix,
// including the Cartesian coordinate system), targetBounds is the element's
// geometry bounds in its local user space (used for objectBoundingBox units),
// and defs maps clipPath ids to their parsed definitions.
func (svg *svgParser) resolveClip(id string, targetM Matrix, targetBounds Rect, defs map[string]*clipPathDef) (*Path, clipResult) {
	return svg.resolveClipRec(id, targetM, targetBounds, defs, map[string]bool{})
}

func (svg *svgParser) resolveClipRec(id string, targetM Matrix, targetBounds Rect, defs map[string]*clipPathDef, seen map[string]bool) (*Path, clipResult) {
	def, ok := defs[id]
	if !ok || def == nil {
		svg.clipError("clip-path references unknown or invalid clipPath: #%s", id)
		return nil, clipEmpty
	} else if seen[id] {
		svg.clipError("cyclic clip-path reference: #%s", id)
		return nil, clipEmpty
	}
	seen[id] = true

	if !finiteMatrix(targetM) {
		svg.clipError("non-finite transformation matrix for clip-path target")
		return nil, clipEmpty
	}

	// objectBoundingBox maps the unit cube [0,1]x[0,1] onto the target's
	// bounding box. A box with zero width or height, or a target without any
	// bounds, disables clipping per the SVG specification.
	bboxM := Identity
	if def.units == "objectBoundingBox" {
		if targetBounds.Zero() || targetBounds.Empty() {
			return nil, clipDisabled
		}
		bboxM = Identity.
			Translate(targetBounds.X0, targetBounds.Y0).
			Scale(targetBounds.W(), targetBounds.H())
		if !finiteMatrix(bboxM) {
			svg.clipError("non-finite objectBoundingBox matrix for clip-path")
			return nil, clipEmpty
		}
	}

	var clip *Path
	for _, geo := range def.clipPath {
		// local clip space -> bbox space (objectBoundingBox only) -> target
		// user space -> canvas space
		m := targetM.Mul(bboxM).Mul(geo.m)
		if !finiteMatrix(m) {
			svg.clipError("non-finite matrix in clipPath geometry")
			continue
		}
		// Settle first so the child's own fill-rule (nonzero/evenodd) is
		// honored before the intersection is taken.
		part := geo.path.Copy().Transform(m).Settle(geo.fillRule)
		if part.Empty() {
			continue
		}
		if clip == nil {
			clip = part
		} else {
			clip = clip.Or(part)
		}
	}

	// nested clipPath: the clipPath element may itself carry a clip-path
	// attribute, in which case both clips apply by intersection.
	if def.clipRef != "" {
		nested, result := svg.resolveClipRec(def.clipRef, targetM, targetBounds, defs, seen)
		if result == clipEmpty {
			return nil, clipEmpty
		} else if result == clipHasGeometry {
			if clip == nil {
				clip = nested
			} else {
				clip = clip.And(nested)
			}
		}
	}
	if clip == nil || clip.Empty() {
		return nil, clipEmpty
	}
	return clip, clipHasGeometry
}

// materializeText converts a text layer into one or more path layers on a
// temporary canvas, so that clipping can intersect the glyph geometry. Text
// is rendered as filling paths through Text.RenderTo.
func materializeText(l layer, sizeW, sizeH float64) []layer {
	tmp := New(sizeW, sizeH)
	l.text.RenderTo(tmp, l.m, 0.0)
	return tmp.layers[0]
}

// clipLayers intersects the painted geometry of a single layer with the clip
// path, returning zero or more replacement layers. Filled and stroked areas
// are converted to filling paths first; the result keeps the original paint
// but drops stroking metadata. Text is materialized into per-span glyph
// paths so each span keeps its own paint. A clip that removes all geometry
// yields an empty slice.
func clipLayers(l layer, clip *Path, sizeW, sizeH float64) []layer {
	if clip == nil || clip.Empty() {
		return []layer{l}
	}

	// text layers are first materialized into glyph path layers, each already
	// carrying its own render matrix and paint
	if l.path == nil && l.text != nil {
		glyphLayers := materializeText(l, sizeW, sizeH)
		var out []layer
		for _, pl := range glyphLayers {
			out = append(out, clipLayers(pl, clip, sizeW, sizeH)...)
		}
		return out
	}
	if l.path == nil && l.img != nil {
		// images cannot be intersected as vector geometry. Clip them against
		// their transformed bounding rectangle: keep the image layer when any
		// part of its box survives, otherwise drop it.
		size := l.img.Bounds().Size()
		box := Rectangle(float64(size.X), float64(size.Y)).Transform(l.m)
		if box.And(clip).Empty() {
			return nil
		}
		return []layer{l}
	}
	if l.path == nil {
		return nil
	}

	style := l.style
	geometry := &Path{}
	if style.HasFill() {
		fill := l.path.Copy().Transform(l.m).Settle(style.FillRule)
		if !fill.Empty() {
			geometry = geometry.Append(fill)
		}
	}
	if style.HasStroke() {
		tolerance := PixelTolerance // units are already in canvas millimeters
		dashed := l.path
		if 0 < len(style.Dashes) {
			dashOffset, dashes := ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
			dashed = l.path.Dash(dashOffset, dashes...)
		}
		stroke := dashed.Stroke(style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, tolerance).Transform(l.m)
		if !stroke.Empty() {
			geometry = geometry.Append(stroke)
		}
	}
	if geometry.Empty() {
		return nil
	}

	clipped := geometry.And(clip)
	if clipped == nil || clipped.Empty() {
		return nil
	}

	out := l
	out.text = nil
	out.path = clipped
	out.m = Identity
	out.style = Style{
		Fill:     style.Fill,
		FillRule: NonZero,
	}
	return []layer{out}
}

// applyClip clips the layers appended since the clip stack was entered.
//
// Composition order: clipping is applied when the referencing element closes,
// after all of its content (including nested groups) has been painted and
// after paints such as gradients have been resolved. The resulting geometry is
// the intersection of every painted area (fills, strokes, glyph outlines) with
// the resolved clip path; layer order and z-index are preserved. There is no
// separate group-opacity model in this renderer, so opacity applied to
// individual paints is unaffected; mask is not supported by this parser.
//
// The clip is resolved against the bounds of the affected layers in the
// element's local user space, which requires mapping canvas bounds back
// through the element's view matrix.
func (svg *svgParser) applyClip(id string, viewM Matrix, clipDefs map[string]*clipPathDef, startIndex int) {
	layers := svg.c.layers[svg.c.zindex]
	if startIndex >= len(layers) {
		return
	}
	affected := layers[startIndex:]

	// bounds in the target element's local user space, for objectBoundingBox
	var localBounds Rect
	haveBounds := false
	if !finiteMatrix(viewM) || Equal(viewM.Det(), 0.0) {
		svg.clipError("non-invertible transformation matrix for clip-path target")
		svg.c.layers[svg.c.zindex] = layers[:startIndex]
		return
	}
	inv := viewM.Inv()
	for _, l := range affected {
		var b Rect
		switch {
		case l.path != nil && !l.path.Empty():
			// use the painted bounds: strokes extend beyond the path geometry
			// and a fill-less line has zero-area path bounds but a non-empty stroke
			b = l.path.FastBounds()
			if l.style.HasStroke() {
				hw := l.style.StrokeWidth / 2.0
				b.X0 -= hw
				b.Y0 -= hw
				b.X1 += hw
				b.Y1 += hw
			}
			b = b.Transform(l.m)
		case l.text != nil:
			b = l.text.Bounds().Transform(l.m)
		case l.img != nil:
			size := l.img.Bounds().Size()
			b = Rect{0.0, 0.0, float64(size.X), float64(size.Y)}.Transform(l.m)
		default:
			continue
		}
		// canvas space -> target element's user space
		b = b.Transform(inv)
		if !haveBounds {
			localBounds, haveBounds = b, true
		} else {
			localBounds = localBounds.Add(b)
		}
	}
	if !haveBounds {
		// no paintable geometry: an empty clip target has a stable empty
		// result regardless of the referenced clip path
		return
	}

	clip, result := svg.resolveClip(id, viewM, localBounds, clipDefs)
	switch result {
	case clipDisabled:
		return // clipping does not apply: content renders unchanged
	case clipEmpty:
		// empty or invalid clip path hides all clipped content
		svg.c.layers[svg.c.zindex] = layers[:startIndex]
		return
	}

	out := make([]layer, 0, len(layers))
	out = append(out, layers[:startIndex]...)
	for _, l := range affected {
		out = append(out, clipLayers(l, clip, svg.c.W, svg.c.H)...)
	}
	svg.c.layers[svg.c.zindex] = out
}
