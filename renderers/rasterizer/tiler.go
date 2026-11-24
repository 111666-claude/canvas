package rasterizer

import (
	"context"
	"image"
	"math"
	"sync"

	"golang.org/x/image/draw"

	"github.com/tdewolff/canvas"
)

// Tiler renders a canvas into reusable, fixed-size tiles instead of one
// full-canvas image. Only tiles intersecting the requested viewport are
// rendered, and rendered tiles are cached so that repeated Render calls and
// moving viewports only redraw dirty tiles. Tiles are stored in their final
// (gamma-compressed) color space and are safe for concurrent read-only access
// via Tile while a Render is in progress.
//
// Every tile is produced with RenderTile from the same virtual full-canvas
// coordinate system, so stitching the cached tiles together reproduces the
// full-canvas Rasterizer output pixel for pixel.
type Tiler struct {
	mu sync.RWMutex

	c            *canvas.Canvas
	resolution   canvas.Resolution
	colorSpace   canvas.ColorSpace
	op           draw.Op
	tileW, tileH int

	fullW, fullH int // virtual full-canvas raster size in pixels

	// cache maps tile grid coordinates (column, row) to a rendered tile.
	cache map[tilerKey]*cacheEntry
	// dirty holds tiles whose cached pixels must be repainted.
	dirty map[tilerKey]struct{}
	// lru lists tile keys from least to most recently used.
	lru []tilerKey

	maxTiles int // maximum number of cached tiles; 0 means unlimited
	maxBytes int // maximum cached pixel bytes; 0 means unlimited
	maxPx    int // hard per-tile pixel limit
	stats    TilerStats

	closed bool
}

type tilerKey struct {
	col, row int
}

type cacheEntry struct {
	img *image.RGBA
	// rect is the tile rectangle in virtual full-canvas pixel coordinates.
	rect image.Rectangle
}

// TilerStats reports Tiler resource usage and rendering counters.
type TilerStats struct {
	// DirtyTiles is the number of tiles repainted by the last Render call.
	DirtyTiles int
	// CachedTiles is the number of tiles currently held in the cache.
	CachedTiles int
	// PeakTiles is the high-water mark of concurrently cached tiles.
	PeakTiles int
	// CachedBytes is the current cache footprint in bytes (RGBA pixels).
	CachedBytes int
	// PeakBytes is the high-water mark of the cache footprint in bytes.
	PeakBytes int
	// Evictions is the cumulative number of tiles evicted by the memory limit.
	Evictions int
}

// TilerOption configures a Tiler.
type TilerOption func(*Tiler)

// WithDrawOp sets the draw operation used while painting (draw.Over, the
// default, or draw.Src). It mirrors Rasterizer.SetOp.
func WithDrawOp(op draw.Op) TilerOption {
	return func(t *Tiler) { t.op = op }
}

// WithMaxCachedTiles limits the number of cached tiles. When the limit is
// reached, least-recently-used tiles are evicted. A non-positive value removes
// the limit.
func WithMaxCachedTiles(n int) TilerOption {
	return func(t *Tiler) { t.maxTiles = n }
}

// WithMaxCacheBytes limits the cache footprint to approximately n bytes of
// RGBA pixels. A non-positive value removes the limit.
func WithMaxCacheBytes(n int) TilerOption {
	return func(t *Tiler) { t.maxBytes = n }
}

// WithMaxTilePixels overrides the hard safety limit on a single tile's pixel
// count. Tiles above the limit are rejected with ErrTileTooLarge rather than
// allocated.
func WithMaxTilePixels(n int) TilerOption {
	return func(t *Tiler) {
		if 0 < n {
			t.maxPx = n
		}
	}
}

// DefaultTilerCacheTiles is the default cap on cached tiles (roughly a 16x16
// grid).
const DefaultTilerCacheTiles = 256

