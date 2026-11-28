package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Each case owns a real Git repository and SQLite profile. Watchers are stopped
// so the test, rather than filesystem timing, determines the refresh boundary.
func nativeRepositoryFixture(t testing.TB) (*Kernel, KernelRepositoryReference) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TEST_ENV", "1")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "empty.gitconfig"))
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
	}
	k, err := NewNativeKernel(context.Background(), git, filepath.Join(root, "profile"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.Close() })
	added, err := k.AddRepositories(context.Background(), []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	ref := added.AddedRepositories[0]
	if _, err := k.SelectRepository(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	k.native.mu.Lock()
	k.native.watchCancel()
	k.native.mu.Unlock()
	return k, ref
}

// A real repository with 300 loaded history rows measures idle reconciliation,
// including Git, without a network, sleeps, or a synthetic replacement owner.
func BenchmarkNativeIdleRefresh(b *testing.B) {
	k, ref := nativeRepositoryFixture(b)
	ctx := context.Background()
	var input strings.Builder
	for i := 0; i < 300; i++ {
		message := fmt.Sprintf("commit %d", i)
		fmt.Fprintf(&input, "commit refs/heads/main\ncommitter Fixture <fixture@example.invalid> %d +0000\ndata %d\n%s\n\n", 1700000000+i, len(message), message)
	}
	input.WriteString("done\n")
	if _, err := k.native.current.Run(ctx, strings.NewReader(input.String()), "fast-import", "--quiet"); err != nil {
		b.Fatal(err)
	}
	if _, err := k.RefreshRepository(ctx, ref); err != nil {
		b.Fatal(err)
	}
	if _, err := k.ChangeSection(ctx, ref, "history"); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := k.LoadHistory(ctx, ref); err != nil {
			b.Fatal(err)
		}
	}
	if len(k.native.snapshot().Selected.History) != 300 {
		b.Fatal("history fixture was not loaded")
	}
	k.native.mu.Lock()
	defer k.native.mu.Unlock()
	commands := k.native.git.Stats().Commands
	b.ReportAllocs()
	for b.Loop() {
		if changed, err := k.native.refreshAndPublish(ctx, false); err != nil || changed {
			b.Fatalf("idle refresh %v %v", changed, err)
		}
	}
	b.ReportMetric(float64(k.native.git.Stats().Commands-commands)/float64(b.N), "git/op")
}

func TestNativeRefreshRetriesFailedDiff(t *testing.T) {
	for _, change := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed-%t", change), func(t *testing.T) {
			k, ref := nativeRepositoryFixture(t)
			ctx := context.Background()
			r := k.native.current
			write := func(text string) {
				if err := os.WriteFile(filepath.Join(ref.Path, "file.txt"), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("base\n")
			if _, err := r.Commit(ctx, []string{"file.txt"}, "base", false, false); err != nil {
				t.Fatal(err)
			}
			write("first\n")
			if _, err := k.RefreshRepository(ctx, ref); err != nil {
				t.Fatal(err)
			}
			before, err := k.SelectFiles(ctx, ref, []string{"file.txt"})
			if err != nil {
				t.Fatal(err)
			}
			expected := "+first"
			if change {
				write("second\n")
				expected = "+second"
			}
			object, err := r.Run(ctx, nil, "rev-parse", "HEAD:file.txt")
			if err != nil {
				t.Fatal(err)
			}
			id := strings.TrimSpace(string(object))
			objectPath := filepath.Join(r.GitDir, "objects", id[:2], id[2:])
			if err := os.Rename(objectPath, objectPath+".held"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := os.Stat(objectPath + ".held"); err == nil {
					os.Rename(objectPath+".held", objectPath)
				}
			})
			if _, err := r.Status(ctx); err != nil {
				t.Fatalf("failure must occur after status: %v", err)
			}
			k.native.mu.Lock()
			_, refreshErr := k.native.refreshAndPublish(ctx, !change)
			k.native.mu.Unlock()
			if refreshErr == nil {
				t.Fatal("diff succeeded while its base blob was unavailable")
			}
			if err := os.Rename(objectPath+".held", objectPath); err != nil {
				t.Fatal(err)
			}
			k.native.mu.Lock()
			_, err = k.native.refreshAndPublish(ctx, false)
			k.native.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			after := k.native.snapshot()
			if change && after.Revision <= before.Snapshot.Revision || k.native.state.Selected.Diff == nil || after.Selected.Diff == nil || after.Selected.Diff.Text == nil || !strings.Contains(*after.Selected.Diff.Text, expected) {
				t.Fatal("unchanged retry consumed failed refresh fingerprint and left stale diff")
			}
		})
	}
}

