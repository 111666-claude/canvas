package canvas

import (
	"fmt"
	"math"
)

// FastStroke skips "settling" the resulting paths for Path.Offset and Path.Stroke, which ensures a well-formed path without overlapping
// nor holes. This fixes overlapping strokes or the inner-bend for corners. Inner-bends are currently already fixed for two line
// segments, but will otherwise leave a hole. Overlapping strokes may not be a problem when using the NonZero winding order for drawing.
// Strokes that are significantly larger than the path itself may be problematic with this enabled, but it may provide a stark
// performance gain for most (trivial) cases.
var FastStroke = false

// NOTE: implementation inspired from github.com/golang/freetype/raster/stroke.go

// StrokeAlignment defines how a stroke is aligned relative to the filled area of its path.
type StrokeAlignment int

// see StrokeAlignment
const (
	// StrokeAlignmentCenter centers the stroke on the path, extending half the stroke width to either side. This is the default.
	StrokeAlignmentCenter StrokeAlignment = iota
	// StrokeAlignmentInside places the stroke entirely inside the filled area of the path. The stroke is clipped to the fill region as defined by the fill rule, so the stroke never extends beyond the path's boundary. For open paths the fill region is enclosed by an implicit closing line; open paths without an enclosed area produce an empty stroke.
	StrokeAlignmentInside
	// StrokeAlignmentOutside places the stroke entirely outside the filled area of the path. The stroke is clipped to the complement of the fill region as defined by the fill rule, so the stroke never extends into the path's interior. For open paths the fill region is enclosed by an implicit closing line; open paths without an enclosed area produce the same stroke as center alignment.
	StrokeAlignmentOutside
)

func (align StrokeAlignment) String() string {
	switch align {
	case StrokeAlignmentCenter:
		return "Center"
	case StrokeAlignmentInside:
		return "Inside"
	case StrokeAlignmentOutside:
		return "Outside"
	}
	return fmt.Sprintf("StrokeAlignment(%d)", int(align))
}

// maxStrokeCoordinate bounds the absolute coordinates for aligned strokes. Beyond this, flattening
// curves or snap-rounding in boolean operations may allocate an unbounded amount of memory or
// overflow. This is far beyond any practical drawing coordinate (1e9mm is 1000km).
const maxStrokeCoordinate = 1e9

// strokePathValid returns true when p has finite coordinates within the supported range for
// aligned stroking.
func strokePathValid(p *Path) bool {
	if p == nil || !p.Sane() {
		return false
	}
	r := p.FastBounds()
	for _, v := range [4]float64{r.X0, r.Y0, r.X1, r.Y1} {
		if math.IsNaN(v) || math.IsInf(v, 0.0) || maxStrokeCoordinate < math.Abs(v) {
			return false
		}
	}
	return true
}

// Capper implements Cap, with rhs the path to append to, halfWidth the half width of the stroke, pivot the pivot point around which to construct a cap, and n0 the normal at the start of the path. The length of n0 is equal to the halfWidth.
type Capper interface {
	Cap(*Path, float64, Point, Point)
}

// RoundCap caps the start or end of a path by a round cap.
var RoundCap Capper = RoundCapper{}

// RoundCapper is a round capper.
type RoundCapper struct{}

// Cap adds a cap to path p of width 2*halfWidth, at a pivot point and initial normal direction of n0.
func (RoundCapper) Cap(p *Path, halfWidth float64, pivot, n0 Point) {
	end := pivot.Sub(n0)
	p.ArcTo(halfWidth, halfWidth, 0, false, true, end.X, end.Y)
}

func (RoundCapper) String() string {
	return "Round"
}

// ButtCap caps the start or end of a path by a butt cap.
var ButtCap Capper = ButtCapper{}

// ButtCapper is a butt capper.
type ButtCapper struct{}

// Cap adds a cap to path p of width 2*halfWidth, at a pivot point and initial normal direction of n0.
func (ButtCapper) Cap(p *Path, halfWidth float64, pivot, n0 Point) {
	end := pivot.Sub(n0)
	p.LineTo(end.X, end.Y)
}

func (ButtCapper) String() string {
	return "Butt"
}

// SquareCap caps the start or end of a path by a square cap.
var SquareCap Capper = SquareCapper{}

// SquareCapper is a square capper.
type SquareCapper struct{}

// Cap adds a cap to path p of width 2*halfWidth, at a pivot point and initial normal direction of n0.
func (SquareCapper) Cap(p *Path, halfWidth float64, pivot, n0 Point) {
	e := n0.Rot90CCW()
	corner1 := pivot.Add(e).Add(n0)
	corner2 := pivot.Add(e).Sub(n0)
	end := pivot.Sub(n0)
	p.LineTo(corner1.X, corner1.Y)
	p.LineTo(corner2.X, corner2.Y)
	p.LineTo(end.X, end.Y)
}