// NewTiler creates a reusable tile renderer for the canvas. tileWidth and
// tileHeight are the tile dimensions in pixels. The virtual full-canvas raster
// size is derived from the canvas size and resolution, exactly as
// rasterizer.Draw computes it.
func NewTiler(c *canvas.Canvas, tileWidth, tileHeight int, resolution canvas.Resolution, colorSpace canvas.ColorSpace, opts ...TilerOption) (*Tiler, error) {
	if c == nil {
		return nil, ErrEmptyViewport
	}
	if tileWidth <= 0 || tileHeight <= 0 {
		return nil, ErrEmptyViewport
	}
	dpmm := resolution.DPMM()
	fullWf := c.W * dpmm
	fullHf := c.H * dpmm
	if fullWf <= 0 || fullHf <= 0 || fullWf > float64(math.MaxInt32) || fullHf > float64(math.MaxInt32) {
		return nil, ErrTileTooLarge
	}
	fullW := int(fullWf + 0.5)
	fullH := int(fullHf + 0.5)
	if fullW <= 0 || fullH <= 0 {
		return nil, ErrEmptyViewport
	}
	if colorSpace == nil {
		colorSpace = canvas.DefaultColorSpace
	}
	t := &Tiler{
		c:          c,
		resolution: resolution,
		colorSpace: colorSpace,
		op:         draw.Over,
		tileW:      tileWidth,
		tileH:      tileHeight,
		fullW:      fullW,
		fullH:      fullH,
		cache:      map[tilerKey]*cacheEntry{},
		dirty:      map[tilerKey]struct{}{},
		maxTiles:   DefaultTilerCacheTiles,
		maxPx:      maxTilePixels,
	}
	for _, opt := range opts {
		opt(t)
	}
	if err := t.checkTileSize(tileWidth, tileHeight); err != nil {
		return nil, err
	}
	// a nominal tile that alone exceeds the byte budget can never be cached
	if t.maxBytes > 0 && tileWidth*tileHeight*4 > t.maxBytes {
		return nil, ErrTileTooLarge
	}
	// The per-tile renderer keeps full-width backing rows (virtualW*tileH
	// pixels) plus a full-height cellIndex. Reject dimensions whose backing
	// allocation would overflow, so an oversized canvas degrades to an error
	// instead of panicking inside image.NewRGBA.
	if math.MaxInt32/fullW < tileHeight || fullW*tileHeight > maxBackingPixels {
		return nil, ErrTileTooLarge
	}
	// No upfront grid bookkeeping: an uncached tile is rendered on demand by
	// Render, and only tiles that are already cached can become dirty. This
	// keeps construction O(1) even for a very large virtual canvas.
	return t, nil
}

func (t *Tiler) cols() int {
	return (t.fullW + t.tileW - 1) / t.tileW
}

func (t *Tiler) rows() int {
	return (t.fullH + t.tileH - 1) / t.tileH
}

func (t *Tiler) checkTileSize(w, h int) error {
	if w <= 0 || h <= 0 {
		return ErrEmptyViewport
	}
	if w > math.MaxInt32 || h > math.MaxInt32 || math.MaxInt32/w < h {
		return ErrTileTooLarge
	}
	if t.maxPx > 0 && w*h > t.maxPx {
		return ErrTileTooLarge
	}
	return nil
}

// FullSize returns the virtual full-canvas raster dimensions in pixels.
func (t *Tiler) FullSize() (int, int) {
	return t.fullW, t.fullH
}

// TileSize returns the configured tile dimensions in pixels.
func (t *Tiler) TileSize() (int, int) {
	return t.tileW, t.tileH
}

// tileRect returns the pixel rectangle (in virtual coordinates) of a grid
// tile, clipped to the full-canvas raster. Edge tiles may be smaller than the
// configured tile size.
func (t *Tiler) tileRect(col, row int) image.Rectangle {
	x0 := col * t.tileW
	y0 := row * t.tileH
	x1 := x0 + t.tileW
	y1 := y0 + t.tileH
	if x1 > t.fullW {
		x1 = t.fullW
	}
	if y1 > t.fullH {
		y1 = t.fullH
	}
	return image.Rect(x0, y0, x1, y1)
}

// MarkDirty marks every tile intersecting rect (a pixel rectangle in virtual
// full-canvas coordinates) as needing repainting on the next Render. An empty
// rectangle is ignored. Call this after mutating the canvas within a region.
func (t *Tiler) MarkDirty(rect image.Rectangle) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.markDirtyLocked(rect)
}

// MarkAllDirty forces every currently cached tile to be repainted on the next
// Render. Tiles that are not cached are rendered on demand anyway.
func (t *Tiler) MarkAllDirty() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key := range t.cache {
		t.dirty[key] = struct{}{}
	}
}

