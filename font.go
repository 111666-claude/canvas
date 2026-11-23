package canvas

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"os"
	"reflect"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/tdewolff/canvas/text"
	"github.com/tdewolff/font"
)

// FontStyle defines the font style to be used for the font. It specifies a boldness with optionally italic, e.g. FontBlack | FontItalic will specify a black boldness (a font-weight of 800 in CSS) and italic.
type FontStyle int

// see FontStyle
const (
	FontRegular    FontStyle = iota // 400
	FontThin                        // 100
	FontExtraLight                  // 200
	FontLight                       // 300
	FontMedium                      // 500
	FontSemiBold                    // 600
	FontBold                        // 700
	FontExtraBold                   // 800
	FontBlack                       // 900
	FontItalic     FontStyle = 1 << 8
)

// Italic returns true if italic.
func (style FontStyle) Italic() bool {
	return style&FontItalic != 0
}

// Weight returns the font weight (FontRegular, FontBold, ...)
func (style FontStyle) Weight() FontStyle {
	return style & 0xFF
}

// CSS returns the CSS boldness value for the font face.
func (style FontStyle) CSS() int {
	switch style.Weight() {
	case FontThin:
		return 100
	case FontExtraLight:
		return 200
	case FontLight:
		return 300
	case FontMedium:
		return 500
	case FontSemiBold:
		return 600
	case FontBold:
		return 700
	case FontExtraBold:
		return 800
	case FontBlack:
		return 900
	}
	return 400
}

// FauxWeight returns the path offset for fake boldness relative to regular style. The offset is multiplied by the font size (in millimeters) for an offset in millimeters.
func (style FontStyle) FauxWeight() float64 {
	switch style.Weight() {
	case FontThin:
		return -0.02
	case FontExtraLight:
		return -0.01
	case FontLight:
		return -0.005
	case FontMedium:
		return 0.005
	case FontSemiBold:
		return 0.01
	case FontBold:
		return 0.02
	case FontExtraBold:
		return 0.03
	case FontBlack:
		return 0.04
	}
	return 0.0
}

func (style FontStyle) String() string {
	var s string
	switch style.Weight() {
	case FontThin:
		s = "Thin"
	case FontExtraLight:
		s = "ExtraLight"
	case FontLight:
		s = "Light"
	case FontRegular:
		s = "Regular"
	case FontMedium:
		s = "Medium"
	case FontSemiBold:
		s = "SemiBold"
	case FontBold:
		s = "Bold"
	case FontExtraBold:
		s = "ExtraBold"
	case FontBlack:
		s = "Black"
	default:
		return "Unknown"
	}
	if style.Italic() {
		s += " Italic"
	}
	return s
}

// FontVariant defines the font variant to be used for the font, such as subscript or smallcaps.
type FontVariant int

// see FontVariant
const (
	FontNormal FontVariant = iota
	FontSubscript
	FontSuperscript
	FontSmallcaps
)

func (variant FontVariant) String() string {
	switch variant {
	case FontNormal:
		return "Normal"
	case FontSubscript:
		return "Subscript"
	case FontSuperscript:
		return "Superscript"
	case FontSmallcaps:
		return "Smallcaps"
	}
	return "Unknown"
}

////////////////////////////////////////////////////////////////

// FontSubsetter holds a map between original glyph IDs and new glyph IDs in a subsetted font.
type FontSubsetter struct {
	IDs   []uint16          // old glyphIDs for increasing new glyphIDs
	IDMap map[uint16]uint16 // old to new glyphID
}

// NewFontSubsetter returns a new font subsetter.
func NewFontSubsetter() *FontSubsetter {
	return &FontSubsetter{
		IDs:   []uint16{0}, // .notdef should always be at zero
		IDMap: map[uint16]uint16{0: 0},
	}
}

// Get maps a glyphID of the original font to the subsetted font. If the glyphID is not subsetted, it will be added to the map.
func (subsetter *FontSubsetter) Get(glyphID uint16) uint16 {
	if subsetGlyphID, ok := subsetter.IDMap[glyphID]; ok {
		return subsetGlyphID
	}
	subsetGlyphID := uint16(len(subsetter.IDs))
	subsetter.IDs = append(subsetter.IDs, glyphID)
	subsetter.IDMap[glyphID] = subsetGlyphID
	return subsetGlyphID
}

// List returns all subsetted IDs in the order of appearance.
func (subsetter *FontSubsetter) List() []uint16 {
	return subsetter.IDs
}

////////////////////////////////////////////////////////////////

var systemFonts = struct {
	*font.SystemFonts
	sync.Mutex
}{}

// FindLocalFont finds the path to a font from the system's fonts.
func FindLocalFont(name string, style FontStyle) string {
	log.Println("WARNING: github.com/tdewolff/canvas/FindLocalFont is deprecated, please use github.com/tdewolff/canvas/FindSystemFont") // TODO: remove
	filename, _ := FindSystemFont(name, style)
	return filename
}

// CacheSystemFonts will write and load the list of system fonts to the given filename. It scans the given directories for fonts, leave nil to use github.com/tdewolff/font/DefaultFontDirs().
func CacheSystemFonts(filename string, dirs []string) error {
	var fonts *font.SystemFonts
	if info, err := os.Stat(filename); err == nil && info.Mode().IsRegular() {
		fonts, err = font.LoadSystemFonts(filename)
		if err != nil {
			return err
		}
	} else {
		if dirs == nil {
			dirs = font.DefaultFontDirs()
		}
		var err error
		fonts, err = font.FindSystemFonts(dirs)
		if err != nil {
			return err
		}
		if err := fonts.Save(filename); err != nil {
			return err
		}
	}
	systemFonts.Lock()
	systemFonts.SystemFonts = fonts
	systemFonts.Unlock()
	return nil
}

// FindSystemFont finds the path to a font from the system's fonts.
func FindSystemFont(name string, style FontStyle) (string, bool) {
	systemFonts.Lock()
	if systemFonts.SystemFonts == nil {
		systemFonts.SystemFonts, _ = font.FindSystemFonts(font.DefaultFontDirs())
	}
	font, ok := systemFonts.Match(name, font.ParseStyleCSS(style.CSS(), style.Italic()))
	systemFonts.Unlock()
	return font.Filename, ok
}

