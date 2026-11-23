package renderstats

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/tdewolff/canvas"
)

// Schema identifies the JSON report schema.
const Schema = "renderstats.report"

// Version is the report schema version. It is incremented whenever a field is
// removed, renamed, or changes meaning. New fields may be added without a
// version bump.
const Version = 1

// OpKind identifies the renderer entry point of an operation.
type OpKind string

// Operation kinds.
const (
	KindPath  OpKind = "path"
	KindText  OpKind = "text"
	KindImage OpKind = "image"
)

// ClipHit describes how a world bounding box relates to the statistic-only
// clip window set with Recorder.SetClip.
type ClipHit string

// Clip classifications.
const (
	ClipNone    ClipHit = "none"    // no clip window was set
	ClipInside  ClipHit = "inside"  // world box fully inside the clip window
	ClipPartial ClipHit = "partial" // world box touches but is not fully inside the window
	ClipOutside ClipHit = "outside" // world box is outside the window, or is not finite
)

// PanicPolicy controls what happens when the wrapped renderer panics.
type PanicPolicy int

const (
	// PanicSwallow records the panic in the operation and continues; the
	// Recorder never panics because of the wrapped renderer. This is the
	// default and is what a statistics gate should use.
	PanicSwallow PanicPolicy = iota
	// PanicPropagate records the panic and then re-panics with the original
	// value, preserving the wrapped renderer's behaviour.
	PanicPropagate
)

// BBox is a bounding box with an explicit validity flag. Invalid boxes have
// all coordinates set to zero so that they always serialize to finite JSON.
type BBox struct {
	X0    float64 `json:"x0"`
	Y0    float64 `json:"y0"`
	X1    float64 `json:"x1"`
	Y1    float64 `json:"y1"`
	Valid bool    `json:"valid"`
	Empty bool    `json:"empty"`
}

// Rect converts the box to a canvas.Rect. The result is only meaningful when
// Valid is true.
func (b BBox) Rect() canvas.Rect {
	return canvas.Rect{X0: b.X0, Y0: b.Y0, X1: b.X1, Y1: b.Y1}
}

// Counts holds per-frame operation counts. Total includes operations whose
// detailed record was dropped due to a bound.
type Counts struct {
	Path  int `json:"path"`
	Text  int `json:"text"`
	Image int `json:"image"`
	Total int `json:"total"`
}

// ClipCounts holds per-frame clip classifications. It only counts operations
// when a clip window was set.
type ClipCounts struct {
	Inside  int `json:"inside"`
	Partial int `json:"partial"`
	Outside int `json:"outside"`
}

// Op is one recorded renderer call.
type Op struct {
	// Seq is the operation's index within its frame. It includes operations
	// whose detailed record was dropped, so gaps indicate dropped records.
	Seq         int     `json:"seq"`
	Kind        OpKind  `json:"kind"`
	ResourceID  string  `json:"resource_id"`
	Local       BBox    `json:"local_bbox"`
	World       BBox    `json:"world_bbox"`
	ClipHit     ClipHit `json:"clip_hit"`
	RenderNanos int64   `json:"render_nanos"`
	RenderValid bool    `json:"render_valid"`
	TempBytes   int64   `json:"temp_buffer_bytes"`
	// StatsError is non-empty when extracting statistics (bounds or resource
	// hash) failed. The renderer call is still forwarded untouched.
	StatsError string `json:"stats_error,omitempty"`
	// RenderError is non-empty when the wrapped renderer panicked.
	RenderError string `json:"render_error,omitempty"`
}

