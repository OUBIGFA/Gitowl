package editor

import (
	"fmt"
	"image"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/mermaid"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/tex"
	"github.com/egoist/mygo/ui"
)

type layoutPageKey struct {
	has                          bool
	page                         Page
	fontSize, lineHeight, column float32
	fontFamily                   string
	noteKey                      string
	codeNoWrap                   bool
}

func sameLayoutPageKey(a, b layoutPageKey) bool {
	return a.has == b.has &&
		a.fontSize == b.fontSize &&
		a.lineHeight == b.lineHeight &&
		a.fontFamily == b.fontFamily &&
		a.column == b.column &&
		a.noteKey == b.noteKey &&
		a.codeNoWrap == b.codeNoWrap &&
		samePage(a.page, b.page)
}

type Editor struct {
	notesDoc      *document.Document
	notesRevision uint64
	notesKey      string
	noteRefs      []noteReferences
	noteOrder     []string
	imageResize   imageResize
	linkRevision  uint64
	wikiHover     wikiHoverState
	services      ui.Services
	pairs         []autoPair
	pairRevision  uint64
	noAutoPair    bool
	hoverID       string
	*Buffer
	Scroll                           ui.ScrollState
	RequestFocus                     bool
	handleIDs, decorationIDs         map[string]int
	rowIDs                           map[string]int
	rowSpace, decorationSpace        spatialIndex
	rowYOff, decYOff                 fenwick64
	handleYOff, foldYOff             fenwick64
	resizerYOff, outlineYOff         fenwick64
	handleEnds                       []blockSliceEnd
	hasLazyY                         bool
	rows                             []row
	decorations                      []decoration
	handles                          []blockHandle
	resizers                         []columnHandle
	cache                            map[string]cachedShape
	cacheClock                       uint64
	heightHints                      map[string]heightHint
	images                           map[string]ImageResource
	imageCaptionID                   string
	imageViewer                      imageViewer
	width, height, viewport, padding float32
	cacheRevision                    uint64
	resizePending                    bool
	resizeScroll                     float32
	windowResize                     windowResize
	resizeBatch                      bool
	resizeRemaining                  int
	layoutInputs                     layoutInputs
	cacheTheme                       ui.Theme
	ready, focused, dragging, reveal bool
	selectionDrag                    selectionDrag
	preferredX                       float32
	vertical                         bool
	lastInput                        time.Time
	dragID                           string
	dropIndex                        int
	resizing                         *columnHandle
	resizeX                          float32
	menuOpen                         bool
	menuID                           string
	menuSub                          string
	menuScroll                       ui.ScrollState
	tableSel                         tableSelection
	cellFillOpen, cellFillOpened     bool
	foldButtons                      []foldButton
	revealKey                        [2]uint64
	find                             finder
	revealAt                         *ui.TextInputRange
	// lastAnnotation is the hand-drawn mark the bar's pen applies;
	// drawIn the new mark drawing itself; annotatePanel the mark picker.
	lastAnnotation, lastAnnotationColor string
	drawIn                              annotationReveal
	annotatePanel, annotatePanelOpened  bool
	reduceMotion                        bool
	markPop                             markPopover
	openReveal                          openReveal
	// refusedAt is when typing last reached the locked page.
	refusedAt   time.Time
	tableDanger bool
	attach      func()
	// pasteImage takes a picture pasted from the clipboard; dropFiles takes
	// files dragged in from another app or pasted, to go before the top-level
	// block at the index (-1: at the caret). fileOver is where files dragged
	// over the page would go, the y of the line showing it, or -1.
	pasteImage func([]byte)
	// contentEnd is the bottom of the last block; laidBlank whether the
	// layout left room for the starter.
	contentEnd                float32
	laidBlank                 bool
	dropFiles                 func([]string, int)
	fileOver                  float32
	slashOpen                 bool
	slashQuery                string
	slashIndex                int
	slashDismissed            bool
	slashReveal               bool
	slashScroll               ui.ScrollState
	linkOpen                  bool
	linkURL                   string
	linkText                  string
	linkRange                 ui.TextInputRange
	linkOpened                bool
	inlineEditing, laidInline string
	notes                     map[string]noteTarget
	noteReturn                int
	noteHasReturn             bool
	noteOriginID              string
	noteOriginOffset          int
	readingSize, readingLine  float32
	fontFamily                string
	openLink                  func(string)
	outline                   []outlineItem
	outlineState              outlineState
	pointer                   ui.Point
	pointerInside             bool
	copiedID                  string
	copiedAt                  time.Time
	page                      Page
	laidPage                  layoutPageKey
	colorOpen                 bool
	colorID                   string
	langOpen                  bool
	langID                    string
	langAt                    ui.Rect
	colorAt                   ui.Rect
	words                     int
	wordsRevision             uint64
	wordsReady                bool
	wordCache                 map[string]cachedWordCount
	accessDoc                 *document.Document
	accessRev                 uint64
	accessStart               int
	accessEnd                 int
	accessVal                 string
	// wiki completes the [[link]] being typed (wikilinks.go).
	wiki wikiState
	// footerH is the height the host's footer took the last frame.
	footerH float32
	// printing paints for paper: no caret, hints, controls or hover
	// (print.go).
	printing bool
	// typewriter keeps the caret's line at the middle of the page; dim
	// fades the blocks but the caret's, as focus mode does.
	typewriter, dim bool
	// header is the page's icon and cover (header.go).
	header headerState
	// mathEditing is the math block the caret is in, which shows its TeX;
	// the others show the formula (math.go).
	mathEditing, laidMath       string
	mermaidEditing, laidMermaid string
	laidFindKey                 string
	bindings                    map[string]Combo
	openWiki, openTag           func(string)
	maths                       map[string]mathShape
	mathGlyphs                  map[string]*ui.TextLayout
	mermaids                    map[string]mermaidShape
	mermaidLayouts              map[string]*ui.TextLayout
	codeLex                     map[string]*codeLexCache
	codeNoWrap                  bool
	codeWrapOverride            map[string]bool
	codeScrollX                 map[string]float32
	codeBarDrag                 string
	codeBarGrab                 float32
	// bookmarks are the host's marks on the page (bookmarks.go).
	bookmarks bookmarkState
	// reserved are keys the host takes, which the page leaves alone.
	reserved []Combo
	// blocks is the block selection (blockselect.go).
	blocks blockSelection
	// column is the text column's width as last laid out.
	column float32
	// complete is the list of emoji and mentions being typed (complete.go);
	// autoSpace puts a space between Chinese and Latin text as it is typed.
	complete  completion
	autoSpace bool
	// goalEditing shows the box that takes a word goal of the writer's own.
	goalEditing bool
	goalText    string
	// cropper is the crop dialog (crop.go); cropImage the host's cut.
	cropper   imageCropper
	cropImage func(id, url string, r image.Rectangle)

	motion          motionState
	dragStartY      float32
	dragY           float32
	dragActive      bool
	inlineMath      inlineMathEdit
	inlineValidBox  tex.Box
	inlineValidID   string
	inlinePopRect   ui.Rect
	inlinePopLayout *ui.TextLayout
	inlinePopTextX  float32
	inlinePopTextY  float32
	inlinePopStart  int
	inlinePopLen    int
	smartBack       smartRollback
	mathFence       mathFenceRollback
	tableDrag       tableRowDrag
	tableColDrag    tableColDrag
	resizeMoved     bool
	scrollGlide     scrollGlideState
	unfoldPending   bool
	taskAnim        map[string]time.Time
	imageEnter      imageEnterState
	captionedImages map[string]bool
	scrollBounds    ui.Rect
	surfaceReserve  float32
}

type row struct {
	leafRevision uint64
	leaf         document.Leaf
	layout       *textLayout
	rect         ui.Rect
	x, y         float32
	picture      *imageFigure
	// number is the position of a numbered item in its list, from 1.
	number int
	// size is the font size, which sizes the row's hand-drawn marks.
	size        float32
	tableHeader bool
	// emptyColumn uses its outline as the hint, keeping the writing area clear.
	emptyColumn bool
	// Math geometry is relative to rect, so moving preceding paragraphs
	// also moves the formula and its preview without reshaping them.
	math       *tex.Box
	mathTop    float32
	mathScale  float32
	mathPanel  float32
	mathHidden bool
	mathErr    string
	mathError  *ui.TextLayout

	mermaid       *mermaid.Diagram
	mermaidTop    float32
	mermaidScale  float32
	mermaidPanel  float32
	mermaidHidden bool
	mermaidErr    string
	mermaidError  *ui.TextLayout
	yApplied      float64
}

// decoration is what the page draws behind its text: the fill of a code
// block or callout, the bar of a quote, a table's cells.
type decoration struct {
	kind   string
	rect   ui.Rect
	header bool
	// table is the whole table a cell belongs to, which clips its header's
	// fill to the table's rounded corners; right and below draw the cell's
	// inner rules, so neighbouring cells share one hairline.
	table        ui.Rect
	right, below bool
	label        string
	// tone is a callout's color.
	tone string
	// id is the block of a code block or table, whose hover controls act
	// on it.
	id      string
	ownerID string
	rowIdx  int
	colIdx  int
	// cols and rows are a table's column and row edges.
	cols, rows []float32
	// column identifies a column container; empty keeps its writing area
	// visible without focus.
	column         *document.Block
	empty          bool
	mermaidHidden  bool
	mermaidWellOff float32
	yApplied       float64
}

// hovered reports whether the pointer is over r, with slack around it for
// controls drawn just outside, as a table's add bars are.
func (e *Editor) hovered(r ui.Rect, slack float32) bool {
	p := e.pointer
	return e.pointerInside && !e.dragging && e.dragID == "" && p.X >= r.X-slack && p.X <= r.X+r.W+slack && p.Y >= r.Y-slack && p.Y <= r.Y+r.H+slack
}

// copyButton is where a code block shows its copy button.
func copyButton(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 76, Y: d.rect.Y + 7, W: 66, H: 22}
}

// languageButton is where a code block shows and picks its language.
func languageButton(d decoration) ui.Rect {
	if d.kind == "mermaid" {
		return mermaidLanguageButton(d)
	}
	return ui.Rect{X: d.rect.X + 12, Y: d.rect.Y + 7, W: 104, H: 22}
}

// codeLanguages are the languages the picker offers, by fence name.
var codeLanguages = []struct{ name, label string }{
	{"", "纯文本"}, {"go", "Go"}, {"javascript", "JavaScript"}, {"typescript", "TypeScript"}, {"python", "Python"},
	{"rust", "Rust"}, {"java", "Java"}, {"c", "C"}, {"cpp", "C++"}, {"csharp", "C#"}, {"swift", "Swift"},
	{"kotlin", "Kotlin"}, {"ruby", "Ruby"}, {"php", "PHP"}, {"bash", "Shell"}, {"sql", "SQL"},
	{"html", "HTML"}, {"css", "CSS"}, {"json", "JSON"}, {"yaml", "YAML"}, {"toml", "TOML"}, {"markdown", "Markdown"}, {"mermaid", "Mermaid"},
}

// languageLabel names a fence language for people.
func languageLabel(name string) string {
	for _, l := range codeLanguages {
		if strings.EqualFold(l.name, name) {
			return i18n.T(l.label)
		}
	}
	return name
}

