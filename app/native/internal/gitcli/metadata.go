package gitcli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Metadata hints include lock/journal traffic. Fingerprint only Git's actual
// refs/config/worktree records so these events cannot reload history and menus.
func (r *Repository) MetadataFingerprint() (string, error) {
	hash := sha256.New()
	read := func(path string) error {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		hash.Write([]byte(path))
		hash.Write(data)
		return nil
	}
	for _, base := range []string{r.CommonDir, r.GitDir} {
		for _, name := range []string{"HEAD", "config", "config.worktree", "packed-refs", "refs/stash", "MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD"} {
			if err := read(filepath.Join(base, name)); err != nil {
				return "", err
			}
		}
	}
	for _, name := range []string{"refs", "worktrees"} {
		root := filepath.Join(r.CommonDir, name)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "logs" {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 || strings.HasSuffix(entry.Name(), ".lock") {
				return nil
			}
			if name == "worktrees" {
				switch entry.Name() {
				case "HEAD", "gitdir", "commondir", "locked", "config.worktree":
				default:
					return nil
				}
			}
			return read(path)
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
