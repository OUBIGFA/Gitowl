package editor

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

func (e *Editor) insertItem(item insertion) { e.insertItemAfter(nil, item) }

// insertItemAfter runs prepare, then inserts item at the caret, as one step
// of the history.
func (e *Editor) insertItemAfter(prepare func(), item insertion) {
	e.UnmarkText()
	e.Engine.Edit(func() {
		if prepare != nil {
			prepare()
		}
		leaf := e.Engine.LeafAt(e.Caret)
		if e.slashOpen {
			e.Engine.Replace(leaf.Start, leaf.End, "")
		}
		switch item.kind {
		case "table":
			e.Engine.Insert(document.Table(3, 3))
		case "columns":
			e.Engine.Insert(document.Columns())
		case "rule":
			e.Engine.Insert(document.NewBlock("rule", ""))
		case "image":
			// The host picks the images and inserts it at the caret.
		case "wiki":
			e.Engine.Replace(e.Caret, e.Caret, "[[")
		case "mermaid-flow":
			e.InsertMermaidFlowchart()
		case "mermaid-seq":
			e.InsertMermaidSequence()
		case "toggle":
			e.Engine.SetKind("toggle", 0)
			e.Engine.OpenToggle(e.Engine.LeafAt(e.Caret).Block.ID)
		default:
			e.Engine.SetKind(item.kind, item.level)
		}
	})
	e.slashOpen, e.slashDismissed = false, true
	e.RequestFocus, e.reveal = true, true
	e.sync()
	if item.kind == "image" && e.attach != nil {
		e.attach()
	}
}

// Corner holds the word count and the format menu, which the host draws
// in its toolbar beside the address. The page has no format bar: text is
// formatted from the bar over a selection, blocks from "/", the "+" and the
// handle beside each block, and everything from the command bar.
func (e *Editor) Corner(c *ui.Context) {
	leaf := e.Engine.LeafAt(e.Caret)
	ui.Row(c.Key("page-corner")).Gap(2).AlignItems(ui.Center).Children(func() {
		e.wordButton(c)
		// The lock and the format menu keep their places in both modes, so
		// the toolbar never shifts under the pointer: read-only dims the
		// format menu rather than taking it away.
		if !e.ReadOnly {
			e.moreMenu(c, leaf)
		} else {
			toolButton(c, "more", iconFormat, i18n.T("只读时不能修改格式"), false).Disabled(true).Opacity(.4)
		}
		icon, tip := iconUnlocked, i18n.T("编辑中，点击设为只读")
		if e.ReadOnly {
			icon, tip = iconLocked, i18n.T("只读，点击恢复编辑")
		}
		if toolButton(c, "reading-lock", icon, tip, e.ReadOnly).Clicked() {
			e.SetReadOnly(!e.ReadOnly)
		}
	})
}

