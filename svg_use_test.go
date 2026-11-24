package canvas

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/tdewolff/test"
)

// svgUseFixture is an icon-library style document: a symbol instantiated three
// times with different transforms, plus a preserveAspectRatio="none" variant.
const svgUseFixture = `<svg width="300" height="120" viewBox="0 0 300 120">
	<defs>
		<symbol id="icon" viewBox="0 0 24 24">
			<path d="M2 2 L22 2 L12 22 z"/>
		</symbol>
		<symbol id="iconNone" viewBox="0 0 24 24" preserveAspectRatio="none">
			<path d="M2 2 L22 2 L12 22 z"/>
		</symbol>
	</defs>
	<use href="#icon" x="10" y="10" width="48" height="24" fill="red"/>
	<use href="#icon" x="100" y="10" width="24" height="24" fill="green" transform="rotate(90 112 22)"/>
	<use href="#iconNone" x="200" y="10" width="48" height="24" fill="blue"/>
</svg>`

// svgUseFixtureExpanded is svgUseFixture with every <use> manually expanded.
// Instance 1: viewport 48x24 at (10,10), viewBox 24x24, xMidYMid meet gives
// scale 1 and an x offset of (48-24)/2=12, i.e. translate(22,10).
// Instance 2: scale 1, the use transform applies around the instance.
// Instance 3: preserveAspectRatio="none" stretches 24x24 onto 48x24.
const svgUseFixtureExpanded = `<svg width="300" height="120" viewBox="0 0 300 120">
	<g transform="translate(22,10)" fill="red"><path d="M2 2 L22 2 L12 22 z"/></g>
	<g transform="rotate(90 112 22)"><g transform="translate(100,10)" fill="green"><path d="M2 2 L22 2 L12 22 z"/></g></g>
	<g transform="translate(200,10) scale(2,1)" fill="blue"><path d="M2 2 L22 2 L12 22 z"/></g>
</svg>`

const svgUseTransparencyFixture = `<svg width="100" height="100" viewBox="0 0 100 100">
	<defs>
		<symbol id="dot" viewBox="0 0 10 10">
			<circle cx="5" cy="5" r="4"/>
		</symbol>
	</defs>
	<use href="#dot" x="10" y="10" width="10" height="10" fill="rgba(255,0,0,0.5)"/>
	<use href="#dot" x="30" y="10" width="10" height="10" fill="#00ff00" fill-opacity="0.25"/>
	<use href="#dot" x="50" y="10" width="10" height="10" fill="none" stroke="#0000ff" stroke-opacity="0.5" stroke-width="2"/>
</svg>`

const svgUseTransparencyFixtureExpanded = `<svg width="100" height="100" viewBox="0 0 100 100">
	<g transform="translate(10,10)" fill="rgba(255,0,0,0.5)"><circle cx="5" cy="5" r="4"/></g>
	<g transform="translate(30,10)" fill="#00ff00" fill-opacity="0.25"><circle cx="5" cy="5" r="4"/></g>
	<g transform="translate(50,10)" fill="none" stroke="#0000ff" stroke-opacity="0.5" stroke-width="2"><circle cx="5" cy="5" r="4"/></g>
</svg>`