// Frame is the statistics for one frame. A frame is opened implicitly by the
// first operation, by BeginFrame, or by RenderCanvas, and closed by EndFrame,
// Cancel, or the next BeginFrame.
type Frame struct {
	Index         int        `json:"index"`
	Name          string     `json:"name"`
	Open          bool       `json:"open"`
	Canceled      bool       `json:"canceled"`
	DurationNanos int64      `json:"duration_nanos"`
	DurationValid bool       `json:"duration_valid"`
	Counts        Counts     `json:"counts"`
	Clip          ClipCounts `json:"clip_hits"`
	ClipSet       bool       `json:"clip_set"`
	ClipRect      BBox       `json:"clip_rect"`
	DroppedOps    int        `json:"dropped_ops"`
	Overflow      bool       `json:"overflow"`
	// TempBytes is the sum of per-operation temporary buffer estimates.
	TempBytes int64 `json:"temp_buffer_bytes"`
	// PeakTempBytes is the largest single-operation temporary buffer estimate
	// in the frame: the peak scratch memory needed at one point in time.
	PeakTempBytes int64 `json:"peak_temp_buffer_bytes"`
	Ops           []Op  `json:"ops"`
}

// Resource describes one distinct content-addressed resource.
type Resource struct {
	ID         string `json:"id"`
	Kind       OpKind `json:"kind"`
	Count      int    `json:"count"`
	FirstFrame int    `json:"first_frame"`
}

// ResourceSet is the bounded resource registry.
type ResourceSet struct {
	Overflow bool       `json:"overflow"`
	Items    []Resource `json:"items"`
}

// Report is the complete, versioned, stably ordered statistics report.
type Report struct {
	Schema    string      `json:"schema"`
	Version   int         `json:"version"`
	Frames    []Frame     `json:"frames"`
	Resources ResourceSet `json:"resources"`
}

// Option configures a Recorder.
type Option func(*Recorder)

// WithMaxOps bounds the number of detailed operations retained per frame. A
// value <= 0 means unlimited. Counts always include dropped operations.
func WithMaxOps(n int) Option {
	return func(r *Recorder) { r.maxOps = n }
}

// WithMaxResources bounds the number of distinct resources retained. A value
// <= 0 means unlimited. Resource IDs are still attached to operations.
func WithMaxResources(n int) Option {
	return func(r *Recorder) { r.maxResources = n }
}

// WithResolution sets the resolution used to estimate raster temporary buffer
// sizes. It does not affect the renderer. The default is
// canvas.DefaultResolution.
func WithResolution(res canvas.Resolution) Option {
	return func(r *Recorder) {
		if res > 0 {
			r.resolution = res
		}
	}
}

// WithClock replaces the clock used for timing. It is intended for tests that
// need deterministic or malformed time sources.
func WithClock(clock func() time.Time) Option {
	return func(r *Recorder) {
		if clock != nil {
			r.clock = clock
		}
	}
}

// WithPanicPolicy sets how wrapped-renderer panics are handled. The default is
// PanicSwallow.
func WithPanicPolicy(policy PanicPolicy) Option {
	return func(r *Recorder) { r.policy = policy }
}

// WithImageHashSamples sets the per-axis sample count used to fingerprint an
// image. A value <= 0 disables pixel sampling, so only the image type and
// bounds are hashed. The default is 32 (at most 1024 sampled pixels).
func WithImageHashSamples(n int) Option {
	return func(r *Recorder) {
		if maxImageSamples < n {
			n = maxImageSamples
		}
		r.imageSamples = n
	}
}

// maxImageSamples bounds the per-axis image sampling so that fingerprinting a
// huge image stays cheap and its index arithmetic cannot overflow.
const maxImageSamples = 256

// Recorder wraps a canvas.Renderer and records statistics. See the package
// documentation for lifecycle and concurrency semantics.
type Recorder struct {
	mu sync.RWMutex

	renderer canvas.Renderer

	frames    []*frameState
	open      *frameState
	resources map[string]*Resource
	resIDs    []string
	resOver   bool

	maxOps       int
	maxResources int
	resolution   canvas.Resolution
	clock        func() time.Time
	policy       PanicPolicy
	imageSamples int

	clipOK  bool
	clip    canvas.Rect
	clipBox BBox
}

type frameState struct {
	Frame

	start time.Time
}

