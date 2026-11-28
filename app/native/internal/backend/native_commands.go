package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type nativeCommand struct {
	Name                        string                    `json:"name"`
	Repository                  KernelRepositoryReference `json:"repository"`
	Paths                       []string                  `json:"paths"`
	Text                        string                    `json:"text"`
	Section                     string                    `json:"section"`
	FileIDs                     []string                  `json:"fileIDs"`
	FileID                      string                    `json:"fileID"`
	SHAs                        []string                  `json:"shas"`
	Included                    bool                      `json:"included"`
	Enabled                     bool                      `json:"enabled"`
	Message                     KernelMessage             `json:"message"`
	Context                     KernelCommitContext       `json:"context"`
	Options                     KernelCommitOptions       `json:"options"`
	Action                      json.RawMessage           `json:"action"`
	ID                          string                    `json:"id"`
	PopupID                     int                       `json:"popupID"`
	Reply                       KernelPopupReply          `json:"reply"`
	NameValue, Email, Step, URL string
	Provider                    ProviderSettings         `json:"provider"`
	ProviderID                  string                   `json:"providerId"`
	ProviderName                string                   `json:"providerName"`
	ModelID                     string                   `json:"modelId"`
	Secret                      *string                  `json:"secret"`
	Source                      string                   `json:"source"`
	Selection                   *KernelProviderSelection `json:"selection"`
	Language                    string                   `json:"language"`
	Endpoint                    string                   `json:"endpoint"`
	Models                      []ProviderDeployment     `json:"models"`
	Identity                    string                   `json:"identity"`
	LineIDs                     []int                    `json:"lineIDs"`
	ImageDiffType               int                      `json:"imageDiffType"`
	Mode, Query, Lease          string
	Path                        []int
	Override                    bool `json:"overrideExistingMessage"`
}

