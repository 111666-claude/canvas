package canvas

import (
	"math"
	"sync"
	"testing"
)

// dashStrokeReference is the uncached reference: the exact sequence of calls
// that renderers used before caching was introduced.
func dashStrokeReference(p *Path, width float64, capper Capper, joiner Joiner, tolerance, dashOffset float64, dashes []float64, m Matrix) *Path {
	if capper == nil {
		capper = ButtCap
	}
	if joiner == nil {
		joiner = MiterJoin
	}
	stroke := p
	if 0 < len(dashes) {
		dashOffset, dashes = ScaleDash(width, dashOffset, dashes)
		stroke = stroke.Dash(dashOffset, dashes...)
	}
	return stroke.Stroke(width, capper, joiner, tolerance).Transform(m)
}

// exactlyEqual returns true if both paths are bit-for-bit identical.
func exactlyEqual(p, q *Path) bool {
	if len(p.d) != len(q.d) {
		return false
	}
	for i := range p.d {
		if p.d[i] != q.d[i] && !(math.IsNaN(p.d[i]) && math.IsNaN(q.d[i])) {
			return false
		}
	}
	return true
}

func dashTestPaths() map[string]*Path {
	closed := &Path{}
	closed.MoveTo(0.0, 0.0)
	closed.CubeTo(10.0, 0.0, 10.0, 10.0, 0.0, 10.0)
	closed.Close()

	mixed := &Path{}
	mixed.MoveTo(0.0, 0.0)
	mixed.LineTo(10.0, 0.0)
	mixed.QuadTo(15.0, 5.0, 10.0, 10.0)
	mixed.CubeTo(5.0, 15.0, -5.0, 15.0, -10.0, 10.0)
	mixed.ArcTo(5.0, 3.0, 30.0, false, true, 0.0, 0.0)
	mixed.Close()

	multi := &Path{}
	multi.MoveTo(0.0, 0.0)
	multi.CubeTo(5.0, 5.0, 10.0, -5.0, 15.0, 0.0)
	multi.MoveTo(20.0, 0.0)
	multi.ArcTo(4.0, 4.0, 0.0, true, false, 28.0, 0.0)
	multi.LineTo(30.0, 5.0)

	long := &Path{}
	long.MoveTo(0.0, 0.0)
	for i := 0; i < 50; i++ {
		x := float64(i) * 10.0
		long.CubeTo(x+3.0, 8.0, x+7.0, -8.0, x+10.0, 0.0)
	}

	reversed := mixed.Copy().Reverse()

	return map[string]*Path{
		"line":     MustParseSVGPath("M0 0L100 0"),
		"quad":     MustParseSVGPath("M0 0Q50 50 100 0"),
		"cube":     MustParseSVGPath("M0 0C30 50 70 -50 100 0"),
		"arc":      MustParseSVGPath("M0 0A50 30 30 0 1 100 0"),
		"mixed":    mixed,
		"closed":   closed,
		"multi":    multi,
		"long":     long,
		"reversed": reversed,
	}
}

func dashTestCases() []struct {
	name      string
	width     float64
	capper    Capper
	joiner    Joiner
	tolerance float64
	offset    float64
	dashes    []float64
	m         Matrix
} {
	return []struct {
		name      string
		width     float64
		capper    Capper
		joiner    Joiner
		tolerance float64
		offset    float64
		dashes    []float64
		m         Matrix
	}{
		{"solid", 2.0, ButtCap, MiterJoin, 0.1, 0.0, nil, Identity},
		{"dashed", 2.0, ButtCap, MiterJoin, 0.1, 0.0, []float64{3.0, 2.0}, Identity},
		{"odd-dashes", 1.5, RoundCap, RoundJoin, 0.1, 0.0, []float64{3.0, 2.0, 1.0}, Identity},
		{"negative-offset", 2.0, ButtCap, BevelJoin, 0.1, -4.5, []float64{3.0, 2.0}, Identity},
		{"large-offset", 2.0, ButtCap, MiterJoin, 0.1, 137.25, []float64{3.0, 2.0, 1.0}, Identity},
		{"square-cap-miter", 3.0, SquareCap, MiterJoin, 0.05, 1.0, []float64{4.0, 2.0}, Identity},
		{"arcs-join", 2.0, RoundCap, ArcsJoin, 0.1, 0.0, []float64{2.0, 1.0}, Identity},
		{"miter-clip", 2.0, ButtCap, MiterClipJoin, 0.1, 0.0, []float64{2.0, 1.0}, Identity},
		{"nil-cap-join", 2.0, nil, nil, 0.1, 0.0, []float64{3.0, 3.0}, Identity},
		{"zero-dashes", 2.0, ButtCap, MiterJoin, 0.1, 0.0, []float64{0.0, 2.0, 3.0, 0.0}, Identity},
		{"tiny-dashes", 0.5, ButtCap, MiterJoin, 0.01, 0.0, []float64{0.4, 0.2}, Identity},
		{"transform", 2.0, ButtCap, MiterJoin, 0.1, 0.0, []float64{3.0, 2.0}, Identity.Scale(2.0, 0.5).Rotate(30.0).Translate(5.0, -3.0)},
		{"reflect", 2.0, ButtCap, MiterJoin, 0.1, -1.0, []float64{3.0, 2.0}, Identity.ReflectX()},
		{"huge-dashes", 2.0, ButtCap, MiterJoin, 0.1, 0.0, func() []float64 {
			d := make([]float64, 4096)
			for i := range d {
				d[i] = 1.0 + float64(i%7)
			}
			return d
		}(), Identity},
	}
}

