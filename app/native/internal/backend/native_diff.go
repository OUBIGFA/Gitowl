package backend

import (
	"context"
	"errors"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
)

// A preview owns its text selection and compressed image data together. Every
// path that clears or replaces the visible diff releases that ownership here.
func (n *NativeOwner) clearDiff() {
	if n.state.Selected != nil {
		n.state.Selected.Diff = nil
		n.state.Selected.DiffSelection = nil
	}
	if n.state.Workspace != nil {
		n.state.Workspace.TextDiff = nil
	}
	n.media.clear()
}

func (n *NativeOwner) selectDiff(ctx context.Context, path string, history bool) error {
	n.clearDiff()
	base, commit := "", ""
	if history {
		if n.historyTip == "" || !n.state.Workspace.HistorySelectionContiguous {
			return errors.New("select contiguous commits to view their diff")
		}
		base, commit = n.historyBase, n.historyTip
	}
	oldPath := ""
	files := n.state.Selected.Files
	if history {
		files = n.state.Selected.HistoryFiles
	}
	for _, file := range files {
		if file.Path == path && file.OldPath != nil {
			oldPath = *file.OldPath
			break
		}
	}
	text, err := n.current.DiffFileBetween(ctx, path, oldPath, base, commit, 3, false)
	if err != nil {
		return err
	}
	kind := "text"
	if strings.Contains(text, "Binary files ") {
		kind = "binary"
	}
	if kind == "binary" || strings.HasSuffix(strings.ToLower(path), ".svg") {
		diff, err := n.imageDiff(ctx, path, commit)
		if err != nil {
			return err
		}
		if diff != nil {
			n.state.Selected.Diff = diff
			n.state.Selected.DiffSelection = nil
			return nil
		}
	}
	n.state.Selected.Diff = &Diff{Kind: kind, Text: &text}
	n.media.clear()
	n.state.Workspace.TextDiff = nil
	if !history && kind == "text" {
		n.diffSelection(path, text)
	} else {
		n.state.Selected.DiffSelection = nil
	}
	if kind == "text" {
		identity, _ := gitcli.PatchLines(text)
		n.state.Workspace.TextDiff = &KernelWorkspaceTextDiff{FileID: path, Identity: identity, Prepared: true, CanExpand: true, Selection: n.state.Selected.DiffSelection}
	}
	return nil
}