func (t *Tiler) markDirtyLocked(rect image.Rectangle) {
	if rect.Empty() {
		return
	}
	col0 := clamp(rect.Min.X/t.tileW, 0, t.cols()-1)
	col1 := clamp((rect.Max.X-1)/t.tileW, 0, t.cols()-1)
	row0 := clamp(rect.Min.Y/t.tileH, 0, t.rows()-1)
	row1 := clamp((rect.Max.Y-1)/t.tileH, 0, t.rows()-1)
	for col := col0; col <= col1; col++ {
		for row := row0; row <= row1; row++ {
			t.dirty[tilerKey{col, row}] = struct{}{}
		}
	}
}

// Render paints all tiles intersecting viewport (a pixel rectangle in virtual
// full-canvas coordinates). Cached, clean tiles are reused; only dirty tiles
// are re-rendered. It returns the list of tile keys in the viewport in stable
// row-major order. The context is checked between tiles so a canceled context
// stops rendering with ErrCanceled while keeping every previously rendered
// tile intact. An empty or out-of-range viewport returns ErrEmptyViewport.
func (t *Tiler) Render(ctx context.Context, viewport image.Rectangle) ([]image.Rectangle, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrCanceled
	}
	// reject degenerate rectangles explicitly, before Canonical normalization
	// hides the inverted Min/Max as a valid-looking empty box.
	if viewport.Min.X > viewport.Max.X || viewport.Min.Y > viewport.Max.Y || viewport.Empty() {
		return nil, ErrEmptyViewport
	}
	clipped := viewport.Intersect(image.Rect(0, 0, t.fullW, t.fullH))
	if clipped.Empty() {
		return nil, ErrEmptyViewport
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrEmptyViewport
	}

	col0 := clamp(clipped.Min.X/t.tileW, 0, t.cols()-1)
	col1 := clamp((clipped.Max.X-1)/t.tileW, 0, t.cols()-1)
	row0 := clamp(clipped.Min.Y/t.tileH, 0, t.rows()-1)
	row1 := clamp((clipped.Max.Y-1)/t.tileH, 0, t.rows()-1)

	rects := make([]image.Rectangle, 0, (col1-col0+1)*(row1-row0+1))
	t.stats.DirtyTiles = 0
	for row := row0; row <= row1; row++ {
		for col := col0; col <= col1; col++ {
			if err := ctx.Err(); err != nil {
				return rects, ErrCanceled
			}
			key := tilerKey{col, row}
			rect := t.tileRect(col, row)
			rects = append(rects, rect)

			entry, cached := t.cache[key]
			_, isDirty := t.dirty[key]
			if cached && !isDirty {
				t.touch(key)
				continue
			}

			if err := t.renderTileLocked(key, rect, entry); err != nil {
				return rects, err
			}
			t.stats.DirtyTiles++
			delete(t.dirty, key)
		}
	}
	return rects, nil
}

// renderTileLocked renders one tile into the cache. A fresh pixel buffer is
// always allocated: images returned by Tile/TileAt are shared with concurrent
// readers and must never be mutated in place. The previous buffer stays
// reachable until the swap, so an in-flight reader keeps a valid image while
// the new render proceeds; after the swap the old buffer is garbage collected
// once released. The transient full-width backing buffer used while rendering
// is released when this call returns.
func (t *Tiler) renderTileLocked(key tilerKey, rect image.Rectangle, previous *cacheEntry) error {
	w, h := rect.Dx(), rect.Dy()
	if err := t.checkTileSize(w, h); err != nil {
		return err
	}
	// the transient backing spans the full raster width over this tile's rows
	if math.MaxInt32/t.fullW < h || t.fullW*h > maxBackingPixels {
		return ErrTileTooLarge
	}
	nBytes := w * h * 4
	if err := t.reserveLocked(key, nBytes); err != nil {
		return err
	}

	img := RenderTile(t.c, w, h, rect.Min.X, rect.Min.Y, t.fullW, t.fullH, t.resolution, t.colorSpace, t.op)

	if previous != nil {
		// free the cache slot accounting of the old buffer before inserting
		t.removeLRU(key)
	}
	t.cache[key] = &cacheEntry{img: img, rect: rect}
	t.touch(key)
	t.updateSizeLocked()
	return nil
}

