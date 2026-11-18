package editor

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

var _ ui.TextInputClient = (*Editor)(nil)

func newTester(markdown string) (*Editor, *ui.Tester) {
	e := New(markdown)
	tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 900, 650)
	tt.Frame()
	return e, tt
}
func focus(e *Editor, tt *ui.Tester) { e.RequestFocus = true; tt.Frame() }

func TestRealRichTypingAndNativeClipboard(t *testing.T) {
	e, tt := newTester("Hello **world**")
	if e.Text() != "Hello world" {
		t.Fatal(e.Text())
	}
	focus(e, tt)
	e.SetSelection(6, 11)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyU)
	if !e.Doc.Blocks[0].Runs[1].Style.Underline {
		t.Fatal("underline is not structural")
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != "<u>**world**</u>" {
		t.Fatal("native copy", tt.Clipboard())
	}
	tt.Type("世界😀")
	if e.Text() != "Hello 世界😀" {
		t.Fatal(e.Text())
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Text() != "Hello world" {
		t.Fatal("undo text")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyX)
	if e.Text() != "" {
		t.Fatal("cut")
	}
	tt.Key(ui.Cmd, ui.KeyV)
	if e.Text() != "Hello world" {
		t.Fatal("paste")
	}
}
func TestIMEIsOneRichTransaction(t *testing.T) {
	e, tt := newTester("A😀 **cafe**")
	focus(e, tt)
	e.SetSelection(4, 8)
	tt.Frame()
	before := e.Doc.Clone()
	tt.Compose("中文", 2)
	if e.Text() != "A😀 cafe" || e.Version() != 0 {
		t.Fatal("preedit committed")
	}
	if e.Doc.PlainText() != "A😀 中文" {
		t.Fatal("preedit missing")
	}
	tt.Type("中文")
	if e.Text() != "A😀 中文" || !e.Doc.Blocks[0].Runs[1].Style.Bold {
		t.Fatal("composition marks lost")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if !reflect.DeepEqual(e.Doc, before) {
		t.Fatal("undo preedit")
	}
	tt.Compose("候选", 2)
	tt.Key(0, ui.KeyEscape)
	if !reflect.DeepEqual(e.Doc, before) {
		t.Fatal("cancel changed rich content")
	}
}
func TestGraphemeDeletion(t *testing.T) {
	for _, glyph := range []string{"😀", "e\u0301", "👨‍👩‍👧‍👦", "🇨🇳"} {
		e, tt := newTester("a" + glyph)
		focus(e, tt)
		e.SetSelection(e.Len(), e.Len())
		tt.Frame()
		tt.Key(0, ui.KeyBackspace)
		if e.Text() != "a" {
			t.Fatal("split grapheme", glyph, e.Text())
		}
		tt.Key(ui.Cmd, ui.KeyZ)
		if e.Text() != "a"+glyph {
			t.Fatal("undo")
		}
	}
}
func TestSlashInsertionAndSingleUndo(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)
	tt.Type("/table")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "table" {
		t.Fatal("slash did not insert table")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Text() != "/table" {
		t.Fatal("slash undo", e.Text())
	}
	tt.Key(0, ui.KeyEscape)
	if e.Text() != "/table" {
		t.Fatal("escape removed query")
	}
}
func TestTableCellInputAndNavigation(t *testing.T) {
	e, tt := newTester("| A | B |\n|---|---|\n| 1 | 2 |")
	focus(e, tt)
	leaves := e.Doc.Leaves()
	e.SetSelection(leaves[2].Start, leaves[2].End)
	tt.Frame()
	tt.Type("中文")
	tt.Key(0, ui.KeyTab)
	tt.Type("x")
	table := e.Doc.Blocks[0]
	if table.Children[1].Children[0].Text() != "中文" || table.Children[1].Children[1].Text() != "x2" {
		t.Fatal("table edit")
	}
	tt.Key(0, ui.KeyTab)
	if len(e.Doc.Blocks[0].Children) != 3 {
		t.Fatal("Tab at end did not add row")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.Doc.Blocks[0].Children) != 2 {
		t.Fatal("row undo")
	}
}
func TestLayoutCachesUnchangedBlocks(t *testing.T) {
	e, tt := newTester("# Title\n\nFirst\n\nSecond\n\nThird")
	old := e.rows[0].layout
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("!")
	if e.rows[0].layout != old {
		t.Fatal("typing reshaped an unchanged heading")
	}
	for _, size := range [][2]int{{390, 650}, {1200, 800}} {
		tt.SetSize(size[0], size[1])
		tt.Frame()
		for _, r := range e.rows {
			if r.rect.W <= 0 {
				t.Fatal("invalid width")
			}
		}
	}
}
func TestMarkdownInterchangePreservesOpaqueContent(t *testing.T) {
	source := "---\nkey: value\n---\n\n# Title\n\nHello **bold and *italic*** [link](https://example.com).\n\n<div>custom</div>\n\n$$\nx^2\n$$\n"
	d := ImportMarkdown(source)
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.ImportSource != source {
		t.Fatal("lost import source")
	}
	out := ExportMarkdown(d)
	for _, value := range []string{"key: value", "Title", "italic", "https://example.com", "<div>custom</div>", "x^2"} {
		if !strings.Contains(out, value) {
			t.Fatalf("export missing %q", value)
		}
	}
	encoded, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := document.Decode(encoded)
	if err != nil || !reflect.DeepEqual(d, loaded) {
		t.Fatal("native round trip", err)
	}
}
func TestBoldFormatsWithoutMarkdownMarkers(t *testing.T) {
	e, tt := newTester("Hello")
	focus(e, tt)
	e.SetSelection(0, 5)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyB)
	if e.Text() != "Hello" || !e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatal("⌘B did not format text")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatal("undo bold")
	}
}
func TestCaretScrollsAndColumnsPersist(t *testing.T) {
	e, tt := newTester(strings.Repeat("Words to write.\n\n", 90))
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("End")
	tt.Frame()
	tt.Frame()
	if e.Scroll.Y == 0 {
		t.Fatal("caret did not scroll")
	}
	e.Engine.Insert(document.Columns())
	tt.Frame()
	e.ReplaceText(nil, "Left")
	tt.Frame()
	d := e.Doc.Blocks[len(e.Doc.Blocks)-1]
	if d.Kind != "columns" || d.Children[0].Children[0].Text() != "Left" {
		t.Fatal("column content")
	}
	tt.Advance(time.Second)
}

