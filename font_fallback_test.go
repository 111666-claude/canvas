package canvas

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/tdewolff/test"
)

// loadDejaVuFamily loads the DejaVu Serif fixture (Latin only, no CJK/symbols/emoji).
func loadDejaVuFamily(t *testing.T) *FontFamily {
	t.Helper()
	family := NewFontFamily("dejavu-serif")
	if err := family.LoadFontFile("resources/DejaVuSerif.ttf", FontRegular); err != nil {
		t.Fatal(err)
	}
	return family
}

// loadUnifontFamily loads the Unifont fixture (covers CJK, symbols and Hebrew in the BMP).
func loadUnifontFamily(t *testing.T) *FontFamily {
	t.Helper()
	family := NewFontFamily("unifont")
	if err := family.LoadFontFile("resources/unifont-13.0.05.ttf", FontRegular); err != nil {
		t.Fatal(err)
	}
	return family
}

// emFace returns a face with MmPerEm == 1.0 so that font units equal millimeters.
func emFace(family *FontFamily) *FontFace {
	pt := ptPerMm * float64(family.fonts[FontRegular].Head.UnitsPerEm)
	return family.Face(pt, Black, FontRegular, FontNormal)
}

// buildTTC builds a font collection (TTC) from single TTF/OTF font files.
func buildTTC(fonts ...[]byte) ([]byte, error) {
	type tableRecord struct {
		tag            string
		checksum       uint32
		offset, length uint32
	}
	directories := make([][]tableRecord, len(fonts))
	for i, b := range fonts {
		if len(b) < 12 {
			return nil, fmt.Errorf("bad font data")
		}
		numTables := int(binary.BigEndian.Uint16(b[4:]))
		if len(b) < 12+16*numTables {
			return nil, fmt.Errorf("bad font data")
		}
		records := make([]tableRecord, numTables)
		for j := range records {
			off := 12 + 16*j
			records[j] = tableRecord{
				tag:      string(b[off : off+4]),
				checksum: binary.BigEndian.Uint32(b[off+4:]),
				offset:   binary.BigEndian.Uint32(b[off+8:]),
				length:   binary.BigEndian.Uint32(b[off+12:]),
			}
			if uint32(len(b)) < records[j].offset+records[j].length {
				return nil, fmt.Errorf("bad font data")
			}
		}
		directories[i] = records
	}

	// lay out: header, then per font a table directory followed by its table data
	offsets := make([]uint32, len(fonts))
	pos := 12 + 4*len(fonts)
	for i := range fonts {
		offsets[i] = uint32(pos)
		pos += 12 + 16*len(directories[i])
		for _, record := range directories[i] {
			pos += int(record.length) + (4-int(record.length)&3)&3
		}
	}

	buf := &bytes.Buffer{}
	buf.WriteString("ttcf")
	_ = binary.Write(buf, binary.BigEndian, uint16(1)) // major version
	_ = binary.Write(buf, binary.BigEndian, uint16(0)) // minor version
	_ = binary.Write(buf, binary.BigEndian, uint32(len(fonts)))
	for _, offset := range offsets {
		_ = binary.Write(buf, binary.BigEndian, offset)
	}
	for i, records := range directories {
		buf.Write(fonts[i][:12]) // SFNT version and (ignored) directory hints
		data := &bytes.Buffer{}
		for _, record := range records {
			offset := offsets[i] + uint32(12+16*len(records)) + uint32(data.Len())
			_ = binary.Write(buf, binary.BigEndian, [4]byte{record.tag[0], record.tag[1], record.tag[2], record.tag[3]})
			_ = binary.Write(buf, binary.BigEndian, record.checksum)
			_ = binary.Write(buf, binary.BigEndian, offset)
			_ = binary.Write(buf, binary.BigEndian, record.length)
			data.Write(fonts[i][record.offset : record.offset+record.length])
			for padding := (4 - record.length&3) & 3; 0 < padding; padding-- {
				data.WriteByte(0)
			}
		}
		buf.Write(data.Bytes())
	}
	return buf.Bytes(), nil
}