func (SquareCapper) String() string {
	return "Square"
}

////////////////

// Joiner implements Join, with rhs the right path and lhs the left path to append to, pivot the intersection of both path elements, n0 and n1 the normals at the start and end of the path respectively. The length of n0 and n1 are equal to the halfWidth.
type Joiner interface {
	Join(*Path, *Path, float64, Point, Point, Point, float64, float64)
}

// BevelJoin connects two path elements by a linear join.
var BevelJoin Joiner = BevelJoiner{}

// BevelJoiner is a bevel joiner.
type BevelJoiner struct{}

// Join adds a join to a right-hand-side and left-hand-side path, of width 2*halfWidth, around a pivot point with starting and ending normals of n0 and n1, and radius of curvatures of the previous and next segments.
func (BevelJoiner) Join(rhs, lhs *Path, halfWidth float64, pivot, n0, n1 Point, r0, r1 float64) {
	rEnd := pivot.Add(n1)
	lEnd := pivot.Sub(n1)
	rhs.LineTo(rEnd.X, rEnd.Y)
	lhs.LineTo(lEnd.X, lEnd.Y)
}

func (BevelJoiner) String() string {
	return "Bevel"
}

// RoundJoin connects two path elements by a round join.
var RoundJoin Joiner = RoundJoiner{}

// RoundJoiner is a round joiner.
type RoundJoiner struct{}

// Join adds a join to a right-hand-side and left-hand-side path, of width 2*halfWidth, around a pivot point with starting and ending normals of n0 and n1, and radius of curvatures of the previous and next segments.
func (RoundJoiner) Join(rhs, lhs *Path, halfWidth float64, pivot, n0, n1 Point, r0, r1 float64) {
	rEnd := pivot.Add(n1)
	lEnd := pivot.Sub(n1)
	cw := 0.0 <= n0.Rot90CW().Dot(n1)
	if cw { // bend to the right, ie. CW (or 180 degree turn)
		rhs.LineTo(rEnd.X, rEnd.Y)
		lhs.ArcTo(halfWidth, halfWidth, 0.0, false, false, lEnd.X, lEnd.Y)
	} else { // bend to the left, ie. CCW
		rhs.ArcTo(halfWidth, halfWidth, 0.0, false, true, rEnd.X, rEnd.Y)
		lhs.LineTo(lEnd.X, lEnd.Y)
	}
}

func (RoundJoiner) String() string {
	return "Round"
}

// MiterJoin connects two path elements by extending the ends of the paths as lines until they meet. If this point is further than the limit, this will result in a bevel join (MiterJoin) or they will meet at the limit (MiterClipJoin).
var MiterJoin Joiner = MiterJoiner{BevelJoin, 4.0}
var MiterClipJoin Joiner = MiterJoiner{nil, 4.0} // TODO: should extend limit*halfwidth before bevel

// MiterJoiner is a miter joiner.
type MiterJoiner struct {
	GapJoiner Joiner
	Limit     float64
}

// Join adds a join to a right-hand-side and left-hand-side path, of width 2*halfWidth, around a pivot point with starting and ending normals of n0 and n1, and radius of curvatures of the previous and next segments.
func (j MiterJoiner) Join(rhs, lhs *Path, halfWidth float64, pivot, n0, n1 Point, r0, r1 float64) {
	if n0.Equals(n1.Neg()) {
		BevelJoin.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
		return
	}

	cw := 0.0 <= n0.Rot90CW().Dot(n1)
	hw := halfWidth
	if cw {
		hw = -hw // used to calculate |R|, when running CW then n0 and n1 point the other way, so the sign of r0 and r1 is negated
	}

	// note that cos(theta) below refers to sin(theta/2) in the documentation of stroke-miterlimit
	// in https://developer.mozilla.org/en-US/docs/Web/SVG/Attribute/stroke-miterlimit
	theta := n0.AngleBetween(n1) / 2.0 // half the angle between normals
	d := hw / math.Cos(theta)          // half the miter length
	limit := math.Max(j.Limit, 1.001)  // otherwise nearly linear joins will also get clipped
	clip := !math.IsNaN(limit) && limit*halfWidth < math.Abs(d)
	if clip && j.GapJoiner != nil {
		j.GapJoiner.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
		return
	}

	rEnd := pivot.Add(n1)
	lEnd := pivot.Sub(n1)
	mid := pivot.Add(n0.Add(n1).Norm(d))
	if clip {
		// miter-clip
		t := math.Abs(limit * halfWidth / d)
		if cw { // bend to the right, ie. CW
			mid0 := lhs.Pos().Interpolate(mid, t)
			mid1 := lEnd.Interpolate(mid, t)
			lhs.LineTo(mid0.X, mid0.Y)
			lhs.LineTo(mid1.X, mid1.Y)
		} else {
			mid0 := rhs.Pos().Interpolate(mid, t)
			mid1 := rEnd.Interpolate(mid, t)
			rhs.LineTo(mid0.X, mid0.Y)
			rhs.LineTo(mid1.X, mid1.Y)
		}
	} else {
		if cw { // bend to the right, ie. CW
			lhs.LineTo(mid.X, mid.Y)
		} else {
			rhs.LineTo(mid.X, mid.Y)
		}
	}
	rhs.LineTo(rEnd.X, rEnd.Y)
	lhs.LineTo(lEnd.X, lEnd.Y)
}

