package gitcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPartialCommitFileKindsAndHunkCoordinates(t *testing.T) {
	base := ""
	for i := 0; i < 30; i++ {
		base += fmt.Sprintf("line %02d\n", i)
	}
	for _, tc := range []struct {
		name, before, after, expected string
		line                          int
		rename, staged                bool
	}{
		{"separate hunks", base, strings.ReplaceAll(strings.ReplaceAll(base, "line 01", "LINE 01"), "line 25", "LINE 25"), strings.ReplaceAll(base, "line 25", "LINE 25"), 25, false, false},
		{"new file", "", "one\ntwo\n", "one\n", 0, false, false},
		{"staged initial file", "", "one\ntwo\n", "one\n", 0, false, true},
		{"deleted file", "one\ntwo\n", "", "two\n", 0, false, false},
		{"no final newline", "one\nmiddle\ntwo", "ONE\nmiddle\nTWO", "one\nmiddle\nTWO", 2, false, false},
		{"rename", base, strings.ReplaceAll(strings.ReplaceAll(base, "line 01", "LINE 01"), "line 25", "LINE 25"), strings.ReplaceAll(base, "line 01", "LINE 01"), 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, r := fixture(t)
			ctx := context.Background()
			path := "file.txt"
			old := ""
			if tc.before != "" {
				if err := os.WriteFile(filepath.Join(r.Path, path), []byte(tc.before), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := r.Commit(ctx, []string{path}, "base", false, false); err != nil {
					t.Fatal(err)
				}
			}
			if tc.rename {
				old, path = path, "renamed.txt"
				if _, err := r.Run(ctx, nil, "mv", old, path); err != nil {
					t.Fatal(err)
				}
			}
			if tc.after == "" {
				// Git removes this fixture file; no user file is involved.
				if _, err := r.Run(ctx, nil, "rm", "--", path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(r.Path, path), []byte(tc.after), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.staged {
				if _, err := r.Run(ctx, nil, "add", "--", path); err != nil {
					t.Fatal(err)
				}
			}
			patch, err := r.DiffFile(ctx, path, old, "", 3, false)
			if err != nil {
				t.Fatal(err)
			}
			identity, lines := PatchLines(patch)
			chosen := map[int]bool{}
			for _, line := range lines {
				if line.Old != nil && *line.Old == tc.line || line.New != nil && *line.New == tc.line {
					chosen[line.ID] = true
				}
			}
			sha, err := r.CommitSelected(ctx, []FileSelection{{Path: path, OldPath: old, Identity: identity, Lines: chosen}}, "partial", false, false, false)
			if err != nil {
				t.Fatalf("%v\n%s", err, patch)
			}
			got, err := r.Blob(ctx, sha, path, 0)
			if err != nil || string(got) != tc.expected {
				t.Fatalf("content %q expected %q: %v", got, tc.expected, err)
			}
			if old != "" {
				if _, err := r.Blob(ctx, sha, old, 0); !errors.Is(err, ErrObjectMissing) {
					t.Fatalf("rename retained old file: %v", err)
				}
			}
			if tc.after != "" {
				work, err := os.ReadFile(filepath.Join(r.Path, path))
				if err != nil || string(work) != tc.after {
					t.Fatal("working copy changed")
				}
			}
		})
	}
}

func TestBlobRejectsOversizeWithoutReadingContent(t *testing.T) {
	e, r := fixture(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(r.Path, "large"), []byte(strings.Repeat("x", 4000)), 0600); err != nil {
		t.Fatal(err)
	}
	sha, err := r.Commit(ctx, []string{"large"}, "base", false, false)
	if err != nil {
		t.Fatal(err)
	}
	before := e.Stats().Commands
	if data, err := r.Blob(ctx, sha, "large", 128); !errors.Is(err, ErrOutputLimit) || data != nil {
		t.Fatalf("oversize result %d %v", len(data), err)
	}
	if e.Stats().Commands-before != 1 {
		t.Fatal("oversize blob content was read")
	}
	if _, err := r.Blob(ctx, sha, "absent", 0); !errors.Is(err, ErrObjectMissing) {
		t.Fatalf("missing object: %v", err)
	}
	commands := e.Stats().Commands
	if _, err := r.Blob(ctx, sha, "absent", 0); !errors.Is(err, ErrObjectMissing) || e.Stats().Commands != commands+1 {
		t.Fatal("missing object error was retained as immutable content")
	}
}

func TestPartialPatchRejectsAmbiguousMissingFinalNewline(t *testing.T) {
	patch := "--- a/file\n+++ b/file\n@@ -1,2 +1,2 @@\n-one\n-two\n\\ No newline at end of file\n+ONE\n+TWO\n\\ No newline at end of file\n"
	if _, err := SelectedPatch(patch, map[int]bool{3: true, 6: true}); err == nil {
		t.Fatal("ambiguous partial replacement would concatenate unselected content")
	}
	if _, err := SelectedPatch(patch, map[int]bool{3: true, 4: true, 6: true, 7: true}); err != nil {
		t.Fatal("complete replacement must remain supported", err)
	}
}

func TestDiscardSelectedChangesPreservesOtherLinesAndStaging(t *testing.T) {
	_, r := fixture(t)
	ctx := context.Background()
	path := filepath.Join(r.Path, "file")
	if err := os.WriteFile(path, []byte("one\nmiddle\ntwo"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, []string{"file"}, "base", false, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("ONE\nmiddle\nTWO"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, nil, "add", "file"); err != nil {
		t.Fatal(err)
	}
	index, _ := os.ReadFile(filepath.Join(r.GitDir, "index"))
	patch, err := r.Diff(ctx, "file", "")
	if err != nil {
		t.Fatal(err)
	}
	_, lines := PatchLines(patch)
	selected := map[int]bool{}
	for _, line := range lines {
		if line.Old != nil && *line.Old == 2 || line.New != nil && *line.New == 2 {
			selected[line.ID] = true
		}
	}
	reverse, err := DiscardPatch(patch, selected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Run(ctx, strings.NewReader(reverse), "apply", "--unidiff-zero", "-"); err != nil {
		t.Fatalf("%v\n%s", err, reverse)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "ONE\nmiddle\ntwo" {
		t.Fatalf("discard result %q %v", data, err)
	}
	after, _ := os.ReadFile(filepath.Join(r.GitDir, "index"))
	if string(after) != string(index) {
		t.Fatal("discard changed staging")
	}
}

func TestInterruptedCommitRecoveryDoesNotRepeatCommit(t *testing.T) {
	_, r := fixture(t)
	ctx := context.Background()
	for _, name := range []string{"chosen", "other"} {
		if err := os.WriteFile(filepath.Join(r.Path, name), []byte("base\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Commit(ctx, []string{"chosen", "other"}, "base", false, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"chosen", "other"} {
		if err := os.WriteFile(filepath.Join(r.Path, name), []byte("edit\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Run(ctx, nil, "add", "other"); err != nil {
		t.Fatal(err)
	}
	sha, err := r.CommitSelected(ctx, []FileSelection{{Path: "chosen"}}, "selected", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	records, err := r.CommitRecoveries()
	if err != nil || len(records) != 1 {
		t.Fatalf("records %v %v", records, err)
	}
	record := records[0]
	record.Stage = "committing"
	record.CommittedHead = ""
	data, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(record.Directory, "recovery.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(record.Directory, "original-index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.GitDir, "index"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverCommit(ctx, record.Directory); err != nil {
		t.Fatal(err)
	}
	head, err := r.Run(ctx, nil, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != sha {
		t.Fatal("recovery repeated or moved the commit")
	}
	staged, err := r.Run(ctx, nil, "diff", "--cached", "--name-only")
	if err != nil || string(staged) != "other\n" {
		t.Fatalf("staging after recovery %q %v", staged, err)
	}
	// A stale record must never overwrite work staged by a different process.
	record.Stage = "committed"
	record.CommittedHead = sha
	data, _ = json.Marshal(record)
	if err := os.WriteFile(filepath.Join(record.Directory, "recovery.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverCommit(ctx, record.Directory); err == nil {
		t.Fatal("changed staging was overwritten")
	}
}
