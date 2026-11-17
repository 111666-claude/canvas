package canvas

import (
	"errors"
	"math"

	"github.com/tdewolff/canvas/text"
	"github.com/tdewolff/font"
)

// TextOnPathAlign determines how text is aligned along the path's arc length.
type TextOnPathAlign int

// See TextOnPathAlign.
const (
	// TextOnPathLeft places the text's start at the given distance along the path.
	TextOnPathLeft TextOnPathAlign = iota
	// TextOnPathCenter centers the text around the given distance along the path.
	TextOnPathCenter
	// TextOnPathRight places the text's end at the given distance along the path.
	TextOnPathRight
)

// TextOnPathOptions contains the options for laying out text along a path.
type TextOnPathOptions struct {
	// Align aligns the text relative to Distance along the path. The default (TextOnPathLeft) starts the text at Distance.
	Align TextOnPathAlign

	// Distance is the arc length in millimeters from the path's start at which the text is placed according to Align. It must be finite and non-negative.
	Distance float64

	// LetterSpacing is the extra arc length in millimeters added between consecutive glyphs. It must be finite and non-negative.
	LetterSpacing float64
}

// pathSegment stores a single non-zero-length drawable path segment together
// with the arc length at which it starts and its own length.
type pathSegment struct {
	cmd                        float64
	start, cp1, cp2, end       Point
	center                     Point
	rx, ry, phi                float64
	sweep                      bool
	theta0, theta1, d0, length float64
}

// arcLengthSegments flattens the path into a list of drawable, non-zero-length
// segments annotated with their start arc length d0 and their own length.
// MoveTo commands split subpaths but do not consume arc length; zero-length
// segments (degenerate lines, Béziers and arcs) are skipped.
func arcLengthSegments(p *Path) []pathSegment {
	segments := []pathSegment{}
	d := 0.0
	current := Point{}
	for i := 0; i < len(p.d); {
		cmd := p.d[i]
		switch cmd {
		case MoveToCmd:
			current = Point{p.d[i+1], p.d[i+2]}
		case LineToCmd, CloseCmd:
			start := current
			end := Point{p.d[i+1], p.d[i+2]}
			if length := end.Sub(start).Length(); Epsilon < length {
				segments = append(segments, pathSegment{cmd: cmd, start: start, end: end, d0: d, length: length})
				d += length
			}
			current = end
		case QuadToCmd:
			start := current
			cp := Point{p.d[i+1], p.d[i+2]}
			end := Point{p.d[i+3], p.d[i+4]}
			if length := quadraticBezierLength(start, cp, end); Epsilon < length {
				segments = append(segments, pathSegment{cmd: cmd, start: start, cp1: cp, end: end, d0: d, length: length})
				d += length
			}
			current = end
		case CubeToCmd:
			start := current
			cp1 := Point{p.d[i+1], p.d[i+2]}
			cp2 := Point{p.d[i+3], p.d[i+4]}
			end := Point{p.d[i+5], p.d[i+6]}
			if length := cubicBezierLength(start, cp1, cp2, end); Epsilon < length {
				segments = append(segments, pathSegment{cmd: cmd, start: start, cp1: cp1, cp2: cp2, end: end, d0: d, length: length})
				d += length
			}
			current = end
		case ArcToCmd:
			start := current
			rx, ry, phi := p.d[i+1], p.d[i+2], p.d[i+3]
			large, sweep := toArcFlags(p.d[i+4])
			end := Point{p.d[i+5], p.d[i+6]}
			cx, cy, theta0, theta1 := ellipseToCenter(start.X, start.Y, rx, ry, phi, large, sweep, end.X, end.Y)
			if length := ellipseLength(rx, ry, theta0, theta1); Epsilon < length {
				segments = append(segments, pathSegment{cmd: cmd, start: start, end: end, center: Point{cx, cy}, rx: rx, ry: ry, phi: phi, sweep: sweep, theta0: theta0, theta1: theta1, d0: d, length: length})
				d += length
			}
			current = end
		}
		i += cmdLen(cmd)
	}
	return segments
}

