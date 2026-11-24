package canvas_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	csvg "github.com/tdewolff/canvas/renderers/svg"
)

// useDocument is an icon-library style document: symbols in <defs>
// instantiated three times with different transforms and transparent styles.
// The clip-path attribute is intentionally included: the parser does not
// support clipPath, so the instance must behave exactly as the expanded
// equivalent (which also ignores it).
const useDocument = `<svg width="300" height="120" viewBox="0 0 300 120">
	<defs>
		<symbol id="icon" viewBox="0 0 24 24">
			<path d="M2 2 L22 2 L12 22 z"/>
			<circle cx="12" cy="9" r="3" fill="rgba(0,0,0,0.5)"/>
		</symbol>
		<symbol id="iconNone" viewBox="0 0 24 24" preserveAspectRatio="none">
			<path d="M2 2 L22 2 L12 22 z"/>
		</symbol>
	</defs>
	<use href="#icon" x="10" y="10" width="48" height="24" fill="red" fill-opacity="0.75"/>
	<use href="#icon" x="100" y="10" width="24" height="24" fill="green" transform="rotate(90 112 22)" clip-path="url(#clip)"/>
	<use href="#iconNone" x="200" y="10" width="48" height="24" fill="blue"/>
</svg>`

// expandedDocument is useDocument with every <use> manually expanded.
const expandedDocument = `<svg width="300" height="120" viewBox="0 0 300 120">
	<g transform="translate(22,10)" fill="red" fill-opacity="0.75">
		<path d="M2 2 L22 2 L12 22 z"/>
		<circle cx="12" cy="9" r="3" fill="rgba(0,0,0,0.5)"/>
	</g>
	<g transform="rotate(90 112 22)" clip-path="url(#clip)"><g transform="translate(100,10)" fill="green">
		<path d="M2 2 L22 2 L12 22 z"/>
		<circle cx="12" cy="9" r="3" fill="rgba(0,0,0,0.5)"/>
	</g></g>
	<g transform="translate(200,10) scale(2,1)" fill="blue">
		<path d="M2 2 L22 2 L12 22 z"/>
	</g>
</svg>`

func renderToSVGString(t *testing.T, c *canvas.Canvas) string {
	t.Helper()
	var buf bytes.Buffer
	w, h := c.Size()
	r := csvg.New(&buf, w, h, nil)
	c.RenderTo(r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestParseSVGUseEquivalentToExpansion parses a document using <use> and the
// manually expanded equivalent, and checks that both produce identical SVG
// renderer output and identical rasterized pixels.
func TestParseSVGUseEquivalentToExpansion(t *testing.T) {
	cUse, err := canvas.ParseSVG(strings.NewReader(useDocument))
	if err != nil {
		t.Fatal(err)
	}
	cExpanded, err := canvas.ParseSVG(strings.NewReader(expandedDocument))
	if err != nil {
		t.Fatal(err)
	}

	// SVG renderer output must be identical
	svgUse := renderToSVGString(t, cUse)
	svgExpanded := renderToSVGString(t, cExpanded)
	if svgUse != svgExpanded {
		t.Fatalf("SVG output differs:\n%s\nvs.\n%s", svgUse, svgExpanded)
	}

	// rasterized pixels must be identical
	imgUse := rasterizer.Draw(cUse, canvas.DPMM(2.0), canvas.DefaultColorSpace)
	imgExpanded := rasterizer.Draw(cExpanded, canvas.DPMM(2.0), canvas.DefaultColorSpace)
	if !imgUse.Bounds().Eq(imgExpanded.Bounds()) {
		t.Fatalf("image bounds differ: %v != %v", imgUse.Bounds(), imgExpanded.Bounds())
	}
	if !bytes.Equal(imgUse.Pix, imgExpanded.Pix) {
		t.Fatal("rasterized pixels differ")
	}

	// sanity: something was actually drawn (not all transparent)
	transparent := true
	for i := 3; i < len(imgUse.Pix); i += 4 {
		if imgUse.Pix[i] != 0 {
			transparent = false
			break
		}
	}
	if transparent {
		t.Fatal("expected non-empty rendering")
	}
}

// TestParseSVGUseBadReferenceRendering checks that a document with a bad
// <use> reference still rasterizes the remaining content (bounded
// degradation).
func TestParseSVGUseBadReferenceRendering(t *testing.T) {
	doc := `<svg width="100" height="50" viewBox="0 0 100 50">
		<defs><symbol id="ok" viewBox="0 0 10 10"><path d="M0 0L10 0L5 10z"/></symbol></defs>
		<use href="#missing" x="0" y="0" width="10" height="10"/>
		<use href="#ok" x="20" y="20" width="10" height="10" fill="red"/>
	</svg>`
	c, err := canvas.ParseSVG(strings.NewReader(doc))
	if err == nil {
		t.Fatal("expected unknown reference error")
	}
	if !strings.Contains(err.Error(), "unknown reference: #missing") {
		t.Fatalf("unexpected error: %v", err)
	}

	img := rasterizer.Draw(c, canvas.DPMM(2.0), canvas.DefaultColorSpace)
	drawn := false
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			drawn = true
			break
		}
	}
	if !drawn {
		t.Fatal("expected the valid instance to be rendered despite the bad reference")
	}
}
