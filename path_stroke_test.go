package canvas

import (
	"fmt"
	"math"
	"testing"

	"github.com/tdewolff/test"
)

func TestPathStroke(t *testing.T) {
	tolerance := 1.0
	var tts = []struct {
		orig   string
		w      float64
		cp     Capper
		jr     Joiner
		stroke string
	}{
		{"M10 10", 2.0, RoundCap, RoundJoin, ""},
		{"M10 10z", 2.0, RoundCap, RoundJoin, ""},
		//{"M10 10L10 5", 2.0, RoundCap, RoundJoin, "M9 5A1 1 0 0 1 11 5L11 10A1 1 0 0 1 9 10z"},
		{"M10 10L10 5", 2.0, ButtCap, RoundJoin, "M9 5L11 5L11 10L9 10z"},
		{"M10 10L10 5", 2.0, SquareCap, RoundJoin, "M9 4L11 4L11 11L9 11z"},

		{"L10 0L20 0", 2.0, ButtCap, RoundJoin, "M0 -1L10 -1L20 -1L20 1L10 1L0 1z"},
		//{"L10 0L10 10", 2.0, ButtCap, RoundJoin, "M9 1L0 1L0 -1L10 -1A1 1 0 0 1 11 0L11 10L9 10z"},
		//{"L10 0L10 -10", 2.0, ButtCap, RoundJoin, "M9 -1L9 -10L11 -10L11 0A1 1 0 0 1 10 1L0 1L0 -1z"},

		{"L10 0L20 0", 2.0, ButtCap, BevelJoin, "M0 -1L10 -1L20 -1L20 1L10 1L0 1z"},
		{"L10 0L10 10", 2.0, ButtCap, BevelJoin, "M0 -1L10 -1L11 0L11 10L9 10L9 1L0 1z"},
		{"L10 0L10 -10", 2.0, ButtCap, BevelJoin, "M0 -1L9 -1L9 -10L11 -10L11 0L10 1L0 1z"},

		{"L10 0L20 0", 2.0, ButtCap, MiterJoiner{BevelJoin, 2.0}, "M0 -1L10 -1L20 -1L20 1L10 1L0 1z"},
		{"L10 0L5 0", 2.0, ButtCap, MiterJoiner{BevelJoin, 2.0}, "M0 -1L10 -1L10 1L0 1z"},
		{"L10 0L10 10", 2.0, ButtCap, MiterJoiner{BevelJoin, 1.0}, "M0 -1L10 -1L11 0L11 10L9 10L9 1L0 1z"},
		{"L10 0L10 10", 2.0, ButtCap, MiterJoiner{BevelJoin, 2.0}, "M0 -1L10 -1L11 -1L11 0L11 10L9 10L9 1L0 1z"},
		{"L10 0L10 -10", 2.0, ButtCap, MiterJoiner{BevelJoin, 2.0}, "M0 -1L9 -1L9 -10L11 -10L11 0L11 1L10 1L0 1z"},

		{"L10 0L20 0", 2.0, ButtCap, ArcsJoiner{BevelJoin, 2.0}, "M0 -1L10 -1L20 -1L20 1L10 1L0 1z"},
		{"L10 0L5 0", 2.0, ButtCap, ArcsJoiner{BevelJoin, 2.0}, "M0 -1L10 -1L10 1L0 1z"},
		{"L10 0L10 10", 2.0, ButtCap, ArcsJoiner{BevelJoin, 1.0}, "M0 -1L10 -1L11 0L11 10L9 10L9 1L0 1z"},

		{"L10 0L10 10L0 10z", 2.0, ButtCap, MiterJoin, "M-1 -1L11 -1L11 11L-1 11zM1 1L1 9L9 9L9 1z"},
		{"L10 0L10 10L0 10z", 2.0, ButtCap, BevelJoin, "M-1 0L0 -1L10 -1L11 0L11 10L10 11L0 11L-1 10zM1 1L1 9L9 9L9 1z"},
		{"L0 10L10 10L10 0z", 2.0, ButtCap, BevelJoin, "M-1 0L0 -1L10 -1L11 0L11 10L10 11L0 11L-1 10zM1 1L1 9L9 9L9 1z"},
		//{"Q10 0 10 10", 2.0, ButtCap, BevelJoin, "M0 -1L9.5137 3.4975L11 10L9 10L7.7845 4.5025L0 1z"},
		//{"C0 10 10 10 10 0", 2.0, ButtCap, BevelJoin, "M-1 0L1 0L3.5291 6.0900L7.4502 5.2589L9 0L11 0L8.9701 6.5589L2.5234 7.8188z"},
		//{"A10 5 0 0 0 20 0", 2.0, ButtCap, BevelJoin, "M1 0A9 4 0 0 0 19 0L21 0A11 6 0 0 1 -1 0z"}, // TODO: enable tests for ellipses when Settle supports them
		//{"A10 5 0 0 1 20 0", 2.0, ButtCap, BevelJoin, "M-1 0A11 6 0 0 1 21 0L19 0A9 4 0 0 0 1 0z"},
		//{"M5 2L2 2A2 2 0 0 0 0 0", 2.0, ButtCap, BevelJoin, "M2.8284 1L5 1L5 3L2 3L1 2A1 1 0 0 0 0 1L0 -1A3 3 0 0 1 2.8284 1z"},

		// two circle quadrants joining at 90 degrees
		//{"A10 10 0 0 1 10 10A10 10 0 0 1 0 0z", 2.0, ButtCap, ArcsJoin, "M0 -1A11 11 0 0 1 11 10A11 11 0 0 1 10.958 10.958A11 11 0 0 1 10 11A11 11 0 0 1 -1 0A11 11 0 0 1 -0.958 -0.958A11 11 0 0 1 0 -1zM1.06230 1.06230A9 9 0 0 0 8.9370 8.9370A9 9 0 0 0 1.06230 1.0630z"},

		// circles joining at one point (10,0), stroke will never join
		//{"A5 5 0 0 0 10 0A10 10 0 0 1 0 10", 2.0, ButtCap, ArcsJoin, "M7 5.6569A6 6 0 0 1 -1 0L1 0A4 4 0 0 0 9 0L11 0A11 11 0 0 1 0 11L0 9A9 9 0 0 0 7 5.6569z"},

		// circle and line intersecting in one point
		//{"A2 2 0 0 1 2 2L5 2", 2.0, ButtCap, ArcsJoiner{BevelJoin, 10.0}, "M2.8284 1L5 1L5 3L0 3A1 1 0 0 0 1 2A1 1 0 0 0 0 1L0 -1A3 3 0 0 1 2.8284 1z"},
		//{"M0 4A2 2 0 0 0 2 2L5 2", 2.0, ButtCap, ArcsJoiner{BevelJoin, 10.0}, "M2.8284 3A3 3 0 0 1 0 5L0 3A1 1 0 0 0 1 2A1 1 0 0 0 0 1L5 1L5 3z"},
		//{"M5 2L2 2A2 2 0 0 0 0 0", 2.0, ButtCap, ArcsJoiner{BevelJoin, 10.0}, "M2.8284 1L5 1L5 3L0 3A1 1 0 0 0 1 2A1 1 0 0 0 0 1L0 -1A3 3 0 0 1 2.8284 1z"},
		//{"M5 2L2 2A2 2 0 0 1 0 4", 2.0, ButtCap, ArcsJoiner{BevelJoin, 10.0}, "M2.8284 3A3 3 0 0 1 0 5L0 3A1 1 0 0 0 1 2A1 1 0 0 0 0 1L5 1L5 3z"},

		// cut by limit
		//{"A2 2 0 0 1 2 2L5 2", 2.0, ButtCap, ArcsJoiner{BevelJoin, 1.0}, "M2.8284 1L5 1L5 3L2 3L1 2A1 1 0 0 0 0 1L0 -1A3 3 0 0 1 2.8284 1z"},

		// no intersection
		//{"A2 2 0 0 1 2 2L5 2", 3.0, ButtCap, ArcsJoiner{BevelJoin, 10.0}, "M3.1623 0.5L5 0.5L5 3.5L2 3.5L0.5 2A0.5 0.5 0 0 0 0 1.5L0 -1.5A3.5 3.5 0 0 1 3.1623 0.5z"},
	}
	origEpsilon := Epsilon
	for _, tt := range tts {
		t.Run(tt.orig, func(t *testing.T) {
			Epsilon = origEpsilon
			stroke := MustParseSVGPath(tt.orig).Stroke(tt.w, tt.cp, tt.jr, tolerance)
			Epsilon = 1e-3
			test.T(t, stroke, MustParseSVGPath(tt.stroke))
		})
	}
	Epsilon = origEpsilon
}

