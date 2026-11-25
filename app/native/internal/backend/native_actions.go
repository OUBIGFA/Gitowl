package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (n *NativeOwner) form(kind, title string, fields []KernelDialogField) {
	n.interactionID++
	id := n.interactionID
	allowed := []string{}
	for _, field := range fields {
		if !field.ReadOnly {
			allowed = append(allowed, field.ID)
		}
	}
	n.state.CurrentPopup = &KernelPopupPresentation{ID: &id, Type: kind, Supported: true, Layout: "form", TitleKey: title, Fields: fields, Actions: []KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true, Fields: allowed}, {ID: "cancel", LabelKey: "settings.cancel"}}}
	n.state.Popups = []KernelPopup{{ID: &id, Type: kind}}
}
func nativeField(id, label, value string) KernelDialogField {
	return KernelDialogField{ID: id, LabelKey: label, Kind: "text", Value: value}
}
func (n *NativeOwner) executeMenu(ctx context.Context, id string) error {
	if n.state.Selected == nil && !slices.Contains([]string{"preferences", "show-repository-list", "add-local-repository", "new-repository", "clone-repository"}, id) {
		return errors.New("no repository selected")
	}
	switch id {
	case "preferences":
		n.preferences("appearance")
	case "recover-commit":
		return n.recoveryDialog()
	case "open-working-directory":
		_, err := n.platform(ctx, "open-path", n.current.Path)
		return err
	case "show-repository-settings":
		return n.remoteForm(ctx)
	case "show-worktrees-list":
		n.state.Workspace.Navigation = "worktrees"
	case "show-filter":
		n.state.Workspace.ShowFilesFilter = !n.state.Workspace.ShowFilesFilter
	case "go-to-commit-message":
		n.state.FocusCommitMessage = true
	case "create-worktree":
		n.form("CreateWorktree", "repositoryDialog.CreateWorktree", []KernelDialogField{nativeField("path", "repositoryDialog.path", ""), nativeField("name", "repositoryDialog.name", "")})
	case "show-stashed-changes":
		return n.showStash(ctx)
	case "show-changes", "show-history":
		section := "changes"
		if id == "show-history" {
			section = "history"
		}
		n.state.Selected.Section = section
		if section == "history" {
			return n.history(ctx, 0)
		}
	case "show-repository-list":
		if n.state.Workspace != nil {
			n.state.Workspace.Navigation = "repositories"
		}
	case "show-branches-list":
		if n.state.Workspace != nil {
			n.state.Workspace.Navigation = "branches"
		}
	case "add-local-repository":
		paths, err := n.chooseDirectory(ctx)
		if err != nil {
			return err
		}
		for _, path := range paths {
			repository, err := n.add(ctx, path)
			if err != nil {
				return err
			}
			if err := n.selectRepository(ctx, repository.KernelRepositoryReference); err != nil {
				return err
			}
		}
	case "new-repository":
		n.form("CreateRepository", "repositoryDialog.CreateRepository", []KernelDialogField{nativeField("path", "repositoryDialog.path", ""), nativeField("name", "repositoryDialog.name", "")})
	case "clone-repository":
		n.cloneForm("", "")
	case "create-branch":
		n.form("CreateBranch", "repositoryDialog.CreateBranch", []KernelDialogField{nativeField("name", "repositoryDialog.name", "")})
	case "merge-branch":
		n.form("MergeBranch", "repositoryDialog.MergeBranch", []KernelDialogField{nativeField("name", "repositoryDialog.name", "")})
	case "fetch", "pull", "push":
		err := n.runRemote(ctx, id)
		if err != nil {
			return err
		}
		_, err = n.refresh(ctx, true)
		return err
	case "refresh":
		_, err := n.refresh(ctx, true)
		return err
	case "stash", "stash-all-changes":
		_, err := n.current.Run(ctx, nil, "stash", "push", "--include-untracked")
		if err != nil {
			return err
		}
		_, err = n.refresh(ctx, true)
		return err
	default:
		return fmt.Errorf("unknown native menu action: %s", id)
	}
	return nil
}