// NewRecorder returns a Recorder forwarding to renderer. A nil renderer is
// allowed: operations are still counted and timed, but not forwarded.
func NewRecorder(renderer canvas.Renderer, opts ...Option) *Recorder {
	r := &Recorder{
		renderer:     renderer,
		resources:    map[string]*Resource{},
		resolution:   canvas.DefaultResolution,
		clock:        time.Now,
		policy:       PanicSwallow,
		imageSamples: 32, // <= maxImageSamples
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// Renderer returns the wrapped renderer.
func (r *Recorder) Renderer() canvas.Renderer {
	return r.renderer
}

// Size implements canvas.Renderer. It returns the wrapped size, or zero for a
// nil renderer.
func (r *Recorder) Size() (float64, float64) {
	if r.renderer == nil {
		return 0.0, 0.0
	}
	return r.renderer.Size()
}

// SetZIndex forwards to the wrapped renderer when it supports SetZIndex, so
// that Context.SetZIndex keeps working when a Canvas is wrapped.
func (r *Recorder) SetZIndex(zindex int) {
	if setter, ok := r.renderer.(interface{ SetZIndex(int) }); ok {
		setter.SetZIndex(zindex)
	}
}

// SetClip sets a statistic-only clip window in world coordinates. It never
// clips rendering; it only classifies each operation's world bounding box.
// Non-finite coordinates disable the window. Use ClearClip to disable it
// explicitly.
func (r *Recorder) SetClip(rect canvas.Rect) {
	box := makeBBox(rect)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clipOK = box.Valid
	r.clip = rect
	r.clipBox = box
}

// ClearClip removes the statistic-only clip window.
func (r *Recorder) ClearClip() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clipOK = false
	r.clip = canvas.Rect{}
	r.clipBox = BBox{}
}

// RenderPath implements canvas.Renderer.
func (r *Recorder) RenderPath(path *canvas.Path, style canvas.Style, m canvas.Matrix) {
	r.record(KindPath, path, style, nil, nil, m)
}

// RenderText implements canvas.Renderer.
func (r *Recorder) RenderText(text *canvas.Text, m canvas.Matrix) {
	r.record(KindText, nil, canvas.Style{}, text, nil, m)
}

// RenderImage implements canvas.Renderer.
func (r *Recorder) RenderImage(img image.Image, m canvas.Matrix) {
	r.record(KindImage, nil, canvas.Style{}, nil, img, m)
}

func (r *Recorder) record(kind OpKind, path *canvas.Path, style canvas.Style, text *canvas.Text, img image.Image, m canvas.Matrix) {
	var op Op
	op.Kind = kind
	op.Local = BBox{}
	op.World = BBox{}

	var local canvas.Rect
	present := false
	func() {
		defer func() {
			if v := recover(); v != nil {
				op.StatsError = fmt.Sprint(v)
				local = canvas.Rect{}
				op.ResourceID = ""
				op.TempBytes = 0
			}
		}()
		switch kind {
		case KindPath:
			op.ResourceID = pathResourceID(path)
			if path != nil {
				present = true
				local = path.Bounds()
				if style.HasStroke() {
					local = local.Expand(style.StrokeWidth / 2.0)
				}
			}
			op.TempBytes = estimatePathTemp(path, style, local, m, r.resolution)
		case KindText:
			op.ResourceID = textResourceID(text)
			if text != nil {
				present = true
				local = text.Bounds()
			}
			op.TempBytes = estimateTextTemp(text, local, m, r.resolution)
		case KindImage:
			op.ResourceID = r.imageResourceID(img)
			if img != nil {
				present = true
				b := img.Bounds()
				local = canvas.Rect{
					X0: float64(b.Min.X), Y0: float64(b.Min.Y),
					X1: float64(b.Max.X), Y1: float64(b.Max.Y),
				}
			}
			op.TempBytes = estimateImageTemp(img, local, m, r.resolution)
		}
		if present {
			// A nil input has no geometry: report an invalid box rather than a
			// zero-size box at the origin.
			op.Local = makeBBox(local)
			op.World = transformBBox(local, m)
		}
	}()

	start := r.clock()
	panicVal := r.forward(kind, path, style, text, img, m)
	end := r.clock()
	op.RenderNanos, op.RenderValid = elapsed(start, end)
	if panicVal != nil {
		op.RenderError = fmt.Sprint(panicVal)
	}

	r.mu.Lock()
	f := r.openFrameLocked()
	op.Seq = f.Counts.Total
	f.Counts.Total++
	switch kind {
	case KindPath:
		f.Counts.Path++
	case KindText:
		f.Counts.Text++
	case KindImage:
		f.Counts.Image++
	}
	op.ClipHit = r.clipHitLocked(op.World)
	switch op.ClipHit {
	case ClipInside:
		f.Clip.Inside++
	case ClipPartial:
		f.Clip.Partial++
	case ClipOutside:
		f.Clip.Outside++
	case ClipNone:
	}
	f.TempBytes += op.TempBytes
	if f.PeakTempBytes < op.TempBytes {
		f.PeakTempBytes = op.TempBytes
	}
	r.addResourceLocked(op.ResourceID, kind, f.Index)
	if r.maxOps > 0 && len(f.Ops) >= r.maxOps {
		f.DroppedOps++
		f.Overflow = true
	} else {
		f.Ops = append(f.Ops, op)
	}
	r.mu.Unlock()

	if panicVal != nil && r.policy == PanicPropagate {
		panic(panicVal)
	}
}

// forward invokes the wrapped renderer and returns a recovered panic value, or
// nil. A nil wrapped renderer is a no-op.
func (r *Recorder) forward(kind OpKind, path *canvas.Path, style canvas.Style, text *canvas.Text, img image.Image, m canvas.Matrix) (panicVal any) {
	if r.renderer == nil {
		return nil
	}
	defer func() {
		if v := recover(); v != nil {
			panicVal = v
		}
	}()
	switch kind {
	case KindPath:
		r.renderer.RenderPath(path, style, m)
	case KindText:
		r.renderer.RenderText(text, m)
	case KindImage:
		r.renderer.RenderImage(img, m)
	}
	return nil
}

// BeginFrame closes any open frame and starts a named frame.
func (r *Recorder) BeginFrame(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeFrameLocked(false)
	r.appendFrameLocked(r.clock(), name)
}

// EndFrame closes the open frame. It is a no-op when no frame is open.
func (r *Recorder) EndFrame() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeFrameLocked(false)
}

