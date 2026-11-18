package editor

// The slash menu, the block menu and the bar over a selection: a 247 wide
// popover with an icon grid of the basic blocks and tinted rows below, 32
// tall rows with an 18 icon, and a 40 tall bar of 24 buttons.

import (
	"fmt"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

const (
	menuWidth = 247
	slashMaxH = 351
	gridCols  = 6
)

// Slash menu groups.
var slashGroups = []struct{ title, search string }{
	{"基础", "基础 basic jichu jc"},
	{"常用", "常用 common changyong cy"},
}

// insertion is an item of the slash menu and the block type menus.
type insertion struct {
	name, kind, search string
	level              int
	icon               *ui.SVG
	// group is the slash menu group; grid items of the basic group show as
	// icons while nothing is typed after "/".
	group int
	grid  bool
	// tint names the icon's color in the slash menu's rows.
	tint string
	// md is the Markdown shortcut, shown in tooltips.
	md string
}

var insertions = []insertion{
	{name: "正文", kind: "paragraph", search: "text paragraph zhengwen zw wenben", icon: iconParagraph, tint: "blue"},
	{name: "一级标题", kind: "heading", level: 1, search: "h1 heading title yijibiaoti yjbt", icon: iconHeading1, grid: true, tint: "blue", md: "# 空格"},
	{name: "二级标题", kind: "heading", level: 2, search: "h2 heading erjibiaoti ejbt", icon: iconHeading2, grid: true, tint: "blue", md: "## 空格"},
	{name: "三级标题", kind: "heading", level: 3, search: "h3 heading sanjibiaoti sjbt", icon: iconHeading3, grid: true, tint: "blue", md: "### 空格"},
	{name: "四级标题", kind: "heading", level: 4, search: "h4 heading sijibiaoti sjbt", icon: iconHeading4, grid: true, tint: "blue", md: "#### 空格"},
	{name: "五级标题", kind: "heading", level: 5, search: "h5 heading wujibiaoti wjbt", icon: iconHeading5, grid: true, tint: "blue", md: "##### 空格"},
	{name: "有序列表", kind: "numbered", search: "ordered numbered list youxuliebiao yxlb", icon: iconOrdered, grid: true, tint: "indigo", md: "1. 空格"},
	{name: "无序列表", kind: "bullet", search: "bullet unordered list wuxuliebiao wxlb", icon: iconList, grid: true, tint: "indigo", md: "- 空格"},
	{name: "任务", kind: "task", search: "todo task renwu rw", icon: iconTodo, group: 1, grid: true, tint: "indigo", md: "[] 空格"},
	{name: "代码块", kind: "code", search: "code daimakuai dmk", icon: iconCodeBlock, grid: true, tint: "green", md: "``` 空格"},
	{name: "引用", kind: "quote", search: "quote yinyong yy cite", icon: iconQuote, grid: true, tint: "blue", md: "> 空格"},
	{name: "高亮块", kind: "callout", search: "callout gaoliangkuai glk highlight", icon: iconBulb, group: 1, grid: true, tint: "orange"},
	{name: "分隔线", kind: "rule", search: "divider rule fengexian fgx line", icon: iconRule, grid: true, tint: "orange", md: "---"},
	{name: "图片", kind: "image", search: "image picture tupian tp", icon: iconImage, group: 1, tint: "yellow"},
	{name: "表格", kind: "table", search: "table biaoge bg", icon: iconTable, group: 1, tint: "teal"},
	{name: "折叠块", kind: "toggle", search: "toggle details fold collapse zhediekuai zdk zhedie", icon: iconToggle, group: 1, tint: "indigo"},
	{name: "分栏", kind: "columns", search: "columns grid fenlan fl shuanglan", icon: iconColumns, group: 1, tint: "indigo"},
	{name: "公式", kind: "math", search: "math formula equation latex tex gongshi gs", icon: iconSigma, group: 1, tint: "teal"},
	{name: "流程图", kind: "mermaid-flow", search: "mermaid flowchart graph diagram liuchengtu lct", icon: iconWorkflow, group: 1, tint: "teal", md: "```mermaid"},
	{name: "时序图", kind: "mermaid-seq", search: "mermaid sequence diagram shixutu sxt", icon: iconSequence, group: 1, tint: "teal"},
	{name: "链接到文档", kind: "wiki", search: "wiki link backlink shuanglian sl lianjie wendang", icon: iconLink, group: 1, tint: "blue", md: "[["},
}

// label is the item's name in the language shown.
func (it insertion) label() string { return i18n.T(it.name) }

// slashEntry is a row or grid cell of the slash menu.
type slashEntry struct {
	item insertion
	grid bool
}

// slashEntries are the slash menu's items in order: with nothing typed,
// the basic grid then the common rows; else the matches, best first —
// name before group, prefix before substring.
func (e *Editor) slashEntries() []slashEntry {
	var out []slashEntry
	q := strings.ToLower(strings.TrimSpace(e.slashQuery))
	if q == "" {
		for _, it := range insertions {
			if it.grid {
				out = append(out, slashEntry{it, true})
			}
		}
		for _, it := range insertions {
			if it.group == 1 && (e.attach != nil || it.kind != "image") {
				out = append(out, slashEntry{it, false})
			}
		}
		return out
	}
	ranked := [4][]slashEntry{}
	for _, it := range insertions {
		if e.attach == nil && it.kind == "image" {
			continue
		}
		words := strings.Fields(strings.ToLower(it.label() + " " + it.search))
		group := strings.Fields(strings.ToLower(slashGroups[it.group].search))
		prefix := func(ws []string) bool {
			for _, w := range ws {
				if strings.HasPrefix(w, q) {
					return true
				}
			}
			return false
		}
		contains := func(ws []string) bool { return strings.Contains(strings.Join(ws, " "), q) }
		switch {
		case prefix(words):
			ranked[0] = append(ranked[0], slashEntry{it, false})
		case prefix(group):
			ranked[1] = append(ranked[1], slashEntry{it, false})
		case contains(words):
			ranked[2] = append(ranked[2], slashEntry{it, false})
		case contains(group):
			ranked[3] = append(ranked[3], slashEntry{it, false})
		}
	}
	for _, r := range ranked {
		out = append(out, r...)
	}
	return out
}

// tint is the color of a slash row's icon.
func (e *Editor) tint(name string) ui.Color {
	if c, ok := e.page.Tints[name]; ok {
		return c
	}
	return e.page.Marker
}

var popoverTransition = ui.ElementTransition{
	Duration: 110 * time.Millisecond,
	Position: true,
	Size:     true,
	Colors:   true,
	Enter:    &ui.Motion{Opacity: 0.9, Scale: 1, Y: 3},
}

type slashPillMatch struct{ ed *Editor }

func slidingPill(c *ui.Context, match any, fill ui.Color, radius float32) {
	ui.Box(c.Key("active-pill")).Absolute().Left(0).Top(0).Right(0).Bottom(0).
		Radius(radius).Background(fill).PassThrough().
		Transition(ui.ElementTransition{Duration: 95 * time.Millisecond, Match: match})
}

// popover styles a floating panel : a hairline, radius 6, a
// soft drop, and smooth position/size/enter transitions.
func (e *Editor) popover(c *ui.Context, el ui.Element) ui.Element {
	t := c.Theme()
	return el.Background(e.page.Popover).Border(1, t.Border).Radius(6).Shadow(0, 4, 16, 0, t.Text.Alpha(.12)).Transition(popoverTransition).KeepFocus()
}

// menuRow is a 32 tall row: an 18 icon, the label, an optional hint.
func (e *Editor) menuRow(c *ui.Context, key string, icon *ui.SVG, iconColor ui.Color, label, hint string, on bool) ui.Element {
	t := c.Theme()
	row := ui.ButtonBase(c.Key(key)).Height(32).PaddingX(8).Radius(4).KeepFocus().Label(label).Cursor(ui.CursorPointer)
	if on || row.Hovered() {
		row.Background(e.page.Hover)
	}
	row.Children(func() {
		ui.Row(c).Grow(1).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(18).TextColor(iconColor)
			ui.Text(c, label).FontSize(14).TextColor(t.Text).Grow(1).NoWrap()
			switch hint {
			case "":
			case "›":
				ui.Icon(c, iconChevronRight).FontSize(14).TextColor(e.page.Caption)
			default:
				ui.Text(c, hint).FontSize(12).TextColor(e.page.Caption).NoWrap()
			}
		})
	})
	return row
}

