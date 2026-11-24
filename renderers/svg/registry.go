package svg

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
)

// ResourceKind identifies the type of a reusable SVG resource such as a gradient, pattern, clip path, mask or embedded image.
type ResourceKind int

// Resource kinds.
const (
	GradientResource ResourceKind = iota
	PatternResource
	ClipPathResource
	MaskResource
	ImageResource
)

func (kind ResourceKind) String() string {
	switch kind {
	case GradientResource:
		return "gradient"
	case PatternResource:
		return "pattern"
	case ClipPathResource:
		return "clipPath"
	case MaskResource:
		return "mask"
	case ImageResource:
		return "image"
	}
	return "unknown"
}

// idPrefix returns the ID prefix used for resources of this kind. Gradients and masks use the same prefixes as the legacy (non-deduplicating) writer.
func (kind ResourceKind) idPrefix() string {
	switch kind {
	case GradientResource:
		return "d"
	case PatternResource:
		return "p"
	case ClipPathResource:
		return "c"
	case MaskResource:
		return "m"
	case ImageResource:
		return "i"
	}
	return "r"
}

// Errors returned by the resource registry and the SVG renderer.
var (
	// ErrResourceIDConflict is returned when a resource ID is already taken by a resource with different content.
	ErrResourceIDConflict = errors.New("resource ID conflict")
	// ErrResourceCircularReference is returned when resources reference each other directly or indirectly.
	ErrResourceCircularReference = errors.New("circular resource reference")
	// ErrResourceTooLarge is returned when a resource definition exceeds the maximum resource size.
	ErrResourceTooLarge = errors.New("resource exceeds maximum size")
	// ErrResourceUnknownReference is returned when a resource references a resource that was never registered.
	ErrResourceUnknownReference = errors.New("unknown resource reference")
	// ErrClosed is returned when the renderer is used after it was closed.
	ErrClosed = errors.New("renderer already closed")
)

// defaultMaxResourceSize is the default upper bound on the size of a single resource definition in bytes.
const defaultMaxResourceSize = 64 << 20 // 64 MiB

// resource is a single registered SVG definition.
type resource struct {
	kind ResourceKind
	id   string
	key  [sha1.Size]byte
	def  []byte
	refs []string // IDs of other resources referenced by def
}

// resourceRegistry deduplicates SVG resources (gradients, patterns, clip paths, masks and images) by content and color space, and assigns document-wide stable IDs in registration order. A registry is valid for a single document only and must not be shared between documents (pages).
type resourceRegistry struct {
	colorSpace string
	maxSize    int
	byKey      map[[sha1.Size]byte]string
	resources  map[string]*resource
	order      []string // IDs in registration order, for stable output
	seq        map[ResourceKind]int
	err        error // sticky error
}

// newResourceRegistry returns a resource registry. Resources with identical content but a different colorSpace never share a key. maxSize bounds the size of a single resource definition in bytes; values <= 0 select the default.
func newResourceRegistry(colorSpace string, maxSize int) *resourceRegistry {
	if maxSize <= 0 {
		maxSize = defaultMaxResourceSize
	}
	return &resourceRegistry{
		colorSpace: colorSpace,
		maxSize:    maxSize,
		byKey:      map[[sha1.Size]byte]string{},
		resources:  map[string]*resource{},
		seq:        map[ResourceKind]int{},
	}
}

// key returns the deduplication key for a resource of the given kind and content. The key covers the resource kind, the color space and the content, so that only truly identical resources in the same color space are merged.
func (reg *resourceRegistry) key(kind ResourceKind, content []byte) [sha1.Size]byte {
	h := sha1.New()
	h.Write([]byte{byte(kind)})
	h.Write([]byte(reg.colorSpace))
	h.Write([]byte{0x00})
	h.Write(content)
	var key [sha1.Size]byte
	h.Sum(key[:0])
	return key
}

// register returns the ID of the resource with the given content, registering it if it is new. The define callback is invoked with the newly assigned ID to produce the resource's definition XML; it is only called for new resources. refs lists the IDs of other registered resources referenced by the definition.
func (reg *resourceRegistry) register(kind ResourceKind, content []byte, refs []string, define func(id string) []byte) (string, error) {
	if reg.err != nil {
		return "", reg.err
	}
	key := reg.key(kind, content)
	if id, ok := reg.byKey[key]; ok {
		return id, nil
	}
	id := fmt.Sprintf("%s%d", kind.idPrefix(), reg.seq[kind])
	reg.seq[kind]++
	return reg.store(kind, id, key, refs, define)
}

