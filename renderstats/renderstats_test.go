package renderstats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
)

// Compile-time proof that the Recorder is a drop-in canvas.Renderer.
var _ canvas.Renderer = (*Recorder)(nil)

// capture is a minimal renderer that records the forwarded call sequence. It
// does no drawing, so wrapping it changes nothing observable.
type capture struct {
	mu    sync.Mutex
	calls []string
}

func (c *capture) Size() (float64, float64) { return 0.0, 0.0 }

func (c *capture) push(s string) {
	c.mu.Lock()
	c.calls = append(c.calls, s)
	c.mu.Unlock()
}

func (c *capture) RenderPath(path *canvas.Path, style canvas.Style, m canvas.Matrix) {
	c.push("path")
}

func (c *capture) RenderText(text *canvas.Text, m canvas.Matrix) {
	c.push("text")
}

func (c *capture) RenderImage(img image.Image, m canvas.Matrix) {
	c.push("image")
}

func (c *capture) sequence() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

func gateFont(t *testing.T) *canvas.FontFamily {
	t.Helper()
	family := canvas.NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile(filepath.Join("..", "resources", "DejaVuSerif.ttf"), canvas.FontRegular); err != nil {
		t.Fatalf("load font: %v", err)
	}
	return family
}

func gateImage() image.Image {
	img := image.NewNRGBA(image.Rect(3, 4, 7, 8)) // deliberately non-zero Bounds.Min
	for y := 4; y < 8; y++ {
		for x := 3; x < 7; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(31 * x),
				G: uint8(29 * y),
				B: uint8(17 * (x + y)),
				A: 255,
			})
		}
	}
	return img
}

// gateScene builds the fixed path/text/image scene used by the threshold
// baseline. It exercises z-index ordering, strokes, text, and a non-zero image
// origin.
func gateScene(t *testing.T) *canvas.Canvas {
	t.Helper()
	face := gateFont(t).Face(10.0, canvas.Black, canvas.FontRegular, canvas.FontNormal)
	text := canvas.NewTextLine(face, "Render gate", canvas.Left)

	path := canvas.MustParseSVGPath("M10 0L20 0Q25 10 30 0C30 10 40 10 40 0A5 5 0 0 0 50 0z")
	box := canvas.Rectangle(20, 12).Translate(0, 0)

	c := canvas.New(120.0, 80.0)
	ctx := canvas.NewContext(c)
	ctx.SetStrokeColor(canvas.Gray)
	ctx.SetStrokeWidth(1.0)
	ctx.SetFillColor(canvas.Red)
	ctx.DrawPath(10.0, 10.0, path)
	ctx.SetFillColor(canvas.Blue)
	ctx.DrawPath(45.0, 15.0, box)
	ctx.SetFillColor(canvas.Black)
	ctx.DrawText(8.0, 65.0, text)
	ctx.DrawImage(65.0, 20.0, gateImage(), 0.5)

	// A later z-index layer must render after the base layer.
	ctx.SetZIndex(1)
	ctx.SetFillColor(canvas.Green)
	ctx.DrawPath(90.0, 10.0, canvas.Rectangle(10, 10))
	ctx.SetZIndex(0)
	return c
}

func TestRecorderForwardsIdentically(t *testing.T) {
	c := gateScene(t)

	direct := &capture{}
	c.RenderTo(direct)

	wrapped := &capture{}
	rec := NewRecorder(wrapped)
	c.RenderTo(rec)
	rec.EndFrame()

	want := direct.sequence()
	got := wrapped.sequence()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("forwarded call order changed:\n got %v\nwant %v", got, want)
	}
	report := rec.Report()
	if len(report.Frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(report.Frames))
	}
	if report.Frames[0].Counts.Total != len(want) {
		t.Fatalf("recorded %d ops, forwarded %d", report.Frames[0].Counts.Total, len(want))
	}
}