// menuSeparator is a hairline between groups of rows.
func menuSeparator(c *ui.Context) {
	ui.Box(c).Height(1).Margin(4, 0).Background(c.Theme().Border)
}

// groupTitle names a group of the slash menu.
func (e *Editor) groupTitle(c *ui.Context, title string) {
	ui.Row(c).Height(32).PaddingX(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(14).TextColor(e.page.Caption)
	})
}

// gridCell is a 24 icon button of the slash and block menus' grids.
func (e *Editor) gridCell(c *ui.Context, key string, it insertion, on, active bool) ui.Element {
	tip := it.label()
	if it.md != "" {
		tip += "\nMarkdown：" + it.md
	}
	cell := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(4).Center().Label(it.label()).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
	switch {
	case active:
		cell.Background(e.page.TurnActive)
	case !on && cell.Hovered():
		cell.Background(e.page.Hover)
	}
	color := e.page.CodeText
	if active {
		color = e.page.Marker
	}
	cell.Children(func() {
		if on && !active {
			slidingPill(c, slashPillMatch{e}, e.page.Hover, 4)
		}
		ui.Icon(c, it.icon).FontSize(18).TextColor(color)
	})
	return cell
}

// iconGrid lays cells out six to a row, spread across the menu.
func iconGrid[T any](c *ui.Context, items []T, cell func(i int, item T)) {
	ui.Column(c).Gap(12).PaddingX(4).Margin(0, 0, 8, 0).Children(func() {
		for start := 0; start < len(items); start += gridCols {
			ui.Row(c.Key(start)).Justify(ui.SpaceBetween).Children(func() {
				for i := start; i < min(start+gridCols, len(items)); i++ {
					cell(i, items[i])
				}
				for i := len(items); i < start+gridCols; i++ {
					ui.Box(c.Key(i)).Size(24, 24)
				}
			})
		}
	})
}

