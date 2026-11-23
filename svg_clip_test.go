package canvas

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/tdewolff/test"
)

// parseClipSVG parses an SVG document and fails the test on parser errors.
func parseClipSVG(t *testing.T, svg string) *Canvas {
	t.Helper()
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)
	return c
}

// clipPaintCount returns the number of non-empty painted path layers on z=0.
func clipPaintCount(c *Canvas) int {
	n := 0
	for _, l := range c.layers[0] {
		if l.path != nil && !l.path.Empty() {
			n++
		}
	}
	return n
}

// clipBounds returns the unioned FastBounds of all painted path layers.
func clipBounds(c *Canvas) Rect {
	var r Rect
	first := true
	for _, l := range c.layers[0] {
		if l.path == nil || l.path.Empty() {
			continue
		}
		b := l.path.FastBounds().Transform(l.m)
		if first {
			r, first = b, false
		} else {
			r = r.Add(b)
		}
	}
	return r
}

func TestClipPathUserSpaceOnUse(t *testing.T) {
	// userSpaceOnUse: clip rect coordinates are in the user coordinate system
	// of the referencing element, independent of the target's own bounds.
	svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<clipPath id="c">
			<rect x="10" y="10" width="30" height="30"/>
		</clipPath>
	</defs>
	<rect x="0" y="0" width="100" height="100" fill="red" clip-path="url(#c)"/>
</svg>`
	c := parseClipSVG(t, svg)
	test.That(t, clipPaintCount(c) == 1)

	// canvas is CartesianI after clipping; SVG point (x,y) lands at (x, H-y)
	b := clipBounds(c)
	test.T(t, math.Round(b.X0), 10.0)
	test.T(t, math.Round(b.Y0), 60.0)
	test.T(t, math.Round(b.X1), 40.0)
	test.T(t, math.Round(b.Y1), 90.0)
}

func TestClipPathObjectBoundingBox(t *testing.T) {
	// objectBoundingBox: unit cube maps onto the target's bounding box. A
	// 0..0.5 square must cover exactly the lower-left (SVG) quarter of the
	// target rect, whatever the target's position and size.
	svg := `<svg width="200" height="200" viewBox="0 0 200 200">
	<defs>
		<clipPath id="c" clipPathUnits="objectBoundingBox">
			<rect x="0" y="0" width="0.5" height="0.5"/>
		</clipPath>
	</defs>
	<rect x="40" y="20" width="80" height="60" fill="blue" clip-path="url(#c)"/>
</svg>`
	c := parseClipSVG(t, svg)
	test.That(t, clipPaintCount(c) == 1)
	b := clipBounds(c)
	// SVG x: 40..80 ; SVG y: 20..50 -> canvas y: 200-50 .. 200-20
	test.T(t, math.Round(b.X0), 40.0)
	test.T(t, math.Round(b.X1), 80.0)
	test.T(t, math.Round(b.Y0), 150.0)
	test.T(t, math.Round(b.Y1), 180.0)
}

func TestClipPathTargetTransform(t *testing.T) {
	// a transform on the clipped element moves both target and clip together
	// for userSpaceOnUse coordinates.
	svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<clipPath id="c">
			<rect x="10" y="10" width="20" height="20"/>
		</clipPath>
	</defs>
	<g transform="translate(30,40)">
		<rect x="0" y="0" width="100" height="100" fill="green" clip-path="url(#c)"/>
	</g>
</svg>`
	c := parseClipSVG(t, svg)
	b := clipBounds(c)
	// clip rect SVG (10,10)-(30,30), translated by (30,40): SVG (40,50)-(60,70)
	test.T(t, math.Round(b.X0), 40.0)
	test.T(t, math.Round(b.X1), 60.0)
	test.T(t, math.Round(b.Y0), 30.0) // 100-70
	test.T(t, math.Round(b.Y1), 50.0) // 100-50
}

