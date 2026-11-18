package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// FontFamily identifies the typeface category for PDF vector text rendering.
type FontFamily uint8

const (
	FontSans FontFamily = iota
	FontSerif
	FontMono
	FontMath
	FontMathItalic
)

// TextStyle controls Type 3 / Base-14 font lookup for PDF vector text.
type TextStyle struct {
	Family FontFamily
	Bold   bool
	Italic bool
}

type faceEntry struct {
	path   string
	data   []byte
	index  int
	family FontFamily
	bold   bool
	italic bool
	cjk    bool
	score  int

	once   sync.Once
	font   *sfnt.Font
	loadEr error
	file   *os.File
}

func (fe *faceEntry) getFont() (*sfnt.Font, error) {
	fe.once.Do(func() {
		var ra io.ReaderAt
		if fe.data != nil {
			ra = bytes.NewReader(fe.data)
		} else {
			f, err := os.Open(fe.path)
			if err != nil {
				fe.loadEr = err
				return
			}
			fe.file = f
			ra = f
		}
		ext := strings.ToLower(filepath.Ext(fe.path))
		if ext == ".ttc" || ext == ".otc" {
			col, err := sfnt.ParseCollectionReaderAt(ra)
			if err != nil {
				fe.loadEr = err
				return
			}
			fe.font, fe.loadEr = col.Font(fe.index)
			return
		}
		fe.font, fe.loadEr = sfnt.ParseReaderAt(ra)
	})
	return fe.font, fe.loadEr
}

func (fe *faceEntry) close() {
	if fe.file != nil {
		_ = fe.file.Close()
		fe.file = nil
	}
}

type fontCandidate struct {
	path   string
	family FontFamily
	bold   bool
	italic bool
	cjk    bool
	score  int
}

var (
	sysFontOnce sync.Once
	sysFonts    []fontCandidate
)

func discoverSystemFonts() []fontCandidate {
	sysFontOnce.Do(func() {
		var dirs []string
		home, _ := os.UserHomeDir()
		switch runtime.GOOS {
		case "darwin":
			dirs = []string{
				"/System/Library/Fonts",
				"/System/Library/Fonts/Supplemental",
				"/Library/Fonts",
			}
			if home != "" {
				dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
			}
		case "windows":
			windir := os.Getenv("WINDIR")
			if windir == "" {
				windir = `C:\Windows`
			}
			dirs = []string{filepath.Join(windir, "Fonts")}
		default:
			dirs = []string{
				"/usr/share/fonts",
				"/usr/local/share/fonts",
			}
			if home != "" {
				dirs = append(dirs, filepath.Join(home, ".local", "share", "fonts"))
			}
		}

		seen := make(map[string]bool)
		var out []fontCandidate
		for _, dir := range dirs {
			_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(d.Name()))
				if ext != ".ttf" && ext != ".otf" && ext != ".ttc" {
					return nil
				}
				if seen[p] {
					return nil
				}
				seen[p] = true
				cand, ok := classifyFontFile(p)
				if ok {
					out = append(out, cand)
				}
				return nil
			})
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].score != out[j].score {
				return out[i].score > out[j].score
			}
			return out[i].path < out[j].path
		})
		sysFonts = out
	})
	return sysFonts
}