// moreMenu holds the formats and actions of the caret's block, grouped:
// text, marks, paragraph and insertions always, then what the caret's
// block offers. What the selection bar or a shortcut already does (bold
// and the other marks, undo and redo) is not repeated here.
func (e *Editor) moreMenu(c *ui.Context, leaf document.Leaf) {
	toolButton(c, "more", iconFormat, i18n.T("更多格式"), false).Menu(func(m *ui.Menu) {
		style := e.Engine.CurrentStyle()
		m.Submenu(i18n.T("文字"), func(sub *ui.Menu) {
			if sub.Item(i18n.T("上标")).Checked(style.Script == "sup").Chosen() {
				e.SetScript("sup")
			}
			if sub.Item(i18n.T("下标")).Checked(style.Script == "sub").Chosen() {
				e.SetScript("sub")
			}
		})
		m.Submenu(i18n.T("手绘标注"), func(sub *ui.Menu) {
			for _, name := range document.Annotations {
				if sub.Item(i18n.T(AnnotationNames[name])).Checked(style.Annotation == name).Chosen() {
					e.Annotate(name, style.AnnotationColor)
				}
			}
			sub.Separator()
			if sub.Item(i18n.T("移除标注")).Disabled(style.Annotation == "").Chosen() {
				e.Annotate("", "")
			}
		})
		m.Submenu(i18n.T("段落"), func(sub *ui.Menu) {
			for _, a := range []struct{ name, value string }{{i18n.T("左对齐"), "left"}, {i18n.T("居中"), "center"}, {i18n.T("右对齐"), "right"}} {
				on := leaf.Block.Align == a.value || a.value == "left" && leaf.Block.Align == ""
				if sub.Item(a.name).Radio(on).Chosen() {
					e.Engine.Align(a.value)
					e.RequestFocus = true
				}
			}
			sub.Separator()
			menuAction(sub, i18n.T("增加缩进"), func() { e.Engine.Indent(1) })
			menuAction(sub, i18n.T("减少缩进"), func() { e.Engine.Indent(-1) })
		})
		m.Submenu(i18n.T("插入"), func(sub *ui.Menu) {
			menuAction(sub, i18n.T("行内公式"), e.InsertInlineMath)
			menuAction(sub, i18n.T("流程图"), e.InsertMermaidFlowchart)
			menuAction(sub, i18n.T("时序图"), e.InsertMermaidSequence)
			menuAction(sub, i18n.T("脚注"), func() { e.InsertFootnote(); e.RequestFocus, e.reveal = true, true })
			sub.Separator()
			menuAction(sub, i18n.T("跳转脚注 / 返回正文"), func() { e.FollowFootnote() })
		})
		switch leaf.Block.Kind {
		case "callout":
			m.Separator()
			m.Submenu(i18n.T("高亮块颜色"), func(sub *ui.Menu) {
				for _, tone := range document.CalloutColors {
					if sub.Item(CalloutName(tone)).Radio(leaf.Block.Color == tone).Chosen() {
						e.Engine.SetColor(leaf.Block.ID, tone)
						e.RequestFocus = true
					}
				}
			})
		case "code":
			m.Separator()
			if !isMermaidBlock(leaf.Block) {
				if m.Item(i18n.T("自动换行")).Checked(!e.isCodeNoWrap(leaf.Block)).Chosen() {
					e.ToggleCodeWrap(leaf.Block.ID)
					e.RequestFocus = true
				}
			}
			menuAction(m, i18n.T("复制代码"), func() { e.copyBlock(leaf.Block.ID) })
		case "cell":
			m.Separator()
			m.Submenu(i18n.T("表格"), func(sub *ui.Menu) {
				if table, row, col, ok := e.Doc.CellAt(leaf.Block.ID); ok {
					sub.Submenu(i18n.T("单元格背景颜色"), func(fill *ui.Menu) {
						for _, name := range append([]string{""}, document.TextHighlights...) {
							label := i18n.T("无")
							if name != "" {
								label = ColorName(name, true)
							}
							if fill.Item(label).Radio(leaf.Block.Background == name).Chosen() {
								e.Engine.SetCellBackground(table.ID, document.CellRange{Row0: row, Col0: col, Row1: row, Col1: col}, name)
							}
						}
					})
					if cols, rows := document.Span(leaf.Block); cols > 1 || rows > 1 {
						menuAction(sub, i18n.T("拆分单元格"), func() { e.Engine.SplitCell(leaf.Block.ID) })
					}
					sub.Separator()
				}
				menuAction(sub, i18n.T("增加一行"), func() { e.Engine.ResizeTable(true, 1) })
				menuAction(sub, i18n.T("增加一列"), func() { e.Engine.ResizeTable(false, 1) })
				menuAction(sub, i18n.T("删除末行"), func() { e.Engine.ResizeTable(true, -1) })
				menuAction(sub, i18n.T("删除末列"), func() { e.Engine.ResizeTable(false, -1) })
			})
		}
	})
}

// calloutEmoji are the icons a callout can take.
var calloutEmoji = []string{"💡", "👍", "🌟", "🎉", "📌", "📚", "✏️", "✍️", "❤️", "📍", "🥖", "⚽", "🎹", "🎼", "⛱️", "🎨", "🥇", "🏆", "🎁", "🌅",
	"🏕️", "🏖️", "🏝️", "🚅", "🥛", "🌰", "🍰", "🍞", "🦄", "🐵"}

