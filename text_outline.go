package canvas

import (
	"errors"
	"fmt"

	"github.com/tdewolff/canvas/text"
	"github.com/tdewolff/font"
)

// TextPath is a single text span converted to a glyph outline path. The path
// is positioned in the same coordinate system as the Text object it
// originates from: text lines are drawn downwards along negative Y, with the
// text box's top-left corner at the origin. Face holds the font face (and
// thus the fill paint and faux styles) used to generate the path.
type TextPath struct {
	// Face is the font face of the text span.
	Face *FontFace

	// X and Y are the baseline position of the text span, including the
	// face's sub/superscript offsets.
	X, Y float64

	// Rotation is the span rotation as used for text in vertical writing
	// modes.
	Rotation text.Rotation

	// Path is the glyph outline path. It is empty for spans that contain no
	// drawable outlines (e.g. whitespace or only missing glyphs).
	Path *Path

	// MissingGlyphs lists the glyph IDs in the span for which no outline
	// was drawn. This contains glyph ID 0 (.notdef, i.e. characters the
	// font does not cover) and the glyphs of fonts that lack TrueType/CFF
	// outline tables. The advances of these glyphs are still honored in the
	// surrounding positions.
	MissingGlyphs []uint16
}

// spanPosition returns the baseline position (x, y) of a text span within
// the text box, mirroring the positioning of Text.renderLineTo and
// WalkSpans.
func (t *Text) spanPosition(line line, span TextSpan) (float64, float64) {
	x, y := span.X, -line.y
	if t.WritingMode != HorizontalTB {
		x, y = line.y, -span.X
	}
	x += span.Face.MmPerEm * float64(span.Face.XOffset)
	y += span.Face.MmPerEm * float64(span.Face.YOffset)
	return x, y
}

// outlineGlyphs converts the glyphs of a single text span to an outline path
// in the span's local coordinates (origin at the span baseline), mirroring
// FontFace.toPath so the geometry is identical to the regular text pipeline.
// It does not apply hinting, so the outlines are resolution independent.
// Whitespace and missing glyphs (glyph ID 0, the .notdef glyph) produce no
// contours but still advance the pen; missing records the glyph IDs that
// could not be outlined. unsupported is true when the font itself provides
// no TrueType/CFF outline tables (bitmap or color fonts), in which case none
// of the span's glyphs can be converted.
func outlineGlyphs(face *FontFace, glyphs []text.Glyph) (p *Path, missing []uint16, unsupported bool) {
	p = &Path{}
	f := face.MmPerEm

	noOutlines := !face.Font.IsTrueType && !face.Font.IsCFF
	x, y := face.XOffset, face.YOffset // pen position in font units
	for _, glyph := range glyphs {
		switch {
		case noOutlines:
			// bitmap and color (CBDT/COLR/SVG) fonts have no outline tables
			missing = append(missing, glyph.ID)
		case glyph.ID == 0:
			// .notdef: missing glyph, keep its advance but draw no outline
			missing = append(missing, glyph.ID)
		default:
			if err := face.Font.GlyphPath(p, glyph.ID, 0, f*float64(x+glyph.XOffset), f*float64(y+glyph.YOffset), f, font.NoHinting); err != nil {
				// unexpected SFNT decoding failure: treat as missing
				missing = append(missing, glyph.ID)
			}
		}
		x += glyph.XAdvance
		y += glyph.YAdvance
	}

	if 0.0005 <= face.FauxBold {
		// only apply a meaningful amount of faux-bold offset
		d := face.FauxBold * face.Size
		if face.Font.IsTrueType {
			// TrueType is CW oriented for filling contours.
			d = -d
		}

		// use FastStroke to omit settling the path
		origFastStroke := FastStroke
		FastStroke = true
		p = p.Offset(d, Tolerance)
		FastStroke = origFastStroke
	}
	if face.FauxItalic != 0.0 {
		p = p.Transform(Identity.Shear(face.FauxItalic, 0.0))
	}
	return p, missing, noOutlines
}

// ErrUnsupportedFontOutlines reports that a text span uses a font without
// TrueType or CFF outline tables (e.g. a bitmap or color font), so its
// glyphs cannot be converted to paths. The errors.Join result returned by
// ToPaths, ToPath, ToCanvas and RenderPathsTo contains one value of this
// type per affected span.
var ErrUnsupportedFontOutlines = errors.New("font has no TrueType or CFF outlines")

