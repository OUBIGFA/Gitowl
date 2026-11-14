package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

// This test drives the production view against the packaged Node/Git runtime.
// No backend mock is allowed to manufacture a successful commit confirmation.
func TestNativeViewCreatesRealSelectedCommit(t *testing.T) {
	exerciseRealSelectedCommit(t, false)
}

func TestNativeComposerShortcutCreatesOneRealCommit(t *testing.T) {
	exerciseRealSelectedCommit(t, true)
}

func exerciseRealSelectedCommit(t *testing.T, shortcut bool) {
	t.Helper()
	runtimeDir := os.Getenv("GITOWL_NATIVE_RUNTIME")
	if runtimeDir == "" {
		t.Skip("set GITOWL_NATIVE_RUNTIME to the verified staged service directory")
	}
	scratch, err := filepath.Abs("../../_temp/native-ui-integration")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	work, err := os.MkdirTemp(scratch, "case-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(work)
	repo := filepath.Join(work, "repository")
	git := filepath.Join(runtimeDir, "git", "cmd", "git.exe")
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+filepath.Join(work, "global-config"), "GIT_AUTHOR_NAME=Native Test", "GIT_AUTHOR_EMAIL=native@example.invalid", "GIT_COMMITTER_NAME=Native Test", "GIT_COMMITTER_EMAIL=native@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	runGit("init", "--initial-branch=main", repo)
	runGit("-C", repo, "config", "user.name", "Native Test")
	runGit("-C", repo, "config", "user.email", "native@example.invalid")
	runGit("-C", repo, "config", "commit.gpgsign", "false")
	for _, name := range []string{"first.txt", "other.txt"} {
		if err = os.WriteFile(filepath.Join(repo, name), []byte("before\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("-C", repo, "add", ".")
	runGit("-C", repo, "commit", "-m", "initial")
	for _, name := range []string{"first.txt", "other.txt"} {
		if err = os.WriteFile(filepath.Join(repo, name), []byte("after\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	service, err := backend.Start(backend.Config{Node: filepath.Join(runtimeDir, "node.exe"), Service: filepath.Join(runtimeDir, "service.js"), UserData: filepath.Join(work, "preferences")})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	a := newApp(service, nil)
	defer a.close()
	updates := make(chan func(), 100)
	a.update = func(fn func()) { updates <- fn }
	tt := ui.NewTester(a.view, 1180, 760)
	tt.SetLocale("en")
	await := func(done func() bool) {
		t.Helper()
		timeout := time.NewTimer(20 * time.Second)
		defer timeout.Stop()
		for !done() {
			select {
			case fn := <-updates:
				fn()
				tt.Frame()
			case <-timeout.C:
				t.Fatalf("native operation did not settle: service=%v status=%v diff=%v save=%v", a.serviceErr, a.statusErr, a.diffErr, a.saveErr)
			}
		}
	}
	a.connect(repo)
	await(func() bool { return a.preview != nil || a.diffErr != nil })
	if a.diffErr != nil || len(a.preview.patches) != 1 {
		t.Fatalf("real upstream patch not displayed: %v", a.diffErr)
	}
	added, deleted := a.preview.patches[0].state.Stats()
	if added != 1 || deleted != 1 {
		t.Fatalf("real native diff counts: +%d -%d", added, deleted)
	}
	for _, label := range []string{"Review first.txt", "Include first.txt", "Version summary"} {
		if err = tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	tt.Type("Native selected version")
	if err = tt.Click("Collapse diff"); err != nil {
		t.Fatal(err)
	}
	if a.reviewingFile || !a.included["first.txt"] || a.commitSummary != "Native selected version" {
		t.Fatal("returning to the overview lost the real selection or draft")
	}
	if shortcut {
		if err = tt.Click("Version summary"); err != nil {
			t.Fatal(err)
		}
		tt.Key(ui.Ctrl, ui.KeyEnter)
		tt.Key(ui.Ctrl, ui.KeyEnter)
	} else if err = tt.Click("Save version"); err != nil {
		t.Fatal(err)
	}
	await(func() bool { return !a.savingVersion && (a.lastSavedSHA != "" || a.saveErr != nil) })
	if a.saveErr != nil {
		t.Fatal(a.saveErr)
	}
	if len(a.lastSavedSHA) != 40 || a.commitSummary != "" {
		t.Fatal("real confirmation did not clear the successful draft")
	}
	if count := strings.TrimSpace(runGit("-C", repo, "rev-list", "--count", "HEAD")); count != "2" {
		t.Fatalf("the save action created repeated commits: %s", count)
	}
	if got := strings.TrimSpace(runGit("-C", repo, "show", "--format=", "--name-only", "HEAD")); got != "first.txt" {
		t.Fatalf("unselected file committed: %q", got)
	}
	if got := runGit("-C", repo, "diff", "--name-only"); !strings.Contains(got, "other.txt") {
		t.Fatal("unselected changes were lost")
	}
	await(func() bool { return !a.loadingStatus && !a.loadingHistory })
	if len(a.status.Files) != 1 || a.status.Files[0].Path != "other.txt" {
		t.Fatalf("view did not refresh after commit: %+v", a.status)
	}
}
