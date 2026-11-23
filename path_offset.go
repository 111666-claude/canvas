package canvas

import (
	"math"
)

// OffsetOptions specifies how Path.OffsetWithOptions offsets a path. The zero value is valid and
// uses a butt cap for open paths, a miter join with a miter limit of 4.0 (falling back to a bevel
// join), and the default Tolerance.
type OffsetOptions struct {
	// Cap is the capper used for the start and end of open paths. The built-in ButtCap, SquareCap
	// and RoundCap are supported; custom cappers fall back to ButtCap. The default is ButtCap.
	Cap Capper

	// Join is the joiner used between path elements, e.g. MiterJoin, BevelJoin, RoundJoin or a
	// custom MiterJoiner{GapJoiner, Limit}. When nil, a miter join with MiterLimit is used.
	Join Joiner

	// MiterLimit is the miter limit used when Join is nil, i.e. the maximum ratio of the miter
	// length to the offset distance before the join is beveled. Values <= 0 (or NaN) use 4.0.
	// It is ignored when Join is set; use a MiterJoiner to configure the limit explicitly.
	MiterLimit float64

	// Tolerance is the maximum deviation from the actual offset when flattening Béziers and
	// optimizing the path. Values <= 0 (or NaN) use the default Tolerance.
	Tolerance float64
}

// capper resolves the configured capper, defaulting to ButtCap.
func (opts OffsetOptions) capper() Capper {
	if opts.Cap == nil {
		return ButtCap
	}
	return opts.Cap
}

// joiner resolves the configured joiner, defaulting to a miter join with MiterLimit.
func (opts OffsetOptions) joiner() Joiner {
	if opts.Join != nil {
		return opts.Join
	}
	limit := opts.MiterLimit
	if !(0.0 < limit) { // also catches NaN
		limit = 4.0
	}
	return MiterJoiner{BevelJoin, limit}
}

// tolerance resolves the configured tolerance, defaulting to Tolerance.
func (opts OffsetOptions) tolerance() float64 {
	if !(0.0 < opts.Tolerance) { // also catches NaN
		return Tolerance
	}
	return opts.Tolerance
}

// OffsetWithOptions offsets the path by w and returns a new path; the source path is never
// modified. A positive w offsets to the right-hand side of the path direction, a negative w to
// the left-hand side. Closed contours are expanded (CCW oriented) or contracted (CW oriented) by
// a positive w, exactly like Path.Offset but with configurable joins. Open paths are converted
// into the closed, fillable outline between the centerline and its offset curve, capped at both
// ends by the configured capper; this turns e.g. road centerlines into fillable boundaries.
//
// Self-intersections, spikes and overlapping or spilled parts are resolved the same way as
// Path.Stroke (unless FastStroke is set), holes are preserved, and the result is oriented CCW for
// filling contours and CW for holes. Invalid input never panics: a non-finite w or a path with
// non-finite coordinates returns an empty path, and non-positive tolerances fall back to the
// default Tolerance.
func (p *Path) OffsetWithOptions(w float64, opts OffsetOptions) *Path {
	if p.Empty() || math.IsNaN(w) || math.IsInf(w, 0.0) || !p.Sane() {
		return &Path{}
	}
	if Equal(w, 0.0) {
		return p.Copy()
	}

	tolerance := opts.tolerance()
	capper := opts.capper()
	joiner := opts.joiner()

	// split into closed contours and open centerlines, they offset differently
	closed := &Path{}
	open := []*Path{}
	for _, pi := range p.Split() {
		if pi.Closed() {
			closed = closed.Append(pi)
		} else {
			open = append(open, pi)
		}
	}

	q := &Path{}
	if !closed.Empty() {
		if !FastStroke {
			// make sure all filling paths are CCW
			closed = closed.Settle(NonZero)
		}
		for _, pi := range closed.Split() {
			rhs, _ := pi.offset(w, capper, joiner, false, tolerance)
			if rhs != nil {
				q = q.Append(rhs)
			}
		}
	}
	for _, pi := range open {
		if oq := pi.offsetOpen(w, capper, joiner, tolerance); oq != nil {
			q = q.Append(oq)
		}
	}
	if !FastStroke && !q.Empty() {
		// fix overlapping and spilled parts
		q = q.Settle(Positive)
	}
	return q
}

// offsetOpen offsets a single open subpath by w and returns the closed region between the
// centerline and the offset curve at distance |w|, capped at both ends. A positive w offsets to
// the right-hand side, a negative w to the left-hand side. It returns nil for closed or empty
// paths.
func (p *Path) offsetOpen(w float64, cr Capper, jr Joiner, tolerance float64) *Path {
	if w < 0.0 {
		// offsetting to the left is offsetting the reversed path to the right; this also keeps
		// the resulting contour oriented the same (filling) way as for positive w
		return p.Reverse().offsetOpen(-w, cr, jr, tolerance)
	}

	states, closed := p.strokeStates(w)
	if closed || len(states) == 0 {
		return nil
	}

	rhs, _ := p.offset(w, cr, jr, false, tolerance)
	if rhs == nil || rhs.Empty() {
		return nil
	}

	// normals of length w pointing towards the offset side
	first, last := states[0], states[len(states)-1]
	n0, n1 := first.n0, last.n1

	// the offset curve runs from (p0 + n0) to (p1 + n1)
	q := rhs
	halfCapEnd(q, cr, w, last.p1, n1)

	// run back along the original centerline
	q = q.Join(p.Reverse())

	halfCapStart(q, cr, w, first.p0, n0)
	q.Close()
	q.optimizeClose()
	return q
}

// halfCapEnd caps the end of a one-sided offset, drawing from the offset curve's end (pivot + n)
// back to the centerline's end (pivot). n has length halfWidth and points towards the offset
// side.
func halfCapEnd(p *Path, cr Capper, halfWidth float64, pivot, n Point) {
	switch cr.(type) {
	case RoundCapper:
		e := n.Rot90CCW() // tangent of length halfWidth, pointing forwards
		p.ArcTo(halfWidth, halfWidth, 0.0, false, true, pivot.X+e.X, pivot.Y+e.Y)
		p.LineTo(pivot.X, pivot.Y)
	case SquareCapper:
		e := n.Rot90CCW()
		p.LineTo(pivot.X+n.X+e.X, pivot.Y+n.Y+e.Y)
		p.LineTo(pivot.X+e.X, pivot.Y+e.Y)
		p.LineTo(pivot.X, pivot.Y)
	default:
		// ButtCap and custom cappers
		p.LineTo(pivot.X, pivot.Y)
	}
}

// halfCapStart caps the start of a one-sided offset, drawing from the centerline's start (pivot)
// to the offset curve's start (pivot + n). n has length halfWidth and points towards the offset
// side.
func halfCapStart(p *Path, cr Capper, halfWidth float64, pivot, n Point) {
	switch cr.(type) {
	case RoundCapper:
		e := n.Rot90CCW()
		p.LineTo(pivot.X-e.X, pivot.Y-e.Y)
		p.ArcTo(halfWidth, halfWidth, 0.0, false, true, pivot.X+n.X, pivot.Y+n.Y)
	case SquareCapper:
		e := n.Rot90CCW()
		p.LineTo(pivot.X-e.X, pivot.Y-e.Y)
		p.LineTo(pivot.X+n.X-e.X, pivot.Y+n.Y-e.Y)
	default:
		// ButtCap and custom cappers: the Close connects back to the offset curve's start
	}
}