func classifyFontFile(p string) (fontCandidate, bool) {
	base := strings.ToLower(filepath.Base(p))
	// Skip bitmap emoji, icon, symbol, and hidden system faces that do not
	// contain outline text or cause huge scans.
	for _, skip := range []string{"emoji", "lastresort", "apple symbols", "zapf", "wingdings", "webdings", "symbol", "marlett"} {
		if strings.Contains(base, skip) {
			return fontCandidate{}, false
		}
	}

	bold := strings.Contains(base, "bold") || strings.Contains(base, "semibold") || strings.Contains(base, "medium") || strings.Contains(base, "heavy") || strings.Contains(base, "-bd")
	italic := strings.Contains(base, "italic") || strings.Contains(base, "oblique") || strings.Contains(base, "-it")

	score := 0
	family := FontSans
	cjk := false

	switch {
	case strings.Contains(base, "pingfang"):
		score, cjk = 120, true
	case strings.Contains(base, "hiragino") && strings.Contains(base, "sans"):
		score, cjk = 105, true
	case strings.Contains(base, "stheiti") || strings.Contains(base, "songti") || strings.Contains(base, "kaiti"):
		score, cjk = 95, true
		if strings.Contains(base, "songti") {
			family = FontSerif
		}
	case strings.Contains(base, "notosanscjk") || strings.Contains(base, "notosanssc") || strings.Contains(base, "wenquanyi"):
		score, cjk = 110, true
	case strings.Contains(base, "notoserifcjk"):
		score, cjk, family = 105, true, FontSerif
	case strings.Contains(base, "msyh") || strings.Contains(base, "yahei") || strings.Contains(base, "simhei") || strings.Contains(base, "deng"):
		score, cjk = 110, true
	case strings.Contains(base, "simsun") || strings.Contains(base, "nsimsun"):
		score, cjk, family = 100, true, FontSerif

	case strings.Contains(base, "menlo") || strings.Contains(base, "sfmono") || strings.Contains(base, "jetbrains") || strings.Contains(base, "consola") || strings.Contains(base, "dejavusansmono") || strings.Contains(base, "liberationmono") || strings.Contains(base, "courier"):
		family, score = FontMono, 90
	case strings.Contains(base, "times") || strings.Contains(base, "georgia") || strings.Contains(base, "stix") || strings.Contains(base, "notoserif") || strings.Contains(base, "liberationserif") || strings.Contains(base, "dejavuserif"):
		family, score = FontSerif, 85
	case strings.Contains(base, "sfns") && !strings.Contains(base, "mono") && !strings.Contains(base, "rounded") ||
		strings.Contains(base, "helvetica") || strings.Contains(base, "arial") || strings.Contains(base, "inter") ||
		strings.Contains(base, "segoe") || strings.Contains(base, "notosans") || strings.Contains(base, "dejavusans") ||
		strings.Contains(base, "liberationsans"):
		family, score = FontSans, 95
	case strings.Contains(base, "arialuni") || strings.Contains(base, "unicode"):
		score, cjk = 80, true
	default:
		// Ignore obscure specialty fonts until needed.
		return fontCandidate{}, false
	}
	if !bold && !italic {
		score += 4
	}
	return fontCandidate{path: p, family: family, bold: bold, italic: italic, cjk: cjk, score: score}, true
}

type type3GlyphKey struct {
	face *faceEntry
	gid  sfnt.GlyphIndex
	r    rune
}

type type3Glyph struct {
	code    byte
	unicode string
	width   float64
	stream  []byte
}

type type3Subset struct {
	alias     string
	objID     int
	glyphs    []type3Glyph
	firstChar int
	lastChar  int
}

type fontManager struct {
	regFaces []*faceEntry
	sysFaces []*faceEntry
	glyphMap map[type3GlyphKey]glyphRef
	subsets  []*type3Subset
	baseUsed map[string]int // Base-14 PDF name -> object ID
	buf      sfnt.Buffer
}

type glyphRef struct {
	subset *type3Subset
	code   byte
	width  float64 // em-normalized advance in 1000-unit font space
}

func newFontManager() *fontManager {
	fm := &fontManager{
		glyphMap: make(map[type3GlyphKey]glyphRef),
		baseUsed: make(map[string]int),
	}
	for _, c := range discoverSystemFonts() {
		// Expand main style subfaces (at most index 0, plus index 1 for TTC regular/bold checks if needed).
		fm.sysFaces = append(fm.sysFaces, &faceEntry{
			path:   c.path,
			family: c.family,
			bold:   c.bold,
			italic: c.italic,
			cjk:    c.cjk,
			score:  c.score,
		})
	}
	return fm
}

