package canvas

import (
	"container/list"
	"math"
	"reflect"
	"sync"
)

// This file implements a bounded, concurrency-safe cache for dashed and stroked
// path outlines. Interactive renderers (e.g. the rasterizer) recompute the dash
// pattern and stroke outline of a path on every frame, which re-discretizes all
// Bézier curves and arcs. When the path, style and view are (nearly) unchanged
// between frames -- as is common in editors while dragging or zooming -- this is
// wasted work. The caches below key the results on a fingerprint of the path
// data, the stroke width, the flattening tolerance, the dash offset and dash
// array, the capper and joiner, and the transformation matrix, so that repeated
// renders reuse the previously computed outline.
//
// Two levels are cached:
//   - dashStrokeCache: the dashed and stroked outline in path coordinates. This
//     survives view (transformation) changes, which is the common case when
//     panning or zooming.
//   - dashTransformCache: the final outline after applying the transformation.
//     This makes exactly repeated renders (no changes at all) nearly free.
//
// Correctness notes:
//   - The path fingerprint is recomputed from the raw path data on every lookup,
//     so any mutation of the path (append, Reset, direct Data() writes, ...)
//     automatically invalidates the cached entries. There is no explicit
//     invalidation protocol to keep in sync.
//   - Non-finite parameters (NaN/Inf) bypass the cache entirely, as do
//     non-comparable custom Capper/Joiner implementations.
//   - Both caches are bounded LRU caches, so memory use is capped regardless of
//     how many distinct paths or styles are rendered.

// dashCacheCapacity is the maximum number of entries per cache. Each entry holds
// a reference to a path, so the total memory is bounded by the number of entries
// times the (shared) path sizes.
const dashCacheCapacity = 128

// fingerprintFloats returns a 64-bit FNV-1a hash over the raw bits of d. It is
// used to detect changes to path or dash array data cheaply.
func fingerprintFloats(d []float64) uint64 {
	h := uint64(14695981039346656037)
	for _, v := range d {
		h ^= math.Float64bits(v)
		h *= 1099511628211
	}
	return h
}

// finiteFloats returns false if any of the values is NaN or infinite.
func finiteFloats(fs ...float64) bool {
	for _, f := range fs {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	return true
}

// finiteMatrix returns false if any of the matrix elements is NaN or infinite.
func finiteMatrix(m Matrix) bool {
	return finiteFloats(m[0][0], m[0][1], m[0][2], m[1][0], m[1][1], m[1][2])
}

// cacheableJoinerCapper returns false if the capper or joiner is not comparable
// and thus cannot be used as (part of) a cache key. All cappers and joiners in
// this package are comparable value types.
func cacheableCapperJoiner(capper Capper, joiner Joiner) bool {
	if capper != nil && !reflect.TypeOf(capper).Comparable() {
		return false
	}
	if joiner != nil && !reflect.TypeOf(joiner).Comparable() {
		return false
	}
	return true
}

// lruCache is a bounded, concurrency-safe least-recently-used cache.
type lruCache[K comparable, V any] struct {
	mu    sync.Mutex
	items map[K]*list.Element
	ll    *list.List // front is most recently used
	cap   int
}

type lruItem[K comparable, V any] struct {
	key K
	val V
}

func newLRUCache[K comparable, V any](capacity int) *lruCache[K, V] {
	return &lruCache[K, V]{
		items: make(map[K]*list.Element),
		ll:    list.New(),
		cap:   capacity,
	}
}

func (c *lruCache[K, V]) get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(lruItem[K, V]).val, true
	}
	var zero V
	return zero, false
}

func (c *lruCache[K, V]) put(key K, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value = lruItem[K, V]{key, val}
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(lruItem[K, V]{key, val})
	c.items[key] = el
	for c.cap < c.ll.Len() {
		back := c.ll.Back()
		delete(c.items, back.Value.(lruItem[K, V]).key)
		c.ll.Remove(back)
	}
}

func (c *lruCache[K, V]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

func (c *lruCache[K, V]) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[K]*list.Element)
	c.ll.Init()
}

