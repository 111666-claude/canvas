package canvas

import (
	"strings"
	"testing"

	"github.com/tdewolff/test"
)

// requireSystemFont skips the test if no system serif font is available, as the SVG parser loads system fonts for text elements.
func requireSystemFont(t *testing.T) {
	t.Helper()
	family := NewFontFamily("serif")
	if err := family.LoadSystemFont("serif", FontRegular); err != nil {
		t.Skip("no system font available:", err)
	}
}

// textLayers returns all text layers of the canvas in order.
func textLayers(c *Canvas) []*Text {
	texts := []*Text{}
	for _, layers := range c.layers {
		for i := range layers {
			if layers[i].text != nil {
				texts = append(texts, layers[i].text)
			}
		}
	}
	return texts
}

func TestParseSVGTextLength(t *testing.T) {
	requireSystemFont(t)
	svg := `<svg width="200" height="100"><text x="10" y="20" textLength="150" lengthAdjust="spacing">Hello</text></svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	texts := textLayers(c)
	test.T(t, len(texts), 1)
	test.Float(t, texts[0].Width, 150.0)
	test.Float(t, texts[0].lines[0].spans[0].TextLength, 150.0)
	test.T(t, texts[0].lines[0].spans[0].LengthAdjust, AdjustSpacing)
}

func TestParseSVGTextLengthSpacingAndGlyphs(t *testing.T) {
	requireSystemFont(t)
	svg := `<svg width="200" height="100"><text x="10" y="20" textLength="150" lengthAdjust="spacingAndGlyphs">Hello</text></svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	texts := textLayers(c)
	test.T(t, len(texts), 1)
	test.Float(t, texts[0].Width, 150.0)
	span := texts[0].lines[0].spans[0]
	test.Float(t, span.TextLength, 150.0)
	test.T(t, span.LengthAdjust, AdjustSpacingAndGlyphs)
}

func TestParseSVGTextLengthInvalid(t *testing.T) {
	requireSystemFont(t)
	for _, attr := range []string{`textLength="0"`, `textLength="-10"`, `textLength="abc"`} {
		svg := `<svg width="200" height="100"><text x="10" y="20" ` + attr + `>Hello</text></svg>`
		c, err := ParseSVG(strings.NewReader(svg))
		if err != nil && attr == `textLength="abc"` {
			continue // parse error for bad numbers is fine
		}
		texts := textLayers(c)
		test.T(t, len(texts), 1)
		test.Float(t, texts[0].lines[0].spans[0].TextLength, 0.0)
	}
}

func TestParseSVGTspan(t *testing.T) {
	requireSystemFont(t)
	svg := `<svg width="200" height="100"><text x="10" y="20"><tspan textLength="50" lengthAdjust="spacing">AB</tspan><tspan dx="5">CD</tspan></text></svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	texts := textLayers(c)
	test.T(t, len(texts), 2)

	// first tspan is adjusted to exactly 50 user units
	test.Float(t, texts[0].Width, 50.0)
	test.Float(t, texts[0].lines[0].spans[0].TextLength, 50.0)

	// second tspan follows the adjusted advance plus dx
	layers := c.layers[0]
	test.T(t, len(layers), 2)
	x, _ := layers[1].m.Pos()
	test.Float(t, x, 10.0+50.0+5.0)
}

func TestParseSVGTspanXY(t *testing.T) {
	requireSystemFont(t)
	svg := `<svg width="200" height="100"><text x="10" y="20">A<tspan x="100" y="50">B</tspan></text></svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	layers := c.layers[0]
	test.T(t, len(layers), 2)
	x, _ := layers[1].m.Pos()
	test.Float(t, x, 100.0)
}

func TestParseSVGTextAnchorWithTextLength(t *testing.T) {
	requireSystemFont(t)
	svg := `<svg width="200" height="100"><text x="100" y="20" text-anchor="middle" textLength="80" lengthAdjust="spacing">Hello</text></svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	texts := textLayers(c)
	test.T(t, len(texts), 1)
	test.Float(t, texts[0].Width, 80.0)
	// anchored at the middle of the adjusted length
	x, _ := c.layers[0][0].m.Pos()
	test.Float(t, x, 100.0-40.0)
}

func TestParseSVGTextPath(t *testing.T) {
	requireSystemFont(t)
	svg := `<svg width="200" height="100">
		<defs><path id="curve" d="M10,50 L190,50"/></defs>
		<text><textPath href="#curve" textLength="180" lengthAdjust="spacing">Hello</textPath></text>
	</svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	layers := c.layers[0]
	test.T(t, len(layers), 1)
	if layers[0].path == nil {
		t.Fatal("expected a path layer for text on a path")
	}
	bounds := layers[0].path.Bounds()
	test.FloatDiff(t, bounds.X0, 10.0, 1.0)
	// the ink ends at or before the adjusted advance, but well beyond the natural width
	test.That(t, 150.0 < bounds.X1, "text not stretched along path")
	test.That(t, bounds.X1 <= 10.0+180.0+0.5, "text extends beyond textLength")
}

func TestParseSVGTextPathSpacingAndGlyphs(t *testing.T) {
	requireSystemFont(t)
	svg := `<svg width="200" height="100">
		<defs><path id="curve" d="M10,50 Q100,10 190,50"/></defs>
		<text><textPath href="#curve" startOffset="10" textLength="100" lengthAdjust="spacingAndGlyphs">ab</textPath></text>
	</svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	layers := c.layers[0]
	test.T(t, len(layers), 1)
	if layers[0].path == nil {
		t.Fatal("expected a path layer for text on a path")
	}
	bounds := layers[0].path.Bounds()
	test.That(t, 19.0 < bounds.X0, "text should start after startOffset")
	test.That(t, bounds.X1 <= 10.0+10.0+100.0+1.0, "text extends beyond startOffset+textLength")
}

func TestParseSVGTextPathMissingReference(t *testing.T) {
	requireSystemFont(t)
	svg := `<svg width="200" height="100"><text><textPath href="#nope">Hello</textPath></text></svg>`
	_, err := ParseSVG(strings.NewReader(svg))
	test.That(t, err != nil, "expected an error for a bad textPath reference")
}

func TestParseSVGTextLengthMixed(t *testing.T) {
	requireSystemFont(t)
	// Latin, CJK (missing glyphs), Hebrew (RTL) and emoji (missing glyphs) in one element
	svg := `<svg width="300" height="100"><text x="10" y="20" textLength="200" lengthAdjust="spacingAndGlyphs">Latin 中文 שלום 🎉</text></svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	texts := textLayers(c)
	test.T(t, len(texts), 1)
	test.Float(t, texts[0].Width, 200.0)
}