func TestFontFallbackGlyphs(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	unifont := loadUnifontFamily(t)
	dejavu.SetFallbacks(unifont)

	face := emFace(dejavu) // MmPerEm == 1.0
	test.T(t, len(face.Fallbacks), 1)

	// Latin comes from the primary font, CJK and symbols from the fallback
	glyphs := face.Glyphs("A世☃")
	test.T(t, len(glyphs), 3)
	test.T(t, glyphs[0].SFNT, face.Font.SFNT)
	test.T(t, glyphs[1].SFNT, unifont.fonts[FontRegular].SFNT)
	test.T(t, glyphs[2].SFNT, unifont.fonts[FontRegular].SFNT)
	test.T(t, glyphs[0].ID, uint16(36))    // A in DejaVu Serif
	test.T(t, glyphs[1].ID, uint16(19993)) // 世 in Unifont
	test.T(t, glyphs[2].ID, uint16(9734))  // ☃ in Unifont

	// advances use each glyph's own font: A is 1479 units of 2048, 世 and ☃ are 1024 units of 1024
	test.Float(t, face.TextWidth("A"), 1479.0)
	test.Float(t, face.TextWidth("世"), 2048.0)
	test.Float(t, face.TextWidth("☃"), 2048.0)
	test.Float(t, face.TextWidth("A世☃"), 1479.0+2048.0+2048.0)

	// cluster indices are preserved
	test.T(t, glyphs[0].Cluster, uint32(0))
	test.T(t, glyphs[1].Cluster, uint32(1))
	test.T(t, glyphs[2].Cluster, uint32(4))

	// glyphs missing from all fonts keep the primary font's .notdef glyph
	glyphs = face.Glyphs("A😀B")
	test.T(t, len(glyphs), 3)
	test.T(t, glyphs[1].ID, uint16(0))
	test.T(t, glyphs[1].SFNT, face.Font.SFNT)
	dejavuOnly2 := loadDejaVuFamily(t)
	test.Float(t, face.TextWidth("A😀B"), emFace(dejavuOnly2).TextWidth("A😀B"))

	// ToPath places fallback glyphs at the same advances as textWidth
	_, width := face.ToPath("A世☃")
	test.Float(t, width, face.TextWidth("A世☃"))

	// metrics, baseline and decorations only depend on the primary font
	metrics := face.Metrics()
	test.Float(t, metrics.Ascent, 1901.0)
	test.Float(t, metrics.Descent, 483.0)
	faceDeco := dejavu.Face(ptPerMm*2048.0, Black, FontRegular, FontNormal, FontUnderline)
	test.T(t, fontDecorate(faceDeco, 10.0), MustParseSVGPath("M0 -265L10 -265L10 -175L0 -175z"))

	// single-font behaviour is unchanged when no fallback chain is set
	dejavuOnly := loadDejaVuFamily(t)
	faceOnly := emFace(dejavuOnly)
	test.T(t, len(faceOnly.Fallbacks), 0)
	test.Float(t, faceOnly.TextWidth("A世☃"), face.TextWidth("A")+2.0*1229.0)
	glyphsOnly := faceOnly.Glyphs("A世☃")
	for _, glyph := range glyphsOnly {
		test.T(t, glyph.SFNT, faceOnly.Font.SFNT)
	}
}

func TestFontFallbackChainOrder(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	garamond := NewFontFamily("garamond")
	if err := garamond.LoadFontFile("resources/EBGaramond12-Regular.otf", FontRegular); err != nil {
		t.Fatal(err)
	}
	unifont := loadUnifontFamily(t)
	dejavu.SetFallbacks(garamond, unifont)

	face := emFace(dejavu)
	test.T(t, len(face.Fallbacks), 2)

	// A is covered by the primary font even though fallbacks also have it
	glyphs := face.Glyphs("A世")
	test.T(t, glyphs[0].SFNT, face.Font.SFNT)
	// 世 is missing from DejaVu and Garamond, so Unifont is used
	test.T(t, glyphs[1].SFNT, unifont.fonts[FontRegular].SFNT)

	// priority: a glyph covered by Garamond must not come from Unifont
	if garamond.fonts[FontRegular].SFNT.GlyphIndex('é') != 0 && dejavu.fonts[FontRegular].SFNT.GlyphIndex('é') == 0 {
		glyphs = face.Glyphs("é")
		test.T(t, glyphs[0].SFNT, garamond.fonts[FontRegular].SFNT)
	}
}

func TestFontFallbackCycle(t *testing.T) {
	a := loadDejaVuFamily(t)
	b := loadUnifontFamily(t)
	a.AddFallback(b)
	b.AddFallback(a) // cycle

	face := emFace(a)
	test.T(t, len(face.Fallbacks), 1) // only b, a itself is skipped
	glyphs := face.Glyphs("世")
	test.T(t, glyphs[0].SFNT, b.fonts[FontRegular].SFNT)

	if err := a.ValidateFallbacks(); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal("expected cycle error, got", err)
	}

	// self-cycle
	c := loadDejaVuFamily(t)
	c.AddFallback(c)
	face = emFace(c)
	test.T(t, len(face.Fallbacks), 0)
	if err := c.ValidateFallbacks(); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal("expected cycle error, got", err)
	}
}