func TestClipPathOwnTransform(t *testing.T) {
	// the clipPath element's own transform applies to its contents only.
	svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<clipPath id="c" transform="translate(20,0)">
			<rect x="0" y="0" width="10" height="100"/>
		</clipPath>
	</defs>
	<rect x="0" y="0" width="100" height="100" fill="red" clip-path="url(#c)"/>
</svg>`
	c := parseClipSVG(t, svg)
	b := clipBounds(c)
	test.T(t, math.Round(b.X0), 20.0)
	test.T(t, math.Round(b.X1), 30.0)
	test.T(t, math.Round(b.Y0), 0.0)
	test.T(t, math.Round(b.Y1), 100.0)
}

func TestClipPathNestedPaths(t *testing.T) {
	// multiple subpaths and group transforms inside one clipPath
	svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<clipPath id="c">
			<g transform="translate(0,40)">
				<rect x="0" y="0" width="10" height="10"/>
				<rect x="20" y="0" width="10" height="10"/>
			</g>
		</clipPath>
	</defs>
	<rect x="0" y="0" width="100" height="100" fill="red" clip-path="url(#c)"/>
</svg>`
	c := parseClipSVG(t, svg)
	b := clipBounds(c)
	// two disjoint bars: SVG x 0..10 and 20..30, y 40..50
	test.T(t, math.Round(b.X0), 0.0)
	test.T(t, math.Round(b.X1), 30.0)
	test.T(t, math.Round(b.Y0), 50.0) // 100-50
	test.T(t, math.Round(b.Y1), 60.0) // 100-40
}

func TestClipPathNestedClipFixtures(t *testing.T) {
	// clipPath element carrying its own clip-path: both apply by intersection
	svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
	<clipPath id="outer">
		<rect x="0" y="0" width="50" height="50"/>
	</clipPath>
	<clipPath id="inner" clip-path="url(#outer)">
		<rect x="25" y="25" width="50" height="50"/>
	</clipPath>
	</defs>
	<rect x="0" y="0" width="100" height="100" fill="red" clip-path="url(#inner)"/>
</svg>`
	c := parseClipSVG(t, svg)
	b := clipBounds(c)
	// intersection of SVG rects (0,0,50,50) and (25,25,75,75): (25,25)-(50,50)
	test.T(t, math.Round(b.X0), 25.0)
	test.T(t, math.Round(b.X1), 50.0)
	test.T(t, math.Round(b.Y0), 50.0) // 100-50
	test.T(t, math.Round(b.Y1), 75.0) // 100-25
}

func TestClipPathGroupWithChildren(t *testing.T) {
	// clip-path on a group applies to all children, in document order
	svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<clipPath id="c">
			<rect x="10" y="10" width="20" height="80"/>
		</clipPath>
	</defs>
	<g clip-path="url(#c)">
		<rect x="0" y="0" width="40" height="40" fill="red"/>
		<rect x="0" y="50" width="40" height="40" fill="blue"/>
	</g>
</svg>`
	c := parseClipSVG(t, svg)
	// both children survive, both constrained to x in 10..30
	test.That(t, clipPaintCount(c) == 2)
	for _, l := range c.layers[0] {
		if l.path == nil {
			continue
		}
		b := l.path.FastBounds()
		test.T(t, math.Round(b.X0), 10.0)
		test.T(t, math.Round(b.X1), 30.0)
	}
}

func TestClipPathNegativeOrigin(t *testing.T) {
	// negative coordinates in both clip geometry and target transform
	svg := `<svg width="100" height="100" viewBox="-50 -50 100 100">
	<defs>
		<clipPath id="c">
			<rect x="-40" y="-40" width="30" height="30"/>
		</clipPath>
	</defs>
	<g transform="translate(-10,-10)">
		<rect x="-50" y="-50" width="100" height="100" fill="red" clip-path="url(#c)"/>
	</g>
</svg>`
	c := parseClipSVG(t, svg)
	b := clipBounds(c)
	// clip SVG (-40,-40)-(-10,-10) translated (-10,-10): (-50,-50)-(-20,-20)
	// canvas mapping for viewBox offset: user (x,y) -> canvas (x+50, 50-y)
	test.T(t, math.Round(b.X0), 0.0)   // -50+50
	test.T(t, math.Round(b.X1), 30.0)  // -20+50
	test.T(t, math.Round(b.Y0), 70.0)  // 50-(-20)
	test.T(t, math.Round(b.Y1), 100.0) // 50-(-50)
}