// placeMenu positions a floating menu of size w×h near anchor within the
// window: below by default, flipping above when it would clip the bottom edge,
// keeping strictly to the larger side when neither side fits full height (above
// minSideH) so the anchor stays uncovered, and clamping with maxH so tall menus
// remain reachable via inner scrolling.
func placeMenu(anchor ui.Rect, w, h, gap, windowW, windowH float32) (x, y, maxH float32) {
	const margin = 8
	const minSideH = 180
	windowLimit := max(48, windowH-2*margin)
	x = max(margin, min(anchor.X, windowW-w-12))
	below := anchor.Y + anchor.H + gap
	roomBelow := windowH - margin - below
	roomAbove := anchor.Y - gap - margin
	switch {
	case min(h, windowLimit) <= roomBelow:
		maxH = min(windowLimit, roomBelow)
		y = below
	case min(h, windowLimit) <= roomAbove:
		maxH = min(windowLimit, roomAbove)
		shownH := min(h, maxH)
		y = anchor.Y - shownH - gap
	case max(roomAbove, roomBelow) >= minSideH:
		if roomAbove > roomBelow {
			maxH = min(windowLimit, roomAbove)
			shownH := min(h, maxH)
			y = max(margin, anchor.Y-shownH-gap)
		} else {
			maxH = min(windowLimit, roomBelow)
			y = below
		}
	default:
		maxH = windowLimit
		shownH := min(h, maxH)
		if roomAbove > roomBelow {
			y = max(margin, anchor.Y-shownH-gap)
		} else {
			y = min(below, windowH-margin-shownH)
		}
		y = max(margin, min(y, windowH-margin-shownH))
	}
	return x, y, maxH
}

// slashMenu is the "/" menu at the caret.
func (e *Editor) slashMenu(c *ui.Context, surface ui.Element) {
	if !e.slashOpen || !e.focused {
		return
	}
	t := c.Theme()
	bounds := e.surfaceBounds(surface)
	caret, _, _ := e.BoundsForRange(e.Selection().Range)
	entries := e.slashEntries()
	e.slashIndex = max(0, min(e.slashIndex, len(entries)-1))
	windowW, windowH := c.Size()
	naturalH := float32(slashMaxH)
	if strings.TrimSpace(e.slashQuery) != "" {
		naturalH = min(float32(slashMaxH), float32(max(1, len(entries)))*32+20)
	}
	anchor := ui.Rect{X: bounds.X + caret.X, Y: bounds.Y + caret.Y, W: caret.W, H: caret.H}
	x, y, maxH := placeMenu(anchor, menuWidth, naturalH, 4, windowW, windowH)
	ui.Overlay(c, func() {
		menu := ui.Column(c.Key("slash-menu")).Absolute().Left(x).Top(y).Width(menuWidth).Padding(1)
		e.popover(c, menu).Children(func() {
			ui.Scroll(c.Key("slash-scroll")).MaxHeight(min(float32(slashMaxH), maxH) - 4).TrackScroll(&e.slashScroll).Children(func() {
				topAnchor := ui.Box(c.Key("slash-top")).Height(0)
				var selectedEl ui.Element
				var hasSelected bool
				ui.Column(c).Padding(8).Children(func() {
					if len(entries) == 0 {
						ui.Row(c).Height(32).PaddingX(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, i18n.T("未能找到相关结果")).FontSize(14).TextColor(e.page.Caption)
						})
						return
					}
					run := func(it insertion) { e.insertItem(it) }
					var grid []int
					for i, en := range entries {
						if en.grid {
							grid = append(grid, i)
						}
					}
					if len(grid) > 0 {
						e.groupTitle(c, i18n.T(slashGroups[0].title))
						iconGrid(c, grid, func(_ int, i int) {
							it := entries[i].item
							cell := e.gridCell(c, "slash:"+it.label(), it, i == e.slashIndex, false)
							if i == e.slashIndex {
								selectedEl, hasSelected = cell, true
							}
							if cell.Clicked() {
								run(it)
							}
						})
					}
					group := -1
					for i, en := range entries {
						if en.grid {
							continue
						}
						if en.item.group != group && e.slashQuery == "" {
							group = en.item.group
							e.groupTitle(c, i18n.T(slashGroups[group].title))
						}
						it := en.item
						selected := i == e.slashIndex
						row := ui.ButtonBase(c.Key("slash-row:" + it.label())).Height(32).PaddingX(4).Radius(4).KeepFocus().Label(it.label()).Cursor(ui.CursorPointer)
						if !selected && row.Hovered() {
							row.Background(e.page.Hover)
						}
						row.Children(func() {
							if selected {
								slidingPill(c, slashPillMatch{e}, e.page.Hover, 4)
							}
							ui.Row(c).Grow(1).AlignItems(ui.Center).Children(func() {
								ui.Box(c).Size(24, 24).Center().Children(func() {
									ui.Icon(c, it.icon).FontSize(18).TextColor(e.tint(it.tint))
								})
								ui.Text(c, it.label()).FontSize(14).TextColor(t.Text).Margin(0, 0, 0, 16).Grow(1)
							})
						})
						if selected {
							selectedEl, hasSelected = row, true
						}
						if row.Clicked() {
							run(it)
						}
					}
				})
				bottomAnchor := ui.Box(c.Key("slash-bottom")).Height(0)
				if e.slashReveal {
					e.slashReveal = false
					if len(entries) > 0 {
						if e.slashIndex == 0 || (entries[e.slashIndex].grid && e.slashIndex < gridCols) {
							topAnchor.ScrollIntoView()
						} else if e.slashIndex == len(entries)-1 {
							bottomAnchor.ScrollIntoView()
						}
						if hasSelected {
							selectedEl.ScrollIntoView()
						}
					}
				}
			})
		})
	})
}

