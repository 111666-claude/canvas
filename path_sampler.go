package canvas

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// Errors returned by PathSampler. They may be wrapped with additional details and can be
// checked with errors.Is.
var (
	// ErrEmptyPath is returned when constructing a sampler for a path without drawable
	// segments (an empty path, or one consisting of only MoveTo and zero-length Close commands).
	ErrEmptyPath = errors.New("path sampler: path is empty")

	// ErrInvalidDistance is returned when a distance or normalized t is NaN or infinite.
	ErrInvalidDistance = errors.New("path sampler: distance must be a finite number")

	// ErrDistanceOutOfRange is returned when a distance is negative or exceeds the path's
	// total length. Clamp the distance yourself, or use AtClamped, to avoid this error.
	ErrDistanceOutOfRange = errors.New("path sampler: distance is out of range [0, length]")
)

// samplerPanels is the number of panels in the per-segment arc-length look-up table.
// Position error is bounded by the largest panel arc length; 32 panels keep that below
// roughly 3% of the segment length even for strongly non-uniform speed, and the final
// bisection drives the arc-length error down to samplerEps of the panel length.
const samplerPanels = 32

// samplerEps is the relative precision (of a panel length) at which bisection stops.
const samplerEps = 1e-12

// samplerMaxBisections caps the number of bisection iterations so that degenerate or
// extremely ill-conditioned segments can never cause infinite subdivision.
const samplerMaxBisections = 64

// samplerIntAbs and samplerIntRel are the absolute and relative tolerances of the
// adaptive Gauss-Legendre (n=15 vs n=7) arc-length integration.
const (
	samplerIntAbs = 1e-12
	samplerIntRel = 1e-12
)

// samplerIntDepth is the maximum recursion depth of the adaptive integrator. The panels
// of the look-up table give a hard fallback once this depth is reached.
const samplerIntDepth = 48

// gaussLegendre15 evaluates f on [a,b] using 15-point Gauss-Legendre quadrature and
// additionally returns the 7-point estimate on the same nodes (all GL7 nodes are also
// GL15 nodes), allowing an adaptive split with a single set of function evaluations.
func gaussLegendre15(f func(float64) float64, a, b float64) (q15, q7 float64) {
	c := (b - a) / 2.0
	d := (a + b) / 2.0
	var s15, s7 float64
	for i := 0; i < 15; i++ {
		y := gl15Nodes[i]
		v := f(c*y + d)
		s15 += gl15Weights[i] * v
		if w, ok := gl7WeightsAtNode[y]; ok {
			s7 += w * v
		}
	}
	return c * s15, c * s7
}

// gl15Nodes are the 15-point Gauss-Legendre nodes on [-1,1], ascending.
var gl15Nodes = [15]float64{
	-0.9914553711208126,
	-0.9491079123427585,
	-0.8648644233597691,
	-0.7415311855993944,
	-0.5860872354676911,
	-0.4058451513773972,
	-0.2077849550078985,
	0.0,
	0.2077849550078985,
	0.4058451513773972,
	0.5860872354676911,
	0.7415311855993944,
	0.8648644233597691,
	0.9491079123427585,
	0.9914553711208126,
}

// gl15Weights are the weights corresponding to gl15Nodes.
var gl15Weights = [15]float64{
	0.02293532201052922,
	0.06309209262997855,
	0.1047900103222502,
	0.1406532597155259,
	0.1690047266392679,
	0.1903505780647854,
	0.2044329400752989,
	0.2094821410847278,
	0.2044329400752989,
	0.1903505780647854,
	0.1690047266392679,
	0.1406532597155259,
	0.1047900103222502,
	0.06309209262997855,
	0.02293532201052922,
}

// gl7WeightsAtNode maps the seven 7-point Gauss-Legendre nodes (a subset of the 15-point
// nodes) to their weights, enabling a nested error estimate.
var gl7WeightsAtNode = map[float64]float64{
	-0.9491079123427585: 0.1294849661688697,
	-0.7415311855993944: 0.2797053914892767,
	-0.4058451513773972: 0.3818300505051189,
	0.0:                 0.4179591836734694,
	0.4058451513773972:  0.3818300505051189,
	0.7415311855993944:  0.2797053914892767,
	0.9491079123427585:  0.1294849661688697,
}

