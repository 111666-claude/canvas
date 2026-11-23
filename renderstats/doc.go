// Package renderstats instruments a canvas.Renderer and records repeatable,
// per-operation rendering statistics: operation counts, bounding boxes, clip
// hits, rasterization time, estimated peak temporary buffer, and stable
// resource IDs. Reports can be exported per frame as versioned JSON.
//
// The package is strictly additive. It does not modify the canvas package, its
// public API, drawing order, or pixels. A Recorder implements canvas.Renderer
// and forwards every call to the wrapped renderer exactly once, in the same
// order, with the same arguments, while collecting statistics on the side.
//
// # Recorded statistics
//
// For every RenderPath, RenderText, and RenderImage call the Recorder records:
//
//   - a per-frame operation count (paths, texts, images, total);
//   - the local bounding box (in the operation's own coordinate space, with
//     stroke width expanded for paths) and the world bounding box after the
//     operation matrix;
//   - whether the world box lies inside, partially inside, or outside the
//     statistic-only clip window (see [Recorder.SetClip]);
//   - the wall-clock time spent inside the wrapped renderer;
//   - an estimate of temporary buffer bytes used by the operation;
//   - a content-derived resource ID that is stable across runs.
//
// Timing that is negative, zero-length on a broken clock, or otherwise not
// usable is reported with valid=false and a zero duration; it is never
// serialized as a non-finite JSON number.
//
// # Lifecycle
//
//   - A Recorder starts with no frame. The first render call opens an implicit
//     frame named "frame-N". BeginFrame auto-closes any open frame and starts a
//     named one. EndFrame closes the open frame. RenderCanvas performs
//     BeginFrame, Canvas.RenderTo, and EndFrame.
//   - Cancel closes the currently open frame as canceled. Calls are still
//     forwarded to the wrapped renderer, and the next draw starts a fresh
//     frame. Cancel never changes drawing.
//   - Canvas.Reset, Canvas.Clip, Canvas.Transform, and Context.Push/Pop
//     (canvas's save/restore mechanism) only affect canvas state or layers.
//     They never reach a Renderer, so they neither add operations nor reset
//     recorded statistics. Only Recorder.Reset clears recorded frames.
//   - An empty canvas produces a frame with zero operations; its op list is
//     serialized as [] rather than null.
//   - Image bounds are used as-is, including non-zero Min values.
//   - SetClip sets a statistic-only clip window. It never clips rendering. Call
//     it between frames: frame metadata records the window in effect when the
//     frame opens, while clip classification of later operations uses the
//     current window.
//   - Reset clears all frames and resources. ExportJSON, ExportFrameJSON, and
//     Report are safe to call repeatedly and concurrently with each other;
//     repeated exports without intervening draws are byte-identical. Reset and
//     Cancel must not race with rendering. Calls into the wrapped renderer are
//     not serialized; serialize drawing if the renderer requires it.
//
// # Bounded memory and failure handling
//
// WithMaxOps bounds the number of detailed operations kept per frame and
// WithMaxResources bounds the number of distinct resources tracked. Once a
// bound is reached, statistics keep counting but detailed records are dropped
// and the report marks the frame or resource set as overflowed. A nil wrapped renderer,
// a renderer that panics, nil paths/texts/images, and non-finite clocks are all
// handled without panicking the Recorder by default; use [WithPanicPolicy] to
// propagate renderer panics instead.
package renderstats