// ToPaths converts the laid-out text (including multi-line content and font
// face switches) to glyph outline paths. One TextPath is returned per text
// span, preserving the span's position, rotation and font face. Inline
// objects (images, paths embedded with RichText.WriteCanvas and friends) are
// not part of the font outlines and are therefore omitted. The returned
// paths are independent of any runtime font file: rendering them needs only
// the path geometry.
//
// Whitespace and missing glyphs (the font's .notdef glyph) produce no
// contours but keep their advance width through the surrounding span
// positions; they are listed in TextPath.MissingGlyphs. Glyphs in fonts
// that do not provide TrueType/CFF outlines (bitmap or color fonts) cannot
// be converted: they are skipped and an ErrUnsupportedFontOutlines error is
// returned together with the partial result.
func (t *Text) ToPaths() ([]TextPath, error) {
	if t.Empty() {
		return []TextPath{}, nil
	}

	paths := make([]TextPath, 0)
	var errs []error
	for _, line := range t.lines {
		for _, span := range line.spans {
			if !span.IsText() {
				// inline canvas objects (images, paths) are not font outlines
				continue
			}
			x, y := t.spanPosition(line, span)
			local, missing, unsupported := outlineGlyphs(span.Face, span.Glyphs)
			// position the local outline at the span baseline
			p := local.Transform(Identity.Translate(x, y))
			paths = append(paths, TextPath{
				Face:          span.Face,
				X:             x,
				Y:             y,
				Rotation:      span.Rotation,
				Path:          p,
				MissingGlyphs: missing,
			})
			if unsupported {
				errs = append(errs, fmt.Errorf("%w: %q", ErrUnsupportedFontOutlines, span.Face.Name()))
			}
		}
	}
	return paths, errors.Join(errs...)
}

// ToPath converts the laid-out text to a single glyph outline path. It is a
// convenience wrapper around ToPaths that merges all span outlines into one
// path, applying each span's rotation (used in vertical writing modes);
// per-span fill paints are lost in the merge, use ToPaths when the text
// mixes font faces with different fills. The returned error reports
// unsupported outline fonts (if any), while the merged path always contains
// the successfully converted outlines.
func (t *Text) ToPath() (*Path, error) {
	paths, err := t.ToPaths()
	p := &Path{}
	for _, tp := range paths {
		if tp.Path.Empty() {
			continue
		}
		if tp.Rotation == text.NoRotation {
			p = p.Append(tp.Path)
		} else {
			// rotate about the span baseline so the layout is preserved
			p = p.Append(tp.Path.Transform(Identity.Translate(tp.X, tp.Y).Rotate(float64(tp.Rotation)).Translate(-tp.X, -tp.Y)))
		}
	}
	return p, err
}

// ToCanvas renders the text as glyph outline path layers onto a new canvas
// of the text box's size. Each text span becomes a RenderPath layer carrying
// its own fill paint (including gradients and patterns), so font face
// switches and mixed colors are preserved. The resulting canvas has no
// dependency on the original font files and can be rendered to SVG, raster
// images or any other Renderer. Inline objects are rendered as well.
func (t *Text) ToCanvas() (*Canvas, error) {
	c := New(t.Width, t.Height)
	err := t.RenderPathsTo(c, Identity, 0.0)
	return c, err
}

// RenderPathsTo renders the text converted to glyph outline paths to the
// given renderer. It is the outline-only counterpart of RenderTo: text is
// emitted through RenderPath calls positioned exactly as the text layout
// dictates (same glyph advances, baselines, line breaks and span rotations),
// hence the output aligns with the regular text rendering on both the SVG
// and raster renderers. Decorations (underlines, strokes, ...) and inline
// objects are rendered as usual. Missing glyphs are silently skipped while
// fonts without outline tables are reported in the returned error.
func (t *Text) RenderPathsTo(r Renderer, m Matrix, resolution Resolution) error {
	var errs []error
	for index := range t.lines {
		line := t.lines[index]
		for _, span := range line.spans {
			x, y := t.spanPosition(line, span)
			if !span.IsText() {
				for _, obj := range span.Objects {
					obj.RenderViewTo(r, m.Mul(obj.View(x, y, span.Face)))
				}
				continue
			}

			p, _, unsupported := outlineGlyphs(span.Face, span.Glyphs)
			if unsupported {
				errs = append(errs, fmt.Errorf("%w: %q", ErrUnsupportedFontOutlines, span.Face.Name()))
			}
			if p.Empty() {
				continue
			}
			style := DefaultStyle
			style.Fill = span.Face.Fill
			// keep the path local and place it with the matrix, exactly as
			// Text.renderLineTo does, so both pipelines align
			r.RenderPath(p, style, m.Translate(x, y).Rotate(float64(span.Rotation)))
		}
	}

	// decorations (underlines, text strokes, ...) build on the same glyph
	// outline paths as the regular text pipeline
	t.RenderDecorationsTo(r, m, resolution)
	return errors.Join(errs...)
}