func TestPathStrokeEllipse(t *testing.T) {
	rx, ry := 20.0, 10.0
	nphi := 12
	ntheta := 120
	for iphi := 0; iphi < nphi; iphi++ {
		phi := float64(iphi) / float64(nphi) * math.Pi
		for itheta := 0; itheta < ntheta; itheta++ {
			theta := float64(itheta) / float64(ntheta) * 2.0 * math.Pi
			outer := EllipsePos(rx+1.0, ry+1.0, phi, 0.0, 0.0, theta)
			inner := EllipsePos(rx-1.0, ry-1.0, phi, 0.0, 0.0, theta)
			test.Float(t, outer.Sub(inner).Length(), 2.0, fmt.Sprintf("phi=%g theta=%g", phi, theta))
		}
	}
}

func TestPathOffset(t *testing.T) {
	tolerance := 0.01
	var tts = []struct {
		orig   string
		w      float64
		offset string
	}{
		{"L10 0L10 10L0 10z", 0.0, "L10 0L10 10L0 10z"},
		//{"L10 0L10 10L0 10", 1.0, "M0 -1L10 -1A1 1 0 0 1 11 0L11 10A1 1 0 0 1 10 11L0 11"},
		//{"L10 0L10 10L0 10z", 1.0, "M10 -1A1 1 0 0 1 11 0L11 10A1 1 0 0 1 10 11L0 11A1 1 0 0 1 -1 10L-1 0A1 1 0 0 1 0 -1z"},
		{"L10 0L10 10L0 10z", -1.0, "M1 1L9 1L9 9L1 9z"},
		{"L10 0L5 0z", -1.0, ""},
	}
	for _, tt := range tts {
		t.Run(fmt.Sprintf("%v/%v", tt.orig, tt.w), func(t *testing.T) {
			offset := MustParseSVGPath(tt.orig).Offset(tt.w, tolerance)
			test.T(t, offset, MustParseSVGPath(tt.offset))
		})
	}
}