// paintLanguage draws a code block's language button.
func (e *Editor) paintLanguage(p *ui.Painter, r ui.Rect, d decoration, t *ui.Theme) {
	label := languageLabel(d.label)
	if label == i18n.T("纯文本") || label == "" {
		label = "Plain Text"
	}
	span := ui.Span{Text: label, Size: 14, Color: t.Text}
	w, h := p.MeasureText(0, span)
	if e.langOpen && e.langID == d.id || e.hovered(languageButton(d), 0) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: w + 26, H: r.H}, e.page.Hover, 4)
	}
	p.RichText(r.X+4, r.Y+(r.H-h)/2, 0, span)
	p.Icon(iconChevron, ui.Rect{X: r.X + 6 + w, Y: r.Y + 4, W: 14, H: 14}, e.page.Placeholder)
}

// calloutIconRect is a callout's icon, which picks its color.
func calloutIconRect(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + 16, Y: d.rect.Y + 17, W: 24, H: 24}
}

// calloutHue is the color of a callout's tone.
func (e *Editor) calloutHue(tone string, t *ui.Theme) ui.Color {
	return e.page.Callouts[tone].Icon
}

// calloutIcon tells the tones apart by more than color, as GitHub's alerts
// do: a note, a tip, something important, a warning, a caution.
func calloutIcon(tone string) *ui.SVG {
	switch tone {
	case "green":
		return iconBulb
	case "purple":
		return iconNotice
	case "yellow":
		return iconWarn
	case "red":
		return iconStop
	}
	return iconInfo
}

// CalloutName names a callout color for menus and the command bar.
func CalloutName(tone string) string {
	switch tone {
	case "green":
		return i18n.T("绿色 · 提示")
	case "purple":
		return i18n.T("紫色 · 重要")
	case "yellow":
		return i18n.T("黄色 · 警告")
	case "red":
		return i18n.T("红色 · 危险")
	}
	return i18n.T("蓝色 · 说明")
}

// blockHandle is the grip beside a top-level block, with the add button
// before it; top is where the block begins, where a dragged block drops.
type blockSliceEnd struct {
	rowEnd, decEnd, resEnd, foldEnd, outEnd int
}

type blockHandle struct {
	id       string
	index    int
	rect     ui.Rect
	top      float32
	yApplied float64
}

// plus is the add button left of the grip, as Notion's.
func (h blockHandle) plus() ui.Rect {
	r := h.rect
	r.X -= 26
	return r
}

// foldButton is the triangle that folds a heading or list item.
type foldButton struct {
	id     string
	rect   ui.Rect
	folded bool
	// always shows the triangle, as a toggle's, not only under the pointer.
	always   bool
	yApplied float64
}

func isList(kind string) bool { return kind == "bullet" || kind == "numbered" || kind == "task" }

type columnHandle struct {
	id             string
	col            int
	x, y, h, width float32
	widths         []float32
	yApplied       float64
}

// outlineDrag is a heading of the outline dragged to move its section.
type outlineDrag struct{ id string }

// outlineItem is a heading of the outline beside the page; the page
// title is one too.
type outlineItem struct {
	id, text string
	level    int
	y        float32
	yApplied float64
}

// pageTop is the room above the first block, as a page's top margin.
const pageTop = 48

// pageBottom is the room left below the last block, and starterRoom the
// more a blank document leaves for the starter.
const (
	pageBottom  = 140
	starterRoom = 120
)

// Keep exact, immutable inputs instead of JSON-encoding every paragraph on
// each resize frame. Runs are copied only when the shape actually changes.
type shapeStyle struct {
	font                                   ui.Font
	align, kind, language, revealed, notes string
	checked, header, noWrap                bool
}

type cachedShape struct {
	key      shapeStyle
	runs     []document.Run
	width    float32
	layout   *textLayout
	lastUsed uint64
}

type Options struct {
	// Resizing keeps offscreen shaping cached during an interactive resize.
	// Hosts must clear it on release; the final layout is always exact.
	Resizing             bool
	NoAutoPair           bool
	CodeNoWrap           bool
	FontSize, LineHeight float32
	// FontFamily is the body text's family list, "" for the system's; Column
	// is the text column's width, 0 for 820. Code keeps its monospace.
	FontFamily   string
	Column       float32
	Width        float32
	Theme        *ui.Theme
	LoadImage    func(string) ImageResource
	LoadRawImage func(string) image.Image
	// ReplaceImage selects a new file for a stable block ID. RevealImage
	// opens its local file (or nearest existing folder) in the file manager.
	ReplaceImage func(id, url string)
	RevealImage  func(url string)
	// CropImage, when set, lets a picture be cropped: the host keeps the
	// part r, in the picture's pixels, as a new file and points the
	// picture id at it.
	CropImage func(id, url string, r image.Rectangle)
	// Attach, when set, lets the slash menu insert images: the host picks
	// them and inserts them at the caret.
	Attach func()
	// DropFiles, when set, makes the page take files dragged in from other
	// apps and files pasted from the clipboard: at is the top-level block
	// they go before, len(Doc.Blocks) for the end, or -1 for the caret.
	// The host decides which files it can insert.
	DropFiles func(paths []string, at int)
	// PasteImage, when set, takes a picture pasted from the clipboard, as
	// PNG.
	PasteImage func(png []byte)
	// Page are the page's colors beyond Theme; empty ones follow Theme.
	Page     Page
	OpenLink func(string)
	// OpenWiki opens the document a [[link]] names, ⌘-clicked; OpenTag
	// shows the documents of a #tag. WikiSuggest lists the documents to
	// complete a [[link]] being typed with, by what is typed of its name.
	OpenWiki    func(name string)
	OpenTag     func(tag string)
	WikiSuggest func(typed string) []WikiItem
	// EmojiSuggest lists the emoji whose shortcodes match what is typed
	// after a colon, the best first.
	EmojiSuggest func(typed string) []EmojiItem
	// AutoSpace puts a space between Chinese and Latin letters or digits
	// as they are typed next to each other.
	AutoSpace   bool
	PreviewWiki func(name string) WikiPreview
	// Footer, when set, draws below the document, in the page's text
	// column: the host's backlinks.
	Footer func(c *ui.Context)
	// DrawAnnotations lets the marks in view draw themselves, one after
	// another, the first time this editor shows its document.
	DrawAnnotations bool
	// Typewriter keeps the caret's line in the middle of the page; Dim
	// fades every block but the caret's.
	Typewriter, Dim bool
	// PickCover, when set, lets the cover be a picture: the host picks one,
	// puts it beside the document and returns its URL.
	PickCover func(done func(url string))
	// Bindings rebind the editor's commands (DefaultBindings) to other
	// keys; a command left out keeps its default.
	Bindings map[string]Combo
	// Outline opens the outline left of the page; SetOutline, when set,
	// shows the outline's buttons and keeps what the reader picks. Without
	// it the page has no outline, as in thumbnails.
	Outline    bool
	SetOutline func(open bool)
	// Bookmarks are the top-level blocks bookmarked, by ID, with the label
	// the reader gave each ("" for none). ToggleBookmark, when set, shows
	// them: a mark beside each, which takes it away, and a section of the
	// outline; LabelBookmark names one. BookmarkKey is the key that toggles
	// one, for tooltips.
	Bookmarks      map[string]string
	ToggleBookmark func(id string)
	LabelBookmark  func(id, label string)
	BookmarkKey    string
	// Reserved are keys the host handles, which the page does not take
	// even where it would, as ⌃⌘↑ would move the caret.
	Reserved []Combo
}

func New(markdown string) *Editor { return FromOwnedDocument(ImportMarkdown(markdown)) }
func FromDocument(doc *document.Document) *Editor {
	return newEditorWithBuffer(newBuffer(doc))
}

// FromOwnedDocument creates an Editor that takes ownership of doc without cloning
// it. Callers must not mutate or reuse doc elsewhere.
func FromOwnedDocument(doc *document.Document) *Editor {
	return newEditorWithBuffer(newOwnedBuffer(doc))
}

func newEditorWithBuffer(buf *Buffer) *Editor {
	// The color button starts out as a yellow highlighter.
	return &Editor{Buffer: buf, cache: make(map[string]cachedShape), images: make(map[string]ImageResource), dropIndex: -1, lastAnnotation: "wavy",
		wiki: wikiState{dismissed: -1}, complete: completion{dismissed: -1}}
}
func (e *Editor) ReplaceText(r *ui.TextInputRange, text string) {
	if e.ReadOnly {
		e.refused = true
		return
	}
	e.editImageCaption()
	e.clearBlocks()
	if e.autoSpace {
		// Typed at the caret, or committed by an input method in place of
		// what it was composing.
		if m, composing := e.MarkedRange(); composing && (r == nil || *r == m) {
			text = e.spaced(text, m.Start)
		} else if r == nil && e.Anchor == e.Caret {
			text = e.spaced(text, e.Caret)
		}
	}
	if !e.pairInput(r, text) {
		e.Buffer.ReplaceText(r, text)
	}
	e.reveal, e.vertical = true, false
	e.slashDismissed = false
}
func (e *Editor) SetMarkedText(r *ui.TextInputRange, text string, selected ui.TextInputRange) {
	if e.ReadOnly {
		e.refused = e.refused || text != ""
		return
	}
	e.editImageCaption()
	e.Buffer.SetMarkedText(r, text, selected)
	e.reveal = true
}