// adaptiveIntegrate integrates f on [a,b] using nested GL15/GL7 quadrature, splitting
// intervals whose two estimates disagree by more than the absolute/relative tolerances.
// Recursion depth is bounded so a singular integrand can not cause infinite subdivision.
func adaptiveIntegrate(f func(float64) float64, a, b float64) float64 {
	var integrate func(a, b float64, depth int) float64
	integrate = func(a, b float64, depth int) float64 {
		q15, q7 := gaussLegendre15(f, a, b)
		if depth >= samplerIntDepth || math.Abs(q15-q7) <= samplerIntAbs+samplerIntRel*math.Abs(q15) {
			return q15
		}
		m := (a + b) / 2.0
		return integrate(a, m, depth+1) + integrate(m, b, depth+1)
	}
	return integrate(a, b, 0)
}

// samplerSegment holds the precomputed data of one drawable segment. Curve parameters are
// normalized to t in [0,1]; arcs additionally keep their center parametrization.
type samplerSegment struct {
	cmd                    float64
	segIndex               int
	start, end, cp1, cp2   Point
	rx, ry, phi            float64
	sweep                  bool
	cx, cy, theta0, theta1 float64
	length                 float64
	cumStart, cumEnd       float64
	// lut[k] is the arc length at u = k/samplerPanels, with lut[0] = 0 and
	// lut[samplerPanels] = length.
	lut [samplerPanels + 1]float64
}

// speed returns |d position / d t| for the segment's local parameter t in [0,1].
func (s *samplerSegment) speed(t float64) float64 {
	switch s.cmd {
	case LineToCmd, CloseCmd:
		// constant speed: the end-to-end distance over the unit parameter interval
		return s.end.Sub(s.start).Length()
	case QuadToCmd:
		return quadraticBezierDeriv(s.start, s.cp1, s.end, t).Length()
	case CubeToCmd:
		return cubicBezierDeriv(s.start, s.cp1, s.cp2, s.end, t).Length()
	case ArcToCmd:
		theta := s.theta0 + t*(s.theta1-s.theta0)
		// ellipseDeriv differentiates with respect to theta, while this function
		// differentiates with respect to t: apply the chain factor dtheta/dt.
		return ellipseDeriv(s.rx, s.ry, s.phi, s.sweep, theta).Length() * math.Abs(s.theta1-s.theta0)
	}
	return 0.0
}

// pos returns the position at local parameter t in [0,1].
func (s *samplerSegment) pos(t float64) Point {
	switch s.cmd {
	case LineToCmd, CloseCmd:
		return s.start.Interpolate(s.end, t)
	case QuadToCmd:
		return quadraticBezierPos(s.start, s.cp1, s.end, t)
	case CubeToCmd:
		return cubicBezierPos(s.start, s.cp1, s.cp2, s.end, t)
	case ArcToCmd:
		theta := s.theta0 + t*(s.theta1-s.theta0)
		return EllipsePos(s.rx, s.ry, s.phi, s.cx, s.cy, theta)
	}
	return Point{}
}

// tangent returns the unit tangent in the direction of travel at local parameter t.
func (s *samplerSegment) tangent(t float64) Point {
	var d Point
	switch s.cmd {
	case LineToCmd, CloseCmd:
		d = s.end.Sub(s.start)
	case QuadToCmd:
		d = quadraticBezierDeriv(s.start, s.cp1, s.end, t)
	case CubeToCmd:
		d = cubicBezierDeriv(s.start, s.cp1, s.cp2, s.end, t)
	case ArcToCmd:
		theta := s.theta0 + t*(s.theta1-s.theta0)
		d = ellipseDeriv(s.rx, s.ry, s.phi, s.sweep, theta)
	}
	if n := d.Length(); 0.0 < n {
		return d.Norm(1.0)
	}
	// Degenerate parameter value (e.g. a cusp where the derivative vanishes): take a
	// one-sided finite difference instead of returning an undefined direction.
	const h = 1e-6
	if t <= 0.0 {
		d = s.pos(math.Min(1.0, h)).Sub(s.pos(0.0))
	} else if 1.0 <= t {
		d = s.pos(1.0).Sub(s.pos(math.Max(0.0, 1.0-h)))
	} else {
		d = s.pos(math.Min(1.0, t+h)).Sub(s.pos(math.Max(0.0, t-h)))
	}
	if n := d.Length(); 0.0 < n {
		return d.Norm(1.0)
	}
	return Point{}
}