// slashKeys moves through the slash menu: the arrows step through the
// grid and the rows in order, Enter runs, Escape closes.
func (e *Editor) slashKeys(ev ui.InputEvent) bool {
	if !e.slashOpen || e.mark != nil {
		return false
	}
	entries := e.slashEntries()
	switch ev.Key {
	case ui.KeyEscape:
		e.slashOpen, e.slashDismissed = false, true
	case ui.KeyDown, ui.KeyRight:
		e.slashIndex = min(len(entries)-1, e.slashIndex+1)
		e.slashReveal = true
	case ui.KeyUp, ui.KeyLeft:
		e.slashIndex = max(0, e.slashIndex-1)
		e.slashReveal = true
	case ui.KeyEnter:
		if len(entries) > 0 {
			e.insertItem(entries[max(0, min(e.slashIndex, len(entries)-1))].item)
		}
	default:
		return false
	}
	return true
}

// turnItems are the block types a block can turn into from its menu:
// none for a table or rule; code and callouts lose the kinds that cannot
// hold them.
func turnItems(b *document.Block) []insertion {
	var out []insertion
	switch b.Kind {
	case "paragraph", "heading", "bullet", "numbered", "task", "quote", "code", "callout":
	default:
		return nil
	}
	for _, it := range insertions {
		switch {
		case it.kind == "rule" || it.kind == "table" || it.kind == "columns" || it.kind == "image" || it.kind == "mermaid-flow" || it.kind == "mermaid-seq":
		case b.Kind == "code" && it.kind == "callout":
		case b.Kind == "callout" && (it.kind == "code" || it.kind == "quote"):
		default:
			out = append(out, it)
		}
	}
	return out
}

// sameKind reports whether b already is it.
func sameKind(b *document.Block, it insertion) bool {
	return b.Kind == it.kind && (it.kind != "heading" || b.Level == it.level)
}

// blockMenuHeight returns the natural outer height of the block menu for b.
func (e *Editor) blockMenuHeight(b *document.Block) float32 {
	// Border(1) + inner Padding(8), indent row, 3 separators, 4 edit rows, 2 move rows, add-below row.
	h := float32(301)
	if items := turnItems(b); len(items) > 0 {
		rows := (len(items) + gridCols - 1) / gridCols
		h += float32(rows)*36 + 5
	}
	if b.Kind == "callout" {
		h += 32
	}
	if b.Kind == "code" && !isMermaidBlock(b) {
		h += 32
	}
	if e.bookmarks.toggle != nil {
		h += 32
	}
	return h
}

// blockSideMenuHeight returns the natural outer height of a block menu's side
// submenu panel.
func blockSideMenuHeight(b *document.Block, sub string) float32 {
	if sub == "color" {
		return 70
	}
	h := float32(82)
	if b.Kind != "code" && b.Kind != "callout" && b.Kind != "rule" && b.Kind != "table" && b.Kind != "columns" {
		h += 105
	}
	return h
}

