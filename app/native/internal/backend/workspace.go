package backend

import (
	"context"
	"errors"
)

// KernelWorkspace contains only whitelisted Desktop presentation state. Git,
// credentials, callback continuations, and worktree ownership stay in Desktop.
type KernelWorkspace struct {
	Repository                 *KernelRepositoryReference       `json:"repository"`
	Navigation                 string                           `json:"navigation"`
	CurrentBranchRef           *string                          `json:"currentBranchRef"`
	Filters                    KernelWorkspaceNavigationFilters `json:"filters"`
	Locked                     bool                             `json:"locked"`
	CheckoutInProgress         bool                             `json:"checkoutInProgress"`
	Branches                   []KernelWorkspaceBranchGroup     `json:"branches"`
	PullRequests               []KernelWorkspacePullRequest     `json:"pullRequests"`
	PullRequestsAvailable      bool                             `json:"pullRequestsAvailable"`
	LoadingPullRequests        bool                             `json:"loadingPullRequests"`
	Worktrees                  []KernelWorkspaceWorktree        `json:"worktrees"`
	Compare                    KernelWorkspaceCompare           `json:"compare"`
	HistorySelectionContiguous bool                             `json:"historySelectionContiguous"`
	LocalCommitSHAs            []string                         `json:"localCommitSHAs"`
	TagsToPush                 []string                         `json:"tagsToPush"`
	ReorderSelection           []string                         `json:"reorderSelection"`
	Stash                      *KernelWorkspaceStash            `json:"stash"`
	FilesFilter                KernelWorkspaceFileFilter        `json:"filesFilter"`
	ShowFilesFilter            bool                             `json:"showFilesFilter"`
	VisibleFileIDs             []string                         `json:"visibleFileIDs"`
	Menus                      []KernelWorkspaceMenu            `json:"menus"`
	Banner                     *KernelWorkspaceBanner           `json:"banner"`
	Conflict                   *KernelWorkspaceConflict         `json:"conflict"`
	TextDiff                   *KernelWorkspaceTextDiff         `json:"textDiff"`
	HideWhitespace             bool                             `json:"hideWhitespace"`
	SideBySide                 bool                             `json:"sideBySide"`
	CheckMarks                 bool                             `json:"checkMarks"`
}

type KernelWorkspaceNavigationFilters struct {
	Branches     string `json:"branches"`
	PullRequests string `json:"pullRequests"`
	Worktrees    string `json:"worktrees"`
	Compare      string `json:"compare"`
}
type KernelWorkspaceConflict struct {
	Kind              string `json:"kind"`
	Identity          string `json:"identity"`
	Remaining         int    `json:"remaining"`
	CanContinue       bool   `json:"canContinue"`
	CanAbort          bool   `json:"canAbort"`
	UntrackedExcluded bool   `json:"untrackedExcluded"`
}

type KernelWorkspaceTextDiff struct {
	FileID       string               `json:"fileID"`
	Identity     string               `json:"identity"`
	Prepared     bool                 `json:"prepared"`
	CanExpand    bool                 `json:"canExpand"`
	ExpandedText *string              `json:"expandedText"`
	Selection    *KernelDiffSelection `json:"selection"`
}

type KernelWorkspaceBranch struct {
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	Remote  bool   `json:"remote"`
	Current bool   `json:"current"`
	Tip     string `json:"tip"`
}
type KernelWorkspaceBranchGroup struct {
	Group string                  `json:"group"`
	Items []KernelWorkspaceBranch `json:"items"`
}
type KernelWorkspacePullRequest struct {
	Number            int    `json:"number"`
	Title             string `json:"title"`
	Author            string `json:"author"`
	Head              string `json:"head"`
	Base              string `json:"base"`
	Draft             bool   `json:"draft"`
	CheckoutAvailable bool   `json:"checkoutAvailable"`
}
type KernelWorkspaceWorktree struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Main        bool   `json:"main"`
	Current     bool   `json:"current"`
	Locked      bool   `json:"locked"`
	Missing     bool   `json:"missing"`
}
type KernelWorkspaceCompare struct {
	ShowBranchList    bool                    `json:"showBranchList"`
	Mode              string                  `json:"mode"`
	Ref               *string                 `json:"ref"`
	Ahead             int                     `json:"ahead"`
	Behind            int                     `json:"behind"`
	MergeStatus       *string                 `json:"mergeStatus"`
	Operation         string                  `json:"operation"`
	OperationDisabled bool                    `json:"operationDisabled"`
	PreviewStatus     *string                 `json:"previewStatus"`
	RebaseCommits     int                     `json:"rebaseCommits"`
	Branches          []KernelWorkspaceBranch `json:"branches"`
}
type KernelWorkspaceStash struct {
	SHA       string       `json:"sha"`
	Branch    string       `json:"branch"`
	LoadState string       `json:"loadState"`
	Selected  bool         `json:"selected"`
	FileID    *string      `json:"fileID"`
	Files     []KernelFile `json:"files"`
}
type KernelWorkspaceFileFilter struct {
	FilterText string `json:"filterText"`
	Included   bool   `json:"isIncludedInCommit"`
	Excluded   bool   `json:"isExcludedFromCommit"`
	New        bool   `json:"isNewFile"`
	Modified   bool   `json:"isModifiedFile"`
	Deleted    bool   `json:"isDeletedFile"`
}
type KernelWorkspaceMenuTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type KernelWorkspaceMenu struct {
	Target KernelWorkspaceMenuTarget `json:"target"`
	Lease  string                    `json:"lease"`
	Items  []KernelWorkspaceMenuItem `json:"items"`
}
type KernelWorkspaceMenuItem struct {
	LabelKey  string                    `json:"labelKey"`
	Data      []string                  `json:"data"`
	Enabled   bool                      `json:"enabled"`
	Separator bool                      `json:"separator"`
	Checked   *bool                     `json:"checked"`
	Children  []KernelWorkspaceMenuItem `json:"children"`
}
type KernelWorkspaceBanner struct {
	ID          int      `json:"id"`
	Type        string   `json:"type"`
	LabelKey    string   `json:"labelKey"`
	Data        []string `json:"data"`
	Actions     []string `json:"actions"`
	Timeout     int      `json:"timeout"`
	Dismissable bool     `json:"dismissable"`
}