func View(c *ui.Context, e *Editor, o Options) ui.Element {
	e.services = c.Services()
	// Native window drags do not go through a split divider or the host's
	// panel animation. Detect them here, including in detached windows.
	if e.windowResize.active(c) {
		e.windowResize.pending = true
		o.Resizing = true
	}
	e.noAutoPair, e.autoSpace, e.codeNoWrap = o.NoAutoPair, o.AutoSpace, o.CodeNoWrap
	if e.ReadOnly {
		o.Attach = nil
		o.DropFiles = nil
		o.PasteImage = nil
		o.ReplaceImage = nil
		o.PickCover = nil
		o.CropImage = nil
	}
	e.page = pageOf(o.Page, o.Theme)
	e.attach = o.Attach
	e.cropImage = o.CropImage
	e.dropFiles, e.pasteImage = o.DropFiles, o.PasteImage
	e.typewriter, e.dim, e.bindings = o.Typewriter, o.Dim, o.Bindings
	if !e.openReveal.checked {
		e.openReveal = openReveal{checked: true, pending: o.DrawAnnotations && hasAnnotations(e.Doc.Blocks)}
	}
	e.openWiki, e.openTag, e.openLink = o.OpenWiki, o.OpenTag, o.OpenLink
	e.header.pickCover = o.PickCover
	e.bookmarks.marks, e.bookmarks.toggle, e.bookmarks.label, e.bookmarks.key = o.Bookmarks, o.ToggleBookmark, o.LabelBookmark, o.BookmarkKey
	e.reserved = o.Reserved
	e.sync()
	if e.imageCaptionID != "" && e.Engine.LeafAt(e.Caret).Block.ID != e.imageCaptionID {
		e.imageCaptionID, e.ready = "", false
	}
	e.findShortcuts(c)
	e.revealCaret()
	e.watchMath()
	e.watchMermaid()
	e.watchInline()
	pane := ui.Column(c).Grow(1).MinHeight(0).MinWidth(0)
	// The outline, open, takes room left of the page: the page slides over
	// as it opens, keeping offscreen shaping while it moves.
	reserve := e.outlineReserve(c, o)
	if reserve > 0 {
		o.Width -= reserve
	}
	if reserve != e.outlineState.target {
		o.Resizing = true
	}
	e.reduceMotion = c.Preferences().ReduceMotion
	e.layoutForView(c, o)
	e.updateMotion(c.Now(), o)
	e.pruneBlocks()
	e.announceBlocks(c)
	e.watchSlash()
	e.watchWiki(o.WikiSuggest)
	e.watchComplete(c.Now(), o.EmojiSuggest, o.WikiSuggest)
	var surface ui.Element
	pane.Children(func() {
		e.stepJump(c)
		e.stepScrollGlide(c)
		if e.revealAt != nil && e.viewport > 0 {
			// A find match scrolls to a third of the way down the page.
			if r, _, ok := e.BoundsForRange(*e.revealAt); ok && (r.Y < e.Scroll.Y+20 || r.Y+r.H > e.Scroll.Y+e.viewport-32) {
				e.glideScrollTo(c, max(0, r.Y-e.viewport/3), 0.055)
			}
			e.revealAt = nil
		}
		if e.reveal && e.viewport > 0 {
			e.revealCodeCaret()
		}
		if e.reveal && e.viewport > 0 && e.typewriter {
			// The caret's line stays in the middle of the page.
			if r, _, ok := e.BoundsForRange(ui.TextInputRange{Start: e.Caret, End: e.Caret}); ok {
				e.glideScrollTo(c, max(0, r.Y+r.H/2-e.viewport/2), 0.050)
			}
			e.reveal = false
		}
		if e.reveal && e.viewport > 0 {
			r, _, ok := e.BoundsForRange(ui.TextInputRange{Start: e.Caret, End: e.Caret})
			if ok {
				if r.Y < e.Scroll.Y+20 {
					e.glideScrollTo(c, max(0, r.Y-20), 0.042)
				} else if r.Y+r.H > e.Scroll.Y+e.viewport-32 {
					e.glideScrollTo(c, max(0, r.Y+r.H-e.viewport+32), 0.042)
				}
			}
			e.reveal = false
		}
		scroll := ui.Scroll(c.Key("page-scroll")).Grow(1).MinHeight(0)
		// Bounds are the last frame's: none yet in the first frame of a new
		// document, where the window's height stands in, or painting would
		// cull every row as out of view and nothing would draw until
		// another frame came.
		// The box as laid out, before transforms: a scaled thumbnail shows all
		// its rows.
		e.viewport = scroll.LayoutBounds().H
		if e.viewport <= 0 {
			_, e.viewport = c.Size()
		}
		e.scrollBounds = scroll.Bounds()
		e.surfaceReserve = reserve
		e.stepSelectionDrag(c, scroll, reserve)
		scroll.TrackScroll(&e.Scroll)
		scroll.Children(func() {
			height := e.height
			if o.Footer != nil {
				height = max(height, e.footerTop()+e.footerH+pageBottom)
			}
			if e.typewriter {
				// Room for the last line to come to the middle.
				height += e.viewport / 2
			}
			// The trailing space has no ink. Whole-DIP bounds keep full and
			// incremental layouts from clamping scroll at different float tails.
			height = float32(math.Round(float64(height)))
			// The surface and what is placed over it scroll together in one
			// box: placed elements stay where the scroll's content was laid
			// out, not where it scrolled to.
			e.outlineState.scrollMax = max(0, max(height, e.viewport)-e.viewport)
			e.revealPending(c)
			content := ui.Box(c.Key("page-content")).Width(o.Width).Height(max(height, e.viewport)).Shrink(0).MarginStart(reserve)
			content.Children(func() {
				surface = ui.Box(c.Key("writing-surface")).Width(o.Width).Height(max(height, e.viewport)).Shrink(0).
					HandleTextInput(e).Role(ui.RoleTextField).Label(i18n.T("文档")).FocusRing(false)
				accessTextValue(surface.Access, e.accessText)
				if e.RequestFocus {
					surface.Focus()
					e.lastInput = c.Now()
					e.RequestFocus = false
				}
				e.focused = surface.Focused()
				px, py, inside := surface.PointerPosition()
				e.pointer, e.pointerInside = ui.Point{X: px, Y: py}, inside
				e.fileOver = -1
				if o.DropFiles != nil {
					// A file drag keeps the pointer's position up to date, so
					// the frame after the drop still has where it landed.
					at, line := e.fileDropAt(py)
					if surface.FileDragOver() {
						e.fileOver = line
					}
					if files := surface.DroppedFiles(); len(files) > 0 {
						o.DropFiles(files, at)
					}
				}
				if e.resizing != nil || e.hoveredResizer() != nil {
					surface.Cursor(ui.CursorResizeEW)
				} else if _, ok := e.hoveredGap(); ok {
					// A click here adds a paragraph rather than placing the caret.
					surface.Cursor(ui.CursorPointer)
				}
				e.hoverID = ""
				if inside {
					if i := sort.Search(len(e.handles), func(i int) bool { return e.indexedHandle(i).top > py+4 }) - 1; i >= 0 {
						e.hoverID = e.indexedHandle(i).id
					}
				}
				services := c.Services()
				surface.HandleInput(func(ev ui.InputEvent) bool {
					return e.input(ev, services.ReadClipboard, services.WriteClipboard, o.OpenLink)
				})
				surface.ContextMenu(func(menu *ui.Menu) {
					menuAction(menu, i18n.T("加粗"), func() { e.Wrap("**") })
					menuAction(menu, i18n.T("斜体"), func() { e.Wrap("*") })
					menuAction(menu, i18n.T("插入表格"), func() { e.Engine.Insert(document.Table(3, 3)); e.RequestFocus = true })
				})
				caret := !e.ReadOnly && e.focused && (c.Now().Sub(e.lastInput) < time.Second || c.Now().UnixMilli()/530%2 == 0)
				if e.focused {
					c.After(530 * time.Millisecond)
				}
				e.reduceMotion = c.Preferences().ReduceMotion
				surface.Draw(func(p *ui.Painter, r ui.Rect) { e.paint(p, r, o.Theme, caret) })
				// Beside the surface, not in it: the surface takes the
				// pointer's presses before anything inside it.
				if !e.ReadOnly {
					e.starter(c)
					e.headerControls(c)
				}
				e.imageControls(c, o)
				e.bookmarkMarks(c)
				if o.Footer != nil {
					footer := ui.Column(c.Key("page-footer")).Absolute().Left(e.padding).Top(e.footerTop()).Width(max(80, e.width-2*e.padding))
					footer.Children(func() { o.Footer(c) })
					if h := footer.LayoutBounds().H; h != e.footerH {
						e.footerH = h
						c.After(0)
					}
				}
			})
		})
		e.outlineView(c, pane, reserve)
		e.findBox(c)
	})
	if !e.ReadOnly {
		e.popups(c, surface)
		e.wikiMenu(c, surface)
		e.completeMenu(c, surface)
		e.headerPopups(c, surface)
	}
	e.imagePreview(c)
	e.cropView(c)
	e.wikiPreview(c, surface, o.PreviewWiki)
	return pane
}

// footerTop is where the host's footer goes: below the last block, and
// the starter when it shows.
func (e *Editor) footerTop() float32 {
	top := e.contentEnd + 40
	if e.laidBlank {
		top += starterRoom
	}
	return top
}

// surfaceBounds returns the writing surface's window-space bounds adjusted for
// the current frame's Scroll.Y and outline reserve, avoiding a one-frame lag
// when popovers open or reposition on the same frame the editor scrolls.
func (e *Editor) surfaceBounds(surface ui.Element) ui.Rect {
	b := surface.Bounds()
	if e.scrollBounds.H > 0 {
		sy := min(max(0, e.Scroll.Y), e.outlineState.scrollMax)
		b.X = e.scrollBounds.X + e.surfaceReserve
		b.Y = e.scrollBounds.Y - sy
		if b.W == 0 {
			b.W = e.width
		}
	}
	return b
}

