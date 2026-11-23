package renderstats

import (
	"encoding/json"
	"flag"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
)

// baselinePath is the checked-in threshold baseline for gateScene.
func baselinePath() string {
	return filepath.Join("testdata", "gate_baseline.json")
}

// emitReport writes the full report and one JSON file per frame into dir.
func emitReport(rec *Recorder, dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := rec.ExportJSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), b, 0o600); err != nil {
		return err
	}
	_, err = rec.WriteFramesJSON(dir)
	return err
}

// update regenerates the checked-in gate baseline when set.
var update = flag.Bool("renderstats.update", false, "regenerate renderstats testdata baselines")

// emitDir, when set, writes per-frame JSON statistics for CI triage.
var emitDir = flag.String("renderstats.emit", "", "write per-frame render statistics JSON to this directory")

// TestGateBaseline is the threshold gate. It renders the fixed scene, exports
// statistics, and compares them against a checked-in baseline with explicit
// bounds. Regenerate with: go test ./renderstats -run TestGateBaseline -renderstats.update
func TestGateBaseline(t *testing.T) {
	rec := NewRecorder(&capture{}, WithResolution(canvas.DPMM(1.0)))
	rec.RenderCanvas(gateScene(t), canvas.Identity, "gate")
	rep := rec.Report()

	if *emitDir != "" {
		if err := emitReport(rec, *emitDir); err != nil {
			t.Fatalf("emit statistics: %v", err)
		}
	}

	if *update {
		base := makeBaseline(rep)
		b, err := base.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(baselinePath(), b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote baseline %s", baselinePath())
		return
	}

	base, err := LoadBaselineFile(baselinePath())
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	if err := base.Check(rep); err != nil {
		t.Fatalf("gate failed:\n%v\n\nreport:\n%s", err, dumpReport(t, rep))
	}
}

// makeBaseline derives a baseline from a known-good report, measuring the
// observed op counts and bboxes, and setting generous but finite threshold
// bounds for timing and temporary memory. A small default tolerance absorbs
// floating-point noise; timing thresholds scale with observed maxima so the
// gate is not flaky but still catches order-of-magnitude regressions.
func makeBaseline(rep *Report) *Baseline {
	base := &Baseline{
		Schema:        Schema,
		Version:       Version,
		Tolerance:     1e-6,
		ResourceCount: len(rep.Resources.Items),
	}
	var maxOpNanos, maxTemp int64
	for _, f := range rep.Frames {
		fb := FrameBaseline{
			Name:     f.Name,
			ClipSet:  f.ClipSet,
			ClipRect: f.ClipRect,
		}
		for _, op := range f.Ops {
			fb.Ops = append(fb.Ops, OpBaseline{
				Kind:  op.Kind,
				Local: op.Local,
				World: op.World,
				Clip:  op.ClipHit,
			})
			if maxOpNanos < op.RenderNanos {
				maxOpNanos = op.RenderNanos
			}
			if maxTemp < op.TempBytes {
				maxTemp = op.TempBytes
			}
		}
		base.Frames = append(base.Frames, fb)
	}
	// Timing is machine-dependent: allow a wide absolute ceiling (250 ms per op
	// and frame) plus 20x headroom over the observed maximum.
	base.NanosMax = maxInt64(maxOpNanos*20, int64(250*1e6))
	base.FrameNanosMax = maxInt64(maxOpNanos*100, int64(5*1e9))
	base.TempBytesMax = maxInt64(maxTemp*4, 1<<20)
	return base
}

func maxInt64(a, b int64) int64 {
	if b < a {
		return a
	}
	return b
}

func dumpReport(t *testing.T, rep *Report) string {
	t.Helper()
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// faultyRecorder wraps a Recorder's package-internal record path so tests can
// inject a single defect and prove the gate rejects it. It is deliberately
// local to tests and cannot be constructed outside this package.
type faultMode int

const (
	faultNone    faultMode = iota
	faultDropOp            // a RenderPath call is not recorded
	faultBadBBox           // a RenderPath local bbox is shifted
)

// faultyRenderer renders a scene while injecting one defect into the
// statistics. It forwards correctly to the wrapped renderer, so only the
// statistics are wrong, exactly like a regression in instrumentation would be.
type faultyRenderer struct {
	mode  faultMode
	inner *Recorder
}

func (f *faultyRenderer) Size() (float64, float64) { return f.inner.Size() }

func (f *faultyRenderer) RenderPath(path *canvas.Path, style canvas.Style, m canvas.Matrix) {
	if f.mode == faultDropOp {
		// Forward the draw but skip recording it: a leaked operation.
		f.inner.renderer.RenderPath(path, style, m)
		return
	}
	f.inner.RenderPath(path, style, m)
	if f.mode == faultBadBBox {
		// Corrupt the most recent operation's bounding box after the fact.
		f.inner.mu.Lock()
		if f.inner.open != nil && 0 < len(f.inner.open.Ops) {
			op := &f.inner.open.Ops[len(f.inner.open.Ops)-1]
			op.Local.X1 += 3.0
			op.World.X1 += 3.0
		}
		f.inner.mu.Unlock()
	}
}

func (f *faultyRenderer) RenderText(text *canvas.Text, m canvas.Matrix) {
	f.inner.RenderText(text, m)
}

func (f *faultyRenderer) RenderImage(img image.Image, m canvas.Matrix) {
	f.inner.RenderImage(img, m)
}

func TestGateDetectsMissingOperation(t *testing.T) {
	base, err := LoadBaselineFile(baselinePath())
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}

	inner := NewRecorder(&capture{}, WithResolution(canvas.DPMM(1.0)))
	faulty := &faultyRenderer{mode: faultDropOp, inner: inner}
	inner.BeginFrame("gate")
	ctx := canvas.NewContext(faulty)
	buildScene(t, ctx)
	inner.EndFrame()

	err = base.Check(inner.Report())
	if err == nil {
		t.Fatal("gate accepted a report with a dropped operation")
	}
	assertMentions(t, err, "operations", "want", "resources")
}

func TestGateDetectsBoundingBoxError(t *testing.T) {
	base, err := LoadBaselineFile(baselinePath())
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}

	inner := NewRecorder(&capture{}, WithResolution(canvas.DPMM(1.0)))
	faulty := &faultyRenderer{mode: faultBadBBox, inner: inner}
	inner.BeginFrame("gate")
	ctx := canvas.NewContext(faulty)
	buildScene(t, ctx)
	inner.EndFrame()

	err = base.Check(inner.Report())
	if err == nil {
		t.Fatal("gate accepted a report with a wrong bounding box")
	}
	assertMentions(t, err, "local_bbox", "world_bbox", "tolerance")
}

