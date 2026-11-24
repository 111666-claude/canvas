package canvas

import (
	"math"
	"strings"
	"testing"

	"github.com/tdewolff/test"
)

// dashLayer returns the first stroked (shape) layer of a parsed SVG document.
func dashLayer(t *testing.T, svg string) layer {
	t.Helper()
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)
	for _, layers := range c.layers {
		for _, l := range layers {
			if l.path != nil && l.style.HasStroke() {
				return l
			}
		}
	}
	t.Fatal("no stroked layer found")
	return layer{}
}

func TestSVGPathLengthDashLine(t *testing.T) {
	// two lines with identical declared pathLength (100) but different true
	// lengths (100 and 200): the same dash array must map to dash widths that
	// scale with the true length ratio
	small := `<svg width="200" height="50"><line x1="0" y1="10" x2="100" y2="10" pathLength="100" stroke="black" stroke-width="2" stroke-dasharray="10 10"/></svg>`
	large := `<svg width="300" height="50"><line x1="0" y1="10" x2="200" y2="10" pathLength="100" stroke="black" stroke-width="2" stroke-dasharray="10 10"/></svg>`

	ls := dashLayer(t, small)
	ll := dashLayer(t, large)
	// dashCanonical collapses the repeated [10 10] pattern into a single
	// [10] entry; the normalized dash still scales with the length ratio
	test.T(t, ls.style.Dashes, []float64{10.0}) // scale 1
	test.T(t, ll.style.Dashes, []float64{20.0}) // scale 2

	// independent length calculation: Euclidean line length
	independentSmall := math.Hypot(100.0, 0.0)
	independentLarge := math.Hypot(200.0, 0.0)
	test.T(t, independentSmall, 100.0)
	test.T(t, independentLarge, 200.0)
	test.T(t, ll.style.Dashes[0]/ls.style.Dashes[0], independentLarge/independentSmall)

	// stroke width is unaffected by pathLength normalization
	test.T(t, ls.style.StrokeWidth, ll.style.StrokeWidth)
}

func TestSVGPathLengthDashShapes(t *testing.T) {
	// normalization must work for every geometry kind: lines, curves and arcs
	line := dashLayer(t, `<svg width="100" height="100"><line x1="0" y1="10" x2="90" y2="10" pathLength="9" stroke="black" stroke-dasharray="1 1"/></svg>`)
	test.T(t, line.style.Dashes, []float64{10.0})

	cube := dashLayer(t, `<svg width="100" height="100"><path d="M0 50C25 0 75 0 100 50" pathLength="10" stroke="black" fill="none" stroke-dasharray="2 2"/></svg>`)
	trueLength := MustParseSVGPath("M0 50C25 0 75 0 100 50").Length()
	expected := 2.0 * trueLength / 10.0
	test.FloatDiff(t, cube.style.Dashes[0], expected, 1e-9)

	arc := dashLayer(t, `<svg width="100" height="100"><path d="M10 50A40 40 0 0 1 90 50" pathLength="8" stroke="black" fill="none" stroke-dasharray="4"/></svg>`)
	arcLength := MustParseSVGPath("M10 50A40 40 0 0 1 90 50").Length()
	expected = 4.0 * arcLength / 8.0
	test.FloatDiff(t, arc.style.Dashes[0], expected, 1e-9)
}

func TestSVGPathLengthDashOffset(t *testing.T) {
	// positive and negative dash offsets scale with the same factor
	base := dashLayer(t, `<svg width="200" height="50"><line x1="0" y1="10" x2="200" y2="10" pathLength="100" stroke="black" stroke-dasharray="10" stroke-dashoffset="5"/></svg>`)
	test.T(t, base.style.DashOffset, 10.0)

	neg := dashLayer(t, `<svg width="200" height="50"><line x1="0" y1="10" x2="200" y2="10" pathLength="100" stroke="black" stroke-dasharray="10" stroke-dashoffset="-5"/></svg>`)
	test.T(t, neg.style.DashOffset, -10.0)
}

