package backend

import (
	"context"
	"errors"
	"fmt"
	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
	"path/filepath"
	"strings"
	"sync"
)

type nativeMetadata struct {
	refs                              []gitcli.Reference
	worktrees, stash, remotes, author []byte
	authorErr                         error
}

// These reads have no ordering dependency. Start them together once per real
// metadata change; the state owner applies all results only after they finish.
func (n *NativeOwner) readMetadata(ctx context.Context) (nativeMetadata, error) {
	var result nativeMetadata
	errs := make([]error, 4)
	var group sync.WaitGroup
	group.Go(func() { result.refs, errs[0] = n.current.References(ctx) })
	group.Go(func() { result.worktrees, errs[1] = n.current.Run(ctx, nil, "worktree", "list", "--porcelain", "-z") })
	group.Go(func() {
		result.stash, errs[2] = n.current.Run(ctx, nil, "stash", "list", "--format=%H%x00%gs", "-n", "1")
	})
	group.Go(func() { result.remotes, errs[3] = n.current.Run(ctx, nil, "remote") })
	group.Go(func() { result.author, result.authorErr = n.current.Run(ctx, nil, "var", "GIT_AUTHOR_IDENT") })
	group.Wait()
	return result, errors.Join(errs...)
}

func (n *NativeOwner) refreshWorkspaceMetadata(data, stash []byte) error {
	workspace := n.state.Workspace
	workspace.Worktrees = []KernelWorkspaceWorktree{}
	var current *KernelWorkspaceWorktree
	for _, record := range strings.Split(string(data), "\x00") {
		key, value, _ := strings.Cut(record, " ")
		switch key {
		case "worktree":
			workspace.Worktrees = append(workspace.Worktrees, KernelWorkspaceWorktree{Path: filepath.Clean(value), Name: filepath.Base(value), Main: len(workspace.Worktrees) == 0, Current: filepath.Clean(value) == n.current.Path})
			current = &workspace.Worktrees[len(workspace.Worktrees)-1]
		case "branch":
			if current != nil {
				current.Description = strings.TrimPrefix(value, "refs/heads/")
			}
		case "locked":
			if current != nil {
				current.Locked = true
			}
		case "prunable":
			if current != nil {
				current.Missing = true
			}
		}
	}
	if len(stash) == 0 {
		workspace.Stash = nil
	} else {
		fields := strings.SplitN(strings.TrimSpace(string(stash)), "\x00", 2)
		if len(fields) != 2 {
			return errors.New("invalid stash record")
		}
		selected := workspace.Stash != nil && workspace.Stash.SHA == fields[0] && workspace.Stash.Selected
		workspace.Stash = &KernelWorkspaceStash{SHA: fields[0], Branch: fields[1], LoadState: "NotLoaded", Selected: selected, Files: []KernelFile{}}
	}
	remaining := 0
	for _, file := range n.status.Files {
		if file.Conflict {
			remaining++
		}
	}
	workspace.Conflict = nil
	if n.status.Operation != "" {
		workspace.Conflict = &KernelWorkspaceConflict{Kind: n.status.Operation, Identity: n.status.Fingerprint, Remaining: remaining, CanContinue: remaining == 0, CanAbort: true}
	}
	workspace.Compare.Branches = []KernelWorkspaceBranch{}
	for _, group := range workspace.Branches {
		for _, branch := range group.Items {
			if !branch.Current {
				workspace.Compare.Branches = append(workspace.Compare.Branches, branch)
			}
		}
	}
	workspace.CurrentBranchRef = nil
	if n.status.Branch != "" {
		ref := "refs/heads/" + n.status.Branch
		workspace.CurrentBranchRef = &ref
	}
	return nil
}

func (n *NativeOwner) showStash(ctx context.Context) error {
	s := n.state.Workspace.Stash
	if s == nil {
		return errors.New("no stash is available")
	}
	data, err := n.current.Run(ctx, nil, "stash", "show", "--name-status", "-z", "--include-untracked", s.SHA)
	if err != nil {
		return err
	}
	parts := strings.Split(string(data), "\x00")
	s.Files = []KernelFile{}
	for i := 0; i+1 < len(parts); i += 2 {
		if parts[i] == "" {
			break
		}
		status := map[byte]string{'A': "new", 'D': "deleted", 'M': "modified", 'R': "renamed"}[parts[i][0]]
		if status == "" {
			status = "modified"
		}
		file := KernelFile{ID: parts[i+1], Path: parts[i+1], Status: status}
		if parts[i][0] == 'R' {
			if i+2 >= len(parts) {
				return errors.New("invalid stash rename")
			}
			old := file.Path
			file.OldPath = &old
			file.Path = parts[i+2]
			file.ID = file.Path
			i++
		}
		s.Files = append(s.Files, file)
	}
	s.LoadState = "Loaded"
	s.Selected = true
	return nil
}

func (n *NativeOwner) compare(ctx context.Context, ref, mode string) error {
	if !strings.HasPrefix(ref, "refs/") {
		return errors.New("invalid comparison ref")
	}
	data, err := n.current.Run(ctx, nil, "rev-list", "--left-right", "--count", "HEAD..."+ref)
	if err != nil {
		return err
	}
	var ahead, behind int
	if _, err := fmt.Sscanf(string(data), "%d\t%d", &ahead, &behind); err != nil {
		return err
	}
	workspace := n.state.Workspace
	workspace.Compare.Ref = &ref
	workspace.Compare.Mode = mode
	workspace.Compare.Ahead = ahead
	workspace.Compare.Behind = behind
	if workspace.Compare.Operation == "" {
		workspace.Compare.Operation = "merge"
	}
	workspace.Compare.OperationDisabled = mode != "behind"
	rangeRef := "HEAD.." + ref
	if mode == "ahead" {
		rangeRef = ref + "..HEAD"
	}
	commits, err := n.current.History(ctx, rangeRef, 0, 100)
	if err != nil {
		return err
	}
	n.state.Selected.History = []Commit{}
	for _, c := range commits {
		n.state.Selected.History = append(n.state.Selected.History, Commit{SHA: c.ID, ShortSHA: c.ID[:7], Summary: c.Subject, Body: c.Body, AuthorName: c.AuthorName, AuthorEmail: c.AuthorEmail, Date: c.Date, Tags: []string{}})
	}
	return nil
}