func (e *Editor) layout(o Options) {
	e.collectNotes()
	e.readingSize, e.readingLine, e.fontFamily = o.FontSize, o.LineHeight, o.FontFamily
	defer func() { e.laidInline = e.inlineEditing }()
	pageKey := layoutPageKey{
		has:        true,
		page:       e.page,
		fontSize:   o.FontSize,
		lineHeight: o.LineHeight,
		fontFamily: o.FontFamily,
		column:     o.Column,
		noteKey:    e.noteKey(),
		codeNoWrap: e.codeNoWrap,
	}
	finishResize := e.resizePending && (!o.Resizing || e.resizeBatch)
	refreshResize := e.resizePending && e.resizeScroll != e.Scroll.Y
	anchor := resizeAnchor{}
	if e.resizePending || o.Resizing {
		anchor = e.resizeAnchor()
	}
	pageSame := sameLayoutPageKey(e.laidPage, pageKey)
	findKey := ""
	if e.find.open {
		findKey = e.find.query
	}
	same := !finishResize && !refreshResize && e.ready && e.width == o.Width && e.cacheTheme == *o.Theme && pageSame && e.laidMath == e.mathEditing && e.laidMermaid == e.mermaidEditing && e.laidFindKey == findKey && e.laidInline == e.inlineEditing
	if same && e.cacheRevision == e.Revision() {
		return
	}
	if same && e.relayoutText(o) {
		e.cacheRevision = e.Revision()
		return
	}
	if e.cacheTheme != *o.Theme || !pageSame {
		clear(e.cache)
		clear(e.heightHints)
		clear(e.mathGlyphs)
		clear(e.maths)
		clear(e.mermaidLayouts)
		clear(e.mermaids)
		clear(e.codeLex)
		pageKey.page = clonePage(e.page)
	} else {
		pageKey.page = e.laidPage.page
	}
	if !e.ready {
		e.Engine.InvalidateIndex()
	}
	inputs := &e.layoutInputs
	inputs.refresh(e.Doc, e.Revision(), e.ready)
	e.resizePending = false
	e.resizeScroll = e.Scroll.Y
	e.ready, e.width, e.cacheRevision, e.cacheTheme, e.laidPage = true, o.Width, e.Revision(), *o.Theme, pageKey
	e.laidMath = e.mathEditing
	e.laidMermaid = e.mermaidEditing
	e.laidFindKey = findKey
	// The text column is 820 wide unless the reader picked another.
	e.column = columnWidth(o.Column)
	e.padding = max(46, (o.Width-e.column)/2)
	width := max(80, o.Width-2*e.padding)
	clear(e.rows)
	clear(e.decorations)
	clear(e.handles)
	clear(e.resizers)
	e.rows = e.rows[:0]
	e.decorations = e.decorations[:0]
	e.handles = e.handles[:0]
	e.resizers = e.resizers[:0]
	hidden, foldable := inputs.hidden, inputs.foldable
	blockLayout := e.blockLayouter(o, func(id string) document.Leaf { l, _ := e.Engine.LeafByID(id); return l }, hidden, false)
	y := e.layoutHeader(o)
	anchorPlaced := false
	e.outline = e.outline[:0]
	e.foldButtons = e.foldButtons[:0]
	e.handleEnds = e.handleEnds[:0]
	wantLazyY := e.shouldVirtualize()
	for i, b := range e.Doc.Blocks {
		if hidden[b.ID] != "" {
			continue
		}
		start := len(e.rows)
		if b.ID == anchor.id && len(b.Children) == 0 {
			// Place a top-level reading anchor before shaping the viewport.
			// Otherwise a large width change first shapes around the old
			// absolute offset and needs another whole layout after restoring it.
			e.Scroll.Y = max(0, y+anchor.offset)
			anchorPlaced = true
		}
		h := blockLayout(b, e.padding, y, width)
		if b.ID == anchor.id && anchorPlaced {
			e.Scroll.Y = max(0, y+min(anchor.offset, max(0, h-1)))
		}
		// The handle sits beside the block's first line.
		hy := y + 2
		if start < len(e.rows) && e.rows[start].layout != nil {
			r := e.rows[start]
			c := r.layout.Caret(0)
			hy = r.y + c.Y + max(c.H, 20)/2 - 12
		}
		hx := e.padding - 30
		if b.Kind == "table" {
			// A table's handle sits left of its column bar, clear of the
			// row bar.
			hx, hy = e.padding-48, y+22-21
		}
		if foldable[b.ID] {
			// The fold triangle sits just left of the block, 20
			// left of the text; the handle further out.
			fx := e.padding - 22
			if isList(b.Kind) {
				fx += float32(b.Indent) * 24
			}
			if b.Kind == "toggle" {
				// A toggle's triangle is its marker, always shown.
				fx = e.padding + float32(b.Indent)*20 - 1
			}
			e.foldButtons = append(e.foldButtons, foldButton{id: b.ID, rect: ui.Rect{X: fx, Y: hy + 4, W: 16, H: 16}, folded: b.Folded, always: b.Kind == "toggle"})
			if fx-hx < 26 {
				hx = fx - 28
			}
		}
		if b.Kind == "heading" && b.Text() != "" {
			e.outline = append(e.outline, outlineItem{b.ID, b.Text(), b.Level, y, 0})
		}
		e.handles = append(e.handles, blockHandle{
			id:    b.ID,
			index: i,
			rect:  ui.Rect{X: hx, Y: hy, W: 24, H: 24},
			top:   y,
		})
		if wantLazyY {
			e.handleEnds = append(e.handleEnds, blockSliceEnd{
				rowEnd:  len(e.rows),
				decEnd:  len(e.decorations),
				resEnd:  len(e.resizers),
				foldEnd: len(e.foldButtons),
				outEnd:  len(e.outline),
			})
		}
		y += h
	}
	if e.handleIDs == nil {
		e.handleIDs = make(map[string]int, len(e.handles))
	}
	clear(e.handleIDs)
	for i, h := range e.handles {
		e.handleIDs[h.id] = i
	}
	if e.decorationIDs == nil {
		e.decorationIDs = make(map[string]int, len(e.decorations))
	}
	clear(e.decorationIDs)
	for i, d := range e.decorations {
		e.decorationIDs[d.id] = i
	}
	if e.rowIDs == nil {
		e.rowIDs = make(map[string]int, len(e.rows))
	}
	clear(e.rowIDs)
	for i, r := range e.rows {
		e.rowIDs[r.leaf.Block.ID] = i
	}
	for id := range e.cache {
		if _, ok := e.rowIDs[id]; !ok {
			delete(e.cache, id)
		}
	}
	if len(e.heightHints) > len(e.rows)+cacheTrimHysteresis {
		for id := range e.heightHints {
			if _, hasDec := e.decorationIDs[id]; !hasDec && !e.Engine.HasLeaf(id) {
				delete(e.heightHints, id)
			}
		}
	}
	e.hasLazyY = wantLazyY
	if e.hasLazyY {
		e.rowYOff.reset(len(e.rows))
		e.decYOff.reset(len(e.decorations))
		e.handleYOff.reset(len(e.handles))
		e.foldYOff.reset(len(e.foldButtons))
		e.resizerYOff.reset(len(e.resizers))
		e.outlineYOff.reset(len(e.outline))
	} else {
		e.rowYOff.reset(0)
		e.decYOff.reset(0)
		e.handleYOff.reset(0)
		e.foldYOff.reset(0)
		e.resizerYOff.reset(0)
		e.outlineYOff.reset(0)
	}
	e.rowSpace.reset(len(e.rows), func(i int) ui.Rect { return e.indexedRow(i).rect })
	e.decorationSpace.reset(len(e.decorations), func(i int) ui.Rect { return e.indexedDecoration(i).rect })
	e.restoreResizeAnchor(anchor)
	if anchorPlaced {
		e.resizeScroll = e.Scroll.Y
	}
	e.contentEnd = y
	e.height = y + pageBottom
	e.laidBlank = e.Blank()
	if e.laidBlank {
		e.height += starterRoom
	}
}

func (e *Editor) layoutOptions() Options {
	return Options{
		Theme:      &e.cacheTheme,
		Page:       e.page,
		Width:      e.width,
		FontSize:   e.readingSize,
		LineHeight: e.readingLine,
		FontFamily: e.fontFamily,
		Column:     e.laidPage.column,
		CodeNoWrap: e.codeNoWrap,
	}
}

func (e *Editor) blockLayouter(o Options, leafOf func(string) document.Leaf, hidden map[string]string, forceShape bool) func(*document.Block, float32, float32, float32) float32 {
	// counters numbers the items of numbered lists by indent; any other
	// block ends the list.
	var counters []int
	header := false
	// cellTop is where the table row being laid out starts. Cells are laid
	// out at y 0 and moved afterwards, so resize deferral needs the real
	// position to tell whether a cell is on screen.
	cellTop := float32(0)
	virtualize := !forceShape && e.shouldVirtualize()
	var winTop, winBottom float32
	var caretID, anchorID, revealID, caretOwner, anchorOwner, revealOwner string
	if virtualize {
		winTop, winBottom = e.virtualWindow()
		caretID, anchorID, revealID, caretOwner, anchorOwner, revealOwner = e.activeBlockOwners()
	}
	var blockLayout func(*document.Block, float32, float32, float32) float32
	blockLayout = func(b *document.Block, x, y, w float32) float32 {
		if b.Kind == "columns" {
			return e.layoutColumns(b, x, y, w, hidden, blockLayout)
		}
		if b.Kind == "table" {
			tableActive := virtualize && (b.ID == e.dragID || b.ID == caretOwner || b.ID == anchorOwner || b.ID == revealOwner)
			counters = nil
			return e.layoutTable(b, x, y, w, virtualize && !tableActive, winTop, winBottom, leafOf, &cellTop, &header, blockLayout)
		}
		if b.Kind != "bullet" && b.Kind != "numbered" && b.Kind != "task" && b.Kind != "toggle" && b.Indent > 0 {
			indent := min(float32(b.Indent)*20, max(0, w-80))
			x += indent
			w -= indent
		}
		m := e.blockMetrics(b, w, header)
		font := m.font
		left, top, bottom, gap, shapeWidth := m.left, m.top, m.bottom, m.gap, m.shapeWidth
		number := 0
		switch b.Kind {
		case "bullet", "task":
			counters = counters[:min(len(counters), b.Indent)]
		case "numbered":
			counters = counters[:min(len(counters), b.Indent+1)]
			for len(counters) <= b.Indent {
				counters = append(counters, 0)
			}
			counters[b.Indent]++
			number = counters[b.Indent]
		default:
			counters = nil
		}
		leaf := leafOf(b.ID)
		key := e.blockShapeKey(b, font, header)
		cached, ok := e.cache[b.ID]
		sameShape := ok && cached.key == key && slices.Equal(cached.runs, b.Runs)
		active := virtualize && (b.ID == caretID || b.ID == anchorID || b.ID == revealID || b.ID == e.dragID || b.ID == e.mathEditing || b.ID == e.mermaidEditing || strings.HasPrefix(e.inlineEditing, b.ID+":"))
		deferShape := virtualize && !active && leaf.Owner == b.ID && canDeferKind(b.Kind)

		var laid *textLayout
		var h float32
		if sameShape {
			if cached.width == shapeWidth {
				cached.lastUsed = e.nextCacheClock()
				e.cache[b.ID] = cached
				laid = cached.layout
			} else if deferShape {
				_, oldHeight := cached.layout.Size()
				offscreen := y+top+oldHeight+bottom < winTop || y > winBottom
				if offscreen {
					if o.Resizing {
						e.resizePending = true
						laid = cached.layout
					} else {
						delete(e.cache, b.ID)
					}
				}
			} else {
				_, oldHeight := cached.layout.Size()
				if e.deferResizeShape(o, cellTop+y, oldHeight+top+bottom) {
					e.resizePending = true
					laid = cached.layout
				}
			}
		}
		if laid == nil && deferShape {
			estH := e.estimateBlockHeight(b, font, e.hintWidthFor(b, shapeWidth))
			total := top + max(estH, font.Size*font.LineHeight) + bottom
			if b.Kind == "math" || isMermaidBlock(b) {
				total = estH
			}
			if y+total < winTop || y > winBottom {
				if ok {
					delete(e.cache, b.ID)
				}
				row := row{leafRevision: e.Revision(), leaf: leaf, rect: ui.Rect{X: x, Y: y, W: w, H: total}, x: x + left, y: y + top, number: number, size: font.Size, tableHeader: header}
				if b.Kind == "math" {
					row.mathHidden = true
				} else if isMermaidBlock(b) {
					row.mermaidHidden = true
				}
				e.rows = append(e.rows, row)
				if b.Kind == "code" || b.Kind == "raw" || b.Kind == "math" || b.Kind == "quote" || b.Kind == "callout" {
					d := decoration{kind: b.Kind, id: b.ID, ownerID: leaf.Owner, rect: ui.Rect{X: x, Y: y, W: w, H: total - gap}}
					if isMermaidBlock(b) {
						d.kind, d.label, d.id, d.mermaidHidden = "mermaid", b.Language, b.ID, true
					} else if b.Kind == "code" {
						d.label, d.id = b.Language, b.ID
					}
					if b.Kind == "callout" {
						d.tone, d.id, d.label = b.Color, b.ID, b.Emoji
					}
					e.decorations = append(e.decorations, d)
				}
				return total
			}
		}
		if laid == nil {
			cached = e.shapeBlock(b, key, shapeWidth, o.Theme)
			e.cache[b.ID] = cached
			laid = cached.layout
		}
		_, h = laid.Size()
		h = max(h, font.Size*font.LineHeight)
		total := top + h + bottom
		row := row{leafRevision: e.Revision(), leaf: leaf, layout: laid, rect: ui.Rect{X: x, Y: y, W: w, H: total}, x: x + left, y: y + top, number: number, size: font.Size, tableHeader: header}
		if b.Kind == "image" {
			total = e.layoutImage(&row, o)
		}
		if b.Kind == "rule" {
			total = 25 + gap
			row.rect.H = total
		}
		if b.Kind == "math" {
			total = e.mathRow(&row, h)
			row.rect.H = total
		}
		var mermaidWellOff float32
		if isMermaidBlock(b) {
			total, mermaidWellOff = e.mermaidRow(&row, h, gap)
			row.rect.H = total
		}
		if virtualize && leaf.Owner == b.ID && canDeferKind(b.Kind) {
			hintH := h
			if b.Kind == "math" || isMermaidBlock(b) {
				hintH = total
			}
			e.recordExactHeight(b.ID, e.hintWidthFor(b, shapeWidth), hintH, font, b.Runs)
		}
		e.rows = append(e.rows, row)
		if b.Kind == "code" || b.Kind == "raw" || b.Kind == "math" || b.Kind == "quote" || b.Kind == "callout" || b.Kind == "rule" {
			d := decoration{kind: b.Kind, id: b.ID, ownerID: leaf.Owner, rect: ui.Rect{X: x, Y: y, W: w, H: total - gap}}
			if isMermaidBlock(b) {
				d.kind, d.label, d.id = "mermaid", b.Language, b.ID
				d.mermaidHidden, d.mermaidWellOff = row.mermaidHidden, mermaidWellOff
			} else if b.Kind == "code" {
				d.label, d.id = b.Language, b.ID
			}
			if b.Kind == "callout" {
				d.tone, d.id, d.label = b.Color, b.ID, b.Emoji
			}
			e.decorations = append(e.decorations, d)
		}
		return total
	}
	return blockLayout
}