// pathArea returns the net filled area of a settled path: the signed areas of the subpaths sum to
// the filled area, with holes contributing negatively.
func pathArea(p *Path) float64 {
	if p == nil || p.Empty() {
		return 0.0
	}
	area := 0.0
	for _, pi := range p.Split() {
		coords := PolylineFromPathCoords(pi).Coords()
		if len(coords) < 3 {
			continue
		}
		sum := 0.0
		for i := 0; i < len(coords); i++ {
			sum += coords[i].PerpDot(coords[(i+1)%len(coords)])
		}
		area += sum / 2.0
	}
	return math.Abs(area)
}

func TestPathStrokeAligned(t *testing.T) {
	tolerance := 0.001
	w := 4.0

	// rectangle 30x30, centered stroke of width w covers half w on either side of the boundary
	rect := MustParseSVGPath("M0 0H30V30H0z")
	center := rect.Stroke(w, ButtCap, MiterJoin, tolerance)
	band := rect.Stroke(2.0*w, ButtCap, MiterJoin, tolerance)
	inside := rect.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	outside := rect.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentOutside, NonZero)

	// center output is unchanged: a band of half w on either side, so area = (30+w)^2-(30-w)^2
	test.Float(t, pathArea(center), (30.0+w)*(30.0+w)-(30.0-w)*(30.0-w))
	// inside keeps the full width within the 30x30 square: area = 30^2 - (30-2w)^2
	test.Float(t, pathArea(inside), 30.0*30.0-(30.0-2.0*w)*(30.0-2.0*w))
	// outside extends the outer boundary by the full width: area = (30+2w)^2 - 30^2
	test.Float(t, pathArea(outside), (30.0+2.0*w)*(30.0+2.0*w)-30.0*30.0)

	// inside and outside partition the double-width centered band, each covering width w
	test.FloatDiff(t, pathArea(inside)+pathArea(outside), pathArea(band), 1e-9)

	// bounds: inside does not exceed the path, outside extends by the full width
	test.T(t, inside.Bounds(), Rect{0.0, 0.0, 30.0, 30.0})
	test.FloatDiff(t, outside.Bounds().X0, -w, 1e-9)
	test.FloatDiff(t, outside.Bounds().Y0, -w, 1e-9)
	test.FloatDiff(t, outside.Bounds().X1, 30.0+w, 1e-9)
	test.FloatDiff(t, outside.Bounds().Y1, 30.0+w, 1e-9)

	// inside strokes stay within the fill region, outside strokes stay outside
	test.That(t, inside.Not(rect.Settle(NonZero)).Empty(), "inside stroke must stay within the fill region")
	test.That(t, outside.And(rect.Settle(NonZero)).Empty(), "outside stroke must stay outside the fill region")

	// negative width behaves like positive width
	negInside := rect.StrokeAligned(-w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	test.Float(t, pathArea(negInside), pathArea(inside))
}

