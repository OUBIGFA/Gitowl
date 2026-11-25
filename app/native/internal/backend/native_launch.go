package backend

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

func (n *NativeOwner) cloneForm(remote, branch string) {
	choices := []KernelDialogChoice{{ID: "", Label: "settings.option.unassigned"}}
	for _, a := range n.accounts {
		choices = append(choices, KernelDialogChoice{ID: a.Endpoint + "\n" + a.Login, Label: a.Login + " · " + a.Endpoint})
	}
	n.form("CloneRepository", "repositoryDialog.CloneRepository", []KernelDialogField{nativeField("url", "repositoryDialog.url", remote), nativeField("path", "repositoryDialog.path", ""), nativeField("branch", "native.cloneBranch", branch), {ID: "account", LabelKey: "settings.account", Kind: "select", Value: "", Choices: choices}})
}

func (n *NativeOwner) clone(ctx context.Context, remote, path, branch, assignment string) (*KernelAccountIdentity, error) {
	var identity *KernelAccountIdentity
	if assignment != "" {
		endpoint, login, ok := strings.Cut(assignment, "\n")
		if !ok {
			return nil, errors.New("invalid clone account")
		}
		identity = &KernelAccountIdentity{Endpoint: endpoint, Login: login}
	}
	env, err := n.gitCredentialEnvironment(ctx, nil, identity)
	if err != nil {
		return nil, err
	}
	args := []string{"clone"}
	if branch != "" {
		if _, err := n.git.Global(ctx, "check-ref-format", "--branch", branch); err != nil {
			return nil, err
		}
		args = append(args, "--branch", branch)
	}
	args = append(args, "--", remote, path)
	err = n.withGitAskpass(ctx, env, func(ctx context.Context, env []string) error {
		_, err := n.git.GlobalWithEnvironment(ctx, env, args...)
		return err
	})
	return identity, err
}

// Keep Desktop's documented openRepo/branch/PR URL shape. Resolve registered
// repositories first; otherwise show an editable clone form before any fetch.
func (n *NativeOwner) launchURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid application URL")
	}
	if strings.EqualFold(u.Host, "oauth") {
		return n.finishSignIn(ctx, raw)
	}
	if !strings.EqualFold(u.Host, "openrepo") {
		return errors.New("unknown application URL action")
	}
	remote := strings.TrimPrefix(u.Path, "/")
	location, err := url.Parse(remote)
	if err != nil || location.Host == "" || (location.Scheme != "https" && location.Scheme != "ssh") || location.User != nil {
		return errors.New("application link requires an HTTPS or SSH repository URL without embedded credentials")
	}
	branch := u.Query().Get("branch")
	if branch != "" {
		if _, err = n.git.Global(ctx, "check-ref-format", "--branch", branch); err != nil {
			return err
		}
	}
	pr := u.Query().Get("pr")
	if pr != "" {
		number, err := strconv.Atoi(pr)
		if err != nil || number <= 0 || (branch != "" && branch != "pr/"+pr) {
			return errors.New("invalid pull request link")
		}
	}
	for _, record := range n.state.Repositories {
		r, err := n.git.Open(ctx, record.Path)
		if err != nil {
			continue
		}
		origin, err := r.Run(ctx, nil, "remote", "get-url", "origin")
		if err != nil {
			continue
		}
		if strings.TrimSuffix(strings.TrimSpace(string(origin)), ".git") != strings.TrimSuffix(remote, ".git") {
			continue
		}
		if err = n.selectRepository(ctx, record.KernelRepositoryReference); err != nil {
			return err
		}
		if pr != "" {
			if err = n.runRemote(ctx, "fetch", "origin", "refs/pull/"+pr+"/head"); err != nil {
				return err
			}
			_, err = n.current.Run(ctx, nil, "switch", "-c", "pr/"+pr, "FETCH_HEAD")
		} else if branch != "" {
			_, err = n.current.Run(ctx, nil, "switch", "--", branch)
		}
		if err != nil {
			return err
		}
		if file := u.Query().Get("filepath"); file != "" {
			if !filepath.IsLocal(file) {
				return errors.New("linked file leaves the repository")
			}
			if _, err = n.platform(ctx, "show-item-in-folder", filepath.Join(r.Path, file)); err != nil {
				return err
			}
		}
		_, err = n.refresh(ctx, true)
		return err
	}
	if pr != "" {
		return errors.New("clone this repository first, then open the pull request link")
	}
	n.cloneForm(remote, branch)
	return nil
}
