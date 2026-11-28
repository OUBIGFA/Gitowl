package backend

import (
	"context"
	"errors"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
)

// commitIncluded owns result settlement, draft clearing and optional push.
// The caller holds the owner lock; Git owns the durable write/recovery boundary.
func (n *NativeOwner) commitIncluded(ctx context.Context, request KernelCommitContext) (KernelResult, error) {
	result := KernelResult{}
	var err error
	if err = n.refreshRecovery(); err != nil {
		return result, err
	}
	if n.state.Workspace.Banner != nil && n.state.Workspace.Banner.Type == "native-recovery" {
		err = n.recoveryDialog()
		if err == nil {
			err = errors.New("resolve the previous commit result before creating another commit")
		}
		return result, err
	}
	paths := []gitcli.FileSelection{}
	for _, file := range n.state.Selected.Files {
		if n.include[file.Path] {
			selection := gitcli.FileSelection{Path: file.Path}
			if file.OldPath != nil {
				selection.OldPath = *file.OldPath
			}
			if partial, ok := n.partial[file.Path]; ok {
				selection.Identity = partial.identity
				selection.Patch = partial.patch
				selection.Lines = partial.lines
			}
			paths = append(paths, selection)
		}
	}
	message := request.Summary
	if request.Description != nil && *request.Description != "" {
		message += "\n\n" + *request.Description
	}
	if len(request.Trailers) > 0 {
		message += "\n"
		for _, trailer := range request.Trailers {
			if strings.ContainsAny(trailer.Token+trailer.Value, "\r\n\x00") || strings.TrimSpace(trailer.Token) == "" {
				return result, errors.New("invalid commit trailer")
			}
			message += "\n" + trailer.Token + ": " + trailer.Value
		}
	}
	sha, commitErr := n.current.CommitSelected(ctx, paths, message, request.Amend, n.state.Selected.CommitView.AllowEmpty, n.state.Selected.CommitView.SignOff, n.state.Selected.CommitView.SkipHooks)
	err = commitErr
	if sha != "" {
		committed := true
		result.Committed = &committed
		n.state.Selected.Message = KernelMessage{}
		err = errors.Join(err, n.saveDraft(ctx))
		n.partial = make(map[string]nativePartialSelection)
		if err == nil && request.PushAfterCommit {
			err = n.runRemote(ctx, "push")
		}
	}
	_, refreshErr := n.refresh(ctx, true)
	err = errors.Join(err, refreshErr, n.refreshRecovery())
	return result, err
}
