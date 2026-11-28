package backend

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
)

func (n *NativeOwner) add(ctx context.Context, path string) (KernelRepository, error) {
	// Reopening an already registered root need not rediscover it once here
	// and again during selection. Selection still verifies Git and its state.
	if absolute, err := filepath.Abs(path); err == nil {
		for _, repository := range n.state.Repositories {
			if filepath.Clean(repository.Path) == filepath.Clean(absolute) {
				return repository, nil
			}
		}
	}
	r, err := n.git.Open(ctx, path)
	if err != nil {
		return KernelRepository{}, err
	}
	id := 1
	for _, existing := range n.state.Repositories {
		if existing.Path == r.Path {
			return existing, nil
		}
		id = max(id, existing.ID+1)
	}
	repository := KernelRepository{KernelRepositoryReference: KernelRepositoryReference{id, r.Path}, Name: filepath.Base(r.Path), Kind: "repository"}
	if err := n.store.SaveRepository(ctx, repository); err != nil {
		return KernelRepository{}, err
	}
	n.state.Repositories = append(n.state.Repositories, repository)
	return repository, nil
}
func textPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func (n *NativeOwner) selectRepository(ctx context.Context, reference KernelRepositoryReference) error {
	if n.current != nil && n.state.Selected != nil && n.state.Selected.Repository.KernelRepositoryReference == reference {
		_, err := n.refresh(ctx, false)
		return err
	}
	n.confirmAction = nil
	if popup := n.state.CurrentPopup; popup != nil && (popup.Type == "NativeConfirm" || popup.Type == "NativeRecovery") {
		n.state.CurrentPopup = nil
		n.state.Popups = []KernelPopup{}
	}
	var selected *KernelRepository
	for i := range n.state.Repositories {
		if n.state.Repositories[i].KernelRepositoryReference == reference {
			selected = &n.state.Repositories[i]
			break
		}
	}
	if selected == nil {
		return errors.New("repository is no longer registered")
	}
	r, err := n.git.Open(ctx, reference.Path)
	if err != nil {
		if n.watchCancel != nil {
			n.watchCancel()
		}
		n.clearDiff()
		n.current = nil
		n.state.Selected = nil
		n.state.Workspace = nil
		message := err.Error()
		n.state.MissingRepository = &KernelMissingRepository{Repository: *selected, Error: &message}
		if strings.Contains(message, "detected dubious ownership") {
			n.state.MissingRepository.UnsafePath = &selected.Path
		}
		return nil
	}
	n.state.MissingRepository = nil
	n.current = r
	n.media.clear()
	n.historyBase, n.historyTip = "", ""
	n.status = gitcli.Status{}
	n.include = make(map[string]bool)
	n.partial = make(map[string]nativePartialSelection)
	n.state.Selected = &KernelSelected{Repository: *selected, Section: "changes", Files: []KernelFile{}, SelectedFileIDs: []string{}, History: []Commit{}, HistorySelection: []string{}, HistoryFiles: []KernelFile{}, CommitView: &KernelCommitView{Mode: "manual", CoAuthorsAvailable: true, ShowSkipHooks: true, Assisted: KernelAssistedState{Kind: "idle"}}}
	n.state.Workspace = &KernelWorkspace{Repository: &reference, Navigation: "", CheckMarks: true, Branches: []KernelWorkspaceBranchGroup{}, Worktrees: []KernelWorkspaceWorktree{}, Menus: []KernelWorkspaceMenu{}, VisibleFileIDs: []string{}, Compare: KernelWorkspaceCompare{Mode: "history", Operation: "merge"}}
	draft, err := n.store.Get(ctx, "draft:"+reference.Path)
	if err != nil {
		return err
	}
	if draft != "" {
		if err := json.Unmarshal([]byte(draft), &n.state.Selected.Message); err != nil {
			return err
		}
	}
	_, err = n.refresh(ctx, true)
	if err == nil {
		err = n.refreshRecovery()
		n.watchRepository()
	}
	return errors.Join(err, n.store.Set(ctx, "last-selected-repository-id", strconv.Itoa(reference.ID)))
}
func fileStatus(file gitcli.File) string {
	if file.Conflict {
		return "conflicted"
	}
	code := file.Index + file.Worktree
	for _, v := range []struct{ letter, name string }{{"?", "new"}, {"R", "renamed"}, {"D", "deleted"}, {"A", "new"}, {"T", "modified"}} {
		if strings.Contains(code, v.letter) {
			return v.name
		}
	}
	return "modified"
}
func (n *NativeOwner) refresh(ctx context.Context, full bool) (changed bool, resultErr error) {
	if n.current == nil {
		return false, nil
	}
	// A failed full refresh may have unchanged file bytes. Keep that work
	// pending so the next watcher/status check can recover without another edit.
	full = full || n.refreshPending
	defer func() { n.refreshPending = resultErr != nil }()
	status, err := n.current.Status(ctx)
	if err != nil {
		return false, err
	}
	changed = status.Fingerprint != n.status.Fingerprint
	if !changed && !full {
		return false, nil
	}
	before := n.status
	// A successful status read is not a successful refresh. Preserve its retry
	// identity until metadata and the visible diff have also been read.
	defer func() {
		if resultErr != nil {
			n.status = before
		}
	}()
	n.status = status
	selected := n.state.Selected
	selected.Branch = textPointer(status.Branch)
	selected.HeadSHA = textPointer(status.Head)
	selected.Upstream = textPointer(status.Upstream)
	selected.Ahead = status.Ahead
	selected.Behind = status.Behind
	selected.ConflictKind = textPointer(status.Operation)
	selected.Files = []KernelFile{}
	present := make(map[string]bool, len(status.Files))
	for _, file := range status.Files {
		present[file.Path] = true
		if _, ok := n.include[file.Path]; !ok {
			n.include[file.Path] = true
		}
		selected.Files = append(selected.Files, KernelFile{ID: file.Path, Path: file.Path, Status: fileStatus(file), OldPath: textPointer(file.OldPath), IsSubmodule: strings.HasPrefix(file.Submodule, "S")})
	}
	// Completed/disappeared changes no longer own an inclusion or line patch.
	for path := range n.include {
		if !present[path] {
			delete(n.include, path)
			delete(n.partial, path)
		}
	}
	n.refreshConflict()
	n.updateInclusions()
	metadataStamp := n.metadataStamp
	if full || before.Head != status.Head {
		stamp, err := n.current.MetadataFingerprint()
		if err != nil {
			return false, err
		}
		metadata, err := n.readMetadata(ctx)
		if err != nil {
			return false, err
		}
		groups := []KernelWorkspaceBranchGroup{{Group: "local", Items: []KernelWorkspaceBranch{}}, {Group: "remote", Items: []KernelWorkspaceBranch{}}}
		n.tags = map[string][]string{}
		for _, ref := range metadata.refs {
			if strings.HasPrefix(ref.Name, "refs/tags/") {
				sha := ref.ID
				if ref.Peeled != "" {
					sha = ref.Peeled
				}
				n.tags[sha] = append(n.tags[sha], strings.TrimPrefix(ref.Name, "refs/tags/"))
				continue
			}
			remote := strings.HasPrefix(ref.Name, "refs/remotes/")
			index := 0
			prefix := "refs/heads/"
			if remote {
				index = 1
				prefix = "refs/remotes/"
			}
			groups[index].Items = append(groups[index].Items, KernelWorkspaceBranch{Ref: ref.Name, Name: strings.TrimPrefix(ref.Name, prefix), Remote: remote, Current: ref.Current, Tip: ref.ID})
		}
		n.state.Workspace.Branches = groups
		if err := n.refreshWorkspaceMetadata(metadata.worktrees, metadata.stash); err != nil {
			return false, err
		}
		selected.HasRemote = len(metadata.remotes) > 0
		selected.CommitView.Author = nil
		if metadata.authorErr == nil {
			identity := string(metadata.author)
			if start := strings.LastIndex(identity, " <"); start >= 0 {
				if end := strings.Index(identity[start+2:], ">"); end >= 0 {
					selected.CommitView.Author = &KernelGitAuthor{Name: identity[:start], Email: identity[start+2 : start+2+end]}
				}
			}
		}
		if selected.Section == "history" {
			if err := n.history(ctx, 0); err != nil {
				return false, err
			}
		}
		metadataStamp = stamp
	}
	if selected.Section == "changes" && len(selected.SelectedFileIDs) > 0 {
		selected.SelectedFileIDs = slices.DeleteFunc(selected.SelectedFileIDs, func(id string) bool { return !present[id] })
		if len(selected.SelectedFileIDs) == 0 {
			n.clearDiff()
		} else if err := n.selectDiff(ctx, selected.SelectedFileIDs[0], false); err != nil {
			return false, err
		}
	}
	n.metadataStamp = metadataStamp
	for i := range n.state.Repositories {
		if n.state.Repositories[i].Path == n.current.Path {
			n.state.Repositories[i].Local = &KernelRepositoryLocal{Ahead: &status.Ahead, Behind: &status.Behind, ChangedFiles: len(status.Files)}
		}
	}
	return changed || full, nil
}
func (n *NativeOwner) updateInclusions() {
	s := n.state.Selected
	if s == nil {
		return
	}
	count := 0
	ids := []string{}
	for i := range s.Files {
		file := &s.Files[i]
		file.Inclusion = "none"
		if n.include[file.Path] {
			file.Inclusion = "all"
			if _, ok := n.partial[file.Path]; ok {
				file.Inclusion = "partial"
			}
			count++
		}
		filter := n.state.Workspace.FilesFilter
		matches := filter.FilterText == "" || strings.Contains(strings.ToLower(file.Path), strings.ToLower(filter.FilterText))
		if filter.Included && !n.include[file.Path] || filter.Excluded && n.include[file.Path] {
			matches = false
		}
		if filter.New || filter.Modified || filter.Deleted {
			matches = matches && (filter.New && file.Status == "new" || filter.Modified && file.Status == "modified" || filter.Deleted && file.Status == "deleted")
		}
		if matches {
			ids = append(ids, file.ID)
		}
	}
	if n.state.Workspace != nil {
		n.state.Workspace.VisibleFileIDs = ids
	}
	s.CommitView.CanCommit = n.status.Operation == "" && (count > 0 || s.CommitView.AllowEmpty || s.CommitView.Amending) && strings.TrimSpace(s.Message.Summary) != ""
	s.CommitView.CanAmend = s.HeadSHA != nil
	s.GenerationAvailable = n.state.CommitGeneration.Source == "custom" || s.Repository.Account != nil
}
func (n *NativeOwner) saveDraft(ctx context.Context) error {
	data, err := json.Marshal(n.state.Selected.Message)
	if err != nil {
		return err
	}
	return n.store.Set(ctx, "draft:"+n.current.Path, string(data))
}
