package backend

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"

	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (n *NativeOwner) diffAction(ctx context.Context, data json.RawMessage) error {
	var a struct {
		Type, FileID, Identity string
		LineIDs                []int
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	w := n.state.Workspace.TextDiff
	if w == nil || w.FileID != a.FileID || w.Identity != a.Identity {
		return errors.New("diff changed; review it again")
	}
	if a.Type == "diff.prepare" {
		return nil
	}
	if a.Type == "diff.collapse" {
		w.ExpandedText = nil
		w.Selection = n.state.Selected.DiffSelection
		w.CanExpand = true
		return nil
	}
	old, commit := "", ""
	files := n.state.Selected.Files
	if n.state.Selected.Section == "history" {
		files = n.state.Selected.HistoryFiles
		if len(n.state.Selected.HistorySelection) > 0 {
			commit = n.state.Selected.HistorySelection[0]
		}
	}
	for _, file := range files {
		if file.ID == a.FileID && file.OldPath != nil {
			old = *file.OldPath
		}
	}
	current, err := n.current.DiffFile(ctx, a.FileID, old, commit, 3, false)
	if err != nil {
		return err
	}
	identity, originalLines := gitcli.PatchLines(current)
	if identity != a.Identity {
		return errors.New("file changed; refresh its diff")
	}
	if a.Type == "diff.discard" {
		if commit != "" || len(a.LineIDs) == 0 {
			return errors.New("select working file changes to discard")
		}
		selected := map[int]bool{}
		for _, id := range a.LineIDs {
			valid := false
			for _, line := range originalLines {
				if line.ID == id {
					valid = true
					break
				}
			}
			if !valid {
				return errors.New("line selection changed")
			}
			selected[id] = true
		}
		patch, err := gitcli.DiscardPatch(current, selected)
		if err != nil {
			return err
		}
		n.confirm("workspace.discardLines", a.FileID, func(ctx context.Context) error {
			latest, err := n.current.DiffFile(ctx, a.FileID, old, "", 3, false)
			if err != nil {
				return err
			}
			if latest != current {
				return errors.New("file changed since confirmation")
			}
			if _, err = n.current.Run(ctx, strings.NewReader(patch), "apply", "--check", "--unidiff-zero", "--whitespace=nowarn", "-"); err != nil {
				return err
			}
			// Recycle a byte-for-byte backup before touching the working file.
			original, err := readPreviewFile(filepath.Join(n.current.Path, a.FileID), 64<<20)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil {
				backup, err := os.CreateTemp(n.current.GitDir, "gitowl-discard-*")
				if err != nil {
					return err
				}
				_, writeErr := backup.Write(original)
				closeErr := backup.Close()
				if err = errors.Join(writeErr, closeErr); err != nil {
					return err
				}
				if err = n.recycle(backup.Name()); err != nil {
					return err
				}
			}
			_, err = n.current.Run(ctx, strings.NewReader(patch), "apply", "--unidiff-zero", "--whitespace=nowarn", "-")
			return err
		})
		return nil
	}
	expanded, err := n.current.DiffFile(ctx, a.FileID, old, commit, 1000000, n.state.Workspace.HideWhitespace)
	if err != nil {
		return err
	}
	w.ExpandedText = &expanded
	w.CanExpand = false
	if n.state.Selected.DiffSelection != nil {
		base := n.state.Selected.DiffSelection
		selection := &KernelDiffSelection{Identity: base.Identity, FileID: base.FileID, ShowCheckMarks: base.ShowCheckMarks, Lines: []KernelSelectableLine{}}
		_, expandedLines := gitcli.PatchLines(expanded)
		positions := map[string]gitcli.SelectableLine{}
		key := func(line gitcli.SelectableLine) string {
			if line.Old != nil {
				return "old:" + strconv.Itoa(*line.Old)
			}
			return "new:" + strconv.Itoa(*line.New)
		}
		for _, line := range expandedLines {
			positions[key(line)] = line
		}
		for i, line := range originalLines {
			if expandedLine, ok := positions[key(line)]; ok {
				value := base.Lines[i]
				value.OldBufferLine = expandedLine.OldBuffer
				value.NewBufferLine = expandedLine.NewBuffer
				selection.Lines = append(selection.Lines, value)
			}
		}
		w.Selection = selection
	}
	return nil
}
