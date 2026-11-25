package gitcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type SelectableLine struct {
	ID                   int
	Kind                 string
	Old, New             *int
	OldBuffer, NewBuffer *int
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// PatchLines exposes buffer positions from the same unified patch MyGo renders.
// Context and no-newline markers are not actionable changes.
func PatchLines(patch string) (string, []SelectableLine) {
	digest := sha256.Sum256([]byte(patch))
	out := []SelectableLine{}
	old, current := 0, 0
	oldBuffer, newBuffer := 0, 0
	inside := false
	for i, line := range strings.Split(strings.TrimSuffix(patch, "\n"), "\n") {
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			old, _ = strconv.Atoi(match[1])
			current, _ = strconv.Atoi(match[3])
			old = max(0, old-1)
			current = max(0, current-1)
			inside = true
			continue
		}
		if !inside || line == "" || line[0] == '\\' {
			continue
		}
		if strings.HasPrefix(line, "diff --git ") {
			inside = false
			continue
		}
		switch line[0] {
		case ' ':
			old++
			current++
			oldBuffer++
			newBuffer++
		case '-':
			position := old
			buffer := oldBuffer
			out = append(out, SelectableLine{ID: i, Kind: "delete", Old: &position, OldBuffer: &buffer})
			old++
			oldBuffer++
		case '+':
			position := current
			buffer := newBuffer
			out = append(out, SelectableLine{ID: i, Kind: "add", New: &position, NewBuffer: &buffer})
			current++
			newBuffer++
		}
	}
	return hex.EncodeToString(digest[:]), out
}

