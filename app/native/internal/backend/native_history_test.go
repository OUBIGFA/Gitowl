package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNativeHistorySelectionUsesCompleteChangeset(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "empty.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	run("config", "user.name", "Fixture")
	run("config", "user.email", "fixture@example.invalid")
	write("base.md", "base\n")
	run("add", ".")
	run("commit", "-m", "root")
	first := run("rev-parse", "HEAD")
	write("base.md", "base\nsecond\n")
	write("earlier.md", "earlier\n")
	run("add", ".")
	run("commit", "-m", "second")
	second := run("rev-parse", "HEAD")
	write("base.md", "base\nsecond\nthird\n")
	write("later.md", "later\n")
	run("add", ".")
	run("commit", "-m", "third")
	third := run("rev-parse", "HEAD")
	k, err := NewNativeKernel(ctx, git, filepath.Join(root, "profile"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	added, err := k.AddRepositories(ctx, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	ref := added.AddedRepositories[0]
	if _, err := k.SelectRepository(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := k.ChangeSection(ctx, ref, "history"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		shas       []string
		contiguous bool
		paths      []string
		added      int
		lines      []string
	}{
		{"single-root", []string{first}, true, []string{"base.md"}, 1, []string{"+base"}},
		{"single-latest", []string{third}, true, []string{"base.md", "later.md"}, 2, []string{"+third"}},
		{"combined-reversed-input", []string{second, third}, true, []string{"base.md", "earlier.md", "later.md"}, 4, []string{"+second", "+third"}},
		{"combined-includes-root", []string{third, second, first}, true, []string{"base.md", "earlier.md", "later.md"}, 5, []string{"+base", "+second", "+third"}},
		{"non-contiguous", []string{third, first}, false, nil, 0, nil},
		{"empty", nil, true, nil, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "history.select", SHAs: tc.shas})
			if err != nil {
				t.Fatal(err)
			}
			s := result.Snapshot.Selected
			if result.Snapshot.Workspace.HistorySelectionContiguous != tc.contiguous {
				t.Fatalf("incorrect continuity for %s", tc.name)
			}
			var paths []string
			for _, file := range s.HistoryFiles {
				paths = append(paths, file.Path)
			}
			if !slices.Equal(paths, tc.paths) || s.HistoryLinesAdded != tc.added {
				t.Fatalf("changeset paths=%v added=%d", paths, s.HistoryLinesAdded)
			}
			if len(tc.lines) == 0 {
				if s.Diff != nil || s.DiffSelection != nil || result.Snapshot.Workspace.TextDiff != nil {
					t.Fatal("old diff survived selection change")
				}
				return
			}
			result, err = k.SelectHistoryFile(ctx, ref, "base.md")
			if err != nil {
				t.Fatal(err)
			}
			assertLines := func(result KernelResult, expanded bool) {
				t.Helper()
				text := result.Snapshot.Selected.Diff.Text
				if expanded {
					text = result.Snapshot.Workspace.TextDiff.ExpandedText
				}
				if text == nil {
					t.Fatal("missing selected diff")
				}
				for _, line := range tc.lines {
					if !strings.Contains(*text, "\n"+line+"\n") {
						t.Fatalf("missing %s in %s", line, *text)
					}
				}
			}
			assertLines(result, false)
			diff := result.Snapshot.Workspace.TextDiff
			result, err = k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "diff.expand", FileID: "base.md", Identity: diff.Identity})
			if err != nil {
				t.Fatal(err)
			}
			assertLines(result, true)
		})
	}
	// The older public selection entry must set the same presentation state.
	result, err := k.SelectHistory(ctx, ref, []string{second}, true)
	if err != nil || !result.Snapshot.Workspace.HistorySelectionContiguous {
		t.Fatalf("public selection lost continuity: %v", err)
	}
	// Images use those same endpoints, including a selection starting at root.
	images := []string{
		`<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="red"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="green"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="blue"/></svg>`,
	}
	var imageSHAs []string
	for _, image := range images {
		write("image.svg", image)
		run("add", "image.svg")
		run("commit", "-m", "image")
		imageSHAs = append(imageSHAs, run("rev-parse", "HEAD"))
	}
	if _, err := k.RefreshRepository(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := k.SelectHistory(ctx, ref, []string{imageSHAs[2], imageSHAs[1]}, true); err != nil {
		t.Fatal(err)
	}
	result, err = k.SelectHistoryFile(ctx, ref, "image.svg")
	if err != nil {
		t.Fatal(err)
	}
	diff := result.Snapshot.Selected.Diff
	if diff == nil || diff.Kind != "image" || diff.Previous == nil || diff.Current == nil {
		t.Fatalf("missing combined images: %+v", diff)
	}
	if string(k.native.media.images[diff.Previous.MediaID].data) != images[0] || string(k.native.media.images[diff.Current.MediaID].data) != images[2] {
		t.Fatal("combined images used one commit's parent")
	}
	if _, err := k.SelectHistory(ctx, ref, []string{imageSHAs[2], imageSHAs[1], imageSHAs[0], third, second, first}, true); err != nil {
		t.Fatal(err)
	}
	result, err = k.SelectHistoryFile(ctx, ref, "image.svg")
	if err != nil || result.Snapshot.Selected.Diff.Previous != nil {
		t.Fatalf("root-inclusive image diff: %v", err)
	}
}