func svgLayersEqual(t *testing.T, c1, c2 *Canvas) {
	layers1, layers2 := c1.layers[0], c2.layers[0]
	if len(layers1) != len(layers2) {
		t.Fatalf("layer count differs: %d != %d", len(layers1), len(layers2))
	}
	for i := range layers1 {
		l1, l2 := layers1[i], layers2[i]
		if (l1.path == nil) != (l2.path == nil) || (l1.text == nil) != (l2.text == nil) {
			t.Fatalf("layer %d: kind differs", i)
		}
		for r := 0; r < 2; r++ {
			for c := 0; c < 3; c++ {
				if 1e-6 < math.Abs(l1.m[r][c]-l2.m[r][c]) {
					t.Errorf("layer %d: transform differs: %v != %v", i, l1.m, l2.m)
				}
			}
		}
		if l1.path != nil {
			b1, b2 := l1.path.FastBounds(), l2.path.FastBounds()
			if 1e-6 < math.Abs(b1.X0-b2.X0) || 1e-6 < math.Abs(b1.Y0-b2.Y0) ||
				1e-6 < math.Abs(b1.X1-b2.X1) || 1e-6 < math.Abs(b1.Y1-b2.Y1) {
				t.Errorf("layer %d: bounds differ: %v != %v", i, b1, b2)
			}
			if l1.style.Fill.Color != l2.style.Fill.Color {
				t.Errorf("layer %d: fill differs: %v != %v", i, l1.style.Fill.Color, l2.style.Fill.Color)
			}
			if l1.style.Stroke.Color != l2.style.Stroke.Color {
				t.Errorf("layer %d: stroke differs: %v != %v", i, l1.style.Stroke.Color, l2.style.Stroke.Color)
			}
		}
	}
}

// TestParseSVGUseTransforms instantiates the same symbol three times with
// different transforms and compares the result against the manually expanded
// document.
func TestParseSVGUseTransforms(t *testing.T) {
	c1, err := ParseSVG(strings.NewReader(svgUseFixture))
	test.Error(t, err)
	c2, err := ParseSVG(strings.NewReader(svgUseFixtureExpanded))
	test.Error(t, err)

	if len(c1.layers[0]) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(c1.layers[0]))
	}
	svgLayersEqual(t, c1, c2)

	// all three instances must be at distinct positions, i.e. the defs source
	// node was not mutated by a previous instantiation
	b0 := c1.layers[0][0].path.Copy().Transform(c1.layers[0][0].m).FastBounds()
	b1 := c1.layers[0][1].path.Copy().Transform(c1.layers[0][1].m).FastBounds()
	b2 := c1.layers[0][2].path.Copy().Transform(c1.layers[0][2].m).FastBounds()
	if b0 == b1 || b1 == b2 || b0 == b2 {
		t.Error("expected three distinct instance positions")
	}
}

// TestParseSVGUseTransparency checks that transparent styles (rgba fill,
// fill-opacity, fill=none with stroke-opacity) inherit into the instance
// exactly as if the content were expanded in place.
func TestParseSVGUseTransparency(t *testing.T) {
	c1, err := ParseSVG(strings.NewReader(svgUseTransparencyFixture))
	test.Error(t, err)
	c2, err := ParseSVG(strings.NewReader(svgUseTransparencyFixtureExpanded))
	test.Error(t, err)

	if len(c1.layers[0]) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(c1.layers[0]))
	}
	svgLayersEqual(t, c1, c2)

	// premultiplied rgba(255,0,0,0.5)
	test.T(t, c1.layers[0][0].style.Fill.Color, color.RGBA{127, 0, 0, 127})
	// fill-opacity applies on top of the inherited fill
	if c1.layers[0][1].style.Fill.Color.A == 255 {
		t.Error("expected fill-opacity to be applied")
	}
	// fill=none keeps a transparent fill but an opaque-ish stroke
	test.T(t, c1.layers[0][2].style.Fill.Color, Transparent)
	if c1.layers[0][2].style.Stroke.Color.A == 255 {
		t.Error("expected stroke-opacity to be applied")
	}
}

