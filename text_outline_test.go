package canvas

import (
	"errors"
	"testing"

	"github.com/tdewolff/font"
	"github.com/tdewolff/test"
)

// countSubpaths counts the number of independent subpaths. MoveTo stores
// its command marker at both ends of the coordinate triplet, so every
// subpath contributes two MoveToCmd values.
func countSubpaths(p *Path) int {
	n := 0
	for _, cmd := range p.d {
		if cmd == MoveToCmd {
			n++
		}
	}
	return n / 2
}

func loadTestFont(t *testing.T, name string) *Font {
	t.Helper()
	f, err := LoadFontFile(name, FontRegular)
	test.Error(t, err)
	return f
}

func TestTextToPathEmpty(t *testing.T) {
	face := loadTestFont(t, "resources/DejaVuSerif.ttf").Face(12.0*ptPerMm, Black)

	// empty text: no lines and no outlines
	rt := NewRichText(face)
	txt := rt.ToText(0.0, 0.0, Left, Top, nil)
	test.That(t, txt.Empty())

	paths, err := txt.ToPaths()
	test.Error(t, err)
	test.T(t, len(paths), 0)

	p, err := txt.ToPath()
	test.Error(t, err)
	test.That(t, p.Empty())

	c, err := txt.ToCanvas()
	test.Error(t, err)
	test.That(t, c.Empty())

	// whitespace only: spaces produce no contours, the tab maps to .notdef
	// (also no outline) but is reported as a missing glyph
	txt = NewTextLine(face, "   \t ", Left)
	paths, err = txt.ToPaths()
	test.Error(t, err)
	test.That(t, 0 < len(paths))
	var missing int
	for _, tp := range paths {
		test.That(t, tp.Path.Empty())
		missing += len(tp.MissingGlyphs)
	}
	test.T(t, missing, 1) // the single tab
	p, err = txt.ToPath()
	test.Error(t, err)
	test.That(t, p.Empty())
}

func TestTextToPathGlyphCount(t *testing.T) {
	face := loadTestFont(t, "resources/DejaVuSerif.ttf").Face(12.0*ptPerMm, Black)
	txt := NewTextLine(face, "hello", Left)

	paths, err := txt.ToPaths()
	test.Error(t, err)
	test.T(t, len(paths), 1)
	test.T(t, len(paths[0].MissingGlyphs), 0)

	// every glyph starts at least one contour (subpath); "hello" contains
	// five glyphs, so the merged path holds at least five subpaths
	p, err := txt.ToPath()
	test.Error(t, err)
	test.That(t, 5 <= countSubpaths(p))

	// the single-string FontFace.ToPath must give the same geometry
	facePath, _ := face.ToPath("hello")
	test.T(t, countSubpaths(facePath), countSubpaths(p))
}

func TestTextToPathMissingGlyph(t *testing.T) {
	face := loadTestFont(t, "resources/DejaVuSerif.ttf").Face(12.0*ptPerMm, Black)

	// U+1234 is not in DejaVu Serif: the shaper emits the .notdef glyph
	// (ID 0), which is skipped in the outlines but reported. The script
	// itemizer puts it in its own span.
	txt := NewTextLine(face, "AሴB", Left)
	paths, err := txt.ToPaths()
	test.Error(t, err)

	var missing []uint16
	var nContours int
	for _, tp := range paths {
		missing = append(missing, tp.MissingGlyphs...)
		nContours += countSubpaths(tp.Path)
	}
	test.T(t, len(missing), 1)
	test.T(t, missing[0], uint16(0))

	p, err := txt.ToPath()
	test.Error(t, err)
	test.That(t, !p.Empty())

	// only A and B contribute contours
	test.T(t, countSubpaths(p), nContours)
	test.That(t, 2 <= countSubpaths(p))

	// the missing glyph still consumes its advance: bounds width must
	// exceed that of "AB" alone
	txtAB := NewTextLine(face, "AB", Left)
	pAB, err := txtAB.ToPath()
	test.Error(t, err)
	test.That(t, p.Bounds().W() > pAB.Bounds().W())
}