func TestRecorderDoesNotChangePixels(t *testing.T) {
	c := gateScene(t)
	res := canvas.DPMM(2.0)

	directImg := image.NewRGBA(image.Rect(0, 0, 240, 160))
	direct := rasterizer.FromImage(directImg, res, canvas.DefaultColorSpace)
	c.RenderTo(direct)
	direct.Close()

	wrappedImg := image.NewRGBA(image.Rect(0, 0, 240, 160))
	wrapped := rasterizer.FromImage(wrappedImg, res, canvas.DefaultColorSpace)
	rec := NewRecorder(wrapped, WithResolution(res))
	c.RenderTo(rec)
	rec.EndFrame()
	wrapped.Close()

	if !bytes.Equal(directImg.Pix, wrappedImg.Pix) {
		t.Fatal("wrapping the renderer changed rendered pixels")
	}
	if rec.Report().Frames[0].Counts.Total == 0 {
		t.Fatal("no operations recorded")
	}
}

func TestLifecycleEmptyResetAndSaveRestore(t *testing.T) {
	// Empty canvas: a frame exists, with zero operations serialized as [].
	rec := NewRecorder(&capture{})
	rec.RenderCanvas(canvas.New(10, 10), canvas.Identity, "empty")
	rep := rec.Report()
	if len(rep.Frames) != 1 || rep.Frames[0].Counts.Total != 0 || rep.Frames[0].Overflow {
		t.Fatalf("unexpected empty frame: %+v", rep.Frames)
	}
	if rep.Frames[0].Ops == nil {
		t.Fatal("empty frame ops must be non-nil so JSON encodes []")
	}
	b, err := rec.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ops": []`) {
		t.Fatalf("empty ops not encoded as []: %s", b)
	}

	// Context Push/Pop is canvas save/restore; it never reaches the Renderer.
	capt := &capture{}
	rec = NewRecorder(capt)
	ctx := canvas.NewContext(rec)
	ctx.SetFillColor(canvas.Red)
	ctx.DrawPath(0, 0, canvas.Rectangle(5, 5))
	ctx.Push()
	ctx.SetFillColor(canvas.Blue)
	ctx.DrawPath(0, 0, canvas.Rectangle(5, 5))
	ctx.Pop()
	ctx.SetFillColor(canvas.Green)
	ctx.DrawPath(0, 0, canvas.Rectangle(5, 5))
	rec.EndFrame()
	if got := capt.sequence(); len(got) != 3 {
		t.Fatalf("save/restore changed op count: %v", got)
	}
	if rec.Report().Frames[0].Counts.Path != 3 {
		t.Fatalf("save/restore changed recorded ops: %+v", rec.Report().Frames[0].Counts)
	}

	// Canvas.Reset and Transform never reach the Renderer and never clear stats.
	c := canvas.New(10, 10)
	ctx = canvas.NewContext(c)
	ctx.DrawPath(0, 0, canvas.Rectangle(5, 5))
	rec = NewRecorder(&capture{})
	rec.RenderCanvas(c, canvas.Identity, "before-reset")
	c.Reset()
	c.Transform(canvas.Identity.Translate(5, 5))
	c.Clip(canvas.Rect{X0: 0, Y0: 0, X1: 10, Y1: 10})
	rec.RenderCanvas(c, canvas.Identity, "after-reset")
	rep = rec.Report()
	if len(rep.Frames) != 2 || rep.Frames[0].Counts.Path != 1 || rep.Frames[1].Counts.Path != 0 {
		t.Fatalf("unexpected frames around Canvas.Reset: %+v", rep.Frames)
	}
}

func TestNonZeroImageBounds(t *testing.T) {
	rec := NewRecorder(&capture{}, WithImageHashSamples(4))
	rec.RenderImage(gateImage(), canvas.Identity)
	rec.EndFrame()
	op := rec.Report().Frames[0].Ops[0]
	want := canvas.Rect{X0: 3, Y0: 4, X1: 7, Y1: 8}
	if op.Local.Rect() != want {
		t.Fatalf("local bbox %+v, want %+v", op.Local.Rect(), want)
	}
}