// Font defines an SFNT font such as TTF or OTF.
type Font struct {
	*font.SFNT
	name       string
	style      FontStyle
	shaper     text.Shaper
	variations string
	features   string
	mu         sync.Mutex // guards the shaper, which is not safe for concurrent use
}

// shape shapes a string with the font's shaper. It is safe for concurrent use.
func (f *Font) shape(s string, ppem uint16, direction text.Direction, script text.Script, lang, features, variations string) []text.Glyph {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shaper.Shape(s, ppem, direction, script, lang, features, variations)
}

// LoadLocalFont loads a font from the system's fonts.
func LoadLocalFont(name string, style FontStyle) (*Font, error) {
	log.Println("WARNING: github.com/tdewolff/canvas/LoadLocalFont is deprecated, please use github.com/tdewolff/canvas/LoadSystemFont") // TODO: remove
	return LoadSystemFont(name, style)
}

// LoadSystemFont loads a font from the system's fonts.
func LoadSystemFont(name string, style FontStyle) (*Font, error) {
	filename, ok := FindSystemFont(name, style)
	if !ok {
		return nil, fmt.Errorf("failed to find font '%s'", name)
	}
	return LoadFontFile(filename, style)
}

// LoadFontFile loads a font from a file.
func LoadFontFile(filename string, style FontStyle) (*Font, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load font file '%s': %w", filename, err)
	}
	return LoadFont(b, 0, style)
}

// LoadFontCollection loads a font from a collection file and uses the font at the specified index.
func LoadFontCollection(filename string, index int, style FontStyle) (*Font, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load font file '%s': %w", filename, err)
	}
	return LoadFont(b, index, style)
}

var nonameFonts = 0

// LoadFont loads a font from memory.
func LoadFont(b []byte, index int, style FontStyle) (*Font, error) {
	SFNT, err := font.ParseSFNT(b, index)
	if err != nil {
		return nil, err
	}

	shaper, err := text.NewShaperSFNT(SFNT)
	if err != nil {
		return nil, err
	}

	name := ""
NameLoop:
	for _, id := range []int{6, 4, 1} {
		for _, record := range SFNT.Name.Get(font.NameID(id)) {
			name = record.String()
			break NameLoop
		}
	}
	if name == "" {
		name = fmt.Sprintf("f%d", nonameFonts)
		nonameFonts++
	}

	font := &Font{
		SFNT:   SFNT,
		name:   name,
		style:  style,
		shaper: shaper,
	}
	return font, nil
}

// Destroy should be called when using HarfBuzz to free the C resources.
func (f *Font) Destroy() {
	f.shaper.Destroy()
}

// Name returns the name of the font.
func (f *Font) Name() string {
	return f.name
}

// Style returns the style of the font.
func (f *Font) Style() FontStyle {
	return f.style
}

// SetVariations sets the font variations (not yet supported).
func (f *Font) SetVariations(variations string) {
	// TODO: support font variations
	f.variations = variations
}

// SetFeatures sets the font features (not yet supported).
func (f *Font) SetFeatures(features string) {
	f.features = features
}

// Face gets the font face given by the font size in points and its style. Fill can be any of Paint, color.Color, or canvas.Pattern.
func (f *Font) Face(size float64, ifill any, deco ...FontDecorator) *FontFace {
	face := &FontFace{}
	face.Font = f
	face.Size = size * mmPerPt
	face.Style = f.style
	face.Variant = FontNormal

	if paint, ok := ifill.(Paint); ok {
		face.Fill = paint
	} else if pattern, ok := ifill.(Pattern); ok {
		face.Fill = Paint{Pattern: pattern}
	} else if gradient, ok := ifill.(Gradient); ok {
		face.Fill = Paint{Gradient: gradient}
	} else if col, ok := ifill.(color.Color); ok {
		face.Fill = Paint{Color: rgbaColor(col)}
	}
	face.Deco = deco
	face.Hinting = font.VerticalHinting
	face.MmPerEm = face.Size / float64(face.Font.Head.UnitsPerEm)
	return face
}

////////////////////////////////////////////////////////////////

// MaxFallbackChainLength is the maximum number of fallback fonts that will be used for a font face. Longer chains are truncated to avoid excessive shaping times.
const MaxFallbackChainLength = 32

// FontFamily contains a family of fonts (bold, italic, ...). Allowing to select an italic style as the native italic font or to use faux italic if not present.
type FontFamily struct {
	name      string
	mu        sync.RWMutex
	fonts     map[FontStyle]*Font
	fallbacks []*FontFamily
}

// NewFontFamily returns a new font family.
func NewFontFamily(name string) *FontFamily {
	return &FontFamily{
		name:  name,
		fonts: map[FontStyle]*Font{},
	}
}

// Destroy should be called when using HarfBuzz to free the C resources. Fallback font families are not destroyed, they are owned by the caller.
func (family *FontFamily) Destroy() {
	family.mu.RLock()
	defer family.mu.RUnlock()
	for _, font := range family.fonts {
		font.Destroy()
	}
}

// SetFallbacks sets the fallback font families that are used when a glyph is missing from this family's font, in decreasing priority. The given slice is copied and will not be modified. Nil families and cycles in the chain are safely ignored when resolving fonts, see also ValidateFallbacks.
func (family *FontFamily) SetFallbacks(fallbacks ...*FontFamily) {
	family.mu.Lock()
	family.fallbacks = append([]*FontFamily(nil), fallbacks...)
	family.mu.Unlock()
}

// AddFallback appends fallback font families to the fallback chain, see SetFallbacks.
func (family *FontFamily) AddFallback(fallbacks ...*FontFamily) {
	family.mu.Lock()
	family.fallbacks = append(family.fallbacks, fallbacks...)
	family.mu.Unlock()
}

// Fallbacks returns a copy of the fallback chain as set by SetFallbacks or AddFallback.
func (family *FontFamily) Fallbacks() []*FontFamily {
	family.mu.RLock()
	defer family.mu.RUnlock()
	return append([]*FontFamily(nil), family.fallbacks...)
}