// pos returns the position and unit tangent of the segment at arc length d,
// measured from the path's origin (not the segment's start).
func (s *pathSegment) pos(d float64) (Point, Point) {
	u := (d - s.d0) / s.length // normalized arc length within the segment in [0,1]
	switch s.cmd {
	case LineToCmd, CloseCmd:
		tangent := s.end.Sub(s.start).Norm(1.0)
		return s.start.Interpolate(s.end, u), tangent
	case QuadToCmd:
		t := s.arcToParam(u)
		return quadraticBezierPos(s.start, s.cp1, s.end, t), quadraticBezierDeriv(s.start, s.cp1, s.end, t).Norm(1.0)
	case CubeToCmd:
		t := s.arcToParam(u)
		return cubicBezierPos(s.start, s.cp1, s.cp2, s.end, t), cubicBezierDeriv(s.start, s.cp1, s.cp2, s.end, t).Norm(1.0)
	case ArcToCmd:
		// invert the arc-length parametrization so the position is consistent
		// with the segment length returned by ellipseLength
		t := s.arcToParam(u)
		theta := s.theta0 + t*(s.theta1-s.theta0)
		return EllipsePos(s.rx, s.ry, s.phi, s.center.X, s.center.Y, theta), ellipseDeriv(s.rx, s.ry, s.phi, s.sweep, theta).Norm(1.0)
	}
	return Point{}, Point{}
}

// prefixLength returns the arc length of the segment prefix up to parameter t.
func (s *pathSegment) prefixLength(t float64) float64 {
	switch s.cmd {
	case LineToCmd, CloseCmd:
		return t * s.length
	case QuadToCmd:
		_, q1, q2, _, _, _ := quadraticBezierSplit(s.start, s.cp1, s.end, t)
		return quadraticBezierLength(s.start, q1, q2)
	case CubeToCmd:
		_, p1, p2, p3, _, _, _, _ := cubicBezierSplit(s.start, s.cp1, s.cp2, s.end, t)
		return cubicBezierLength(s.start, p1, p2, p3)
	case ArcToCmd:
		return ellipseLength(s.rx, s.ry, s.theta0, s.theta0+t*(s.theta1-s.theta0))
	}
	return 0.0
}