func (fm *fontManager) close() {
	for _, fe := range fm.sysFaces {
		fe.close()
	}
	for _, fe := range fm.regFaces {
		fe.close()
	}
}

func (fm *fontManager) registerFont(family FontFamily, bold, italic bool, data []byte) {
	if len(data) == 0 {
		return
	}
	fm.regFaces = append(fm.regFaces, &faceEntry{
		data:   data,
		family: family,
		bold:   bold,
		italic: italic,
		score:  200,
	})
}

func (fm *fontManager) candidatesFor(style TextStyle) []*faceEntry {
	var out []*faceEntry
	// Registered embedded fonts (e.g. KaTeX) first: exact bold match before
	// same-family weight fallback.
	for _, fe := range fm.regFaces {
		if fe.family == style.Family && fe.bold == style.Bold {
			out = append(out, fe)
		}
	}
	for _, fe := range fm.regFaces {
		if fe.family == style.Family && fe.bold != style.Bold {
			out = append(out, fe)
		}
	}
	// System faces scored by matching family, bold, italic, and CJK affinity.
	type scored struct {
		fe *faceEntry
		s  int
	}
	var list []scored
	for _, fe := range fm.sysFaces {
		s := fe.score
		if fe.family == style.Family {
			s += 200
		} else if style.Family == FontMathItalic && fe.family == FontSerif {
			s += 120
		} else if (style.Family == FontMath || style.Family == FontMathItalic) && fe.family == FontSans {
			s += 60
		} else if style.Family == FontSerif && fe.family == FontSans {
			s += 20
		}
		if fe.bold == style.Bold {
			s += 80
		}
		if fe.italic == style.Italic || (style.Family == FontMathItalic && fe.italic) {
			s += 60
		}
		list = append(list, scored{fe, s})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].s > list[j].s })
	for _, it := range list {
		out = append(out, it.fe)
	}
	return out
}

// resolveRune finds a Type 3 glyph for r under style, loading outline segments
// from sfnt into a document-shared Type 3 subset font.
func (fm *fontManager) resolveRune(r rune, style TextStyle, allocID func() int) (glyphRef, bool) {
	cands := fm.candidatesFor(style)
	for _, fe := range cands {
		f, err := fe.getFont()
		if err != nil || f == nil {
			continue
		}
		gid, err := f.GlyphIndex(&fm.buf, r)
		if err != nil || gid == 0 {
			continue
		}
		key := type3GlyphKey{face: fe, gid: gid, r: r}
		if ref, ok := fm.glyphMap[key]; ok {
			return ref, true
		}
		adv, err := f.GlyphAdvance(&fm.buf, gid, fixed.I(1000), font.HintingNone)
		if err != nil {
			continue
		}
		wx := float64(adv) / 64.0
		segs, err := f.LoadGlyph(&fm.buf, gid, fixed.I(1000), nil)
		if err != nil {
			// Skip color bitmap emoji (ErrColoredGlyph) cleanly.
			continue
		}
		stream := buildCharProc(wx, segs)
		ref := fm.appendGlyph(key, string(r), wx, stream, allocID)
		return ref, true
	}
	if r > 0x7e {
		// Keep color bitmap emoji and non-outline symbols in /ToUnicode for search/copy.
		fallbackKey := type3GlyphKey{r: r}
		if ref, ok := fm.glyphMap[fallbackKey]; ok {
			return ref, true
		}
		ref := fm.appendGlyph(fallbackKey, string(r), 1000.0, []byte("1000.0 0 0 0 0 0 d1\n"), allocID)
		return ref, true
	}
	return glyphRef{}, false
}