// dashStrokeKey identifies a dashed and stroked outline in path coordinates.
type dashStrokeKey struct {
	pathHash  uint64
	pathLen   int
	width     float64
	tolerance float64
	offset    float64
	dashHash  uint64
	dashLen   int
	capper    Capper
	joiner    Joiner
	fast      bool // value of FastStroke at computation time
}

// dashTransformKey identifies a dashed, stroked and transformed outline.
type dashTransformKey struct {
	dashStrokeKey
	m Matrix
}

// checkDashKey identifies a checkDash result.
type checkDashKey struct {
	pathHash uint64
	pathLen  int
	offset   float64
	dashHash uint64
	dashLen  int
}

type checkDashValue struct {
	dashes []float64
	ok     bool
}

var (
	dashStrokeCache    = newLRUCache[dashStrokeKey, *Path](dashCacheCapacity)
	dashTransformCache = newLRUCache[dashTransformKey, *Path](dashCacheCapacity)
	checkDashCache     = newLRUCache[checkDashKey, checkDashValue](dashCacheCapacity)
)

// ClearDashCache clears all cached dash and stroke outlines. The caches are
// bounded and invalidate automatically when paths or styles change, so this is
// only needed to release memory promptly, or in tests.
func ClearDashCache() {
	dashStrokeCache.clear()
	dashTransformCache.clear()
	checkDashCache.clear()
}

// dashStroke computes the dashed and stroked outline of p in path coordinates.
// This is the uncached reference implementation used by DashedStroke.
func dashStroke(p *Path, width float64, capper Capper, joiner Joiner, tolerance, dashOffset float64, dashes []float64) *Path {
	stroke := p
	if 0 < len(dashes) {
		dashOffset, dashes = ScaleDash(width, dashOffset, dashes)
		stroke = stroke.Dash(dashOffset, dashes...)
	}
	return stroke.Stroke(width, capper, joiner, tolerance)
}

// DashedStroke returns the outline of p after applying the dash pattern (scaled
// by the stroke width, see ScaleDash), converting it to a stroke of the given
// width, capper, joiner and tolerance, and transforming it by m. The result is
// identical to calling ScaleDash, Path.Dash, Path.Stroke and Path.Transform in
// sequence, but repeated calls with equal inputs reuse cached results.
//
// The returned path may be shared between calls and must not be modified; use
// Path.Copy first if modification is needed. Calls with non-finite dash
// offsets, dash elements or matrix elements, or with non-comparable custom
// cappers or joiners, bypass the cache. A non-finite stroke width results in an
// empty path (a non-finite width can never be stroked, see Style.HasStroke) and
// a non-finite tolerance falls back to the default Tolerance, so that
// non-finite inputs never panic or corrupt the cache.
func DashedStroke(p *Path, width float64, capper Capper, joiner Joiner, tolerance, dashOffset float64, dashes []float64, m Matrix) *Path {
	if math.IsNaN(width) || math.IsInf(width, 0) {
		return &Path{}
	}
	if math.IsNaN(tolerance) || math.IsInf(tolerance, 0) {
		tolerance = Tolerance
	}
	if capper == nil {
		capper = ButtCap
	}
	if joiner == nil {
		joiner = MiterJoin
	}

	if !finiteFloats(width, tolerance, dashOffset) || !finiteFloats(dashes...) ||
		!finiteMatrix(m) || !cacheableCapperJoiner(capper, joiner) {
		return dashStroke(p, width, capper, joiner, tolerance, dashOffset, dashes).Transform(m)
	}

	key := dashStrokeKey{
		pathHash:  fingerprintFloats(p.d),
		pathLen:   len(p.d),
		width:     width,
		tolerance: tolerance,
		offset:    dashOffset,
		dashHash:  fingerprintFloats(dashes),
		dashLen:   len(dashes),
		capper:    capper,
		joiner:    joiner,
		fast:      FastStroke,
	}
	tkey := dashTransformKey{key, m}
	if cached, ok := dashTransformCache.get(tkey); ok {
		return cached
	}

	stroke, ok := dashStrokeCache.get(key)
	if !ok {
		stroke = dashStroke(p, width, capper, joiner, tolerance, dashOffset, dashes)
		dashStrokeCache.put(key, stroke)
	}
	stroke = stroke.Copy().Transform(m)
	dashTransformCache.put(tkey, stroke)
	return stroke
}