// indexedRow resolves absolute text offsets only when a row is used. Geometry
// and shaping stay cached, so text edits need not rewrite every later row.
func (e *Editor) indexedRow(i int) *row {
	r := &e.rows[i]
	if e.hasLazyY {
		if off := e.rowYOff.query64(i); off != r.yApplied {
			dy := float32(off - r.yApplied)
			r.rect.Y += dy
			r.y += dy
			if f := r.picture; f != nil {
				f.bounds.Y += dy
				for k := range f.actions {
					f.actions[k].bounds.Y += dy
				}
			}
			r.yApplied = off
		}
	}
	if r.leafRevision != e.Revision() {
		if leaf, ok := e.Engine.LeafByID(r.leaf.Block.ID); ok {
			r.leaf = leaf
		}
		r.leafRevision = e.Revision()
	}
	return r
}
func (e *Editor) rowIndexAt(at int) int {
	if len(e.rows) == 0 {
		return -1
	}
	leaf := e.Engine.LeafAt(at)
	if i, ok := e.rowIDs[leaf.Block.ID]; ok {
		return i
	}
	// A folded leaf has no row: retain the previous next-visible-row behavior.
	i := sort.Search(len(e.rows), func(i int) bool { return at <= e.indexedRow(i).leaf.End })
	return min(i, len(e.rows)-1)
}

func (e *Editor) rowAt(at int) *row {
	i := e.rowIndexAt(at)
	if i < 0 {
		return nil
	}
	return e.indexedRow(i)
}

func (e *Editor) BoundsForRange(r ui.TextInputRange) (ui.Rect, ui.TextInputRange, bool) {
	idx := e.rowIndexAt(r.Start)
	if idx < 0 {
		return ui.Rect{}, r, false
	}
	row := e.ensureRow(idx)
	local := max(0, min(r.Start-row.leaf.Start, row.leaf.End-row.leaf.Start))
	if row.picture != nil && !e.captionVisible(row.leaf.Block) {
		rect := row.picture.bounds
		rect.H = min(26, rect.H)
		return rect, ui.TextInputRange{Start: row.leaf.Start, End: row.leaf.Start}, true
	}
	if row.mermaidHidden && row.mermaid != nil {
		mw := row.mermaid.W * row.mermaidScale
		rect := ui.Rect{X: row.rect.X + (row.rect.W-mw)/2, Y: row.rect.Y + row.mermaidTop, W: 2, H: 20}
		return rect, ui.TextInputRange{Start: row.leaf.Start, End: row.leaf.Start}, true
	}
	rect := row.layout.Caret(local)
	rect.X += row.x
	rect.Y += row.y
	rect.H = max(20, rect.H)
	if _, _, availW, _, scrollX, noWrap := e.codeScrollFor(*row); noWrap {
		rect.X = min(max(row.x, rect.X-scrollX), row.x+availW)
	}
	return rect, ui.TextInputRange{Start: row.leaf.Start + local, End: row.leaf.Start + local}, true
}
func (e *Editor) IndexForPoint(point ui.Point) (int, bool) {
	if len(e.rows) == 0 {
		return 0, false
	}
	best := e.ensureRow(e.rowSpace.nearest(point))
	if (best.picture != nil && !e.captionVisible(best.leaf.Block)) || best.mermaidHidden {
		return best.leaf.Start, true
	}
	px := point.X - best.x
	if _, _, _, _, scrollX, noWrap := e.codeScrollFor(*best); noWrap {
		px += scrollX
	}
	return best.leaf.Start + best.layout.IndexAt(ui.Point{X: px, Y: point.Y - best.y}), true
}

func (e *Editor) decorationOffset(d decoration) (dx, dy float32) {
	if e.printing || e.reduceMotion {
		return 0, 0
	}
	dy = e.ownerVisualDY(d.ownerID)
	if d.id != "" {
		if ind, ok := e.motion.indents[d.id]; ok {
			dx += ind.dx
		}
	}
	if d.kind == "cell" && d.ownerID != "" {
		dy += e.tableRowVisualDY(d.ownerID, d.rowIdx)
		if e.tableDrag.active && e.tableDrag.id == d.ownerID && e.tableDrag.from == d.rowIdx {
			dy += e.tableDrag.y - e.tableDrag.startY
		}
		dx += e.tableColVisualDX(d.ownerID, d.colIdx)
		if e.tableColDrag.active && e.tableColDrag.id == d.ownerID && e.tableColDrag.from == d.colIdx {
			dx += e.tableColDrag.x - e.tableColDrag.startX
		}
	}
	return dx, dy
}