// reserveLocked evicts least-recently-used tiles until adding a tile of
// nBytes at key fits within the configured limits. When replacing an existing
// tile, its current buffer is reclaimable and excluded from the footprint.
func (t *Tiler) reserveLocked(key tilerKey, nBytes int) error {
	if t.maxBytes > 0 && nBytes > t.maxBytes {
		return ErrTileTooLarge
	}
	footprint := func() int {
		n := t.cacheBytesLocked()
		if e, ok := t.cache[key]; ok {
			n -= len(e.img.Pix)
		}
		return n
	}
	count := func() int {
		n := len(t.cache)
		if _, ok := t.cache[key]; ok {
			n--
		}
		return n
	}
	for {
		overTiles := t.maxTiles > 0 && count() >= t.maxTiles
		overBytes := t.maxBytes > 0 && footprint()+nBytes > t.maxBytes
		if !overTiles && !overBytes {
			return nil
		}
		// never evict the tile being replaced; find another LRU victim
		victim := tilerKey{}
		found := false
		for _, k := range t.lru {
			if k != key {
				victim = k
				found = true
				break
			}
		}
		if !found {
			return ErrCacheFull
		}
		t.dropLocked(victim)
		t.stats.Evictions++
	}
}

func (t *Tiler) dropLocked(key tilerKey) {
	entry, ok := t.cache[key]
	if !ok {
		return
	}
	delete(t.cache, key)
	t.removeLRU(key)
	// drop any pending dirty marker so an evicted tile cannot leak map entries;
	// it will simply be rendered fresh if requested again later.
	delete(t.dirty, key)
	// explicit nil helps the GC reclaim the pixel buffer promptly
	entry.img = nil
}

func (t *Tiler) touch(key tilerKey) {
	t.removeLRU(key)
	t.lru = append(t.lru, key)
}

func (t *Tiler) removeLRU(key tilerKey) {
	for i, k := range t.lru {
		if k == key {
			t.lru = append(t.lru[:i], t.lru[i+1:]...)
			return
		}
	}
}

func (t *Tiler) cacheBytesLocked() int {
	n := 0
	for _, e := range t.cache {
		n += len(e.img.Pix)
	}
	return n
}

func (t *Tiler) updateSizeLocked() {
	t.stats.CachedTiles = len(t.cache)
	t.stats.CachedBytes = t.cacheBytesLocked()
	if t.stats.CachedTiles > t.stats.PeakTiles {
		t.stats.PeakTiles = t.stats.CachedTiles
	}
	if t.stats.CachedBytes > t.stats.PeakBytes {
		t.stats.PeakBytes = t.stats.CachedBytes
	}
}

// Tile returns the cached tile image for the tile covering virtual pixel
// point (x, y), or nil if it has not been rendered yet. The returned image is
// shared, not copied: callers must treat it as read-only. Safe for concurrent
// use with Render (which only replaces whole cache entries, never mutates an
// image that has been handed out).
func (t *Tiler) Tile(x, y int) (*image.RGBA, image.Rectangle, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if x < 0 || y < 0 || x >= t.fullW || y >= t.fullH {
		return nil, image.Rectangle{}, false
	}
	key := tilerKey{x / t.tileW, y / t.tileH}
	entry, ok := t.cache[key]
	if !ok {
		return nil, t.tileRect(key.col, key.row), false
	}
	return entry.img, entry.rect, true
}

// TileAt returns the cached tile image at grid coordinates (col, row).
func (t *Tiler) TileAt(col, row int) (*image.RGBA, image.Rectangle, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if col < 0 || row < 0 || col >= t.cols() || row >= t.rows() {
		return nil, image.Rectangle{}, false
	}
	key := tilerKey{col, row}
	entry, ok := t.cache[key]
	if !ok {
		return nil, t.tileRect(col, row), false
	}
	return entry.img, entry.rect, true
}

// Stats returns a snapshot of the tiler counters.
func (t *Tiler) Stats() TilerStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s := t.stats
	s.CachedTiles = len(t.cache)
	s.CachedBytes = t.cacheBytesLocked()
	return s
}

// Clear empties the cache, releasing all pixel buffers, and marks every
// previously cached tile dirty so it is repainted on the next Render.
func (t *Tiler) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key := range t.cache {
		t.dirty[key] = struct{}{}
		t.dropLocked(key)
	}
	t.updateSizeLocked()
}

// Close releases all cached tiles. After Close, Render and Tile return errors
// / false and the Tiler must not be reused.
func (t *Tiler) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key := range t.cache {
		t.dropLocked(key)
	}
	t.dirty = map[tilerKey]struct{}{}
	t.lru = nil
	t.closed = true
	t.updateSizeLocked()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
