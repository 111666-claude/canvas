package svg

import (
	"errors"
	"strings"
	"testing"
)

func defineEcho(id string) []byte {
	return []byte(`<def id="` + id + `"/>`)
}

func TestRegistryDedup(t *testing.T) {
	reg := newResourceRegistry("srgb", 0)

	id1, err := reg.register(GradientResource, []byte("gradient-a"), nil, defineEcho)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := reg.register(GradientResource, []byte("gradient-b"), nil, defineEcho)
	if err != nil {
		t.Fatal(err)
	}
	id1again, err := reg.register(GradientResource, []byte("gradient-a"), nil, defineEcho)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != "d0" || id2 != "d1" {
		t.Fatalf("unexpected gradient IDs %q, %q", id1, id2)
	}
	if id1again != id1 {
		t.Fatalf("identical content not deduplicated: %q != %q", id1again, id1)
	}

	// identical content under a different kind must not be merged
	mid, err := reg.register(MaskResource, []byte("gradient-a"), nil, defineEcho)
	if err != nil {
		t.Fatal(err)
	}
	if mid != "m0" {
		t.Fatalf("unexpected mask ID %q", mid)
	}

	// remaining kinds get their own prefixes and sequences
	for kind, want := range map[ResourceKind]string{
		PatternResource:  "p0",
		ClipPathResource: "c0",
		ImageResource:    "i0",
	} {
		id, err := reg.register(kind, []byte("x"), nil, defineEcho)
		if err != nil {
			t.Fatal(err)
		}
		if id != want {
			t.Fatalf("unexpected %s ID %q, want %q", kind, id, want)
		}
	}

	if reg.len() != 6 {
		t.Fatalf("unexpected resource count %d", reg.len())
	}

	// resources are emitted in registration order in a single defs element
	sb := &strings.Builder{}
	if err := reg.writeTo(sb); err != nil {
		t.Fatal(err)
	}
	want := `<defs><def id="d0"/><def id="d1"/><def id="m0"/><def id="p0"/><def id="c0"/><def id="i0"/></defs>`
	if sb.String() != want {
		t.Fatalf("unexpected defs output:\ngot  %s\nwant %s", sb.String(), want)
	}
}

func TestRegistrySharedClipPath(t *testing.T) {
	// a clip path shared by many elements is registered once
	reg := newResourceRegistry("srgb", 0)
	clip := []byte("M0 0H10V10H0z")
	define := func(id string) []byte {
		return []byte(`<clipPath id="` + id + `"><path d="` + string(clip) + `"/></clipPath>`)
	}
	var first string
	for i := 0; i < 5; i++ {
		id, err := reg.register(ClipPathResource, clip, nil, define)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		} else if id != first {
			t.Fatalf("shared clip path not deduplicated: %q != %q", id, first)
		}
	}
	if reg.len() != 1 {
		t.Fatalf("expected a single clip path definition, got %d", reg.len())
	}
	sb := &strings.Builder{}
	if err := reg.writeTo(sb); err != nil {
		t.Fatal(err)
	}
	if strings.Count(sb.String(), "<clipPath") != 1 {
		t.Fatalf("expected one clipPath in output: %s", sb.String())
	}
}

func TestRegistryColorSpace(t *testing.T) {
	// identical content in different color spaces must never share a key
	srgb := newResourceRegistry("srgb", 0)
	linear := newResourceRegistry("linear", 0)
	content := []byte("same-content")
	if srgb.key(GradientResource, content) == linear.key(GradientResource, content) {
		t.Fatal("color space not included in deduplication key")
	}
	// and different kinds must not share a key either
	if srgb.key(GradientResource, content) == srgb.key(MaskResource, content) {
		t.Fatal("resource kind not included in deduplication key")
	}
}

