package renderstats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// Baseline is a threshold baseline for a fixed scene. It is intentionally
// tolerant of floating-point and timing noise while being strict about
// operation counts, operation order, resource IDs, clip classification, and
// bounding boxes within a tolerance. Use [Baseline.Check] to gate a [Report].
type Baseline struct {
	Schema  string `json:"schema"`
	Version int    `json:"version"`
	// Tolerance is the absolute bounding-box tolerance in canvas units
	// (millimeters). Operation baselines may override it.
	Tolerance float64 `json:"tolerance"`
	// NanosMax is the maximum accepted per-operation render time.
	NanosMax int64 `json:"nanos_max"`
	// FrameNanosMax is the maximum accepted total frame duration. It is
	// intentionally much more generous than NanosMax so that CI scheduling
	// noise does not fail the gate.
	FrameNanosMax int64 `json:"frame_nanos_max"`
	// TempBytesMax is the maximum accepted per-operation temporary buffer
	// estimate.
	TempBytesMax int64 `json:"temp_bytes_max"`
	// ResourceCount is the exact number of distinct resources expected.
	ResourceCount int             `json:"resource_count"`
	Frames        []FrameBaseline `json:"frames"`
}

// FrameBaseline describes the expected operations of one frame, in draw order.
type FrameBaseline struct {
	Name     string       `json:"name"`
	ClipSet  bool         `json:"clip_set"`
	ClipRect BBox         `json:"clip_rect"`
	Ops      []OpBaseline `json:"ops"`
}

// OpBaseline describes one expected operation.
type OpBaseline struct {
	Kind  OpKind  `json:"kind"`
	Local BBox    `json:"local_bbox"`
	World BBox    `json:"world_bbox"`
	Clip  ClipHit `json:"clip_hit"`
	// Tolerance overrides Baseline.Tolerance when non-nil.
	Tolerance *float64 `json:"tolerance,omitempty"`
}

// LoadBaseline decodes a baseline from r and validates its schema version.
func LoadBaseline(r io.Reader) (*Baseline, error) {
	var b Baseline
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("renderstats: decode baseline: %w", err)
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	return &b, nil
}