func TestPathStrokeAlignedHole(t *testing.T) {
	tolerance := 0.001
	w := 4.0

	// annulus: CCW outer square, CW inner square hole (NonZero). The hole is outside of the fill
	// region, so an inside stroke grows away from the hole and an outside stroke grows into it.
	annulus := MustParseSVGPath("M0 0H30V30H0zM10 10V20H20V10z")
	region := annulus.Settle(NonZero)
	test.Float(t, pathArea(region), 30.0*30.0-10.0*10.0)

	inside := annulus.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	outside := annulus.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentOutside, NonZero)

	// the inside stroke contains the inner hole unchanged, plus a band inside the material

	// the inside stroke contains the inner hole unchanged, plus a band inside the material
	test.That(t, inside.Not(region).Empty(), "inside stroke must not extend beyond the fill region")
	test.That(t, inside.And(MustParseSVGPath("M10 10H20V20H10z").Settle(NonZero)).Empty(), "inside stroke must not cover the hole")
	// areas: inside is a band of width w inside both the outer and inner boundary
	test.FloatDiff(t, pathArea(inside), (30.0*30.0-10.0*10.0)-((30.0-2.0*w)*(30.0-2.0*w)-(10.0+2.0*w)*(10.0+2.0*w)), 1e-9)
	// outside is a band of width w outside both boundaries
	test.FloatDiff(t, pathArea(outside), ((30.0+2.0*w)*(30.0+2.0*w)-(10.0-2.0*w)*(10.0-2.0*w))-(30.0*30.0-10.0*10.0), 1e-9)
	// together they partition the double-width centered band
	test.FloatDiff(t, pathArea(inside)+pathArea(outside), pathArea(annulus.Stroke(2.0*w, ButtCap, MiterJoin, tolerance)), 1e-9)
}

func TestPathStrokeAlignedFillRule(t *testing.T) {
	tolerance := 0.001
	w := 4.0

	// same-direction inner square: a hole under EvenOdd, filled under NonZero
	path := MustParseSVGPath("M0 0H30V30H0zM10 10H20V20H10z")
	test.Float(t, pathArea(path.Settle(NonZero)), 30.0*30.0)
	test.Float(t, pathArea(path.Settle(EvenOdd)), 30.0*30.0-10.0*10.0)

	inside := path.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	outsideZero := path.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentOutside, NonZero)
	insideEvenOdd := path.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, EvenOdd)
	outsideEvenOdd := path.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentOutside, EvenOdd)

	// NonZero fills the inner square, so the inside stroke also bands both sides of its boundary
	test.Float(t, pathArea(inside), (30.0*30.0-(30.0-2.0*w)*(30.0-2.0*w))+((10.0+2.0*w)*(10.0+2.0*w)-(10.0-2.0*w)*(10.0-2.0*w)))
	// EvenOdd treats the inner square as a hole, so only the material side of its boundary is banded
	test.Float(t, pathArea(insideEvenOdd), (30.0*30.0-(30.0-2.0*w)*(30.0-2.0*w))+((10.0+2.0*w)*(10.0+2.0*w)-10.0*10.0))
	test.That(t, pathArea(inside) != pathArea(insideEvenOdd), "fill rule must affect aligned stroke")

	// outside strokes partition the double-width centered band for both fill rules
	for _, tt := range []struct {
		fillRule FillRule
		outside  *Path
	}{
		{NonZero, outsideZero},
		{EvenOdd, outsideEvenOdd},
	} {
		band := path.Stroke(2.0*w, ButtCap, MiterJoin, tolerance)
		inside := path.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, tt.fillRule)
		test.FloatDiff(t, pathArea(inside)+pathArea(tt.outside), pathArea(band), 1e-9)
	}
}