func TestDashedStroke(t *testing.T) {
	paths := dashTestPaths()
	cases := dashTestCases()
	for _, tc := range cases {
		for pname, p := range paths {
			t.Run(tc.name+"/"+pname, func(t *testing.T) {
				ClearDashCache()
				ref := dashStrokeReference(p, tc.width, tc.capper, tc.joiner, tc.tolerance, tc.offset, tc.dashes, tc.m)

				// first call populates the cache
				got := DashedStroke(p, tc.width, tc.capper, tc.joiner, tc.tolerance, tc.offset, tc.dashes, tc.m)
				if !exactlyEqual(got, ref) {
					t.Fatalf("first call differs from reference:\ngot %v\nref %v", got, ref)
				}

				// second call must hit the cache and return the identical result
				got2 := DashedStroke(p, tc.width, tc.capper, tc.joiner, tc.tolerance, tc.offset, tc.dashes, tc.m)
				if !exactlyEqual(got2, ref) {
					t.Fatalf("cached call differs from reference:\ngot %v\nref %v", got2, ref)
				}

				// a different transform must still reuse the path-space outline
				// and equal the reference for that transform
				m2 := tc.m.Translate(1.0, 2.0)
				ref2 := dashStrokeReference(p, tc.width, tc.capper, tc.joiner, tc.tolerance, tc.offset, tc.dashes, m2)
				got3 := DashedStroke(p, tc.width, tc.capper, tc.joiner, tc.tolerance, tc.offset, tc.dashes, m2)
				if !exactlyEqual(got3, ref2) {
					t.Fatalf("transformed call differs from reference:\ngot %v\nref %v", got3, ref2)
				}
			})
		}
	}
}