func assertMentions(t *testing.T, err error, needles ...string) {
	t.Helper()
	msg := err.Error()
	for _, n := range needles {
		if !strings.Contains(msg, n) {
			t.Fatalf("gate error does not mention %q:\n%s", n, msg)
		}
	}
}

// buildScene is the same fixed path/text/image scene as gateScene but drawn
// through an arbitrary renderer, so fault injection can wrap it.
func buildScene(t *testing.T, ctx *canvas.Context) {
	t.Helper()
	face := gateFont(t).Face(10.0, canvas.Black, canvas.FontRegular, canvas.FontNormal)
	text := canvas.NewTextLine(face, "Render gate", canvas.Left)
	path := canvas.MustParseSVGPath("M10 0L20 0Q25 10 30 0C30 10 40 10 40 0A5 5 0 0 0 50 0z")
	box := canvas.Rectangle(20, 12)

	ctx.SetStrokeColor(canvas.Gray)
	ctx.SetStrokeWidth(1.0)
	ctx.SetFillColor(canvas.Red)
	ctx.DrawPath(10.0, 10.0, path)
	ctx.SetFillColor(canvas.Blue)
	ctx.DrawPath(45.0, 15.0, box)
	ctx.SetFillColor(canvas.Black)
	ctx.DrawText(8.0, 65.0, text)
	ctx.DrawImage(65.0, 20.0, gateImage(), 0.5)
	ctx.SetZIndex(1)
	ctx.SetFillColor(canvas.Green)
	ctx.DrawPath(90.0, 10.0, canvas.Rectangle(10, 10))
	ctx.SetZIndex(0)
}