// ValidateFallbacks returns an error if the fallback chain contains nil or empty font families, a cycle, or exceeds MaxFallbackChainLength. Note that an invalid chain is still safe to use: offending families are skipped when resolving fallback fonts.
func (family *FontFamily) ValidateFallbacks() error {
	seen := map[*FontFamily]bool{family: true}
	stack := map[*FontFamily]bool{}
	var walk func(f *FontFamily) error
	walk = func(f *FontFamily) error {
		stack[f] = true
		defer delete(stack, f)
		f.mu.RLock()
		fallbacks := append([]*FontFamily(nil), f.fallbacks...)
		numFonts := len(f.fonts)
		f.mu.RUnlock()
		if f != family && numFonts == 0 {
			return fmt.Errorf("font family '%s' in fallback chain of font family '%s' has no fonts loaded", f.name, family.name)
		}
		for _, next := range fallbacks {
			if next == nil {
				return fmt.Errorf("fallback chain of font family '%s' contains a nil font family", family.name)
			}
			if stack[next] {
				return fmt.Errorf("fallback chain of font family '%s' contains a cycle involving font family '%s'", family.name, next.name)
			}
			if !seen[next] {
				seen[next] = true
				if MaxFallbackChainLength < len(seen)-1 {
					return fmt.Errorf("fallback chain of font family '%s' exceeds the maximum length of %d", family.name, MaxFallbackChainLength)
				}
			}
			if err := walk(next); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(family)
}

// fallbackChain returns the flattened list of fallback font families in decreasing priority. Nil families and families already seen (cycles) are skipped, and the list is truncated at MaxFallbackChainLength entries.
func (family *FontFamily) fallbackChain() []*FontFamily {
	chain := []*FontFamily{}
	visited := map[*FontFamily]bool{family: true}
	queue := []*FontFamily{family}
	for 0 < len(queue) && len(chain) < MaxFallbackChainLength {
		f := queue[0]
		queue = queue[1:]
		f.mu.RLock()
		fallbacks := append([]*FontFamily(nil), f.fallbacks...)
		f.mu.RUnlock()
		for _, next := range fallbacks {
			if next == nil || visited[next] {
				continue
			}
			visited[next] = true
			chain = append(chain, next)
			if MaxFallbackChainLength <= len(chain) {
				break
			}
			queue = append(queue, next)
		}
	}
	return chain
}

// matchStyle returns the font that matches the requested style closest, together with the style it actually has. It returns nil for an empty family.
func (family *FontFamily) matchStyle(style FontStyle) (*Font, FontStyle) {
	family.mu.RLock()
	defer family.mu.RUnlock()
	if font := family.fonts[style]; font != nil {
		return font, style
	}
	minDiff := math.Inf(1.0)
	minStyle := FontRegular
	for s := range family.fonts {
		diff := math.Abs(style.FauxWeight() - s.FauxWeight())
		if style.Italic() != s.Italic() {
			diff += 0.02
		}
		if diff < minDiff {
			minStyle = s
			minDiff = diff
		}
	}
	return family.fonts[minStyle], minStyle
}

// resolveFallbacks returns the fonts of the fallback chain that best match the requested style. Families without fonts are skipped.
func (family *FontFamily) resolveFallbacks(style FontStyle) []*Font {
	chain := family.fallbackChain()
	if len(chain) == 0 {
		return nil
	}
	fonts := make([]*Font, 0, len(chain))
	for _, f := range chain {
		if font, _ := f.matchStyle(style); font != nil {
			fonts = append(fonts, font)
		}
	}
	if len(fonts) == 0 {
		return nil
	}
	return fonts
}

// Name returns the name of the font family.
func (family *FontFamily) Name() string {
	return family.name
}

// SetVariations sets the font variations (not yet supported).
func (family *FontFamily) SetVariations(variations string) {
	family.mu.RLock()
	defer family.mu.RUnlock()
	for _, font := range family.fonts {
		font.SetVariations(variations)
	}
}

// SetFeatures sets the font features (not yet supported).
func (family *FontFamily) SetFeatures(features string) {
	family.mu.RLock()
	defer family.mu.RUnlock()
	for _, font := range family.fonts {
		font.SetFeatures(features)
	}
}

// LoadLocalFont loads a font from the system's fonts.
func (family *FontFamily) LoadLocalFont(name string, style FontStyle) error {
	log.Println("WARNING: github.com/tdewolff/canvas/FontFamily.LoadLocalFont is deprecated, please use github.com/tdewolff/canvas/FontFamily.LoadSystemFont") // TODO: remove
	return family.LoadSystemFont(name, style)
}

// MustLoadLocalFont loads a font from the system's fonts and panics on error.
func (family *FontFamily) MustLoadLocalFont(name string, style FontStyle) {
	log.Println("WARNING: github.com/tdewolff/canvas/FontFamily.MustLoadLocalFont is deprecated, please use github.com/tdewolff/canvas/FontFamily.MustLoadSystemFont") // TODO: remove
	family.MustLoadSystemFont(name, style)
}

// LoadSystemFont loads a font from the system's fonts.
func (family *FontFamily) LoadSystemFont(name string, style FontStyle) error {
	font, err := LoadSystemFont(name, style)
	if err != nil {
		return err
	}
	family.mu.Lock()
	family.fonts[style] = font
	font.name = family.name
	family.mu.Unlock()
	return nil
}

// MustLoadSystemFont loads a font from the system's fonts and panics on error.
func (family *FontFamily) MustLoadSystemFont(name string, style FontStyle) {
	if err := family.LoadSystemFont(name, style); err != nil {
		panic(err)
	}
}

// LoadFontFile loads a font from a file.
func (family *FontFamily) LoadFontFile(filename string, style FontStyle) error {
	font, err := LoadFontFile(filename, style)
	if err != nil {
		return err
	}
	family.mu.Lock()
	family.fonts[style] = font
	font.name = family.name
	family.mu.Unlock()
	return nil
}

// MustLoadFontFile loads a font from a filea and panics on error.
func (family *FontFamily) MustLoadFontFile(filename string, style FontStyle) {
	if err := family.LoadFontFile(filename, style); err != nil {
		panic(err)
	}
}

// LoadFontCollection loads a font from a collection file and uses the font at the specified index.
func (family *FontFamily) LoadFontCollection(filename string, index int, style FontStyle) error {
	font, err := LoadFontCollection(filename, index, style)
	if err != nil {
		return err
	}
	family.mu.Lock()
	family.fonts[style] = font
	font.name = family.name
	family.mu.Unlock()
	return nil
}

// MustLoadFontCollection loads a font from a collection file and uses the font at the specified index. It panics on error.
func (family *FontFamily) MustLoadFontCollection(filename string, index int, style FontStyle) {
	if err := family.LoadFontCollection(filename, index, style); err != nil {
		panic(err)
	}
}

// LoadFont loads a font from memory.
func (family *FontFamily) LoadFont(b []byte, index int, style FontStyle) error {
	font, err := LoadFont(b, index, style)
	if err != nil {
		return err
	}
	family.mu.Lock()
	family.fonts[style] = font
	font.name = family.name
	family.mu.Unlock()
	return nil
}

// MustLoadFont loads a font from memory. It panics on error.
func (family *FontFamily) MustLoadFont(b []byte, index int, style FontStyle) {
	if err := family.LoadFont(b, index, style); err != nil {
		panic(err)
	}
}

// Face gets the font face given by the font size in points. Other arguments that can be passed: Paint/Pattern/color.Color (=Black), FontStyle (=FontRegular), FontVariant (=FontNormal), multiple FontDecorator, and Hinting (=VerticalHinting).
func (family *FontFamily) Face(size float64, args ...any) *FontFace {
	family.mu.RLock()
	isEmpty := len(family.fonts) == 0
	family.mu.RUnlock()
	if isEmpty {
		panic("font family is empty")
	}

	face := &FontFace{
		Fill:    Paint{Color: Black},
		Hinting: font.VerticalHinting,
		Size:    size * mmPerPt,
	}
	for _, iarg := range args {
		switch arg := iarg.(type) {
		case Paint:
			face.Fill = arg
		case Pattern:
			face.Fill = Paint{Pattern: arg}
		case Gradient:
			face.Fill = Paint{Gradient: arg}
		case color.Color:
			face.Fill = Paint{Color: rgbaColor(arg)}
		case FontStyle:
			face.Style = arg
		case FontVariant:
			face.Variant = arg
		case FontDecorator:
			face.Deco = append(face.Deco, arg)
		case font.Hinting:
			face.Hinting = arg
		}
	}

	// add weight for sub- and superscript
	if face.Variant == FontSubscript || face.Variant == FontSuperscript {
		switch face.Style.Weight() {
		case FontThin:
			face.Style = face.Style&FontItalic | FontExtraLight
		case FontExtraLight:
			face.Style = face.Style&FontItalic | FontLight
		case FontLight:
			face.Style = face.Style&FontItalic | FontRegular
		case FontRegular:
			face.Style = face.Style&FontItalic | FontSemiBold
		case FontMedium:
			face.Style = face.Style&FontItalic | FontSemiBold
		case FontSemiBold:
			face.Style = face.Style&FontItalic | FontBold
		case FontBold:
			face.Style = face.Style&FontItalic | FontExtraBold
		case FontExtraBold:
			face.Style = face.Style&FontItalic | FontBlack
		default:
			face.FauxBold += 0.02
		}
	}

	// find closest font that matches requested style
	font, minStyle := family.matchStyle(face.Style)
	face.Font = font
	if minStyle != face.Style {
		face.FauxBold += face.Style.FauxWeight() - minStyle.FauxWeight()
		if face.Style.Italic() != minStyle.Italic() {
			sign := 1.0
			if !face.Style.Italic() {
				sign = -1.0
			}
			if face.Font.Post.ItalicAngle != 0 {
				face.FauxItalic = sign * math.Tan(-face.Font.Post.ItalicAngle)
			} else {
				face.FauxItalic = sign * 0.3
			}
		}
	}

	// resolve fallback fonts for glyphs missing from the primary font
	face.Fallbacks = family.resolveFallbacks(face.Style)

	if face.Variant == FontSubscript || face.Variant == FontSuperscript {
		scale := 0.583
		xOffset, yOffset := int16(0), int16(0)
		units := float64(face.Font.Head.UnitsPerEm)
		if face.Variant == FontSubscript {
			if face.Font.OS2.YSubscriptXSize != 0 {
				scale = float64(face.Font.OS2.YSubscriptXSize) / units
			}
			if face.Font.OS2.YSubscriptXOffset != 0 {
				xOffset = face.Font.OS2.YSubscriptXOffset
			}
			yOffset = int16(0.33 * units)
			if face.Font.OS2.YSubscriptYOffset != 0 {
				yOffset = -face.Font.OS2.YSubscriptYOffset
			}
		} else if face.Variant == FontSuperscript {
			if face.Font.OS2.YSuperscriptXSize != 0 {
				scale = float64(face.Font.OS2.YSuperscriptXSize) / units
			}
			if face.Font.OS2.YSuperscriptXOffset != 0 {
				xOffset = face.Font.OS2.YSuperscriptXOffset
			}
			yOffset = int16(-0.33 * units)
			if face.Font.OS2.YSuperscriptYOffset != 0 {
				yOffset = face.Font.OS2.YSuperscriptYOffset
			}
		}
		face.Size *= scale
		face.XOffset = int32(float64(xOffset) / scale)
		face.YOffset = int32(float64(yOffset) / scale)
	}
	face.MmPerEm = face.Size / float64(face.Font.Head.UnitsPerEm)
	return face
}

////////////////////////////////////////////////////////////////

// FontFace defines a font face from a given font. It specifies the font size, color, faux styles and font decorations.
type FontFace struct {
	Font *Font

	// Fallbacks are the fonts used for glyphs that are missing from Font, in decreasing priority. It is nil when the font family has no fallback chain.
	Fallbacks []*Font

	Size    float64 // in mm
	Style   FontStyle
	Variant FontVariant

	Fill    Paint
	Deco    []FontDecorator
	Hinting font.Hinting

	// faux styles for bold, italic, and sub- and superscript
	FauxBold, FauxItalic float64
	XOffset, YOffset     int32

	Language  string
	Script    text.Script
	Direction text.Direction // TODO: really needed here?

	// letter spacing
	// stroke and stroke color
	// line height
	// shadow

	MmPerEm float64 // millimeters per EM unit!
}

// Equals returns true when two font face are equal.
func (face *FontFace) Equals(other *FontFace) bool {
	return reflect.DeepEqual(face, other)
}

// Name returns the name of the underlying font.
func (face *FontFace) Name() string {
	return face.Font.name
}

// FontFor returns the font (the primary font or one of the fallback fonts) that owns the given SFNT font, or nil if none does. A nil SFNT returns the primary font.
func (face *FontFace) FontFor(sfnt *font.SFNT) *Font {
	if face.Font == nil {
		return nil
	}
	if sfnt == nil || sfnt == face.Font.SFNT {
		return face.Font
	}
	for _, fallback := range face.Fallbacks {
		if fallback.SFNT == sfnt {
			return fallback
		}
	}
	return nil
}

// HasDecoration returns true if the font face has decorations enabled.
func (face *FontFace) HasDecoration() bool {
	return 0 < len(face.Deco)
}

// FontMetrics contains a number of metrics that define a font face. See https://developer.apple.com/library/archive/documentation/TextFonts/Conceptual/CocoaTextArchitecture/Art/glyph_metrics_2x.png for an explanation of the different metrics.
type FontMetrics struct {
	LineHeight float64
	Ascent     float64
	Descent    float64
	LineGap    float64
	XHeight    float64
	CapHeight  float64

	XMin, YMin float64
	XMax, YMax float64
}

func (m FontMetrics) String() string {
	return fmt.Sprintf("{LineHeight: %v, Ascent: %v, Descent: %v, LineGap: %v, XHeight: %v, CapHeight: %v}", m.LineHeight, m.Ascent, m.Descent, m.LineGap, m.XHeight, m.CapHeight)
}

// Metrics returns the font metrics. See https://developer.apple.com/library/archive/documentation/TextFonts/Conceptual/CocoaTextArchitecture/Art/glyph_metrics_2x.png for an explanation of the different metrics.
func (face *FontFace) Metrics() FontMetrics {
	// TODO: use resolution
	sfnt := face.Font.SFNT
	ascender, descender, lineGap := sfnt.VerticalMetrics()
	return FontMetrics{
		LineHeight: face.MmPerEm * float64(ascender+descender+lineGap),
		Ascent:     face.MmPerEm * float64(ascender),
		Descent:    face.MmPerEm * float64(descender),
		LineGap:    face.MmPerEm * float64(lineGap),
		XHeight:    face.MmPerEm * float64(sfnt.OS2.SxHeight),
		CapHeight:  face.MmPerEm * float64(sfnt.OS2.SCapHeight),
		XMin:       face.MmPerEm * float64(sfnt.Head.XMin),
		YMin:       face.MmPerEm * float64(sfnt.Head.YMin),
		XMax:       face.MmPerEm * float64(sfnt.Head.XMax),
		YMax:       face.MmPerEm * float64(sfnt.Head.YMax),
	}
}

// PPEM returns the pixels-per-EM for a given resolution of the font face.
func (face *FontFace) PPEM(resolution Resolution) uint16 {
	// ppem is for hinting purposes only, this does not influence glyph advances
	if resolution == 0.0 {
		resolution = DefaultResolution
	}
	return uint16(resolution.DPMM() * face.MmPerEm * float64(face.Font.Head.UnitsPerEm))
}

// LineHeight returns the height (ascent+descent) of a line.
func (face *FontFace) LineHeight() float64 {
	metrics := face.Metrics()
	return metrics.Ascent + metrics.Descent
}

func (face *FontFace) Glyphs(s string) []text.Glyph {
	ppem := face.PPEM(DefaultResolution)
	return face.shape(s, ppem, face.Direction, face.Script)
}

// shape shapes a string with the face's font and applies the fallback chain for missing glyphs. Each glyph gets its SFNT and Size set so that glyphs from different fonts can be mixed in a single slice.
func (face *FontFace) shape(s string, ppem uint16, direction text.Direction, script text.Script) []text.Glyph {
	glyphs := face.Font.shape(s, ppem, direction, script, face.Language, face.Font.features, face.Font.variations)
	if 0 < len(face.Fallbacks) {
		glyphs = face.shapeFallback(s, glyphs, ppem, direction, script)
	}
	for i := range glyphs {
		if glyphs[i].SFNT == nil {
			glyphs[i].SFNT = face.Font.SFNT
		}
		if glyphs[i].Size == 0.0 {
			glyphs[i].Size = face.Size
		}
	}
	return glyphs
}

// isMissingGlyph returns true if the glyph is the .notdef glyph (GID 0) for a character that should have a glyph. Control and format characters (such as ZWJ) legitimately shape to invisible glyphs and are not considered missing.
func isMissingGlyph(glyph text.Glyph) bool {
	if glyph.ID != 0 || len(glyph.Text) == 0 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(glyph.Text)
	return !unicode.Is(unicode.Cc, r) && !unicode.Is(unicode.Cf, r) && !unicode.Is(unicode.Cs, r)
}

// shapeFallback replaces runs of missing glyphs (.notdef) by glyphs shaped with the fallback fonts, in decreasing priority. Consecutive missing glyphs are reshaped as a group so that clusters (such as emoji sequences) are shaped together and cluster indices are preserved. Glyphs that are missing from all fonts keep the primary font's .notdef glyph.
func (face *FontFace) shapeFallback(s string, glyphs []text.Glyph, ppem uint16, direction text.Direction, script text.Script) []text.Glyph {
	for _, fallback := range face.Fallbacks {
		if fallback == nil || fallback.SFNT == nil {
			continue
		}
		hasMissing := false
		for i := range glyphs {
			if isMissingGlyph(glyphs[i]) {
				hasMissing = true
				break
			}
		}
		if !hasMissing {
			break
		}

		out := make([]text.Glyph, 0, len(glyphs))
		for i := 0; i < len(glyphs); {
			if !isMissingGlyph(glyphs[i]) {
				out = append(out, glyphs[i])
				i++
				continue
			}

			// group consecutive missing glyphs, their clusters span the substring to reshape
			j := i + 1
			for j < len(glyphs) && isMissingGlyph(glyphs[j]) {
				j++
			}
			start, end := uint32(math.MaxUint32), uint32(0)
			for k := i; k < j; k++ {
				if glyphs[k].Cluster < start {
					start = glyphs[k].Cluster
				}
				if clusterEnd := glyphs[k].Cluster + uint32(len(glyphs[k].Text)); end < clusterEnd {
					end = clusterEnd
				}
			}
			if uint32(len(s)) < end {
				// should never happen, keep the original glyphs
				out = append(out, glyphs[i:j]...)
				i = j
				continue
			}

			subGlyphs := fallback.shape(s[start:end], ppem, direction, script, face.Language, fallback.features, fallback.variations)
			covered := false
			for k := range subGlyphs {
				subGlyphs[k].Cluster += start
				subGlyphs[k].SFNT = fallback.SFNT
				subGlyphs[k].Size = face.Size
				if !isMissingGlyph(subGlyphs[k]) {
					covered = true
				}
			}
			if covered {
				// glyphs that are still missing get another chance with the next fallback font
				out = append(out, subGlyphs...)
			} else {
				out = append(out, glyphs[i:j]...)
			}
			i = j
		}
		glyphs = out
	}
	return glyphs
}

// TextWidth returns the width of a given string in millimeters.
func (face *FontFace) TextWidth(s string) float64 {
	return face.textWidth(face.Glyphs(s))
}

func (face *FontFace) textWidth(glyphs []text.Glyph) float64 {
	w := int32(0)
	wmm := 0.0 // width in millimeters of glyphs from fallback fonts
	for _, glyph := range glyphs {
		if glyph.SFNT != nil && glyph.SFNT != face.Font.SFNT && glyph.Size != 0.0 {
			advance := float64(glyph.XAdvance)
			if glyph.Vertical {
				advance = -float64(glyph.YAdvance)
			}
			wmm += advance * glyph.Size / float64(glyph.SFNT.Head.UnitsPerEm)
			continue
		}
		if !glyph.Vertical {
			w += glyph.XAdvance
		} else {
			w -= glyph.YAdvance
		}
	}
	return face.MmPerEm*float64(w) + wmm
}

func (face *FontFace) heights(mode WritingMode) (float64, float64, float64, float64) {
	metrics := face.Metrics()
	if mode != HorizontalTB {
		ascent, descent, lineGap, xHeight := metrics.Ascent, metrics.Descent, metrics.LineGap, metrics.XHeight
		ascent -= xHeight / 2.0
		descent += xHeight / 2.0
		if mode == VerticalLR {
			ascent, descent = descent, ascent
		}
		return ascent + lineGap, ascent, descent, descent + lineGap
	}
	return metrics.Ascent + metrics.LineGap, metrics.Ascent, metrics.Descent, metrics.Descent + metrics.LineGap
}

// ToPath converts a string to its glyph paths.
func (face *FontFace) ToPath(s string) (*Path, float64) {
	return face.toPath(face.Glyphs(s), face.PPEM(DefaultResolution))
}

func (face *FontFace) toPath(glyphs []text.Glyph, ppem uint16) (*Path, float64) {
	p := &Path{}
	x, y := face.XOffset, face.YOffset // in font units of the primary font
	xmm, ymm := 0.0, 0.0               // in millimeters, for glyphs of fallback fonts
	for _, glyph := range glyphs {
		sfnt, f, glyphPPEM := face.Font.SFNT, face.MmPerEm, ppem
		if glyph.SFNT != nil && glyph.SFNT != face.Font.SFNT && glyph.Size != 0.0 {
			sfnt = glyph.SFNT
			f = glyph.Size / float64(sfnt.Head.UnitsPerEm)
			if ppem != 0 && face.MmPerEm != 0.0 {
				glyphPPEM = uint16(float64(ppem) * f * float64(face.Font.Head.UnitsPerEm) / face.MmPerEm / float64(sfnt.Head.UnitsPerEm))
			}
		}
		gx := face.MmPerEm*float64(x) + xmm + f*float64(glyph.XOffset)
		gy := face.MmPerEm*float64(y) + ymm + f*float64(glyph.YOffset)
		_ = sfnt.GlyphPath(p, glyph.ID, glyphPPEM, gx, gy, f, font.NoHinting)
		if sfnt == face.Font.SFNT {
			x += glyph.XAdvance
			y += glyph.YAdvance
		} else {
			xmm += f * float64(glyph.XAdvance)
			ymm += f * float64(glyph.YAdvance)
		}
	}
	width := face.MmPerEm*float64(x) + xmm

	if face.FauxBold != 0.0 {
		d := face.FauxBold * face.Size
		if face.Font.IsTrueType {
			// TrueType is CW oriented for filling contours.
			d = -d
		}

		// use FastStroke to omit settling the path
		origFastStroke := FastStroke
		FastStroke = true
		p = p.Offset(d, Tolerance)
		FastStroke = origFastStroke
	}
	if face.FauxItalic != 0.0 {
		p = p.Transform(Identity.Shear(face.FauxItalic, 0.0))
	}
	return p, width
}

// Decorate will return the decoration path over a given width in millimeters.
func (face *FontFace) Decorate(width float64) *Path {
	fmt.Println("DEPRECATED: FontFace.Decorate is removed in favour of FontFace.RenderTo")
	return &Path{}
}

func (face *FontFace) RenderTo(r Renderer, m Matrix, s string, resolution Resolution) {
	face.renderTo(r, m, face.Glyphs(s), face.PPEM(resolution))
}

func (face *FontFace) renderTo(r Renderer, m Matrix, glyphs []text.Glyph, ppem uint16) {
	p, width := face.toPath(glyphs, ppem)
	if ppem != 0 && face.Hinting != font.NoHinting && !m.HasRotation() {
		// grid-align vertically on pixel raster, this improves font sharpness
		dpmm := float64(ppem) / face.MmPerEm / float64(face.Font.Head.UnitsPerEm)
		_, y := m.Pos()
		m = m.Translate(0.0, float64(int(y*dpmm+0.5))/dpmm-y)
	}
	if face.Deco != nil {
		for _, deco := range face.Deco {
			// TODO: start at XOffset and subtract from width?
			deco.Decorate(r, m, face, p, width)
		}
	}

	style := DefaultStyle
	style.Fill = face.Fill
	r.RenderPath(p, style, m)
}

////////////////////////////////////////////////////////////////

// FontDecorator is an interface that returns a path given a font face and a width in millimeters.
type FontDecorator interface {
	Decorate(Renderer, Matrix, *FontFace, *Path, float64)
}

const underlineDistance = 0.075
const underlineThickness = 0.05

// FontUnderline is a font decoration that draws a line under the text.
var FontUnderline = underline{}

type underline struct {
	Fill    Paint
	Outline float64 // skip distance from text
}

func (deco underline) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := face.Size * underlineThickness
	y := -face.Size * underlineDistance
	if face.Font.Post.UnderlineThickness != 0 {
		h = face.MmPerEm * float64(face.Font.Post.UnderlineThickness)
	}
	if face.Font.Post.UnderlinePosition != 0 {
		y = face.MmPerEm * float64(face.Font.Post.UnderlinePosition)
	}
	y -= h

	p := &Path{}
	p.MoveTo(0.0, y)
	p.LineTo(w, y)
	p = p.Stroke(h, ButtCap, BevelJoin, Tolerance)
	if 0.0 < deco.Outline {
		p = p.Not(text.Offset(deco.Outline, Tolerance))
	}

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (underline) String() string {
	return "Underline"
}

// FontOverline is a font decoration that draws a line over the text.
var FontOverline = overline{}

type overline struct {
	Fill    Paint
	Outline float64 // skip distance from text
}

func (deco overline) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := face.Size * underlineThickness
	y := face.Metrics().Ascent
	if face.Font.Post.UnderlineThickness != 0 {
		h = face.MmPerEm * float64(face.Font.Post.UnderlineThickness)
	}
	y -= 0.5 * h

	dx := face.FauxItalic * y
	w += face.FauxItalic * y

	p := &Path{}
	p.MoveTo(dx, y)
	p.LineTo(w, y)
	p = p.Stroke(h, ButtCap, BevelJoin, Tolerance)
	if 0.0 < deco.Outline {
		p = p.Not(text.Offset(deco.Outline, Tolerance))
	}

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (overline) String() string {
	return "Overline"
}

// FontStrikethrough is a font decoration that draws a line through the text.
var FontStrikethrough = strikethrough{}

type strikethrough struct {
	Fill Paint
}

func (deco strikethrough) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := face.Size * underlineThickness
	y := face.Metrics().XHeight / 2.0
	if face.Font.OS2.YStrikeoutSize != 0 {
		h = face.MmPerEm * float64(face.Font.OS2.YStrikeoutSize)
	}
	if face.Font.OS2.YStrikeoutPosition != 0 {
		y = face.MmPerEm * float64(face.Font.OS2.YStrikeoutPosition)
	}
	y += 0.5 * h

	dx := face.FauxItalic * y
	w += face.FauxItalic * y

	p := &Path{}
	p.MoveTo(dx, y)
	p.LineTo(w, y)
	p = p.Stroke(h, ButtCap, BevelJoin, Tolerance)

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (strikethrough) String() string {
	return "Strikethrough"
}

// FontDoubleUnderline is a font decoration that draws two lines under the text.
var FontDoubleUnderline = doubleUnderline{}

type doubleUnderline struct {
	Fill    Paint
	Outline float64 // skip distance from text
}

func (deco doubleUnderline) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := face.Size * underlineThickness
	y := -face.Size * underlineDistance
	if face.Font.Post.UnderlineThickness != 0 {
		h = face.MmPerEm * float64(face.Font.Post.UnderlineThickness)
	}
	if face.Font.Post.UnderlinePosition != 0 {
		y = face.MmPerEm * float64(face.Font.Post.UnderlinePosition)
	}
	y -= h

	p := &Path{}
	p.MoveTo(0.0, y)
	p.LineTo(w, y)
	p.MoveTo(0.0, y-1.5*h)
	p.LineTo(w, y-1.5*h)
	p = p.Stroke(h, ButtCap, BevelJoin, Tolerance)
	if 0.0 < deco.Outline {
		p = p.Not(text.Offset(deco.Outline, Tolerance))
	}

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (doubleUnderline) String() string {
	return "DoubleUnderline"
}

// FontDottedUnderline is a font decoration that draws a dotted line under the text.
var FontDottedUnderline = dottedUnderline{}

type dottedUnderline struct {
	Fill    Paint
	Outline float64 // skip distance from text
}

func (deco dottedUnderline) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := 0.5 * face.Size * underlineThickness
	y := -face.Size * underlineDistance
	if face.Font.Post.UnderlineThickness != 0 {
		h = 0.5 * face.MmPerEm * float64(face.Font.Post.UnderlineThickness)
	}
	if face.Font.Post.UnderlinePosition != 0 {
		y = face.MmPerEm * float64(face.Font.Post.UnderlinePosition)
	}
	y -= 2.0 * h
	w -= 2.0 * h
	if w < 0.0 {
		return
	}

	d := 4.0 * h
	n := int(w/d) + 1
	var p *Path
	if n == 1 {
		p = Circle(h).Translate(h+w/2.0, y)
	} else {
		p = &Path{}
		d = w / float64(n-1)
		for i := 0; i < n; i++ {
			p = p.Append(Circle(h).Translate(h+float64(i)*d, y))
		}
	}
	if 0.0 < deco.Outline {
		p = p.Not(text.Offset(deco.Outline, Tolerance))
	}

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (dottedUnderline) String() string {
	return "DottedUnderline"
}

// FontDashedUnderline is a font decoration that draws a dashed line under the text.
var FontDashedUnderline = dashedUnderline{}

type dashedUnderline struct {
	Fill    Paint
	Outline float64 // skip distance from text
}

func (deco dashedUnderline) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := face.Size * underlineThickness
	y := -face.Size * underlineDistance
	if face.Font.Post.UnderlineThickness != 0 {
		h = face.MmPerEm * float64(face.Font.Post.UnderlineThickness)
	}
	if face.Font.Post.UnderlinePosition != 0 {
		y = face.MmPerEm * float64(face.Font.Post.UnderlinePosition)
	}
	y -= h

	d := 3.0 * h
	n := 2*int((w-d)/(2.0*d)) + 1

	p := &Path{}
	p.MoveTo(0.0, y)
	p.LineTo(w, y)
	if 2 < n {
		d = w / float64(n)
		p = p.Dash(0.0, d)
	}
	p = p.Stroke(h, ButtCap, BevelJoin, Tolerance)
	if 0.0 < deco.Outline {
		p = p.Not(text.Offset(deco.Outline, Tolerance))
	}

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (dashedUnderline) String() string {
	return "DashedUnderline"
}

// FontWavyUnderline is a font decoration that draws a wavy path under the text.
var FontWavyUnderline = wavyUnderline{}

type wavyUnderline struct {
	Fill    Paint
	Outline float64 // skip distance from text
}

func (deco wavyUnderline) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := face.Size * underlineThickness
	y := -face.Size * underlineDistance
	if face.Font.Post.UnderlineThickness != 0 {
		h = face.MmPerEm * float64(face.Font.Post.UnderlineThickness)
	}
	if face.Font.Post.UnderlinePosition != 0 {
		y = face.MmPerEm * float64(face.Font.Post.UnderlinePosition)
	}
	y -= h

	dx := 0.707 * h
	w -= 2.0 * dx
	dh := -face.Size * 0.10
	d := 5.0 * h
	n := int(0.5 + w/d)
	if n == 0 {
		return
	}
	d = w / float64(n)

	p := &Path{}
	p.MoveTo(dx, y)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			p.LineTo(dx+d/3.0, y)
			p.LineTo(dx+d, y+dh)
		} else {
			p.LineTo(dx+d/3.0, y+dh)
			p.LineTo(dx+d, y)
		}
		dx += d
	}
	p = p.Stroke(h, ButtCap, MiterJoin, Tolerance)
	if 0.0 < deco.Outline {
		p = p.Not(text.Offset(deco.Outline, Tolerance))
	}

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (wavyUnderline) String() string {
	return "WavyUnderline"
}

