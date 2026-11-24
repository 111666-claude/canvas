package svg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	cimage "github.com/tdewolff/canvas/image"
	"github.com/tdewolff/canvas/renderers/rasterizer"
)

////////////////////////////////////////////////////////////////

var urlRefRe = regexp.MustCompile(`url\(#([^)]+)\)`)

// svgDoc is a structural summary of an SVG document.
type svgDoc struct {
	counts map[string]int  // element name -> number of occurrences
	ids    map[string]bool // defined IDs
	refs   []string        // referenced IDs (url(#..) and href="#..")
}

func parseSVGDoc(t *testing.T, s string) svgDoc {
	t.Helper()
	doc := svgDoc{counts: map[string]int{}, ids: map[string]bool{}}
	decoder := xml.NewDecoder(strings.NewReader(s))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("malformed SVG: %v", err)
		}
		if se, ok := token.(xml.StartElement); ok {
			doc.counts[se.Name.Local]++
			for _, attr := range se.Attr {
				if attr.Name.Local == "id" {
					doc.ids[attr.Value] = true
				}
				for _, m := range urlRefRe.FindAllStringSubmatch(attr.Value, -1) {
					doc.refs = append(doc.refs, m[1])
				}
				if attr.Name.Local == "href" && strings.HasPrefix(attr.Value, "#") {
					doc.refs = append(doc.refs, attr.Value[1:])
				}
			}
		}
	}
	return doc
}

// checkRefs verifies that all references resolve to a defined ID.
func (doc svgDoc) checkRefs(t *testing.T) {
	t.Helper()
	for _, ref := range doc.refs {
		if !doc.ids[ref] {
			t.Fatalf("reference #%s does not resolve to a defined resource", ref)
		}
	}
}

func renderSVG(t *testing.T, c *canvas.Canvas, mutate func(*Options)) string {
	t.Helper()
	opts := DefaultOptions
	if mutate != nil {
		mutate(&opts)
	}
	buf := &bytes.Buffer{}
	svg := New(buf, c.W, c.H, &opts)
	c.RenderTo(svg)
	if err := svg.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func newTestGradient() *canvas.LinearGradient {
	grad := canvas.NewLinearGradient(canvas.Point{X: 0.0, Y: 0.0}, canvas.Point{X: 10.0, Y: 0.0})
	grad.Add(0.0, canvas.Red)
	grad.Add(0.5, color.RGBA{0, 255, 0, 255})
	grad.Add(1.0, canvas.Blue)
	return grad
}

// repeatedGradientCanvas returns a canvas with several distinct but content-identical gradient objects: three shapes at the same position (same transformation) and one at another position.
func repeatedGradientCanvas() *canvas.Canvas {
	c := canvas.New(40.0, 20.0)
	ctx := canvas.NewContext(c)
	for i := 0; i < 3; i++ {
		ctx.SetFillGradient(newTestGradient())
		ctx.DrawPath(5.0, 5.0, canvas.Rectangle(10.0, 10.0))
	}
	ctx.SetFillGradient(newTestGradient())
	ctx.DrawPath(20.0, 5.0, canvas.Rectangle(10.0, 10.0))
	return c
}

// transparentTestImage returns a semi-transparent image. If red is set the image is reddish, otherwise greenish; the alpha channel is identical either way.
func transparentTestImage(red bool) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			col := color.NRGBA{30, 200, 40, uint8(64 + x*32 + y*16)}
			if red {
				col = color.NRGBA{200, 30, 40, uint8(64 + x*32 + y*16)}
			}
			img.SetNRGBA(x, y, col)
		}
	}
	return img
}