func TestPathStrokeAlignedSelfIntersect(t *testing.T) {
	tolerance := 0.001
	w := 4.0

	// bowtie: two triangles in opposite winding joined at the crossing
	bowtie := MustParseSVGPath("M0 0L20 20L20 0L0 20z")
	for _, fillRule := range []FillRule{NonZero, EvenOdd} {
		inside := bowtie.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, fillRule)
		outside := bowtie.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentOutside, fillRule)

		test.That(t, inside.Sane(), "inside stroke must be sane")
		test.That(t, outside.Sane(), "outside stroke must be sane")
		test.That(t, inside.Not(bowtie.Settle(fillRule)).Empty(), "inside stroke must stay within the fill region")
		test.FloatDiff(t, pathArea(inside)+pathArea(outside), pathArea(bowtie.Stroke(2.0*w, ButtCap, MiterJoin, tolerance)), 1e-9)
	}
}

func TestPathStrokeAlignedOpen(t *testing.T) {
	tolerance := 0.001
	w := 4.0

	// an open path without enclosed area has an empty inside stroke and an outside stroke equal
	// to the centered stroke, so that a line is not doubled in width
	line := MustParseSVGPath("M0 0H10")
	test.That(t, line.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero).Empty(), "inside stroke of a line must be empty")
	test.T(t, line.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentOutside, NonZero),
		line.Stroke(w, ButtCap, MiterJoin, tolerance))

	// an open path that does enclose an area is implicitly closed for its fill region
	open := MustParseSVGPath("M0 0H20V20")
	region := open.Settle(NonZero)
	test.Float(t, pathArea(region), 200.0)
	inside := open.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	test.That(t, !inside.Empty(), "inside stroke of an open rectangle must exist")
	test.That(t, inside.Not(region).Empty(), "inside stroke must stay within the fill region")

	// caps apply to the outline, the square cap extends outside and is clipped for inside
	for _, cr := range []Capper{ButtCap, SquareCap, RoundCap} {
		inside := open.StrokeAligned(w, cr, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
		outside := open.StrokeAligned(w, cr, MiterJoin, tolerance, StrokeAlignmentOutside, NonZero)
		test.That(t, !inside.Empty(), "inside stroke must exist for", cr)
		test.That(t, !outside.Empty(), "outside stroke must exist for", cr)
		test.That(t, inside.Not(region).Empty(), "inside stroke must stay inside for", cr)
	}
}

func TestPathStrokeAlignedBezierAndArc(t *testing.T) {
	tolerance := 0.001
	w := 4.0

	// a closed Bézier blob: the aligned strokes stay on their respective sides of the fill region
	blob := MustParseSVGPath("M0 0C10 20 30 20 40 0C30 -20 10 -20 0 0z")
	region := blob.Settle(NonZero)
	test.That(t, !region.Empty(), "the blob must enclose an area")
	for _, jr := range []Joiner{RoundJoin, BevelJoin, MiterJoin} {
		inside := blob.StrokeAligned(w, RoundCap, jr, tolerance, StrokeAlignmentInside, NonZero)
		outside := blob.StrokeAligned(w, RoundCap, jr, tolerance, StrokeAlignmentOutside, NonZero)
		test.That(t, inside.Sane(), "inside Blob stroke must be sane for", jr)
		test.That(t, outside.Sane(), "outside Blob stroke must be sane for", jr)
		test.That(t, inside.Not(region).Empty(), "inside Blob stroke must stay within the fill region")
		test.That(t, outside.And(region).Empty(), "outside Blob stroke must stay outside the fill region")
	}

	// a circle made of two arcs: inside area for a stroke of width w is pi*(R^2-(R-w)^2)
	radius := 10.0
	circle := MustParseSVGPath("M10 0A10 10 0 1 1 -10 0A10 10 0 1 1 10 0z")
	inside := circle.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	outside := circle.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentOutside, NonZero)
	test.FloatDiff(t, pathArea(inside), math.Pi*(radius*radius-(radius-w)*(radius-w)), 0.1)
	test.FloatDiff(t, pathArea(outside), math.Pi*((radius+w)*(radius+w)-radius*radius), 0.1)
}