// registerWithID is like register but registers the resource under an explicit ID. If the ID is already taken by a resource with different content, an ErrResourceIDConflict error is returned; if the content is identical, the existing resource is reused.
func (reg *resourceRegistry) registerWithID(kind ResourceKind, id string, content []byte, refs []string, define func(id string) []byte) (string, error) {
	if reg.err != nil {
		return "", reg.err
	}
	key := reg.key(kind, content)
	if existing, ok := reg.resources[id]; ok {
		if existing.key == key {
			return id, nil
		}
		reg.err = fmt.Errorf("svg: %s %q: %w", kind, id, ErrResourceIDConflict)
		return "", reg.err
	}
	return reg.store(kind, id, key, refs, define)
}

func (reg *resourceRegistry) store(kind ResourceKind, id string, key [sha1.Size]byte, refs []string, define func(id string) []byte) (string, error) {
	if existing, ok := reg.resources[id]; ok {
		if existing.key == key {
			return id, nil
		}
		reg.err = fmt.Errorf("svg: %s %q: %w", kind, id, ErrResourceIDConflict)
		return "", reg.err
	}
	def := define(id)
	if reg.maxSize < len(def) {
		reg.err = fmt.Errorf("svg: %s %q is %d bytes, maximum is %d bytes: %w", kind, id, len(def), reg.maxSize, ErrResourceTooLarge)
		return "", reg.err
	}
	reg.resources[id] = &resource{kind: kind, id: id, key: key, def: def, refs: refs}
	reg.byKey[key] = id
	reg.order = append(reg.order, id)
	return id, nil
}

// len returns the number of registered resources.
func (reg *resourceRegistry) len() int {
	return len(reg.order)
}

// size returns the total size of all resource definitions in bytes.
func (reg *resourceRegistry) size() (n int) {
	for _, id := range reg.order {
		n += len(reg.resources[id].def)
	}
	return
}

// validate checks that all references between resources resolve and are free of cycles.
func (reg *resourceRegistry) validate() error {
	for _, id := range reg.order {
		res := reg.resources[id]
		for _, ref := range res.refs {
			if _, ok := reg.resources[ref]; !ok {
				return fmt.Errorf("svg: %s %q references %q: %w", res.kind, id, ref, ErrResourceUnknownReference)
			}
		}
	}

	const (
		white = iota // unvisited
		gray         // on the current DFS path
		black        // fully explored
	)
	state := map[string]int{}
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case gray:
			res := reg.resources[id]
			return fmt.Errorf("svg: %s %q: %w", res.kind, id, ErrResourceCircularReference)
		case black:
			return nil
		}
		state[id] = gray
		for _, ref := range reg.resources[id].refs {
			if err := visit(ref); err != nil {
				return err
			}
		}
		state[id] = black
		return nil
	}
	for _, id := range reg.order {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// writeTo writes all registered resources as a single defs element in registration order, after validating the reference graph.
func (reg *resourceRegistry) writeTo(w io.Writer) error {
	if reg.err != nil {
		return reg.err
	}
	if err := reg.validate(); err != nil {
		reg.err = err
		return err
	}
	if len(reg.order) == 0 {
		return nil
	}
	if _, err := io.WriteString(w, "<defs>"); err != nil {
		return fmt.Errorf("svg: writing defs: %w", err)
	}
	for _, id := range reg.order {
		if _, err := w.Write(reg.resources[id].def); err != nil {
			return fmt.Errorf("svg: writing %s %q: %w", reg.resources[id].kind, id, err)
		}
	}
	if _, err := io.WriteString(w, "</defs>"); err != nil {
		return fmt.Errorf("svg: writing defs: %w", err)
	}
	return nil
}

// trackedWriter wraps a writer and records the first write error, converting short writes into io.ErrShortWrite. This allows render methods that cannot return errors to surface write failures at Close.
type trackedWriter struct {
	w   io.Writer
	err error
}

func (t *trackedWriter) Write(p []byte) (int, error) {
	if t.err != nil {
		return 0, t.err
	}
	n, err := t.w.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		t.err = err
	}
	return n, err
}