func (j MiterJoiner) String() string {
	if j.GapJoiner == nil {
		return "MiterClip"
	}
	return "Miter"
}

// ArcsJoin connects two path elements by extending the ends of the paths as circle arcs until they meet. If this point is further than the limit, this will result in a bevel join (ArcsJoin) or they will meet at the limit (ArcsClipJoin).
var ArcsJoin Joiner = ArcsJoiner{BevelJoin, 4.0}
var ArcsClipJoin Joiner = ArcsJoiner{nil, 4.0}

// ArcsJoiner is an arcs joiner.
type ArcsJoiner struct {
	GapJoiner Joiner
	Limit     float64
}

func closestArcIntersection(c Point, cw bool, pivot, i0, i1 Point) Point {
	thetaPivot := pivot.Sub(c).Angle()
	dtheta0 := i0.Sub(c).Angle() - thetaPivot
	dtheta1 := i1.Sub(c).Angle() - thetaPivot
	if cw { // arc runs clockwise, so look the other way around
		dtheta0 = -dtheta0
		dtheta1 = -dtheta1
	}
	if angleNorm(dtheta1) < angleNorm(dtheta0) {
		return i1
	}
	return i0
}

// Join adds a join to a right-hand-side and left-hand-side path, of width 2*halfWidth, around a pivot point with starting and ending normals of n0 and n1, and radius of curvatures of the previous and next segments, which are positive for CCW arcs.
func (j ArcsJoiner) Join(rhs, lhs *Path, halfWidth float64, pivot, n0, n1 Point, r0, r1 float64) {
	if n0.Equals(n1.Neg()) {
		BevelJoin.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
		return
	} else if math.IsNaN(r0) && math.IsNaN(r1) {
		MiterJoiner(j).Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
		return
	}
	limit := math.Max(j.Limit, 1.001) // 1.001 so that nearly linear joins will not get clipped

	cw := 0.0 <= n0.Rot90CW().Dot(n1)
	hw := halfWidth
	if cw {
		hw = -hw // used to calculate |R|, when running CW then n0 and n1 point the other way, so the sign of r0 and r1 is negated
	}

	// r is the radius of the original curve, R the radius of the stroke curve, c are the centers of the circles
	c0 := pivot.Add(n0.Norm(-r0))
	c1 := pivot.Add(n1.Norm(-r1))
	R0, R1 := math.Abs(r0+hw), math.Abs(r1+hw)

	// TODO: can simplify if intersection returns angles too?
	var i0, i1 Point
	var ok bool
	if math.IsNaN(r0) {
		line := pivot.Add(n0)
		if cw {
			line = pivot.Sub(n0)
		}
		i0, i1, ok = intersectionRayCircle(line, line.Add(n0.Rot90CCW()), c1, R1)
	} else if math.IsNaN(r1) {
		line := pivot.Add(n1)
		if cw {
			line = pivot.Sub(n1)
		}
		i0, i1, ok = intersectionRayCircle(line, line.Add(n1.Rot90CCW()), c0, R0)
	} else {
		i0, i1, ok = intersectionCircleCircle(c0, R0, c1, R1)
	}
	if !ok {
		// no intersection
		BevelJoin.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
		return
	}

	// find the closest intersection when following the arc (using either arc r0 or r1 with center c0 or c1 respectively)
	var mid Point
	if !math.IsNaN(r0) {
		mid = closestArcIntersection(c0, r0 < 0.0, pivot, i0, i1)
	} else {
		mid = closestArcIntersection(c1, 0.0 <= r1, pivot, i0, i1)
	}

	// check arc limit
	d := mid.Sub(pivot).Length()
	clip := !math.IsNaN(limit) && limit*halfWidth < d
	if clip && j.GapJoiner != nil {
		j.GapJoiner.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
		return
	}

	mid2 := mid
	if clip {
		// arcs-clip
		start, end := pivot.Add(n0), pivot.Add(n1)
		if cw {
			start, end = pivot.Sub(n0), pivot.Sub(n1)
		}

		var clipMid, clipNormal Point
		if !math.IsNaN(r0) && !math.IsNaN(r1) && (0.0 < r0) == (0.0 < r1) {
			// circle have opposite direction/sweep
			// NOTE: this may cause the bevel to be imperfectly oriented
			clipMid = mid.Sub(pivot).Norm(limit * halfWidth)
			clipNormal = clipMid.Rot90CCW()
		} else {
			// circle in between both stroke edges
			rMid := (r0 - r1) / 2.0
			if math.IsNaN(r0) {
				rMid = -(r1 + hw) * 2.0
			} else if math.IsNaN(r1) {
				rMid = (r0 + hw) * 2.0
			}

			sweep := 0.0 < rMid
			RMid := math.Abs(rMid)
			cx, cy, a0, _ := ellipseToCenter(pivot.X, pivot.Y, RMid, RMid, 0.0, false, sweep, mid.X, mid.Y)
			cMid := Point{cx, cy}
			dtheta := limit * halfWidth / rMid

			clipMid = EllipsePos(RMid, RMid, 0.0, cMid.X, cMid.Y, a0+dtheta)
			clipNormal = ellipseNormal(RMid, RMid, 0.0, sweep, a0+dtheta, 1.0)
		}

		if math.IsNaN(r1) {
			i0, ok = intersectionRayLine(clipMid, clipMid.Add(clipNormal), mid, end)
			if !ok {
				// not sure when this occurs
				BevelJoin.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
				return
			}
			mid2 = i0
		} else {
			i0, i1, ok = intersectionRayCircle(clipMid, clipMid.Add(clipNormal), c1, R1)
			if !ok {
				// not sure when this occurs
				BevelJoin.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
				return
			}
			mid2 = closestArcIntersection(c1, 0.0 <= r1, pivot, i0, i1)
		}

		if math.IsNaN(r0) {
			i0, ok = intersectionRayLine(clipMid, clipMid.Add(clipNormal), start, mid)
			if !ok {
				// not sure when this occurs
				BevelJoin.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
				return
			}
			mid = i0
		} else {
			i0, i1, ok = intersectionRayCircle(clipMid, clipMid.Add(clipNormal), c0, R0)
			if !ok {
				// not sure when this occurs
				BevelJoin.Join(rhs, lhs, halfWidth, pivot, n0, n1, r0, r1)
				return
			}
			mid = closestArcIntersection(c0, r0 < 0.0, pivot, i0, i1)
		}
	}

	rEnd := pivot.Add(n1)
	lEnd := pivot.Sub(n1)
	if cw { // bend to the right, ie. CW
		rhs.LineTo(rEnd.X, rEnd.Y)
		if math.IsNaN(r0) {
			lhs.LineTo(mid.X, mid.Y)
		} else {
			lhs.ArcTo(R0, R0, 0.0, false, 0.0 < r0, mid.X, mid.Y)
		}
		if clip {
			lhs.LineTo(mid2.X, mid2.Y)
		}
		if math.IsNaN(r1) {
			lhs.LineTo(lEnd.X, lEnd.Y)
		} else {
			lhs.ArcTo(R1, R1, 0.0, false, 0.0 < r1, lEnd.X, lEnd.Y)
		}
	} else { // bend to the left, ie. CCW
		if math.IsNaN(r0) {
			rhs.LineTo(mid.X, mid.Y)
		} else {
			rhs.ArcTo(R0, R0, 0.0, false, 0.0 < r0, mid.X, mid.Y)
		}
		if clip {
			rhs.LineTo(mid2.X, mid2.Y)
		}
		if math.IsNaN(r1) {
			rhs.LineTo(rEnd.X, rEnd.Y)
		} else {
			rhs.ArcTo(R1, R1, 0.0, false, 0.0 < r1, rEnd.X, rEnd.Y)
		}
		lhs.LineTo(lEnd.X, lEnd.Y)
	}
}