// arcToU maps an arc length d measured from the segment's start to its local parameter
// t in [0,1]. The look-up table brackets the containing panel, a linear interpolation of
// the table seeds Newton's method, and bisection provides a guaranteed-converging fallback.
// Iterations are capped so a vanishing speed (cusp) can never loop indefinitely.
func (s *samplerSegment) arcToU(d float64) float64 {
	if d <= 0.0 || s.length == 0.0 {
		return 0.0
	} else if s.length <= d {
		return 1.0
	}

	// binary search over the panel boundaries (lut is strictly increasing for a
	// non-zero-length segment)
	lo, hi := 0, samplerPanels
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if s.lut[mid] <= d {
			lo = mid
		} else {
			hi = mid
		}
	}

	d0, d1 := s.lut[lo], s.lut[hi]
	panelLen := d1 - d0
	if panelLen <= 0.0 {
		return float64(lo) / samplerPanels
	}
	tPanel0 := float64(lo) / samplerPanels
	target := d - d0
	tol := math.Max(samplerIntAbs, samplerEps*panelLen)

	// Newton seeded by linear interpolation, bracketed by [umin,umax] inside the panel.
	// Every Newton step that leaves the bracket is replaced by a bisection step, keeping
	// the guaranteed convergence of bisection with the quadratic rate of Newton.
	umin, umax := 0.0, 1.0
	u := target / panelLen // panel-relative parameter seed
	if u < 0.0 {
		u = 0.0
	} else if 1.0 < u {
		u = 1.0
	}
	for n := 0; n < samplerMaxBisections; n++ {
		t := tPanel0 + u/samplerPanels
		arc, _ := gaussLegendre15(s.speed, tPanel0, t)
		if math.Abs(arc-target) <= tol {
			return t
		}
		if arc < target {
			umin = u
		} else {
			umax = u
		}
		if umax-umin <= samplerEps {
			return tPanel0 + (umin+umax)/2.0/samplerPanels
		}

		// Newton step: F'(u) = speed(t) / samplerPanels
		v := s.speed(t) / samplerPanels
		unext := u + (target-arc)/v
		if !(umin < unext && unext < umax) || math.IsNaN(unext) || math.IsInf(unext, 0.0) {
			u = (umin + umax) / 2.0 // bisection fallback
		} else {
			u = unext
		}
	}
	return tPanel0 + (umin+umax)/2.0/samplerPanels
}

// PathSampler provides arc-length parametrization of a Path. It precomputes the arc length
// of every drawable segment and allows querying the position, unit tangent and unit normal
// at any distance along the path (or at a normalized t in [0,1]), together with the index
// of the segment containing the query point. It supports line, quadratic and cubic Bézier,
// elliptical arc, Close and paths with multiple subpaths. MoveTo commands and zero-length
// segments carry no length.
//
// The sampler is a snapshot: mutating the path after construction does not affect the
// sampler. Construct a new sampler in that case. A sampler is safe for concurrent use.
type PathSampler struct {
	segments []*samplerSegment
	length   float64
}

// NewPathSampler returns a sampler for path p. It returns ErrEmptyPath when the path has no
// drawable segments. Construction performs adaptive high-accuracy integration once; all
// subsequent queries are cheap.
func NewPathSampler(p *Path) (*PathSampler, error) {
	return NewPathSamplerWithContext(context.Background(), p)
}