// blockMenu is the menu of the block whose handle was clicked: its type as
// an icon grid, indent and alignment, color, then cut, copy and delete.
func (e *Editor) blockMenu(c *ui.Context, surface ui.Element) {
	if !e.menuOpen {
		return
	}
	b := e.blockByID(e.menuID)
	var handle *blockHandle
	for i := range e.handles {
		if e.handles[i].id == e.menuID {
			handle = &e.handles[i]
		}
	}
	if b == nil || handle == nil {
		e.menuOpen = false
		return
	}
	if len(e.blocks.ids) > 1 && e.blockSelected(e.menuID) {
		e.blocksMenu(c, surface, handle)
		return
	}
	t := c.Theme()
	bounds := e.surfaceBounds(surface)
	id := e.menuID
	done := func() { e.menuOpen, e.menuSub = false, ""; e.RequestFocus = true; e.sync() }
	act := func(run func()) func() { return func() { run(); done() } }
	windowW, windowH := c.Size()
	anchor := ui.Rect{
		X: bounds.X + handle.rect.X,
		Y: bounds.Y + handle.rect.Y + e.ownerVisualDY(handle.id),
		W: handle.rect.W,
		H: handle.rect.H,
	}
	x, y, maxH := placeMenu(anchor, menuWidth, e.blockMenuHeight(b), 4, windowW, windowH)
	ui.Overlay(c, func() {
		menu := ui.Column(c.Key("block-menu")).Absolute().Left(x).Top(y).Width(menuWidth)
		e.popover(c, menu).Children(func() {
			ui.Scroll(c.Key("block-menu-scroll")).MaxHeight(maxH - 2).TrackScroll(&e.menuScroll).Children(func() {
				ui.Column(c).Padding(8).Children(func() {
					if items := turnItems(b); len(items) > 0 {
						iconGrid(c, items, func(_ int, it insertion) {
							if e.gridCell(c, "turn:"+it.label(), it, false, sameKind(b, it)).Clicked() {
								e.Engine.FocusBlock(id)
								e.sync()
								e.insertItem(it)
								done()
							}
						})
						menuSeparator(c)
					}
					hover := func(row ui.Element, sub string) ui.Element {
						if row.Hovered() || (sub != "" && row.Clicked()) {
							e.menuSub = sub
						}
						return row
					}
					hover(e.menuRow(c, "bm-indent", iconIndent, e.page.CodeText, i18n.T("缩进和对齐"), "›", e.menuSub == "indent"), "indent")
					if b.Kind == "callout" {
						hover(e.menuRow(c, "bm-color", iconPalette, e.page.CodeText, i18n.T("颜色"), "›", e.menuSub == "color"), "color")
					}
					if b.Kind == "code" && !isMermaidBlock(b) {
						if hover(e.menuRow(c, "bm-wrap", iconWrapText, e.page.CodeText, i18n.T("自动换行"), "", !e.isCodeNoWrap(b)), "").Clicked() {
							act(func() { e.ToggleCodeWrap(id) })()
						}
					}
					menuSeparator(c)
					for _, row := range []struct {
						key, label string
						icon       *ui.SVG
						run        func()
					}{
						{"bm-cut", i18n.T("剪切"), iconScissors, func() { e.copyBlockContent(id); e.Engine.DeleteBlock(id) }},
						{"bm-copy", i18n.T("复制"), iconCopy, func() { e.copyBlockContent(id) }},
						{"bm-duplicate", i18n.T("创建副本"), iconDuplicate, func() { e.Engine.Duplicate(id) }},
						{"bm-delete", i18n.T("删除"), iconTrash, func() { e.Engine.DeleteBlock(id) }},
					} {
						if hover(e.menuRow(c, row.key, row.icon, e.page.CodeText, row.label, "", false), "").Clicked() {
							act(row.run)()
						}
					}
					menuSeparator(c)
					for _, row := range []struct {
						key, label string
						icon       *ui.SVG
						delta      int
					}{{"bm-up", i18n.T("上移"), iconArrowUp, -1}, {"bm-down", i18n.T("下移"), iconArrowDown, 1}} {
						if hover(e.menuRow(c, row.key, row.icon, e.page.CodeText, row.label, "", false), "").Clicked() {
							act(func() { e.Engine.MoveBlock(id, handle.index+row.delta) })()
						}
					}
					menuSeparator(c)
					if e.bookmarks.toggle != nil {
						_, marked := e.bookmarks.marks[id]
						label := i18n.T("添加书签")
						if marked {
							label = i18n.T("移除书签")
						}
						if hover(e.menuRow(c, "bm-bookmark", iconBookmark, e.page.CodeText, label, e.bookmarks.key, false), "").Clicked() {
							toggle := e.bookmarks.toggle
							act(func() { toggle(id) })()
						}
					}
					if hover(e.menuRow(c, "bm-add", iconSquarePlus, e.page.CodeText, i18n.T("在下方添加"), "", false), "").Clicked() {
						e.menuOpen, e.menuSub = false, ""
						e.insertAfter(id)
					}
				})
			})
		})
		outside := menu.PressedOutside()
		// The side panel of a "›" row opens beside the menu, flipping to the
		// opposite side or clamping inside the window when space is tight.
		if e.menuSub != "" {
			rowOffset := float32(0)
			if n := len(turnItems(b)); n > 0 {
				rowOffset += float32((n+gridCols-1)/gridCols)*36 + 5
			}
			if e.menuSub == "color" {
				rowOffset += 32
			}
			sideX := x + menuWidth + 4
			if sideX+menuWidth > windowW-8 {
				if x-menuWidth-4 >= 8 {
					sideX = x - menuWidth - 4
				} else {
					sideX = max(8, windowW-menuWidth-8)
				}
			}
			sideMaxH := max(48, windowH-16)
			sideH := min(blockSideMenuHeight(b, e.menuSub), sideMaxH)
			alignedY := max(y, y+rowOffset-e.menuScroll.Y)
			sideY := max(8, min(alignedY, windowH-8-sideH))
			side := ui.Column(c.Key("block-menu-side")).Absolute().Left(sideX).Top(sideY).Width(menuWidth)
			e.popover(c, side).Children(func() {
				ui.Scroll(c.Key("block-menu-side-scroll")).MaxHeight(sideMaxH - 2).Children(func() {
					ui.Column(c).Padding(8).Children(func() {
						switch e.menuSub {
						case "indent":
							if b.Kind != "code" && b.Kind != "callout" && b.Kind != "rule" && b.Kind != "table" && b.Kind != "columns" {
								for _, a := range []struct{ key, label, value string }{{"al-l", i18n.T("左对齐"), "left"}, {"al-c", i18n.T("居中对齐"), "center"}, {"al-r", i18n.T("右对齐"), "right"}} {
									on := b.Align == a.value || a.value == "left" && b.Align == ""
									icon := map[string]*ui.SVG{"left": iconAlignLeft, "center": iconAlignCenter, "right": iconAlignRight}[a.value]
									if e.menuRow(c, a.key, icon, e.page.CodeText, a.label, "", on).Clicked() {
										act(func() { e.Engine.FocusBlock(id); e.Engine.Align(a.value) })()
									}
								}
								menuSeparator(c)
							}
							if e.menuRow(c, "in-inc", iconIndent, e.page.CodeText, i18n.T("增加缩进"), "Tab", false).Clicked() {
								act(func() { e.Engine.FocusBlock(id); e.Engine.Indent(1) })()
							}
							if e.menuRow(c, "in-dec", iconOutdent, e.page.CodeText, i18n.T("减少缩进"), "Shift + Tab", false).Clicked() {
								act(func() { e.Engine.FocusBlock(id); e.Engine.Indent(-1) })()
							}
						case "color":
							ui.Text(c, i18n.T("高亮块颜色")).FontSize(12).TextColor(t.Text).Margin(0, 0, 6, 0)
							e.colorSwatches(c, id, done)
						}
					})
				})
			})
			if !side.PressedOutside() {
				outside = false
			}
		}
		if outside {
			e.menuOpen, e.menuSub = false, ""
		}
	})
}