func TestSVGDedupGradients(t *testing.T) {
	c := repeatedGradientCanvas()
	s := renderSVG(t, c, nil)
	doc := parseSVGDoc(t, s)

	// three identical gradients at the same transformation merge into one def,
	// the one at another transformation gets a second def
	if doc.counts["linearGradient"] != 2 {
		t.Fatalf("expected 2 gradient definitions, got %d\n%s", doc.counts["linearGradient"], s)
	}
	if doc.counts["defs"] != 1 {
		t.Fatalf("expected a single defs element, got %d", doc.counts["defs"])
	}
	if doc.counts["path"] != 4 {
		t.Fatalf("expected 4 paths, got %d", doc.counts["path"])
	}
	if n := strings.Count(s, `fill="url(#d0)"`); n != 3 {
		t.Fatalf("expected 3 references to #d0, got %d", n)
	}
	if n := strings.Count(s, `fill="url(#d1)"`); n != 1 {
		t.Fatalf("expected 1 reference to #d1, got %d", n)
	}
	doc.checkRefs(t)

	// defs are emitted before the elements that reference them
	if strings.Index(s, "<path") < strings.Index(s, "<defs>") {
		t.Fatalf("defs element must precede the document body")
	}

	// output is deterministic: rendering the same canvas again gives identical bytes
	if s2 := renderSVG(t, repeatedGradientCanvas(), nil); s2 != s {
		t.Fatalf("output is not deterministic:\n%s\n%s", s, s2)
	}

	// rendering the canvas multiple times into the same document reuses the registry
	opts := DefaultOptions
	buf := &bytes.Buffer{}
	svg := New(buf, c.W, c.H, &opts)
	c.RenderTo(svg)
	c.RenderTo(svg)
	if err := svg.Close(); err != nil {
		t.Fatal(err)
	}
	doc2 := parseSVGDoc(t, buf.String())
	if doc2.counts["linearGradient"] != 2 {
		t.Fatalf("expected 2 gradient definitions after rendering twice, got %d", doc2.counts["linearGradient"])
	}
	if doc2.counts["path"] != 8 {
		t.Fatalf("expected 8 paths after rendering twice, got %d", doc2.counts["path"])
	}
	doc2.checkRefs(t)

	// without dedup, every distinct gradient object gets its own def and the file grows
	sOff := renderSVG(t, repeatedGradientCanvas(), func(o *Options) { o.Dedup = false })
	docOff := parseSVGDoc(t, sOff)
	if docOff.counts["linearGradient"] != 4 {
		t.Fatalf("expected 4 gradient definitions without dedup, got %d", docOff.counts["linearGradient"])
	}
	t.Logf("file size: dedup %d bytes, no dedup %d bytes", len(s), len(sOff))
	if len(sOff) <= len(s) {
		t.Fatalf("deduplicated output (%d bytes) should be smaller than legacy output (%d bytes)", len(s), len(sOff))
	}
}

func TestSVGDedupRadialGradient(t *testing.T) {
	newRadial := func() *canvas.RadialGradient {
		grad := canvas.NewRadialGradient(canvas.Point{X: 5.0, Y: 5.0}, 1.0, canvas.Point{X: 5.0, Y: 5.0}, 8.0)
		grad.Add(0.0, canvas.Red)
		grad.Add(1.0, canvas.Blue)
		return grad
	}
	c := canvas.New(40.0, 20.0)
	ctx := canvas.NewContext(c)
	for i := 0; i < 3; i++ {
		ctx.SetFillGradient(newRadial())
		ctx.DrawPath(5.0, 5.0, canvas.Rectangle(10.0, 10.0))
	}
	s := renderSVG(t, c, nil)
	doc := parseSVGDoc(t, s)
	if doc.counts["radialGradient"] != 1 {
		t.Fatalf("expected 1 radial gradient definition, got %d\n%s", doc.counts["radialGradient"], s)
	}
	if n := strings.Count(s, `fill="url(#d0)"`); n != 3 {
		t.Fatalf("expected 3 references to #d0, got %d", n)
	}
	doc.checkRefs(t)
}