// Cancel closes the open frame as canceled. It does not affect rendering and
// does not stop later operations from being recorded: the next operation
// starts a fresh implicit frame. Canceling with no open frame is a no-op.
func (r *Recorder) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeFrameLocked(true)
}

// RenderCanvas opens a frame named name, renders c with the given view through
// the Recorder, and closes the frame. It is equivalent to BeginFrame,
// Canvas.RenderViewTo, EndFrame.
func (r *Recorder) RenderCanvas(c *canvas.Canvas, view canvas.Matrix, name string) {
	r.BeginFrame(name)
	if c != nil {
		c.RenderViewTo(r, view)
	}
	r.EndFrame()
}

// Reset clears all frames, resources, and clip state. Configuration (options)
// is retained. It must not race with rendering.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = r.frames[:0]
	r.open = nil
	r.resources = map[string]*Resource{}
	r.resIDs = r.resIDs[:0]
	r.resOver = false
	r.clipOK = false
	r.clip = canvas.Rect{}
	r.clipBox = BBox{}
}

func (r *Recorder) openFrameLocked() *frameState {
	if r.open == nil {
		name := fmt.Sprintf("frame-%d", len(r.frames)+1)
		r.appendFrameLocked(r.clock(), name)
	}
	return r.open
}

func (r *Recorder) appendFrameLocked(start time.Time, name string) {
	f := &frameState{
		Frame: Frame{
			Index:    len(r.frames),
			Name:     name,
			Open:     true,
			Ops:      []Op{},
			ClipSet:  r.clipOK,
			ClipRect: r.clipBox,
		},
		start: start,
	}
	r.frames = append(r.frames, f)
	r.open = f
}