// LoadBaselineFile loads a baseline from a JSON file.
func LoadBaselineFile(filename string) (*Baseline, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	base, err := LoadBaseline(f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return base, err
}

// Marshal encodes the baseline as indented, stable JSON.
func (b *Baseline) Marshal() ([]byte, error) {
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func (b *Baseline) validate() error {
	if b.Schema != Schema {
		return fmt.Errorf("renderstats: baseline schema %q, want %q", b.Schema, Schema)
	}
	if b.Version != Version {
		return fmt.Errorf("renderstats: baseline version %d, want %d", b.Version, Version)
	}
	if !finite(b.Tolerance) || b.Tolerance < 0 {
		return fmt.Errorf("renderstats: baseline tolerance must be a non-negative finite number")
	}
	if b.NanosMax < 0 || b.FrameNanosMax < 0 || b.TempBytesMax < 0 || b.ResourceCount < 0 {
		return fmt.Errorf("renderstats: baseline thresholds must be non-negative")
	}
	if len(b.Frames) == 0 {
		return fmt.Errorf("renderstats: baseline has no frames")
	}
	for i, f := range b.Frames {
		if len(f.Ops) == 0 {
			return fmt.Errorf("renderstats: baseline frame %d has no operations", i)
		}
		for j, op := range f.Ops {
			switch op.Kind {
			case KindPath, KindText, KindImage:
			default:
				return fmt.Errorf("renderstats: baseline frame %d op %d has unknown kind %q", i, j, op.Kind)
			}
			switch op.Clip {
			case "", ClipNone, ClipInside, ClipPartial, ClipOutside:
			default:
				return fmt.Errorf("renderstats: baseline frame %d op %d has unknown clip hit %q", i, j, op.Clip)
			}
			if op.Tolerance != nil && (!finite(*op.Tolerance) || *op.Tolerance < 0) {
				return fmt.Errorf("renderstats: baseline frame %d op %d has invalid tolerance", i, j)
			}
		}
	}
	return nil
}

// Check compares a report against the baseline. It returns nil when every
// threshold holds, and otherwise an error whose message lists all failures in
// stable order (frames in index order, operations in draw order, fields in
// declaration order). Check never panics and is safe to call on a report
// captured concurrently.
func (b *Baseline) Check(rep *Report) error {
	if rep == nil {
		return fmt.Errorf("renderstats: nil report")
	}
	var failures []string
	add := func(format string, args ...any) {
		failures = append(failures, fmt.Sprintf(format, args...))
	}

	if rep.Schema != Schema {
		add("report schema %q, want %q", rep.Schema, Schema)
	}
	if rep.Version != Version {
		add("report version %d, want %d", rep.Version, Version)
	}
	for i, frame := range rep.Frames {
		if frame.Index != i {
			add("frame %d: index %d, want %d (frames must be ordered)", i, frame.Index, i)
		}
		if frame.Open {
			add("frame %d (%q): is still open", i, frame.Name)
		}
		if frame.Overflow {
			add("frame %d (%q): overflowed with %d dropped operations", i, frame.Name, frame.DroppedOps)
		}
		if !frame.DurationValid {
			add("frame %d (%q): duration is not valid", i, frame.Name)
		}
		if frame.DurationNanos < 0 || b.FrameNanosMax < frame.DurationNanos {
			add("frame %d (%q): duration %dns outside [0,%d]", i, frame.Name, frame.DurationNanos, b.FrameNanosMax)
		}
		checkCounts(add, i, &frame)
		for j, op := range frame.Ops {
			checkOpFinite(add, i, j, op)
			if op.RenderValid {
				if op.RenderNanos < 0 || b.NanosMax < op.RenderNanos {
					add("frame %d op %d: render time %dns outside [0,%d]", i, j, op.RenderNanos, b.NanosMax)
				}
			} else if op.RenderNanos != 0 {
				add("frame %d op %d: invalid render timing must be zero, got %d", i, j, op.RenderNanos)
			}
			if op.TempBytes < 0 || b.TempBytesMax < op.TempBytes {
				add("frame %d op %d: temp buffer %d outside [0,%d]", i, j, op.TempBytes, b.TempBytesMax)
			}
			if op.RenderError != "" {
				add("frame %d op %d: renderer error: %s", i, j, op.RenderError)
			}
			if op.StatsError != "" {
				add("frame %d op %d: stats error: %s", i, j, op.StatsError)
			}
			if op.ResourceID == "" {
				add("frame %d op %d: empty resource ID", i, j)
			}
		}
	}
	if len(rep.Frames) != len(b.Frames) {
		add("frames: got %d, want %d", len(rep.Frames), len(b.Frames))
	}
	for i := 0; i < len(rep.Frames) && i < len(b.Frames); i++ {
		checkFrame(add, b, i, rep.Frames[i], b.Frames[i])
	}
	checkResources(add, b, rep)
	return joinFailures(failures)
}

func checkCounts(add func(string, ...any), i int, frame *Frame) {
	counts := Counts{}
	for _, op := range frame.Ops {
		switch op.Kind {
		case KindPath:
			counts.Path++
		case KindText:
			counts.Text++
		case KindImage:
			counts.Image++
		}
		counts.Total++
	}
	counts.Total += frame.DroppedOps
	// Dropped operations are only counted in Total, not per kind, so per-kind
	// consistency can only be checked when nothing was dropped. Overflow is
	// already reported as its own failure by Check.
	if !frame.Overflow && counts != frame.Counts {
		add("frame %d (%q): counts %+v inconsistent with %d ops and %d dropped, want %+v",
			i, frame.Name, frame.Counts, len(frame.Ops), frame.DroppedOps, counts)
	}
	clip := ClipCounts{}
	for _, op := range frame.Ops {
		switch op.ClipHit {
		case ClipInside:
			clip.Inside++
		case ClipPartial:
			clip.Partial++
		case ClipOutside:
			clip.Outside++
		case ClipNone:
		default:
			add("frame %d (%q): op %d has unknown clip hit %q", i, frame.Name, op.Seq, op.ClipHit)
		}
	}
	if clip != frame.Clip {
		add("frame %d (%q): clip counts %+v, want %+v", i, frame.Name, frame.Clip, clip)
	}
	if frame.ClipSet != frame.ClipRect.Valid {
		add("frame %d (%q): clip_set=%v but clip_rect.valid=%v", i, frame.Name, frame.ClipSet, frame.ClipRect.Valid)
	}
}

func checkOpFinite(add func(string, ...any), i, j int, op Op) {
	if !finite(op.Local.X0) || !finite(op.Local.Y0) || !finite(op.Local.X1) || !finite(op.Local.Y1) {
		add("frame %d op %d: local bbox is not finite", i, j)
	}
	if !finite(op.World.X0) || !finite(op.World.Y0) || !finite(op.World.X1) || !finite(op.World.Y1) {
		add("frame %d op %d: world bbox is not finite", i, j)
	}
}

func checkFrame(add func(string, ...any), b *Baseline, i int, frame Frame, want FrameBaseline) {
	if want.Name != "" && frame.Name != want.Name {
		add("frame %d: name %q, want %q", i, frame.Name, want.Name)
	}
	if frame.ClipSet != want.ClipSet {
		add("frame %d (%q): clip_set %v, want %v", i, frame.Name, frame.ClipSet, want.ClipSet)
	}
	if want.ClipSet {
		checkBBox(add, fmt.Sprintf("frame %d (%q) clip_rect", i, frame.Name), frame.ClipRect, want.ClipRect, b.Tolerance)
	}
	if len(frame.Ops) != len(want.Ops) {
		add("frame %d (%q): got %d operations, want %d", i, frame.Name, len(frame.Ops), len(want.Ops))
	}
	for j := 0; j < len(frame.Ops) && j < len(want.Ops); j++ {
		got, exp := frame.Ops[j], want.Ops[j]
		where := fmt.Sprintf("frame %d (%q) op %d", i, frame.Name, j)
		if got.Seq != j {
			add("%s: seq %d, want %d (operations must be in draw order)", where, got.Seq, j)
		}
		if got.Kind != exp.Kind {
			add("%s: kind %q, want %q", where, got.Kind, exp.Kind)
			continue
		}
		tol := b.Tolerance
		if exp.Tolerance != nil {
			tol = *exp.Tolerance
		}
		checkBBox(add, where+" local_bbox", got.Local, exp.Local, tol)
		checkBBox(add, where+" world_bbox", got.World, exp.World, tol)
		clip := exp.Clip
		if clip == "" {
			clip = ClipNone
		}
		if got.ClipHit != clip {
			add("%s: clip_hit %q, want %q", where, got.ClipHit, clip)
		}
		if got.ResourceID == "" || !strings.HasPrefix(got.ResourceID, string(exp.Kind)+"-") {
			add("%s: resource_id %q is not a %q resource ID", where, got.ResourceID, exp.Kind)
		}
	}
}

func checkBBox(add func(string, ...any), where string, got, want BBox, tol float64) {
	if got.Valid != want.Valid {
		add("%s: valid %v, want %v", where, got.Valid, want.Valid)
		return
	}
	if !got.Valid {
		return
	}
	if got.Empty != want.Empty {
		add("%s: empty %v, want %v", where, got.Empty, want.Empty)
	}
	for _, c := range []struct {
		name     string
		got, exp float64
	}{
		{"x0", got.X0, want.X0},
		{"y0", got.Y0, want.Y0},
		{"x1", got.X1, want.X1},
		{"y1", got.Y1, want.Y1},
	} {
		if math.Abs(c.got-c.exp) > tol {
			add("%s.%s: %g, want %g (tolerance %g)", where, c.name, c.got, c.exp, tol)
		}
	}
}

func checkResources(add func(string, ...any), b *Baseline, rep *Report) {
	if rep.Resources.Overflow {
		add("resources: overflowed")
	}
	if len(rep.Resources.Items) != b.ResourceCount {
		add("resources: got %d, want %d", len(rep.Resources.Items), b.ResourceCount)
	}
	seen := map[string]bool{}
	prev := ""
	for i, res := range rep.Resources.Items {
		if res.ID == "" {
			add("resources[%d]: empty ID", i)
		}
		if seen[res.ID] {
			add("resources[%d]: duplicate ID %q", i, res.ID)
		}
		seen[res.ID] = true
		if i != 0 && res.ID < prev {
			add("resources[%d]: ID %q is not sorted after %q", i, res.ID, prev)
		}
		prev = res.ID
		if res.Count < 1 {
			add("resources[%d] (%q): count %d < 1", i, res.ID, res.Count)
		}
		if res.FirstFrame < 0 {
			add("resources[%d] (%q): negative first frame %d", i, res.ID, res.FirstFrame)
		}
	}
}

func joinFailures(failures []string) error {
	if len(failures) == 0 {
		return nil
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "renderstats: %d gate failure(s):", len(failures))
	for _, f := range failures {
		buf.WriteString("\n  - ")
		buf.WriteString(f)
	}
	return fmt.Errorf("%s", buf.String())
}
