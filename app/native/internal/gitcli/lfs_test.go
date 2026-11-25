package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedCommitUsesOfficialLFSAndHooks(t *testing.T) {
	_, r := fixture(t)
	ctx := context.Background()
	if _, err := r.Run(ctx, nil, "lfs", "version"); err != nil {
		t.Fatal("official Git distribution must include LFS", err)
	}
	if _, err := r.Run(ctx, nil, "lfs", "install", "--local"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, ".gitattributes"), []byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0600); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("binary-fixture", 120)
	if err := os.WriteFile(filepath.Join(r.Path, "asset.bin"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	sha, err := r.CommitSelected(ctx, []FileSelection{{Path: ".gitattributes"}, {Path: "asset.bin"}}, "LFS file", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := r.Blob(ctx, sha, "asset.bin", 0)
	if err != nil || !strings.HasPrefix(string(pointer), "version https://git-lfs.github.com/spec/v1\n") {
		t.Fatalf("LFS clean filter was bypassed: %v", err)
	}
	work, err := os.ReadFile(filepath.Join(r.Path, "asset.bin"))
	if err != nil || string(work) != content {
		t.Fatal("LFS working file was changed")
	}
	if _, err := r.Run(ctx, nil, "lfs", "fsck"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.GitDir, "hooks", "pre-commit"), []byte("#!/bin/sh\necho native-fixture-rejected >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "new.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(r.GitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if sha, err := r.CommitSelected(ctx, []FileSelection{{Path: "new.txt"}}, "rejected", false, false, false); err == nil || sha != "" || !strings.Contains(err.Error(), "native-fixture-rejected") {
		t.Fatal("hook rejection was ignored")
	}
	after, err := os.ReadFile(filepath.Join(r.GitDir, "index"))
	if err != nil || string(before) != string(after) {
		t.Fatal("failed hook changed real staging")
	}
	if _, err := r.CommitSelected(ctx, []FileSelection{{Path: "new.txt"}}, "explicit skip", false, false, false, true); err != nil {
		t.Fatal("explicit skip-hooks did not work", err)
	}
}
