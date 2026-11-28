package backend

import (
	"context"
	"slices"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
)

func (n *NativeOwner) history(ctx context.Context, skip int) error {
	if n.status.Head == "" {
		n.state.Selected.History = []Commit{}
		return nil
	}
	ref := n.status.Head
	if compare := n.state.Workspace.Compare; compare.Ref != nil && compare.Mode != "history" {
		if compare.Mode == "ahead" {
			ref = *compare.Ref + ".." + n.status.Head
		} else {
			ref = n.status.Head + ".." + *compare.Ref
		}
	}
	commits, err := n.current.History(ctx, ref, skip, 100)
	if err != nil {
		return err
	}
	if skip == 0 {
		n.state.Selected.History = []Commit{}
	}
	for _, c := range commits {
		n.state.Selected.History = append(n.state.Selected.History, Commit{SHA: c.ID, ShortSHA: c.ID[:7], Summary: c.Subject, Body: c.Body, AuthorName: c.AuthorName, AuthorEmail: c.AuthorEmail, Date: c.Date, Tags: append([]string{}, n.tags[c.ID]...)})
	}
	return nil
}
func (n *NativeOwner) historyFiles(ctx context.Context) error {
	s := n.state.Selected
	s.HistoryFiles = []KernelFile{}
	s.HistoryFileID = nil
	n.clearDiff()
	s.HistoryLinesAdded, s.HistoryLinesDeleted = 0, 0
	n.state.Workspace.HistorySelectionContiguous = true
	n.historyBase, n.historyTip = "", ""
	if len(s.HistorySelection) == 0 {
		return nil
	}
	if len(s.HistorySelection) > 1 {
		// Native selection order can follow Ctrl-click order. Compare the
		// oldest selected commit's first parent with the newest selected tree.
		ordered := []string{}
		first, last := -1, -1
		for i, commit := range s.History {
			if slices.Contains(s.HistorySelection, commit.SHA) {
				if first < 0 {
					first = i
				}
				last = i
				ordered = append(ordered, commit.SHA)
			}
		}
		if len(ordered) != len(s.HistorySelection) || last-first+1 != len(ordered) {
			n.state.Workspace.HistorySelectionContiguous = false
			return nil
		}
		s.HistorySelection = ordered
		parents, err := n.current.Run(ctx, nil, "rev-list", "--parents", "-n", "1", ordered[len(ordered)-1], "--")
		if err != nil {
			return err
		}
		if ids := strings.Fields(string(parents)); len(ids) > 1 {
			n.historyBase = ids[1]
		} else {
			// Ask this repository's Git for the empty tree's object-format ID.
			empty, err := n.current.Run(ctx, strings.NewReader(""), "hash-object", "-t", "tree", "--stdin")
			if err != nil {
				return err
			}
			n.historyBase = strings.TrimSpace(string(empty))
		}
	}
	n.historyTip = s.HistorySelection[0]
	changes, err := n.current.ChangesBetween(ctx, n.historyBase, n.historyTip)
	if err != nil {
		return err
	}
	for _, file := range changes {
		s.HistoryFiles = append(s.HistoryFiles, KernelFile{ID: file.Path, Path: file.Path, OldPath: textPointer(file.OldPath), Status: fileStatus(gitcli.File{Index: file.Status})})
	}
	_, s.HistoryLinesAdded, s.HistoryLinesDeleted, err = n.current.SummaryBetween(ctx, n.historyBase, n.historyTip)
	return err
}