func TestSVGDedupAcrossStateAndNestedRenders(t *testing.T) {
	c := canvas.New(40.0, 20.0)
	ctx := canvas.NewContext(c)

	// Push/Pop (Save/Restore) must not affect the registry
	ctx.Push()
	ctx.SetView(canvas.Identity.Translate(2.0, 0.0))
	ctx.SetFillGradient(newTestGradient())
	ctx.DrawPath(5.0, 5.0, canvas.Rectangle(10.0, 10.0))
	ctx.Pop()
	ctx.SetFillGradient(newTestGradient())
	ctx.DrawPath(5.0, 5.0, canvas.Rectangle(10.0, 10.0))

	// a nested canvas rendered at the same absolute transformation shares the def
	sub := canvas.New(40.0, 20.0)
	subCtx := canvas.NewContext(sub)
	subCtx.SetFillGradient(newTestGradient())
	subCtx.DrawPath(7.0, 5.0, canvas.Rectangle(10.0, 10.0))

	opts := DefaultOptions
	buf := &bytes.Buffer{}
	svg := New(buf, c.W, c.H, &opts)
	c.RenderTo(svg)
	sub.RenderViewTo(svg, canvas.Identity.Translate(-2.0, 0.0))
	if err := svg.Close(); err != nil {
		t.Fatal(err)
	}

	s := buf.String()
	doc := parseSVGDoc(t, s)
	// the pushed-view draw and the nested render share (content, transform) -> d0,
	// the untransformed draw gets its own def -> d1
	if doc.counts["linearGradient"] != 2 {
		t.Fatalf("expected 2 gradient definitions, got %d\n%s", doc.counts["linearGradient"], s)
	}
	if doc.counts["path"] != 3 {
		t.Fatalf("expected 3 paths, got %d", doc.counts["path"])
	}
	doc.checkRefs(t)
}

func TestSVGDedupImages(t *testing.T) {
	newImageCanvas := func() *canvas.Canvas {
		c := canvas.New(40.0, 20.0)
		ctx := canvas.NewContext(c)
		for i := 0; i < 3; i++ {
			ctx.DrawImage(float64(i)*10.0, 5.0, transparentTestImage(true), canvas.DPMM(1.0))
		}
		return c
	}

	// lossless: the transparent PNG is embedded once and referenced by use elements
	s := renderSVG(t, newImageCanvas(), nil)
	doc := parseSVGDoc(t, s)
	if doc.counts["image"] != 1 {
		t.Fatalf("expected 1 image definition, got %d\n%s", doc.counts["image"], s)
	}
	if doc.counts["use"] != 3 {
		t.Fatalf("expected 3 use elements, got %d", doc.counts["use"])
	}
	if doc.counts["mask"] != 0 {
		t.Fatalf("expected no mask for lossless PNG, got %d", doc.counts["mask"])
	}
	if n := strings.Count(s, `<use xlink:href="#i0"`); n != 3 {
		t.Fatalf("expected 3 use references to #i0, got %d", n)
	}
	doc.checkRefs(t)

	// the image transformation stays on the use element, not on the shared def
	if !strings.Contains(s, `<use xlink:href="#i0" transform="translate(10,11)"/>`) {
		t.Fatalf("expected translated use element in:\n%s", s)
	}

	// lossy: the alpha channel becomes a shared mask, also embedded once
	sLossy := renderSVG(t, newImageCanvas(), func(o *Options) { o.ImageEncoding = cimage.Lossy })
	docLossy := parseSVGDoc(t, sLossy)
	if docLossy.counts["mask"] != 1 {
		t.Fatalf("expected 1 mask definition, got %d\n%s", docLossy.counts["mask"], sLossy)
	}
	if docLossy.counts["image"] != 2 { // the masked image and the image inside the mask
		t.Fatalf("expected 2 image elements, got %d", docLossy.counts["image"])
	}
	if n := strings.Count(sLossy, `mask="url(#m0)"`); n != 1 {
		t.Fatalf("expected image def to reference mask #m0 once, got %d", n)
	}
	docLossy.checkRefs(t)

	// two images with identical alpha channels but different colors share one mask
	c := canvas.New(40.0, 20.0)
	ctx := canvas.NewContext(c)
	ctx.DrawImage(0.0, 5.0, transparentTestImage(true), canvas.DPMM(1.0))
	ctx.DrawImage(10.0, 5.0, transparentTestImage(false), canvas.DPMM(1.0))
	sShared := renderSVG(t, c, func(o *Options) { o.ImageEncoding = cimage.Lossy })
	docShared := parseSVGDoc(t, sShared)
	if docShared.counts["mask"] != 1 {
		t.Fatalf("expected 1 shared mask definition, got %d\n%s", docShared.counts["mask"], sShared)
	}
	if docShared.counts["use"] != 2 {
		t.Fatalf("expected 2 use elements, got %d", docShared.counts["use"])
	}
	docShared.checkRefs(t)

	// deduplication shrinks the file
	sOff := renderSVG(t, newImageCanvas(), func(o *Options) { o.Dedup = false })
	t.Logf("file size: dedup %d bytes, no dedup %d bytes", len(s), len(sOff))
	if len(sOff) <= len(s) {
		t.Fatalf("deduplicated output (%d bytes) should be smaller than legacy output (%d bytes)", len(s), len(sOff))
	}
}