// SelectedPatch ports the existing MIT Desktop patch-formatter semantics:
// omitted additions disappear, omitted deletions become context, and hunk
// counts/no-newline markers are rebuilt. Git apply validates the result.
func SelectedPatch(patch string, selection map[int]bool) (string, error) {
	lines := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
	var header strings.Builder
	var output strings.Builder
	for i := 0; i < len(lines); {
		match := hunkHeader.FindStringSubmatch(lines[i])
		if match == nil {
			if strings.HasPrefix(lines[i], "diff --git ") || strings.HasPrefix(lines[i], "new file mode ") || strings.HasPrefix(lines[i], "rename from ") || strings.HasPrefix(lines[i], "rename to ") || strings.HasPrefix(lines[i], "old mode ") || strings.HasPrefix(lines[i], "new mode ") {
				header.WriteString(lines[i] + "\n")
			}
			if strings.HasPrefix(lines[i], "--- ") || strings.HasPrefix(lines[i], "+++ ") {
				header.WriteString(lines[i] + "\n")
			}
			i++
			continue
		}
		oldStart, _ := strconv.Atoi(match[1])
		newStart, _ := strconv.Atoi(match[3])
		i++
		var body strings.Builder
		oldCount, newCount := 0, 0
		changed := false
		wrote := false
		for i < len(lines) && !hunkHeader.MatchString(lines[i]) && !strings.HasPrefix(lines[i], "diff --git ") {
			line := lines[i]
			if line == "" {
				i++
				continue
			}
			kind := line[0]
			switch kind {
			case ' ':
				body.WriteString(line + "\n")
				oldCount++
				newCount++
				wrote = true
			case '+':
				wrote = selection[i]
				if wrote {
					body.WriteString(line + "\n")
					newCount++
					changed = true
				}
			case '-':
				if selection[i] {
					body.WriteString(line + "\n")
					oldCount++
					changed = true
				} else {
					body.WriteString(" " + line[1:] + "\n")
					oldCount++
					newCount++
				}
				wrote = true
			case '\\':
				if wrote {
					body.WriteString(line + "\n")
				}
			default:
				return "", errors.New("unsupported patch record")
			}
			i++
		}
		if changed {
			// A retained no-newline marker cannot be followed by another line
			// on that side. Git otherwise concatenates separately selected edits
			// in a way the visible line selection does not describe.
			oldEnded, newEnded := false, false
			lastKind := byte(0)
			for _, line := range strings.Split(body.String(), "\n") {
				if line == "" {
					continue
				}
				if line[0] == '\\' {
					if lastKind != '+' {
						oldEnded = true
					}
					if lastKind != '-' {
						newEnded = true
					}
					continue
				}
				if oldEnded && line[0] != '+' || newEnded && line[0] != '-' {
					return "", errors.New("select the adjacent changed lines together at the file's missing final newline")
				}
				lastKind = line[0]
			}
			fmt.Fprintf(&output, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
			output.WriteString(body.String())
		}
	}
	if output.Len() == 0 {
		return "", errors.New("no selected patch changes")
	}
	head := header.String()
	if strings.Contains(head, "+++ /dev/null\n") {
		for _, line := range strings.Split(head, "\n") {
			if path, ok := strings.CutPrefix(line, "--- "); ok {
				head = strings.Replace(head, "+++ /dev/null\n", "+++ "+strings.Replace(path, "a/", "b/", 1)+"\n", 1)
				break
			}
		}
	}
	return head + output.String(), nil
}

type FileSelection struct {
	Path     string
	OldPath  string
	Patch    string
	Identity string
	Lines    map[int]bool
}

// CommitSelected uses Git's alternate-index mechanism. Hooks and signing run
// against the candidate index; unrelated real-index entries are never committed.
// Recovery files are retained in the Git directory until explicitly recycled.
func (r *Repository) CommitSelected(ctx context.Context, files []FileSelection, message string, amend, allowEmpty, signOff bool, skipHooks ...bool) (string, error) {
	r.mutation.Lock()
	defer r.mutation.Unlock()
	if strings.TrimSpace(message) == "" {
		return "", errors.New("a commit message is required")
	}
	if len(files) == 0 && !allowEmpty && !amend {
		return "", errors.New("no selected changes")
	}
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(r.GitDir, name)); err == nil {
			return "", errors.New("finish or abort the current Git operation before creating a selected commit")
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	work, err := os.MkdirTemp(r.GitDir, "gitowl-commit-")
	if err != nil {
		return "", err
	}
	realIndex := filepath.Join(r.GitDir, "index")
	originalIndex, readErr := os.ReadFile(realIndex)
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", readErr
	}
	if readErr == nil {
		if err := os.WriteFile(filepath.Join(work, "original-index"), originalIndex, 0600); err != nil {
			return "", err
		}
	}
	index := filepath.Join(work, "index")
	env := append(append([]string{}, r.Environment...), "GIT_INDEX_FILE="+index)
	run := func(input string, args ...string) ([]byte, error) {
		return r.engine.run(ctx, r.Path, strings.NewReader(input), env, args...)
	}
	head, headErr := r.Run(ctx, nil, "rev-parse", "--verify", "HEAD")
	journal := map[string]any{"version": 1, "repository": r.Path, "originalHead": strings.TrimSpace(string(head)), "paths": files, "stage": "preparing"}
	record := func() error {
		data, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(work, "recovery.json"), data, 0600)
	}
	if err := record(); err != nil {
		return "", err
	}
	if headErr == nil {
		_, err = run("", "read-tree", strings.TrimSpace(string(head)))
	} else {
		_, err = run("", "read-tree", "--empty")
	}
	if err != nil {
		return "", err
	}
	paths := []string{}
	for _, file := range files {
		if !filepath.IsLocal(file.Path) {
			return "", errors.New("selected path leaves the repository")
		}
		paths = append(paths, file.Path)
		if file.OldPath != "" {
			if !filepath.IsLocal(file.OldPath) {
				return "", errors.New("original selected path leaves the repository")
			}
			paths = append(paths, file.OldPath)
		}
		if file.Lines == nil {
			args := []string{"add", "--all", "--", file.Path}
			if file.OldPath != "" {
				args = append(args, file.OldPath)
			}
			if _, err := run("", args...); err != nil {
				return "", err
			}
			continue
		}
		current, err := r.DiffFile(ctx, file.Path, file.OldPath, "", 3, false)
		if err != nil {
			return "", err
		}
		identity, _ := PatchLines(current)
		if identity != file.Identity {
			return "", errors.New("selected diff changed; review the current file")
		}
		patch, err := SelectedPatch(current, file.Lines)
		if err != nil {
			return "", err
		}
		if _, err := run(patch, "apply", "--cached", "--unidiff-zero", "--whitespace=nowarn", "-"); err != nil {
			return "", err
		}
	}
	args := []string{"commit", "--file=-"}
	if len(skipHooks) > 0 && skipHooks[0] {
		args = append(args, "--no-verify")
	}
	if amend {
		args = append(args, "--amend")
	}
	if allowEmpty {
		args = append(args, "--allow-empty")
	}
	if signOff {
		args = append(args, "--signoff")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	latest, latestErr := r.Run(ctx, nil, "rev-parse", "--verify", "HEAD")
	if (latestErr == nil) != (headErr == nil) || string(latest) != string(head) {
		return "", errors.New("repository HEAD changed while preparing the commit; recovery data was retained")
	}
	journal["stage"] = "committing"
	if err := record(); err != nil {
		return "", err
	}
	// Once Git owns a write, UI cancellation must not kill it and lose whether
	// the commit happened. The owner waits for the real exit and records the SHA.
	ctx = context.WithoutCancel(ctx)
	if _, err := run(message, args...); err != nil {
		observed, verifyErr := r.Run(ctx, nil, "rev-parse", "--verify", "HEAD")
		if (verifyErr == nil) == (headErr == nil) && string(observed) == string(head) {
			journal["stage"] = "rejected"
			return "", errors.Join(err, record())
		}
		return "", err
	}
	committed, err := r.Run(ctx, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(committed))
	journal["stage"] = "committed"
	journal["committedHead"] = sha
	if err := record(); err != nil {
		return sha, err
	}
	// Keep a byte-for-byte recovery index before reconciling only the selected
	// paths. A failure after commit returns its SHA so callers never retry it.
	currentIndex, indexErr := os.ReadFile(realIndex)
	if indexErr != nil && !os.IsNotExist(indexErr) {
		return sha, indexErr
	}
	if string(currentIndex) != string(originalIndex) {
		return sha, fmt.Errorf("commit %s succeeded, but another process changed staging; preserved both indexes in %s", sha, work)
	}
	if len(paths) > 0 {
		_, err = r.Run(ctx, nil, append([]string{"reset", "--quiet", "HEAD", "--"}, paths...)...)
	}
	if err == nil {
		journal["stage"] = "settled"
		err = record()
	}
	return sha, err
}