// FontSineUnderline is a font decoration that draws a wavy sine path under the text.
var FontSineUnderline = sineUnderline{}

type sineUnderline struct {
	Fill    Paint
	Outline float64 // skip distance from text
}

func (deco sineUnderline) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := face.Size * underlineThickness
	y := -face.Size * underlineDistance
	if face.Font.Post.UnderlineThickness != 0 {
		h = face.MmPerEm * float64(face.Font.Post.UnderlineThickness)
	}
	if face.Font.Post.UnderlinePosition != 0 {
		y = face.MmPerEm * float64(face.Font.Post.UnderlinePosition)
	}
	y -= h

	w -= h
	d := 4.0 * h
	n := int(0.5 + w/d)
	if n == 0 {
		return
	}
	d = (w - h) / float64(n)

	dx := h
	dh := -face.Size * 0.10
	y += 0.5 * dh
	p := &Path{}
	p.MoveTo(dx, y)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			p.QuadTo(dx+d*0.5, y-dh, dx+d, y)
		} else {
			p.QuadTo(dx+d*0.5, y+dh, dx+d, y)
		}
		dx += d
	}
	p = p.Stroke(h, RoundCap, RoundJoin, Tolerance)
	if 0.0 < deco.Outline {
		p = p.Not(text.Offset(deco.Outline, Tolerance))
	}

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (sineUnderline) String() string {
	return "SineUnderline"
}