func TestClipPathErrorAndEdgeCases(t *testing.T) {
	t.Run("unknown reference hides content", func(t *testing.T) {
		// a missing definition is a contextual error or an empty clip; either
		// way the clipped content must not be rendered
		svg := `<svg width="10" height="10"><rect width="10" height="10" fill="red" clip-path="url(#missing)"/></svg>`
		c, err := ParseSVG(strings.NewReader(svg))
		test.That(t, err == nil || strings.Contains(err.Error(), "clipPath"))
		test.That(t, clipPaintCount(c) == 0)
	})

	t.Run("invalid units hides content", func(t *testing.T) {
		svg := `<svg width="10" height="10">
		<defs><clipPath id="c" clipPathUnits="bogus"><rect width="5" height="5"/></clipPath></defs>
		<rect width="10" height="10" fill="red" clip-path="url(#c)"/></svg>`
		c, err := ParseSVG(strings.NewReader(svg))
		test.That(t, err != nil)
		test.That(t, strings.Contains(err.Error(), "clipPathUnits"))
		test.That(t, clipPaintCount(c) == 0)
	})

	t.Run("empty clip path hides content", func(t *testing.T) {
		svg := `<svg width="10" height="10">
		<defs><clipPath id="c"></clipPath></defs>
		<rect width="10" height="10" fill="red" clip-path="url(#c)"/></svg>`
		c := parseClipSVG(t, svg)
		test.That(t, clipPaintCount(c) == 0)
	})

	t.Run("zero width bounding box disables clip", func(t *testing.T) {
		svg := `<svg width="10" height="10">
		<defs><clipPath id="c" clipPathUnits="objectBoundingBox"><rect width="0.5" height="0.5"/></clipPath></defs>
		<line x1="5" y1="0" x2="5" y2="10" stroke="red" stroke-width="2" clip-path="url(#c)"/></svg>`
		c := parseClipSVG(t, svg)
		// the line's fill bounds have zero width before stroking; the clip
		// is disabled and the stroked line renders unchanged
		test.That(t, clipPaintCount(c) == 1)
	})

	t.Run("cyclic reference hides content", func(t *testing.T) {
		svg := `<svg width="10" height="10">
		<defs>
		<clipPath id="a" clip-path="url(#b)"><rect width="5" height="5"/></clipPath>
		<clipPath id="b" clip-path="url(#a)"><rect width="5" height="5"/></clipPath>
		</defs>
		<rect width="10" height="10" fill="red" clip-path="url(#a)"/></svg>`
		c, err := ParseSVG(strings.NewReader(svg))
		test.That(t, err != nil)
		test.That(t, strings.Contains(err.Error(), "cyclic"))
		test.That(t, clipPaintCount(c) == 0)
	})

	t.Run("non-finite target matrix hides content", func(t *testing.T) {
		svg := `<svg width="10" height="10">
		<defs><clipPath id="c"><rect width="5" height="5"/></clipPath></defs>
		<rect width="10" height="10" fill="red" transform="scale(Infinity,1)" clip-path="url(#c)"/></svg>`
		c, err := ParseSVG(strings.NewReader(svg))
		test.That(t, err != nil)
		test.That(t, clipPaintCount(c) == 0)
	})

	t.Run("clip outside clip region is stable empty", func(t *testing.T) {
		// target entirely outside the clip: deterministic no-paint result
		svg := `<svg width="100" height="100">
		<defs><clipPath id="c"><rect x="0" y="0" width="10" height="10"/></clipPath></defs>
		<rect x="80" y="80" width="10" height="10" fill="red" clip-path="url(#c)"/></svg>`
		c := parseClipSVG(t, svg)
		test.That(t, clipPaintCount(c) == 0)
	})

	t.Run("clipPath outside defs is supported", func(t *testing.T) {
		svg := `<svg width="100" height="100">
		<clipPath id="c"><rect x="10" y="10" width="20" height="20"/></clipPath>
		<rect width="100" height="100" fill="red" clip-path="url(#c)"/></svg>`
		c := parseClipSVG(t, svg)
		test.That(t, clipPaintCount(c) == 1)
		b := clipBounds(c)
		test.T(t, math.Round(b.X0), 10.0)
	})

	t.Run("nested clip attributes intersect", func(t *testing.T) {
		// clip-path on a parent group and on the child both apply
		svg := `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
	<clipPath id="a"><rect x="0" y="0" width="60" height="100"/></clipPath>
	<clipPath id="b"><rect x="40" y="0" width="60" height="100"/></clipPath>
	</defs>
	<g clip-path="url(#a)">
		<rect x="0" y="0" width="100" height="100" fill="red" clip-path="url(#b)"/>
	</g>
</svg>`
		c := parseClipSVG(t, svg)
		b := clipBounds(c)
		test.T(t, math.Round(b.X0), 40.0)
		test.T(t, math.Round(b.X1), 60.0)
	})

	t.Run("clip path via css style attribute", func(t *testing.T) {
		svg := `<svg width="100" height="100">
	<defs><clipPath id="c"><rect x="10" y="10" width="20" height="20"/></clipPath></defs>
	<rect width="100" height="100" fill="red" style="clip-path:url(#c)"/></svg>`
		c := parseClipSVG(t, svg)
		test.That(t, clipPaintCount(c) == 1)
		b := clipBounds(c)
		test.T(t, math.Round(b.X0), 10.0)
	})

	t.Run("polygon and circle clip shapes", func(t *testing.T) {
		svg := `<svg width="100" height="100">
	<defs><clipPath id="p"><polygon points="0,0 50,0 50,50"/></clipPath></defs>
	<rect width="100" height="100" fill="red" clip-path="url(#p)"/></svg>`
		c := parseClipSVG(t, svg)
		test.That(t, clipPaintCount(c) == 1)

		svg2 := `<svg width="100" height="100">
	<defs><clipPath id="ci"><circle cx="30" cy="30" r="20"/></clipPath></defs>
	<rect width="100" height="100" fill="red" clip-path="url(#ci)"/></svg>`
		c2 := parseClipSVG(t, svg2)
		test.That(t, clipPaintCount(c2) == 1)
	})
}