func (j ArcsJoiner) String() string {
	if j.GapJoiner == nil {
		return "ArcsClip"
	}
	return "Arcs"
}

func (p *Path) optimizeInnerBend(i int) {
	// i is the index of the line segment in the inner bend connecting both edges
	ai := i - cmdLen(p.d[i-1])
	bi := i + cmdLen(p.d[i])
	if ai == 0 {
		return
	}

	a0 := Point{p.d[ai-3], p.d[ai-2]}
	b0 := Point{p.d[bi-3], p.d[bi-2]}
	if bi == len(p.d) {
		// inner bend is at the path's start
		bi = 4
	}

	// TODO: implement other segment combinations
	zs_ := [2]Intersection{}
	zs := zs_[:]
	if (p.d[ai] == LineToCmd || p.d[ai] == CloseCmd) && (p.d[bi] == LineToCmd || p.d[bi] == CloseCmd) {
		zs = intersectionSegment(zs[:0], a0, p.d[ai:ai+4], b0, p.d[bi:bi+4])
		// TODO: check conditions for pathological cases
		if len(zs) == 1 && zs[0].T[0] != 0.0 && zs[0].T[0] != 1.0 && zs[0].T[1] != 0.0 && zs[0].T[1] != 1.0 {
			p.d[ai+1] = zs[0].X
			p.d[ai+2] = zs[0].Y
			if bi == 4 {
				// inner bend is at the path's start
				if p.d[i] == CloseCmd {
					if p.d[ai] == LineToCmd {
						p.d[ai] = CloseCmd
						p.d[ai+3] = CloseCmd
					} else {
						p.d = append(p.d, CloseCmd, zs[0].X, zs[1].Y, CloseCmd)
					}
				}
				p.d = p.d[:i]
				p.d[1] = zs[0].X
				p.d[2] = zs[0].Y
			} else {
				p.d = append(p.d[:i], p.d[bi:]...)
			}
		}
	}
}