func TestRichClipboardAndStructurePaste(t *testing.T) {
	e, tt := newTester("**Bold** and *italic*")
	focus(e, tt)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyC)
	original := e.Doc.PlainText()
	tt.Key(0, ui.KeyRight)
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Key(ui.Cmd, ui.KeyV)
	last := e.Doc.Blocks[len(e.Doc.Blocks)-1]
	if last.Text() != original || !last.Runs[0].Style.Bold || !last.Runs[len(last.Runs)-1].Style.Italic {
		t.Fatal("rich paste lost marks")
	}
	e.Engine.Insert(document.Table(2, 2))
	e.sync()
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Key(ui.Cmd, ui.KeyX)
	tt.Key(ui.Cmd, ui.KeyV)
	if err := e.Doc.Validate(); err != nil {
		t.Fatal("invalid clipboard structure", err)
	}
	found := false
	for _, b := range e.Doc.Blocks {
		found = found || b.Kind == "table"
	}
	if !found {
		t.Fatal("clipboard lost table")
	}
}
func TestCodeDecorationKeepsEveryCharacter(t *testing.T) {
	source := "func main() {\n  value := \"hello 世界\" // comment\n  return 42\n}"
	spans := codeSpans(source, pageOf(Page{}, ui.LightTheme()))
	var out strings.Builder
	for _, span := range spans {
		out.WriteString(span.Text)
	}
	if out.String() != source {
		t.Fatal("highlight changed source")
	}
}
func TestBlockDragAndColumnResize(t *testing.T) {
	e, tt := newTester("First\n\nSecond\n\n| A | B |\n|---|---|\n| 1 | 2 |")
	bounds, _ := tt.Find("文档")
	first, second := e.handles[0], e.handles[1]
	tt.Press(bounds.X+first.rect.X+10, bounds.Y+first.rect.Y+10)
	tt.Move(bounds.X+second.rect.X+10, bounds.Y+second.rect.Y+10)
	tt.Release(bounds.X+second.rect.X+10, bounds.Y+second.rect.Y+10)
	if e.Doc.Blocks[1].Text() != "First" {
		t.Fatal("block drag failed")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Blocks[0].Text() != "First" {
		t.Fatal("drag undo failed")
	}
	tt.Frame()
	bounds, _ = tt.Find("文档")
	handle := e.resizers[0]
	tt.Press(bounds.X+handle.x, bounds.Y+handle.y+12)
	tt.Move(bounds.X+handle.x+35, bounds.Y+handle.y+12)
	tt.Release(bounds.X+handle.x+35, bounds.Y+handle.y+12)
	table := e.Doc.Blocks[2]
	if len(table.Widths) != 2 || table.Widths[0] <= table.Widths[1] {
		t.Fatal("column resize failed")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.Doc.Blocks[2].Widths) != 0 {
		t.Fatal("resize undo failed")
	}
}

func TestNativeReplacementOutsideMarkedText(t *testing.T) {
	e, _ := newTester("abc")
	e.SetSelection(3, 3)
	e.SetMarkedText(nil, "中文", ui.TextInputRange{Start: 2, End: 2})
	r := ui.TextInputRange{Start: 0, End: 1}
	e.ReplaceText(&r, "A")
	if e.Text() != "Abc中文" {
		t.Fatal("native replacement lost committed preedit", e.Text())
	}
	e.Undo()
	if e.Text() != "abc" {
		t.Fatal("native replacement split composition history")
	}
}

func TestNumberedListsCountByIndent(t *testing.T) {
	e, tt := newTester("1. a\n2. b\n   - c\n3. d\n\npara\n\n1. e")
	tt.Frame()
	var got []int
	for _, r := range e.rows {
		if r.leaf.Block.Kind == "numbered" {
			got = append(got, r.number)
		}
	}
	if fmt.Sprint(got) != "[1 2 3 1]" {
		t.Fatal("numbers", got)
	}
}

func TestAddButtonOpensSlashMenuBelowBlock(t *testing.T) {
	e, tt := newTester("First\n\nSecond")
	bounds, _ := tt.Find("文档")
	p := e.handles[0].plus()
	tt.Move(bounds.X+p.X+10, bounds.Y+p.Y+10)
	tt.ClickAt(bounds.X+p.X+10, bounds.Y+p.Y+10)
	tt.Frame()
	if len(e.Doc.Blocks) != 3 || e.Doc.Blocks[1].Text() != "/" || !e.slashOpen {
		t.Fatal("add button", e.Text(), e.slashOpen)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.Doc.Blocks) != 2 {
		t.Fatal("add undo", e.Text())
	}
}

func outlineDoc() *Editor {
	var md strings.Builder
	md.WriteString("# Title\n\n")
	for _, h := range []string{"Alpha", "Beta", "Gamma"} {
		md.WriteString("## " + h + "\n\n" + strings.Repeat("text\n\n", 30))
	}
	return New(md.String())
}

func TestOutlineJumpsToHeading(t *testing.T) {
	e := outlineDoc()
	open := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Outline: open, SetOutline: func(o bool) { open = o }})
	}, 1400, 650)
	tt.Frame()
	if len(e.outline) != 4 || !tt.HasText("Gamma") || !e.OutlineOpen() {
		t.Fatal("outline", e.outline)
	}
	if err := tt.Click("跳到 Gamma"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(jumpDuration)
	tt.Frame()
	tt.Frame()
	if e.Scroll.Y < e.outline[3].y-100 {
		t.Fatal("outline did not scroll", e.Scroll.Y, e.outline[3].y)
	}
}