// TestClipPathRandomizedNoPanic feeds randomized clipPath documents, including
// negative coordinates, both unit systems and both fill rules, to ensure the
// parser always terminates with a stable result rather than panicking.
func TestClipPathRandomizedNoPanic(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	units := []string{"", ` clipPathUnits="userSpaceOnUse"`, ` clipPathUnits="objectBoundingBox"`, ` clipPathUnits="bad"`}
	rules := []string{"nonzero", "evenodd"}
	for i := 0; i < 200; i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on iteration %d: %v", i, r)
				}
			}()
			n := func() int { return rng.IntN(120) - 20 }
			var b strings.Builder
			b.WriteString(`<svg width="100" height="100" viewBox="0 0 100 100"><defs>`)
			for k := 0; k < 3; k++ {
				b.WriteString(`<clipPath id="c` + string(rune('0'+k)) + `"` + units[rng.IntN(len(units))] + `>`)
				for j := 0; j < rng.IntN(3)+1; j++ {
					switch rng.IntN(4) {
					case 0:
						b.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d"/>`, n(), n(), rng.IntN(60), rng.IntN(60)))
					case 1:
						b.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d"/>`, n(), n(), rng.IntN(40)))
					case 2:
						b.WriteString(fmt.Sprintf(`<path d="M%d,%d H%d V%d Z" fill-rule="%s"/>`, n(), n(), n(), n(), rules[rng.IntN(2)]))
					default:
						b.WriteString(fmt.Sprintf(`<polygon points="%d,%d %d,%d %d,%d"/>`, n(), n(), n(), n(), n(), n()))
					}
				}
				b.WriteString(`</clipPath>`)
			}
			b.WriteString(`</defs>`)
			for k := 0; k < 4; k++ {
				cid := "c" + string(rune('0'+rng.IntN(3)))
				b.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="red" clip-path="url(#%s)"/>`, n(), n(), rng.IntN(80)+1, rng.IntN(80)+1, cid))
			}
			b.WriteString(`</svg>`)
			_, _ = ParseSVG(strings.NewReader(b.String()))
		}()
	}
}
