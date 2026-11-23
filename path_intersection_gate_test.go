package canvas

// Structured quality gate for the boolean path operations (And/Or/Xor/Not/Div).
//
// The gate exists to localize where complex boolean operations go wrong (e.g. hairline
// cracks in the output): it runs every sample through explicit stages
// (scan -> settle -> boolean -> winding) and reports per-stage checks instead of only
// asserting the final outline. Each run emits a stable JSON summary with the sample ID,
// stage, tolerance, timings and the enforced resource limits. The corpus is fixed and
// purely computational: no fonts, no network, no rasterization, so results are
// reproducible across platforms.
//
// Run with:   go test -run TestPathBooleanGate -v
// Race mode:  go test -race -run TestPathBooleanGate
// Write JSON: CANVAS_GATE_REPORT=report.json go test -run TestPathBooleanGate

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"
)

// gateSample is one fixed corpus entry. areas holds optional analytic areas per op
// (only for axis-aligned/exact cases); when present the gate checks the measured area
// against it, which gives the tolerance fault-injection something to bite on.
type gateSample struct {
	id       string
	category string
	p, q     string
	areas    map[string]float64
}

var gateSamples = []gateSample{
	// straight lines
	{id: "lines/overlap-rects", category: "lines",
		p: "M0 0L10 0L10 10L0 10z", q: "M5.5 5.5L15.5 5.5L15.5 15.5L5.5 15.5z",
		areas: map[string]float64{"AND": 20.25, "OR": 179.75, "XOR": 159.5, "NOT": 79.75, "DIV": 100.0}},
	{id: "lines/contained-rect", category: "lines",
		p: "M0 0L10 0L10 10L0 10z", q: "M2.5 2.5L7.5 2.5L7.5 7.5L2.5 7.5z",
		areas: map[string]float64{"AND": 25.0, "OR": 100.0, "XOR": 75.0, "NOT": 75.0, "DIV": 100.0}},
	{id: "lines/disjoint-rects", category: "lines",
		p: "M0 0L10 0L10 10L0 10z", q: "M20.5 0.5L30.5 0.5L30.5 10.5L20.5 10.5z",
		areas: map[string]float64{"AND": 0.0, "OR": 200.0, "XOR": 200.0, "NOT": 100.0, "DIV": 100.0}},
	{id: "lines/touch-point-triangles", category: "lines",
		p: "M0 0L10 0L5 5z", q: "M5 5L10 10L0 10z",
		areas: map[string]float64{"AND": 0.0, "OR": 50.0, "XOR": 50.0, "NOT": 25.0, "DIV": 25.0}},
	{id: "lines/touch-edge-rects", category: "lines",
		p: "M0 0L10 0L10 10L0 10z", q: "M10 0L20 0L20 10L10 10z",
		areas: map[string]float64{"AND": 0.0, "OR": 200.0, "XOR": 200.0, "NOT": 100.0, "DIV": 100.0}},

	// Bézier curves
	{id: "bezier/cubic-circle-rect", category: "bezier",
		p: "M10 5C10 7.761423749 7.761423749 10 5 10C2.238576251 10 0 7.761423749 0 5C0 2.238576251 2.238576251 0 5 0C7.761423749 0 10 2.238576251 10 5z",
		q: "M2.5 0L12.5 0L12.5 10L2.5 10z"},
	{id: "bezier/quad-arch-rect", category: "bezier",
		p: "M0 0Q10 10 20 0L20 10L0 10z",
		q: "M5 2.5L15 2.5L15 7.5L5 7.5z"},

	// tangency (touching, not crossing)
	{id: "tangent/cubic-circle-rect", category: "tangent",
		p: "M10 5C10 7.761423749 7.761423749 10 5 10C2.238576251 10 0 7.761423749 0 5C0 2.238576251 2.238576251 0 5 0C7.761423749 0 10 2.238576251 10 5z",
		q: "M10 0L20 0L20 10L10 10z"},
	{id: "tangent/triangle-apex-rect", category: "tangent",
		p: "M0 0L10 0L5 5z", q: "M0 5L10 5L10 15L0 15z",
		areas: map[string]float64{"AND": 0.0, "OR": 125.0, "XOR": 125.0, "NOT": 25.0, "DIV": 25.0}},

	// self-intersecting inputs
	{id: "self-intersecting/bowtie-bar", category: "self-intersecting",
		p: "M0 0L10 10L10 0L0 10z", q: "M2.5 -5L7.5 -5L7.5 15L2.5 15z"},
	{id: "self-intersecting/pentagram-rect", category: "self-intersecting",
		p: "M5 0L1.9098 9.5106L9.7553 3.6373L0.2447 3.6373L8.0902 9.5106z",
		q: "M0 2L10 2L10 6L0 6z"},

	// coincident (overlapping collinear) edges
	{id: "coincident-edges/partial-overlap", category: "coincident-edges",
		p: "M0 0L10 0L10 5L0 5z", q: "M5 0L15 0L15 5L5 5z",
		areas: map[string]float64{"AND": 25.0, "OR": 75.0, "XOR": 50.0, "NOT": 25.0, "DIV": 50.0}},
	{id: "coincident-edges/equal-rects", category: "coincident-edges",
		p: "M0 0L10 0L10 10L0 10z", q: "M0 0L10 0L10 10L0 10z",
		areas: map[string]float64{"AND": 100.0, "OR": 100.0, "XOR": 0.0, "NOT": 0.0, "DIV": 100.0}},
	{id: "coincident-edges/shared-corner", category: "coincident-edges",
		p: "M0 0L10 0L10 10L0 10z", q: "M0 0L5 0L5 5L0 5z",
		areas: map[string]float64{"AND": 25.0, "OR": 100.0, "XOR": 75.0, "NOT": 75.0, "DIV": 100.0}},

	// extreme coordinates
	{id: "extreme-coordinates/large-offset", category: "extreme-coordinates",
		p:     "M10000000 10000000L10010000 10000000L10010000 10010000L10000000 10010000z",
		q:     "M10003000 10003000L10013000 10003000L10013000 10013000L10003000 10013000z",
		areas: map[string]float64{"AND": 4.9e7, "OR": 1.51e8, "XOR": 1.02e8, "NOT": 5.1e7, "DIV": 1e8}},
	{id: "extreme-coordinates/small-scale", category: "extreme-coordinates",
		p: "M0 0L0.01 0L0.01 0.01L0 0.01z", q: "M0.005 0.005L0.015 0.005L0.015 0.015L0.005 0.015z",
		areas: map[string]float64{"AND": 2.5e-5, "OR": 1.75e-4, "XOR": 1.5e-4, "NOT": 7.5e-5, "DIV": 1e-4}},
}