func (r *Recorder) closeFrameLocked(canceled bool) {
	f := r.open
	if f == nil {
		return
	}
	f.DurationNanos, f.DurationValid = elapsed(f.start, r.clock())
	f.Open = false
	f.Canceled = canceled
	r.open = nil
}

func (r *Recorder) clipHitLocked(world BBox) ClipHit {
	if !r.clipOK {
		return ClipNone
	}
	if !world.Valid {
		return ClipOutside
	}
	rect := world.Rect()
	if r.clip.Contains(rect) {
		return ClipInside
	} else if r.clip.Touches(rect) {
		return ClipPartial
	}
	return ClipOutside
}

func (r *Recorder) addResourceLocked(id string, kind OpKind, frame int) {
	if id == "" {
		return
	}
	if res, ok := r.resources[id]; ok {
		res.Count++
		return
	}
	if r.maxResources > 0 && len(r.resources) >= r.maxResources {
		r.resOver = true
		return
	}
	r.resources[id] = &Resource{ID: id, Kind: kind, Count: 1, FirstFrame: frame}
	r.resIDs = append(r.resIDs, id)
}

// Report returns a snapshot of all recorded statistics. The returned report is
// independent of the Recorder and safe to retain.
func (r *Recorder) Report() *Report {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.reportLocked()
}

func (r *Recorder) reportLocked() *Report {
	rep := &Report{
		Schema:  Schema,
		Version: Version,
		Frames:  make([]Frame, 0, len(r.frames)),
		Resources: ResourceSet{
			Overflow: r.resOver,
			Items:    make([]Resource, 0, len(r.resources)),
		},
	}
	for _, f := range r.frames {
		fr := f.Frame
		fr.Ops = append([]Op(nil), f.Ops...)
		if fr.Ops == nil {
			fr.Ops = []Op{}
		}
		if fr.Open {
			fr.DurationNanos, fr.DurationValid = elapsed(f.start, r.clock())
		}
		rep.Frames = append(rep.Frames, fr)
	}
	ids := append([]string(nil), r.resIDs...)
	sort.Strings(ids)
	for _, id := range ids {
		rep.Resources.Items = append(rep.Resources.Items, *r.resources[id])
	}
	return rep
}

// ExportJSON returns the full report as indented, versioned JSON with stable
// field and slice ordering. Closed frames export byte-identically across
// calls. An open frame's duration advances with wall-clock time.
func (r *Recorder) ExportJSON() ([]byte, error) {
	return marshalReport(r.Report())
}

// ExportFrameJSON returns the JSON of a single frame by its Index. It returns
// an error when the frame does not exist.
func (r *Recorder) ExportFrameJSON(index int) ([]byte, error) {
	rep := r.Report()
	for _, frame := range rep.Frames {
		if frame.Index == index {
			return marshalReport(&Report{
				Schema:    rep.Schema,
				Version:   rep.Version,
				Frames:    []Frame{frame},
				Resources: rep.Resources,
			})
		}
	}
	return nil, fmt.Errorf("renderstats: frame %d not found", index)
}