func (fm *fontManager) appendGlyph(key type3GlyphKey, uni string, width float64, stream []byte, allocID func() int) glyphRef {
	var sub *type3Subset
	if n := len(fm.subsets); n > 0 && len(fm.subsets[n-1].glyphs) < 255 {
		sub = fm.subsets[n-1]
	} else {
		sub = &type3Subset{
			alias: fmt.Sprintf("/VF%d", n+1),
			objID: allocID(),
		}
		fm.subsets = append(fm.subsets, sub)
	}
	code := byte(len(sub.glyphs) + 1)
	if sub.firstChar == 0 {
		sub.firstChar = int(code)
	}
	sub.lastChar = int(code)
	sub.glyphs = append(sub.glyphs, type3Glyph{
		code:    code,
		unicode: uni,
		width:   width,
		stream:  stream,
	})
	ref := glyphRef{subset: sub, code: code, width: width / 1000.0}
	fm.glyphMap[key] = ref
	return ref
}

func (fm *fontManager) base14For(style TextStyle, allocID func() int) (alias, pdfName string, objID int) {
	switch style.Family {
	case FontMono:
		switch {
		case style.Bold && style.Italic:
			pdfName = "Courier-BoldOblique"
		case style.Bold:
			pdfName = "Courier-Bold"
		case style.Italic:
			pdfName = "Courier-Oblique"
		default:
			pdfName = "Courier"
		}
	case FontSerif, FontMath, FontMathItalic:
		switch {
		case style.Bold && (style.Italic || style.Family == FontMathItalic):
			pdfName = "Times-BoldItalic"
		case style.Bold:
			pdfName = "Times-Bold"
		case style.Italic || style.Family == FontMathItalic:
			pdfName = "Times-Italic"
		default:
			pdfName = "Times-Roman"
		}
	default:
		switch {
		case style.Bold && style.Italic:
			pdfName = "Helvetica-BoldOblique"
		case style.Bold:
			pdfName = "Helvetica-Bold"
		case style.Italic:
			pdfName = "Helvetica-Oblique"
		default:
			pdfName = "Helvetica"
		}
	}
	id, ok := fm.baseUsed[pdfName]
	if !ok {
		id = allocID()
		fm.baseUsed[pdfName] = id
	}
	return "/B14_" + strings.ReplaceAll(pdfName, "-", "_"), pdfName, id
}

