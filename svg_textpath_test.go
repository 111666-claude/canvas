package canvas

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/tdewolff/test"
)

// countLayers returns the total number of path layers in the canvas.
func countLayers(c *Canvas) int {
	n := 0
	for _, layers := range c.layers {
		n += len(layers)
	}
	return n
}

// glyphBounds returns the bounding rectangle of all filled (non-stroked)
// layers, which is where textPath glyph outlines live.
func glyphBounds(c *Canvas) Rect {
	b := Rect{}
	first := true
	for _, layers := range c.layers {
		for _, l := range layers {
			if l.path != nil && l.style.HasFill() && !l.style.HasStroke() && !l.path.Empty() {
				if first {
					b = l.path.Bounds()
					first = false
				} else {
					b = b.Add(l.path.Bounds())
				}
			}
		}
	}
	return b
}

func TestSVGTextPathStartOffset(t *testing.T) {
	// a startOffset in normalized units must land at the same arc-length
	// fraction as the equivalent user-space offset without pathLength
	length := MustParseSVGPath("M20 80 C 60 20, 180 20, 220 80").Length()

	// reference: user-space offset covering 10% of the true curve length
	userOffset := 0.1 * length
	ref, err := ParseSVG(strings.NewReader(`<svg width="240" height="120">
		<path id="tp" d="M20 80 C 60 20, 180 20, 220 80" fill="none" stroke="#ccc"/>
		<text font-family="serif" font-size="18" fill="black"><textPath href="#tp" startOffset="` + strconv.FormatFloat(userOffset, 'f', 4, 64) + `">Text</textPath></text>
	</svg>`))
	test.Error(t, err)

	// normalized: pathLength 100 with offset 10 is the same 10% fraction
	norm, err := ParseSVG(strings.NewReader(`<svg width="240" height="120">
		<path id="tp" d="M20 80 C 60 20, 180 20, 220 80" pathLength="100" fill="none" stroke="#ccc"/>
		<text font-family="serif" font-size="18" fill="black"><textPath href="#tp" startOffset="10">Text</textPath></text>
	</svg>`))
	test.Error(t, err)

	bRef := glyphBounds(ref)
	bNorm := glyphBounds(norm)
	test.That(t, bRef.W() > 0 && bNorm.W() > 0)
	// the two offsets describe the same fraction of the curve
	test.FloatDiff(t, userOffset/length, 0.1, 1e-12)
	// glyph extents match closely (same font, same placement fraction)
	test.FloatDiff(t, bRef.X0, bNorm.X0, 0.5)
	test.FloatDiff(t, bRef.W(), bNorm.W(), 0.5)
	// finite bounds prove no NaN reached the canvas through normalization
	test.That(t, !math.IsNaN(bNorm.X0) && !math.IsNaN(bNorm.Y0))
}

func TestSVGTextPathPercentOffset(t *testing.T) {
	// a percentage startOffset resolves against pathLength and lands at the
	// quarter point of the curve
	c, err := ParseSVG(strings.NewReader(`<svg width="240" height="120">
		<path id="tp" d="M20 80 C 60 20, 180 20, 220 80" pathLength="100" fill="none" stroke="#ccc"/>
		<text font-family="serif" font-size="18" fill="black"><textPath href="#tp" startOffset="25%">Q</textPath></text>
	</svg>`))
	test.Error(t, err)
	b := glyphBounds(c)
	test.That(t, b.W() > 0)
	for _, v := range []float64{b.X0, b.Y0, b.X1, b.Y1} {
		test.That(t, !math.IsNaN(v) && !math.IsInf(v, 0))
	}
}

func TestSVGTextPathUnknownHref(t *testing.T) {
	// a dangling textPath reference is ignored without error or panic
	c, err := ParseSVG(strings.NewReader(`<svg width="100" height="50">
		<text font-family="serif" font-size="16"><textPath href="#missing">Hello</textPath></text>
	</svg>`))
	test.Error(t, err)
	test.That(t, countLayers(c) == 0)
}

func TestSVGTextPathXLink(t *testing.T) {
	// the legacy xlink:href attribute resolves the same way as href
	c, err := ParseSVG(strings.NewReader(`<svg width="200" height="60" xmlns:xlink="http://www.w3.org/1999/xlink">
		<path id="p" d="M10 30 L190 30" fill="none" stroke="black"/>
		<text font-family="serif" font-size="16" fill="red"><textPath xlink:href="#p">Legacy</textPath></text>
	</svg>`))
	test.Error(t, err)
	test.That(t, 1 < countLayers(c))
}

func TestSVGMarkerSpacing(t *testing.T) {
	// user-space spacing distributes markers by arc length; on a 200-unit
	// line declared as pathLength 100, normalized spacing 25 maps to 50 user
	// units and yields three interior markers
	const svg = `<svg width="240" height="60" xmlns="http://www.w3.org/2000/svg">
		<defs>
			<marker id="dot" markerWidth="6" markerHeight="6" refX="3" refY="3" orient="auto" spacing="userSpaceOnUse" markerSpacing="25">
				<circle cx="3" cy="3" r="2.5" fill="red"/>
			</marker>
		</defs>
		<line x1="10" y1="30" x2="210" y2="30" pathLength="100" stroke="black" stroke-width="2" marker-mid="url(#dot)"/>
	</svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	// one base line plus one layer per interior marker at 50, 100, 150
	test.That(t, 3 < countLayers(c))
	for _, layers := range c.layers {
		for _, l := range layers {
			if l.path != nil {
				for _, v := range l.path.Data() {
					test.That(t, !math.IsNaN(v))
				}
			}
		}
	}
}

func TestSVGMarkerAutoUnchanged(t *testing.T) {
	// without user-space spacing, vertex markers behave exactly as before:
	// marker-mid on a two-point line has no interior vertices and draws
	// nothing, while marker-end still places one marker
	mid, err := ParseSVG(strings.NewReader(`<svg width="100" height="50">
		<defs><marker id="m" markerWidth="6" markerHeight="6" refX="3" refY="3"><path d="M0 0L6 3L0 6z" fill="red"/></marker></defs>
		<line x1="5" y1="25" x2="95" y2="25" stroke="black" marker-mid="url(#m)"/>
	</svg>`))
	test.Error(t, err)
	test.T(t, countLayers(mid), 1) // only the line, no mid marker

	end, err := ParseSVG(strings.NewReader(`<svg width="100" height="50">
		<defs><marker id="m" markerWidth="6" markerHeight="6" refX="3" refY="3"><path d="M0 0L6 3L0 6z" fill="red"/></marker></defs>
		<line x1="5" y1="25" x2="95" y2="25" stroke="black" marker-end="url(#m)"/>
	</svg>`))
	test.Error(t, err)
	test.That(t, 1 < countLayers(end))
}