// WriteFramesJSON writes one JSON file per frame into dir, named
// frame-<index>.json, and returns the written paths in frame order. The
// directory must already exist. Per-frame files contain the same schema and
// version as the full report.
func (r *Recorder) WriteFramesJSON(dir string) ([]string, error) {
	rep := r.Report()
	paths := make([]string, 0, len(rep.Frames))
	for _, frame := range rep.Frames {
		b, err := marshalReport(&Report{
			Schema:    rep.Schema,
			Version:   rep.Version,
			Frames:    []Frame{frame},
			Resources: rep.Resources,
		})
		if err != nil {
			return paths, err
		}
		name := fmt.Sprintf("frame-%04d.json", frame.Index)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return paths, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func marshalReport(rep *Report) ([]byte, error) {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Frames returns the number of recorded frames.
func (r *Recorder) Frames() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.frames)
}

// elapsed returns end-start in nanoseconds when the result is usable, and
// false otherwise. Negative durations (a clock that went backwards) are
// reported as invalid rather than as a negative number.
func elapsed(start, end time.Time) (int64, bool) {
	d := end.Sub(start)
	if d < 0 {
		return 0, false
	}
	return int64(d), true
}

// makeBBox converts a rect to a BBox. Non-finite rects are invalid; valid
// boxes carry their emptiness so that a zero-size image is distinguishable
// from a failed measurement.
func makeBBox(rect canvas.Rect) BBox {
	if !finite(rect.X0) || !finite(rect.Y0) || !finite(rect.X1) || !finite(rect.Y1) {
		return BBox{}
	}
	return BBox{
		X0: rect.X0, Y0: rect.Y0, X1: rect.X1, Y1: rect.Y1,
		Valid: true,
		Empty: rect.Empty(),
	}
}

// transformBBox returns the world bounding box after m. Non-finite matrices or
// rects yield an invalid box.
func transformBBox(rect canvas.Rect, m canvas.Matrix) BBox {
	if !makeBBox(rect).Valid {
		return BBox{}
	}
	for _, row := range m {
		for _, v := range row {
			if !finite(v) {
				return BBox{}
			}
		}
	}
	return makeBBox(rect.Transform(m))
}

func finite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// estimatePathTemp estimates geometry copies plus raster coverage memory.
func estimatePathTemp(path *canvas.Path, style canvas.Style, local canvas.Rect, m canvas.Matrix, res canvas.Resolution) int64 {
	if path == nil {
		return 0
	}
	data := int64(len(path.Data())) * 8
	geometry := data
	if style.HasFill() && style.HasStroke() {
		geometry = data * 2
	}
	return clampBytes(float64(geometry) + rasterBytes(local, m, res))
}

func estimateTextTemp(text *canvas.Text, local canvas.Rect, m canvas.Matrix, res canvas.Resolution) int64 {
	if text == nil {
		return 0
	}
	glyphs := 0
	text.WalkSpans(func(_, _ float64, span canvas.TextSpan) {
		if len(span.Glyphs) != 0 {
			glyphs += len(span.Glyphs)
		} else {
			glyphs += len(span.Text)
		}
	})
	geometry := float64(glyphs) * 64.0 // estimated outline scratch space
	return clampBytes(geometry + rasterBytes(local, m, res))
}

func estimateImageTemp(img image.Image, local canvas.Rect, m canvas.Matrix, res canvas.Resolution) int64 {
	if img == nil {
		return 0
	}
	b := img.Bounds()
	src := float64(b.Dx()) * float64(b.Dy()) * 4.0
	return clampBytes(src + rasterBytes(local, m, res))
}

// rasterBytes estimates coverage+raster memory for a transformed box at res.
func rasterBytes(local canvas.Rect, m canvas.Matrix, res canvas.Resolution) float64 {
	world := local.Transform(m)
	if !finite(world.X0) || !finite(world.Y0) || !finite(world.X1) || !finite(world.Y1) {
		return 0.0
	}
	w, h := world.W(), world.H()
	if !finite(w) || !finite(h) || w < 0.0 || h < 0.0 {
		return 0.0
	}
	dpmm := res.DPMM()
	if !finite(dpmm) || dpmm <= 0.0 {
		dpmm = 1.0
	}
	px := w * dpmm * h * dpmm
	if !finite(px) || px <= 0.0 {
		return 0.0
	}
	return px * 4.0
}

// maxTempBytes caps estimates so a huge scene cannot overflow int64.
const maxTempBytes = float64(1 << 42)

func clampBytes(v float64) int64 {
	if !finite(v) || v <= 0.0 {
		return 0
	}
	if v >= maxTempBytes {
		return int64(maxTempBytes)
	}
	return int64(v)
}

func pathResourceID(path *canvas.Path) string {
	if path == nil {
		return nilID("path")
	}
	h := fnv.New64a()
	h.Write([]byte{'p'})
	var buf [8]byte
	for _, f := range path.Data() {
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(f))
		h.Write(buf[:])
	}
	return formatID(KindPath, h.Sum64())
}