type pathStrokeState struct {
	cmd    float64
	p0, p1 Point   // position of start and end
	n0, n1 Point   // normal of start and end (points right when walking the path)
	r0, r1 float64 // radius of start and end

	cp1, cp2                    Point   // Béziers
	rx, ry, rot, theta0, theta1 float64 // arcs
	large, sweep                bool    // arcs
}

// offset returns the rhs and lhs paths from offsetting a path (must not have subpaths). It closes rhs and lhs when p is closed as well.
func (p *Path) offset(halfWidth float64, cr Capper, jr Joiner, strokeOpen bool, tolerance float64) (*Path, *Path) {
	// only non-empty paths are evaluated
	closed := false
	states := []pathStrokeState{}
	var start, end Point
	for i := 0; i < len(p.d); {
		cmd := p.d[i]
		switch cmd {
		case MoveToCmd:
			end = Point{p.d[i+1], p.d[i+2]}
		case LineToCmd:
			end = Point{p.d[i+1], p.d[i+2]}
			n := end.Sub(start).Rot90CW().Norm(halfWidth)
			states = append(states, pathStrokeState{
				cmd: LineToCmd,
				p0:  start,
				p1:  end,
				n0:  n,
				n1:  n,
				r0:  math.NaN(),
				r1:  math.NaN(),
			})
		case QuadToCmd, CubeToCmd:
			var cp1, cp2 Point
			if cmd == QuadToCmd {
				cp := Point{p.d[i+1], p.d[i+2]}
				end = Point{p.d[i+3], p.d[i+4]}
				cp1, cp2 = quadraticToCubicBezier(start, cp, end)
			} else {
				cp1 = Point{p.d[i+1], p.d[i+2]}
				cp2 = Point{p.d[i+3], p.d[i+4]}
				end = Point{p.d[i+5], p.d[i+6]}
			}
			n0 := cubicBezierNormal(start, cp1, cp2, end, 0.0, halfWidth)
			n1 := cubicBezierNormal(start, cp1, cp2, end, 1.0, halfWidth)
			r0 := cubicBezierCurvatureRadius(start, cp1, cp2, end, 0.0)
			r1 := cubicBezierCurvatureRadius(start, cp1, cp2, end, 1.0)
			states = append(states, pathStrokeState{
				cmd: CubeToCmd,
				p0:  start,
				p1:  end,
				n0:  n0,
				n1:  n1,
				r0:  r0,
				r1:  r1,
				cp1: cp1,
				cp2: cp2,
			})
		case ArcToCmd:
			rx, ry, phi := p.d[i+1], p.d[i+2], p.d[i+3]
			large, sweep := toArcFlags(p.d[i+4])
			end = Point{p.d[i+5], p.d[i+6]}
			_, _, theta0, theta1 := ellipseToCenter(start.X, start.Y, rx, ry, phi, large, sweep, end.X, end.Y)
			n0 := ellipseNormal(rx, ry, phi, sweep, theta0, halfWidth)
			n1 := ellipseNormal(rx, ry, phi, sweep, theta1, halfWidth)
			r0 := ellipseCurvatureRadius(rx, ry, sweep, theta0)
			r1 := ellipseCurvatureRadius(rx, ry, sweep, theta1)
			states = append(states, pathStrokeState{
				cmd:    ArcToCmd,
				p0:     start,
				p1:     end,
				n0:     n0,
				n1:     n1,
				r0:     r0,
				r1:     r1,
				rx:     rx,
				ry:     ry,
				rot:    phi * 180.0 / math.Pi,
				theta0: theta0,
				theta1: theta1,
				large:  large,
				sweep:  sweep,
			})
		case CloseCmd:
			end = Point{p.d[i+1], p.d[i+2]}
			if !Equal(start.X, end.X) || !Equal(start.Y, end.Y) {
				n := end.Sub(start).Rot90CW().Norm(halfWidth)
				states = append(states, pathStrokeState{
					cmd: LineToCmd,
					p0:  start,
					p1:  end,
					n0:  n,
					n1:  n,
					r0:  math.NaN(),
					r1:  math.NaN(),
				})
			}
			closed = true
		}
		start = end
		i += cmdLen(cmd)
	}
	if len(states) == 0 {
		return nil, nil
	}

	rhs, lhs := &Path{}, &Path{}
	rStart := states[0].p0.Add(states[0].n0)
	lStart := states[0].p0.Sub(states[0].n0)
	rhs.MoveTo(rStart.X, rStart.Y)
	lhs.MoveTo(lStart.X, lStart.Y)
	rhsJoinIndex, lhsJoinIndex := -1, -1
	for i, cur := range states {
		switch cur.cmd {
		case LineToCmd:
			rEnd := cur.p1.Add(cur.n1)
			lEnd := cur.p1.Sub(cur.n1)
			rhs.LineTo(rEnd.X, rEnd.Y)
			lhs.LineTo(lEnd.X, lEnd.Y)
		case CubeToCmd:
			rhs = rhs.Join(strokeCubicBezier(cur.p0, cur.cp1, cur.cp2, cur.p1, halfWidth, tolerance))
			lhs = lhs.Join(strokeCubicBezier(cur.p0, cur.cp1, cur.cp2, cur.p1, -halfWidth, tolerance))
		case ArcToCmd:
			rStart := cur.p0.Add(cur.n0)
			lStart := cur.p0.Sub(cur.n0)
			rEnd := cur.p1.Add(cur.n1)
			lEnd := cur.p1.Sub(cur.n1)
			dr := halfWidth
			if !cur.sweep { // bend to the right, ie. CW
				dr = -dr
			}

			rLambda := ellipseRadiiCorrection(rStart, cur.rx+dr, cur.ry+dr, cur.rot*math.Pi/180.0, rEnd)
			lLambda := ellipseRadiiCorrection(lStart, cur.rx-dr, cur.ry-dr, cur.rot*math.Pi/180.0, lEnd)
			if rLambda <= 1.0 && lLambda <= 1.0 {
				rLambda, lLambda = 1.0, 1.0
			}
			rhs.ArcTo(rLambda*(cur.rx+dr), rLambda*(cur.ry+dr), cur.rot, cur.large, cur.sweep, rEnd.X, rEnd.Y)
			lhs.ArcTo(lLambda*(cur.rx-dr), lLambda*(cur.ry-dr), cur.rot, cur.large, cur.sweep, lEnd.X, lEnd.Y)
		}

		// optimize inner bend
		if 0 < i {
			prev := states[i-1]
			cw := 0.0 <= prev.n1.Rot90CW().Dot(cur.n0)
			if cw && rhsJoinIndex != -1 {
				rhs.optimizeInnerBend(rhsJoinIndex)
			} else if !cw && lhsJoinIndex != -1 {
				lhs.optimizeInnerBend(lhsJoinIndex)
			}
		}
		rhsJoinIndex = -1
		lhsJoinIndex = -1

		// join the cur and next path segments
		if i+1 < len(states) || closed {
			next := states[0]
			if i+1 < len(states) {
				next = states[i+1]
			}
			if !cur.n1.Equals(next.n0) {
				rhsJoinIndex = len(rhs.d)
				lhsJoinIndex = len(lhs.d)
				jr.Join(rhs, lhs, halfWidth, cur.p1, cur.n1, next.n0, cur.r1, next.r0)
			}
		}
	}

	if closed {
		rhs.Close()
		lhs.Close()

		// optimize inner bend
		if 1 < len(states) {
			cw := 0.0 <= states[len(states)-1].n1.Rot90CW().Dot(states[0].n0)
			if cw && rhsJoinIndex != -1 {
				rhs.optimizeInnerBend(rhsJoinIndex)
			} else if !cw && lhsJoinIndex != -1 {
				lhs.optimizeInnerBend(lhsJoinIndex)
			}
		}

		rhs.optimizeClose()
		lhs.optimizeClose()
	} else if strokeOpen {
		lhs = lhs.Reverse()
		cr.Cap(rhs, halfWidth, states[len(states)-1].p1, states[len(states)-1].n1)
		rhs = rhs.Join(lhs)
		cr.Cap(rhs, halfWidth, states[0].p0, states[0].n0.Neg())
		lhs = nil

		rhs.Close()
		rhs.optimizeClose()
	}
	return rhs, lhs
}

