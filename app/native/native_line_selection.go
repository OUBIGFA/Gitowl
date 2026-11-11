package main

import (
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func (a *app) nativeDiffSelection() *backend.KernelDiffSelection {
	if a.core == nil || a.coreSnapshot == nil || a.coreSnapshot.Selected == nil || a.tab != changesTab || a.loadingDiff || a.preview == nil {
		return nil
	}
	return a.workspaceReviewSelection(a.coreSnapshot.Selected.DiffSelection)
}

// MyGo SetPatch buffers only hunk contents. The kernel supplies their matching
// indices separately from the original patch IDs; omitted context and no-newline
// markers must never be mistaken for selectable added/deleted lines.
func (a *app) nativeRangeIDs(patchIndex int, selected editor.DiffLineRange) []int {
	selection := a.nativeDiffSelection()
	if selection == nil || patchIndex < 0 || patchIndex >= len(a.preview.patches) {
		return nil
	}
	oldOffset, newOffset := 0, 0
	for _, patch := range a.preview.patches[:patchIndex] {
		for _, hunk := range patch.file.Hunks {
			for _, line := range hunk.Lines {
				if line == "" || strings.HasPrefix(line, "\\") {
					continue
				}
				if line[0] != '+' {
					oldOffset++
				}
				if line[0] != '-' {
					newOffset++
				}
			}
		}
	}
	start, end := selected.Start, selected.End
	if end < start {
		start, end = end, start
	}
	var ids []int
	for _, line := range selection.Lines {
		index, offset := line.NewBufferLine, newOffset
		if selected.Side == editor.DiffOld {
			index, offset = line.OldBufferLine, oldOffset
		}
		if index != nil && *index >= start+offset && *index <= end+offset {
			ids = append(ids, line.ID)
		}
	}
	return ids
}

func (a *app) includeCoreLineIDs(ids []int, included bool, ref backend.KernelRepositoryReference, selection *backend.KernelDiffSelection) {
	if a.core == nil || selection == nil || len(ids) == 0 || a.composerLocked() || a.coreInclusionPending != 0 {
		return
	}
	// Both owner and patch identity were captured from the frame that offered
	// this action. Never attach old line IDs to a newer diff/repository.
	core, ctx, fileID, identity := a.core, a.ctx, selection.FileID, selection.Identity
	a.queueCore(nativeOperation{inclusion: true, call: func() (backend.KernelResult, error) {
		return core.IncludeLines(ctx, ref, fileID, identity, ids, included)
	}})
}

func (a *app) selectedNativeLineIDs() []int {
	if a.nativeDiffSelection() == nil {
		return nil
	}
	var ids []int
	for i, patch := range a.preview.patches {
		if selected, ok := patch.state.SelectedLines(); ok {
			ids = append(ids, a.nativeRangeIDs(i, selected)...)
		}
	}
	return ids
}

func (a *app) nativeDiffControls(c *ui.Context, compact bool) {
	if a.nativeDiffSelection() == nil {
		return
	}
	selection := a.nativeDiffSelection()
	ref, _ := a.coreReference()
	ids := a.selectedNativeLineIDs()
	label := func(key string) string { return nativeDialogLabel(c.Locale(), "workspace."+key) }
	row := ui.Row(c.Key("native-line-controls")).Gap(8)
	if !compact {
		row.FillWidth().Wrap().Margin(0, 0, 12, 0)
	}
	button := func(c *ui.Context, label, icon string) ui.Element {
		if compact {
			return iconButton(c, label, icon)
		}
		return quietButton(c, label, icon)
	}
	row.Children(func() {
		button(c.Key("include-lines"), label("includeLines"), "check").Disabled(len(ids) == 0 || a.composerLocked() || a.coreInclusionPending != 0).
			OnClick(func() { a.includeCoreLineIDs(ids, true, ref, selection) })
		button(c.Key("exclude-lines"), label("excludeLines"), "x").Disabled(len(ids) == 0 || a.composerLocked() || a.coreInclusionPending != 0).
			OnClick(func() { a.includeCoreLineIDs(ids, false, ref, selection) })
		if !compact {
			ui.Text(c, label("selectLinesHint")).TextColor(c.Theme().TextMuted).FontSize(c.Theme().Rem(14.0 / 15)).Grow(1).MinWidth(0)
		}
	})
}

func (a *app) configureNativeDiffSelection(c *ui.Context, patchIndex int, state *editor.DiffState) {
	selection := a.nativeDiffSelection()
	state.LineSelection = selection != nil && !a.composerLocked()
	state.OnGutterButton, state.RenderGutterButton = nil, nil
	if selection == nil || !selection.ShowCheckMarks {
		return
	}
	selectedIDs := func(ids []int) bool {
		if len(ids) == 0 {
			return false
		}
		for _, id := range ids {
			found := false
			for _, line := range a.nativeDiffSelection().Lines {
				if line.ID == id && line.Selected {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	ref, _ := a.coreReference()
	state.OnGutterButton = func(r editor.DiffLineRange) {
		ids := a.nativeRangeIDs(patchIndex, r)
		a.includeCoreLineIDs(ids, !selectedIDs(ids), ref, selection)
	}
	state.RenderGutterButton = func(c *ui.Context, button ui.Element, r editor.DiffLineRange) {
		ids := a.nativeRangeIDs(patchIndex, r)
		icon, key := "plus", "workspace.includeLines"
		if selectedIDs(ids) {
			icon, key = "check", "workspace.excludeLines"
		}
		label := nativeDialogLabel(c.Locale(), key)
		button.Background(c.Theme().Surface).Radius(4).Label(label).Tooltip(label).
			Disabled(len(ids) == 0 || a.composerLocked() || a.coreInclusionPending != 0).
			Children(func() { ui.Icon(c, icons[icon]).FontSize(14) })
	}
}

func (a *app) fileCheckState(path string) ui.CheckState {
	if a.coreSnapshot != nil && a.coreSnapshot.Selected != nil {
		for _, file := range a.coreSnapshot.Selected.Files {
			if file.Path == path && file.Inclusion == "partial" {
				return ui.CheckMixed
			}
		}
	}
	if a.included[path] {
		return ui.CheckOn
	}
	return ui.CheckOff
}

func (a *app) allFileCheckState() ui.CheckState {
	if a.status == nil || len(a.status.Files) == 0 {
		return ui.CheckOff
	}
	all, none := true, true
	for _, file := range a.status.Files {
		state := a.fileCheckState(file.Path)
		all, none = all && state == ui.CheckOn, none && state == ui.CheckOff
	}
	if all {
		return ui.CheckOn
	}
	if none {
		return ui.CheckOff
	}
	return ui.CheckMixed
}
