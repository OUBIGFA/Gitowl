package gitcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type File struct {
	Path, OldPath, Index, Worktree, Submodule string
	Conflict                                  bool
}
type Status struct {
	Head, Branch, Upstream string
	Ahead, Behind          int
	Files                  []File
	Operation              string
	Fingerprint            string
}

// Porcelain v2 -z is Git's documented stable machine format; NUL framing keeps
// tabs, Unicode and newlines in file names out of the parser's field grammar.
func (r *Repository) Status(ctx context.Context) (Status, error) {
	var state Status
	state.Files = []File{}
	data, err := r.Run(ctx, nil, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return state, err
	}
	records := strings.Split(string(data), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "# ") {
			key, value, ok := strings.Cut(record[2:], " ")
			if !ok {
				return state, errors.New("invalid Git branch record")
			}
			switch key {
			case "branch.oid":
				if value != "(initial)" {
					state.Head = value
				}
			case "branch.head":
				if value != "(detached)" {
					state.Branch = value
				}
			case "branch.upstream":
				state.Upstream = value
			case "branch.ab":
				if _, err := fmt.Sscanf(value, "+%d -%d", &state.Ahead, &state.Behind); err != nil {
					return state, err
				}
			}
			continue
		}
		switch record[0] {
		case '?':
			state.Files = append(state.Files, File{Path: record[2:], Index: "?", Worktree: "?"})
		case '1', '2', 'u':
			count := 9
			if record[0] == '2' {
				count = 10
			}
			if record[0] == 'u' {
				count = 11
			}
			fields := strings.SplitN(record, " ", count)
			if len(fields) != count || len(fields[1]) != 2 {
				return state, errors.New("invalid Git status record")
			}
			file := File{Path: fields[count-1], Index: fields[1][:1], Worktree: fields[1][1:], Submodule: fields[2], Conflict: record[0] == 'u'}
			if record[0] == '2' {
				i++
				if i >= len(records) {
					return state, errors.New("missing original rename path")
				}
				file.OldPath = records[i]
			}
			state.Files = append(state.Files, file)
		case '!':
		default:
			return state, errors.New("unknown Git status record")
		}
	}
	for _, op := range []struct{ path, kind string }{{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"}, {"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"}, {"BISECT_LOG", "bisect"}} {
		if _, err := os.Stat(filepath.Join(r.GitDir, op.path)); err == nil {
			state.Operation = op.kind
			break
		} else if !os.IsNotExist(err) {
			return state, err
		}
	}
	hash := sha256.New()
	hash.Write(data)
	hash.Write([]byte(state.Operation))
	// Status codes alone do not detect edits to an already-modified file. Include
	// actual file bytes for changed regular files, never cache mutable work files.
	for _, file := range state.Files {
		if file.Submodule != "" && file.Submodule[0] == 'S' {
			continue
		}
		path := filepath.Join(r.Path, filepath.FromSlash(file.Path))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return state, err
		}
		if info.Mode().IsRegular() {
			sum, err := fileDigest(path)
			if err != nil {
				return state, err
			}
			hash.Write(sum)
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return state, err
			}
			hash.Write([]byte(target))
		}
	}
	state.Fingerprint = hex.EncodeToString(hash.Sum(nil))
	return state, nil
}

type Commit struct{ ID, Parents, Subject, Body, AuthorName, AuthorEmail, Date string }

func (r *Repository) History(ctx context.Context, ref string, skip, limit int) ([]Commit, error) {
	if limit <= 0 || limit > 500 {
		return nil, errors.New("history page limit must be 1..500")
	}
	if ref == "" {
		ref = "HEAD"
	}
	read := func() ([]byte, error) {
		return r.Run(ctx, nil, "log", "--encoding=UTF-8", "--date=iso-strict", "--format=%H%x00%P%x00%s%x00%b%x00%an%x00%ae%x00%aI%x00", "--max-count="+strconv.Itoa(limit), "--skip="+strconv.Itoa(max(0, skip)), "--end-of-options", ref, "--")
	}
	var data []byte
	var err error
	if objectID(ref) {
		data, err = r.engine.load(ctx, fmt.Sprintf("history\x00%s\x00%s\x00%d\x00%d", r.CommonDir, ref, skip, limit), read)
	} else {
		data, err = read()
	}
	if err != nil {
		return nil, err
	}
	fields := strings.Split(string(data), "\x00")
	out := []Commit{}
	for i := 0; i+7 < len(fields); i += 7 {
		out = append(out, Commit{strings.TrimSpace(fields[i]), fields[i+1], fields[i+2], fields[i+3], fields[i+4], fields[i+5], fields[i+6]})
	}
	return out, nil
}

// Commit --only leaves unrelated staged changes in the real index. Git itself
// owns commit/index locks, filters, LFS, signing and hooks. A failed hook keeps
// files and staging available for correction; no reset/clean is used for recovery.
func (r *Repository) Commit(ctx context.Context, paths []string, message string, amend, allowEmpty bool) (string, error) {
	r.mutation.Lock()
	defer r.mutation.Unlock()
	if strings.TrimSpace(message) == "" {
		return "", errors.New("a commit message is required")
	}
	if len(paths) == 0 && !allowEmpty && !amend {
		return "", errors.New("no selected changes")
	}
	if len(paths) > 0 {
		if _, err := r.Run(ctx, nil, append([]string{"add", "--"}, paths...)...); err != nil {
			return "", err
		}
	}
	args := []string{"commit", "--file=-"}
	if amend {
		args = append(args, "--amend")
	}
	if allowEmpty {
		args = append(args, "--allow-empty")
	}
	args = append(args, "--only")
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	if _, err := r.Run(ctx, strings.NewReader(message), args...); err != nil {
		return "", err
	}
	sha, err := r.Run(ctx, nil, "rev-parse", "--verify", "HEAD")
	return strings.TrimSpace(string(sha)), err
}