// TestParseSVGUseNested checks nested <use> (a symbol referencing a group
// referencing a path), instantiation of g and path elements, and that the
// render order equals document order.
func TestParseSVGUseNested(t *testing.T) {
	svg := `<svg width="100" height="50" viewBox="0 0 100 50">
		<defs>
			<path id="tri" d="M0 0 L10 0 L5 10 z"/>
			<g id="pair">
				<use href="#tri"/>
				<use href="#tri" x="15"/>
			</g>
			<symbol id="combo" viewBox="0 0 30 10">
				<use href="#pair"/>
			</symbol>
		</defs>
		<rect x="0" y="20" width="5" height="5" fill="red"/>
		<use href="#combo" x="10" y="10" width="30" height="10" fill="blue"/>
		<rect x="50" y="20" width="5" height="5" fill="green"/>
	</svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)

	layers := c.layers[0]
	if len(layers) != 4 {
		t.Fatalf("expected 4 layers (rect, tri, tri, rect), got %d", len(layers))
	}
	test.T(t, layers[0].style.Fill.Color, color.RGBA{255, 0, 0, 255})
	test.T(t, layers[1].style.Fill.Color, color.RGBA{0, 0, 255, 255})
	test.T(t, layers[2].style.Fill.Color, color.RGBA{0, 0, 255, 255})
	test.T(t, layers[3].style.Fill.Color, color.RGBA{0, 128, 0, 255})

	// the second triangle is offset by x=15 from the first (user units map
	// 1:1 onto millimeters at the root viewport)
	b1 := layers[1].path.Copy().Transform(layers[1].m).FastBounds()
	b2 := layers[2].path.Copy().Transform(layers[2].m).FastBounds()
	test.Float(t, b2.X0-b1.X0, 15.0)
}

// TestParseSVGUseSymbolNotRendered checks that a top-level <symbol> is not
// rendered directly but can be instantiated.
func TestParseSVGUseSymbolNotRendered(t *testing.T) {
	svg := `<svg width="50" height="50" viewBox="0 0 50 50">
		<symbol id="star" viewBox="0 0 10 10"><path d="M0 0L10 0L5 10z"/></symbol>
		<use href="#star" x="5" y="5" width="10" height="10"/>
	</svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)
	if len(c.layers[0]) != 1 {
		t.Fatalf("expected 1 layer (only the instance), got %d", len(c.layers[0]))
	}
}