// copyBlockContent puts block id on the clipboard with ArcMD's structured
// fragment, as the block menu's copy does.
func (e *Editor) copyBlockContent(id string) {
	start, end := -1, -1
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == id || leaf.Owner == id {
			if start < 0 {
				start = leaf.Start
			}
			end = leaf.End
		}
	}
	if start < 0 || e.services == (ui.Services{}) {
		return
	}
	e.SetSelection(start, end)
	e.copySelection(e.services.WriteClipboard)
	e.SetSelection(start, start)
}

// selectionBar floats over selected text once the pointer lets go:
// block type and alignment menus, the marks, the
// link, then the block's other actions.
func (e *Editor) selectionBar(c *ui.Context, surface ui.Element) {
	sel := e.Selection().Range
	if !e.focused || sel.Start == sel.End || e.dragging || e.slashOpen || e.mark != nil || e.menuOpen || e.linkOpen {
		e.annotatePanel = false
		return
	}
	if _, _, across := e.cellRange(); across {
		// Cells selected across get the cell toolbar instead.
		e.annotatePanel = false
		return
	}
	first, _, ok := e.BoundsForRange(ui.TextInputRange{Start: sel.Start, End: sel.Start})
	if !ok {
		return
	}
	last, _, _ := e.BoundsForRange(ui.TextInputRange{Start: sel.End, End: sel.End})
	bounds := surface.Bounds()
	width, height := float32(390), float32(42)
	windowW, windowH := c.Size()
	x := max(8, min(bounds.X+first.X-12, windowW-width-12))
	if first.Y == last.Y {
		// One line: centered over it, but not left of the text column,
		// which would put it over the sidebar.
		x = max(8, min(max(bounds.X+(first.X+last.X)/2-width/2, bounds.X+e.padding-12), windowW-width-12))
	}
	// Above the selection's first line while that stays in view, else below
	// its last; a selection taller than the view keeps the bar at the
	// view's top edge rather than off it, over the text it marks.
	top, bottom := bounds.Y+e.Scroll.Y, bounds.Y+e.Scroll.Y+e.viewport
	y := bounds.Y + first.Y - height - 5
	if y < top+4 {
		y = bounds.Y + last.Y + last.H + 5
		if y+height > bottom-4 {
			y = top + 8
		}
	}
	leaf := e.Engine.LeafAt(e.Caret)
	style := e.Engine.CurrentStyle()
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("selection-bar")).Absolute().Left(x).Top(y).Height(height).PaddingX(8).Gap(8).AlignItems(ui.Center)
		e.popover(c, bar).Children(func() {
			drop := func(key, tip string, icon *ui.SVG, build func(m *ui.Menu)) {
				b := ui.ButtonBase(c.Key(key)).Size(44, 24).Radius(4).Label(tip).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
				if b.Hovered() {
					b.Background(e.page.Hover)
				}
				b.Menu(build)
				b.Children(func() {
					ui.Row(c).Grow(1).Gap(6).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icon).FontSize(18).TextColor(e.page.CodeText)
						ui.Icon(c, iconChevron).FontSize(12).TextColor(e.page.Placeholder)
					})
				})
			}
			sep := func() { ui.Box(c).Size(1, 20).Background(c.Theme().Border) }
			drop("float:turn", i18n.T("转换为"), kindIcon(leaf.Block), func(m *ui.Menu) {
				for _, it := range turnItems(leaf.Block) {
					if m.Item(it.label()).Checked(sameKind(leaf.Block, it)).Chosen() {
						e.insertItem(it)
					}
				}
			})
			sep()
			align := map[string]*ui.SVG{"center": iconAlignCenter, "right": iconAlignRight}[leaf.Block.Align]
			if align == nil {
				align = iconAlignLeft
			}
			drop("float:align", i18n.T("对齐方式"), align, func(m *ui.Menu) {
				for _, a := range []struct{ name, value string }{{i18n.T("左对齐"), "left"}, {i18n.T("居中对齐"), "center"}, {i18n.T("右对齐"), "right"}} {
					if m.Item(a.name).Checked(leaf.Block.Align == a.value || a.value == "left" && leaf.Block.Align == "").Chosen() {
						e.Engine.Align(a.value)
						e.RequestFocus = true
					}
				}
			})
			sep()
			mark := func(key string, icon *ui.SVG, tip, marker string, on bool) {
				if markButton(c, e.page, key, icon, tip, on).Clicked() {
					e.Wrap(marker)
					e.RequestFocus = true
				}
			}
			mark("float:**", iconBold, i18n.T("加粗 (⌘B)\nMarkdown：**文本**"), "**", style.Bold)
			mark("float:~~", iconStrike, i18n.T("删除线 (⇧⌘X)\nMarkdown：~~文本~~"), "~~", style.Strike)
			mark("float:*", iconItalic, i18n.T("倾斜 (⌘I)\nMarkdown：*文本*"), "*", style.Italic)
			mark("float:_", iconUnderline, i18n.T("下划线 (⌘U)"), "_", style.Underline)
			if markButton(c, e.page, "float:link", iconLink, i18n.T("添加链接"), style.Link != "").Clicked() {
				e.EditLink()
			}
			mark("float:`", iconCode, i18n.T("行内代码 (⌘E)\nMarkdown：`代码`"), "`", style.Code)
			sep()
			e.annotateSplit(c)
		})
		if e.annotatePanel {
			// Below the bar, or above it when the window ends first.
			py := y + height + 4
			if py+annotationPanelHeight > windowH-8 && y-annotationPanelHeight-4 > 8 {
				py = y - annotationPanelHeight - 4
			}
			px := max(8, min(x+width-annotationPanelWidth, windowW-annotationPanelWidth-8))
			py = max(8, min(py, windowH-annotationPanelHeight-8))
			e.annotationPanel(c, px, py)
		}
	})
}