func TestSVGPathLengthClosedRing(t *testing.T) {
	// a closed square ring of perimeter 40 declared as pathLength 20 makes
	// each normalized dash unit span two user units; the ring stays closed
	ring := dashLayer(t, `<svg width="100" height="100"><polygon points="0,0 10,0 10,10 0,10" pathLength="20" stroke="black" fill="none" stroke-dasharray="2 2"/></svg>`)
	test.T(t, ring.path.Closed(), true)
	test.T(t, ring.style.Dashes, []float64{4.0})

	// dashing the ring produces subpaths that are still closed-capable and
	// never NaN coordinates
	dashed := ring.path.Dash(ring.style.DashOffset, ring.style.Dashes...)
	for _, v := range dashed.Data() {
		test.That(t, !math.IsNaN(v))
	}
}

func TestSVGPathLengthInvalid(t *testing.T) {
	// zero and negative declarations fall back to the true length, i.e. the
	// dash pattern is used verbatim and no NaN appears
	for _, pl := range []string{"0", "-5"} {
		l := dashLayer(t, `<svg width="200" height="50"><line x1="0" y1="10" x2="100" y2="10" pathLength="`+pl+`" stroke="black" stroke-dasharray="10 10"/></svg>`)
		test.T(t, l.style.Dashes, []float64{10.0})
		for _, v := range l.style.Dashes {
			test.That(t, !math.IsNaN(v))
		}
	}

	// an overflowing numeric input is rejected by the number parser as a
	// stable parse error rather than producing NaN geometry
	_, err := ParseSVG(strings.NewReader(`<svg width="10" height="10"><line x1="0" y1="0" x2="5" y2="0" pathLength="1e999" stroke="black"/></svg>`))
	test.That(t, err != nil)

	// NaN and Infinity parse as non-finite floats and fall back to the true
	// length, yielding finite dash values rather than propagating NaN
	for _, pl := range []string{"NaN", "Infinity"} {
		l := dashLayer(t, `<svg width="200" height="50"><line x1="0" y1="10" x2="100" y2="10" pathLength="`+pl+`" stroke="black" stroke-dasharray="10 10"/></svg>`)
		for _, v := range l.style.Dashes {
			test.That(t, !math.IsNaN(v) && !math.IsInf(v, 0))
		}
	}

	// the normalization API itself also falls back to the true length for
	// non-finite input without ever returning NaN
	p := MustParseSVGPath("M0 0L100 0")
	test.T(t, p.NormalizeLength(math.Inf(1)), 1.0)
	test.T(t, p.NormalizeLength(math.NaN()), 1.0)

	// an extremely short (point) path does not crash and keeps the dashes
	l := dashLayer(t, `<svg width="10" height="10"><line x1="3" y1="3" x2="3" y2="3" pathLength="10" stroke="black" stroke-dasharray="1 1"/></svg>`)
	for _, v := range l.style.Dashes {
		test.That(t, !math.IsNaN(v))
	}
}

func TestSVGPathLengthTransform(t *testing.T) {
	// pathLength is evaluated in the path's local coordinate frame: a
	// transformed 100-unit line and an untransformed line of the same local
	// length produce identical normalized dashes, while a physically twice
	// as long line without a transform doubles them
	transformed := dashLayer(t, `<svg width="200" height="100"><g transform="translate(0,20) scale(2)"><line x1="0" y1="0" x2="100" y2="0" pathLength="100" stroke="black" stroke-dasharray="10 10"/></g></svg>`)
	local := dashLayer(t, `<svg width="200" height="100"><line x1="0" y1="20" x2="100" y2="20" pathLength="100" stroke="black" stroke-dasharray="10 10"/></svg>`)
	explicit := dashLayer(t, `<svg width="200" height="100"><line x1="0" y1="20" x2="200" y2="20" pathLength="100" stroke="black" stroke-dasharray="10 10"/></svg>`)
	test.T(t, transformed.style.Dashes, local.style.Dashes)
	test.T(t, explicit.style.Dashes[0], 2.0*local.style.Dashes[0])
}

func TestSVGNoPathLengthUnchanged(t *testing.T) {
	// ordinary paths without pathLength keep exactly the authored dash array
	l := dashLayer(t, `<svg width="200" height="50"><line x1="0" y1="10" x2="100" y2="10" stroke="black" stroke-dasharray="7 3 2"/></svg>`)
	test.T(t, l.style.Dashes, []float64{7.0, 3.0, 2.0})
}