// Offset offsets the path by w and returns a new path. A positive w will offset the path to the right-hand side, that is, it expands CCW oriented contours and contracts CW oriented contours. If you don't know the orientation you can use `Path.CCW` to find out, but if there may be self-intersection you should use `Path.Settle` to remove them and orient all filling contours CCW. The tolerance is the maximum deviation from the actual offset when flattening Béziers and optimizing the path.
func (p *Path) Offset(w float64, tolerance float64) *Path {
	if Equal(w, 0.0) {
		return p
	}

	q := &Path{}
	if !FastStroke {
		// make sure all filling paths are CCW
		p = p.Settle(NonZero) // TODO: set/check boolean if path is settled to avoid costly and unnecessary settling
	}
	for _, pi := range p.Split() {
		rhs, _ := pi.offset(w, ButtCap, RoundJoin, false, tolerance)
		if rhs != nil {
			q = q.Append(rhs)
		}
	}
	if !FastStroke {
		// fix overlapping and spilled parts
		q = q.Settle(Positive)
	}
	return q
}

// Stroke converts a path into a stroke of width w and returns a new path. It uses cr to cap the start and end of the path, and jr to join all path elements. If the path closes itself, it will use a join between the start and end instead of capping them. The tolerance is the maximum deviation from the original path when flattening Béziers and optimizing the stroke.
func (p *Path) Stroke(w float64, cr Capper, jr Joiner, tolerance float64) *Path {
	if cr == nil {
		cr = ButtCap
	}
	if jr == nil {
		jr = MiterJoin
	}

	q := &Path{}
	halfWidth := math.Abs(w) / 2.0
	for _, pi := range p.Split() {
		rhs, lhs := pi.offset(halfWidth, cr, jr, true, tolerance)
		if rhs != nil {
			q = q.Append(rhs).Append(lhs.Reverse())
		}
	}
	if !FastStroke {
		// fix overlapping and spilled parts
		q = q.Settle(Positive)
	}
	return q
}