// colorNames name the text colors and highlights for people.
var colorNames = map[string]string{
	"gray": "灰色", "red": "红色", "orange": "橙色", "yellow": "黄色", "green": "绿色", "blue": "蓝色", "purple": "紫色",
	"light-gray": "浅灰", "light-red": "浅红", "light-orange": "浅橙", "light-yellow": "浅黄", "light-green": "浅绿",
	"light-blue": "浅蓝", "light-purple": "浅紫", "medium-gray": "中灰",
}

// ColorName names a text color or highlight; strong highlights read as
// "深红" and so on.
func ColorName(name string, highlight bool) string {
	if highlight && !strings.HasPrefix(name, "light-") && name != "medium-gray" {
		return i18n.T("深") + strings.TrimSuffix(i18n.T(colorNames[name]), "色")
	}
	return i18n.T(colorNames[name])
}

// markButton is a 24 button of the selection bar; on marks a format the
// selection has.
func markButton(c *ui.Context, pg Page, key string, icon *ui.SVG, tip string, on bool) ui.Element {
	label := strings.SplitN(tip, " ", 2)[0]
	label = strings.SplitN(label, "\n", 2)[0]
	b := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(4).Center().Label(label).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
	color := pg.CodeText
	switch {
	case on:
		b.Background(pg.TurnActive)
		color = pg.Marker
	case b.Hovered():
		b.Background(pg.Hover)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(18).TextColor(color) })
	return b
}