// TestParseSVGUseZeroSize checks bounded degradation for zero sizes: the
// instance is not rendered and no error is reported.
func TestParseSVGUseZeroSize(t *testing.T) {
	svg := `<svg width="50" height="50" viewBox="0 0 50 50">
		<defs><symbol id="s" viewBox="0 0 10 10"><path d="M0 0L10 10"/></symbol></defs>
		<use href="#s" x="5" y="5" width="0" height="10"/>
	</svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	test.Error(t, err)
	if len(c.layers[0]) != 0 {
		t.Fatalf("expected no layers, got %d", len(c.layers[0]))
	}
}

func TestParseSVGUseErrors(t *testing.T) {
	var tests = []struct {
		name string
		svg  string
		err  string
	}{
		{
			"missing href",
			`<svg width="10" height="10"><use x="0" y="0"/></svg>`,
			"missing href",
		},
		{
			"empty reference",
			`<svg width="10" height="10"><use href="#"/></svg>`,
			"empty reference",
		},
		{
			"unknown reference",
			`<svg width="10" height="10"><use href="#nope"/></svg>`,
			"unknown reference: #nope",
		},
		{
			"external reference",
			`<svg width="10" height="10"><use href="icons.svg#a"/></svg>`,
			"external reference not supported: icons.svg#a",
		},
		{
			"duplicate ID",
			`<svg width="10" height="10"><defs><symbol id="a" viewBox="0 0 1 1"><path d="M0 0L1 1"/></symbol><symbol id="a" viewBox="0 0 1 1"><path d="M0 0L1 1"/></symbol></defs></svg>`,
			"duplicate ID: a",
		},
		{
			"circular self",
			`<svg width="10" height="10"><defs><g id="a"><use href="#a"/></g></defs><use href="#a"/></svg>`,
			"circular reference: a -> a",
		},
		{
			"circular chain",
			`<svg width="10" height="10"><defs><g id="a"><use href="#b"/></g><g id="b"><use href="#a"/></g></defs><use href="#a"/></svg>`,
			"circular reference: a -> b -> a",
		},
		{
			"negative size",
			`<svg width="10" height="10"><defs><symbol id="s" viewBox="0 0 1 1"><path d="M0 0L1 1"/></symbol></defs><use href="#s" width="-5" height="5"/></svg>`,
			"negative width/height",
		},
		{
			"bad viewBox",
			`<svg width="10" height="10"><defs><symbol id="s" viewBox="0 0 1"><path d="M0 0L1 1"/></symbol></defs><use href="#s" width="5" height="5"/></svg>`,
			"bad viewBox",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseSVG(strings.NewReader(tt.svg))
			if err == nil {
				t.Fatalf("expected error containing %q", tt.err)
			}
			if !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.err)
			}
			if c == nil {
				t.Fatal("expected partial canvas to be returned")
			}
		})
	}
}

// TestParseSVGUseDeepNesting checks that extremely deep (but acyclic) nesting
// of <use> references degrades with a bounded error instead of recursing
// forever.
func TestParseSVGUseDeepNesting(t *testing.T) {
	sb := strings.Builder{}
	sb.WriteString(`<svg width="10" height="10"><defs>`)
	depth := svgMaxUseDepth + 10
	for i := 0; i < depth; i++ {
		fmt.Fprintf(&sb, `<g id="g%d"><use href="#g%d"/></g>`, i, i+1)
	}
	fmt.Fprintf(&sb, `<path id="g%d" d="M0 0L1 1"/>`, depth)
	sb.WriteString(`</defs><use href="#g0"/></svg>`)

	c, err := ParseSVG(strings.NewReader(sb.String()))
	if err == nil {
		t.Fatal("expected maximum depth error")
	}
	if !strings.Contains(err.Error(), "maximum instantiation depth") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "g0 -> g1") {
		t.Fatalf("error does not contain reference chain: %v", err)
	}
	if c == nil {
		t.Fatal("expected partial canvas to be returned")
	}
}

// TestParseSVGUseInstanceExplosion checks that exponentially expanding <use>
// graphs (each group instantiating the next twice) are bounded.
func TestParseSVGUseInstanceExplosion(t *testing.T) {
	sb := strings.Builder{}
	sb.WriteString(`<svg width="10" height="10"><defs>`)
	depth := 14 // 2^14 = 16384 instances without a bound
	for i := 0; i < depth; i++ {
		fmt.Fprintf(&sb, `<g id="d%d"><use href="#d%d"/><use href="#d%d"/></g>`, i, i+1, i+1)
	}
	fmt.Fprintf(&sb, `<path id="d%d" d="M0 0L1 1"/>`, depth)
	sb.WriteString(`</defs><use href="#d0"/></svg>`)

	c, err := ParseSVG(strings.NewReader(sb.String()))
	if err == nil {
		t.Fatal("expected maximum instance count error")
	}
	if !strings.Contains(err.Error(), "maximum instantiation count") {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected partial canvas to be returned")
	}
}

// TestParseSVGUseDuplicateIDFirstWins checks that on duplicate IDs the first
// definition is used for instantiation.
func TestParseSVGUseDuplicateIDFirstWins(t *testing.T) {
	svg := `<svg width="10" height="10" viewBox="0 0 10 10">
		<defs>
			<path id="p" d="M0 0L1 1"/>
			<path id="p" d="M5 5L9 9"/>
		</defs>
		<use href="#p"/>
	</svg>`
	c, err := ParseSVG(strings.NewReader(svg))
	if err == nil || !strings.Contains(err.Error(), "duplicate ID") {
		t.Fatalf("expected duplicate ID error, got %v", err)
	}
	if len(c.layers[0]) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(c.layers[0]))
	}
	b := c.layers[0][0].path.FastBounds()
	if 2.0 < b.X0 { // the first path lives in [0,1]mm-ish, the second in [5,9]
		t.Error("expected the first definition to win")
	}
}