func textResourceID(text *canvas.Text) string {
	if text == nil {
		return nilID("text")
	}
	h := fnv.New64a()
	h.Write([]byte{'t'})
	h.Write([]byte(text.Text))
	var buf [8]byte
	putInt := func(v int) {
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		h.Write(buf[:])
	}
	putFloat := func(f float64) {
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(f))
		h.Write(buf[:])
	}
	putInt(int(text.WritingMode))
	putInt(int(text.TextOrientation))
	text.WalkSpans(func(x, y float64, span canvas.TextSpan) {
		putFloat(x)
		putFloat(y)
		putFloat(span.Width)
		h.Write([]byte(span.Text))
		if span.Face != nil {
			if span.Face.Font != nil {
				h.Write([]byte(span.Face.Font.Name()))
			}
			putFloat(span.Face.Size)
			putInt(int(span.Face.Style))
			putInt(int(span.Face.Variant))
			if span.Face.Fill.IsColor() {
				c := span.Face.Fill.Color
				h.Write([]byte{c.R, c.G, c.B, c.A})
			}
		}
		putInt(len(span.Glyphs))
		for _, g := range span.Glyphs {
			putInt(int(g.ID))
			putInt(int(g.Cluster))
			putInt(int(g.XAdvance))
			putInt(int(g.YAdvance))
		}
	})
	return formatID(KindText, h.Sum64())
}

func (r *Recorder) imageResourceID(img image.Image) string {
	if img == nil {
		return nilID("image")
	}
	h := fnv.New64a()
	h.Write([]byte{'i'})
	h.Write([]byte(fmt.Sprintf("%T", img)))
	b := img.Bounds()
	var buf [8]byte
	for _, v := range []int{b.Min.X, b.Min.Y, b.Max.X, b.Max.Y} {
		binary.LittleEndian.PutUint64(buf[:], uint64(int64(v)))
		h.Write(buf[:])
	}
	if 0 < r.imageSamples && !b.Empty() {
		func() {
			defer func() { _ = recover() }() // hostile image.Image.At must not panic the gate
			n := r.imageSamples
			if maxImageSamples < n {
				n = maxImageSamples
			}
			if 0 < n {
				dx, dy := int64(b.Dx()), int64(b.Dy())
				for j := 0; j < n; j++ {
					for i := 0; i < n; i++ {
						// Sample at pixel centres, deterministically spanning the bounds.
						x := b.Min.X + int((2*int64(i)+1)*dx/int64(2*n))
						y := b.Min.Y + int((2*int64(j)+1)*dy/int64(2*n))
						cr, cg, cb, ca := img.At(x, y).RGBA()
						binary.LittleEndian.PutUint32(buf[:4], cr)
						binary.LittleEndian.PutUint32(buf[4:], cg)
						h.Write(buf[:8])
						binary.LittleEndian.PutUint32(buf[:4], cb)
						binary.LittleEndian.PutUint32(buf[4:], ca)
						h.Write(buf[:8])
					}
				}
			}
		}()
	}
	return formatID(KindImage, h.Sum64())
}

func formatID(kind OpKind, sum uint64) string {
	return fmt.Sprintf("%s-%016x", kind, sum)
}

func nilID(kind string) string {
	h := fnv.New64a()
	h.Write([]byte(kind))
	return fmt.Sprintf("nil-%s-%016x", kind, h.Sum64())
}