// emojiGrid lets a callout take an emoji as its icon, or go back to its
// color's icon; done runs after a pick.
func (e *Editor) emojiGrid(c *ui.Context, id string, done func()) {
	b := e.blockByID(id)
	if b == nil {
		return
	}
	pick := func(emoji string) {
		e.Engine.SetEmoji(id, emoji)
		e.RequestFocus = true
		done()
	}
	items := append([]string{""}, calloutEmoji...)
	ui.Column(c).Gap(2).Children(func() {
		for row := 0; row < len(items); row += 8 {
			ui.Row(c.Key(row)).Justify(ui.SpaceBetween).Children(func() {
				for _, emoji := range items[row:min(row+8, len(items))] {
					label := i18n.T("使用颜色图标")
					if emoji != "" {
						label = i18n.T("图标：") + emoji
					}
					cell := ui.ButtonBase(c.Key("emoji:"+emoji)).Size(26, 26).Radius(4).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
					if b.Emoji == emoji {
						cell.Background(e.page.TurnActive)
					} else if cell.Hovered() {
						cell.Background(e.page.Hover)
					}
					cell.Children(func() {
						if emoji == "" {
							ui.Icon(c, calloutIcon(b.Color)).FontSize(16).TextColor(e.calloutHue(b.Color, c.Theme()))
						} else {
							ui.Text(c, emoji).FontSize(17)
						}
					})
					if cell.Clicked() {
						pick(emoji)
					}
				}
				for i := len(items); i < row+8; i++ {
					ui.Box(c.Key(i)).Size(26, 26)
				}
			})
		}
	})
}

// colorSwatches lets a callout's color be picked: a round swatch per tone,
// named for assistive technology and tooltips; done runs after a pick.
func (e *Editor) colorSwatches(c *ui.Context, id string, done func()) {
	t := c.Theme()
	b := e.blockByID(id)
	if b == nil {
		return
	}
	ui.Row(c).Gap(6).Children(func() {
		for _, tone := range document.CalloutColors {
			hue := e.calloutHue(tone, t)
			name := CalloutName(tone)
			s := ui.ButtonBase(c.Key("swatch:"+tone)).Size(28, 28).Radius(14).Center().Label(i18n.T("高亮块颜色：")+name).Tooltip(name).
				KeepFocus().Cursor(ui.CursorPointer).Background(hue.Alpha(.14)).Border(1, hue.Alpha(.4))
			if b.Color == tone {
				s.Border(2, hue)
			} else if s.Hovered() {
				s.Background(hue.Alpha(.24))
			}
			s.Children(func() { ui.Icon(c, calloutIcon(tone)).FontSize(14).TextColor(hue) })
			if s.Clicked() {
				e.Engine.SetColor(id, tone)
				e.RequestFocus = true
				done()
			}
		}
	})
}

// OpenColorPicker opens the color picker of callout id, as a click on its
// icon does.
// OpenBlockMenu opens the menu of top-level block id, as a click on its
// handle does, with the side panel sub ("indent", "color" or "") open.
func (e *Editor) OpenBlockMenu(id, sub string) {
	e.menuOpen, e.menuID, e.menuSub = true, id, sub
	e.menuScroll = ui.ScrollState{}
}

func (e *Editor) OpenColorPicker(id string) {
	for _, d := range e.decorations {
		if d.kind == "callout" && d.id == id {
			e.colorOpen, e.colorID, e.colorAt = true, id, calloutIconRect(d)
		}
	}
}