func TestRobustness(t *testing.T) {
	t.Run("non-finite timing", func(t *testing.T) {
		t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		var n int
		clock := func() time.Time {
			n++
			if n%2 == 0 {
				return t0.Add(-time.Second) // clock steps backwards
			}
			return t0
		}
		rec := NewRecorder(&capture{}, WithClock(clock))
		rec.RenderPath(canvas.Rectangle(1, 1), canvas.DefaultStyle, canvas.Identity)
		rec.EndFrame()
		rep := rec.Report()
		op := rep.Frames[0].Ops[0]
		if op.RenderValid || op.RenderNanos != 0 {
			t.Fatalf("backwards clock produced timing %d valid=%v", op.RenderNanos, op.RenderValid)
		}
		if rep.Frames[0].DurationValid || rep.Frames[0].DurationNanos != 0 {
			t.Fatalf("backwards clock produced frame duration %d valid=%v", rep.Frames[0].DurationNanos, rep.Frames[0].DurationValid)
		}
		b, err := rec.ExportJSON()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("NaN")) || bytes.Contains(b, []byte("Inf")) {
			t.Fatalf("non-finite JSON: %s", b)
		}
	})

	t.Run("repeated exports", func(t *testing.T) {
		rec := NewRecorder(&capture{})
		rec.RenderPath(canvas.Rectangle(1, 1), canvas.DefaultStyle, canvas.Identity)
		rec.EndFrame()
		a, err := rec.ExportJSON()
		if err != nil {
			t.Fatal(err)
		}
		b, err := rec.ExportJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatal("repeated closed-frame exports differ")
		}
		frame, err := rec.ExportFrameJSON(0)
		if err != nil {
			t.Fatal(err)
		}
		var rep Report
		if err := json.Unmarshal(frame, &rep); err != nil {
			t.Fatal(err)
		}
		if len(rep.Frames) != 1 || rep.Frames[0].Index != 0 {
			t.Fatalf("unexpected frame export: %+v", rep.Frames)
		}
		if _, err := rec.ExportFrameJSON(9); err == nil {
			t.Fatal("expected error for missing frame")
		}
	})

	t.Run("huge scene bounded", func(t *testing.T) {
		rec := NewRecorder(&capture{}, WithMaxOps(10), WithMaxResources(5))
		for i := 0; i < 500; i++ {
			rec.RenderPath(canvas.Rectangle(1, 1).Translate(float64(i), 0), canvas.DefaultStyle, canvas.Identity)
		}
		rec.EndFrame()
		rep := rec.Report()
		f := rep.Frames[0]
		if !f.Overflow || f.DroppedOps != 490 || f.Counts.Total != 500 || len(f.Ops) != 10 {
			t.Fatalf("bounds not enforced: overflow=%v dropped=%d total=%d kept=%d",
				f.Overflow, f.DroppedOps, f.Counts.Total, len(f.Ops))
		}
		if !rep.Resources.Overflow || len(rep.Resources.Items) > 5 {
			t.Fatalf("resource bound not enforced: %+v", rep.Resources)
		}
		if f.TempBytes < 0 || f.TempBytes > int64(500)*int64(maxTempBytes) {
			t.Fatalf("temp bytes out of range: %d", f.TempBytes)
		}
		if _, err := rec.ExportJSON(); err != nil {
			t.Fatalf("huge scene JSON: %v", err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		rec := NewRecorder(&capture{})
		rec.Cancel() // no open frame: must be a no-op
		rec.BeginFrame("work")
		rec.RenderPath(canvas.Rectangle(1, 1), canvas.DefaultStyle, canvas.Identity)
		rec.Cancel()
		rec.RenderPath(canvas.Rectangle(2, 2), canvas.DefaultStyle, canvas.Identity)
		rec.EndFrame()
		rep := rec.Report()
		if len(rep.Frames) != 2 {
			t.Fatalf("got %d frames, want 2", len(rep.Frames))
		}
		if !rep.Frames[0].Canceled || rep.Frames[0].Counts.Total != 1 {
			t.Fatalf("first frame not canceled correctly: %+v", rep.Frames[0])
		}
		if rep.Frames[1].Canceled || rep.Frames[1].Counts.Total != 1 {
			t.Fatalf("post-cancel draw not recorded in fresh frame: %+v", rep.Frames[1])
		}
	})

	t.Run("bad renderer", func(t *testing.T) {
		bad := &panicky{}
		rec := NewRecorder(bad)
		rec.RenderPath(canvas.Rectangle(1, 1), canvas.DefaultStyle, canvas.Identity)
		rec.RenderText(nil, canvas.Identity)
		rec.RenderImage(nil, canvas.Identity)
		rec.EndFrame()
		rep := rec.Report()
		if rep.Frames[0].Counts.Total != 3 {
			t.Fatalf("panicking renderer stopped recording: %+v", rep.Frames[0].Counts)
		}
		for i, op := range rep.Frames[0].Ops {
			if op.RenderError == "" {
				t.Fatalf("op %d: panic not recorded", i)
			}
		}
		if _, err := rec.ExportJSON(); err != nil {
			t.Fatalf("JSON after panics: %v", err)
		}

		// Propagating policy re-panics with the original value.
		rec = NewRecorder(bad, WithPanicPolicy(PanicPropagate))
		func() {
			defer func() {
				if v := recover(); v == nil {
					t.Fatal("expected propagated panic")
				}
			}()
			rec.RenderPath(canvas.Rectangle(1, 1), canvas.DefaultStyle, canvas.Identity)
		}()

		// A nil renderer must record without panicking.
		rec = NewRecorder(nil)
		rec.RenderPath(canvas.Rectangle(1, 1), canvas.DefaultStyle, canvas.Identity)
		rec.RenderText(nil, canvas.Identity)
		rec.RenderImage(nil, canvas.Identity)
		rec.EndFrame()
		if rec.Report().Frames[0].Counts.Total != 3 {
			t.Fatal("nil renderer did not record")
		}
	})

	t.Run("hostile inputs", func(t *testing.T) {
		// Nil path, nil text, nil image, and a matrix with NaN are all safe.
		rec := NewRecorder(&capture{})
		nan := canvas.Identity
		nan[0][0] = func() float64 { var z float64; return z / z }()
		rec.RenderPath(nil, canvas.DefaultStyle, canvas.Identity)
		rec.RenderText(nil, nan)
		rec.RenderImage(nil, nan)
		rec.EndFrame()
		rep := rec.Report()
		if rep.Frames[0].Counts.Total != 3 {
			t.Fatalf("hostile inputs lost counts: %+v", rep.Frames[0].Counts)
		}
		for i, op := range rep.Frames[0].Ops {
			if op.World.Valid {
				t.Fatalf("op %d: NaN matrix produced a valid world bbox", i)
			}
			if op.ClipHit != ClipNone {
				t.Fatalf("op %d: unexpected clip hit %q", i, op.ClipHit)
			}
		}
		if _, err := rec.ExportJSON(); err != nil {
			t.Fatalf("hostile inputs JSON: %v", err)
		}
	})
}

func TestClipClassification(t *testing.T) {
	rec := NewRecorder(&capture{})
	rec.SetClip(canvas.Rect{X0: 0, Y0: 0, X1: 10, Y1: 10})
	rec.RenderPath(canvas.Rectangle(4, 4).Translate(2, 2), canvas.DefaultStyle, canvas.Identity)   // inside
	rec.RenderPath(canvas.Rectangle(4, 4).Translate(8, 8), canvas.DefaultStyle, canvas.Identity)   // partial
	rec.RenderPath(canvas.Rectangle(4, 4).Translate(20, 20), canvas.DefaultStyle, canvas.Identity) // outside
	rec.EndFrame()
	rep := rec.Report()
	f := rep.Frames[0]
	if !f.ClipSet || f.ClipRect.Rect() != (canvas.Rect{X0: 0, Y0: 0, X1: 10, Y1: 10}) {
		t.Fatalf("clip metadata missing: %+v", f)
	}
	want := ClipCounts{Inside: 1, Partial: 1, Outside: 1}
	if f.Clip != want {
		t.Fatalf("clip counts %+v, want %+v", f.Clip, want)
	}
	got := []ClipHit{f.Ops[0].ClipHit, f.Ops[1].ClipHit, f.Ops[2].ClipHit}
	if fmt.Sprint(got) != fmt.Sprint([]ClipHit{ClipInside, ClipPartial, ClipOutside}) {
		t.Fatalf("clip classifications %v", got)
	}

	// A NaN clip window must disable classification rather than poison it.
	rec = NewRecorder(&capture{})
	rec.SetClip(canvas.Rect{X0: 0, Y0: 0, X1: nan(), Y1: 10})
	rec.RenderPath(canvas.Rectangle(4, 4), canvas.DefaultStyle, canvas.Identity)
	rec.EndFrame()
	if rec.Report().Frames[0].ClipSet {
		t.Fatal("non-finite clip window was accepted")
	}
}

func TestConcurrentReadOnly(t *testing.T) {
	rec := NewRecorder(&capture{})
	rec.BeginFrame("concurrent")

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = rec.Report()
				_, _ = rec.ExportJSON()
				_ = rec.Frames()
			}
		}()
	}
	for i := 0; i < 200; i++ {
		rec.RenderPath(canvas.Rectangle(1, 1).Translate(float64(i), 0), canvas.DefaultStyle, canvas.Identity)
	}
	close(stop)
	wg.Wait()
	rec.EndFrame()

	if got := rec.Report().Frames[0].Counts.Total; got != 200 {
		t.Fatalf("recorded %d ops, want 200", got)
	}
}