func (n *NativeOwner) replyForm(ctx context.Context, reply KernelPopupReply) error {
	popup := n.state.CurrentPopup
	if popup == nil || popup.ID == nil || reply.PopupID != *popup.ID {
		return errors.New("dialog is no longer active")
	}
	if popup.Type == "NativeConfirm" {
		return n.confirmReply(ctx, reply)
	}
	if reply.Action == "cancel" || reply.Action == "close" {
		n.state.CurrentPopup = nil
		n.state.Popups = []KernelPopup{}
		return nil
	}
	if popup.Type == "Preferences" {
		return n.replyPreferences(ctx, reply)
	}
	if popup.Type == "NativeRecovery" {
		return n.replyRecovery(ctx, reply)
	}
	if reply.Action != "save" {
		return errors.New("unknown native dialog action")
	}
	text := func(key string) string { value, _ := reply.Values[key].(string); return strings.TrimSpace(value) }
	var err error
	switch popup.Type {
	case "NativeRemote":
		name, remote := text("name"), text("url")
		if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\r\n\x00 ") || remote == "" || strings.HasPrefix(remote, "-") {
			return errors.New("enter a remote name and URL")
		}
		existing, readErr := n.current.Run(ctx, nil, "remote")
		if readErr != nil {
			return readErr
		}
		operation := "add"
		if slices.Contains(strings.Fields(string(existing)), name) {
			operation = "set-url"
		}
		_, err = n.current.Run(ctx, nil, "remote", operation, name, remote)
	case "RenameBranch":
		ref, _ := popup.Fields[0].Value.(string)
		name := text("name")
		if _, err = n.current.Run(ctx, nil, "check-ref-format", "--branch", name); err == nil {
			_, err = n.current.Run(ctx, nil, "branch", "-m", "--", strings.TrimPrefix(ref, "refs/heads/"), name)
		}
	case "CreateTag":
		sha, _ := popup.Fields[0].Value.(string)
		name := text("name")
		if _, err = n.current.Run(ctx, nil, "check-ref-format", "refs/tags/"+name); err == nil {
			_, err = n.current.Run(ctx, nil, "tag", "--", name, sha)
		}
	case "DeleteTag":
		name := text("name")
		if !slices.ContainsFunc(popup.Fields[0].Choices, func(c KernelDialogChoice) bool { return c.ID == name }) {
			return errors.New("tag selection changed")
		}
		_, err = n.current.Run(ctx, nil, "tag", "--delete", "--", name)
	case "CreateBranch":
		name := text("name")
		if name == "" {
			return errors.New("branch name is required")
		}
		_, err = n.current.Run(ctx, nil, "check-ref-format", "--branch", name)
		if err == nil {
			err = n.runRemote(ctx, "switch", "-c", name)
		}
	case "MergeBranch":
		name := text("name")
		if name == "" || strings.HasPrefix(name, "-") {
			return errors.New("select a branch to merge")
		}
		err = n.runRemote(context.WithoutCancel(ctx), "merge", "--", name)
	case "CreateWorktree":
		path, name := text("path"), text("name")
		if !filepath.IsAbs(path) || name == "" {
			return errors.New("worktree path and branch name are required")
		}
		if _, err = n.current.Run(ctx, nil, "check-ref-format", "--branch", name); err == nil {
			err = n.runRemote(context.WithoutCancel(ctx), "worktree", "add", "-b", name, "--", path)
		}
	case "NativeConflicts":
		path := text("file")
		if !slices.ContainsFunc(n.status.Files, func(f gitcli.File) bool { return f.Path == path && f.Conflict }) {
			return errors.New("selected conflict is no longer present")
		}
		if _, err = n.current.Run(ctx, nil, "diff", "--check", "--", path); err == nil {
			_, err = n.current.Run(ctx, nil, "add", "--", path)
		}
	case "CreateRepository":
		path := filepath.Join(text("path"), text("name"))
		if !filepath.IsAbs(path) {
			return errors.New("an absolute repository directory is required")
		}
		if entries, e := os.ReadDir(path); e == nil && len(entries) > 0 {
			return errors.New("the selected repository directory is not empty")
		}
		if err = os.MkdirAll(path, 0700); err == nil {
			_, err = n.git.Init(ctx, path)
		}
		if err == nil {
			var repository KernelRepository
			repository, err = n.add(ctx, path)
			if err == nil {
				err = n.selectRepository(ctx, repository.KernelRepositoryReference)
			}
		}
	case "CloneRepository":
		path := text("path")
		remote := text("url")
		if !filepath.IsAbs(path) || remote == "" || strings.HasPrefix(remote, "-") {
			return errors.New("a remote URL and absolute destination are required")
		}
		var identity *KernelAccountIdentity
		identity, err = n.clone(ctx, remote, path, text("branch"), text("account"))
		if err == nil {
			var repository KernelRepository
			repository, err = n.add(ctx, path)
			if err == nil {
				if identity != nil {
					repository.Account = identity
					repository.Login = &identity.Login
					if err = n.store.SaveRepository(ctx, repository); err != nil {
						return err
					}
					for i := range n.state.Repositories {
						if n.state.Repositories[i].ID == repository.ID {
							n.state.Repositories[i] = repository
						}
					}
				}
				err = n.selectRepository(ctx, repository.KernelRepositoryReference)
			}
		}
	default:
		return fmt.Errorf("unknown native dialog: %s", popup.Type)
	}
	if err != nil {
		message := err.Error()
		popup.Error = &message
		return err
	}
	n.state.CurrentPopup = nil
	n.state.Popups = []KernelPopup{}
	_, err = n.refresh(ctx, true)
	return err
}