// kindIcon is the icon of a block's type.
func kindIcon(b *document.Block) *ui.SVG {
	for _, it := range insertions {
		if sameKind(b, it) {
			return it.icon
		}
	}
	return iconParagraph
}

// blocksMenu is the block menu of several selected blocks: the types they
// can all turn into, indent, then cut, copy, duplicate, delete and move,
// each acting on every selected block at once.
func (e *Editor) blocksMenu(c *ui.Context, surface ui.Element, handle *blockHandle) {
	ids := e.BlockSelection()
	bounds := e.surfaceBounds(surface)
	done := func() { e.menuOpen, e.menuSub = false, ""; e.RequestFocus = true; e.sync() }
	// The types every selected text block can take.
	var items []insertion
	for _, it := range insertions {
		switch it.kind {
		case "paragraph", "heading", "bullet", "numbered", "task", "quote", "toggle":
			items = append(items, it)
		}
	}
	rows := (len(items) + gridCols - 1) / gridCols
	menuH := float32(18 + 28 + rows*36 - 4 + 9 + 64 + 9 + 128 + 9 + 64)
	windowW, windowH := c.Size()
	anchor := ui.Rect{
		X: bounds.X + handle.rect.X,
		Y: bounds.Y + handle.rect.Y + e.ownerVisualDY(handle.id),
		W: handle.rect.W,
		H: handle.rect.H,
	}
	x, y, maxH := placeMenu(anchor, menuWidth, menuH, 4, windowW, windowH)
	ui.Overlay(c, func() {
		menu := ui.Column(c.Key("blocks-menu")).Absolute().Left(x).Top(y).Width(menuWidth)
		e.popover(c, menu).Children(func() {
			ui.Scroll(c.Key("blocks-menu-scroll")).MaxHeight(maxH - 2).TrackScroll(&e.menuScroll).Children(func() {
				ui.Column(c).Padding(8).Children(func() {
					ui.Row(c).Height(28).PaddingX(4).AlignItems(ui.Center).Children(func() {
						ui.Text(c, fmt.Sprintf(i18n.T("已选 %d 个内容块"), len(ids))).FontSize(12).TextColor(e.page.Caption)
					})
					iconGrid(c, items, func(_ int, it insertion) {
						if e.gridCell(c, "turn-all:"+it.label(), it, false, false).Clicked() {
							e.Engine.SetBlocksKind(ids, it.kind, it.level)
							done()
						}
					})
					menuSeparator(c)
					for _, row := range []struct {
						key, label, hint string
						icon             *ui.SVG
						run              func()
					}{
						{"bms-indent", i18n.T("增加缩进"), "Tab", iconIndent, func() { e.Engine.IndentBlocks(ids, 1) }},
						{"bms-outdent", i18n.T("减少缩进"), "Shift + Tab", iconOutdent, func() { e.Engine.IndentBlocks(ids, -1) }},
					} {
						if e.menuRow(c, row.key, row.icon, e.page.CodeText, row.label, row.hint, false).Clicked() {
							row.run()
							done()
						}
					}
					menuSeparator(c)
					for _, row := range []struct {
						key, label, hint string
						icon             *ui.SVG
						run              func()
					}{
						{"bms-cut", i18n.T("剪切"), "⌘X", iconScissors, func() {
							e.copyBlocks(ids, e.services.WriteClipboard)
							e.Engine.DeleteBlocks(ids)
							e.clearBlocks()
						}},
						{"bms-copy", i18n.T("复制"), "⌘C", iconCopy, func() { e.copyBlocks(ids, e.services.WriteClipboard) }},
						{"bms-duplicate", i18n.T("创建副本"), "", iconDuplicate, func() {
							if copies := e.Engine.DuplicateBlocks(ids); len(copies) > 0 {
								// The copies are selected once they are laid out.
								e.blocks.pending = [2]string{copies[0], copies[len(copies)-1]}
							}
						}},
						{"bms-delete", i18n.T("删除"), "⌫", iconTrash, func() { e.Engine.DeleteBlocks(ids); e.clearBlocks() }},
					} {
						if e.menuRow(c, row.key, row.icon, e.page.CodeText, row.label, row.hint, false).Clicked() {
							row.run()
							done()
						}
					}
					menuSeparator(c)
					for _, row := range []struct {
						key, label, hint string
						icon             *ui.SVG
						delta            int
					}{{"bms-up", i18n.T("上移"), "⇧⌘↑", iconArrowUp, -1}, {"bms-down", i18n.T("下移"), "⇧⌘↓", iconArrowDown, 1}} {
						if e.menuRow(c, row.key, row.icon, e.page.CodeText, row.label, row.hint, false).Clicked() {
							e.Engine.ShiftBlocks(ids, row.delta)
							done()
						}
					}
				})
			})
		})
		if menu.PressedOutside() {
			e.menuOpen, e.menuSub = false, ""
		}
	})
}