func TestReportVersionAndStableOrdering(t *testing.T) {
	rec := NewRecorder(&capture{})
	rec.RenderPath(canvas.Rectangle(2, 3), canvas.DefaultStyle, canvas.Identity)
	rec.RenderPath(canvas.Rectangle(1, 4), canvas.DefaultStyle, canvas.Identity)
	rec.RenderImage(gateImage(), canvas.Identity)
	rec.EndFrame()
	rep := rec.Report()
	if rep.Schema != Schema || rep.Version != Version {
		t.Fatalf("schema/version: %q/%d", rep.Schema, rep.Version)
	}
	for i := 1; i < len(rep.Resources.Items); i++ {
		if rep.Resources.Items[i-1].ID >= rep.Resources.Items[i].ID {
			t.Fatalf("resources not sorted: %q >= %q", rep.Resources.Items[i-1].ID, rep.Resources.Items[i].ID)
		}
	}
	b, err := rec.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"schema": "renderstats.report"`, `"version": 1`, `"frames":`, `"resources":`, `"local_bbox"`, `"world_bbox"`, `"render_nanos"`, `"temp_buffer_bytes"`, `"resource_id"`} {
		if !bytes.Contains(b, []byte(key)) {
			t.Fatalf("report JSON missing %s:\n%s", key, b)
		}
	}
}

// panicky is a renderer whose every call fails.
type panicky struct{}

func (p *panicky) Size() (float64, float64) { return 0.0, 0.0 }
func (p *panicky) RenderPath(*canvas.Path, canvas.Style, canvas.Matrix) {
	panic("path renderer failed")
}
func (p *panicky) RenderText(*canvas.Text, canvas.Matrix) { panic("text renderer failed") }
func (p *panicky) RenderImage(image.Image, canvas.Matrix) { panic("image renderer failed") }

func nan() float64 {
	var z float64
	return z / z
}

func TestPerFrameExportAndPeakTemp(t *testing.T) {
	rec := NewRecorder(&capture{})
	rec.BeginFrame("a")
	rec.RenderPath(canvas.Rectangle(1, 1), canvas.DefaultStyle, canvas.Identity)
	rec.RenderImage(gateImage(), canvas.Identity)
	rec.EndFrame()
	rec.BeginFrame("b")
	rec.RenderPath(canvas.Rectangle(9, 9), canvas.DefaultStyle, canvas.Identity)
	rec.EndFrame()

	rep := rec.Report()
	for i, f := range rep.Frames {
		if f.PeakTempBytes <= 0 || f.PeakTempBytes > f.TempBytes {
			t.Fatalf("frame %d: bad peak %d of total %d", i, f.PeakTempBytes, f.TempBytes)
		}
		if f.DurationValid == false && f.DurationNanos != 0 {
			t.Fatalf("frame %d: invalid duration must be zero", i)
		}
	}

	dir := t.TempDir()
	paths, err := rec.WriteFramesJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d per-frame files, want 2", len(paths))
	}
	for i, path := range paths {
		var one Report
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &one); err != nil {
			t.Fatalf("frame %d JSON: %v", i, err)
		}
		if one.Schema != Schema || one.Version != Version || len(one.Frames) != 1 || one.Frames[0].Index != i {
			t.Fatalf("frame %d file header wrong: %+v", i, one)
		}
	}
}

func TestResourceIDsStableAcrossRuns(t *testing.T) {
	ids := func() (string, string, string) {
		rec := NewRecorder(&capture{})
		rec.RenderPath(canvas.Rectangle(3, 4), canvas.DefaultStyle, canvas.Identity)
		face := gateFont(t).Face(10, canvas.Black, canvas.FontRegular, canvas.FontNormal)
		rec.RenderText(canvas.NewTextLine(face, "stable", canvas.Left), canvas.Identity)
		rec.RenderImage(gateImage(), canvas.Identity)
		rec.EndFrame()
		ops := rec.Report().Frames[0].Ops
		return ops[0].ResourceID, ops[1].ResourceID, ops[2].ResourceID
	}
	a1, a2, a3 := ids()
	b1, b2, b3 := ids()
	if a1 != b1 || a2 != b2 || a3 != b3 {
		t.Fatalf("resource IDs not stable: %q/%q/%q vs %q/%q/%q", a1, a2, a3, b1, b2, b3)
	}
	if a1 == a2 || a2 == a3 || a1 == a3 {
		t.Fatalf("distinct resources share an ID: %q %q %q", a1, a2, a3)
	}

	// A different path must yield a different ID.
	rec := NewRecorder(&capture{})
	rec.RenderPath(canvas.Rectangle(3, 5), canvas.DefaultStyle, canvas.Identity)
	rec.EndFrame()
	if got := rec.Report().Frames[0].Ops[0].ResourceID; got == a1 {
		t.Fatal("different path produced the same resource ID")
	}
}

func TestImageResourceIDSensitiveToPixels(t *testing.T) {
	a := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	b := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			a.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 31), A: 255})
			b.SetNRGBA(x, y, color.NRGBA{R: uint8(y * 31), A: 255})
		}
	}
	rec := NewRecorder(&capture{}, WithImageHashSamples(8))
	rec.RenderImage(a, canvas.Identity)
	rec.RenderImage(b, canvas.Identity)
	rec.EndFrame()
	ops := rec.Report().Frames[0].Ops
	if ops[0].ResourceID == ops[1].ResourceID {
		t.Fatal("pixel-distinct images share a resource ID")
	}

	// Disabling sampling falls back to type and bounds only.
	rec = NewRecorder(&capture{}, WithImageHashSamples(0))
	rec.RenderImage(a, canvas.Identity)
	rec.RenderImage(b, canvas.Identity)
	rec.EndFrame()
	ops = rec.Report().Frames[0].Ops
	if ops[0].ResourceID != ops[1].ResourceID {
		t.Fatal("sampling disabled but IDs still differ by pixels")
	}
}