func TestFontFallbackEmptyAndNil(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	empty := NewFontFamily("empty")
	dejavu.SetFallbacks(nil, empty)

	face := emFace(dejavu)
	test.T(t, len(face.Fallbacks), 0) // nil and empty families are skipped
	test.Float(t, face.TextWidth("A"), 1479.0)

	if err := dejavu.ValidateFallbacks(); err == nil {
		t.Fatal("expected error for nil and empty fallback families")
	}

	// a family that failed to load (corrupt font) is empty and thus skipped
	corrupt := NewFontFamily("corrupt")
	if err := corrupt.LoadFont([]byte("not a font"), 0, FontRegular); err == nil {
		t.Fatal("expected error loading corrupt font")
	}
	dejavu.SetFallbacks(corrupt)
	face = emFace(dejavu)
	test.T(t, len(face.Fallbacks), 0)
	test.Float(t, face.TextWidth("A"), 1479.0)

	// missing font file gives a contextual error
	if err := dejavu.LoadFontFile("resources/does-not-exist.ttf", FontRegular); err == nil || !strings.Contains(err.Error(), "does-not-exist.ttf") {
		t.Fatal("expected contextual error for missing font file, got", err)
	}
}

func TestFontFallbackTooLong(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	chain := []*FontFamily{}
	for i := 0; i < MaxFallbackChainLength+8; i++ {
		family := NewFontFamily(fmt.Sprintf("f%d", i))
		if err := family.LoadFontFile("resources/DejaVuSerif.ttf", FontRegular); err != nil {
			t.Fatal(err)
		}
		chain = append(chain, family)
	}
	dejavu.SetFallbacks(chain...)

	face := emFace(dejavu)
	test.T(t, len(face.Fallbacks), MaxFallbackChainLength) // truncated
	if err := dejavu.ValidateFallbacks(); err == nil || !strings.Contains(err.Error(), "maximum length") {
		t.Fatal("expected maximum length error, got", err)
	}
}

func TestFontFallbackCallerSliceUnmodified(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	unifont := loadUnifontFamily(t)
	other := loadDejaVuFamily(t)

	chain := []*FontFamily{unifont}
	dejavu.SetFallbacks(chain...)
	chain[0] = other // must not affect the family's chain
	test.T(t, len(dejavu.Fallbacks()), 1)
	test.T(t, dejavu.Fallbacks()[0], unifont)

	// the returned chain is a copy
	fallbacks := dejavu.Fallbacks()
	fallbacks[0] = other
	test.T(t, dejavu.Fallbacks()[0], unifont)

	dejavu.AddFallback(other)
	test.T(t, len(dejavu.Fallbacks()), 2)
	test.T(t, dejavu.Fallbacks()[1], other)
}

func TestFontFallbackConcurrent(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	unifont := loadUnifontFamily(t)
	dejavu.SetFallbacks(unifont)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				face := dejavu.Face(12.0, Black, FontRegular, FontNormal)
				_ = face.TextWidth("A世☃")
				_ = dejavu.Fallbacks()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		extra := NewFontFamily("extra")
		_ = extra.LoadFontFile("resources/DejaVuSerif.ttf", FontRegular)
		for j := 0; j < 10; j++ {
			dejavu.AddFallback(extra)
			dejavu.SetFallbacks(unifont)
		}
	}()
	wg.Wait()
}