func buildCharProc(wx float64, segs sfnt.Segments) []byte {
	var body bytes.Buffer
	var llx, lly, urx, ury float64
	hasPts := false
	expand := func(x, y float64) {
		if !hasPts {
			llx, urx = x, x
			lly, ury = y, y
			hasPts = true
			return
		}
		if x < llx {
			llx = x
		}
		if x > urx {
			urx = x
		}
		if y < lly {
			lly = y
		}
		if y > ury {
			ury = y
		}
	}

	var curX, curY float64
	started := false
	for _, seg := range segs {
		switch seg.Op {
		case sfnt.SegmentOpMoveTo:
			if started {
				body.WriteString("h\n")
			}
			x := float64(seg.Args[0].X) / 64.0
			y := -float64(seg.Args[0].Y) / 64.0
			expand(x, y)
			fmt.Fprintf(&body, "%.2f %.2f m\n", x, y)
			curX, curY = x, y
			started = true

		case sfnt.SegmentOpLineTo:
			x := float64(seg.Args[0].X) / 64.0
			y := -float64(seg.Args[0].Y) / 64.0
			expand(x, y)
			fmt.Fprintf(&body, "%.2f %.2f l\n", x, y)
			curX, curY = x, y

		case sfnt.SegmentOpQuadTo:
			qx := float64(seg.Args[0].X) / 64.0
			qy := -float64(seg.Args[0].Y) / 64.0
			p2x := float64(seg.Args[1].X) / 64.0
			p2y := -float64(seg.Args[1].Y) / 64.0
			c1x := curX + (2.0/3.0)*(qx-curX)
			c1y := curY + (2.0/3.0)*(qy-curY)
			c2x := p2x + (2.0/3.0)*(qx-p2x)
			c2y := p2y + (2.0/3.0)*(qy-p2y)
			expand(c1x, c1y)
			expand(c2x, c2y)
			expand(p2x, p2y)
			fmt.Fprintf(&body, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", c1x, c1y, c2x, c2y, p2x, p2y)
			curX, curY = p2x, p2y

		case sfnt.SegmentOpCubeTo:
			c1x := float64(seg.Args[0].X) / 64.0
			c1y := -float64(seg.Args[0].Y) / 64.0
			c2x := float64(seg.Args[1].X) / 64.0
			c2y := -float64(seg.Args[1].Y) / 64.0
			p3x := float64(seg.Args[2].X) / 64.0
			p3y := -float64(seg.Args[2].Y) / 64.0
			expand(c1x, c1y)
			expand(c2x, c2y)
			expand(p3x, p3y)
			fmt.Fprintf(&body, "%.2f %.2f %.2f %.2f %.2f %.2f c\n", c1x, c1y, c2x, c2y, p3x, p3y)
			curX, curY = p3x, p3y
		}
	}
	if started {
		body.WriteString("h f\n")
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "%.1f 0 %.1f %.1f %.1f %.1f d1\n", wx, llx, lly, urx, ury)
	out.Write(body.Bytes())
	return out.Bytes()
}

func flateCompress(data []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}

func (w *Writer) writeFontObjects() {
	// 1. Base-14 fallback dictionaries.
	names := make([]string, 0, len(w.fm.baseUsed))
	for name := range w.fm.baseUsed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		id := w.fm.baseUsed[name]
		w.object(id, fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", name), nil)
	}

	// 2. Type 3 vector outline font subsets + /CharProcs + /ToUnicode CMaps.
	for _, sub := range w.fm.subsets {
		if len(sub.glyphs) == 0 {
			continue
		}
		procIDs := make([]int, len(sub.glyphs))
		for i, g := range sub.glyphs {
			pid := w.allocID()
			procIDs[i] = pid
			comp := flateCompress(g.stream)
			w.object(pid, fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>", len(comp)), comp)
		}

		// /CharProcs dictionary
		var procs strings.Builder
		procs.WriteString("<< ")
		var enc strings.Builder
		enc.WriteString(fmt.Sprintf("<< /Type /Encoding /Differences [ %d ", sub.firstChar))
		var widths strings.Builder
		widths.WriteString("[ ")
		for i, g := range sub.glyphs {
			fmt.Fprintf(&procs, "/g%02X %d 0 R ", g.code, procIDs[i])
			fmt.Fprintf(&enc, "/g%02X ", g.code)
			fmt.Fprintf(&widths, "%.1f ", g.width)
		}
		procs.WriteString(">>")
		enc.WriteString("] >>")
		widths.WriteString("]")

		// /ToUnicode CMap
		var cmap strings.Builder
		cmap.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /ArcMDType3 def\n/CMapType 2 def\n1 begincodespacerange\n<00> <FF>\nendcodespacerange\n")
		for start := 0; start < len(sub.glyphs); start += 100 {
			end := min(start+100, len(sub.glyphs))
			fmt.Fprintf(&cmap, "%d beginbfchar\n", end-start)
			for i := start; i < end; i++ {
				g := sub.glyphs[i]
				fmt.Fprintf(&cmap, "<%02X> <%s\n", g.code, strings.TrimPrefix(textString(g.unicode), "<FEFF"))
			}
			cmap.WriteString("endbfchar\n")
		}
		cmap.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
		cmapID := w.allocID()
		compCmap := flateCompress([]byte(cmap.String()))
		w.object(cmapID, fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>", len(compCmap)), compCmap)

		procDictID := w.allocID()
		w.object(procDictID, procs.String(), nil)
		encID := w.allocID()
		w.object(encID, enc.String(), nil)

		w.object(sub.objID, fmt.Sprintf(
			"<< /Type /Font /Subtype /Type3 /FontBBox [-500 -500 1500 1500] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs %d 0 R /Encoding %d 0 R /FirstChar %d /LastChar %d /Widths %s /ToUnicode %d 0 R >>",
			procDictID, encID, sub.firstChar, sub.lastChar, widths.String(), cmapID,
		), nil)
	}
}