func TestNativeStatusRefreshUpdatesConflict(t *testing.T) {
	k, ref := nativeRepositoryFixture(t)
	ctx := context.Background()
	r := k.native.current
	run := func(args ...string) {
		t.Helper()
		if _, err := r.Run(ctx, nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(ref.Path, "file.txt"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("base\n")
	run("add", ".")
	run("commit", "-m", "base")
	run("switch", "-c", "other")
	write("other\n")
	run("commit", "-am", "other")
	run("switch", "main")
	write("main\n")
	run("commit", "-am", "main")
	if _, err := r.Run(ctx, nil, "merge", "other"); err == nil {
		t.Fatal("expected merge conflict")
	}
	if _, err := k.RefreshRepository(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if k.native.snapshot().Workspace.Conflict.Remaining != 1 {
		t.Fatal("missing conflict")
	}
	write("resolved\n")
	run("add", "file.txt")
	k.native.mu.Lock()
	_, err := k.native.refreshAndPublish(ctx, false)
	k.native.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	conflict := k.native.snapshot().Workspace.Conflict
	if conflict == nil || conflict.Remaining != 0 || !conflict.CanContinue || conflict.Identity != k.native.status.Fingerprint {
		t.Fatalf("status-only refresh left stale conflict controls: %+v", conflict)
	}
}

func TestNativeClearsInvisiblePreview(t *testing.T) {
	for _, action := range []string{"deselect-file", "deselect-history", "missing-repository"} {
		t.Run(action, func(t *testing.T) {
			k, ref := nativeRepositoryFixture(t)
			ctx := context.Background()
			if err := os.WriteFile(filepath.Join(ref.Path, "image.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"/>`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := k.RefreshRepository(ctx, ref); err != nil {
				t.Fatal(err)
			}
			if _, err := k.SelectFiles(ctx, ref, []string{"image.svg"}); err != nil {
				t.Fatal(err)
			}
			if len(k.native.media.images) == 0 {
				t.Fatal("fixture did not create image preview")
			}
			switch action {
			case "deselect-file":
				if _, err := k.SelectFiles(ctx, ref, nil); err != nil {
					t.Fatal(err)
				}
			case "deselect-history":
				if _, err := k.SelectHistory(ctx, ref, nil, true); err != nil {
					t.Fatal(err)
				}
			case "missing-repository":
				// A registered second repository disappears before selection.
				missing := filepath.Join(filepath.Dir(ref.Path), "missing")
				if err := os.Mkdir(missing, 0700); err != nil {
					t.Fatal(err)
				}
				if _, err := k.native.git.Init(ctx, missing); err != nil {
					t.Fatal(err)
				}
				added, err := k.AddRepositories(ctx, []string{missing})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(missing, missing+"-moved"); err != nil {
					t.Fatal(err)
				}
				if _, err := k.SelectRepository(ctx, added.AddedRepositories[0]); err != nil {
					t.Fatal(err)
				}
			}
			if len(k.native.media.images) != 0 {
				t.Fatal("invisible preview kept compressed image bytes")
			}
			if s := k.native.snapshot().Selected; s != nil && (s.Diff != nil || s.DiffSelection != nil) {
				t.Fatal("invisible diff remained selectable")
			}
		})
	}
}