// TestGateRejectsTamperedReport proves the baseline itself is sensitive to
// every field it claims to gate, without relying on the renderer path.
func TestGateRejectsTamperedReport(t *testing.T) {
	base, err := LoadBaselineFile(baselinePath())
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	rec := NewRecorder(&capture{}, WithResolution(canvas.DPMM(1.0)))
	rec.RenderCanvas(gateScene(t), canvas.Identity, "gate")
	if err := base.Check(rec.Report()); err != nil {
		t.Fatalf("pristine report rejected: %v", err)
	}

	cases := []struct {
		name   string
		tamper func(*Report)
		want   string
	}{
		{"kind", func(r *Report) { r.Frames[0].Ops[0].Kind = KindImage }, "kind"},
		{"order", func(r *Report) {
			ops := r.Frames[0].Ops
			ops[0], ops[1] = ops[1], ops[0]
		}, "seq"},
		{"local bbox", func(r *Report) { r.Frames[0].Ops[0].Local.X0 -= 1 }, "local_bbox"},
		{"world bbox", func(r *Report) { r.Frames[0].Ops[0].World.Y1 += 1 }, "world_bbox"},
		{"clip hit", func(r *Report) { r.Frames[0].Ops[0].ClipHit = ClipOutside }, "clip_hit"},
		{"count", func(r *Report) { r.Frames[0].Counts.Total++ }, "counts"},
		{"open", func(r *Report) { r.Frames[0].Open = true }, "open"},
		{"render error", func(r *Report) { r.Frames[0].Ops[0].RenderError = "boom" }, "renderer error"},
		{"resource id", func(r *Report) { r.Frames[0].Ops[0].ResourceID = "" }, "resource ID"},
		{"version", func(r *Report) { r.Version++ }, "version"},
		{"timing bound", func(r *Report) {
			r.Frames[0].Ops[0].RenderNanos = 1 << 60
			r.Frames[0].Ops[0].RenderValid = true
		}, "render time"},
		{"temp bound", func(r *Report) { r.Frames[0].Ops[0].TempBytes = 1 << 50 }, "temp buffer"},
		{"overflow", func(r *Report) {
			r.Frames[0].Overflow = true
			r.Frames[0].DroppedOps = 2
		}, "overflowed"},
		{"resource overflow", func(r *Report) { r.Resources.Overflow = true }, "overflowed"},
		{"frame count", func(r *Report) { r.Frames = append(r.Frames, r.Frames[0]) }, "frames: got"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := rec.Report()
			tc.tamper(rep)
			err := base.Check(rep)
			if err == nil {
				t.Fatalf("gate accepted tampered %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error for %s does not mention %q:\n%v", tc.name, tc.want, err)
			}
		})
	}
}

func TestBaselineValidation(t *testing.T) {
	bad := []string{
		`{"schema":"wrong","version":1,"tolerance":0,"nanos_max":0,"temp_bytes_max":0,"resource_count":0,"frames":[]}`,
		`{"schema":"renderstats.report","version":99,"tolerance":0,"nanos_max":0,"temp_bytes_max":0,"resource_count":0,"frames":[]}`,
		`{"schema":"renderstats.report","version":1,"tolerance":-1,"nanos_max":0,"temp_bytes_max":0,"resource_count":0,"frames":[]}`,
		`{"schema":"renderstats.report","version":1,"tolerance":0,"nanos_max":0,"temp_bytes_max":0,"resource_count":0,"frames":[{"name":"x","clip_set":false,"clip_rect":{},"ops":[]}]}`,
	}
	for i, raw := range bad {
		if _, err := LoadBaseline(strings.NewReader(raw)); err == nil {
			t.Fatalf("case %d: invalid baseline accepted", i)
		}
	}
	if _, err := LoadBaseline(strings.NewReader(`{"schema":"renderstats.report"}`)); err == nil {
		t.Fatal("partial baseline accepted (unknown-field decoding should also reject missing fields)")
	}
}

func TestGateNilReport(t *testing.T) {
	base := &Baseline{Schema: Schema, Version: Version}
	if err := base.Check(nil); err == nil {
		t.Fatal("nil report accepted")
	}
}

// TestGateRejectsFrameDuration proves the frame-level timing bound is enforced
// separately from the per-operation bound.
func TestGateRejectsFrameDuration(t *testing.T) {
	base, err := LoadBaselineFile(baselinePath())
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	rec := NewRecorder(&capture{}, WithResolution(canvas.DPMM(1.0)))
	rec.RenderCanvas(gateScene(t), canvas.Identity, "gate")
	rep := rec.Report()
	rep.Frames[0].DurationNanos = base.FrameNanosMax + 1
	rep.Frames[0].DurationValid = true
	err = base.Check(rep)
	if err == nil {
		t.Fatal("gate accepted a frame that exceeded its duration bound")
	}
	assertMentions(t, err, "duration")
}
