package editor

// The starter: while a document is blank, a title and nothing else
// written, a row of the blocks a page often starts with sits below it, as
// Feishu's and Notion's empty pages offer, so that a new page shows what it
// can hold without a menu. Typing anything puts it away.

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// starterKinds are the starter's blocks, by kind, in order.
var starterKinds = []string{"task", "bullet", "table", "callout", "code", "image", "columns"}

// Blank reports whether nothing is written but the title: the document is
// an optional level 1 heading and at most one empty paragraph.
func (e *Editor) Blank() bool {
	blocks := e.Doc.Blocks
	if len(blocks) > 0 && blocks[0].Kind == "heading" && blocks[0].Level == 1 {
		blocks = blocks[1:]
	}
	switch len(blocks) {
	case 0:
		return true
	case 1:
		b := blocks[0]
		return b.Kind == "paragraph" && b.Text() == "" && len(b.Children) == 0
	}
	return false
}

// starterItems are the starter's blocks the host supports.
func (e *Editor) starterItems() []insertion {
	var out []insertion
	for _, kind := range starterKinds {
		if kind == "image" && e.attach == nil {
			continue
		}
		for _, it := range insertions {
			if it.kind == kind {
				out = append(out, it)
			}
		}
	}
	return out
}

// start begins the body with item: in the empty paragraph below the title,
// made first when there is none, as one step of the history.
func (e *Editor) start(item insertion) {
	e.insertItemAfter(func() {
		blocks := e.Engine.Doc.Blocks
		last := blocks[len(blocks)-1]
		if len(blocks) == 1 && last.Kind == "heading" {
			last = document.NewBlock("paragraph", "")
			e.Engine.InsertAt(len(blocks), last)
		}
		e.Engine.FocusBlock(last.ID)
	}, item)
}

// starter draws the starter below the blank document, in the surface's
// coordinates.
func (e *Editor) starter(c *ui.Context) {
	if !e.Blank() || e.mark != nil || e.slashOpen {
		return
	}
	t := c.Theme()
	top := e.contentEnd + 16
	ui.Column(c.Key("starter")).Absolute().Left(e.padding).Top(top).Width(e.width - 2*e.padding).Gap(12).Children(func() {
		ui.Text(c, i18n.T("快速开始")).FontSize(13).TextColor(e.page.Caption)
		ui.Row(c).Gap(8).Wrap().Children(func() {
			for _, it := range e.starterItems() {
				chip := ui.ButtonBase(c.Key("starter:"+it.kind)).Height(34).PaddingX(12).Gap(8).Radius(8).
					Border(1, e.page.TableLine).AlignItems(ui.Center).Label(i18n.T("插入") + it.label()).Cursor(ui.CursorPointer)
				if chip.Hovered() {
					chip.Background(e.page.Hover)
				}
				chip.Children(func() {
					ui.Icon(c, it.icon).FontSize(16).TextColor(e.tint(it.tint))
					ui.Text(c, it.label()).FontSize(14).TextColor(t.Text)
				})
				if chip.Clicked() {
					e.start(it)
				}
			}
		})
		hint := i18n.T("或直接开始输入，输入「/」插入更多内容")
		if e.dropFiles != nil {
			hint += i18n.T("，也可以把图片拖到这里")
		}
		ui.Text(c, hint+"。").FontSize(13).TextColor(e.page.Placeholder)
	})
}
