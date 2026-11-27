package backend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"

	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func (n *NativeOwner) workspaceMenus() {
	n.state.RepositoryMenus = []KernelRepositoryMenu{}
	for _, r := range n.state.Repositories {
		digest := sha256.Sum256([]byte(r.Path + "\x00" + strconv.Itoa(r.ID)))
		n.state.RepositoryMenus = append(n.state.RepositoryMenus, KernelRepositoryMenu{Repository: r.KernelRepositoryReference, Lease: fmt.Sprintf("%x", digest[:]), Items: []KernelWorkspaceMenuItem{{LabelKey: "workspace.copyRepoPath", Enabled: true}, {LabelKey: "native.revealFile", Enabled: true}, {LabelKey: "workspace.removeRepository", Enabled: true}}})
	}
	w, s := n.state.Workspace, n.state.Selected
	if w == nil || s == nil {
		return
	}
	w.Menus = []KernelWorkspaceMenu{}
	add := func(kind, id string, labels ...string) {
		digest := sha256.Sum256([]byte(s.Repository.Path + "\x00" + n.status.Fingerprint + "\x00" + kind + "\x00" + id + "\x00" + strings.Join(labels, "\x00")))
		menu := KernelWorkspaceMenu{Target: KernelWorkspaceMenuTarget{kind, id}, Lease: fmt.Sprintf("%x", digest[:])}
		for _, label := range labels {
			menu.Items = append(menu.Items, KernelWorkspaceMenuItem{LabelKey: label, Enabled: true})
		}
		w.Menus = append(w.Menus, menu)
	}
	for _, file := range s.Files {
		add("file", file.ID, "workspace.copyPath", "workspace.copyRelativePath", "workspace.openDefault", "native.revealFile", "workspace.discard", "workspace.ignoreFile")
	}
	for _, file := range s.HistoryFiles {
		add("historyFile", file.ID, "workspace.copyRelativePath")
	}
	for _, commit := range s.History {
		add("commit", commit.SHA, "workspace.copySHA", "workspace.createTag", "workspace.deleteTag", "workspace.revert", "workspace.cherryPick")
	}
	for _, group := range w.Branches {
		for _, branch := range group.Items {
			labels := []string{"workspace.copyBranch"}
			if !branch.Remote {
				labels = append(labels, "workspace.rename")
				if !branch.Current {
					labels = append(labels, "native.deleteBranch")
				}
			}
			add("branch", branch.Ref, labels...)
		}
	}
	for _, worktree := range w.Worktrees {
		add("worktree", worktree.Path, "workspace.copyWorktreePath", "native.revealFile")
	}
	if len(s.Files) > 0 {
		add("changes", "", "workspace.discardAll")
	}
}

func (n *NativeOwner) repositoryMenuCommand(ctx context.Context, c nativeCommand) error {
	var menu *KernelRepositoryMenu
	for i := range n.state.RepositoryMenus {
		item := &n.state.RepositoryMenus[i]
		if item.Repository == c.Repository && item.Lease == c.Lease {
			menu = item
			break
		}
	}
	if menu == nil || len(c.Path) != 1 || c.Path[0] < 0 || c.Path[0] >= len(menu.Items) {
		return errors.New("repository menu expired")
	}
	switch c.Path[0] {
	case 0:
		_, err := n.platform(ctx, "write-clipboard-text", c.Repository.Path)
		return err
	case 1:
		_, err := n.platform(ctx, "show-item-in-folder", c.Repository.Path)
		return err
	case 2:
		n.confirm("workspace.removeRepositoryConfirm", c.Repository.Path, func(ctx context.Context) error {
			if err := n.store.RemoveRepository(ctx, c.Repository.ID); err != nil {
				return err
			}
			n.state.Repositories = slices.DeleteFunc(n.state.Repositories, func(r KernelRepository) bool { return r.KernelRepositoryReference == c.Repository })
			if n.state.Selected != nil && n.state.Selected.Repository.KernelRepositoryReference == c.Repository {
				if n.watchCancel != nil {
					n.watchCancel()
				}
				n.current = nil
				n.state.Selected = nil
				n.state.Workspace = nil
			}
			return nil
		})
	}
	return nil
}