// NewPathSamplerWithContext is NewPathSampler but honors ctx cancellation during the
// construction-time integration, returning the context's error when cancelled.
func NewPathSamplerWithContext(ctx context.Context, p *Path) (*PathSampler, error) {
	if p == nil || p.Empty() {
		return nil, ErrEmptyPath
	}
	if !p.Sane() {
		return nil, fmt.Errorf("%w: path contains NaN or infinite values", ErrInvalidDistance)
	}

	sampler := &PathSampler{}
	var start, end Point
	segIndex := -1
	cum := 0.0
	work := 0 // number of integrated panels, used for periodic cancellation checks
	addSegment := func(seg *samplerSegment) error {
		if err := sampler.buildLUT(ctx, seg, &work); err != nil {
			return err
		}
		if seg.length <= 0.0 {
			return nil // zero-length segment, carries no arc length
		}
		seg.cumStart, seg.cumEnd = cum, cum+seg.length
		cum += seg.length
		sampler.segments = append(sampler.segments, seg)
		return nil
	}
	for i := 0; i < len(p.d); {
		cmd := p.d[i]
		segIndex++
		switch cmd {
		case MoveToCmd:
			end = Point{p.d[i+1], p.d[i+2]}
		case LineToCmd, CloseCmd:
			end = Point{p.d[i+1], p.d[i+2]}
			if err := addSegment(&samplerSegment{
				cmd:      cmd,
				segIndex: segIndex,
				start:    start,
				end:      end,
				length:   end.Sub(start).Length(),
			}); err != nil {
				return nil, err
			}
		case QuadToCmd:
			cp := Point{p.d[i+1], p.d[i+2]}
			end = Point{p.d[i+3], p.d[i+4]}
			if err := addSegment(&samplerSegment{
				cmd:      cmd,
				segIndex: segIndex,
				start:    start,
				cp1:      cp,
				end:      end,
			}); err != nil {
				return nil, err
			}
		case CubeToCmd:
			cp1 := Point{p.d[i+1], p.d[i+2]}
			cp2 := Point{p.d[i+3], p.d[i+4]}
			end = Point{p.d[i+5], p.d[i+6]}
			if err := addSegment(&samplerSegment{
				cmd:      cmd,
				segIndex: segIndex,
				start:    start,
				cp1:      cp1,
				cp2:      cp2,
				end:      end,
			}); err != nil {
				return nil, err
			}
		case ArcToCmd:
			large, sweep := toArcFlags(p.d[i+4])
			end = Point{p.d[i+5], p.d[i+6]}
			seg := &samplerSegment{
				cmd:      cmd,
				segIndex: segIndex,
				start:    start,
				rx:       p.d[i+1],
				ry:       p.d[i+2],
				phi:      p.d[i+3],
				sweep:    sweep,
				end:      end,
			}
			seg.cx, seg.cy, seg.theta0, seg.theta1 = ellipseToCenter(start.X, start.Y, seg.rx, seg.ry, seg.phi, large, sweep, end.X, end.Y)
			if err := addSegment(seg); err != nil {
				return nil, err
			}
		}
		i += cmdLen(cmd)
		start = end
	}

	if len(sampler.segments) == 0 {
		return nil, ErrEmptyPath
	}
	sampler.length = cum
	return sampler, nil
}

// buildLUT fills the segment's look-up table with adaptive panel integrations and sets its
// total length. It checks ctx every few panels so cancellation is responsive regardless of
// the number of segments.
func (s *PathSampler) buildLUT(ctx context.Context, seg *samplerSegment, work *int) error {
	seg.lut[0] = 0.0
	for k := 1; k <= samplerPanels; k++ {
		if *work&0x7 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		*work++
		a := float64(k-1) / samplerPanels
		b := float64(k) / samplerPanels
		seg.lut[k] = seg.lut[k-1] + adaptiveIntegrate(seg.speed, a, b)
	}
	seg.length = seg.lut[samplerPanels]
	return nil
}

// Length returns the total arc length of the path in millimeters, computed with adaptive
// Gauss-Legendre integration (nested n=15/n=7, relative tolerance 1e-12).
func (s *PathSampler) Length() float64 {
	return s.length
}