func TestSVGTwoPages(t *testing.T) {
	// two pages exported as two SVG documents: IDs are stable and restart per
	// page, no state leaks from the previous page
	newPage := func() *canvas.Canvas {
		c := repeatedGradientCanvas()
		ctx := canvas.NewContext(c)
		ctx.DrawImage(0.0, 0.0, transparentTestImage(true), canvas.DPMM(1.0))
		return c
	}

	page1 := renderSVG(t, newPage(), nil)
	page2 := renderSVG(t, newPage(), nil)
	if page1 != page2 {
		t.Fatalf("identical pages rendered differently:\n%s\n%s", page1, page2)
	}
	for _, id := range []string{`id="d0"`, `id="d1"`, `id="i0"`} {
		if n := strings.Count(page1, id); n != 1 {
			t.Fatalf("expected exactly one %s per page, got %d", id, n)
		}
	}
	doc := parseSVGDoc(t, page1)
	doc.checkRefs(t)
	if doc.counts["linearGradient"] != 2 || doc.counts["image"] != 1 {
		t.Fatalf("unexpected page structure: %v", doc.counts)
	}

	// a page with different content starts numbering from scratch as well
	other := renderSVG(t, canvas.New(10.0, 10.0), nil)
	if strings.Contains(other, `id="`) {
		t.Fatalf("empty page should have no resources:\n%s", other)
	}
}

func TestSVGDedupDisabledCompat(t *testing.T) {
	// with dedup disabled the output is byte-identical to the legacy renderer
	c := canvas.New(100.0, 50.0)
	ctx := canvas.NewContext(c)
	grad := canvas.NewLinearGradient(canvas.Point{X: 0.0, Y: 0.0}, canvas.Point{X: 40.0, Y: 0.0})
	grad.Add(0.0, canvas.Red)
	grad.Add(1.0, canvas.Blue)
	ctx.SetFillGradient(grad)
	ctx.DrawPath(5.0, 5.0, canvas.Rectangle(40.0, 20.0))

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	img.SetRGBA(1, 0, color.RGBA{0, 255, 0, 255})
	img.SetRGBA(0, 1, color.RGBA{0, 0, 255, 255})
	img.SetRGBA(1, 1, color.RGBA{255, 255, 0, 255})
	ctx.DrawImage(50.0, 5.0, img, canvas.DPMM(1.0))

	got := renderSVG(t, c, func(o *Options) { o.Dedup = false })
	want := `<svg version="1.1" width="100mm" height="50mm" viewBox="0 0 100 50" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><path d="M5 45H45V25H5z" fill="url(#d0)"/><image transform="translate(50,43)" width="2" height="2" xlink:href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAG0lEQVR4nAAOAPH/Av8AAAD/AAIBAP//AAADAB0hBAKiwpICAAAAAElFTkSuQmCC"/><defs><linearGradient id="d0" gradientUnits="userSpaceOnUse" x1="5" y1="45" x2="45" y2="45"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient></defs></svg>`
	if got != want {
		t.Fatalf("legacy output changed:\ngot  %s\nwant %s", got, want)
	}
}