func (e *Editor) paint(p *ui.Painter, bounds ui.Rect, t *ui.Theme, caret bool) {
	if !e.printing {
		e.stepMotion(p.Now())
	}
	offset := func(r ui.Rect) ui.Rect { r.X += bounds.X; r.Y += bounds.Y; return r }
	e.paintHeader(p, bounds)
	plan := e.currentDragPlan()
	if !e.printing {
		rowSlack := float32(40)
		if e.motion.moving || plan.active || e.tableDrag.active || e.tableColDrag.active {
			rowSlack = 200
		}
		e.ensureVisibleShaped(rowSlack, t)
	}
	if plan.active && !e.printing {
		slot := ui.Rect{
			X: bounds.X + e.padding,
			Y: bounds.Y + plan.slotY,
			W: max(48, e.width-2*e.padding),
			H: max(24, plan.slotH-8),
		}
		p.Fill(slot, t.Accent.Alpha(0.055), 8)
		p.StrokeDashed(slot, t.Accent.Alpha(0.65), 8, 1.25)
	}
	decSlack := float32(1)
	if e.motion.moving || plan.active || e.tableDrag.active || e.tableColDrag.active {
		decSlack = 160
	}
	paintDecorationPass := func(draggedOnly bool) {
		for i := range e.visibleDecorations(decSlack) {
			d := *e.indexedDecoration(i)
			if d.rect.Y+d.rect.H < e.Scroll.Y-decSlack || d.rect.Y > e.Scroll.Y+e.viewport+decSlack {
				continue
			}
			if plan.active && e.isDraggedOwner(d.ownerID, plan) != draggedOnly {
				continue
			}
			ddx, ddy := e.decorationOffset(d)
			r := d.rect
			r.X += bounds.X + ddx
			r.Y += bounds.Y + ddy
			switch d.kind {
			case "column":
				if !e.printing {
					e.paintColumn(p, r, d)
				}
			case "code", "raw":
				pg := e.page
				border := pg.CodeBorder
				if d.kind == "code" && (e.hovered(d.rect, 0) || e.langOpen && e.langID == d.id) {
					border = pg.CodeHover
				}
				p.Fill(r, pg.CodeFill, 8)
				p.Stroke(r, border, 8, 1)
				if d.kind == "code" {
					lb := languageButton(d)
					lb.X += bounds.X + ddx
					lb.Y += bounds.Y + ddy
					e.paintLanguage(p, lb, d, t)
					e.paintLineNumbers(p, ui.Rect{X: bounds.X + ddx, Y: bounds.Y + ddy, W: bounds.W, H: bounds.H}, d)
				} else if d.label != "" {
					p.Text(r.X+20, r.Y+10, d.label, 11, pg.Caption)
				}
			case "mermaid":
				e.paintMermaidDecoration(p, bounds, d, ddx, ddy, t)
			case "callout":
				look := e.page.Callouts[d.tone]
				if !e.printing && !e.reduceMotion && e.motion.callouts != nil {
					if cm, ok := e.motion.callouts[d.id]; ok {
						look = Callout{Fill: cm.fill, Border: cm.border, Icon: cm.icon}
					}
				}
				p.Fill(r, look.Fill, 8)
				p.Stroke(r, look.Border, 8, 1)
				icon := calloutIconRect(d)
				icon.X += bounds.X + ddx
				icon.Y += bounds.Y + ddy
				if d.id == e.colorID && e.colorOpen || e.hovered(calloutIconRect(d), 0) {
					p.Fill(icon, e.page.Hover, 4)
				}
				if d.label != "" {
					span := ui.Span{Text: d.label, Size: 18}
					w, h := p.MeasureText(0, span)
					p.RichText(icon.X+(icon.W-w)/2, icon.Y+(icon.H-h)/2, 0, span)
				} else {
					p.Icon(calloutIcon(d.tone), ui.Rect{X: icon.X + 3, Y: icon.Y + 3, W: 18, H: 18}, look.Icon)
				}
			case "quote":
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 2, H: r.H}, e.page.QuoteBar, 1)
			case "rule":
				p.Line(r.X, r.Y+12.5, r.X+r.W, r.Y+12.5, 1, e.page.Rule)
			case "cell":
				tb := d.table
				tb.X += bounds.X
				tb.Y += bounds.Y + e.ownerVisualDY(d.ownerID)
				if d.header {
					p.Clip(tb, 8, func() { p.Fill(r, e.page.TableHeader, 0) })
				}
				if fill, ok := e.page.CellFills[d.tone]; ok {
					p.Clip(tb, 8, func() { p.Fill(r, fill, 0) })
				}
			case "table":
				p.Stroke(r, e.page.TableLine, 8, 1)
			}
		}
		for i := range e.visibleDecorations(decSlack) {
			d := *e.indexedDecoration(i)
			if d.kind != "cell" || d.rect.Y+d.rect.H < e.Scroll.Y-decSlack || d.rect.Y > e.Scroll.Y+e.viewport+decSlack {
				continue
			}
			if plan.active && e.isDraggedOwner(d.ownerID, plan) != draggedOnly {
				continue
			}
			ddx, ddy := e.decorationOffset(d)
			r := d.rect
			r.X += bounds.X + ddx
			r.Y += bounds.Y + ddy
			if d.right {
				p.Line(r.X+r.W, r.Y, r.X+r.W, r.Y+r.H, 1, e.page.TableLine)
			}
			if d.below {
				p.Line(r.X, r.Y+r.H, r.X+r.W, r.Y+r.H, 1, e.page.TableLine)
			}
		}
	}
	paintDecorationPass(false)
	e.paintBlockSelection(p, bounds, t)
	if !e.printing {
		e.paintHoverControls(p, bounds, t)
		e.paintGap(p, bounds, t)
		e.paintTables(p, bounds)
		e.paintColumnsResizers(p, bounds)
		e.paintMatches(p, bounds)
		e.paintFlash(p, bounds)
	}
	e.openReveal.order = nil
	selection := e.Selection().Range
	rowSlack := float32(40)
	if e.motion.moving || plan.active || e.tableDrag.active || e.tableColDrag.active {
		rowSlack = 200
	}
	paintRowPass := func(draggedOnly bool) {
		for i := range e.visibleRows(rowSlack) {
			r := *e.indexedRow(i)
			if r.rect.Y+r.rect.H < e.Scroll.Y-rowSlack || r.rect.Y > e.Scroll.Y+e.viewport+rowSlack {
				continue
			}
			if plan.active && e.isDraggedOwner(r.leaf.Owner, plan) != draggedOnly {
				continue
			}
			dx, dy := e.visualOffset(r)
			if r.picture != nil {
				e.paintImage(p, bounds, r, t)
				if !e.captionVisible(r.leaf.Block) {
					continue
				}
			}
			e.paintMarker(p, bounds, r, t)
			paintRowBody := func(scrollX float32) {
				if e.focused && len(e.blocks.ids) == 0 && selection.End > r.leaf.Start && selection.Start < r.leaf.End && e.inlineMath.blockID != r.leaf.Block.ID {
					if r.mathHidden && r.math != nil {
						w, h := r.math.W*r.mathScale, (r.math.Ascent+r.math.Descent)*r.mathScale
						p.Fill(ui.Rect{X: bounds.X + r.rect.X + dx + (r.rect.W-w)/2 - 3, Y: bounds.Y + r.rect.Y + dy + r.mathTop - 2, W: w + 6, H: h + 4}, t.Selection, 3)
					} else if r.mermaidHidden && r.mermaid != nil {
						mw, mh := r.mermaid.W*r.mermaidScale, r.mermaid.H*r.mermaidScale
						p.Fill(ui.Rect{X: bounds.X + r.rect.X + dx + (r.rect.W-mw)/2 - 3, Y: bounds.Y + r.rect.Y + dy + r.mermaidTop - 2, W: mw + 6, H: mh + 4}, t.Selection, 4)
					} else {
						local := ui.TextInputRange{Start: max(0, selection.Start-r.leaf.Start), End: min(r.leaf.End, selection.End) - r.leaf.Start}
						for _, rect := range r.layout.SelectionRects(local) {
							rect.X += bounds.X + r.x + dx - scrollX - 1.5
							rect.Y += bounds.Y + r.y + dy
							rect.W += 3
							p.Fill(rect, t.Selection, 3.5)
						}
					}
				}
				if e.paintMath(p, bounds, r, t) {
					return
				}
				if e.paintMermaid(p, bounds, r, t) {
					return
				}
				shiftedBounds := ui.Rect{X: bounds.X + dx - scrollX, Y: bounds.Y + dy, W: bounds.W, H: bounds.H}
				e.paintAnnotations(p, shiftedBounds, r, true)
				textColor := t.Text
				if r.leaf.Block.Kind == "image" && e.imageCaptionID != r.leaf.Block.ID {
					textColor = e.page.Caption
				}
				if !e.printing && !e.reduceMotion && e.motion.foldFade != nil {
					if op, ok := e.motion.foldFade[r.leaf.Owner]; ok && op < 1 {
						textColor = textColor.Alpha(float32(textColor.A) / 255 * op)
					}
				}
				p.TextLayout(r.layout.TextLayout, bounds.X+r.x+dx-scrollX, bounds.Y+r.y+dy, textColor)
				e.paintInline(p, bounds, r, t)
				e.paintAnnotations(p, shiftedBounds, r, false)
				if !e.printing {
					e.paintHint(p, bounds, r, t)
				}
				if e.mark != nil && e.mark.End >= r.leaf.Start && e.mark.Start <= r.leaf.End {
					for _, rect := range r.layout.SelectionRects(ui.TextInputRange{Start: max(0, e.mark.Start-r.leaf.Start), End: min(r.leaf.End, e.mark.End) - r.leaf.Start}) {
						p.Line(bounds.X+r.x+dx-scrollX+rect.X, bounds.Y+r.y+dy+rect.Y+rect.H-2, bounds.X+r.x+dx-scrollX+rect.X+rect.W, bounds.Y+r.y+dy+rect.Y+rect.H-2, 1, t.Accent)
					}
				}
			}
			if _, clipBox, _, _, scrollX, noWrap := e.codeScrollFor(r); noWrap {
				p.Clip(ui.Rect{X: bounds.X + clipBox.X + dx, Y: bounds.Y + clipBox.Y + dy, W: clipBox.W, H: clipBox.H}, 0, func() {
					paintRowBody(scrollX)
				})
			} else {
				paintRowBody(0)
			}
		}
	}
	paintRowPass(false)
	if plan.active && !e.printing {
		card := ui.Rect{
			X: bounds.X + e.padding - 8,
			Y: bounds.Y + e.indexedHandle(plan.first).top - 4 + plan.floatDY,
			W: max(64, e.width-2*e.padding+16),
			H: max(28, plan.slotH-4),
		}
		p.Shadow(card, 10, 0, 8, 24, 0, e.page.MathShadow)
		p.Fill(card, e.page.Popover.Alpha(0.94), 10)
		p.Stroke(card, t.Accent.Alpha(0.45), 10, 1.25)
		paintDecorationPass(true)
		paintRowPass(true)
	}
	e.paintDim(p, bounds, t)
	if e.printing {
		return
	}
	e.paintActivePair(p, bounds, t)
	e.paintReadOnlyNotice(p, bounds, t)
	for i := sort.Search(len(e.foldButtons), func(i int) bool { return e.indexedFold(i).rect.Y+16 >= e.Scroll.Y }); i < len(e.foldButtons) && e.indexedFold(i).rect.Y <= e.Scroll.Y+e.viewport; i++ {
		f := *e.indexedFold(i)
		if !f.folded && !f.always && f.id != e.hoverID {
			continue
		}
		color := e.page.Caption
		if e.hovered(f.rect, 2) {
			color = t.Text
		}
		angle := float32(0)
		if f.folded {
			angle = -90
		}
		if !e.reduceMotion && e.motion.foldAngles != nil {
			if a, ok := e.motion.foldAngles[f.id]; ok {
				angle = a
			}
		}
		fr := offset(f.rect)
		fr.Y += e.ownerVisualDY(f.id)
		switch angle {
		case 0:
			p.Icon(iconTriangleDown, fr, color)
		case -90:
			p.Icon(iconTriangleRight, fr, color)
		default:
			p.Transform(ui.Rotation(angle), fr.X+fr.W/2, fr.Y+fr.H/2, func() {
				p.Icon(iconTriangleDown, fr, color)
			})
		}
	}
	for i := max(0, sort.Search(len(e.handles), func(i int) bool { return e.indexedHandle(i).top > e.Scroll.Y })-1); i < len(e.handles) && e.indexedHandle(i).top <= e.Scroll.Y+e.viewport; i++ {
		h := *e.indexedHandle(i)
		if e.ReadOnly {
			break
		}
		if h.id != e.hoverID && h.id != e.dragID && !(e.menuOpen && e.menuID == h.id) && !(len(e.blocks.ids) > 0 && e.blocks.ids[0] == h.id) {
			continue
		}
		if h.rect.Y < e.Scroll.Y || h.rect.Y > e.Scroll.Y+e.viewport {
			continue
		}
		hdy := e.ownerVisualDY(h.id)
		hr := offset(h.rect)
		hr.Y += hdy
		if h.id == e.dragID || (e.menuOpen && e.menuID == h.id) {
			p.Fill(hr, t.Text.Alpha(.06), 6)
		}
		p.Icon(iconGrip, ui.Rect{X: hr.X + 4, Y: hr.Y + 4, W: 16, H: 16}, t.TextMuted.Alpha(.7))
		if e.dragID == "" {
			pr := offset(h.plus())
			pr.Y += hdy
			p.Icon(iconPlus, ui.Rect{X: pr.X + 4, Y: pr.Y + 4, W: 16, H: 16}, t.TextMuted.Alpha(.7))
		}
	}
	if e.dragID != "" && !plan.active && e.dropIndex >= 0 && e.dropIndex < len(e.handles) {
		y := bounds.Y + e.indexedHandle(e.dropIndex).top - 4
		p.Fill(ui.Rect{X: bounds.X + e.padding, Y: y - 1, W: e.width - 2*e.padding, H: 2}, t.Accent, 1)
	}
	if e.fileOver >= 0 {
		e.paintFileDropPreview(p, bounds, t)
	}
	if len(e.Doc.Blocks) == 1 && e.Doc.Blocks[0].Kind == "paragraph" && e.Len() == 0 && !e.focused {
		// The starter below says the rest.
		p.Text(bounds.X+e.padding, bounds.Y+pageTop+4, i18n.T("开始写作"), 16, t.TextMuted.Alpha(.8))
	}
	inlineCaretDoc, hasInlineCaret := e.paintInlineMathPopover(p, bounds, t)
	caretMoving := false
	if !e.ReadOnly && e.focused && len(e.blocks.ids) == 0 && selection.Start == selection.End && (e.Engine.LeafAt(e.Caret).Block.Kind != "image" || e.captionVisible(e.Engine.LeafAt(e.Caret).Block)) {
		var targetDoc ui.Rect
		hasTarget := false
		if hasInlineCaret {
			targetDoc, hasTarget = inlineCaretDoc, true
		} else if r, _, ok := e.BoundsForRange(selection); ok {
			if cr := e.rowAt(selection.Start); cr != nil {
				cdx, cdy := e.visualOffset(*cr)
				r.X += cdx
				r.Y += cdy
			}
			r.W = 1.75
			targetDoc, hasTarget = r, true
		}
		if hasTarget {
			cur, moving := e.stepCaretGlide(p.Now(), targetDoc)
			caretMoving = moving
			if caret || moving {
				cur = offset(cur)
				p.Fill(cur, t.Text, 0.85)
			}
		}
	}
	if e.motion.moving || caretMoving {
		p.AnimationFrame()
	}
}