// A long outline builds only the rows near what it shows, keeping the
// others' room so that it scrolls as far as before.
func TestLongOutlineBuildsRowsInView(t *testing.T) {
	var md strings.Builder
	md.WriteString("# Title\n\n")
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&md, "## Heading %d\n\ntext\n\n", i)
	}
	e := New(md.String())
	open := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Outline: open, SetOutline: func(o bool) { open = o }})
	}, 1400, 650)
	tt.Frame()
	if !tt.HasText("Heading 1") || tt.HasText("Heading 300") {
		t.Fatal("rows out of view were built")
	}
	if e.outlineState.list.MaxY < 300*outlineRow-650 {
		t.Fatal("outline lost the room of the rows out of view", e.outlineState.list.MaxY)
	}
	e.outlineState.list.Y = math.MaxFloat32
	tt.Frame()
	tt.Frame()
	if !tt.HasText("Heading 300") || tt.HasText("Heading 1") {
		t.Fatal("scrolled outline does not show its end", e.outlineState.list.Y)
	}
	if err := tt.Click("跳到 Heading 300"); err != nil {
		t.Fatal(err)
	}
}

func TestOutlineCollapses(t *testing.T) {
	e := outlineDoc()
	open := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Outline: open, SetOutline: func(o bool) { open = o }})
	}, 1200, 650)
	tt.Frame()
	pad := e.padding
	closeAt, ok := tt.Find("收起目录")
	if !ok {
		t.Fatal("no collapse button")
	}
	if err := tt.Click("收起目录"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(outlineSlide)
	tt.Frame()
	tt.Frame()
	if open || e.OutlineOpen() || tt.HasText("跳到 Gamma") {
		t.Fatal("outline stayed open")
	}
	if e.padding <= pad {
		t.Fatal("page did not take the outline's room back", pad, e.padding)
	}
	// Collapsing leaves the pointer on the button that opens it again.
	if openAt, ok := tt.Find("展开目录"); !ok || openAt != closeAt {
		t.Fatal("open and collapse buttons apart", openAt, closeAt)
	}
	if err := tt.Click("展开目录"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(outlineSlide)
	tt.Frame()
	tt.Frame()
	if !open || e.padding != pad {
		t.Fatal("outline did not open", open, e.padding, pad)
	}
}

func TestOutlineFloatsOverNarrowPage(t *testing.T) {
	e := outlineDoc()
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Outline: true, SetOutline: func(bool) { t.Fatal("narrow outline is not a lasting choice") }})
	}, 700, 650)
	tt.Frame()
	if e.OutlineOpen() || e.outlineState.mode != outlineFloating {
		t.Fatal("narrow page opened the outline beside it")
	}
	if err := tt.Click("展开目录"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if !e.OutlineOpen() || !tt.HasText("Beta") {
		t.Fatal("outline did not float open")
	}
	if err := tt.Click("收起目录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if e.OutlineOpen() {
		t.Fatal("outline stayed open")
	}
}

func TestNoOutlineWithoutHeadings(t *testing.T) {
	e := New("# Title\n\ntext\n")
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Outline: true, SetOutline: func(bool) {}})
	}, 1200, 650)
	tt.Frame()
	if e.outlineState.mode != outlineOff || tt.Click("展开目录") == nil {
		t.Fatal("outline without headings")
	}
}

func TestCountWords(t *testing.T) {
	if n := countWords("写作 is simple, 2026 年。"); n != 6 {
		t.Fatal("words", n)
	}
}

func TestSelectionBarFormatsSelection(t *testing.T) {
	e, tt := newTester("Intro\n\nhello world")
	focus(e, tt)
	leaf := e.Doc.Leaves()[1]
	e.SetSelection(leaf.Start, leaf.Start+5)
	tt.Frame()
	if err := tt.Click("加粗"); err != nil {
		t.Fatal(err)
	}
	if run := e.Doc.Blocks[1].Runs[0]; run.Text != "hello" || !run.Style.Bold {
		t.Fatal("selection bar bold", e.Doc.Blocks[1].Runs)
	}
	e.SetSelection(leaf.Start, leaf.Start)
	tt.Frame()
	if tt.HasText("加粗") {
		t.Fatal("selection bar stays without a selection")
	}
}

