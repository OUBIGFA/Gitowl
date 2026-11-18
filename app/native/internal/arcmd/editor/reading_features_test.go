package editor

import (
	"image"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestInlineMathUTF16EditingAndRoundTrip(t *testing.T) {
	source := `😀 before $\frac{x^2}{2}$ middle $y_1$ after`
	e, tt := newTester(source)
	r := e.rows[0]
	if len(r.layout.objects) != 2 {
		t.Fatalf("objects: %d, %q", len(r.layout.objects), e.Text())
	}
	for _, o := range r.layout.objects {
		if r.layout.model(o.index) != o.start || r.layout.model(o.index+1) != o.end {
			t.Fatal("UTF16 mapping")
		}
		if len(r.layout.SelectionRects(ui.TextInputRange{Start: o.start + 1, End: o.end - 1})) == 0 {
			t.Fatal("hidden-source selection missing")
		}
	}
	last := r.layout.Caret(document.Len(source))
	if last.X <= r.layout.objects[1].rect.X {
		t.Fatal("trailing text offset lost")
	}
	if strings.TrimSpace(ExportMarkdown(e.Doc)) != source {
		t.Fatal("TeX escaped on export", ExportMarkdown(e.Doc))
	}
	b, _ := tt.Find("文档")
	o := r.layout.objects[0]
	tt.ClickAt(b.X+r.x+o.rect.X+o.rect.W/2, b.Y+r.y+o.rect.Y+o.rect.H/2)
	tt.Frame()
	if e.inlineEditing == "" || len(e.rows[0].layout.objects) != 2 || !e.rows[0].layout.objects[0].editing {
		t.Fatal("formula did not enter live inline editing")
	}
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	if len(e.rows[0].layout.objects) != 2 {
		t.Fatal("formula did not restore")
	}
	for _, s := range []string{`price $5 and $6`, "`$x$`", `\$escaped\$`, "$$x$$", "$ x$", "$x $"} {
		x, _ := newTester(s)
		if len(x.rows[0].layout.objects) > 0 {
			t.Errorf("literal rendered: %s", s)
		}
	}
}

func TestClipboardMarkdownHTMLAndLiteralPaste(t *testing.T) {
	for _, source := range []string{"# Title", "- one\n- two", "| a | b |\n|---|---|\n| 1 | 2 |", "```go\nx := 1\n```"} {
		e, tt := newTester("")
		focus(e, tt)
		tt.SetClipboard(source)
		tt.Key(ui.Cmd, ui.KeyV)
		if e.Doc.Blocks[0].Kind == "paragraph" {
			t.Fatal("flattened Markdown", source)
		}
		e.SetSelection(0, e.Len())
		tt.Key(ui.Cmd, ui.KeyC)
		data, err := tt.ClipboardData().Read(transfer.HTML)
		if err != nil || !strings.Contains(string(data), "<!--StartFragment-->") {
			t.Fatal("no HTML", err)
		}
		tt.Key(ui.Cmd, ui.KeyZ)
		if e.Text() != "" {
			t.Fatal("paste not one undo")
		}
	}
	d := ImportHTML(`<p>H<sub>2</sub>O and x<sup>2</sup> <a href="https://example.com/?x=1&amp;y=2">link</a></p>`)
	round := ImportMarkdown(ExportMarkdown(d))
	if !reflect.DeepEqual(d.Blocks[0].Runs, round.Blocks[0].Runs) {
		t.Fatal("scripts roundtrip", ExportMarkdown(d), round.Blocks[0].Runs)
	}
	html := ExportHTML(d)
	if !strings.Contains(html, "<sub>2</sub>") || !strings.Contains(html, "&amp;y=2") {
		t.Fatal(html)
	}
	e, tt := newTester("```\ncode\n```")
	focus(e, tt)
	e.SetSelection(4, 4)
	tt.SetClipboard("# literal")
	tt.Key(ui.Cmd, ui.KeyV)
	if e.Text() != "code# literal" || e.Doc.Blocks[0].Kind != "code" {
		t.Fatal("literal paste")
	}
}

func TestBracketPairingSelectionIMEAndUndo(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)
	tt.Type("（")
	if e.Text() != "（）" || e.Caret != 1 {
		t.Fatal(e.Text(), e.Caret)
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "" {
		t.Fatal("paired deletion")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Text() != "（）" {
		t.Fatal("paired undo")
	}
	e.SetSelection(0, e.Len())
	tt.Type("你好😀")
	e.SetSelection(0, e.Len())
	tt.Type("「")
	if e.Text() != "「你好😀」" || e.Selection().Range.End != 5 {
		t.Fatal("wrap", e.Text(), e.Selection())
	}
	e.SetSelection(0, e.Len())
	tt.Type("")
	tt.Compose("（", 1)
	if e.Text() != "" {
		t.Fatal("preedit saved")
	}
	tt.Type("（")
	if e.Text() != "（）" || e.Caret != 1 {
		t.Fatal("IME pair", e.Text(), e.Caret)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Text() != "" {
		t.Fatal("IME pair undo")
	}
	tt.Type("(")
	tt.Type("x")
	tt.Type(")")
	if e.Text() != "(x)" || e.Caret != 3 {
		t.Fatal("closer duplicated", e.Text(), e.Caret)
	}
	e.SetSelection(0, e.Len())
	tt.Type("don't")
	if e.Text() != "don't" {
		t.Fatal("apostrophe paired")
	}
}

func TestFootnoteClickReturnAndScripts(t *testing.T) {
	e, tt := newTester("Text[^proof] and x<sup>2</sup>.\n\n[^proof]: A note with **bold** text.")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[1].Kind != "footnote" || len(e.rows[0].layout.objects) != 2 {
		t.Fatal("footnote import", e.Doc.Blocks)
	}
	r := e.rows[0]
	o := r.layout.objects[0]
	b, _ := tt.Find("文档")
	tt.ClickAt(b.X+r.x+o.rect.X+2, b.Y+r.y+o.rect.Y+2)
	if e.LeafAt(e.Caret).Block.Kind != "footnote" {
		t.Fatal("click did not jump")
	}
	if !e.ReturnFootnote() || e.Caret != 4 {
		t.Fatal("return lost originating reference", e.Caret)
	}
	md := ExportMarkdown(e.Doc)
	round := ImportMarkdown(md)
	if round.Blocks[1].NoteLabel != "proof" || round.Blocks[0].Text() != e.Doc.Blocks[0].Text() {
		t.Fatal("footnote round trip", md)
	}
	e.SetSelection(1, 1)
	e.InsertFootnote()
	tt.Frame()
	if e.LeafAt(e.Caret).Block.Kind != "footnote" || !e.ReturnFootnote() || e.Caret != 1 {
		t.Fatal("new footnote has no return target")
	}
}

func TestReadOnlyBlocksAllInputsAndReadingReflow(t *testing.T) {
	e, tt := newTester("Read **this** $x^2$.")
	focus(e, tt)
	e.SetSelection(0, 4)
	before := e.Doc.Clone()
	e.SetReadOnly(true)
	tt.Type("overwrite")
	tt.Compose("候选", 2)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.SetClipboard("# change")
	tt.Key(ui.Cmd, ui.KeyV)
	e.SetScript("sup")
	e.InsertFootnote()
	if !reflect.DeepEqual(e.Doc, before) || e.composition != nil {
		t.Fatal("reading lock failed")
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != "Read" {
		t.Fatal("lock blocked copy", tt.Clipboard())
	}
	e.SetReadOnly(false)
	tt.Type("Write")
	if !strings.HasPrefix(e.Text(), "Write") {
		t.Fatal("unlock")
	}
	x, _ := newTester(strings.Repeat("content ", 80))
	old := x.rows[0].rect.H
	x.layout(Options{Width: 900, Theme: ui.LightTheme(), FontSize: 24, LineHeight: 2})
	if x.rows[0].rect.H <= old {
		t.Fatal("reading settings did not reflow")
	}
}

func TestLinkPopoverEditsWholeStyledLink(t *testing.T) {
	e, tt := newTester("Visit [**our** site](https://example.com) today.")
	focus(e, tt)
	e.SetSelection(8, 8)
	e.EditLink()
	tt.Frame()
	if e.linkText != "our site" || !tt.HasText("编辑链接") {
		t.Fatal("link range", e.linkText)
	}
	e.linkText = "新链接"
	e.linkURL = "https://example.org"
	tt.Frame()
	if err := tt.Click("应用链接"); err != nil {
		t.Fatal(err)
	}
	if e.Text() != "Visit 新链接 today." || e.linkAt(7) != "https://example.org" {
		t.Fatal("link not applied", e.Text())
	}
	e.Undo()
	if e.Text() != "Visit our site today." {
		t.Fatal("link undo")
	}
	e.EditLink()
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if e.linkOpen {
		t.Fatal("escape")
	}
}

func TestWikiHoverDelayAndDismissal(t *testing.T) {
	e := New("Read [[Target]] here.")
	calls := 0
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{Width: 800, Theme: c.Theme(), PreviewWiki: func(name string) WikiPreview { calls++; return WikiPreview{Title: name, Text: "Preview content"} }})
	}, 800, 600)
	tt.Frame()
	r, _, _ := e.BoundsForRange(ui.TextInputRange{Start: 9, End: 9})
	b, _ := tt.Find("文档")
	tt.Move(b.X+r.X, b.Y+r.Y+8)
	tt.Frame()
	if calls != 0 {
		t.Fatal("loaded before hover delay")
	}
	tt.Advance(500 * time.Millisecond)
	tt.Frame()
	if calls != 1 || !tt.HasText("Preview content") {
		t.Fatal("no preview", calls)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if e.wikiHover.open {
		t.Fatal("preview not dismissed")
	}
}

func TestImageResizeAndContinuousZoom(t *testing.T) {
	bitmap := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 800, 400)))
	e, tt := imageTester("![picture](photo.png)\n\nAfter", 900, ImageResource{Bitmap: bitmap}, nil, nil)
	r, ok := tt.Find("调整图片宽度")
	if !ok {
		t.Fatal("no image handle")
	}
	before := e.Doc.Clone()
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Move(r.X+70, r.Y+r.H/2)
	tt.Frame()
	tt.Release(r.X+70, r.Y+r.H/2)
	tt.Frame()
	if e.Doc.Blocks[0].ImageWidth <= 0 || e.Doc.Blocks[0].ImageWidth >= 800 {
		t.Fatal("resize", e.Doc.Blocks[0].ImageWidth)
	}
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("resize undo")
	}
	tt.Frame()
	tt.Click("查看图片：photo.png")
	tt.Key(0, ui.KeyEnter)
	tt.Click("放大图片")
	if e.imageViewer.zoom <= 1 {
		t.Fatal("zoom button", e.imageViewer.zoom)
	}
	canvas, ok := tt.Find("图片预览画布：滚轮缩放，拖动平移")
	if !ok {
		t.Fatal("canvas")
	}
	old := e.imageViewer.zoom
	tt.Scroll(canvas.X+80, canvas.Y+80, 0, -100)
	if e.imageViewer.zoom <= old {
		t.Fatal("continuous wheel zoom")
	}
	tt.Press(canvas.X+100, canvas.Y+100)
	tt.Move(canvas.X+150, canvas.Y+120)
	tt.Frame()
	tt.Release(canvas.X+150, canvas.Y+120)
	if e.imageViewer.pan.X == 0 {
		t.Fatal("pan")
	}
}

func TestScriptsHaveDistinctBaselinesAndPrintableText(t *testing.T) {
	e, _ := newTester("H<sub>2</sub>O x<sup>2</sup> 😀")
	r := e.rows[0]
	objects := r.layout.objects
	if len(objects) != 2 || !objects[0].script || !objects[1].script {
		t.Fatal("script layout")
	}
	sub := objects[0].rect.Y + objects[0].label.Lines()[0].Baseline
	sup := objects[1].rect.Y + objects[1].label.Lines()[0].Baseline
	base := r.layout.Lines()[0].Baseline
	if sub <= base || sup >= base {
		t.Fatal("script baselines", sub, base, sup)
	}
	e.Paginate(Options{Width: 600, Theme: ui.LightTheme()}, 900)
	var text strings.Builder
	for _, cluster := range e.PageText(0, 900) {
		text.WriteString(cluster.Text)
	}
	if text.String() != "H2O x2 😀" {
		t.Fatal("script PDF text", text.String())
	}
}
