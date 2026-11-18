package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestMultilinePasteCaretAndContinuedTyping(t *testing.T) {
	first := strings.Repeat("第一段文字会在页面中自动换行。", 4)
	last := strings.Repeat("最后一段也会换行，光标应该停在整段末尾。", 6) + "😀"
	for _, width := range []int{420, 900} {
		for _, format := range []string{"native", "html", "markdown", "plain", "crlf"} {
			t.Run(fmt.Sprintf("%s/width-%d", format, width), func(t *testing.T) {
				e, tt := newTester(first + "\n\n" + last)
				tt.SetSize(width, 650)
				focus(e, tt)
				tt.Key(ui.Cmd, ui.KeyA)
				selection := e.Selection()
				tt.Key(ui.Cmd, ui.KeyC)
				if e.Selection() != selection {
					t.Fatal("copy moved the selection")
				}
				tt.Key(0, ui.KeyBackspace)
				switch format {
				case "html":
					tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.HTML, []byte("<p><strong>"+first+"</strong></p><p>"+last+"</p>")))))
				case "markdown":
					tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.Markdown, []byte("**"+first+"**\n\n"+last)))))
				case "plain":
					tt.SetClipboard(first + "\n" + last)
				case "crlf":
					tt.SetClipboard(first + "\r\n" + last)
				}
				tt.Key(ui.Cmd, ui.KeyV)
				want := first + "\n" + last
				if e.Text() != want {
					t.Fatal("paste changed the text")
				}
				end := ui.UTF16Len(want)
				if e.Anchor != end || e.Caret != end {
					t.Fatalf("paste selection = %d,%d; want %d,%d", e.Anchor, e.Caret, end, end)
				}
				start := ui.UTF16Len(first) + 1
				firstLine, _, _ := e.BoundsForRange(ui.TextInputRange{Start: start, End: start})
				caret, _, ok := e.BoundsForRange(e.Selection().Range)
				if !ok || caret.Y <= firstLine.Y {
					t.Fatal("caret did not reach the final wrapped line")
				}
				tt.Key(ui.Cmd, ui.KeyZ)
				if e.Text() != "" {
					t.Fatal("paste was not undone in one step")
				}
				tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
				if e.Text() != want || e.Anchor != end || e.Caret != end {
					t.Fatal("redo did not restore the pasted caret")
				}
				tt.Type("继续写")
				if e.Text() != want+"继续写" {
					t.Fatal("continued typing did not append to the pasted text")
				}
			})
		}
	}
}
