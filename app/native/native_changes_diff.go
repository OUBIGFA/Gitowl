package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

type changesDiffLayout struct {
	width, expandedWidth, available float32
	expanded                        bool
}

func (a *app) isChangesWorkspace() bool {
	if a.repo == nil || a.tab != changesTab {
		return false
	}
	w := a.nativeWorkspace()
	return w == nil || w.Stash == nil || !w.Stash.Selected
}

func (a *app) expandChangesDiff() {
	if _, _, found := a.currentFile(); found {
		a.reviewFile(a.changeIndex)
	} else {
		a.reviewingFile, a.reviewOptionsOpen = true, false
	}
}

func (a *app) changesDiffToggle(c *ui.Context, tx locales.Messages) {
	label, icon := tx.ExpandDiff(), "panel-left-open"
	if a.reviewingFile {
		label, icon = tx.CollapseDiff(), "panel-left-close"
	}
	button := ui.ButtonBase(c.Key("toggle-diff")).Size(36, 36).Padding(9).Label(label).Tooltip(label).
		Expanded(a.reviewingFile).Disabled(!a.ready)
	quietSurface(c, button).Children(func() {
		// Reuse the registered Lucide panel symbols, facing the right pane.
		ui.Icon(c, icons[icon]).FontSize(18).Rotate(180)
	}).OnClick(func() {
		if a.reviewingFile {
			a.backFromReview()
		} else {
			a.expandChangesDiff()
		}
	})
}

// The native split is kept even while folded. Its minimum second pane holds
// the expand button, so the list and composer never change parents and native
// focus, text selection, undo and scrolling survive every panel transition.
func (a *app) changesWorkspace(c *ui.Context, tx locales.Messages, colors workspaceColors, available float32) {
	layout := &a.changesDiffLayout
	minimum := min(float32(280), (available-1)/2)
	fit := func(width float32) float32 { return max(minimum, min(width, available-1-minimum)) }
	if layout.available != available || layout.expanded != a.reviewingFile {
		if a.reviewingFile {
			width := layout.expandedWidth
			if width == 0 {
				width = (available - 1) / 2
			}
			layout.width = fit(width)
		} else {
			layout.width = available - 1 - c.Theme().Space(10)
		}
		layout.available, layout.expanded = available, a.reviewingFile
	}
	if a.reviewingFile {
		layout.width = fit(layout.width)
	}
	split := ui.Split(c.Key("changes-diff-split"), &layout.width, func() {
		ui.Column(c.Key("changes-primary")).Fill().MinWidth(0).Padding(0, 16, 0, 0).Children(func() {
			ui.Column(c.Key("changes-page")).Grow(1).MinHeight(0).FillWidth().Children(func() {
				a.changesOverview(c, tx, colors)
			})
			if a.commitCapable {
				a.commitForm(c, tx)
			}
		})
	}, func() {
		padding := float32(2)
		if a.reviewingFile {
			padding = 16
		}
		ui.Column(c.Key("changes-diff-pane")).Fill().MinWidth(0).Padding(0, 0, 0, padding).Children(func() {
			if a.reviewingFile {
				a.diffPane(c, tx, available-layout.width-1-padding)
			} else {
				ui.Row(c.Key("diff-heading")).FillWidth().MinHeight(36).Children(func() { a.changesDiffToggle(c, tx) })
			}
		})
	}).Fill().MinWidth(0).MinHeight(0)
	if split.Changed() {
		layout.width = fit(layout.width)
		layout.expandedWidth = layout.width
		if !a.reviewingFile {
			a.expandChangesDiff()
		}
	}
}