func TestPathStrokeAlignedJoins(t *testing.T) {
	tolerance := 0.001
	w := 4.0

	// round, bevel, and miter joins all stay on the same side of the fill region
	square := MustParseSVGPath("M0 0H30V30H0z")
	region := square.Settle(NonZero)
	for _, jr := range []Joiner{BevelJoin, RoundJoin, MiterJoin, MiterJoiner{BevelJoin, 10.0}, ArcsJoin} {
		inside := square.StrokeAligned(w, ButtCap, jr, tolerance, StrokeAlignmentInside, NonZero)
		outside := square.StrokeAligned(w, ButtCap, jr, tolerance, StrokeAlignmentOutside, NonZero)
		test.That(t, inside.Sane(), "inside stroke must be sane for", jr)
		test.That(t, outside.Sane(), "outside stroke must be sane for", jr)
		test.That(t, inside.Not(region).Empty(), "inside stroke must stay inside for", jr)
		test.That(t, outside.And(region).Empty(), "outside stroke must stay outside for", jr)
	}
	// the miter limit clips the 90 degree corners: a limit of 1 yields a bevel and thus a smaller
	// area than a limit of 4 (the default), and both remain outside the fill region
	small := square.StrokeAligned(w, ButtCap, MiterJoiner{BevelJoin, 1.0}, tolerance, StrokeAlignmentOutside, NonZero)
	large := square.StrokeAligned(w, ButtCap, MiterJoiner{BevelJoin, 10.0}, tolerance, StrokeAlignmentOutside, NonZero)
	test.That(t, pathArea(small) < pathArea(large), "a smaller miter limit must produce a bevel and less area")
	test.That(t, small.And(region).Empty(), "clipped join must stay outside")
	test.That(t, large.And(region).Empty(), "miter join must stay outside")
}