func TestTextToPathMultilineBaselines(t *testing.T) {
	face := loadTestFont(t, "resources/DejaVuSerif.ttf").Face(12.0*ptPerMm, Black)
	txt := NewTextLine(face, "ab\ncd", Left)
	test.T(t, txt.Lines(), 2)

	paths, err := txt.ToPaths()
	test.Error(t, err)
	test.T(t, len(paths), 2)

	// both spans start at x=0 (left aligned)
	test.Float(t, paths[0].X, 0.0)
	test.Float(t, paths[1].X, 0.0)

	// baselines follow the line height downward (negative Y)
	lineHeight := face.Metrics().LineHeight
	test.Float(t, paths[0].Y, 0.0)
	test.Float(t, paths[1].Y, -lineHeight)

	// the distance between the two outlines in the merged path matches the
	// line height, and both lines retain their four glyphs
	p, err := txt.ToPath()
	test.Error(t, err)
	test.That(t, 4 <= countSubpaths(p))
	test.Float(t, paths[0].Y-paths[1].Y, lineHeight)
}

func TestTextToPathFontSwitch(t *testing.T) {
	ttf := loadTestFont(t, "resources/DejaVuSerif.ttf")
	otf := loadTestFont(t, "resources/EBGaramond12-Regular.otf") // CFF outlines
	faceTTF := ttf.Face(12.0*ptPerMm, Black)
	faceOTF := otf.Face(12.0*ptPerMm, Red)

	rt := NewRichText(faceTTF)
	rt.WriteFace(faceTTF, "TTF ")
	rt.WriteFace(faceOTF, "OTF")
	txt := rt.ToText(0.0, 0.0, Left, Top, nil)

	paths, err := txt.ToPaths()
	test.Error(t, err)
	test.That(t, 1 < len(paths))

	// each span keeps its face and fill paint
	var sawTTF, sawOTF bool
	for _, tp := range paths {
		if tp.Face.Font == ttf {
			sawTTF = true
			test.T(t, tp.Face.Fill.Color, Black)
		} else if tp.Face.Font == otf {
			sawOTF = true
			test.T(t, tp.Face.Fill.Color, Red)
		}
		test.That(t, !tp.Path.Empty())
	}
	test.That(t, sawTTF)
	test.That(t, sawOTF)

	// ToCanvas preserves the different fills as separate path layers
	c, err := txt.ToCanvas()
	test.Error(t, err)
	nLayers := 0
	for _, layers := range c.layers {
		nLayers += len(layers)
	}
	test.That(t, 1 < nLayers)

	// merged path still contains all six letter outlines (each >= 1 contour)
	p, err := txt.ToPath()
	test.Error(t, err)
	test.That(t, 6 <= countSubpaths(p))
}

func TestTextToPathNegativeKerning(t *testing.T) {
	face := loadTestFont(t, "resources/DejaVuSerif.ttf").Face(12.0*ptPerMm, Black)

	// "A" + combining macron (U+0304) shapes to a mark glyph with a
	// negative XOffset in this font
	glyphs := face.Glyphs("A̅")
	found := false
	for _, g := range glyphs {
		if g.XOffset < 0 {
			found = true
		}
	}
	test.That(t, found)

	txt := NewTextLine(face, "A̅", Left)
	paths, err := txt.ToPaths()
	test.Error(t, err)
	test.T(t, len(paths), 1)

	// negative offsets must not lose or collapse any glyph: both the base
	// letter and the combining mark contours are present
	test.That(t, 1 < countSubpaths(paths[0].Path))

	// the outline geometry must match the per-face path builder exactly
	ref, _ := face.toPath(glyphs, face.PPEM(DefaultResolution))
	test.That(t, paths[0].Path.Equals(ref))
}

func TestTextToPathUnsupportedOutline(t *testing.T) {
	// construct a minimal SFNT-like failure by checking the error path on a
	// font whose flags report neither TrueType nor CFF. We emulate this by
	// temporarily clearing the flags on a loaded copy.
	f := loadTestFont(t, "resources/DejaVuSerif.ttf")
	origTT, origCFF := f.IsTrueType, f.IsCFF
	f.IsTrueType, f.IsCFF = false, false
	defer func() { f.IsTrueType, f.IsCFF = origTT, origCFF }()

	face := f.Face(12.0*ptPerMm, Black)
	txt := NewTextLine(face, "AB", Left)

	paths, err := txt.ToPaths()
	test.That(t, errors.Is(err, ErrUnsupportedFontOutlines))
	test.T(t, len(paths), 1)
	test.That(t, paths[0].Path.Empty())
	test.T(t, len(paths[0].MissingGlyphs), 2)

	p, err := txt.ToPath()
	test.That(t, errors.Is(err, ErrUnsupportedFontOutlines))
	test.That(t, p.Empty())

	c, err := txt.ToCanvas()
	test.That(t, errors.Is(err, ErrUnsupportedFontOutlines))
	test.That(t, c.Empty())
}