func TestSVGCloseTwice(t *testing.T) {
	buf := &bytes.Buffer{}
	svg := New(buf, 10.0, 10.0, nil)
	repeatedGradientCanvas().RenderTo(svg)
	if err := svg.Close(); err != nil {
		t.Fatal(err)
	}
	n := buf.Len()
	if err := svg.Close(); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed on second Close, got %v", err)
	}
	if buf.Len() != n {
		t.Fatalf("second Close wrote to the document")
	}

	// rendering after Close is a no-op
	svg.RenderPath(canvas.Rectangle(5.0, 5.0), canvas.DefaultStyle, canvas.Identity)
	svg.RenderImage(transparentTestImage(true), canvas.Identity)
	if buf.Len() != n {
		t.Fatalf("render after Close wrote to the document")
	}
}

// shortWriter returns short writes without an error.
type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

// failWriter always fails.
type failWriter struct{ err error }

func (w failWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

func TestSVGShortWrite(t *testing.T) {
	for _, dedup := range []bool{true, false} {
		opts := DefaultOptions
		opts.Dedup = dedup
		svg := New(shortWriter{}, 10.0, 10.0, &opts)
		repeatedGradientCanvas().RenderTo(svg)
		if err := svg.Close(); !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("dedup=%v: expected io.ErrShortWrite, got %v", dedup, err)
		}
	}
}

func TestSVGWriteError(t *testing.T) {
	errFail := errors.New("disk full")
	for _, dedup := range []bool{true, false} {
		opts := DefaultOptions
		opts.Dedup = dedup
		svg := New(failWriter{errFail}, 10.0, 10.0, &opts)
		repeatedGradientCanvas().RenderTo(svg)
		if err := svg.Close(); !errors.Is(err, errFail) {
			t.Fatalf("dedup=%v: expected %v, got %v", dedup, errFail, err)
		}
	}
}

func TestSVGMaxResourceSize(t *testing.T) {
	opts := DefaultOptions
	opts.MaxResourceSize = 8 // bytes: any gradient def exceeds this
	buf := &bytes.Buffer{}
	svg := New(buf, 40.0, 20.0, &opts)
	repeatedGradientCanvas().RenderTo(svg)
	if err := svg.Close(); !errors.Is(err, ErrResourceTooLarge) {
		t.Fatalf("expected ErrResourceTooLarge, got %v", err)
	}
}

func TestSVGDedupRoundTripPixels(t *testing.T) {
	// a canvas with repeated gradients at different positions
	newCanvas := func(shared bool) *canvas.Canvas {
		c := canvas.New(60.0, 20.0)
		ctx := canvas.NewContext(c)
		grads := make([]canvas.Gradient, 3)
		for i := range grads {
			grads[i] = newTestGradient()
		}
		if shared {
			grads[1], grads[2] = grads[0], grads[0]
		}
		for i, grad := range grads {
			ctx.SetFillGradient(grad)
			ctx.DrawPath(float64(i)*20.0, 0.0, canvas.Rectangle(15.0, 20.0))
		}
		return c
	}

	// content-identical gradients render identically, whether or not they are
	// the same object: this is the premise of content-based deduplication
	imgDistinct := rasterizer.Draw(newCanvas(false), canvas.DPMM(2.0), canvas.DefaultColorSpace)
	imgShared := rasterizer.Draw(newCanvas(true), canvas.DPMM(2.0), canvas.DefaultColorSpace)
	if !bytes.Equal(imgDistinct.Pix, imgShared.Pix) {
		t.Fatalf("content-identical gradients render differently")
	}

	// the deduplicated SVG parses back to a pixel-identical rendering
	c := newCanvas(false)
	s := renderSVG(t, c, nil)
	parsed, err := canvas.ParseSVG(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	img1 := rasterizer.Draw(c, canvas.DPMM(2.0), canvas.DefaultColorSpace)
	img2 := rasterizer.Draw(parsed, canvas.DPMM(2.0*25.4/96.0), canvas.DefaultColorSpace) // parsed SVG is in px units
	if img1.Bounds() != img2.Bounds() {
		t.Fatalf("rasterized bounds differ: %v vs %v", img1.Bounds(), img2.Bounds())
	}
	if !bytes.Equal(img1.Pix, img2.Pix) {
		t.Fatalf("round-tripped SVG renders differently")
	}
}