// gateLimits are the resource bounds enforced by the gate and echoed into the report.
type gateLimits struct {
	MaxSampleDurationMicros int64   `json:"max_sample_duration_us"`
	MaxResultSegments       int     `json:"max_result_segments"`
	MaxAreaRelErr           float64 `json:"max_area_rel_err"`
	AreaAbsTol              float64 `json:"area_abs_tol"`
	FlattenRelTol           float64 `json:"flatten_rel_tol"`
}

var gateDefaultLimits = gateLimits{
	MaxSampleDurationMicros: 10_000_000, // 10 s per sample
	MaxResultSegments:       100_000,
	MaxAreaRelErr:           1e-6,
	AreaAbsTol:              1e-6,
	FlattenRelTol:           1e-7,
}

// gateFaults deliberately perturbs the pipeline to prove the gate catches defects.
type gateFaults struct {
	epsilon           float64 // when > 0, temporarily override BentleyOttmannEpsilon
	scrambleScanOrder bool    // reverse the scan-stage intersection order
	corruptResult     func(op string, r *Path) *Path
}

// --- JSON report schema (field order is fixed, so the summary is stable) ---

type gateCheckJSON struct {
	Stage  string `json:"stage"`
	Check  string `json:"check"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

type gateOpJSON struct {
	Op             string          `json:"op"`
	Segments       int             `json:"segments"`
	FillingPaths   int             `json:"filling_paths"`
	Holes          int             `json:"holes"`
	Area           float64         `json:"area"`
	AreaErr        *float64        `json:"area_err,omitempty"`
	DurationMicros int64           `json:"duration_us"`
	Checks         []gateCheckJSON `json:"checks"`
}

type gateSampleJSON struct {
	ID             string          `json:"id"`
	Category       string          `json:"category"`
	Tolerance      float64         `json:"tolerance"`
	Intersections  int             `json:"intersections"`
	SegmentsP      int             `json:"segments_p"`
	SegmentsQ      int             `json:"segments_q"`
	AreaP          float64         `json:"area_p"`
	AreaQ          float64         `json:"area_q"`
	AreaErrMax     float64         `json:"area_err_max"`
	Ops            []gateOpJSON    `json:"ops"`
	Checks         []gateCheckJSON `json:"checks"`
	DurationMicros int64           `json:"duration_us"`
	Pass           bool            `json:"pass"`
}

type gateReportJSON struct {
	Version               int              `json:"version"`
	FillRule              string           `json:"fill_rule"`
	BentleyOttmannEpsilon float64          `json:"bentley_ottmann_epsilon"`
	Limits                gateLimits       `json:"limits"`
	Samples               []gateSampleJSON `json:"samples"`
	Passed                int              `json:"passed"`
	Failed                int              `json:"failed"`
	DurationMicros        int64            `json:"duration_us"`
}

var gateOps = []struct {
	name string
	fn   func(p, q *Path) *Path
}{
	{"AND", (*Path).And},
	{"OR", (*Path).Or},
	{"XOR", (*Path).Xor},
	{"NOT", (*Path).Not},
	{"DIV", (*Path).Div},
}

// --- geometric helpers ---

func gateSegmentCount(p *Path) int {
	n := 0
	for i := 0; i < len(p.d); {
		i += cmdLen(p.d[i])
		n++
	}
	return n
}

func gateNoNaN(p *Path) (int, bool) {
	for i, v := range p.d {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return i, false
		}
	}
	return -1, true
}

func gateAllSubpathsClosed(p *Path) bool {
	if p.Empty() {
		return true
	}
	closed := false
	for i := 0; i < len(p.d); {
		cmd := p.d[i]
		if cmd == MoveToCmd && i != 0 && !closed {
			return false
		}
		closed = cmd == CloseCmd
		i += cmdLen(cmd)
	}
	return closed
}

// gateSignedArea is the shoelace area of a single (flattened) subpath.
func gateSignedArea(sub *Path) float64 {
	coords := sub.Coords()
	area := 0.0
	n := len(coords)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		area += coords[i].X*coords[j].Y - coords[j].X*coords[i].Y
	}
	return area / 2.0
}

// gateAreaFilled returns the net filled area of a settled path: filling contours and
// holes are oppositely oriented, so the absolute signed sum is the filled area.
func gateAreaFilled(p *Path, limits gateLimits) float64 {
	if p.Empty() {
		return 0.0
	}
	b := p.FastBounds()
	diag := math.Hypot(b.X1-b.X0, b.Y1-b.Y0)
	tol := limits.FlattenRelTol * math.Max(1.0, diag)
	flat := p.Flatten(tol)
	area := 0.0
	for _, sub := range flat.Split() {
		area += gateSignedArea(sub)
	}
	return math.Abs(area)
}

// gateOrientationOK verifies the orientation contract of settled output: every subpath
// is filling under NonZero iff it is oriented CCW (filling contours CCW, holes CW).
func gateOrientationOK(p *Path) (string, bool) {
	subs := p.Split()
	nz := p.Filling(NonZero)
	for i, sub := range subs {
		if nz[i] != sub.CCW() {
			return fmt.Sprintf("subpath %d: filling %v but CCW %v", i, nz[i], sub.CCW()), false
		}
	}
	return "", true
}

// gateFillRuleAgree verifies that the filled region is the same under NonZero and
// EvenOdd, as settled output has no overlapping or doubly-wound contours. It probes a
// deterministic grid of points instead of contour start points, so coincident contours
// (e.g. an island exactly on a hole boundary, as produced by Div) do not confuse it.
func gateFillRuleAgree(p *Path) (string, bool) {
	b := p.FastBounds()
	if b.X1 <= b.X0 || b.Y1 <= b.Y0 {
		return "", true
	}
	const n = 8
	for gy := 1; gy < n; gy++ {
		for gx := 1; gx < n; gx++ {
			x := b.X0 + (float64(gx)+0.5)/float64(n)*(b.X1-b.X0)
			y := b.Y0 + (float64(gy)+0.5)/float64(n)*(b.Y1-b.Y0)
			zs := p.RayIntersections(x, y)
			w, boundary := windings(zs)
			if boundary {
				continue // probe landed on a contour, classification is ambiguous
			}
			if NonZero.Fills(w) != EvenOdd.Fills(w) {
				return fmt.Sprintf("probe (%v,%v): winding %d fills under NonZero but not EvenOdd", x, y, w), false
			}
		}
	}
	return "", true
}

func gateFillCounts(p *Path) (filling, holes int) {
	for _, f := range p.Filling(NonZero) {
		if f {
			filling++
		} else {
			holes++
		}
	}
	return
}

func gateIntersectionsSorted(zs []Point) (int, bool) {
	for i := 1; i < len(zs); i++ {
		if Equal(zs[i-1].X, zs[i].X) {
			if zs[i].Y < zs[i-1].Y && !Equal(zs[i].Y, zs[i-1].Y) {
				return i, false
			}
		} else if zs[i].X < zs[i-1].X {
			return i, false
		}
	}
	return -1, true
}

// --- fault injectors ---

// gateCorruptUnclose strips the close command of every subpath (keeps geometry).
func gateCorruptUnclose(r *Path) *Path {
	r = r.Copy()
	for i := 0; i < len(r.d); {
		if r.d[i] == CloseCmd {
			r.d[i] = LineToCmd
			r.d[i+cmdLen(CloseCmd)-1] = LineToCmd
		}
		i += cmdLen(r.d[i])
	}
	return r
}

// gateCorruptNaN injects a NaN into the first coordinate.
func gateCorruptNaN(r *Path) *Path {
	if r.Empty() {
		return r
	}
	r = r.Copy()
	r.d[1] = math.NaN()
	return r
}

// gateCorruptReverse flips the winding of the whole result.
func gateCorruptReverse(r *Path) *Path {
	return r.Reverse()
}

// --- gate runner ---

func runBooleanGate(samples []gateSample, faults *gateFaults) gateReportJSON {
	limits := gateDefaultLimits
	if faults != nil && faults.epsilon > 0.0 {
		orig := BentleyOttmannEpsilon
		BentleyOttmannEpsilon = faults.epsilon
		defer func() { BentleyOttmannEpsilon = orig }()
	}

	report := gateReportJSON{
		Version:               1,
		FillRule:              NonZero.String(),
		BentleyOttmannEpsilon: BentleyOttmannEpsilon,
		Limits:                limits,
	}
	gateStart := time.Now()

	for _, sample := range samples {
		sr := gateSampleJSON{ID: sample.id, Category: sample.category}
		sampleStart := time.Now()
		addCheck := func(stage, name string, pass bool, detail string) {
			sr.Checks = append(sr.Checks, gateCheckJSON{Stage: stage, Check: name, Pass: pass, Detail: detail})
		}

		p := MustParseSVGPath(sample.p)
		q := MustParseSVGPath(sample.q)
		sr.SegmentsP = gateSegmentCount(p)
		sr.SegmentsQ = gateSegmentCount(q)

		// --- stage: scan ---
		zs := p.Intersections(q)
		if faults != nil && faults.scrambleScanOrder && 1 < len(zs) {
			for i, j := 0, len(zs)-1; i < j; i, j = i+1, j-1 {
				zs[i], zs[j] = zs[j], zs[i]
			}
		}
		sr.Intersections = len(zs)
		if idx, ok := gateIntersectionsSorted(zs); !ok {
			addCheck("scan", "intersections-sorted", false, fmt.Sprintf("intersection %d of %d out of order: %v", idx, len(zs), zs))
		} else {
			addCheck("scan", "intersections-sorted", true, "")
		}
		nanZ := false
		for _, z := range zs {
			if math.IsNaN(z.X) || math.IsNaN(z.Y) || math.IsInf(z.X, 0) || math.IsInf(z.Y, 0) {
				nanZ = true
			}
		}
		addCheck("scan", "intersections-finite", !nanZ, fmt.Sprintf("%d intersections", len(zs)))

		// --- stage: settle ---
		ps := p.Settle(NonZero)
		qs := q.Settle(NonZero)
		for _, sp := range []struct {
			name string
			path *Path
		}{{"p", ps}, {"q", qs}} {
			if idx, ok := gateNoNaN(sp.path); !ok {
				addCheck("settle", "no-nan-"+sp.name, false, fmt.Sprintf("NaN/Inf at d[%d]", idx))
			} else {
				addCheck("settle", "no-nan-"+sp.name, true, "")
			}
			addCheck("settle", "closed-"+sp.name, gateAllSubpathsClosed(sp.path), "")
			if detail, ok := gateOrientationOK(sp.path); !ok {
				addCheck("settle", "winding-"+sp.name, false, detail)
			} else {
				addCheck("settle", "winding-"+sp.name, true, "")
			}
			if detail, ok := gateFillRuleAgree(sp.path); !ok {
				addCheck("settle", "fillrule-"+sp.name, false, detail)
			} else {
				addCheck("settle", "fillrule-"+sp.name, true, "")
			}
		}
		sr.AreaP = gateAreaFilled(ps, limits)
		sr.AreaQ = gateAreaFilled(qs, limits)
		tol := limits.MaxAreaRelErr*(sr.AreaP+sr.AreaQ) + limits.AreaAbsTol
		sr.Tolerance = tol

		// --- stages: boolean + winding, per op ---
		areas := map[string]float64{}
		for _, op := range gateOps {
			or := gateOpJSON{Op: op.name}
			start := time.Now()
			r := op.fn(p, q)
			or.DurationMicros = time.Since(start).Microseconds()
			if faults != nil && faults.corruptResult != nil {
				r = faults.corruptResult(op.name, r)
			}
			or.Segments = gateSegmentCount(r)

			addOpCheck := func(stage, name string, pass bool, detail string) {
				or.Checks = append(or.Checks, gateCheckJSON{Stage: stage, Check: name, Pass: pass, Detail: detail})
			}

			// boolean stage: structural integrity of the raw result
			idx, finite := gateNoNaN(r)
			addOpCheck("boolean", "no-nan", finite, func() string {
				if !finite {
					return fmt.Sprintf("NaN/Inf at d[%d]", idx)
				}
				return ""
			}())
			closed := gateAllSubpathsClosed(r)
			addOpCheck("boolean", "closed", closed, "")
			addOpCheck("boolean", "segment-limit", or.Segments <= limits.MaxResultSegments,
				fmt.Sprintf("%d segments (limit %d)", or.Segments, limits.MaxResultSegments))

			if finite && closed {
				// winding stage: direction, fill-rule agreement and area
				or.FillingPaths, or.Holes = gateFillCounts(r)
				if detail, ok := gateOrientationOK(r); !ok {
					addOpCheck("winding", "orientation", false, detail)
				} else {
					addOpCheck("winding", "orientation", true, "")
				}
				if detail, ok := gateFillRuleAgree(r); !ok {
					addOpCheck("winding", "fillrule-agree", false, detail)
				} else {
					addOpCheck("winding", "fillrule-agree", true, "")
				}
				or.Area = gateAreaFilled(r, limits)
				areas[op.name] = or.Area
				if want, ok := sample.areas[op.name]; ok {
					err := math.Abs(or.Area - want)
					or.AreaErr = &err
					sr.AreaErrMax = math.Max(sr.AreaErrMax, err)
					addOpCheck("winding", "area-analytic", err <= tol,
						fmt.Sprintf("area %v, want %v, err %v, tol %v", or.Area, want, err, tol))
				}
			}
			sr.Ops = append(sr.Ops, or)
		}

		// --- winding stage: cross-op area identities (catch hairline cracks) ---
		if len(areas) == len(gateOps) {
			identities := []struct {
				name string
				got  float64
				want float64
			}{
				{"and+xor=or", areas["AND"] + areas["XOR"], areas["OR"]},
				{"not+and=area_p", areas["NOT"] + areas["AND"], sr.AreaP},
				{"or+and=area_p+area_q", areas["OR"] + areas["AND"], sr.AreaP + sr.AreaQ},
				{"div=area_p", areas["DIV"], sr.AreaP},
			}
			for _, id := range identities {
				err := math.Abs(id.got - id.want)
				sr.AreaErrMax = math.Max(sr.AreaErrMax, err)
				addCheck("winding", "identity:"+id.name, err <= tol,
					fmt.Sprintf("got %v, want %v, err %v, tol %v", id.got, id.want, err, tol))
			}
		}

		// --- resource limits ---
		sr.DurationMicros = time.Since(sampleStart).Microseconds()
		durationDetail := ""
		if limits.MaxSampleDurationMicros < sr.DurationMicros {
			durationDetail = fmt.Sprintf("%d us exceeds limit %d us", sr.DurationMicros, limits.MaxSampleDurationMicros)
		}
		addCheck("resource", "duration-limit", sr.DurationMicros <= limits.MaxSampleDurationMicros, durationDetail)

		sr.Pass = true
		for _, c := range sr.Checks {
			if !c.Pass {
				sr.Pass = false
			}
		}
		for _, op := range sr.Ops {
			for _, c := range op.Checks {
				if !c.Pass {
					sr.Pass = false
				}
			}
		}
		if sr.Pass {
			report.Passed++
		} else {
			report.Failed++
		}
		report.Samples = append(report.Samples, sr)
	}
	report.DurationMicros = time.Since(gateStart).Microseconds()
	return report
}

func gateReportFailures(report gateReportJSON) string {
	s := ""
	for _, sample := range report.Samples {
		for _, c := range sample.Checks {
			if !c.Pass {
				s += fmt.Sprintf("  %s [%s] %s: %s\n", sample.ID, c.Stage, c.Check, c.Detail)
			}
		}
		for _, op := range sample.Ops {
			for _, c := range op.Checks {
				if !c.Pass {
					s += fmt.Sprintf("  %s %s [%s] %s: %s\n", sample.ID, op.Op, c.Stage, c.Check, c.Detail)
				}
			}
		}
	}
	return s
}

// --- tests ---

func TestPathBooleanGate(t *testing.T) {
	report := runBooleanGate(gateSamples, nil)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("boolean gate report:\n%s", data)
	if path := os.Getenv("CANVAS_GATE_REPORT"); path != "" {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Error(err)
		}
	}

	if report.Failed != 0 {
		t.Errorf("gate failed for %d of %d samples:\n%s", report.Failed, len(report.Samples), gateReportFailures(report))
	}
}

func TestPathBooleanGateFaultInjection(t *testing.T) {
	// Use a representative subset so the injection runs stay fast.
	subset := []gateSample{gateSamples[0], gateSamples[1], gateSamples[5], gateSamples[13]}

	expectFailure := func(name string, faults *gateFaults, wantStage string) {
		t.Run(name, func(t *testing.T) {
			report := runBooleanGate(subset, faults)
			if report.Failed == 0 {
				t.Fatalf("injection %q did not trip the gate", name)
			}
			found := false
			for _, sample := range report.Samples {
				for _, c := range sample.Checks {
					if !c.Pass && c.Stage == wantStage {
						found = true
					}
				}
				for _, op := range sample.Ops {
					for _, c := range op.Checks {
						if !c.Pass && c.Stage == wantStage {
							found = true
						}
					}
				}
			}
			if !found {
				t.Errorf("injection %q: no failing check at stage %q:\n%s", name, wantStage, gateReportFailures(report))
			}
			t.Logf("injection %q tripped the gate as expected:\n%s", name, gateReportFailures(report))
		})
	}

	// 1. tolerance perturbation: a snap grid of 1.0 destroys sub-unit precision, which
	//    must surface as winding-stage area errors against the analytic references.
	expectFailure("tolerance/snap-grid-1.0", &gateFaults{epsilon: 1.0}, "winding")

	// 2. intersection-order perturbation: scrambling the scan-stage order must be
	//    caught by the sortedness check before anything downstream runs.
	expectFailure("ordering/scrambled-intersections", &gateFaults{scrambleScanOrder: true}, "scan")

	// 3. result corruption: unclosed output must be caught at the boolean stage.
	expectFailure("result/unclosed", &gateFaults{
		corruptResult: func(op string, r *Path) *Path { return gateCorruptUnclose(r) },
	}, "boolean")

	// 4. result corruption: NaN coordinates must be caught at the boolean stage.
	expectFailure("result/nan", &gateFaults{
		corruptResult: func(op string, r *Path) *Path { return gateCorruptNaN(r) },
	}, "boolean")

	// 5. result corruption: reversed winding must be caught at the winding stage.
	expectFailure("result/reversed-winding", &gateFaults{
		corruptResult: func(op string, r *Path) *Path { return gateCorruptReverse(r) },
	}, "winding")
}