func TestTextToPathBounds(t *testing.T) {
	face := loadTestFont(t, "resources/DejaVuSerif.ttf").Face(12.0*ptPerMm, Black)
	txt := NewTextLine(face, "H", Left)

	p, err := txt.ToPath()
	test.Error(t, err)
	b := p.Bounds()

	// the capital H outline sits on the baseline (y=0) and reaches the
	// cap height upwards; bounds are finite and strictly positive
	test.That(t, b.X1 > b.X0)
	test.That(t, b.Y1 > b.Y0)
	test.Float(t, b.Y0, 0.0) // baseline aligned
	test.That(t, b.Y1 <= face.Metrics().Ascent+Tolerance)
	test.That(t, face.Metrics().CapHeight-Tolerance <= b.Y1)

	// outline bounds agree with the slow OutlineBounds helper of Text
	ob := txt.OutlineBounds()
	test.Float(t, b.X0, ob.X0)
	test.Float(t, b.Y0, ob.Y0)
	test.Float(t, b.X1, ob.X1)
	test.Float(t, b.Y1, ob.Y1)
}

func TestTextRenderPathsToAlignment(t *testing.T) {
	face := loadTestFont(t, "resources/DejaVuSerif.ttf").Face(12.0*ptPerMm, Black)
	face.Hinting = font.NoHinting // disable grid alignment so both pipelines are identical
	txt := NewTextLine(face, "test\nline", Left)

	// regular text pipeline (which itself renders glyph outlines)
	cText := New(txt.Width, txt.Height)
	txt.RenderTo(cText, Identity, 0.0)

	// outline-only pipeline
	cPath := New(txt.Width, txt.Height)
	err := txt.RenderPathsTo(cPath, Identity, 0.0)
	test.Error(t, err)

	// both canvases hold the same number of path layers with equal geometry
	// and styles (decorations add layers only when configured; none here)
	flatten := func(c *Canvas) []*Path {
		ps := []*Path{}
		for _, layers := range c.layers {
			for _, l := range layers {
				ps = append(ps, l.path)
			}
		}
		return ps
	}
	pathsText := flatten(cText)
	pathsPath := flatten(cPath)
	test.T(t, len(pathsText), len(pathsPath))
	for i := range pathsText {
		test.That(t, pathsText[i].Equals(pathsPath[i]))
	}

	// applying a transform to both pipelines keeps the geometry aligned
	m := Identity.Translate(5.0, 7.0).Rotate(0.3).Scale(1.5, 1.5)
	cText2 := New(40.0, 40.0)
	txt.RenderTo(cText2, m, 0.0)
	cPath2 := New(40.0, 40.0)
	test.Error(t, txt.RenderPathsTo(cPath2, m, 0.0))
	pathsText = flatten(cText2)
	pathsPath = flatten(cPath2)
	test.T(t, len(pathsText), len(pathsPath))
	for i := range pathsText {
		test.That(t, pathsText[i].Equals(pathsPath[i]))
	}
}

func TestTextToPathVertical(t *testing.T) {
	face := loadTestFont(t, "resources/unifont-13.0.05.ttf").Face(12.0*ptPerMm, Black)
	rt := NewRichText(face)
	rt.SetWritingMode(VerticalRL)
	rt.WriteString("中文测试")
	txt := rt.ToText(0.0, 0.0, Top, Right, nil)

	p, err := txt.ToPath()
	test.Error(t, err)
	test.That(t, !p.Empty())

	// the outline canvas renders without error in vertical mode as well
	c, err := txt.ToCanvas()
	test.Error(t, err)
	test.That(t, !c.Empty())
}

func TestTextToPathInlineObject(t *testing.T) {
	face := loadTestFont(t, "resources/DejaVuSerif.ttf").Face(12.0*ptPerMm, Black)
	rt := NewRichText(face)
	rt.WriteString("a")
	rt.WritePath(Circle(1.0), Red, Baseline)
	rt.WriteString("b")
	txt := rt.ToText(0.0, 0.0, Left, Top, nil)

	// ToPaths omits the inline object: only the two letters remain
	paths, err := txt.ToPaths()
	test.Error(t, err)
	test.T(t, len(paths), 2)

	// RenderPathsTo keeps the inline object as a rendered path layer
	c := New(txt.Width, txt.Height)
	test.Error(t, txt.RenderPathsTo(c, Identity, 0.0))
	nLayers := 0
	for _, layers := range c.layers {
		nLayers += len(layers)
	}
	// at least the two letter span paths and the inline circle
	test.That(t, 3 <= nLayers)
}