func (n *NativeOwner) workspace(ctx context.Context, data json.RawMessage) (resultErr error) {
	var header struct{ Type string }
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if header.Type == "menu" {
		return n.workspaceMenuCommand(ctx, data)
	}
	if header.Type == "banner" {
		var a struct {
			ID     int
			Action string
		}
		if err := json.Unmarshal(data, &a); err != nil {
			return err
		}
		if n.state.Workspace == nil || n.state.Workspace.Banner == nil || n.state.Workspace.Banner.ID != a.ID {
			return errors.New("notification changed")
		}
		if a.ID == 1 {
			return n.recoveryDialog()
		}
		if a.ID == 2 {
			if a.Action == "refresh" {
				if _, err := n.refresh(ctx, true); err != nil {
					return err
				}
			}
			n.state.Workspace.Banner = nil
			return nil
		}
		return errors.New("unknown notification")
	}
	var action struct {
		Type, Ref, InitialName, Path, Mode, SHA, FileID, Target, Scope, Text, Operation, Option, Identity string
		SHAs                                                                                              []string
		Visible, Value                                                                                    bool
		Filter                                                                                            KernelWorkspaceFileFilter
	}
	if err := json.Unmarshal(data, &action); err != nil {
		return err
	}
	workspace := n.state.Workspace
	if slices.Contains([]string{"branch.checkout", "stash.create", "stash.restore", "stash.discard", "compare.merge", "conflicts.continue", "conflicts.abort"}, action.Type) {
		defer func() {
			if resultErr != nil {
				_, err := n.refresh(ctx, true)
				resultErr = errors.Join(resultErr, err)
			}
		}()
	}
	switch action.Type {
	case "submodule.open":
		if !slices.ContainsFunc(n.state.Selected.Files, func(f KernelFile) bool { return f.ID == action.FileID && f.IsSubmodule }) {
			return errors.New("submodule selection changed")
		}
		r, err := n.add(ctx, filepath.Join(n.current.Path, action.FileID))
		if err != nil {
			return err
		}
		return n.selectRepository(ctx, r.KernelRepositoryReference)
	case "diff.prepare", "diff.expand", "diff.collapse", "diff.expandHunk", "diff.discard":
		return n.diffAction(ctx, data)
	case "navigation.open", "navigation.close":
		if action.Type == "navigation.open" {
			workspace.Navigation = action.Target
		} else {
			workspace.Navigation = ""
		}
		return nil
	case "navigation.filter":
		switch action.Scope {
		case "branches":
			workspace.Filters.Branches = action.Text
		case "worktrees":
			workspace.Filters.Worktrees = action.Text
		case "compare":
			workspace.Filters.Compare = action.Text
		default:
			return errors.New("unknown navigation filter")
		}
		return nil
	case "files.filter":
		workspace.FilesFilter = action.Filter
		return nil
	case "branch.create":
		n.form("CreateBranch", "repositoryDialog.CreateBranch", []KernelDialogField{nativeField("name", "repositoryDialog.name", action.InitialName)})
		return nil
	case "branch.checkout":
		if !strings.HasPrefix(action.Ref, "refs/heads/") && !strings.HasPrefix(action.Ref, "refs/remotes/") {
			return errors.New("invalid branch ref")
		}
		name := strings.TrimPrefix(action.Ref, "refs/heads/")
		if strings.HasPrefix(action.Ref, "refs/remotes/") {
			name = strings.TrimPrefix(action.Ref, "refs/remotes/")
			if err := n.runRemote(context.WithoutCancel(ctx), "switch", "--track", name); err != nil {
				return err
			}
		} else {
			if err := n.runRemote(context.WithoutCancel(ctx), "switch", "--", name); err != nil {
				return err
			}
		}
	case "worktree.select":
		repository, err := n.add(ctx, action.Path)
		if err != nil {
			return err
		}
		return n.selectRepository(ctx, repository.KernelRepositoryReference)
	case "worktree.create":
		n.form("CreateWorktree", "repositoryDialog.CreateWorktree", []KernelDialogField{nativeField("path", "repositoryDialog.path", ""), nativeField("name", "repositoryDialog.name", "")})
		return nil
	case "stash.create":
		if _, err := n.current.Run(ctx, nil, "stash", "push", "--include-untracked"); err != nil {
			return err
		}
	case "stash.restore":
		if len(action.SHA) != 40 && len(action.SHA) != 64 {
			return errors.New("invalid stash object")
		}
		if err := n.runRemote(context.WithoutCancel(ctx), "stash", "apply", "--index", action.SHA); err != nil {
			return err
		}
	case "stash.view":
		return n.showStash(ctx)
	case "stash.discard":
		if workspace.Stash == nil || workspace.Stash.SHA != action.SHA {
			return errors.New("stash changed")
		}
		sha := action.SHA
		n.confirm("workspace.discardStash", workspace.Stash.Branch, func(ctx context.Context) error {
			data, err := n.current.Run(ctx, nil, "stash", "list", "--format=%gd%x00%H")
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(data), "\n") {
				ref, id, ok := strings.Cut(line, "\x00")
				if ok && id == sha {
					_, err := n.current.Run(ctx, nil, "stash", "drop", ref)
					return err
				}
			}
			return errors.New("stash no longer exists")
		})
		return nil
	case "stash.hide":
		if workspace.Stash != nil {
			workspace.Stash.Selected = false
		}
		return nil
	case "stash.file":
		if workspace.Stash == nil || workspace.Stash.SHA != action.SHA {
			return errors.New("stash changed")
		}
		text, err := n.current.Run(ctx, nil, "diff", action.SHA+"^", action.SHA, "--", action.FileID)
		if err != nil {
			return err
		}
		value := string(text)
		workspace.Stash.FileID = &action.FileID
		n.state.Selected.Diff = &Diff{Kind: "text", Text: &value}
		return nil
	case "history.select":
		n.state.Selected.HistorySelection = action.SHAs
		return n.historyFiles(ctx)
	case "compare.list":
		workspace.Compare.ShowBranchList = action.Visible
		return nil
	case "compare.operation":
		workspace.Compare.Operation = action.Operation
		return nil
	case "compare.history":
		workspace.Compare.Ref = nil
		workspace.Compare.Mode = "history"
		return n.history(ctx, 0)
	case "compare.branch":
		if !strings.HasPrefix(action.Ref, "refs/") {
			return errors.New("invalid compare ref")
		}
		return n.compare(ctx, action.Ref, action.Mode)
	case "compare.merge":
		if workspace.Compare.Ref == nil {
			return errors.New("no branch selected for merge")
		}
		operation := workspace.Compare.Operation
		if operation == "" {
			operation = "merge"
		}
		if operation != "merge" && operation != "rebase" {
			return errors.New("unknown comparison operation")
		}
		if err := n.runRemote(context.WithoutCancel(ctx), operation, "--", *workspace.Compare.Ref); err != nil {
			return err
		}
	case "conflicts.continue":
		if action.Identity != n.status.Fingerprint {
			return errors.New("conflict state changed; review it again")
		}
		if n.status.Operation == "" {
			return errors.New("there is no operation to continue")
		}
		if slices.ContainsFunc(n.status.Files, func(f gitcli.File) bool { return f.Conflict }) {
			return errors.New("resolve and mark every conflicted file before continuing")
		}
		if _, err := n.current.Run(context.WithoutCancel(ctx), nil, "-c", "core.editor=true", n.status.Operation, "--continue"); err != nil {
			return err
		}
	case "conflicts.abort":
		if action.Identity != n.status.Fingerprint {
			return errors.New("conflict state changed; review it again")
		}
		if n.status.Operation == "" {
			return errors.New("there is no operation to abort")
		}
		if _, err := n.current.Run(ctx, nil, n.status.Operation, "--abort"); err != nil {
			return err
		}
	case "conflicts.open":
		choices := []KernelDialogChoice{}
		for _, file := range n.status.Files {
			if file.Conflict {
				choices = append(choices, KernelDialogChoice{ID: file.Path, Label: file.Path})
			}
		}
		if len(choices) == 0 {
			return errors.New("no unresolved files remain")
		}
		n.form("NativeConflicts", "native.resolveConflicts", []KernelDialogField{{ID: "instructions", Kind: "readonly", ReadOnly: true, LabelKey: "native.resolveConflicts", Text: ""}, {ID: "file", Kind: "select", LabelKey: "native.resolvedFile", Value: choices[0].ID, Choices: choices}})
		n.state.CurrentPopup.DescriptionKey = "native.resolveInstructions"
		return nil
	case "diff.option":
		switch action.Option {
		case "hideWhitespace":
			workspace.HideWhitespace = action.Value
			if workspace.TextDiff != nil {
				payload, _ := json.Marshal(map[string]string{"type": "diff.expand", "fileID": workspace.TextDiff.FileID, "identity": workspace.TextDiff.Identity})
				return n.diffAction(ctx, payload)
			}
		case "sideBySide":
			workspace.SideBySide = action.Value
		case "checkMarks":
			workspace.CheckMarks = action.Value
			if n.state.Selected.DiffSelection != nil {
				n.state.Selected.DiffSelection.ShowCheckMarks = action.Value
			}
		default:
			return errors.New("unknown diff option")
		}
		return nil
	default:
		return fmt.Errorf("unknown native workspace action: %s", action.Type)
	}
	_, err := n.refresh(ctx, true)
	return err
}