func TestPathStrokeAlignedDashes(t *testing.T) {
	tolerance := 0.001
	rect := MustParseSVGPath("M0 0H30V30H0z")
	original := rect.Copy()
	dashed := rect.Dash(3.0, 5.0, 3.0)

	// aligned-to keeps using the original fill region while stroking the dashed path
	inside := dashed.StrokeAlignedTo(original, 4.0, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	outside := dashed.StrokeAlignedTo(original, 4.0, ButtCap, MiterJoin, tolerance, StrokeAlignmentOutside, NonZero)

	test.That(t, !inside.Empty(), "dashed inside stroke must not be empty")
	test.That(t, !outside.Empty(), "dashed outside stroke must not be empty")
	test.That(t, inside.Not(original.Settle(NonZero)).Empty(), "dashed inside stroke must stay within the fill region")
	test.FloatDiff(t, pathArea(inside)+pathArea(outside), pathArea(dashed.Stroke(8.0, ButtCap, MiterJoin, tolerance)), 1e-9)
	// the dash pattern is preserved: the dashed stroke covers less than the solid stroke
	solidInside := original.StrokeAligned(4.0, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	test.That(t, pathArea(inside) < pathArea(solidInside), "dashes must reduce the stroke area")
	test.That(t, !inside.Empty() && !outside.Empty(), "dashes must not remove the stroke entirely")
}

func TestPathStrokeAlignedSharpCorners(t *testing.T) {
	tolerance := 0.001

	// a very sharp wedge with a huge miter limit produces an enormous centered stroke, which is
	// clipped by the inside alignment and must not allocate beyond the bound
	wedge := &Path{}
	wedge.MoveTo(0.0, 0.0)
	wedge.LineTo(10.0, 0.2)
	wedge.LineTo(10.0, -0.2)
	wedge.Close()
	joiner := MiterJoiner{BevelJoin, 1e6}
	inside := wedge.StrokeAligned(2.0, ButtCap, joiner, tolerance, StrokeAlignmentInside, NonZero)
	test.That(t, inside.Sane(), "sharp inside stroke must be sane")
	test.That(t, inside.Not(wedge.Settle(NonZero)).Empty(), "inside stroke must stay within the fill region")

	// a miter limit of 4 (the default) clips the sharp corner and is still stable
	for _, jr := range []Joiner{BevelJoin, RoundJoin, MiterJoin, ArcsJoin} {
		q := wedge.StrokeAligned(2.0, RoundCap, jr, tolerance, StrokeAlignmentOutside, NonZero)
		test.That(t, q.Sane(), "sharp outside stroke must be sane for", jr)
	}
}

func TestPathStrokeAlignedDegenerate(t *testing.T) {
	tolerance := 0.001
	square := MustParseSVGPath("M0 0H30V30H0z")

	for _, w := range []float64{0.0, -0.0, 1e-300, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, align := range []StrokeAlignment{StrokeAlignmentInside, StrokeAlignmentOutside} {
			q := square.StrokeAligned(w, ButtCap, MiterJoin, tolerance, align, NonZero)
			test.That(t, q.Empty(), "width must result in an empty path for alignment", w, align)
		}
	}
	for _, tol := range []float64{0.0, -1.0, math.NaN(), math.Inf(1)} {
		q := square.StrokeAligned(2.0, ButtCap, MiterJoin, tol, StrokeAlignmentInside, NonZero)
		test.That(t, q.Empty(), "tolerance must result in an empty path", tol)
	}

	// non-finite and excessively large coordinates must be rejected without allocating
	invalid := &Path{}
	invalid.MoveTo(math.NaN(), 0.0)
	invalid.LineTo(10.0, 0.0)
	invalid.Close()
	test.That(t, invalid.StrokeAligned(2.0, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero).Empty(), "NaN coordinates must result in an empty path")

	huge := &Path{}
	huge.MoveTo(0.0, 0.0)
	huge.LineTo(1e15, 0.0)
	huge.LineTo(1e15, 1e15)
	huge.Close()
	test.That(t, huge.StrokeAligned(2.0, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero).Empty(), "huge coordinates must result in an empty path")

	// extremely small tolerance with curves would explode the number of segments
	curve := &Path{}
	curve.MoveTo(0.0, 0.0)
	curve.CubeTo(10.0, 10.0, 20.0, -10.0, 30.0, 0.0)
	curve.LineTo(30.0, 30.0)
	curve.Close()
	test.That(t, curve.StrokeAligned(2.0, ButtCap, MiterJoin, 1e-300, StrokeAlignmentInside, NonZero).Empty(), "tiny tolerance must result in an empty path")

	// empty paths and unknown alignment
	test.That(t, (&Path{}).StrokeAligned(2.0, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero).Empty(), "empty path must result in an empty stroke")
	unknown := square.StrokeAligned(2.0, ButtCap, MiterJoin, tolerance, StrokeAlignment(42), NonZero)
	test.T(t, unknown, square.Stroke(2.0, ButtCap, MiterJoin, tolerance), "unknown alignment must behave as center")
}

func TestPathStrokeAlignedNegativeCoordinates(t *testing.T) {
	tolerance := 0.001
	w := 4.0

	// the same shape translated to negative coordinates must give the same stroke geometry
	positive := MustParseSVGPath("M0 0H30V30H0z")
	negative := positive.Copy().Translate(-100.0, -100.0)
	insidePositive := positive.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	insideNegative := negative.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	test.Float(t, pathArea(insideNegative), pathArea(insidePositive))
	test.T(t, insideNegative.Translate(100.0, 100.0).Settle(NonZero), insidePositive.Settle(NonZero))

	// coordinates straddling zero
	straddle := positive.Copy().Translate(-15.0, -15.0)
	insideStraddle := straddle.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	test.Float(t, pathArea(insideStraddle), pathArea(insidePositive))
}

func TestPathStrokeAlignedTransform(t *testing.T) {
	tolerance := 0.001
	w := 4.0
	square := MustParseSVGPath("M0 0H30V30H0z")

	// the alignment is resolved in path space, so a uniform scale scales the stroke outline by
	// exactly the same factor
	inside := square.StrokeAligned(w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	scaled := inside.Copy().Scale(3.0, 3.0)
	direct := square.Copy().Scale(3.0, 3.0).StrokeAligned(3.0*w, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	test.FloatDiff(t, pathArea(scaled), pathArea(direct), 1e-6)

	// a non-uniform scale is applied to the resolved outline, which is what renderers do
	stretched := inside.Copy().Scale(2.0, 0.5)
	test.FloatDiff(t, pathArea(stretched), pathArea(inside)*2.0*0.5, 1e-9)
}

func TestPathStrokeStyle(t *testing.T) {
	tolerance := 0.001

	// center alignment must match Path.Stroke with scaled dashes exactly
	rect := MustParseSVGPath("M0 0H30V30H0z")
	style := DefaultStyle
	style.Stroke = Paint{Color: Black}
	style.StrokeWidth = 4.0
	style.StrokeCapper = ButtCap
	style.StrokeJoiner = MiterJoin
	style.StrokeAlign = StrokeAlignmentCenter
	test.T(t, rect.StrokeStyle(style, tolerance), rect.Stroke(4.0, ButtCap, MiterJoin, tolerance))

	style.Dashes = []float64{1.0, 1.0}
	style.DashOffset = 0.5
	dashOffset, dashes := ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
	test.T(t, rect.StrokeStyle(style, tolerance),
		rect.Dash(dashOffset, dashes...).Stroke(4.0, ButtCap, MiterJoin, tolerance))

	// aligned styles use the original undashed path as fill region
	style.StrokeAlign = StrokeAlignmentInside
	combined := rect.StrokeStyle(style, tolerance)
	expected := rect.Dash(dashOffset, dashes...).StrokeAlignedTo(rect, 4.0, ButtCap, MiterJoin, tolerance, StrokeAlignmentInside, NonZero)
	test.T(t, combined, expected)

	// unknown alignment is treated as center
	style.StrokeAlign = StrokeAlignment(42)
	style.Dashes = nil
	test.T(t, rect.StrokeStyle(style, tolerance), rect.Stroke(4.0, ButtCap, MiterJoin, tolerance))
}

func TestStyleIsAligned(t *testing.T) {
	test.That(t, !DefaultStyle.IsAligned(), "default style must not be aligned")
	for _, tt := range []struct {
		align StrokeAlignment
		want  bool
	}{
		{StrokeAlignmentCenter, false},
		{StrokeAlignmentInside, true},
		{StrokeAlignmentOutside, true},
		{StrokeAlignment(42), false},
	} {
		style := DefaultStyle
		style.StrokeAlign = tt.align
		test.T(t, style.IsAligned(), tt.want, tt.align)
	}
}

func TestStrokeAlignmentString(t *testing.T) {
	test.String(t, StrokeAlignmentCenter.String(), "Center")
	test.String(t, StrokeAlignmentInside.String(), "Inside")
	test.String(t, StrokeAlignmentOutside.String(), "Outside")
	test.String(t, StrokeAlignment(42).String(), "StrokeAlignment(42)")
}

func TestContextStrokeAlign(t *testing.T) {
	c := New(100.0, 100.0)
	ctx := NewContext(c)
	test.T(t, ctx.Style.StrokeAlign, StrokeAlignmentCenter)

	ctx.SetStrokeAlign(StrokeAlignmentInside)
	test.T(t, ctx.Style.StrokeAlign, StrokeAlignmentInside)
	ctx.Push()
	ctx.SetStrokeAlign(StrokeAlignmentOutside)
	ctx.Pop()
	test.T(t, ctx.Style.StrokeAlign, StrokeAlignmentInside)
	ctx.ResetStyle()
	test.T(t, ctx.Style.StrokeAlign, StrokeAlignmentCenter)
}
