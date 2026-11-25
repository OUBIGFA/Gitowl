package gitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CommitRecovery is the durable boundary between an accepted Git commit and
// reconciliation of the user's original staging area. It never stores tokens.
type CommitRecovery struct {
	Version       int             `json:"version"`
	Repository    string          `json:"repository"`
	OriginalHead  string          `json:"originalHead"`
	CommittedHead string          `json:"committedHead,omitempty"`
	Stage         string          `json:"stage"`
	Paths         []FileSelection `json:"paths"`
	Directory     string          `json:"-"`
}

func (r *Repository) CommitRecoveries() ([]CommitRecovery, error) {
	entries, err := os.ReadDir(r.GitDir)
	if err != nil {
		return nil, err
	}
	result := []CommitRecovery{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "gitowl-commit-") {
			continue
		}
		directory := filepath.Join(r.GitDir, entry.Name())
		data, err := os.ReadFile(filepath.Join(directory, "recovery.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var record CommitRecovery
		if json.Unmarshal(data, &record) != nil || record.Version != 1 || record.Repository != r.Path {
			return nil, errors.New("invalid commit recovery record; original data preserved")
		}
		record.Directory = directory
		result = append(result, record)
	}
	return result, nil
}

// RecoverCommit settles only the exact candidate Git wrote. It refuses to
// overwrite externally changed staging or to replay a commit whose result is
// uncertain. The UI can reveal both saved indexes for manual recovery instead.
func (r *Repository) RecoverCommit(ctx context.Context, directory string) error {
	r.mutation.Lock()
	defer r.mutation.Unlock()
	records, err := r.CommitRecoveries()
	if err != nil {
		return err
	}
	var record *CommitRecovery
	for i := range records {
		if records[i].Directory == directory {
			record = &records[i]
			break
		}
	}
	if record == nil {
		return errors.New("commit recovery record no longer exists")
	}
	if record.Stage == "settled" || record.Stage == "preparing" || record.Stage == "rejected" {
		return nil
	}
	head, err := r.Run(ctx, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	sha := strings.TrimSpace(string(head))
	if sha == record.OriginalHead {
		return nil
	} // Git rejected the candidate.
	if record.CommittedHead != "" && record.CommittedHead != sha {
		return errors.New("HEAD changed since this commit; inspect the retained indexes")
	}
	info, err := r.Run(ctx, nil, "show", "-s", "--format=%T%n%P", sha, "--")
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(info), "\n"), "\n")
	candidate, err := r.engine.run(ctx, r.Path, nil, append(append([]string{}, r.Environment...), "GIT_INDEX_FILE="+filepath.Join(directory, "index")), "write-tree")
	if err != nil {
		return err
	}
	if len(lines) != 2 || lines[0] != strings.TrimSpace(string(candidate)) || record.CommittedHead == "" && lines[1] != record.OriginalHead {
		return errors.New("commit result cannot be identified safely; inspect the retained indexes")
	}
	original, err := os.ReadFile(filepath.Join(directory, "original-index"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	current, err := os.ReadFile(filepath.Join(r.GitDir, "index"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(original, current) {
		return errors.New("staging changed after the commit; both indexes were preserved")
	}
	paths := []string{}
	for _, file := range record.Paths {
		if !filepath.IsLocal(file.Path) || file.OldPath != "" && !filepath.IsLocal(file.OldPath) {
			return errors.New("invalid recovery path")
		}
		paths = append(paths, file.Path)
		if file.OldPath != "" {
			paths = append(paths, file.OldPath)
		}
	}
	if len(paths) > 0 {
		if _, err := r.Run(context.WithoutCancel(ctx), nil, append([]string{"reset", "--quiet", sha, "--"}, paths...)...); err != nil {
			return err
		}
	}
	record.Stage, record.CommittedHead = "settled", sha
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "recovery.json"), data, 0600)
}
