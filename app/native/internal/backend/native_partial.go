package backend

import (
	"errors"
	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
)

type nativePartialSelection struct {
	identity, patch string
	lines           map[int]bool
}

func (n *NativeOwner) diffSelection(path, patch string) {
	identity, lines := gitcli.PatchLines(patch)
	partial, has := n.partial[path]
	if has && partial.identity != identity {
		delete(n.partial, path)
		n.include[path] = false
		has = false
	}
	view := &KernelDiffSelection{Identity: identity, FileID: path, ShowCheckMarks: n.state.Workspace.CheckMarks, Lines: []KernelSelectableLine{}}
	for _, line := range lines {
		selected := n.include[path]
		if has {
			selected = partial.lines[line.ID]
		}
		view.Lines = append(view.Lines, KernelSelectableLine{ID: line.ID, Kind: line.Kind, OldBufferLine: line.OldBuffer, NewBufferLine: line.NewBuffer, Selected: selected})
	}
	n.state.Selected.DiffSelection = view
}
func (n *NativeOwner) includeLines(command nativeCommand) error {
	view := n.state.Selected.DiffSelection
	diff := n.state.Selected.Diff
	if view == nil || diff == nil || diff.Text == nil || command.Identity != view.Identity || command.FileID != view.FileID || len(command.LineIDs) == 0 {
		return errors.New("the diff changed; review the current file before selecting lines")
	}
	lines := make(map[int]bool)
	for _, line := range view.Lines {
		lines[line.ID] = line.Selected
	}
	for _, id := range command.LineIDs {
		if _, ok := lines[id]; !ok {
			return errors.New("line is not selectable")
		}
		lines[id] = command.Included
	}
	count := 0
	for _, included := range lines {
		if included {
			count++
		}
	}
	n.include[view.FileID] = count > 0
	if count == 0 || count == len(lines) {
		delete(n.partial, view.FileID)
	} else {
		n.partial[view.FileID] = nativePartialSelection{view.Identity, *diff.Text, lines}
	}
	n.diffSelection(view.FileID, *diff.Text)
	if w := n.state.Workspace.TextDiff; w != nil {
		if w.ExpandedText == nil {
			w.Selection = n.state.Selected.DiffSelection
		} else if w.Selection != nil {
			for i := range w.Selection.Lines {
				w.Selection.Lines[i].Selected = lines[w.Selection.Lines[i].ID]
			}
		}
	}
	return nil
}