func TestRegistryIDConflict(t *testing.T) {
	reg := newResourceRegistry("srgb", 0)
	if _, err := reg.registerWithID(GradientResource, "custom", []byte("a"), nil, defineEcho); err != nil {
		t.Fatal(err)
	}
	// same ID with different content conflicts
	if _, err := reg.registerWithID(GradientResource, "custom", []byte("b"), nil, defineEcho); !errors.Is(err, ErrResourceIDConflict) {
		t.Fatalf("expected ErrResourceIDConflict, got %v", err)
	}
	// the error is sticky
	if _, err := reg.register(GradientResource, []byte("c"), nil, defineEcho); !errors.Is(err, ErrResourceIDConflict) {
		t.Fatalf("expected sticky ErrResourceIDConflict, got %v", err)
	}

	// same ID with identical content is a dedup hit
	reg = newResourceRegistry("srgb", 0)
	if _, err := reg.registerWithID(GradientResource, "custom", []byte("a"), nil, defineEcho); err != nil {
		t.Fatal(err)
	}
	id, err := reg.registerWithID(GradientResource, "custom", []byte("a"), nil, defineEcho)
	if err != nil || id != "custom" {
		t.Fatalf("expected dedup hit on identical content, got %q, %v", id, err)
	}

	// an automatically assigned ID colliding with an explicit ID conflicts
	reg = newResourceRegistry("srgb", 0)
	if _, err := reg.registerWithID(GradientResource, "d0", []byte("a"), nil, defineEcho); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.register(GradientResource, []byte("b"), nil, defineEcho); !errors.Is(err, ErrResourceIDConflict) {
		t.Fatalf("expected ErrResourceIDConflict on auto ID collision, got %v", err)
	}
}

func TestRegistryCircularReference(t *testing.T) {
	// mutual references
	reg := newResourceRegistry("srgb", 0)
	if _, err := reg.registerWithID(PatternResource, "pa", []byte("a"), []string{"pb"}, defineEcho); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.registerWithID(PatternResource, "pb", []byte("b"), []string{"pa"}, defineEcho); err != nil {
		t.Fatal(err)
	}
	if err := reg.writeTo(&strings.Builder{}); !errors.Is(err, ErrResourceCircularReference) {
		t.Fatalf("expected ErrResourceCircularReference, got %v", err)
	}

	// self reference
	reg = newResourceRegistry("srgb", 0)
	if _, err := reg.registerWithID(MaskResource, "m0", []byte("a"), []string{"m0"}, defineEcho); err != nil {
		t.Fatal(err)
	}
	if err := reg.writeTo(&strings.Builder{}); !errors.Is(err, ErrResourceCircularReference) {
		t.Fatalf("expected ErrResourceCircularReference for self reference, got %v", err)
	}

	// acyclic references are fine
	reg = newResourceRegistry("srgb", 0)
	if _, err := reg.registerWithID(MaskResource, "m0", []byte("a"), nil, defineEcho); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.registerWithID(ImageResource, "i0", []byte("b"), []string{"m0"}, defineEcho); err != nil {
		t.Fatal(err)
	}
	if err := reg.writeTo(&strings.Builder{}); err != nil {
		t.Fatalf("unexpected error for acyclic references: %v", err)
	}
}

func TestRegistryUnknownReference(t *testing.T) {
	reg := newResourceRegistry("srgb", 0)
	if _, err := reg.registerWithID(ImageResource, "i0", []byte("a"), []string{"m9"}, defineEcho); err != nil {
		t.Fatal(err)
	}
	if err := reg.writeTo(&strings.Builder{}); !errors.Is(err, ErrResourceUnknownReference) {
		t.Fatalf("expected ErrResourceUnknownReference, got %v", err)
	}
}

func TestRegistryMaxSize(t *testing.T) {
	reg := newResourceRegistry("srgb", 16)
	_, err := reg.register(ImageResource, []byte("big"), nil, func(id string) []byte {
		return make([]byte, 100)
	})
	if !errors.Is(err, ErrResourceTooLarge) {
		t.Fatalf("expected ErrResourceTooLarge, got %v", err)
	}
	// the oversized resource was not registered
	if reg.len() != 0 {
		t.Fatalf("oversized resource was registered")
	}
	// the error is sticky and reported by writeTo
	if _, err := reg.register(GradientResource, []byte("g"), nil, defineEcho); !errors.Is(err, ErrResourceTooLarge) {
		t.Fatalf("expected sticky ErrResourceTooLarge, got %v", err)
	}
	if err := reg.writeTo(&strings.Builder{}); !errors.Is(err, ErrResourceTooLarge) {
		t.Fatalf("expected ErrResourceTooLarge from writeTo, got %v", err)
	}

	// exactly at the limit is fine
	reg = newResourceRegistry("srgb", 16)
	if _, err := reg.register(ImageResource, []byte("ok"), nil, func(id string) []byte {
		return make([]byte, 16)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryEmpty(t *testing.T) {
	reg := newResourceRegistry("srgb", 0)
	sb := &strings.Builder{}
	if err := reg.writeTo(sb); err != nil {
		t.Fatal(err)
	}
	if sb.String() != "" {
		t.Fatalf("expected no output for empty registry, got %q", sb.String())
	}
}