// FontSawtoothUnderline is a font decoration that draws a wavy sawtooth path under the text.
var FontSawtoothUnderline = sawtoothUnderline{}

type sawtoothUnderline struct {
	Fill    Paint
	Outline float64 // skip distance from text
}

func (deco sawtoothUnderline) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	h := face.Size * underlineThickness
	y := -face.Size * underlineDistance
	if face.Font.Post.UnderlineThickness != 0 {
		h = face.MmPerEm * float64(face.Font.Post.UnderlineThickness)
	}
	if face.Font.Post.UnderlinePosition != 0 {
		y = face.MmPerEm * float64(face.Font.Post.UnderlinePosition)
	}
	y -= h

	dx := 0.707 * h
	w -= 2.0 * dx
	dh := -face.Size * 0.15
	d := 4.0 * h
	n := int(0.5 + w/d)
	if n == 0 {
		return
	}
	d = w / float64(n)

	p := &Path{}
	p.MoveTo(dx, y)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			p.LineTo(dx+d, y+dh)
		} else {
			p.LineTo(dx+d, y)
		}
		dx += d
	}
	p = p.Stroke(h, ButtCap, MiterJoin, Tolerance)
	if 0.0 < deco.Outline {
		p = p.Not(text.Offset(deco.Outline, Tolerance))
	}

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (sawtoothUnderline) String() string {
	return "SawtoothUnderline"
}

// FontStroke is a font decoration that draws a stroke around the text.
func FontStroke(width float64, ifill any) fontStroke {
	var fill Paint
	if paint, ok := ifill.(Paint); ok {
		fill = paint
	} else if pattern, ok := ifill.(Pattern); ok {
		fill = Paint{Pattern: pattern}
	} else if gradient, ok := ifill.(Gradient); ok {
		fill = Paint{Gradient: gradient}
	} else if col, ok := ifill.(color.Color); ok {
		fill = Paint{Color: rgbaColor(col)}
	}
	return fontStroke{
		Fill:  fill,
		Width: width,
	}
}

type fontStroke struct {
	Fill  Paint
	Width float64 // stroke width
}

func (deco fontStroke) Decorate(r Renderer, m Matrix, face *FontFace, text *Path, w float64) {
	if deco.Width < 0.0 || Equal(deco.Width, 0.0) {
		return
	}
	p := text.Offset(deco.Width, Tolerance)

	style := DefaultStyle
	if deco.Fill.Has() {
		style.Fill = deco.Fill
	}
	r.RenderPath(p, style, m)
}

func (fontStroke) String() string {
	return "Stroke"
}