func (n *NativeOwner) command(ctx context.Context, value any) (KernelResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return KernelResult{}, err
	}
	var command nativeCommand
	if err = json.Unmarshal(data, &command); err != nil {
		return KernelResult{}, err
	}
	if command.Name == "state" {
		return KernelResult{Snapshot: n.snapshot()}, nil
	}
	if command.Name == "popup.reply" || command.Name == "popup.close" {
		if handled, err := n.answerModal(command); handled {
			return KernelResult{Snapshot: n.snapshot()}, err
		}
	}
	if command.Name == "repository.generate.cancel" {
		n.cancelMu.Lock()
		if n.generationCancel != nil {
			n.generationCancel()
		}
		n.cancelMu.Unlock()
		return KernelResult{Snapshot: n.snapshot()}, nil
	}
	n.active.Add(1)
	defer n.active.Add(-1)
	// Requests retain their own deadline and values, while owner shutdown also
	// cancels active I/O. Git writes explicitly detach cancellation at their
	// established commit/operation boundary so shutdown still drains safe writes.
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(n.ctx, cancel)
	defer func() { stop(); cancel() }()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return KernelResult{}, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return KernelResult{}, err
	}
	if command.Name != "repository.select" && command.Name != "repository.missing" && command.Name != "repository.menu" && command.Repository.Path != "" && (n.state.Selected == nil || command.Repository != n.state.Selected.Repository.KernelRepositoryReference) {
		return KernelResult{}, errors.New("repository selection changed")
	}
	result := KernelResult{}
	if n.state.Selected == nil && (strings.HasPrefix(command.Name, "history.") || strings.HasPrefix(command.Name, "commit.") || command.Name == "workspace.action" || strings.HasPrefix(command.Name, "repository.") && command.Name != "repository.select" && command.Name != "repository.missing" && command.Name != "repository.menu") {
		return result, errors.New("no repository selected")
	}
	switch command.Name {
	case "imageDiff.type":
		if command.ImageDiffType < 0 || command.ImageDiffType > 3 {
			err = errors.New("invalid image comparison mode")
		} else {
			n.state.ImageDiffType = command.ImageDiffType
			err = n.store.Set(ctx, "image-diff-type", strconv.Itoa(command.ImageDiffType))
		}
	case "commit.mode":
		if command.Mode != "manual" {
			err = errors.New("automatic multi-commit grouping is not available in the native Git workflow")
		} else {
			n.state.Selected.CommitView.Mode = "manual"
		}
	case "commit.coAuthors.show", "commit.coAuthors.search", "commit.coAuthors.add", "commit.coAuthors.remove":
		err = n.coAuthors(ctx, command)
	case "repository.menu":
		err = n.repositoryMenuCommand(ctx, command)
	case "repositories.add":
		for _, path := range command.Paths {
			repository, e := n.add(ctx, path)
			if e != nil {
				return result, e
			}
			result.AddedRepositories = append(result.AddedRepositories, repository.KernelRepositoryReference)
		}
	case "repository.select":
		err = n.selectRepository(ctx, command.Repository)
	case "repository.missing":
		err = n.recoverMissing(ctx, command)
	case "repositories.filter":
		paths := []string{}
		for _, repository := range n.state.Repositories {
			name := repository.Name
			if repository.Alias != nil {
				name = *repository.Alias
			}
			if strings.Contains(strings.ToLower(name), strings.ToLower(command.Text)) {
				paths = append(paths, repository.Path)
			}
		}
		n.state.RepositoryFilter = &KernelRepositoryFilter{Text: command.Text, Paths: paths}
	case "repository.refresh", "repository.refreshChanges":
		_, err = n.refresh(ctx, true)
	case "repository.section":
		n.state.Selected.Section = command.Section
		if command.Section == "history" {
			err = n.history(ctx, 0)
		}
	case "history.loadNext":
		err = n.history(ctx, len(n.state.Selected.History))
	case "history.select":
		n.state.Selected.HistorySelection = command.SHAs
		err = n.historyFiles(ctx)
	case "history.file.select":
		n.state.Selected.HistoryFileID = &command.FileID
		err = n.selectDiff(ctx, command.FileID, true)
	case "history.summaries":
		for _, sha := range command.SHAs {
			var count, added, deleted int
			count, added, deleted, err = n.current.Summary(ctx, sha)
			if err != nil {
				break
			}
			result.HistorySummaries = append(result.HistorySummaries, KernelHistorySummary{sha, count, added, deleted})
		}
	case "repository.files.select":
		n.state.Selected.SelectedFileIDs = command.FileIDs
		if len(command.FileIDs) > 0 {
			err = n.selectDiff(ctx, command.FileIDs[0], false)
		} else {
			n.clearDiff()
		}
	case "repository.files.include", "repository.files.includeAll":
		for _, file := range n.state.Selected.Files {
			if command.Name == "repository.files.includeAll" || slices.Contains(command.FileIDs, file.ID) {
				n.include[file.Path] = command.Included
				delete(n.partial, file.Path)
			}
		}
		n.updateInclusions()
	case "repository.message":
		n.state.Selected.Message = command.Message
		err = n.saveDraft(ctx)
	case "commit.options":
		view := n.state.Selected.CommitView
		if v := command.Options.AllowEmpty; v != nil {
			view.AllowEmpty = *v
		}
		if v := command.Options.SignOff; v != nil {
			view.SignOff = *v
		}
		if v := command.Options.SkipHooks; v != nil {
			view.SkipHooks = *v
		}
	case "commit.amend":
		n.state.Selected.CommitView.Amending = command.Enabled
		if command.Enabled {
			commits, e := n.current.History(ctx, "HEAD", 0, 1)
			if e != nil {
				err = e
			} else if len(commits) == 1 {
				c := commits[0]
				n.state.Selected.Message = KernelMessage{Summary: c.Subject, Description: &c.Body}
				err = n.saveDraft(ctx)
			}
		}
	case "repository.commit":
		result, err = n.commitIncluded(ctx, command.Context)
	case "repository.fetch", "repository.pull", "repository.push":
		operation := strings.TrimPrefix(command.Name, "repository.")
		args := []string{operation}
		if operation == "fetch" {
			args = []string{"-c", "fetch.followRemoteHEAD=always", "fetch"}
		}
		err = n.runRemote(ctx, args...)
		_, refreshErr := n.refresh(ctx, true)
		err = errors.Join(err, refreshErr)
	case "presentation.resizable-focus", "presentation.repository-list-focused", "presentation.commit-message-focused":
		n.state.FocusCommitMessage = false
	case "welcome.advance":
		err = n.welcome(ctx, command.Step)
	case "welcome.configureGit":
		err = n.configureAuthor(ctx, command.NameValue, command.Email)
	case "popup.close":
		n.confirmAction = nil
		n.state.CurrentPopup = nil
		n.state.Popups = []KernelPopup{}
	case "menu.execute":
		err = n.executeMenu(ctx, command.ID)
	case "popup.reply":
		err = n.replyForm(ctx, command.Reply)
	case "workspace.action":
		err = n.workspace(ctx, command.Action)
	case "repository.generate":
		err = n.requestGeneration(ctx, command)
	case "signIn.dotcom":
		err = n.beginSignIn(ctx, "https://api.github.com")
	case "signIn.enterprise":
		n.state.SignIn = &KernelSignIn{Kind: "EndpointEntry"}
		n.form("SignIn", "", nil)
		n.state.CurrentPopup.Layout = ""
	case "signIn.endpoint":
		endpoint := strings.TrimSuffix(command.Endpoint, "/")
		if !strings.HasSuffix(endpoint, "/api/v3") && !strings.HasSuffix(endpoint, "api.github.com") {
			endpoint += "/api/v3"
		}
		if _, e := htmlEndpoint(endpoint); e != nil {
			err = e
		} else {
			n.state.SignIn = &KernelSignIn{Kind: "Authentication", Endpoint: &endpoint}
		}
	case "signIn.browser":
		if n.state.SignIn == nil || n.state.SignIn.Endpoint == nil {
			err = errors.New("select an account endpoint")
		} else {
			err = n.beginSignIn(ctx, *n.state.SignIn.Endpoint)
		}
	case "signIn.reset":
		n.oauthPending = nil
		n.state.SignIn = nil
	case "launch.url":
		err = n.launchURL(ctx, command.URL)
	case "launch.cli":
		var action struct{ Kind, Path, URL, Branch string }
		if e := json.Unmarshal(command.Action, &action); e != nil {
			err = e
			break
		}
		if action.Kind == "open-repository" {
			var repository KernelRepository
			repository, err = n.add(ctx, action.Path)
			if err == nil {
				err = n.selectRepository(ctx, repository.KernelRepositoryReference)
			}
		} else if action.Kind == "clone-url" {
			n.cloneForm(action.URL, action.Branch)
		} else {
			err = errors.New("unknown native launch action")
		}
	case "generation.provider.save", "generation.provider.rename", "generation.provider.delete", "generation.provider.models.refresh", "generation.provider.models.enabled", "generation.provider.deployments.set", "generation.select", "generation.language":
		err = n.providerCommand(ctx, command)
	case "generation.copilot.settings":
		n.preferences("copilot")
	case "repository.lines.include":
		err = n.includeLines(command)
	default:
		err = fmt.Errorf("unknown native command: %s", command.Name)
	}
	if n.state.Selected != nil {
		n.updateInclusions()
	}
	n.publish()
	result.Snapshot = n.snapshot()
	return result, err
}