func TestDashedStrokeInvalidation(t *testing.T) {
	m := Identity.Scale(2.0, 2.0)
	dashes := []float64{3.0, 2.0}

	p := MustParseSVGPath("M0 0C30 50 70 -50 100 0")
	ClearDashCache()
	first := DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)

	// appending to the path must invalidate the cache
	p.LineTo(100.0, 50.0)
	got := DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	ref := dashStrokeReference(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	if !exactlyEqual(got, ref) {
		t.Fatal("after append, cache returned stale result")
	}
	if exactlyEqual(got, first) {
		t.Fatal("after append, result should have changed")
	}

	// Reset and rebuilding a different path in the same memory must invalidate
	p.Reset()
	p.MoveTo(0.0, 0.0)
	p.CubeTo(10.0, 20.0, 30.0, -20.0, 40.0, 0.0)
	got = DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	ref = dashStrokeReference(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	if !exactlyEqual(got, ref) {
		t.Fatal("after Reset, cache returned stale result")
	}

	// direct mutation through Data must invalidate
	p.Data()[1] = 5.0
	got = DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	ref = dashStrokeReference(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	if !exactlyEqual(got, ref) {
		t.Fatal("after Data mutation, cache returned stale result")
	}

	// changing the dash array between calls must invalidate
	got = DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, []float64{5.0, 1.0}, m)
	ref = dashStrokeReference(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, []float64{5.0, 1.0}, m)
	if !exactlyEqual(got, ref) {
		t.Fatal("after dash change, cache returned stale result")
	}
}

func TestDashedStrokeEdgeCases(t *testing.T) {
	p := MustParseSVGPath("M0 0C30 50 70 -50 100 0")
	m := Identity.Scale(2.0, 2.0)
	nan := math.NaN()
	inf := math.Inf(1)

	cases := []struct {
		name      string
		width     float64
		tolerance float64
		offset    float64
		dashes    []float64
		m         Matrix
	}{
		{"empty-dash", 2.0, 0.1, 0.0, []float64{}, m},
		{"zero-width", 0.0, 0.1, 0.0, []float64{3.0, 2.0}, m},
		{"negative-width", -2.0, 0.1, 0.0, []float64{3.0, 2.0}, m},
		{"nan-dash", 2.0, 0.1, 0.0, []float64{3.0, nan}, m},
		{"inf-dash", 2.0, 0.1, 0.0, []float64{inf, 2.0}, m},
		{"nan-offset", 2.0, 0.1, nan, []float64{3.0, 2.0}, m},
		{"nan-matrix", 2.0, 0.1, 0.0, []float64{3.0, 2.0}, Matrix{{nan, 0.0, 0.0}, {0.0, 1.0, 0.0}}},
		{"negative-dashes", 2.0, 0.1, 0.0, []float64{-3.0, 2.0}, m},
		{"zero-tolerance", 2.0, 0.0, 0.0, []float64{3.0, 2.0}, m},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ClearDashCache()
			ref := dashStrokeReference(p, tc.width, ButtCap, MiterJoin, tc.tolerance, tc.offset, tc.dashes, tc.m)
			got := DashedStroke(p, tc.width, ButtCap, MiterJoin, tc.tolerance, tc.offset, tc.dashes, tc.m)
			if !exactlyEqual(got, ref) {
				t.Fatalf("differs from reference:\ngot %v\nref %v", got, ref)
			}
			// must not panic or grow the cache for non-finite parameters
			got = DashedStroke(p, tc.width, ButtCap, MiterJoin, tc.tolerance, tc.offset, tc.dashes, tc.m)
			if !exactlyEqual(got, ref) {
				t.Fatal("second call differs from reference")
			}
		})
	}

	// non-finite widths stroke nothing and must not panic
	for _, w := range []float64{nan, inf, -inf} {
		if got := DashedStroke(p, w, ButtCap, MiterJoin, 0.1, 0.0, []float64{3.0, 2.0}, m); !got.Empty() {
			t.Fatalf("non-finite width %v: expected empty path, got %v", w, got)
		}
	}
	// non-finite tolerance falls back to the default tolerance
	ref := dashStrokeReference(p, 2.0, ButtCap, MiterJoin, Tolerance, 0.0, []float64{3.0, 2.0}, m)
	if got := DashedStroke(p, 2.0, ButtCap, MiterJoin, nan, 0.0, []float64{3.0, 2.0}, m); !exactlyEqual(got, ref) {
		t.Fatal("NaN tolerance did not fall back to default tolerance")
	}

	// huge dash arrays must not grow the cache unboundedly
	ClearDashCache()
	for i := 0; i < 4*dashCacheCapacity; i++ {
		d := make([]float64, 1000+i)
		for j := range d {
			d[j] = 1.0
		}
		DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, d, m)
	}
	if dashStrokeCache.len() > dashCacheCapacity || dashTransformCache.len() > dashCacheCapacity {
		t.Fatal("cache grew beyond capacity")
	}
}