// KernelWorkspaceAction is an explicit union encoded below. Even a caller with
// a spare field cannot accidentally put it on the wire. The trusted renderer
// validates the shape and resolves every reference against current AppStore.
type KernelWorkspaceAction struct {
	Type                                                                       string
	Ref, InitialName, Path, Mode, SHA, FileID, Lease, Action, Identity, Option string
	Number, ID                                                                 int
	SHAs                                                                       []string
	BeforeSHA                                                                  *string
	Filter                                                                     KernelWorkspaceFileFilter
	Target                                                                     KernelWorkspaceMenuTarget
	MenuPath, LineIDs                                                          []int
	Value                                                                      bool
}

func (a KernelWorkspaceAction) wire() (map[string]any, error) {
	out := map[string]any{"type": a.Type}
	switch a.Type {
	case "submodule.open":
		out["fileID"] = a.FileID
	case "conflicts.open", "conflicts.continue", "conflicts.abort":
		out["identity"] = a.Identity
	case "navigation.open", "navigation.close":
		out["target"] = a.Option
	case "navigation.filter":
		out["scope"], out["text"] = a.Option, a.Path
	case "pullRequest.refresh":
	case "branch.checkout":
		out["ref"] = a.Ref
	case "branch.create":
		out["initialName"] = a.InitialName
	case "pullRequest.checkout":
		out["number"] = a.Number
	case "worktree.select":
		out["path"] = a.Path
	case "compare.list":
		out["visible"] = a.Value
	case "compare.operation":
		out["operation"] = a.Option
	case "compare.branch":
		out["ref"], out["mode"] = a.Ref, a.Mode
	case "worktree.create", "compare.history", "compare.merge", "history.reorder.cancel", "stash.view", "stash.hide", "stash.create":
	case "history.select":
		shas := a.SHAs
		if shas == nil {
			shas = []string{}
		}
		out["shas"] = shas
	case "history.reorder":
		out["shas"], out["beforeSHA"] = a.SHAs, a.BeforeSHA
	case "stash.restore", "stash.discard":
		out["sha"] = a.SHA
	case "stash.file":
		out["sha"], out["fileID"] = a.SHA, a.FileID
	case "files.filter":
		out["filter"] = a.Filter
	case "menu":
		out["target"], out["lease"], out["path"] = a.Target, a.Lease, a.MenuPath
	case "banner":
		out["id"], out["action"] = a.ID, a.Action
	case "diff.prepare", "diff.expand", "diff.collapse":
		out["fileID"], out["identity"] = a.FileID, a.Identity
	case "diff.discard":
		out["fileID"], out["identity"], out["lineIDs"] = a.FileID, a.Identity, a.LineIDs
	case "diff.option":
		out["option"], out["value"] = a.Option, a.Value
	default:
		return nil, errors.New("unknown workspace action; no command sent")
	}
	return out, nil
}

// WorkspaceBannerAction also works on the empty project screen: OS support and
// thank-you banners are global and do not require a selected repository.
func (k *Kernel) WorkspaceBannerAction(ctx context.Context, id int, action string) (KernelResult, error) {
	wire, err := (KernelWorkspaceAction{Type: "banner", ID: id, Action: action}).wire()
	if err != nil {
		return KernelResult{}, err
	}
	return k.command(ctx, struct {
		Name       string                     `json:"name"`
		Repository *KernelRepositoryReference `json:"repository"`
		Action     map[string]any             `json:"action"`
	}{"workspace.action", nil, wire})
}

func (k *Kernel) WorkspaceAction(ctx context.Context, repository KernelRepositoryReference, action KernelWorkspaceAction) (KernelResult, error) {
	wire, err := action.wire()
	if err != nil {
		return KernelResult{}, err
	}
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		Action     map[string]any            `json:"action"`
	}{"workspace.action", repository, wire})
}
