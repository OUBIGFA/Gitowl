package editor

import (
	"strings"
	"testing"
)

func TestImportHTML(t *testing.T) {
	doc := ImportHTML(`<meta charset="utf-8"><style>p{}</style>
<h1>Title</h1>
<p>Some <b>bold</b>, <span style="font-style: italic">italic</span> and <a href="https://go.dev">a link</a>.<br>Second line</p>
<ul><li>one<ul><li>nested</li></ul></li><li><input type="checkbox" checked> done</li></ul>
<ol><li>first</li></ol>
<blockquote><p>quoted</p></blockquote>
<pre><code class="language-go">fmt.Println("hi")
</code></pre>
<hr>
<table><tr><th>a</th><th>b</th></tr><tr><td><i>c</i></td></tr></table>
<img src="https://x/y.png" alt="pic"><script>alert(1)</script>`)
	var got []string
	for _, b := range doc.Blocks {
		got = append(got, b.Kind+":"+b.Text())
	}
	want := []string{
		"heading:Title", "paragraph:Some bold, italic and a link.", "paragraph:Second line",
		"bullet:one", "bullet:nested", "task:done", "numbered:first", "quote:quoted",
		`code:fmt.Println("hi")`, "rule:", "table:", "paragraph:pic",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("blocks\n got %q\nwant %q", got, want)
	}
	b := doc.Blocks
	if b[0].Level != 1 || b[4].Indent != 1 || !b[5].Checked || b[8].Language != "go" {
		t.Errorf("attributes: level %d indent %d checked %v lang %q", b[0].Level, b[4].Indent, b[5].Checked, b[8].Language)
	}
	runs := b[1].Runs
	if !runs[1].Style.Bold || !runs[3].Style.Italic || runs[5].Style.Link != "https://go.dev" {
		t.Errorf("styles %+v", runs)
	}
	table := b[10]
	if len(table.Children) != 2 || len(table.Children[1].Children) != 2 || !table.Children[1].Children[0].Runs[0].Style.Italic {
		t.Errorf("table %+v", table.Children)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if rich(ImportHTML("<p>just text</p>")) {
		t.Error("plain text counted as rich")
	}
}