func TestCodeCopyAndTableBars(t *testing.T) {
	e, tt := newTester("```go\nfmt.Println(1)\n```\n\n| A | B |\n|---|---|\n| 1 | 2 |")
	bounds, _ := tt.Find("文档")
	find := func(kind string) decoration {
		for _, d := range e.decorations {
			if d.kind == kind {
				return d
			}
		}
		t.Fatal("no", kind)
		return decoration{}
	}
	code := find("code")
	b := copyButton(code)
	tt.Move(bounds.X+b.X+10, bounds.Y+b.Y+10)
	tt.ClickAt(bounds.X+b.X+10, bounds.Y+b.Y+10)
	if tt.Clipboard() != "fmt.Println(1)" || e.copiedID == "" {
		t.Fatal("copy", tt.Clipboard())
	}
	at := func(r ui.Rect) (float32, float32) { return bounds.X + r.X + r.W/2, bounds.Y + r.Y + r.H/2 }
	click := func(r ui.Rect) {
		x, y := at(r)
		tt.Move(x, y)
		tt.Frame()
		tt.ClickAt(x, y)
		tt.Frame()
	}
	// The pin at the table's right edge appends a column.
	table := find("table")
	click(pin(table, true, 2))
	if cols := len(e.Doc.Blocks[1].Children[0].Children); cols != 3 {
		t.Fatal("pin column", cols)
	}
	// The row bar selects the second row; its toolbar inserts above it.
	table = find("table")
	click(rowBar(table, 1))
	if !e.tableSel.ok || !e.tableSel.rows || e.tableSel.index != 1 {
		t.Fatal("row bar", e.tableSel)
	}
	if err := tt.Click("在上方插入行"); err != nil {
		t.Fatal(err)
	}
	if rows := len(e.Doc.Blocks[1].Children); rows != 3 || e.Doc.Blocks[1].Children[2].Children[0].Text() != "1" {
		t.Fatal("insert row above", rows)
	}
	// Selecting the first column, then Backspace, deletes it.
	focus(e, tt)
	table = find("table")
	click(columnBar(table, 0))
	tt.Key(0, ui.KeyBackspace)
	if cols := len(e.Doc.Blocks[1].Children[0].Children); cols != 2 || e.Doc.Blocks[1].Children[0].Children[0].Text() != "B" {
		t.Fatal("delete column", cols)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if cols := len(e.Doc.Blocks[1].Children[0].Children); cols != 3 {
		t.Fatal("undo column", cols)
	}
}

func TestCalloutAlertsRoundTrip(t *testing.T) {
	for md, color := range map[string]string{"> [!NOTE]\n> a": "", "> [!TIP]\n> a": "green", "> [!IMPORTANT]\n> a": "purple", "> [!WARNING]\n> a": "yellow", "> [!CAUTION]\n> a": "red"} {
		d := ImportMarkdown(md)
		if len(d.Blocks) != 1 || d.Blocks[0].Kind != "callout" || d.Blocks[0].Color != color || d.Blocks[0].Text() != "a" {
			t.Fatalf("import %q: %+v %q", md, d.Blocks[0], d.Blocks[0].Text())
		}
		if out := strings.TrimSpace(ExportMarkdown(d)); out != md {
			t.Fatalf("export %q: %q", md, out)
		}
	}
}

func TestCalloutIconPicksColor(t *testing.T) {
	e, tt := newTester("> [!NOTE]\n> remember")
	bounds, _ := tt.Find("文档")
	var callout decoration
	for _, d := range e.decorations {
		if d.kind == "callout" {
			callout = d
		}
	}
	icon := calloutIconRect(callout)
	tt.ClickAt(bounds.X+icon.X+10, bounds.Y+icon.Y+10)
	tt.Frame()
	if !e.colorOpen || !tt.HasText("高亮块颜色") {
		t.Fatal("picker did not open")
	}
	if err := tt.Click("高亮块颜色：" + CalloutName("yellow")); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[0].Color != "yellow" || e.colorOpen {
		t.Fatal("pick", e.Doc.Blocks[0].Color, e.colorOpen)
	}
	if out := ExportMarkdown(e.Doc); !strings.HasPrefix(out, "> [!WARNING]") {
		t.Fatal("export", out)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Blocks[0].Color != "" {
		t.Fatal("undo color")
	}
}

func TestBackspaceRemovesEmptiedTable(t *testing.T) {
	e, tt := newTester("Intro\n\n| A | B |\n| --- | --- |\n| C | D |\n\nAfter")
	focus(e, tt)
	// Select the table's text and delete it: the table keeps its shape.
	e.SetSelection(6, 13)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	if e.Doc.Blocks[1].Kind != "table" || !document.Empty(e.Doc.Blocks[1]) {
		t.Fatal("table after clearing", e.Text())
	}
	// Backspace in the emptied table now removes it.
	tt.Key(0, ui.KeyBackspace)
	if len(e.Doc.Blocks) != 2 || e.Text() != "Intro\nAfter" {
		t.Fatalf("table not removed: %q", e.Text())
	}
	tt.Type("!")
	if e.Text() != "Intro!\nAfter" {
		t.Fatalf("caret not above the table: %q", e.Text())
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Blocks[1].Kind != "table" {
		t.Fatal("undo did not restore the table")
	}
}

func TestCodeLanguagePicker(t *testing.T) {
	e, tt := newTester("```go\nfmt.Println(1)\n```")
	bounds, _ := tt.Find("文档")
	var code decoration
	for _, d := range e.decorations {
		if d.kind == "code" {
			code = d
		}
	}
	b := languageButton(code)
	tt.ClickAt(bounds.X+b.X+10, bounds.Y+b.Y+10)
	if !e.langOpen {
		t.Fatal("language picker did not open")
	}
	tt.Click("代码语言：Python")
	if e.Doc.Blocks[0].Language != "python" || e.langOpen {
		t.Fatal("language", e.Doc.Blocks[0].Language)
	}
	if md := ExportMarkdown(e.Doc); !strings.HasPrefix(md, "```python") {
		t.Fatal("export", md)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Blocks[0].Language != "go" {
		t.Fatal("undo language", e.Doc.Blocks[0].Language)
	}
}

func TestListNumbersByDepth(t *testing.T) {
	for _, c := range []struct {
		n, indent int
		want      string
	}{{3, 0, "3"}, {1, 1, "a"}, {27, 1, "aa"}, {4, 2, "iv"}, {9, 5, "ix"}, {2, 3, "2"}} {
		if got := listNumber(c.n, c.indent); got != c.want {
			t.Fatalf("listNumber(%d, %d) = %q, want %q", c.n, c.indent, got, c.want)
		}
	}
}

func TestSlashMenuGridAndSearch(t *testing.T) {
	e, tt := newTester("Intro")
	focus(e, tt)
	e.SetSelection(5, 5)
	tt.Key(0, ui.KeyEnter)
	tt.Type("/")
	if !e.slashOpen {
		t.Fatal("slash menu did not open")
	}
	entries := e.slashEntries()
	if !entries[0].grid || entries[0].item.name != "一级标题" {
		t.Fatal("grid first", entries[0].item.name)
	}
	if err := tt.Click("二级标题"); err != nil {
		t.Fatal(err)
	}
	if b := e.Doc.Blocks[1]; b.Kind != "heading" || b.Level != 2 || b.Text() != "" {
		t.Fatal("grid cell", b.Kind, b.Level, b.Text())
	}
	tt.Type("Plan")
	tt.Key(0, ui.KeyEnter)
	tt.Type("/zzz")
	if len(e.slashEntries()) != 0 || !tt.HasText("未能找到相关结果") {
		t.Fatal("empty result")
	}
	for range 3 {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type("bg")
	if got := e.slashEntries(); len(got) == 0 || got[0].item.name != "表格" || got[0].grid {
		t.Fatal("search ranking", got)
	}
	tt.Key(0, ui.KeyEnter)
	if e.Doc.Blocks[2].Kind != "table" {
		t.Fatal("enter did not insert the table", e.Doc.Blocks[2].Kind)
	}
}

func TestBlockMenuTurnsIndentsAndDeletes(t *testing.T) {
	e, tt := newTester("First\n\nSecond")
	focus(e, tt)
	first := e.Doc.Blocks[0].ID
	e.OpenBlockMenu(first, "")
	tt.Frame()
	if err := tt.Click("一级标题"); err != nil {
		t.Fatal(err)
	}
	if b := e.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 1 || e.menuOpen {
		t.Fatal("turn into", b.Kind, b.Level, e.menuOpen)
	}
	e.OpenBlockMenu(e.Doc.Blocks[1].ID, "")
	tt.Frame()
	r, ok := tt.Find("缩进和对齐")
	if !ok {
		t.Fatal("indent row missing")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if e.menuSub != "indent" {
		t.Fatal("side panel did not open", e.menuSub)
	}
	if err := tt.Click("增加缩进"); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[1].Indent != 1 || e.menuOpen {
		t.Fatal("indent", e.Doc.Blocks[1].Indent)
	}
	e.OpenBlockMenu(e.Doc.Blocks[1].ID, "")
	tt.Frame()
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	if len(e.Doc.Blocks) != 1 {
		t.Fatal("delete", e.Text())
	}
	e.OpenBlockMenu(e.Doc.Blocks[0].ID, "")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if e.menuOpen {
		t.Fatal("escape did not close the menu")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.Doc.Blocks) != 2 {
		t.Fatal("undo delete", e.Text())
	}
}

func TestTextColorsAndExport(t *testing.T) {
	e, tt := newTester("Hello world")
	focus(e, tt)
	// The selection bar has no color button: hand-drawn marks stand in for
	// it, and colors stay in the command bar.
	e.SetSelection(6, 11)
	tt.Frame()
	if tt.Click("文字颜色") == nil || tt.Click("应用颜色") == nil {
		t.Fatal("selection bar still has a color button")
	}
	red, blue := "red", "light-blue"
	e.Engine.SetTextColor(&red, &blue)
	r := e.Doc.Blocks[0].Runs[1]
	if r.Text != "world" || r.Style.Color != "red" || r.Style.Highlight != "light-blue" {
		t.Fatal("colors", e.Doc.Blocks[0].Runs)
	}
	// Colors preserve rich styles in Markdown without splitting outer marks.
	e.SetSelection(0, 11)
	tt.Key(ui.Cmd, ui.KeyB)
	md := ExportMarkdown(e.Doc)
	if !strings.HasPrefix(md, "**Hello <span ") || !strings.HasSuffix(md, ">world</span>**\n") {
		t.Fatalf("export %q", md)
	}
	round := ImportMarkdown(md)
	if len(round.Blocks[0].Runs) != 2 || !round.Blocks[0].Runs[0].Style.Bold ||
		!round.Blocks[0].Runs[1].Style.Bold || round.Blocks[0].Runs[1].Style.Color != "red" ||
		round.Blocks[0].Runs[1].Style.Highlight != "light-blue" {
		t.Fatalf("roundtrip %+v", round.Blocks[0].Runs)
	}
	none := ""
	e.Engine.SetTextColor(&none, &none)
	for _, r := range e.Doc.Blocks[0].Runs {
		if r.Style.Color != "" || r.Style.Highlight != "" {
			t.Fatal("reset", r.Style)
		}
	}
}

func TestFindAndReplace(t *testing.T) {
	e, tt := newTester("Apple pie\n\napple tart\n\n| apple | x |\n|---|---|\n| y | z |")
	focus(e, tt)
	tt.Key(ui.Cmd, ui.KeyF)
	if !e.find.open {
		t.Fatal("⌘F did not open find")
	}
	tt.Type("APPLE")
	tt.Frame()
	if len(e.find.matches) != 3 || !tt.HasText("1/3") {
		t.Fatal("matches", len(e.find.matches))
	}
	tt.Key(0, ui.KeyEnter)
	if e.find.current != 1 || !tt.HasText("2/3") {
		t.Fatal("next", e.find.current)
	}
	tt.Click("切换替换 (⇧⌘H)")
	tt.Frame()
	if err := tt.Click("替换为"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Pear")
	if err := tt.Click("全部替换"); err != nil {
		t.Fatal(err)
	}
	if e.Text() != "Pear pie\nPear tart\nPear\nx\ny\nz" {
		t.Fatalf("replace all %q", e.Text())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if e.find.open {
		t.Fatal("escape")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if !strings.HasPrefix(e.Text(), "Apple pie\napple tart\napple") {
		t.Fatalf("replace all is one undo step %q", e.Text())
	}
	if got := findIn("aAa😀aa", "aa"); fmt.Sprint(got) != "[[0 2] [5 7]]" {
		t.Fatal("findIn", got)
	}
}

func TestFoldHidesSectionAndRevealsCaret(t *testing.T) {
	e, tt := newTester("# Title\n\n## Part\n\nhidden text\n\n## Next\n\nshown")
	focus(e, tt)
	part := e.Doc.Blocks[1].ID
	var button foldButton
	for _, f := range e.foldButtons {
		if f.id == part {
			button = f
		}
	}
	bounds, _ := tt.Find("文档")
	tt.Move(bounds.X+button.rect.X+8, bounds.Y+button.rect.Y+8)
	tt.Frame()
	tt.ClickAt(bounds.X+button.rect.X+8, bounds.Y+button.rect.Y+8)
	tt.Frame()
	if !e.Doc.Blocks[1].Folded {
		t.Fatal("fold button")
	}
	for _, r := range e.rows {
		if r.leaf.Block.Text() == "hidden text" {
			t.Fatal("folded block still laid out")
		}
	}
	// The caret moving into the folded section opens it.
	leaf := e.Doc.Leaves()[2]
	e.SetSelection(leaf.Start+2, leaf.Start+2)
	tt.Frame()
	if e.Doc.Blocks[1].Folded {
		t.Fatal("caret did not reveal")
	}
	// Undo does not refold.
	e.ToggleFoldAtCaret()
	tt.Frame()
	if e.Doc.Blocks[1].Folded {
		t.Fatal("paragraphs do not fold")
	}
}

func TestMergeCellsFromSelection(t *testing.T) {
	e, tt := newTester("| a | b |\n|---|---|\n| c | d |")
	focus(e, tt)
	leaves := e.Doc.Leaves()
	e.SetSelection(leaves[0].Start, leaves[3].End)
	tt.Frame()
	if err := tt.Click("合并单元格"); err != nil {
		t.Fatal(err)
	}
	cell := e.Doc.Blocks[0].Children[0].Children[0]
	if cell.ColSpan != 2 || cell.RowSpan != 2 || len(e.Doc.Leaves()) != 1 {
		t.Fatal("merge", cell.ColSpan, cell.RowSpan)
	}
	tt.Frame()
	var rect ui.Rect
	for _, r := range e.rows {
		if r.leaf.Block == cell {
			rect = r.rect
		}
	}
	table := e.decorations[len(e.decorations)-1]
	if rect.W != table.rect.W || rect.H != table.rect.H {
		t.Fatal("merged cell does not fill the table", rect, table.rect)
	}
	e.SplitCaretCell()
	if len(e.Doc.Leaves()) != 4 {
		t.Fatal("split")
	}
	leaves = e.Doc.Leaves()
	e.SetSelection(leaves[0].Start, leaves[1].End)
	tt.Frame()
	tt.Click("单元格背景颜色")
	if err := tt.Click("单元格背景：浅蓝"); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[0].Children[0].Children[1].Background != "light-blue" {
		t.Fatal("cell background")
	}
}

func TestCalloutEmoji(t *testing.T) {
	e, tt := newTester("> [!TIP]\n> Keep going")
	e.OpenColorPicker(e.Doc.Blocks[0].ID)
	tt.Frame()
	if err := tt.Click("图标：🎉"); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[0].Emoji != "🎉" {
		t.Fatal("emoji", e.Doc.Blocks[0].Emoji)
	}
	e.OpenColorPicker(e.Doc.Blocks[0].ID)
	tt.Frame()
	tt.Click("使用颜色图标")
	if e.Doc.Blocks[0].Emoji != "" || e.Doc.Blocks[0].Color != "green" {
		t.Fatal("back to color icon")
	}
}

func TestClickingAGapAddsAParagraph(t *testing.T) {
	e, tt := newTester("# T\n\n```go\nx\n```\n\n```go\ny\n```")
	focus(e, tt)
	bounds, _ := tt.Find("文档")
	gaps := e.gaps()
	if len(gaps) != 2 || gaps[0].end || !gaps[1].end {
		t.Fatalf("gaps between and after closing code: %+v", gaps)
	}
	// Under the pointer the gap shows; elsewhere the page is as it was.
	end := gaps[1].rect
	tt.Move(bounds.X+end.X+40, bounds.Y+end.Y+end.H/2)
	tt.Frame()
	tt.Frame()
	if _, ok := e.hoveredGap(); !ok || tt.Cursor() != ui.CursorPointer {
		t.Fatal("the gap under the pointer does not show a hand", tt.Cursor())
	}
	tt.ClickAt(bounds.X+end.X+40, bounds.Y+end.Y+end.H/2)
	tt.Type("after")
	if len(e.Doc.Blocks) != 4 || e.Doc.Blocks[3].Kind != "paragraph" || e.Doc.Blocks[3].Text() != "after" {
		t.Fatalf("click after the last block: %q", e.Text())
	}
	if gaps = e.gaps(); len(gaps) != 1 {
		t.Fatalf("a paragraph at the end needs no gap: %+v", gaps)
	}
	between := gaps[0].rect
	tt.ClickAt(bounds.X+between.X+40, bounds.Y+between.Y+between.H/2)
	tt.Type("between")
	if len(e.Doc.Blocks) != 5 || e.Doc.Blocks[2].Text() != "between" || e.Doc.Blocks[1].Text() != "x" || e.Doc.Blocks[3].Text() != "y" {
		t.Fatalf("click between two code blocks: %q", e.Text())
	}
	if len(e.gaps()) != 0 {
		t.Fatal("gaps remain")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.Doc.Blocks) != 4 {
		t.Fatal("adding the paragraph is not its own undo step")
	}
	e.ReadOnly = true
	e.Doc.Blocks = e.Doc.Blocks[:3]
	if len(e.gaps()) != 0 {
		t.Fatal("a read-only page offers gaps")
	}
}

func TestBlockMenuFitsShortViewport(t *testing.T) {
	var paras []string
	for i := 1; i <= 24; i++ {
		paras = append(paras, fmt.Sprintf("Paragraph %d with enough text to scroll.", i))
	}
	e := New(strings.Join(paras, "\n\n"))
	marks := map[string]string{}
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{
			Width:          w,
			Theme:          c.Theme(),
			Bookmarks:      marks,
			ToggleBookmark: func(string) {},
			BookmarkKey:    "⌘D",
		})
	}, 900, 650)
	tt.Frame()
	focus(e, tt)

	assertWithin := func(label string, minH, w, h float32) ui.Rect {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing %q", label)
		}
		if r.W <= 0 || r.H < minH || r.X < 0 || r.Y < 0 || r.X+r.W > w || r.Y+r.H > h {
			t.Fatalf("%q rect %+v overflows or clips in viewport %gx%g (want H>=%g)", label, r, w, h, minH)
		}
		return r
	}

	// Single-frame scroll putting the last block near the bottom edge of a 640x620 viewport:
	// menu flips above immediately without 1-frame lag, and every item including bookmark and
	// add-below stays completely visible without clipping or inner scrolling.
	tt.SetSize(640, 620)
	tt.Frame()
	lastBlock := e.Doc.Blocks[len(e.Doc.Blocks)-1]
	lastID := lastBlock.ID
	lastEnd := e.Len()
	lastCaret, _, ok := e.BoundsForRange(ui.TextInputRange{Start: lastEnd, End: lastEnd})
	if !ok {
		t.Fatal("missing bounds for last block")
	}
	e.Scroll.Y = max(0, lastCaret.Y-(620-44))
	beforeCount := len(e.Doc.Blocks)
	e.OpenBlockMenu(lastID, "")
	tt.Frame()
	for _, label := range []string{"一级标题", "缩进和对齐", "剪切", "复制", "创建副本", "删除", "上移", "下移", "添加书签", "在下方添加"} {
		minH := float32(32)
		if label == "一级标题" {
			minH = 24
		}
		assertWithin(label, minH, 640, 620)
	}
	if err := tt.Click("创建副本"); err != nil {
		t.Fatal(err)
	}
	if len(e.Doc.Blocks) != beforeCount+1 || e.menuOpen {
		t.Fatalf("duplicate from flipped menu failed: len=%d menuOpen=%v", len(e.Doc.Blocks), e.menuOpen)
	}

	// Scrolled all the way to the bottom (pageBottom leaves ~434px above):
	// menu still flips above immediately on a single frame without overlapping the handle,
	// and inner scroll reaches the bottom item.
	e.Scroll.Y = 1e6
	lastID = e.Doc.Blocks[len(e.Doc.Blocks)-1].ID
	e.OpenBlockMenu(lastID, "")
	tt.Frame()
	assertWithin("一级标题", 24, 640, 620)
	e.menuScroll.Y = 1e6
	tt.Frame()
	assertWithin("在下方添加", 32, 640, 620)
	tt.Key(0, ui.KeyEscape)

	// Open with the indent side submenu at the bottom of a narrow and short viewport,
	// and verify every submenu item including 减少缩进 is reachable and clickable.
	tt.Move(0, 0)
	tt.SetSize(500, 620)
	tt.Frame()
	e.Scroll.Y = 1e6
	lastID = e.Doc.Blocks[len(e.Doc.Blocks)-1].ID
	e.Engine.FocusBlock(lastID)
	e.Engine.Indent(1)
	e.sync()
	e.OpenBlockMenu(lastID, "indent")
	tt.Frame()
	for _, label := range []string{"左对齐", "居中对齐", "右对齐", "增加缩进", "减少缩进"} {
		assertWithin(label, 32, 500, 620)
	}
	if err := tt.Click("减少缩进"); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[len(e.Doc.Blocks)-1].Indent != 0 || e.menuOpen {
		t.Fatalf("outdent from side menu failed: indent=%d menuOpen=%v", e.Doc.Blocks[len(e.Doc.Blocks)-1].Indent, e.menuOpen)
	}

	// Medium-height viewport (720px) with the handle near the vertical middle:
	// neither side fits the full 446px menu, so the menu stays strictly on the
	// larger side without overlapping the handle, and scrolls internally.
	tt.SetSize(640, 720)
	tt.Frame()
	midHandle := e.handles[len(e.handles)/2]
	e.Scroll.Y = midHandle.rect.Y - 350
	tt.Frame()
	bounds, _ := tt.Find("文档")
	handleWin := ui.Rect{
		X: bounds.X + midHandle.rect.X,
		Y: bounds.Y + midHandle.rect.Y,
		W: midHandle.rect.W,
		H: midHandle.rect.H,
	}
	e.OpenBlockMenu(midHandle.id, "")
	tt.Frame()
	topCell := assertWithin("一级标题", 24, 640, 720)
	if !(topCell.Y+topCell.H <= handleWin.Y || topCell.Y >= handleWin.Y+handleWin.H) {
		t.Fatalf("menu overlaps handle rect %+v: top cell at %+v", handleWin, topCell)
	}
	e.menuScroll.Y = 1e6
	tt.Frame()
	addRow := assertWithin("在下方添加", 32, 640, 720)
	if !(addRow.Y+addRow.H <= handleWin.Y || addRow.Y >= handleWin.Y+handleWin.H) {
		t.Fatalf("scrolled menu overlaps handle rect %+v: bottom row at %+v", handleWin, addRow)
	}
	if err := tt.Click("在下方添加"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !e.slashOpen {
		t.Fatal("expected slash menu after clicking 在下方添加 in medium viewport")
	}
	tt.Key(0, ui.KeyEscape)

	// Ultra-short viewport shorter than the menu itself: scrolling reaches bottom items.
	tt.SetSize(640, 300)
	tt.Frame()
	e.Scroll.Y = 1e6
	lastID = e.Doc.Blocks[len(e.Doc.Blocks)-1].ID
	e.OpenBlockMenu(lastID, "")
	tt.Frame()
	e.menuScroll.Y = 1e6
	tt.Frame()
	assertWithin("在下方添加", 32, 640, 300)
	if err := tt.Click("在下方添加"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(150 * time.Millisecond)
	if !e.slashOpen {
		t.Fatal("expected slash menu after clicking 在下方添加")
	}
	assertWithin("一级标题", 24, 640, 300)
	tt.Key(0, ui.KeyEscape)

	// Multi-block selection menu at the bottom of a short viewport.
	tt.SetSize(640, 620)
	tt.Frame()
	e.Scroll.Y = 1e6
	n := len(e.Doc.Blocks)
	e.SelectBlocks(e.Doc.Blocks[n-2].ID, e.Doc.Blocks[n-1].ID)
	e.OpenBlockMenu(e.Doc.Blocks[n-2].ID, "")
	tt.Frame()
	assertWithin("下移", 32, 640, 620)
	if err := tt.Click("下移"); err != nil {
		t.Fatal(err)
	}
	if e.menuOpen {
		t.Fatal("expected multi-block menu to close after clicking 下移")
	}
}

func TestSlashAndCompletionMenusFollowArrowKeys(t *testing.T) {
	e := New("")
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{
			Width:  w,
			Theme:  c.Theme(),
			Attach: func() {},
			WikiSuggest: func(string) []WikiItem {
				var out []WikiItem
				for i := 1; i <= 8; i++ {
					out = append(out, WikiItem{Name: fmt.Sprintf("Doc %d", i), Hint: "Space"})
				}
				return out
			},
			EmojiSuggest: func(string) []EmojiItem {
				var out []EmojiItem
				for i := 1; i <= 8; i++ {
					out = append(out, EmojiItem{Emoji: "😀", Name: fmt.Sprintf("sm%d", i)})
				}
				return out
			},
		})
	}, 900, 650)
	tt.Frame()
	focus(e, tt)

	assertRowVisible := func(label string, minH, winW, winH float32) {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing %q", label)
		}
		if r.W <= 0 || r.H < minH || r.X < 0 || r.Y < 0 || r.X+r.W > winW || r.Y+r.H > winH {
			t.Fatalf("%q rect %+v not fully visible in %gx%g (want H>=%g)", label, r, winW, winH, minH)
		}
	}

	// 1. Slash menu: arrow down to bottom items ("公式", "链接到文档") scrolls them into full view,
	// arrow up back to index 0 returns to the top ("基础", "一级标题"), and manual scroll stays put.
	tt.Type("/")
	tt.Advance(150 * time.Millisecond)
	if !e.slashOpen {
		t.Fatal("slash menu did not open")
	}
	if r, ok := tt.Find("链接到文档"); ok && r.H >= 32 {
		t.Fatalf("expected 链接到文档 to initially start clipped below slash scroll, got %+v", r)
	}
	entries := e.slashEntries()
	for i := 0; i < len(entries)-1; i++ {
		tt.Key(0, ui.KeyDown)
		if entries[e.slashIndex].item.name == "公式" {
			assertRowVisible("公式", 32, 900, 650)
		}
	}
	if e.slashScroll.Y <= 0 {
		t.Fatalf("expected slash menu to scroll down, got scroll.Y=%v", e.slashScroll.Y)
	}
	assertRowVisible("链接到文档", 32, 900, 650)

	for i := 0; i < len(entries)-1; i++ {
		tt.Key(0, ui.KeyUp)
	}
	if e.slashIndex != 0 {
		t.Fatalf("expected slashIndex 0 after arrowing back up, got %d", e.slashIndex)
	}
	if e.slashScroll.Y != 0 {
		t.Fatalf("expected slashScroll.Y=0 at top, got %v", e.slashScroll.Y)
	}
	assertRowVisible("基础", 14, 900, 650)
	assertRowVisible("一级标题", 24, 900, 650)

	e.slashScroll.Y = 40
	tt.Frame()
	if e.slashScroll.Y <= 0 {
		t.Fatalf("expected manual slash scroll to persist, got %v", e.slashScroll.Y)
	}

	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyBackspace)

	// 2. Wiki completion menu: 9 items (8 suggestions + 新建文档) inside an 8-row scroll area.
	tt.Type("[[new")
	tt.Advance(150 * time.Millisecond)
	if !e.wiki.open || len(e.wiki.items) != 9 {
		t.Fatalf("expected 9 wiki items, open=%v len=%d", e.wiki.open, len(e.wiki.items))
	}
	for range 8 {
		tt.Key(0, ui.KeyDown)
	}
	if e.wiki.scroll.Y <= 0 {
		t.Fatalf("expected wiki menu to scroll down, got %v", e.wiki.scroll.Y)
	}
	assertRowVisible("链接到 new", 36, 900, 650)

	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)

	// 3. Emoji/mention completion menu in a short viewport (640x220).
	tt.SetSize(640, 220)
	tt.Frame()
	tt.Type(":sm")
	tt.Advance(150 * time.Millisecond)
	if !e.complete.open || len(e.complete.items) != 8 {
		t.Fatalf("expected 8 emoji items, open=%v len=%d", e.complete.open, len(e.complete.items))
	}
	for range 7 {
		tt.Key(0, ui.KeyDown)
	}
	if e.complete.scroll.Y <= 0 {
		t.Fatalf("expected complete menu to scroll down, got %v", e.complete.scroll.Y)
	}
	assertRowVisible(":sm8:", 34, 640, 220)
}
