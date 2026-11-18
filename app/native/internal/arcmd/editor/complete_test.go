package editor

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func emojiTester(markdown string, auto bool) (*Editor, *ui.Tester) {
	e := New(markdown)
	emoji := func(typed string) []EmojiItem {
		var out []EmojiItem
		for _, it := range []EmojiItem{{"😄", "smile"}, {"😊", "smiling_face"}, {"🎉", "tada"}} {
			if strings.HasPrefix(it.Name, strings.ToLower(typed)) {
				out = append(out, it)
			}
		}
		return out
	}
	docs := func(typed string) []WikiItem { return []WikiItem{{Name: "周报", Hint: "工作"}} }
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), EmojiSuggest: emoji, WikiSuggest: docs, AutoSpace: auto})
	}, 900, 650)
	tt.Frame()
	return e, tt
}

func TestEmojiShortcodes(t *testing.T) {
	e, tt := emojiTester("", false)
	focus(e, tt)
	tt.Type("好 :smi")
	tt.Frame()
	if !e.complete.open || len(e.complete.items) != 2 || !tt.HasText(":smile:") {
		t.Fatal("the list opens", e.complete.items)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if e.Text() != "好 😊" {
		t.Fatal("pick", e.Text())
	}
	tt.Type(" :tada:")
	tt.Frame()
	if e.Text() != "好 😊 🎉" {
		t.Fatal("the closing colon puts the emoji in", e.Text())
	}
	tt.Type(" 12:30")
	tt.Frame()
	if e.complete.open || !strings.HasSuffix(e.Text(), "12:30") {
		t.Fatal("a time is not a shortcode")
	}
}

func TestMentions(t *testing.T) {
	e, tt := emojiTester("", false)
	focus(e, tt)
	tt.Type("截止 @mt")
	tt.Frame()
	if !e.complete.open || e.complete.items[0].label != "明天" {
		t.Fatal("dates", e.complete.items)
	}
	tt.Key(0, ui.KeyEnter)
	want := tt.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if e.Text() != "截止 "+want {
		t.Fatal("date", e.Text())
	}
	tt.Type(" @周")
	tt.Frame()
	if !tt.HasText("周报") {
		t.Fatal("documents")
	}
	tt.Key(0, ui.KeyEnter)
	if !strings.HasSuffix(e.Text(), "[[周报]]") {
		t.Fatal("mention links the document", e.Text())
	}
	tt.Type(" a@b")
	tt.Frame()
	if e.complete.open {
		t.Fatal("an address is not a mention")
	}
}

func TestAutoSpaceBetweenChineseAndLatin(t *testing.T) {
	e, tt := emojiTester("", true)
	focus(e, tt)
	tt.Type("用")
	tt.Type("Go")
	tt.Type("写")
	tt.Type("。OK")
	if e.Text() != "用 Go 写。OK" {
		t.Fatal(e.Text())
	}
	tt.Compose("中文", 2)
	tt.Type("中文")
	if e.Text() != "用 Go 写。OK 中文" {
		t.Fatal("input method", e.Text())
	}
	off, tt2 := emojiTester("", false)
	focus(off, tt2)
	tt2.Type("用")
	tt2.Type("Go")
	if off.Text() != "用Go" {
		t.Fatal("off by default")
	}
}

func TestWordGoalFromTheCount(t *testing.T) {
	e := New("# 标题\n\n写作可以很简单")
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		ui.Column(c).Fill().Children(func() {
			e.Corner(c)
			View(c, e, Options{Width: w, Theme: c.Theme()})
		})
	}, 900, 650)
	tt.Frame()
	if err := tt.Click("9 字"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("字数目标", "1000 字"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	tt.Frame()
	if e.Doc.Goal != 1000 || !tt.HasText("9 / 1000 字") {
		t.Fatal("goal", e.Doc.Goal, tt.Texts())
	}
	if err := tt.Click("9 / 1000 字"); err != nil {
		t.Fatal(err)
	}
	menu := strings.Join(tt.Menu(), "|")
	if !strings.Contains(menu, "约 1 分钟读完") || !strings.Contains(menu, "距离目标还差 991 字") {
		t.Fatal(menu)
	}
	if err := tt.ChooseMenuItem("字数目标", "自定义…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	e.goalText = "5"
	tt.Frame()
	if err := tt.Click("设定"); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Goal != 5 || !tt.HasText("9 / 5 字") {
		t.Fatal("custom goal", e.Doc.Goal)
	}
}

func TestReadingMinutes(t *testing.T) {
	if readingMinutes("") != 1 || readingMinutes(strings.Repeat("字", 800)) != 2 || readingMinutes(strings.Repeat("word ", 400)+strings.Repeat("字", 400)) != 3 {
		t.Fatal(readingMinutes(strings.Repeat("字", 800)))
	}
}

func TestSlashMenuInEnglish(t *testing.T) {
	i18n.SetLanguage("en")
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	e, tt := newTester("")
	focus(e, tt)
	tt.Type("/")
	tt.Frame()
	if !tt.HasText("Basic") || !tt.HasText("Table") || !tt.HasText("Toggle") {
		t.Fatal(tt.Texts())
	}
	tt.Type("tab")
	tt.Frame()
	if len(e.slashEntries()) == 0 || e.slashEntries()[0].item.kind != "table" {
		t.Fatal("English names are searched")
	}
}
