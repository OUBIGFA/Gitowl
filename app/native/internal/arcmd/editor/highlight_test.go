package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPerLanguageSyntaxHighlightingAndNoLimit(t *testing.T) {
	pg := pageOf(Page{}, ui.LightTheme())
	concat := func(spans []ui.Span) string {
		var b strings.Builder
		for _, sp := range spans {
			b.WriteString(sp.Text)
		}
		return b.String()
	}
	hasSpan := func(spans []ui.Span, sub string, want ui.Color) bool {
		for _, sp := range spans {
			if strings.Contains(sp.Text, sub) && sp.Color == want {
				return true
			}
		}
		return false
	}

	// Rust: #[derive(Debug)] must NOT be treated as a comment!
	rustSrc := "#[derive(Debug)]\nfn main() {\n    let s = r#\"hello # not comment\"#;\n    // real comment\n}"
	rustSpans := codeSpansFor("b1", "rust", rustSrc, pg, nil)
	if concat(rustSpans) != rustSrc {
		t.Fatal("rust spans altered text")
	}
	if !hasSpan(rustSpans, "#[derive(Debug)]", pg.Tokens.Keyword) {
		t.Fatalf("rust attribute not highlighted as keyword: %+v", rustSpans)
	}
	if hasSpan(rustSpans, "#[derive(Debug)]", pg.Tokens.Comment) {
		t.Fatal("rust attribute misclassified as comment")
	}
	if !hasSpan(rustSpans, "r#\"hello # not comment\"#", pg.Tokens.String) {
		t.Fatalf("rust raw string misclassified: %+v", rustSpans)
	}
	if !hasSpan(rustSpans, "// real comment", pg.Tokens.Comment) {
		t.Fatal("rust line comment missed")
	}

	// CSS: #fff hex color must NOT be treated as a comment!
	cssSrc := ".card {\n  color: #1f2023;\n  /* comment */\n}"
	cssSpans := codeSpansFor("b2", "css", cssSrc, pg, nil)
	if concat(cssSpans) != cssSrc {
		t.Fatal("css spans altered text")
	}
	if !hasSpan(cssSpans, "#1f2023", pg.Tokens.Number) {
		t.Fatalf("css hex color not highlighted as number: %+v", cssSpans)
	}
	if !hasSpan(cssSpans, "color", pg.Tokens.Type) {
		t.Fatalf("css property not highlighted as type: %+v", cssSpans)
	}

	// JSON: keys vs string values vs boolean/null.
	jsonSrc := "{\n  \"name\": \"ArcMD\",\n  \"count\": 42,\n  \"ok\": true\n}"
	jsonSpans := codeSpansFor("b3", "json", jsonSrc, pg, nil)
	if concat(jsonSpans) != jsonSrc {
		t.Fatal("json spans altered text")
	}
	if !hasSpan(jsonSpans, "\"name\"", pg.Tokens.Type) || !hasSpan(jsonSpans, "\"ArcMD\"", pg.Tokens.String) || !hasSpan(jsonSpans, "true", pg.Tokens.Keyword) {
		t.Fatalf("json spans incorrect: %+v", jsonSpans)
	}

	// SQL: -- comment and case-insensitive keywords.
	sqlSrc := "-- list notes\nselect id, title from notes where active = 1;"
	sqlSpans := codeSpansFor("b4", "sql", sqlSrc, pg, nil)
	if concat(sqlSpans) != sqlSrc {
		t.Fatal("sql spans altered text")
	}
	if !hasSpan(sqlSpans, "-- list notes", pg.Tokens.Comment) || !hasSpan(sqlSpans, "select", pg.Tokens.Keyword) {
		t.Fatalf("sql spans incorrect: %+v", sqlSpans)
	}

	// Plain text: no false-positive keyword coloring.
	plainSrc := "return value if true # not comment"
	plainSpans := codeSpansFor("b5", "", plainSrc, pg, nil)
	if len(plainSpans) != 1 || plainSpans[0].Text != plainSrc || plainSpans[0].Color != pg.CodeText {
		t.Fatalf("plain text unexpectedly colored: %+v", plainSpans)
	}

	// Over 64 KB code block must still be highlighted (64 KB limit removed), and incremental cache must match full lexing.
	big := strings.Repeat("func step(x int) int { return x + 1 }\n", 2600) // ~98 KB
	if len(big) <= 64<<10 {
		t.Fatalf("fixture too small: %d", len(big))
	}
	cache := map[string]*codeLexCache{}
	bigSpans := codeSpansFor("big", "go", big, pg, cache)
	if concat(bigSpans) != big {
		t.Fatal("large go spans altered text")
	}
	if !hasSpan(bigSpans, "return", pg.Tokens.Keyword) {
		t.Fatal("large code block > 64KB lost syntax highlighting")
	}
	edited := strings.Replace(big, "return x + 1", "return x + 2", 1)
	incSpans := codeSpansFor("big", "go", edited, pg, cache)
	freshSpans := codeSpansFor("big", "go", edited, pg, nil)
	if len(incSpans) != len(freshSpans) || concat(incSpans) != edited {
		t.Fatalf("incremental lexing diverged: inc=%d fresh=%d", len(incSpans), len(freshSpans))
	}
	for i := range incSpans {
		if incSpans[i] != freshSpans[i] {
			t.Fatalf("incremental span %d differs: %+v vs %+v", i, incSpans[i], freshSpans[i])
		}
	}
}

func TestCodeWrapToggleAndHorizontalScroll(t *testing.T) {
	longLine := "value := \"" + strings.Repeat("abcdefghij", 16) + "\""
	e, tt := newTester("```go\n" + longLine + "\n```")
	tt.Frame()

	b := e.Doc.Blocks[0]
	wrappedH := e.rows[0].rect.H
	if e.isCodeNoWrap(b) {
		t.Fatal("expected code wrap enabled by default")
	}

	e.ToggleCodeWrap(b.ID)
	tt.Frame()
	if !e.isCodeNoWrap(b) {
		t.Fatal("expected code wrap disabled after toggle")
	}
	unwrappedH := e.rows[0].rect.H
	if unwrappedH >= wrappedH {
		t.Fatalf("expected unwrapped height (%v) < wrapped height (%v)", unwrappedH, wrappedH)
	}
	_, _, availW, contentW, _, _ := e.codeScrollFor(e.rows[0])
	if contentW <= availW {
		t.Fatalf("expected unwrapped code contentW (%v) > availW (%v)", contentW, availW)
	}

	// Wheel horizontal scroll inside the unwrapped code block.
	bounds, _ := tt.Find("文档")
	r := e.rows[0].rect
	if !e.handleCodeScrollInput(ui.InputEvent{Kind: ui.InputScroll, X: r.X + 80, Y: r.Y + 48, DX: 120}) {
		t.Fatal("expected horizontal scroll event inside unwrapped code block to be handled")
	}
	tt.Frame()
	if e.codeScrollX[b.ID] <= 0 {
		t.Fatalf("expected codeScrollX > 0, got %v", e.codeScrollX[b.ID])
	}
	_ = bounds

	// Read-only mode still allows toggling code wrap.
	e.SetReadOnly(true)
	tt.Frame()
	e.ToggleCodeWrap(b.ID)
	tt.Frame()
	if e.isCodeNoWrap(b) {
		t.Fatal("expected code wrap toggle back to wrapped in ReadOnly")
	}
}