func (n *NativeOwner) workspaceMenuCommand(ctx context.Context, data json.RawMessage) error {
	var a struct {
		Target KernelWorkspaceMenuTarget
		Lease  string
		Path   []int
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if n.state.Workspace == nil {
		return errors.New("repository selection changed")
	}
	var menu *KernelWorkspaceMenu
	for i := range n.state.Workspace.Menus {
		item := &n.state.Workspace.Menus[i]
		if item.Target == a.Target && item.Lease == a.Lease {
			menu = item
			break
		}
	}
	if menu == nil || len(a.Path) != 1 || a.Path[0] < 0 || a.Path[0] >= len(menu.Items) {
		return errors.New("menu expired; open it again")
	}
	key, id := menu.Items[a.Path[0]].LabelKey, a.Target.ID
	switch key {
	case "workspace.copyRelativePath", "workspace.copySHA", "workspace.copyBranch", "workspace.copyWorktreePath":
		_, err := n.platform(ctx, "write-clipboard-text", id)
		return err
	case "workspace.copyPath":
		_, err := n.platform(ctx, "write-clipboard-text", filepath.Join(n.current.Path, id))
		return err
	case "workspace.openDefault", "native.revealFile":
		path := id
		if a.Target.Kind != "worktree" {
			path = filepath.Join(n.current.Path, id)
		}
		operation := "show-item-in-folder"
		if key == "workspace.openDefault" {
			operation = "open-path"
		}
		_, err := n.platform(ctx, operation, path)
		return err
	case "workspace.discard", "workspace.discardAll":
		paths := []string{id}
		if key == "workspace.discardAll" {
			paths = nil
			for _, file := range n.state.Selected.Files {
				paths = append(paths, file.Path)
			}
		}
		identity := n.status.Fingerprint
		n.confirm("workspace.discardConfirm", strings.Join(paths, "\n"), func(ctx context.Context) error { return n.discardFiles(ctx, identity, paths) })
	case "workspace.ignoreFile":
		if strings.ContainsAny(id, "\r\n") {
			return errors.New("this filename cannot be represented as an ignore rule")
		}
		name := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "?", "\\?", " ", "\\ ").Replace(id)
		file, err := os.OpenFile(filepath.Join(n.current.Path, ".gitignore"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = file.WriteString("\n/" + name + "\n")
		err = errors.Join(err, file.Close())
		if err != nil {
			return err
		}
		_, err = n.refresh(ctx, true)
		return err
	case "workspace.rename":
		n.form("RenameBranch", "menu.rename-branch", []KernelDialogField{{ID: "ref", Kind: "readonly", ReadOnly: true, Value: id, Text: id}, nativeField("name", "repositoryDialog.name", strings.TrimPrefix(id, "refs/heads/"))})
	case "native.deleteBranch":
		n.confirm("menu.delete-branch", id, func(ctx context.Context) error {
			_, err := n.current.Run(ctx, nil, "branch", "-d", "--", strings.TrimPrefix(id, "refs/heads/"))
			return err
		})
	case "workspace.createTag":
		n.form("CreateTag", "workspace.createTag", []KernelDialogField{{ID: "sha", Kind: "readonly", ReadOnly: true, Value: id, Text: id}, nativeField("name", "repositoryDialog.name", "")})
	case "workspace.deleteTag":
		data, err := n.current.Run(ctx, nil, "tag", "--points-at", id)
		if err != nil {
			return err
		}
		choices := []KernelDialogChoice{}
		for _, tag := range strings.Fields(string(data)) {
			choices = append(choices, KernelDialogChoice{ID: tag, Label: tag})
		}
		if len(choices) == 0 {
			return errors.New("this commit has no tags")
		}
		n.form("DeleteTag", "workspace.deleteTag", []KernelDialogField{{ID: "name", LabelKey: "repositoryDialog.name", Kind: "select", Value: choices[0].ID, Choices: choices}})
		n.state.CurrentPopup.Actions[0].Destructive = true
	case "workspace.revert", "workspace.cherryPick":
		n.confirm(key, id, func(ctx context.Context) error {
			status, err := n.current.Status(ctx)
			if err != nil {
				return err
			}
			if len(status.Files) > 0 || status.Operation != "" {
				return errors.New("commit or stash local changes before applying this commit")
			}
			operation := "revert"
			if key == "workspace.cherryPick" {
				operation = "cherry-pick"
			}
			_, err = n.current.Run(context.WithoutCancel(ctx), nil, operation, "--no-edit", id)
			_, refreshErr := n.refresh(ctx, true)
			return errors.Join(err, refreshErr)
		})
	default:
		return errors.New("unknown context menu action")
	}
	return nil
}

func (n *NativeOwner) discardFiles(ctx context.Context, identity string, paths []string) error {
	status, err := n.current.Status(ctx)
	if err != nil {
		return err
	}
	if status.Fingerprint != identity {
		return errors.New("files changed since confirmation; review them again")
	}
	if status.Operation != "" {
		return errors.New("finish or abort the current Git operation before discarding files")
	}
	for _, path := range paths {
		index := slices.IndexFunc(status.Files, func(file gitcli.File) bool { return file.Path == path })
		if index < 0 {
			return errors.New("file is no longer changed")
		}
		file := status.Files[index]
		if strings.HasPrefix(file.Submodule, "S") {
			return errors.New("open the submodule to manage its local changes")
		}
		// Recycle the current file before Git restores any tracked content.
		absolute := filepath.Join(n.current.Path, path)
		if _, err := os.Lstat(absolute); err == nil {
			if err = n.recycle(absolute); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		tracked := []string{path}
		if file.OldPath != "" {
			tracked = append(tracked, file.OldPath)
		}
		if file.Index != "?" {
			if status.Head != "" {
				if _, err = n.current.Run(ctx, nil, append([]string{"reset", "--quiet", status.Head, "--"}, tracked...)...); err != nil {
					return err
				}
				// A newly staged file has no HEAD entry to restore.
				for _, candidate := range tracked {
					entries, e := n.current.Run(ctx, nil, "ls-tree", "--name-only", "-z", status.Head, "--", candidate)
					if e != nil {
						return e
					}
					if len(entries) > 0 {
						// Restore both copies together so Git records the checked-out
						// size after filters/CRLF conversion. Worktree-only restore
						// leaves a false modification under GIT_OPTIONAL_LOCKS=0.
						if _, e = n.current.Run(ctx, nil, "restore", "--source="+status.Head, "--staged", "--worktree", "--", candidate); e != nil {
							return e
						}
					}
				}
			} else {
				if _, err = n.current.Run(ctx, nil, append([]string{"rm", "--cached", "--force", "--"}, tracked...)...); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