// strokeSegmentEstimate estimates the number of segments produced when flattening p at the given
// tolerance. Curve flattening spawns roughly sqrt(size/tolerance) segments. This is used to reject
// aligned strokes that would otherwise allocate an unbounded amount of memory. The boolean
// operations flatten at the global Tolerance, so the estimate takes the finer of the two to
// overestimate rather than underestimate.
func (p *Path) strokeSegmentEstimate(tolerance float64) float64 {
	if p.Empty() {
		return 0.0
	}
	tol := tolerance
	if 0.0 < Tolerance && Tolerance < tol {
		tol = Tolerance
	}
	if !(Epsilon < tol) || math.IsInf(tol, 0.0) || math.IsNaN(tol) {
		return math.Inf(1)
	}
	bounds := p.FastBounds()
	diag := math.Hypot(bounds.W(), bounds.H())
	if math.IsInf(diag, 0.0) || math.IsNaN(diag) {
		return math.Inf(1)
	}
	if diag == 0.0 {
		return float64(p.Len())
	}
	est := 0.0
	for i := 0; i < len(p.d); {
		cmd := p.d[i]
		if cmd == QuadToCmd || cmd == CubeToCmd || cmd == ArcToCmd {
			est += math.Sqrt(diag / tol)
		} else {
			est += 1.0
		}
		i += cmdLen(cmd)
	}
	return est
}

// pathFilledArea returns the largest contour area of an already settled path. Settled paths are
// grouped as a filling contour followed by its holes, but scanning all subpaths keeps this robust
// for degenerate input. It is used to detect paths that do not enclose an area.
func pathFilledArea(p *Path) float64 {
	area := 0.0
	for _, pi := range p.Split() {
		if a := PolylineFromPathCoords(pi).Area(); area < a {
			area = a
		}
	}
	return area
}

// maxStrokeSegments is the maximum estimated number of segments for an aligned stroke. Beyond this
// the aligned stroke is considered degenerate and an empty path is returned, to avoid allocating
// an unbounded amount of memory for paths with very large coordinates or very small tolerances.
const maxStrokeSegments = 1 << 20

// StrokeAligned converts path p into a stroke of width w aligned to the fill region of p itself.
// It is equivalent to p.StrokeAlignedTo(p, ...), see there for the alignment semantics.
func (p *Path) StrokeAligned(w float64, cr Capper, jr Joiner, tolerance float64, align StrokeAlignment, fillRule FillRule) *Path {
	return p.StrokeAlignedTo(p, w, cr, jr, tolerance, align, fillRule)
}

