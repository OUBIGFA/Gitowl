package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffUsesStablePathsAndRenamePolicy(t *testing.T) {
	_, r := fixture(t)
	ctx := context.Background()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(r.Path, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		if _, err := r.Run(ctx, nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	run("config", "diff.noprefix", "true")
	write("new.txt", "one\ntwo\n")
	patch, err := r.DiffFile(ctx, "new.txt", "", "", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "+++ b/new.txt\n") {
		t.Errorf("untracked patch inherited user prefix config: %s", patch)
	}
	if _, err := r.Commit(ctx, []string{"new.txt"}, "base", false, false); err != nil {
		t.Fatal(err)
	}
	run("config", "diff.renames", "false")
	run("mv", "new.txt", "renamed.txt")
	patch, err = r.DiffFile(ctx, "renamed.txt", "new.txt", "", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "rename from new.txt\nrename to renamed.txt\n") {
		t.Errorf("diff disagrees with rename-aware file list: %s", patch)
	}
}