// arcToParam converts the normalized arc length u in [0,1] to the segment
// parameter t in [0,1] using bisection on the prefix arc length.
func (s *pathSegment) arcToParam(u float64) float64 {
	if u <= 0.0 {
		return 0.0
	} else if 1.0 <= u {
		return 1.0
	}
	target := u * s.length
	lo, hi := 0.0, 1.0
	for range 48 {
		mid := (lo + hi) / 2.0
		if s.prefixLength(mid) < target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2.0
}

// locateAt returns the segment containing arc length d together with the
// position and unit tangent at that distance.
func locateAt(segments []pathSegment, d float64) (*pathSegment, Point, Point) {
	k := 0
	for k+1 < len(segments) && segments[k+1].d0 <= d {
		k++
	}
	s := &segments[k]
	pos, tangent := s.pos(math.Min(d, s.d0+s.length))
	return s, pos, tangent
}

// TextOnPath lays out the glyphs of s along the given path using face, so that
// each glyph is positioned by its advance width along the path's arc length and
// is rotated to the path's local tangent. Line segments, quadratic and cubic
// Bézier curves and elliptical arcs are all supported. The glyph outlines are
// returned as a single filled path, which renders identically on every
// renderer (SVG, rasterizer, PDF, ...) as it only relies on RenderPath.
//
// The returned path uses the path's own coordinate system; pass it to
// Context.DrawPath to place it on the canvas. An error is returned for empty
// text, a nil font face, a zero-length path, an invalid distance, text with
// line breaks, or a distance beyond the path's end. Glyphs whose center falls
// beyond the path's end are clipped, and a path too short for a single glyph
// returns an error.
func TextOnPath(face *FontFace, s string, p *Path, opts TextOnPathOptions) (*Path, error) {
	out, _, err := textOnPath(face, s, p, opts)
	return out, err
}

// textOnPath implements TextOnPath and additionally returns the number of
// glyphs actually placed (clipping may drop glyphs at the path's end).
func textOnPath(face *FontFace, s string, p *Path, opts TextOnPathOptions) (*Path, int, error) {
	if face == nil || face.Font == nil {
		return nil, 0, errors.New("text on path: font face is nil")
	} else if s == "" {
		return nil, 0, errors.New("text on path: text is empty")
	}
	for _, r := range s {
		if text.IsParagraphSeparator(r) {
			return nil, 0, errors.New("text on path: multi-line text is not supported, remove line breaks")
		}
	}
	if math.IsNaN(opts.Distance) || math.IsInf(opts.Distance, 0) || opts.Distance < 0.0 {
		return nil, 0, errors.New("text on path: distance must be a finite, non-negative number")
	} else if math.IsNaN(opts.LetterSpacing) || math.IsInf(opts.LetterSpacing, 0) || opts.LetterSpacing < 0.0 {
		return nil, 0, errors.New("text on path: letter spacing must be a finite, non-negative number")
	} else if p == nil || p.Empty() {
		return nil, 0, errors.New("text on path: path is empty")
	}

	glyphs := face.Glyphs(s)
	if len(glyphs) == 0 {
		return nil, 0, errors.New("text on path: text contains no shapable glyphs")
	}
	f := face.MmPerEm

	// glyph advances in millimeters, plus the letter spacing between glyphs
	advances := make([]float64, len(glyphs))
	textWidth := 0.0
	for i, glyph := range glyphs {
		advances[i] = f * float64(glyph.XAdvance)
		textWidth += advances[i]
		if i+1 < len(glyphs) {
			textWidth += opts.LetterSpacing
		}
	}

	segments := arcLengthSegments(p)
	pathLength := 0.0
	if 0 < len(segments) {
		last := &segments[len(segments)-1]
		pathLength = last.d0 + last.length
	}
	if pathLength <= Epsilon {
		return nil, 0, errors.New("text on path: path has zero length")
	}

	// arc length of the first glyph's start
	start := opts.Distance
	if opts.Align == TextOnPathCenter {
		start -= textWidth / 2.0
	} else if opts.Align == TextOnPathRight {
		start -= textWidth
	}
	if start < 0.0 {
		// negative offsets are only reachable through centered/right alignment;
		// clamp to the path origin so as much text as possible is visible
		start = 0.0
	}
	if pathLength < start-Epsilon {
		return nil, 0, errors.New("text on path: distance is beyond the path's end")
	}

	ppem := face.PPEM(DefaultResolution)
	out := &Path{}
	placed := 0
	cursor := start
	for i, glyph := range glyphs {
		center := cursor + advances[i]/2.0
		if pathLength < center-Epsilon {
			break // glyph center is beyond the path's end: clip this and all later glyphs
		}
		center = math.Min(center, pathLength)

		_, pos, tangent := locateAt(segments, center)
		angle := tangent.Angle()

		gp := &Path{}
		if err := face.Font.GlyphPath(gp, glyph.ID, ppem, f*float64(glyph.XOffset), f*float64(glyph.YOffset), f, font.NoHinting); err != nil {
			return nil, 0, err
		}
		if face.FauxItalic != 0.0 {
			gp = gp.Transform(Identity.Shear(face.FauxItalic, 0.0))
		}
		// center the glyph on its advance, rotate to the local tangent and place it on the path
		m := Identity.Translate(pos.X, pos.Y).Rotate(angle).Translate(-advances[i]/2.0, 0.0)
		out = out.Append(gp.Transform(m))
		placed++

		cursor += advances[i]
		if i+1 < len(glyphs) {
			cursor += opts.LetterSpacing
		}
	}

	if placed == 0 {
		return nil, 0, errors.New("text on path: path is too short to fit a single glyph")
	}

	if 0.0005 <= face.FauxBold {
		// match FontFace.toPath: use FastStroke to omit settling the offset path
		d := face.FauxBold * face.Size
		if face.Font.IsTrueType {
			d = -d // TrueType is CW oriented for filling contours
		}
		origFastStroke := FastStroke
		FastStroke = true
		out = out.Offset(d, Tolerance)
		FastStroke = origFastStroke
	}
	return out, placed, nil
}

// DrawPathText draws text along the given path at position (x,y) using the
// current draw state. When the context has no fill paint, the font face's fill
// is used instead; the glyph outlines are never stroked. See TextOnPath for the
// layout semantics and error conditions.
func (c *Context) DrawPathText(x, y float64, face *FontFace, s string, path *Path, opts TextOnPathOptions) error {
	glyphs, err := TextOnPath(face, s, path, opts)
	if err != nil {
		return err
	}

	style := c.Style
	if !style.HasFill() {
		style = DefaultStyle
		style.Fill = face.Fill
	}
	style.Stroke = Paint{} // glyphs are filled only

	coord := c.coordView.Dot(Point{x, y})
	m := c.CoordSystemView().Mul(c.view).Translate(coord.X, coord.Y)
	c.RenderPath(glyphs, style, m)
	return nil
}