// StrokeAlignedTo converts path p into a stroke of width w aligned to the fill region of fillPath.
// This is useful when p has been modified (e.g. dashed) while fillPath is the original path whose
// fill region defines what is inside and outside.
//
// The fill region is the area that would be painted when filling fillPath with the given fill rule.
// StrokeAlignmentInside keeps the full stroke width inside the fill region, StrokeAlignmentOutside
// keeps the full stroke width outside the fill region, and StrokeAlignmentCenter does not clip at
// all. The stroke boundary opposite to the path is moved by the full width, so a stroke of width w
// always covers a band of width w, as for center alignment. Consequently:
//   - holes and contours negated by the fill rule are outside of the fill region, so an inside
//     stroke moves away from a hole (into the material) and an outside stroke moves into a hole;
//   - self-intersecting paths are resolved using the fill rule;
//   - open paths are implicitly closed to form their fill region, so an open path that does not
//     enclose an area has an empty inside stroke and an outside stroke equal to the centered one;
//   - the fill region is independent of the fill paint, its alpha, and the compositing order;
//   - caps and joins apply to the stroke's outline; joins on the boundary side are honored.
//
// A non-positive or non-finite width, a non-positive or non-finite tolerance, an unknown alignment
// (which degrades to center), or non-finite or excessively large coordinates (see
// maxStrokeCoordinate) result in an empty path rather than an error or an unbounded allocation.
// Extreme angles may cause the centered stroke itself to overflow, in which case an empty path is
// returned as well.
//
// The alignment is resolved in the path's own coordinate space, before any renderer or view
// transformation is applied, just like the stroke width itself. The stroke is flattened using the
// boolean-operation tolerance, so aligned strokes are polygonal.
func (p *Path) StrokeAlignedTo(fillPath *Path, w float64, cr Capper, jr Joiner, tolerance float64, align StrokeAlignment, fillRule FillRule) *Path {
	if align != StrokeAlignmentInside && align != StrokeAlignmentOutside {
		return p.Stroke(w, cr, jr, tolerance)
	}
	if math.IsInf(w, 0.0) || math.IsNaN(w) {
		return &Path{}
	}
	w = math.Abs(w)
	if !(Epsilon < w) {
		// zero width has no stroke
		return &Path{}
	}
	if !(Epsilon < tolerance) || math.IsInf(tolerance, 0.0) || math.IsNaN(tolerance) {
		return &Path{}
	}
	if !strokePathValid(p) || !strokePathValid(fillPath) {
		return &Path{}
	}
	if p.Empty() {
		return &Path{}
	}
	if maxStrokeCoordinate < w || math.IsInf(2.0*w, 0.0) {
		// a stroke wider than the coordinate bound would overflow
		return &Path{}
	}
	if maxStrokeSegments < p.strokeSegmentEstimate(tolerance)+fillPath.strokeSegmentEstimate(tolerance) {
		// too many segments would be generated, reject before allocating
		return &Path{}
	}

	region := fillPath.Settle(fillRule)
	if region.Empty() || !(Epsilon < pathFilledArea(region)) {
		// no enclosed area, e.g. for open paths that do not enclose anything. Inside has nothing
		// to fill, outside degrades to the centered stroke so that a line is not doubled in width.
		if align == StrokeAlignmentInside {
			return &Path{}
		}
		return p.Stroke(w, cr, jr, tolerance)
	}

	// Use a band of twice the width so that clipping it at the path halves it into a full-width
	// stroke on either side. This is equivalent to offsetting the path by the full width and
	// stroking it, but stays well-defined for self-intersections and narrow regions.
	band := p.Stroke(2.0*w, cr, jr, tolerance)
	if band.Empty() || !strokePathValid(band) {
		// empty stroke, or an extreme join caused the centered stroke to overflow
		return &Path{}
	}

	var q *Path
	if align == StrokeAlignmentInside {
		q = band.And(region)
	} else {
		q = band.Not(region)
	}
	if q == nil {
		return &Path{}
	}
	return q
}

// StrokeStyle converts path p into its stroked outline according to the style's stroke width,
// capper, joiner, dashes, and alignment. The caller is responsible for checking Style.HasStroke.
// Dashes in Style.Dashes are multiples of the stroke width and are scaled accordingly. For aligned
// strokes the dash pattern is applied in the path's own coordinate space and the fill region that
// defines inside and outside is derived from the original (undashed) path, so that each dash is
// aligned to the same boundary.
func (p *Path) StrokeStyle(style Style, tolerance float64) *Path {
	stroke := p
	if 0 < len(style.Dashes) {
		dashOffset, dashes := ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
		stroke = stroke.Dash(dashOffset, dashes...)
	}
	if style.StrokeAlign == StrokeAlignmentInside || style.StrokeAlign == StrokeAlignmentOutside {
		return stroke.StrokeAlignedTo(p, style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, tolerance, style.StrokeAlign, style.FillRule)
	}
	return stroke.Stroke(style.StrokeWidth, style.StrokeCapper, style.StrokeJoiner, tolerance)
}