// paintHoverControls draws the controls a block shows under the pointer:
// a code block's copy button. Tables draw their bars in paintTables.
func (e *Editor) paintHoverControls(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	offset := func(r ui.Rect) ui.Rect { r.X += bounds.X; r.Y += bounds.Y; return r }
	// The copied indicator expires even if its code block was scrolled away.
	if i, ok := e.decorationIDs[e.copiedID]; ok {
		if dc := e.indexedDecoration(i); (dc.kind == "code" || dc.kind == "mermaid") && p.Now().Sub(e.copiedAt) < 1500*time.Millisecond {
			p.After(1500*time.Millisecond - p.Now().Sub(e.copiedAt))
		}
	}
	for i := range e.visibleDecorations(1) {
		d := *e.indexedDecoration(i)
		switch {
		case d.kind == "mermaid" && d.id != "":
			e.paintMermaidHover(p, bounds, d, t)
		case d.kind == "code" && d.id != "":
			if idx, ok := e.rowIDs[d.id]; ok {
				barBox, _, availW, contentW, scrollX, noWrap := e.codeScrollFor(*e.indexedRow(idx))
				if noWrap && contentW > availW+1 && (e.hovered(d.rect, 0) || e.codeBarDrag == d.id || scrollX > 0) {
					barBox.X += bounds.X
					barBox.Y += bounds.Y + e.ownerVisualDY(d.ownerID)
					p.Scrollbar(ui.Scrollbar{Box: barBox, Horizontal: true, View: availW, Content: contentW, Offset: scrollX}, ui.ScrollbarState{Dragging: e.codeBarDrag == d.id})
				}
			}
			e.paintCodeWrapButton(p, bounds, d, t)
			copied := d.id == e.copiedID && p.Now().Sub(e.copiedAt) < 1500*time.Millisecond
			if !copied && !e.hovered(d.rect, 0) {
				continue
			}
			if copied {
				p.After(1500*time.Millisecond - p.Now().Sub(e.copiedAt))
			}
			// A flat header button: icon and
			// caption text, a fill only under the pointer.
			r := offset(copyButton(d))
			ddy := e.ownerVisualDY(d.ownerID)
			r.Y += ddy
			if e.hovered(copyButton(d), 0) {
				p.Fill(r, e.page.Hover, 4)
			}
			icon, label := iconCopy, i18n.T("复制")
			fg := e.page.Caption
			scale := float32(1)
			if copied {
				icon, label = iconCheck, i18n.T("已复制")
				fg = t.Accent
				if elapsed := p.Now().Sub(e.copiedAt); elapsed >= 0 && elapsed < 160*time.Millisecond && !e.reduceMotion {
					u := float64(elapsed) / float64(160*time.Millisecond)
					scale = 1 + 0.08*float32(math.Sin(math.Pi*u))
					p.AnimationFrame()
				}
			}
			drawBtn := func() {
				p.Icon(icon, ui.Rect{X: r.X + 4, Y: r.Y + 3, W: 16, H: 16}, fg)
				span := ui.Span{Text: label, Size: 14, Color: fg}
				_, h := p.MeasureText(0, span)
				p.RichText(r.X+24, r.Y+(r.H-h)/2, 0, span)
			}
			if scale != 1 {
				p.Transform(ui.Scaling(scale, scale), r.X+r.W/2, r.Y+r.H/2, drawBtn)
			} else {
				drawBtn()
			}
		}
	}
}

// hoverControl runs the hover control under a press, if any.
func (e *Editor) hoverControl(x, y float32) bool {
	in := func(r ui.Rect) bool { return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H }
	fs, fe := e.visibleFoldRange(y-24, y+24)
	for i := fs; i < fe; i++ {
		f := *e.indexedFold(i)
		if (f.folded || f.always || f.id == e.hoverID) && in(ui.Rect{X: f.rect.X - 2, Y: f.rect.Y - 2, W: f.rect.W + 4, H: f.rect.H + 4}) {
			e.toggleFold(f.id)
			return true
		}
	}
	for i := range e.decorationSpace.between(y-28, y+28) {
		d := *e.indexedDecoration(i)
		switch {
		case d.kind == "callout" && d.id != "" && in(calloutIconRect(d)):
			e.colorOpen = !(e.colorOpen && e.colorID == d.id)
			e.colorID, e.colorAt = d.id, calloutIconRect(d)
			return true
		case d.kind == "code" && d.id != "" && in(languageButton(d)):
			e.langOpen = !(e.langOpen && e.langID == d.id)
			e.langID, e.langAt = d.id, languageButton(d)
			return true
		case d.kind == "code" && d.id != "" && canShowWrapButton(d) && in(wrapButton(d)) && (e.hovered(d.rect, 0) || e.isCodeNoWrap(e.blockByID(d.id))):
			e.ToggleCodeWrap(d.id)
			return true
		case d.kind == "code" && d.id != "" && in(copyButton(d)) && e.hovered(d.rect, 0):
			e.copyBlock(d.id)
			return true
		case d.kind == "mermaid" && d.id != "" && !d.mermaidHidden && in(mermaidLanguageButton(d)):
			e.langOpen = !(e.langOpen && e.langID == d.id)
			e.langID, e.langAt = d.id, mermaidLanguageButton(d)
			return true
		case d.kind == "mermaid" && d.id != "" && d.mermaidHidden && in(mermaidEditButton(d)) && e.hovered(d.rect, 0):
			return e.focusBlockStart(d.id)
		case d.kind == "mermaid" && d.id != "" && in(mermaidCopyButton(d)) && e.hovered(d.rect, 0):
			e.copyBlock(d.id)
			return true
		case d.kind == "table" && e.tableControl(d, x, y):
			return true
		}
	}
	return false
}

// copyBlock copies the text of a code block, as its copy button does.
func (e *Editor) copyBlock(id string) {
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == id {
			if e.services != (ui.Services{}) {
				e.services.WriteClipboard(leaf.Block.Text())
			}
			e.copiedID, e.copiedAt = id, e.lastInput
			return
		}
	}
}

// paintHint shows what an empty block is for, as Notion does: the type
// of an empty heading, and on the empty line being written, the slash menu.
func (e *Editor) paintHint(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) {
	if e.ReadOnly {
		return
	}
	b := r.leaf.Block
	if r.leaf.Start != r.leaf.End || e.mark != nil || r.emptyColumn {
		return
	}
	hint := ""
	size := float32(16)
	switch {
	case b.Kind == "heading":
		size = []float32{26, 22, 20, 18, 16, 16}[max(1, min(6, b.Level))-1]
		if len(e.Doc.Blocks) > 0 && e.Doc.Blocks[0].ID == b.ID {
			size, hint = 34, i18n.T("请输入标题")
		} else {
			hint = blockLabel(b)
		}
	case b.Kind == "toggle":
		hint = i18n.T("折叠块")
	case e.focused && e.Caret == r.leaf.Start && (b.Kind == "paragraph" || b.Kind == "bullet" || b.Kind == "numbered" || b.Kind == "task"):
		hint = i18n.T("输入「/」插入内容")
	}
	if hint == "" {
		return
	}
	line := r.layout.Caret(0)
	span := ui.Span{Text: hint, Size: size, Color: e.page.Placeholder}
	if b.Kind == "heading" {
		span.Weight = 500
	}
	_, h := p.MeasureText(0, span)
	dx, dy := e.visualOffset(r)
	p.RichText(bounds.X+r.x+dx, bounds.Y+r.y+dy+line.Y+(max(line.H, h)-h)/2, 0, span)
}

// paintMarker draws the marker of a list item before its first line: a
// dot, its number, or a task's check box.
func (e *Editor) paintMarker(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) {
	kind := r.leaf.Block.Kind
	if kind != "bullet" && kind != "numbered" && kind != "task" {
		return
	}
	dx, dy := e.visualOffset(r)
	line := r.layout.Caret(0)
	cy := bounds.Y + r.y + dy + line.Y + max(line.H, 26)/2
	x := bounds.X + r.x + dx
	pg := e.page
	switch kind {
	case "bullet":
		// Nested lists cycle disc, circle and square.
		dot := ui.Rect{X: x - 20, Y: cy - 2.75, W: 5.5, H: 5.5}
		switch r.leaf.Block.Indent % 3 {
		case 0:
			p.Fill(dot, pg.Marker, 2.75)
		case 1:
			p.Stroke(dot, pg.Marker, 2.75, 1)
		default:
			p.Fill(ui.Rect{X: x - 19.75, Y: cy - 2.5, W: 5, H: 5}, pg.Marker, 0)
		}
	case "numbered":
		label := listNumber(max(1, r.number), r.leaf.Block.Indent) + "."
		span := ui.Span{Text: label, Size: 16, Color: pg.Marker, Features: "tnum"}
		w, h := p.MeasureText(0, span)
		p.RichText(x-6-w, cy-h/2, 0, span)
	case "task":
		box := ui.Rect{X: x - 24, Y: cy - 8, W: 16, H: 16}
		if r.leaf.Block.Checked {
			scale := float32(1)
			revealW := float32(12)
			if !e.printing && !e.reduceMotion && e.taskAnim != nil {
				if at, ok := e.taskAnim[r.leaf.Block.ID]; ok {
					if at.IsZero() {
						at = p.Now()
						e.taskAnim[r.leaf.Block.ID] = at
					}
					elapsed := p.Now().Sub(at)
					if elapsed >= 0 && elapsed < 200*time.Millisecond {
						u := float64(elapsed) / float64(200*time.Millisecond)
						scale = 1 + 0.14*float32(math.Sin(math.Pi*u)*math.Exp(-1.6*u))
						checkP := min(1, float32(u*1.45))
						revealW = 12 * checkP
						p.AnimationFrame()
					} else {
						delete(e.taskAnim, r.leaf.Block.ID)
					}
				}
			}
			drawChecked := func() {
				p.Fill(box, pg.Marker.Alpha(0.10), 4)
				p.Stroke(box, pg.Marker, 4, 1.2)
				iconRect := ui.Rect{X: box.X + 2, Y: box.Y + 2, W: 12, H: 12}
				if revealW < 11.9 {
					p.Clip(ui.Rect{X: iconRect.X, Y: iconRect.Y, W: max(0.5, revealW), H: iconRect.H}, 0, func() {
						p.Icon(iconCheck, iconRect, pg.Marker)
					})
				} else {
					p.Icon(iconCheck, iconRect, pg.Marker)
				}
			}
			if scale != 1 {
				p.Transform(ui.Scaling(scale, scale), box.X+box.W/2, box.Y+box.H/2, drawChecked)
			} else {
				drawChecked()
			}
		} else {
			p.Fill(box, t.Background, 4)
			p.Stroke(box, pg.TaskBorder, 4, 1)
		}
	}
}

// paintLineNumbers numbers a code block's lines in its left 36; a
// wrapped line keeps one number.
func (e *Editor) paintLineNumbers(p *ui.Painter, bounds ui.Rect, d decoration) {
	idx, ok := e.rowIDs[d.id]
	if !ok {
		return
	}
	r := e.ensureRow(idx)
	text := r.leaf.Block.Text()
	clip := !e.printing && e.viewport > 0
	visTop := e.Scroll.Y - 40
	visBot := e.Scroll.Y + e.viewport + 40
	pos := 0
	at := 0
	for i := 0; ; i++ {
		nl := strings.IndexByte(text[pos:], '\n')
		var line string
		if nl < 0 {
			line = text[pos:]
		} else {
			line = text[pos : pos+nl]
		}
		caret := r.layout.Caret(at)
		lineY := r.y + caret.Y
		if clip && lineY > visBot {
			break
		}
		if !clip || lineY+max(caret.H, 22) >= visTop {
			span := ui.Span{Text: strconv.Itoa(i + 1), Font: "monospace", Size: 14, Color: e.page.Caption, Features: "tnum"}
			w, h := p.MeasureText(0, span)
			p.RichText(bounds.X+d.rect.X+36-w, bounds.Y+lineY+(22-h)/2, 0, span)
		}
		if nl < 0 {
			break
		}
		pos += nl + 1
		at += document.Len(line) + 1
	}
}