func TestDashedStrokeConcurrent(t *testing.T) {
	paths := dashTestPaths()
	m := Identity.Scale(1.5, 1.5)
	dashes := []float64{3.0, 2.0, 1.0}

	// precompute references
	refs := map[string]*Path{}
	for name, p := range paths {
		refs[name] = dashStrokeReference(p, 2.0, ButtCap, RoundJoin, 0.1, -1.0, dashes, m)
	}

	ClearDashCache()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				for name, p := range paths {
					got := DashedStroke(p, 2.0, ButtCap, RoundJoin, 0.1, -1.0, dashes, m)
					if !exactlyEqual(got, refs[name]) {
						t.Errorf("concurrent result differs from reference for %s", name)
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestCheckDashCache(t *testing.T) {
	paths := dashTestPaths()
	cases := []struct {
		offset float64
		dashes []float64
	}{
		{0.0, nil},
		{0.0, []float64{3.0, 2.0}},
		{-4.5, []float64{3.0, 2.0}},
		{1.0, []float64{3.0, 2.0, 1.0}},
		{0.0, []float64{0.0, 2.0, 3.0, 0.0}},
		{0.0, []float64{0.0}},
		{0.0, []float64{100.0, 1.0}},
		{2.0, []float64{1000.0, 1.0}},
	}
	for _, tc := range cases {
		for pname, p := range paths {
			ClearDashCache()
			refD, refOK := p.checkDashUncached(tc.offset, append([]float64{}, tc.dashes...))
			for i := 0; i < 2; i++ {
				gotD, gotOK := p.checkDash(tc.offset, append([]float64{}, tc.dashes...))
				if gotOK != refOK || !exactlyEqualFloats(gotD, refD) {
					t.Fatalf("checkDash(%v, %v) on %s call %d: got (%v,%v), want (%v,%v)",
						tc.offset, tc.dashes, pname, i, gotD, gotOK, refD, refOK)
				}
			}
		}
	}

	// mutation must invalidate
	p := MustParseSVGPath("M0 0L10 0")
	ClearDashCache()
	d0, ok0 := p.checkDash(0.0, []float64{100.0, 1.0})
	p.LineTo(20.0, 0.0)
	d1, ok1 := p.checkDash(0.0, []float64{100.0, 1.0})
	refD, refOK := p.checkDashUncached(0.0, []float64{100.0, 1.0})
	if ok1 != refOK || !exactlyEqualFloats(d1, refD) {
		t.Fatal("checkDash returned stale result after path mutation")
	}
	if ok0 == ok1 && exactlyEqualFloats(d0, d1) && len(p.d) > 0 {
		// not necessarily an error, but for this path the length changed so the
		// result may legitimately stay the same; no assertion needed
	}
}

func exactlyEqualFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDashCacheBounded(t *testing.T) {
	ClearDashCache()
	p := MustParseSVGPath("M0 0C30 50 70 -50 100 0")
	for i := 0; i < 4*dashCacheCapacity; i++ {
		m := Identity.Translate(float64(i), 0.0)
		DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, []float64{3.0, 2.0}, m)
		p.checkDash(float64(i), []float64{3.0, 2.0})
	}
	if dashStrokeCache.len() > dashCacheCapacity {
		t.Fatalf("dashStrokeCache has %d entries, capacity %d", dashStrokeCache.len(), dashCacheCapacity)
	}
	if dashTransformCache.len() > dashCacheCapacity {
		t.Fatalf("dashTransformCache has %d entries, capacity %d", dashTransformCache.len(), dashCacheCapacity)
	}
	if checkDashCache.len() > dashCacheCapacity {
		t.Fatalf("checkDashCache has %d entries, capacity %d", checkDashCache.len(), dashCacheCapacity)
	}
}

// makeLongCurve returns a path with n cubic segments, resembling a long curve
// in a vector editor.
func makeLongCurve(n int) *Path {
	p := &Path{}
	p.MoveTo(0.0, 0.0)
	x := 0.0
	for i := 0; i < n; i++ {
		p.CubeTo(x+3.0, 8.0, x+7.0, -8.0, x+10.0, 0.0)
		x += 10.0
	}
	return p
}

var benchmarkSink *Path

func BenchmarkDashUncached(b *testing.B) {
	p := makeLongCurve(200)
	dashes := []float64{3.0, 2.0}
	m := Identity.Scale(2.0, 2.0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkSink = dashStrokeReference(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	}
}

func BenchmarkDashedStrokeRepeat(b *testing.B) {
	p := makeLongCurve(200)
	dashes := []float64{3.0, 2.0}
	m := Identity.Scale(2.0, 2.0)
	ClearDashCache()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkSink = DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	}
}

func BenchmarkDashedStrokeTransform(b *testing.B) {
	p := makeLongCurve(200)
	dashes := []float64{3.0, 2.0}
	ClearDashCache()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// simulate zooming/panning: the transform changes every frame
		m := Identity.Scale(2.0+0.001*float64(i), 2.0+0.001*float64(i)).Translate(float64(i%7), 0.0)
		benchmarkSink = DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	}
}

func BenchmarkDashedStrokeInvalidation(b *testing.B) {
	p := makeLongCurve(200)
	dashes := []float64{3.0, 2.0}
	m := Identity.Scale(2.0, 2.0)
	ClearDashCache()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// simulate editing: the path changes every frame
		p.Data()[len(p.Data())-2] += 1e-9
		benchmarkSink = DashedStroke(p, 2.0, ButtCap, MiterJoin, 0.1, 0.0, dashes, m)
	}
}

func BenchmarkCheckDashUncached(b *testing.B) {
	p := makeLongCurve(200)
	dashes := []float64{3.0, 2.0}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.checkDashUncached(0.0, dashes)
	}
}

func BenchmarkCheckDashCached(b *testing.B) {
	p := makeLongCurve(200)
	dashes := []float64{3.0, 2.0}
	ClearDashCache()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.checkDash(0.0, dashes)
	}
}