// segmentAt returns the segment containing distance d. At an exact segment boundary the
// segment that ends there is returned (i.e. the previous segment), except at d=0 and
// d=Length(), which return the first and last segment respectively.
func (s *PathSampler) segmentAt(d float64) *samplerSegment {
	if d <= 0.0 {
		return s.segments[0]
	} else if s.length <= d {
		return s.segments[len(s.segments)-1]
	}
	// binary search on cumulative end lengths
	lo, hi := 0, len(s.segments)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if d < s.segments[mid].cumEnd {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	// exact boundary: prefer the segment ending at d
	if 0 < lo && d == s.segments[lo].cumStart {
		return s.segments[lo-1]
	}
	return s.segments[lo]
}

func validateDistance(d, length float64) error {
	if math.IsNaN(d) || math.IsInf(d, 0.0) {
		return fmt.Errorf("%w: %v", ErrInvalidDistance, d)
	}
	// tolerate a few ULPs of round-off so that expressions such as length*N/N map to
	// the endpoint instead of spuriously failing the range check. The floor covers
	// subnormal/tiny lengths without admitting any appreciable overshoot on real paths.
	ulp := math.Nextafter(length, math.Inf(1)) - length
	tol := math.Max(ulp*8.0, 1e-300)
	if d < -tol || length+tol < d {
		return fmt.Errorf("%w: distance %v not in [0,%v]", ErrDistanceOutOfRange, d, length)
	}
	return nil
}

// At returns the position, unit tangent (direction of travel), unit normal (the tangent
// rotated 90 degrees clockwise, i.e. to the right of the direction of travel) and the
// index of the segment containing distance d. The segment index counts all path commands
// starting from zero, so MoveTo commands are included. At an exact segment boundary the
// index of the segment ending there is returned; at d=0 it is the first drawable segment.
// Distances within a few ULPs of the endpoints are treated as the endpoints.
//
// It returns ErrInvalidDistance for NaN/infinite distances and ErrDistanceOutOfRange when
// d is negative or exceeds Length().
func (s *PathSampler) At(d float64) (Point, Point, Point, int, error) {
	if err := validateDistance(d, s.length); err != nil {
		return Point{}, Point{}, Point{}, 0, err
	}
	if d < 0.0 {
		d = 0.0
	} else if s.length < d {
		d = s.length
	}
	seg := s.segmentAt(d)
	u := seg.arcToU(d - seg.cumStart)
	pos := seg.pos(u)
	tan := seg.tangent(u)
	normal := tan.Rot90CW()
	return pos, tan, normal, seg.segIndex, nil
}

// PositionAt returns the position at arc length d. See At for the distance rules.
func (s *PathSampler) PositionAt(d float64) (Point, error) {
	pos, _, _, _, err := s.At(d)
	return pos, err
}

// TangentAt returns the unit tangent at arc length d. See At for the distance rules.
func (s *PathSampler) TangentAt(d float64) (Point, error) {
	_, tan, _, _, err := s.At(d)
	return tan, err
}

// NormalAt returns the unit normal at arc length d, pointing to the right of the direction
// of travel. See At for the distance rules.
func (s *PathSampler) NormalAt(d float64) (Point, error) {
	_, _, normal, _, err := s.At(d)
	return normal, err
}

// SegmentIndexAt returns the index of the segment containing distance d, counting all path
// commands from zero (MoveTo included). See At for the distance and boundary rules.
func (s *PathSampler) SegmentIndexAt(d float64) (int, error) {
	if err := validateDistance(d, s.length); err != nil {
		return 0, err
	}
	if d < 0.0 {
		d = 0.0
	} else if s.length < d {
		d = s.length
	}
	return s.segmentAt(d).segIndex, nil
}

// AtT queries at normalized t in [0,1] mapping linearly onto [0, Length()]. It is equivalent
// to At(t * Length()). The same error rules as At apply.
func (s *PathSampler) AtT(t float64) (Point, Point, Point, int, error) {
	if math.IsNaN(t) || math.IsInf(t, 0.0) {
		return Point{}, Point{}, Point{}, 0, fmt.Errorf("%w: t=%v", ErrInvalidDistance, t)
	}
	return s.At(t * s.length)
}

// AtClamped behaves as At but clamps d to [0, Length()] instead of returning
// ErrDistanceOutOfRange. NaN and infinite distances still return ErrInvalidDistance.
func (s *PathSampler) AtClamped(d float64) (Point, Point, Point, int, error) {
	if math.IsNaN(d) || math.IsInf(d, 0.0) {
		return Point{}, Point{}, Point{}, 0, fmt.Errorf("%w: %v", ErrInvalidDistance, d)
	}
	d = math.Max(0.0, math.Min(s.length, d))
	seg := s.segmentAt(d)
	u := seg.arcToU(d - seg.cumStart)
	pos := seg.pos(u)
	tan := seg.tangent(u)
	normal := tan.Rot90CW()
	return pos, tan, normal, seg.segIndex, nil
}

// SegmentLengths returns the arc length of every drawable segment, in path order and
// summing to Length(). Zero-length commands are omitted.
func (s *PathSampler) SegmentLengths() []float64 {
	lengths := make([]float64, len(s.segments))
	for i, seg := range s.segments {
		lengths[i] = seg.length
	}
	return lengths
}
