package gitcli

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

type Change struct{ Path, OldPath, Status string }

func (r *Repository) Changes(ctx context.Context, commit string) ([]Change, error) {
	return r.ChangesBetween(ctx, "", commit)
}

// An empty base requests a single commit (including root/merge semantics).
// Otherwise the two immutable endpoints describe the complete selection.
func (r *Repository) ChangesBetween(ctx context.Context, base, commit string) ([]Change, error) {
	if !objectID(commit) || base != "" && !objectID(base) {
		return nil, errors.New("a full commit object ID is required")
	}
	data, err := r.engine.load(ctx, "changes\x00"+r.CommonDir+"\x00"+base+"\x00"+commit, func() ([]byte, error) {
		args := []string{"diff-tree", "--root", "--first-parent", "--no-commit-id", "--name-status", "-r", "-z", "-M"}
		if base != "" {
			args = append(args, base)
		}
		return r.Run(ctx, nil, append(args, commit, "--")...)
	})
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(data), "\x00")
	result := []Change{}
	for i := 0; i < len(parts)-1; {
		code := parts[i]
		i++
		if code == "" {
			continue
		}
		if i >= len(parts)-1 {
			return nil, errors.New("invalid change record")
		}
		file := Change{Path: parts[i], Status: code[:1]}
		i++
		if code[0] == 'R' || code[0] == 'C' {
			if i >= len(parts)-1 {
				return nil, errors.New("missing rename destination")
			}
			file.OldPath = file.Path
			file.Path = parts[i]
			i++
		}
		result = append(result, file)
	}
	return result, nil
}

func (r *Repository) Diff(ctx context.Context, path, commit string) (string, error) {
	return r.DiffFile(ctx, path, "", commit, 3, false)
}

// DiffFile keeps both paths of a rename in Git's path filter. This is necessary
// for line selections to refer to the original file rather than a new blob.
func (r *Repository) DiffFile(ctx context.Context, path, oldPath, commit string, contextLines int, hideWhitespace bool) (string, error) {
	return r.DiffFileBetween(ctx, path, oldPath, "", commit, contextLines, hideWhitespace)
}

// DiffFileBetween keeps the same endpoints for the initial and expanded view.
func (r *Repository) DiffFileBetween(ctx context.Context, path, oldPath, base, commit string, contextLines int, hideWhitespace bool) (string, error) {
	if commit != "" && !objectID(commit) || base != "" && (!objectID(base) || commit == "") {
		return "", errors.New("a full commit object ID is required")
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--src-prefix=a/", "--dst-prefix=b/"}
	if base != "" {
		args = append(args, base, commit)
	} else if commit != "" {
		args = []string{"show", "--format=", "--first-parent", "--no-ext-diff", "--no-textconv", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", commit}
	}
	args = append(args, "--unified="+strconv.Itoa(contextLines))
	if hideWhitespace {
		args = append(args, "--ignore-all-space")
	}
	if commit == "" {
		if head, err := r.Run(ctx, nil, "rev-parse", "--verify", "HEAD"); err == nil && len(head) > 0 {
			args = append(args, "HEAD")
		} else {
			empty, err := r.Run(ctx, strings.NewReader(""), "hash-object", "-t", "tree", "--stdin")
			if err != nil {
				return "", err
			}
			args = append(args, strings.TrimSpace(string(empty)))
		}
	}
	args = append(args, "--", path)
	if oldPath != "" {
		args = append(args, oldPath)
	}
	var data []byte
	var err error
	if commit != "" {
		data, err = r.engine.load(ctx, "diff\x00"+r.CommonDir+"\x00"+strings.Join(args, "\x00"), func() ([]byte, error) { return r.Run(ctx, nil, args...) })
	} else {
		data, err = r.Run(ctx, nil, args...)
	}
	if err != nil {
		return "", err
	}
	if len(data) == 0 && commit == "" {
		tracked, err := r.Run(ctx, nil, "ls-files", "--error-unmatch", "--", path)
		if err != nil && len(tracked) == 0 {
			data, err = r.Run(ctx, nil, "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--", "/dev/null", path)
			var command *CommandError
			if errors.As(err, &command) && command.ExitCode == 1 {
				err = nil
			}
		}
		if err != nil {
			return "", err
		}
	}
	return string(data), nil
}

type Reference struct {
	Name, ID, Upstream string
	Peeled             string
	Current            bool
}

func (r *Repository) References(ctx context.Context) ([]Reference, error) {
	data, err := r.Run(ctx, nil, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream)%00%(HEAD)%00%(*objectname)", "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}
	result := []Reference{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 5 {
			return nil, errors.New("invalid ref record")
		}
		result = append(result, Reference{Name: fields[0], ID: fields[1], Upstream: fields[2], Peeled: fields[4], Current: strings.TrimSpace(fields[3]) == "*"})
	}
	return result, nil
}

func (r *Repository) Summary(ctx context.Context, commit string) (int, int, int, error) {
	return r.SummaryBetween(ctx, "", commit)
}

// Statistics must use the same trees as the file list, not a sum of commits.
func (r *Repository) SummaryBetween(ctx context.Context, base, commit string) (int, int, int, error) {
	if !objectID(commit) || base != "" && !objectID(base) {
		return 0, 0, 0, errors.New("a full commit object ID is required")
	}
	data, err := r.engine.load(ctx, "summary\x00"+r.CommonDir+"\x00"+base+"\x00"+commit, func() ([]byte, error) {
		if base != "" {
			return r.Run(ctx, nil, "diff", "--numstat", "-z", base, commit, "--")
		}
		return r.Run(ctx, nil, "show", "--format=", "--first-parent", "--numstat", "-z", commit, "--")
	})
	if err != nil {
		return 0, 0, 0, err
	}
	count, added, deleted := 0, 0, 0
	parts := strings.Split(string(data), "\x00")
	for i := 0; i < len(parts)-1; i++ {
		line := parts[i]
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return 0, 0, 0, errors.New("invalid numstat")
		}
		count++
		a, _ := strconv.Atoi(fields[0])
		d, _ := strconv.Atoi(fields[1])
		added += a
		deleted += d
		if fields[2] == "" {
			i += 2
		}
	}
	return count, added, deleted, nil
}