// blockByID finds a block anywhere in the document.
func (e *Editor) blockByID(id string) *document.Block {
	var walk func([]*document.Block) *document.Block
	walk = func(blocks []*document.Block) *document.Block {
		for _, b := range blocks {
			if b.ID == id {
				return b
			}
			if found := walk(b.Children); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(e.Doc.Blocks)
}

// wordCount counts the document as Chinese writers do: each Han
// character, kana or hangul is a word, as is each run of other letters and
// digits. It streams over leaf blocks without flattening the full document text.
func (e *Editor) wordCount() int {
	if e.wordsReady && e.wordsRevision == e.Revision() {
		return e.words
	}
	if change, ok := e.TextChangeSince(e.wordsRevision); ok && e.wordsReady {
		e.words += countWords(change.After) - countWords(change.Before)
	} else {
		e.sync()
		han, words := e.countEngineWordStats()
		e.words = han + words
	}
	e.wordsRevision, e.wordsReady = e.Revision(), true
	return e.words
}

type cachedWordCount struct {
	stamp runsStamp
	han   int
	words int
}

func (e *Editor) countEngineWordStats() (han, words int) {
	n := e.Engine.LeafCount()
	if e.wordCache == nil {
		e.wordCache = make(map[string]cachedWordCount, n)
	}
	for i := 0; i < n; i++ {
		leaf := e.Engine.LeafAtIndex(i)
		b := leaf.Block
		stamp := makeRunsStamp(b.Runs)
		if c, ok := e.wordCache[b.ID]; ok && c.stamp == stamp {
			han += c.han
			words += c.words
			continue
		}
		inWord := false
		bh, bw := 0, 0
		for j := range b.Runs {
			scanWordStats(b.Runs[j].Text, &bh, &bw, &inWord)
		}
		e.wordCache[b.ID] = cachedWordCount{stamp: stamp, han: bh, words: bw}
		han += bh
		words += bw
	}
	if len(e.wordCache) > n+cacheTrimHysteresis {
		for id := range e.wordCache {
			if !e.Engine.HasLeaf(id) {
				delete(e.wordCache, id)
			}
		}
	}
	return han, words
}

func scanWordStats(text string, han, words *int, inWord *bool) {
	for _, r := range text {
		switch {
		case r < 0x80:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				if !*inWord {
					*words++
				}
				*inWord = true
			} else {
				*inWord = false
			}
		case (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x3040 && r <= 0x30FF) || (r >= 0xAC00 && r <= 0xD7A3):
			*han++
			*inWord = false
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			*han++
			*inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if !*inWord {
				*words++
			}
			*inWord = true
		default:
			*inWord = false
		}
	}
}

func countWords(text string) int {
	han, words, inWord := 0, 0, false
	scanWordStats(text, &han, &words, &inWord)
	return han + words
}

// toolButton is a square icon button; on marks a format
// the selection already has.
func toolButton(c *ui.Context, key string, icon *ui.SVG, tip string, on bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(30, 30).Radius(7).Center().Label(tip).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
	color := t.Text.Alpha(.68)
	if b.Hovered() {
		b.Background(t.Text.Alpha(.06))
		color = t.Text.Alpha(.9)
	}
	if on {
		b.Background(t.Accent.Alpha(.12))
		color = t.Accent
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(color) })
	return b
}

// toolSeparator parts groups of a bar of tools.
func toolSeparator(c *ui.Context) {
	ui.Box(c).Width(1).Height(16).Margin(0, 6).Background(c.Theme().Border)
}

