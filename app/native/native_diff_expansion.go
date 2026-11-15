package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func (a *app) diffHunkButton(c *ui.Context, diff *backend.KernelWorkspaceTextDiff, index int, direction, header string) {
	key := "workspace.expandContextAbove"
	if direction == "down" {
		key = "workspace.expandContextBelow"
	}
	label := workspaceLabel(c.Locale(), key)
	var button ui.Element
	if header == "" {
		button = quietButton(c.Key(direction), label, "arrow-"+direction).Height(24).Padding(2, 8)
	} else {
		label += " · " + header
		button = iconButton(c.Key(direction), label, "arrow-"+direction).Size(24, 24).Padding(4)
	}
	button.Tooltip(label).Disabled(!diff.Prepared || !diff.CanExpand || a.workspacePending > 0).OnClick(func() {
		a.workspaceAction(backend.KernelWorkspaceAction{
			Type: "diff.expandHunk", FileID: diff.FileID, Identity: diff.Identity,
			Hunk: index, Direction: direction,
		})
	})
}

func (a *app) workspaceDiffSeparator(c *ui.Context, separator ui.Element, section *editor.DiffSeparator) {
	workspace := a.nativeWorkspace()
	if workspace == nil || workspace.TextDiff == nil || section.Lines != 0 {
		workspaceDiffSeparator(c, separator, section)
		return
	}
	diff := workspace.TextDiff
	for index, hunk := range diff.Hunks {
		if hunk.Header != section.Header || hunk.Header == "" || hunk.Expansion == "None" {
			continue
		}
		workspaceDiffSeparatorWithActions(c, separator, section, func(c *ui.Context) {
			if hunk.Expansion == "Up" || hunk.Expansion == "Both" || hunk.Expansion == "Short" {
				a.diffHunkButton(c, diff, index, "up", hunk.Header)
			}
			if hunk.Expansion == "Down" || hunk.Expansion == "Both" {
				a.diffHunkButton(c, diff, index, "down", hunk.Header)
			}
		})
		return
	}
	workspaceDiffSeparator(c, separator, section)
}

func (a *app) configureNativeDiffExpansion(state *editor.DiffState) {
	state.RenderSeparator = a.workspaceDiffSeparator
	state.Annotations, state.RenderAnnotation = nil, nil
	workspace := a.nativeWorkspace()
	if workspace == nil || workspace.TextDiff == nil {
		return
	}
	diff := workspace.TextDiff
	index := len(diff.Hunks) - 1
	if index < 0 || diff.Hunks[index].Header != "" || diff.Hunks[index].Expansion != "Down" || state.New.LineCount() == 0 {
		return
	}
	state.Annotations = []editor.DiffAnnotation{{
		Side: editor.DiffNew, Line: state.New.LineCount() - 1, Height: 32,
		Key: "original-context-below:" + diff.Identity,
	}}
	state.RenderAnnotation = func(c *ui.Context, _ editor.DiffAnnotation) {
		ui.Row(c).FillWidth().Padding(4).Children(func() { a.diffHunkButton(c, diff, index, "down", "") })
	}
}
