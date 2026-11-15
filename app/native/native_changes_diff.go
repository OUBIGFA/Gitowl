package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

type workspaceDiffLayout struct {
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

func (a *app) isFileWorkspace() bool {
	return a.isChangesWorkspace() || a.repo != nil && a.tab == historyTab
}

func (a *app) expandWorkspaceDiff() {
	if _, _, found := a.currentFile(); found {
		index := a.changeIndex
		if a.tab == historyTab {
			index = a.commitFileIndex
		}
		a.reviewFile(index)
	} else {
		a.reviewingFile, a.reviewOptionsOpen = true, false
	}
}

func (a *app) workspaceDiffToggle(c *ui.Context, tx locales.Messages) {
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
			a.expandWorkspaceDiff()
		}
	})
}

// Changes and history share this split and its pane styling. Keeping it while
// folded preserves native focus, selection, undo and list/diff scroll state.
func (a *app) fileWorkspace(c *ui.Context, tx locales.Messages, colors workspaceColors, available float32) {
	layout := &a.diffPanelLayout
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
	bottomPadding := float32(0)
	if a.reviewingFile {
		bottomPadding = 16
	}
	split := ui.Split(c.Key("workspace-diff-split"), &layout.width, func() {
		ui.Column(c.Key("workspace-primary")).Fill().MinWidth(0).Padding(0, 16, bottomPadding, 0).Children(func() {
			ui.Column(c.Key("workspace-overview")).Grow(1).MinHeight(0).FillWidth().Children(func() {
				if a.tab == historyTab {
					a.historyPage(c, tx)
				} else {
					a.changesOverview(c, tx, colors)
				}
			})
			if a.tab == changesTab && a.commitCapable {
				a.commitForm(c, tx)
			}
		})
	}, func() {
		ui.Column(c.Key("workspace-diff-pane")).Fill().MinWidth(0).Children(func() {
			if a.reviewingFile {
				a.diffPane(c, tx, available-layout.width-1)
			} else {
				ui.Row(c.Key("diff-heading")).FillWidth().MinHeight(36).Children(func() { a.workspaceDiffToggle(c, tx) })
			}
		})
	}).Fill().MinWidth(0).MinHeight(0)
	if split.Changed() {
		layout.width = fit(layout.width)
		layout.expandedWidth = layout.width
		if !a.reviewingFile {
			a.expandWorkspaceDiff()
		}
	}
}