func (e *Editor) watchSlash() {
	wasOpen := e.slashOpen
	leaf := e.Engine.LeafAt(e.Caret)
	text := leaf.Block.Text()
	show := !e.ReadOnly && !e.slashDismissed && e.Anchor == e.Caret && leaf.Block.Kind == "paragraph" && strings.HasPrefix(text, "/") && !strings.Contains(text, "\n")
	if !show {
		e.slashOpen = false
		return
	}
	query := strings.TrimPrefix(text, "/")
	if !wasOpen || query != e.slashQuery {
		e.slashIndex = 0
		e.slashReveal = true
		e.slashScroll = ui.ScrollState{}
	}
	e.slashQuery, e.slashOpen = query, true
}
func (e *Editor) popups(c *ui.Context, surface ui.Element) {
	t := c.Theme()
	e.slashMenu(c, surface)
	e.selectionBar(c, surface)
	e.markBar(c, surface)
	if e.colorOpen && e.colorID != "" {
		bounds := e.surfaceBounds(surface)
		windowW, windowH := c.Size()
		anchor := ui.Rect{
			X: bounds.X + e.colorAt.X - 6,
			Y: bounds.Y + e.colorAt.Y + e.ownerVisualDY(e.colorID),
			W: e.colorAt.W,
			H: e.colorAt.H,
		}
		x, y, maxH := placeMenu(anchor, 230, 220, 6, windowW, windowH)
		ui.Overlay(c, func() {
			panel := ui.Column(c.Key("callout-colors")).Absolute().Left(x).Top(y).Width(230)
			e.popover(c, panel).Children(func() {
				ui.Scroll(c.Key("callout-colors-scroll")).MaxHeight(max(72, maxH-2)).Children(func() {
					ui.Column(c).Padding(10).Gap(6).Children(func() {
						ui.Text(c, i18n.T("高亮块颜色")).FontSize(12).TextColor(t.Text)
						e.colorSwatches(c, e.colorID, func() { e.colorOpen = false })
						ui.Text(c, i18n.T("图标")).FontSize(12).TextColor(t.Text).Margin(6, 0, 0, 0)
						e.emojiGrid(c, e.colorID, func() { e.colorOpen = false })
					})
				})
			})
		})
	}
	if e.langOpen && e.langID != "" {
		bounds := e.surfaceBounds(surface)
		windowW, windowH := c.Size()
		anchor := ui.Rect{
			X: bounds.X + e.langAt.X,
			Y: bounds.Y + e.langAt.Y + e.ownerVisualDY(e.langID),
			W: e.langAt.W,
			H: e.langAt.H,
		}
		x, y, maxH := placeMenu(anchor, 180, 294, 4, windowW, windowH)
		current := ""
		if b := e.blockByID(e.langID); b != nil {
			current = b.Language
		}
		ui.Overlay(c, func() {
			ui.Column(c.Key("code-languages")).Absolute().Left(x).Top(y).Width(180).
				Padding(6).Radius(10).Background(t.Background).Border(1, t.Border).Shadow(0, 8, 24, 0, t.Text.Alpha(.14)).KeepFocus().Children(func() {
				ui.Scroll(c).MaxHeight(min(280, maxH-14)).Children(func() {
					for _, l := range codeLanguages {
						row := ui.ButtonBase(c.Key("lang:" + l.name)).Height(30).PaddingX(8).Radius(6).KeepFocus().Label(i18n.T("代码语言：") + i18n.T(l.label)).Cursor(ui.CursorPointer)
						if row.Hovered() {
							row.Background(t.Text.Alpha(.05))
						}
						row.Children(func() {
							ui.Row(c).Grow(1).AlignItems(ui.Center).Children(func() {
								ui.Text(c, i18n.T(l.label)).FontSize(13).Grow(1)
								if strings.EqualFold(current, l.name) {
									ui.Icon(c, iconCheck).FontSize(14).TextColor(t.Accent)
								}
							})
						})
						if row.Clicked() {
							e.Engine.SetLanguage(e.langID, l.name)
							e.langOpen = false
							e.sync()
							e.RequestFocus = true
						}
					}
				})
			})
		})
	}
	e.blockMenu(c, surface)
	e.tableToolbar(c, surface)
	e.linkPopover(c, surface)
}

func (e *Editor) cellTab(back bool) bool {
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "cell" {
		return false
	}
	cells := e.Doc.Leaves()
	for i, cell := range cells {
		if cell.Block.ID != leaf.Block.ID {
			continue
		}
		next := i + 1
		if back {
			next = i - 1
		}
		if next >= 0 && next < len(cells) && cells[next].Owner == leaf.Owner {
			e.SetSelection(cells[next].Start, cells[next].Start)
			e.reveal = true
			return true
		}
		if !back {
			e.Engine.ResizeTable(true, 1)
			cells = e.Doc.Leaves()
			if i+1 < len(cells) {
				e.SetSelection(cells[i+1].Start, cells[i+1].Start)
			}
		}
		return true
	}
	return true
}

func menuAction(m *ui.Menu, label string, run func()) {
	if m.Item(label).Chosen() {
		run()
	}
}

// goalPresets are the word goals the count's menu offers.
var goalPresets = []int{500, 1000, 2000, 3000, 5000}

// readingMinutes is how long the page takes to read: 400 Chinese
// characters or 200 other words a minute, at least one.
func readingMinutes(text string) int {
	han, words, inWord := 0, 0, false
	scanWordStats(text, &han, &words, &inWord)
	return minutesFromStats(han, words)
}

func minutesFromStats(han, words int) int {
	return max(1, int(math.Ceil(float64(han)/400+float64(words)/200)))
}

func (e *Editor) readingMinutes() int {
	han, words := e.countEngineWordStats()
	return minutesFromStats(han, words)
}

