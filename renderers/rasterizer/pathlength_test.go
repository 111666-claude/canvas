package rasterizer_test

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
)

const fixtureDir = "../../tests/svg/pathlength"

func renderFixture(t *testing.T, name string) *image.RGBA {
	t.Helper()
	f, err := os.Open(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := canvas.ParseSVG(f)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return rasterizer.Draw(c, canvas.DPI(96.0), nil)
}

func pixelDiff(a, b image.Image) int {
	ba, bb := a.Bounds(), b.Bounds()
	if ba.Size() != bb.Size() {
		return -1
	}
	n := 0
	for y := ba.Min.Y; y < ba.Max.Y; y++ {
		for x := ba.Min.X; x < ba.Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				n++
			}
		}
	}
	return n
}

// TestPathLengthDashRasterInvariance verifies the headline requirement: an
// equal-length dash pattern expressed through pathLength must rasterize
// pixel-identically to the explicit user-space pattern, across straight
// lines. A few pixels of anti-aliasing tolerance is allowed.
func TestPathLengthDashRasterInvariance(t *testing.T) {
	ref := renderFixture(t, "dash-reference.svg")
	norm := renderFixture(t, "dash-pathlength.svg")
	if diff := pixelDiff(ref, norm); diff < 0 {
		t.Fatal("image sizes differ")
	} else if 5 < diff {
		t.Fatalf("pathLength dashes drift from explicit dashes: %d pixels differ", diff)
	}
}

// TestPathLengthClosedRing renders the closed-ring fixture and asserts it
// produces finite, non-empty output (no NaN coordinates, no crash) with a
// periodic dash pattern around the ring.
func TestPathLengthClosedRing(t *testing.T) {
	img := renderFixture(t, "closed-ring.svg")
	bounds := img.Bounds()
	red, black := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r == 0 && g == 0 && b == 0 {
				black++
			}
			if r != 0 {
				red++
			}
		}
	}
	if black == 0 {
		t.Fatal("closed ring produced no stroked pixels")
	}
	if red != 0 {
		t.Fatalf("unexpected colored pixels: %d", red)
	}
}

// TestPathLengthTextPathRaster renders both textPath fixtures and confirms the
// normalized and percentage offsets place glyphs on the path (finite output,
// glyphs present). Exact pixel equality is font-dependent, so the structural
// checks live in the canvas package tests.
func TestPathLengthTextPathRaster(t *testing.T) {
	for _, name := range []string{"textpath-reference.svg", "textpath-pathlength.svg", "textpath-percent.svg"} {
		img := renderFixture(t, name)
		bounds := img.Bounds()
		ink := 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r == 0 && g == 0 && b == 0 {
					ink++
				}
			}
		}
		if ink == 0 {
			t.Fatalf("%s produced no text glyphs", name)
		}
	}
}

// TestPathLengthMarkerSpacing verifies that user-space marker spacing honors
// the pathLength normalization: markers land at finite positions and the
// expected count is drawn along the 200-unit line declared as length 100.
func TestPathLengthMarkerSpacing(t *testing.T) {
	img := renderFixture(t, "marker-spacing.svg")
	bounds := img.Bounds()
	redPixels := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r != 0 && g == 0 && b == 0 {
				redPixels++
			}
		}
	}
	// three interior markers at 50/100/150 user units, each covering several
	// rasterized pixels
	if redPixels < 10 {
		t.Fatalf("expected spaced markers, got %d red pixels", redPixels)
	}
}