// insertAfter opens a new paragraph below block id with the slash menu
// open in it, as Notion's add button does; an empty paragraph is reused.
func (e *Editor) insertAfter(id string) {
	end := -1
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == id || leaf.Owner == id {
			end = leaf.End
		}
	}
	if end < 0 {
		return
	}
	e.UnmarkText()
	e.SetSelection(end, end)
	e.Engine.Insert(document.NewBlock("paragraph", "/"))
	_, caret := e.Engine.Positions()
	e.SetSelection(caret+1, caret+1)
	e.slashDismissed = false
	e.RequestFocus, e.reveal = true, true
	e.sync()
}

func (e *Editor) linkAt(at int) string {
	leaf := e.Engine.LeafAt(at)
	offset := leaf.Start
	for _, run := range leaf.Block.Runs {
		offset += document.Len(run.Text)
		if at <= offset {
			return run.Style.Link
		}
	}
	return ""
}
func (e *Editor) toggleTask(at int, x float32) bool {
	r := e.rowAt(at)
	if r == nil || r.leaf.Block.Kind != "task" || x > r.x || x < r.x-28 {
		return false
	}
	wasChecked := r.leaf.Block.Checked
	id := r.leaf.Block.ID
	e.Engine.ToggleTask(id)
	if !wasChecked && !e.reduceMotion {
		if e.taskAnim == nil {
			e.taskAnim = make(map[string]time.Time)
		}
		e.taskAnim[id] = time.Time{}
	} else if e.taskAnim != nil {
		delete(e.taskAnim, id)
	}
	return true
}

func (e *Editor) structuralPointer(ev ui.InputEvent) bool {
	if e.ReadOnly {
		if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
			in := func(r ui.Rect) bool { return ev.X >= r.X && ev.X <= r.X+r.W && ev.Y >= r.Y && ev.Y <= r.Y+r.H }
			for i := range e.decorationSpace.between(ev.Y-28, ev.Y+28) {
				d := *e.indexedDecoration(i)
				if d.id == "" {
					continue
				}
				if d.kind == "mermaid" {
					if in(mermaidCopyButton(d)) && e.hovered(d.rect, 0) {
						e.copyBlock(d.id)
						return true
					}
					continue
				}
				if d.kind != "code" {
					continue
				}
				if canShowWrapButton(d) && in(wrapButton(d)) && (e.hovered(d.rect, 0) || e.isCodeNoWrap(e.blockByID(d.id))) {
					e.ToggleCodeWrap(d.id)
					return true
				}
				if in(copyButton(d)) && e.hovered(d.rect, 0) {
					e.copyBlock(d.id)
					return true
				}
			}
		}
		return false
	}
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
		e.closeHeader()
		if e.hoverControl(ev.X, ev.Y) {
			return true
		}
		if g, ok := e.gapAt(ev.X, ev.Y); ok && ev.Mods == 0 {
			e.fillGap(g)
			return true
		}
		e.colorOpen, e.langOpen = false, false
		e.tableSel = tableSelection{}
		for ri := range e.resizers {
			resize := *e.indexedResizer(ri)
			if math.Abs(float64(ev.X-resize.x)) < 5 && ev.Y >= resize.y && ev.Y <= resize.y+resize.h {
				if ev.Clicks >= 2 && resize.col >= 0 && resize.col+1 < len(resize.widths) {
					widths := append([]float32(nil), resize.widths...)
					half := (widths[resize.col] + widths[resize.col+1]) * 0.5
					widths[resize.col], widths[resize.col+1] = half, half
					e.Engine.SetWidths(resize.id, widths)
					e.resizing = nil
					e.resizeMoved = false
					return true
				}
				value := resize
				e.resizing = &value
				e.resizeX = ev.X
				e.resizeMoved = false
				return true
			}
		}
		hs, he := e.visibleHandleRange(ev.Y-64, ev.Y+64)
		for hi := hs; hi < he; hi++ {
			h := *e.indexedHandle(hi)
			if p := h.plus(); ev.X >= p.X && ev.X <= p.X+p.W && ev.Y >= p.Y && ev.Y <= p.Y+p.H {
				e.insertAfter(h.id)
				return true
			}
			if ev.X >= h.rect.X && ev.X <= h.rect.X+h.rect.W && ev.Y >= h.rect.Y && ev.Y <= h.rect.Y+h.rect.H {
				if !e.blockSelected(h.id) {
					e.clearBlocks()
				}
				e.dragID, e.dropIndex = h.id, h.index
				e.dragStartY, e.dragY, e.dragActive = ev.Y, ev.Y, false
				return true
			}
		}
	}
	if e.tableDragPointer(ev) {
		return true
	}
	if e.resizing != nil {
		if ev.Kind == ui.InputPointerMove {
			e.resizeX = ev.X
			if abs32(ev.X-e.resizing.x) > 8 {
				e.resizeMoved = true
			}
			return true
		}
		if ev.Kind == ui.InputPointerUp {
			r := e.resizing
			widths := append([]float32(nil), r.widths...)
			delta, _ := clampedResizeDelta(r, e.resizeX, e.resizeMoved)
			widths[r.col] += delta
			widths[r.col+1] -= delta
			e.Engine.SetWidths(r.id, widths)
			e.resizing = nil
			e.resizeMoved = false
			return true
		}
	}
	if e.dragID != "" {
		if ev.Kind == ui.InputPointerMove {
			e.dragY = ev.Y
			if !e.dragActive && abs32(ev.Y-e.dragStartY) > 4 {
				e.dragActive = true
			}
			if idx := sort.Search(len(e.handles), func(i int) bool { return e.indexedHandle(i).top-6 > ev.Y }) - 1; idx >= 0 {
				e.dropIndex = e.handles[idx].index
			} else if len(e.handles) > 0 {
				e.dropIndex = e.handles[0].index
			}
			return true
		}
		if ev.Kind == ui.InputPointerUp {
			old := 0
			for _, h := range e.handles {
				if h.id == e.dragID {
					old = h.index
				}
			}
			switch {
			case !e.dragActive && old == e.dropIndex:
				e.menuOpen, e.menuID, e.menuSub = true, e.dragID, ""
				e.menuScroll = ui.ScrollState{}
			case old == e.dropIndex:
				e.commitBlockDragMotion()
			case len(e.blocks.ids) > 1 && e.blockSelected(e.dragID):
				// The selected blocks go together, before the block the line
				// shows.
				e.commitBlockDragMotion()
				e.Engine.MoveBlocks(e.BlockSelection(), e.dropIndex)
			default:
				e.commitBlockDragMotion()
				e.Engine.MoveBlock(e.dragID, e.dropIndex)
			}
			e.dragID, e.dragActive = "", false
			e.RequestFocus = true
			return true
		}
	}
	return false
}

// fileDropAt is where files dropped at y on the page go: before the
// top-level block whose upper half y is in, or after the last, and the y
// of the line showing it.
func (e *Editor) fileDropAt(y float32) (int, float32) {
	end := e.contentEnd
	for j := range e.handles {
		h := *e.indexedHandle(j)
		bottom := end
		if j+1 < len(e.handles) {
			bottom = e.indexedHandle(j + 1).top
		}
		if y < (h.top+bottom)/2 {
			return h.index, h.top
		}
	}
	return len(e.Doc.Blocks), end + 4
}

func blockLabel(b *document.Block) string {
	switch b.Kind {
	case "heading":
		return fmt.Sprintf(i18n.T("标题 %d"), b.Level)
	case "code":
		return i18n.T("代码块")
	case "cell":
		return i18n.T("表格")
	case "quote":
		return i18n.T("引用")
	case "task":
		return i18n.T("任务")
	case "bullet":
		return i18n.T("无序列表")
	case "numbered":
		return i18n.T("有序列表")
	case "callout":
		return i18n.T("高亮块")
	case "toggle":
		return i18n.T("折叠块")
	}
	return i18n.T("正文")
}

func (e *Editor) Focused() bool { return e.focused }

// listNumber writes n as nested ordered lists count: 1, a, i by depth.
func listNumber(n, indent int) string {
	switch indent % 3 {
	case 1:
		out := ""
		for n > 0 {
			n--
			out = string(rune('a'+n%26)) + out
			n /= 26
		}
		return out
	case 2:
		var out strings.Builder
		for _, v := range []struct {
			value int
			digit string
		}{{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"}, {50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"}} {
			for n >= v.value {
				out.WriteString(v.digit)
				n -= v.value
			}
		}
		return out.String()
	}
	return fmt.Sprint(n)
}

// toggleFold folds or unfolds block id; a caret folded away moves to the
// end of the block that hides it.
func (e *Editor) toggleFold(id string) {
	if b := e.blockByID(id); b != nil && b.Folded {
		e.unfoldPending = true
	}
	e.Engine.ToggleFold(id)
	e.sync()
	leaf := e.Engine.LeafAt(e.Caret)
	hidden := e.Doc.HiddenBlocks()
	if hidden[leaf.Owner] == id || hidden[leaf.Block.ID] == id {
		for _, l := range e.Doc.Leaves() {
			if l.Block.ID == id {
				e.SetSelection(l.End, l.End)
			}
		}
	}
}

// revealCaret unfolds a section the caret has moved into, as by arrow keys,
// undo or a search, so the caret is never inside folded text.
func (e *Editor) revealCaret() {
	key := [2]uint64{e.Revision(), uint64(e.Caret)}
	if key == e.revealKey {
		return
	}
	e.revealKey = key
	leaf := e.Engine.LeafAt(e.Caret)
	if e.Engine.Reveal(leaf.Owner) || e.Engine.Reveal(leaf.Block.ID) {
		e.unfoldPending = true
		e.sync()
		e.revealKey = [2]uint64{e.Revision(), uint64(e.Caret)}
	}
}

// ToggleFoldAtCaret folds or unfolds the heading or list item holding the
// caret.
func (e *Editor) ToggleFoldAtCaret() {
	leaf := e.Engine.LeafAt(e.Caret)
	e.toggleFold(leaf.Block.ID)
}

// UnfoldAll opens every folded heading and list item.
func (e *Editor) UnfoldAll() {
	var walk func([]*document.Block)
	walk = func(blocks []*document.Block) {
		for _, b := range blocks {
			if b.Folded {
				e.unfoldPending = true
				e.Engine.ToggleFold(b.ID)
			}
			walk(b.Children)
		}
	}
	walk(e.Doc.Blocks)
	e.sync()
}

// columnWidth is the text column's width: the reader's, kept between 560
// and 1200, or 820.
func columnWidth(w float32) float32 {
	if w <= 0 {
		return 820
	}
	return max(560, min(1200, w))
}

// bodyFamily is the family list body text is set in.
func bodyFamily(family string) string {
	if family == "" {
		return "system-ui"
	}
	return family
}

// Padding is the page's margin either side of its text column, as last
// laid out.
func (e *Editor) Padding() float32 { return e.padding }