// wordButton is the word count, with the goal and its progress under it
// when the page has one; its menu sets the goal and says how long the
// page takes to read.
func (e *Editor) wordButton(c *ui.Context) {
	t := c.Theme()
	words, goal := e.wordCount(), e.Doc.Goal
	count := fmt.Sprintf(i18n.T("%d 字"), words)
	if goal > 0 {
		count = fmt.Sprintf(i18n.T("%d / %d 字"), words, goal)
	}
	if sel := e.Selection().Range; sel.Start != sel.End {
		text, _ := e.TextForRange(sel)
		count = fmt.Sprintf(i18n.T("已选 %d / %d 字"), countWords(text), words)
	}
	reached := goal > 0 && words >= goal
	b := ui.ButtonBase(c.Key("word-count")).Height(30).PaddingX(6).Radius(7).Label(count).
		Tooltip(i18n.T("字数（中文按字、英文按词）· 点击设置字数目标")).KeepFocus().Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(t.Text.Alpha(.06))
	}
	b.Children(func() {
		ui.Column(c).Gap(3).AlignItems(ui.Center).Children(func() {
			color := t.TextMuted
			if reached {
				color = t.Accent
			}
			ui.Text(c, count).FontSize(12).TextColor(color).NoWrap()
			if goal > 0 {
				// A thin bar under the count fills toward the goal.
				const w = 44
				done := float32(min(words, goal)) / float32(goal)
				ui.Box(c).Size(w, 2).Radius(1).Background(t.Text.Alpha(.1)).Children(func() {
					ui.Box(c).Size(w*done, 2).Radius(1).Background(t.Accent)
				})
			}
		})
	})
	b.Menu(func(m *ui.Menu) {
		e.sync()
		m.Item(fmt.Sprintf(i18n.T("约 %d 分钟读完"), e.readingMinutes())).Disabled(true)
		if goal > 0 {
			left := goal - words
			if left > 0 {
				m.Item(fmt.Sprintf(i18n.T("距离目标还差 %d 字"), left)).Disabled(true)
			} else {
				m.Item(i18n.T("已达成字数目标 🎉")).Disabled(true)
			}
		}
		if e.ReadOnly {
			return
		}
		m.Separator()
		m.Submenu(i18n.T("字数目标"), func(sub *ui.Menu) {
			for _, n := range goalPresets {
				if sub.Item(fmt.Sprintf(i18n.T("%d 字"), n)).Checked(goal == n).Chosen() {
					e.Engine.SetGoal(n)
				}
			}
			sub.Separator()
			if sub.Item(i18n.T("自定义…")).Chosen() {
				e.goalEditing, e.goalText = true, ""
				if goal > 0 {
					e.goalText = fmt.Sprint(goal)
				}
			}
			if sub.Item(i18n.T("移除目标")).Disabled(goal == 0).Chosen() {
				e.Engine.SetGoal(0)
			}
		})
	})
	if e.goalEditing {
		e.goalPopover(c, b)
	}
}

// SetGoal sets the page's word goal, 0 for none.
func (e *Editor) SetGoal(words int) { e.Engine.SetGoal(words) }

// goalPopover takes a goal of the writer's own below the count: Enter
// sets it, Escape or a press outside puts it away.
func (e *Editor) goalPopover(c *ui.Context, anchor ui.Element) {
	r := anchor.Bounds()
	windowW, _ := c.Size()
	ui.Overlay(c, func() {
		panel := ui.Column(c.Key("goal-popover")).Absolute().Left(max(8, min(r.X+r.W-220, windowW-228))).Top(r.Y + r.H + 6).Width(220).Padding(12).Gap(8)
		e.popover(c, panel).Children(func() {
			ui.Text(c, i18n.T("字数目标")).FontSize(12).TextColor(e.page.Caption)
			in := ui.TextInput(c.Key("goal-input"), &e.goalText).Placeholder(i18n.T("例如 1500")).AutoFocus().Label(i18n.T("字数目标"))
			set := func() {
				n, err := strconv.Atoi(strings.TrimSpace(e.goalText))
				if err == nil && n >= 0 && n <= 1000000 {
					e.Engine.SetGoal(n)
					e.goalEditing = false
					e.RequestFocus = true
				}
			}
			if in.Submitted() {
				set()
			}
			if in.Shortcut(0, ui.KeyEscape) {
				e.goalEditing = false
				e.RequestFocus = true
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("goal-cancel"), i18n.T("取消")).Clicked() {
					e.goalEditing = false
				}
				if ui.PrimaryButton(c.Key("goal-set"), i18n.T("设定")).Clicked() {
					set()
				}
			})
		})
		if panel.PressedOutside() {
			e.goalEditing = false
		}
	})
}