func TestFontFallbackTTC(t *testing.T) {
	b0, err := os.ReadFile("resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	b1, err := os.ReadFile("resources/unifont-13.0.05.ttf")
	if err != nil {
		t.Fatal(err)
	}
	// NOTE: tdewolff/font cannot parse the last font of a TTC due to a bounds
	// check bug upstream, so append a dummy font at the end
	ttc, err := buildTTC(b0, b1, b0)
	if err != nil {
		t.Fatal(err)
	}

	primary := NewFontFamily("primary")
	if err := primary.LoadFont(ttc, 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	fallback := NewFontFamily("fallback")
	if err := fallback.LoadFont(ttc, 1, FontRegular); err != nil {
		t.Fatal(err)
	}
	primary.SetFallbacks(fallback)

	face := emFace(primary)
	glyphs := face.Glyphs("A世")
	test.T(t, glyphs[0].SFNT, primary.fonts[FontRegular].SFNT)
	test.T(t, glyphs[1].SFNT, fallback.fonts[FontRegular].SFNT)
	test.Float(t, face.TextWidth("A世"), 1479.0+2048.0)

	// bad collection index gives an error
	if err := primary.LoadFont(ttc, 3, FontRegular); err == nil {
		t.Fatal("expected error for bad font collection index")
	}
}

func TestFontFallbackTextLine(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	unifont := loadUnifontFamily(t)
	dejavu.SetFallbacks(unifont)

	face := emFace(dejavu)
	txt := NewTextLine(face, "AB世CD", Left)
	test.T(t, txt.Lines(), 1)
	test.Float(t, txt.Width, face.TextWidth("AB世CD"))

	// glyph sources are preserved through the text layout
	names := map[*Font]bool{}
	for _, font := range txt.Fonts() {
		names[font] = true
	}
	test.T(t, len(names), 2)

	// line heights only depend on the primary font's metrics
	dejavuOnly := loadDejaVuFamily(t)
	txtOnly := NewTextLine(emFace(dejavuOnly), "ABCD", Left)
	test.Float(t, txt.Height, txtOnly.Height)
}

func TestFontFallbackRichText(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	unifont := loadUnifontFamily(t)
	dejavu.SetFallbacks(unifont)

	face := emFace(dejavu)
	rt := NewRichText(face)
	rt.WriteString("Hello 世界!")
	txt := rt.ToText(0.0, 0.0, Left, Top, nil)
	test.T(t, txt.Lines(), 1)
	test.Float(t, txt.Width, face.TextWidth("Hello 世界!"))

	// check glyph sources and positions
	x := 0.0
	nFallback := 0
	txt.WalkSpans(func(xOffset, yOffset float64, span TextSpan) {
		for _, glyph := range span.Glyphs {
			mmPerEm := face.MmPerEm
			if glyph.SFNT == unifont.fonts[FontRegular].SFNT {
				nFallback++
				mmPerEm = glyph.Size / float64(glyph.SFNT.Head.UnitsPerEm)
			}
			x += mmPerEm * float64(glyph.XAdvance)
		}
	})
	test.T(t, nFallback, 2) // 世 and 界
	test.Float(t, txt.Width, x)

	// both fonts are registered
	test.T(t, len(txt.Fonts()), 2)

	// rendering to paths works and is non-degenerate
	c := New(txt.Width, txt.Height)
	txt.RenderTo(c, Identity, DefaultResolution)
	bounds := c.Bounds()
	if bounds.X1 <= bounds.X0 || bounds.Y1 <= bounds.Y0 {
		t.Fatal("expected non-degenerate bounds, got", bounds)
	}

	// spans report their fallback glyphs for renderers
	hasFallback := false
	txt.WalkSpans(func(xOffset, yOffset float64, span TextSpan) {
		if span.HasFallbackGlyphs() {
			hasFallback = true
			if span.ToPath(DefaultResolution).Empty() {
				t.Fatal("expected non-empty path for fallback span")
			}
		}
	})
	test.T(t, hasFallback, true)
}

func TestFontFallbackRTL(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	unifont := loadUnifontFamily(t)
	dejavu.SetFallbacks(unifont)

	face := emFace(dejavu)
	// ש (Hebrew, RTL) is missing from DejaVu Serif and falls back to Unifont
	glyphs := face.Glyphs("ש")
	test.T(t, len(glyphs), 1)
	test.T(t, glyphs[0].SFNT, unifont.fonts[FontRegular].SFNT)
	test.Float(t, face.TextWidth("ש"), 1024.0) // 512 units of 1024

	// mixed LTR/RTL: Hebrew item shapes right-to-left with the fallback font
	txt := NewTextLine(face, "ABשCD", Left)
	test.Float(t, txt.Width, face.TextWidth("AB")+face.TextWidth("ש")+face.TextWidth("CD"))
	found := false
	txt.WalkSpans(func(xOffset, yOffset float64, span TextSpan) {
		for _, glyph := range span.Glyphs {
			if glyph.SFNT == unifont.fonts[FontRegular].SFNT {
				found = true
			}
		}
	})
	test.T(t, found, true)
}

func TestFontFallbackVerticalAndEmojiSequence(t *testing.T) {
	dejavu := loadDejaVuFamily(t)
	unifont := loadUnifontFamily(t)
	dejavu.SetFallbacks(unifont)

	// vertical writing mode with CJK from the fallback font
	face := dejavu.Face(12.0, Black, FontRegular, FontNormal)
	rt := NewRichText(face)
	rt.SetWritingMode(VerticalRL)
	rt.WriteString("世界A")
	txt := rt.ToText(0.0, 0.0, Left, Top, nil)
	if txt.Empty() {
		t.Fatal("expected non-empty vertical text")
	}
	if txt.Width <= 0.0 || txt.Height <= 0.0 {
		t.Fatal("expected positive vertical text dimensions, got", txt.Width, txt.Height)
	}

	// emoji ZWJ sequence missing from all fonts: keeps the primary font's
	// .notdef glyphs and does not break on multi-codepoint clusters
	glyphs := emFace(dejavu).Glyphs("👨‍👩‍👧")
	for _, glyph := range glyphs {
		test.T(t, glyph.SFNT, dejavu.fonts[FontRegular].SFNT)
	}
}
