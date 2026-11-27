package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// Exercise the actual menus and dialogs with snapshots from the Go owner.
// Only the OS recycle boundary is replaced, retaining the original bytes.
func TestNativeHistoryAndDiscardThroughView(t *testing.T) {
	for _, scenario := range []string{"history", "discard", "discard-crlf"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("TEST_ENV", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "empty.gitconfig"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			git, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(root, "repo")
			if err := os.MkdirAll(filepath.Join(repo, "writing"), 0700); err != nil {
				t.Fatal(err)
			}
			run := func(args ...string) string {
				t.Helper()
				cmd := exec.Command(git, args...)
				cmd.Dir = repo
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %s %v", args, out, err)
				}
				return strings.TrimSpace(string(out))
			}
			run("init", "-b", "main")
			run("config", "user.name", "Fixture")
			run("config", "user.email", "fixture@example.invalid")
			run("config", "core.autocrlf", "false")
			path := "writing/Skill 技能合集.md"
			file := filepath.Join(repo, filepath.FromSlash(path))
			if err := os.WriteFile(file, []byte("before\n"), 0600); err != nil {
				t.Fatal(err)
			}
			run("add", ".")
			run("commit", "-m", "Initial document")
			if scenario == "discard-crlf" {
				// The checked-out size changes when Git converts LF to CRLF.
				// Restoring only the worktree leaves the index's old size behind.
				run("config", "core.autocrlf", "true")
			}
			if err := os.WriteFile(file, []byte("after\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario != "history" {
				if err := os.WriteFile(filepath.Join(repo, "keep.txt"), []byte("keep staged\n"), 0600); err != nil {
					t.Fatal(err)
				}
				run("add", "keep.txt")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			profile := filepath.Join(root, "profile")
			store, err := backend.OpenNativeStore(ctx, profile)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Set(ctx, "has-shown-welcome-flow", "1"); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			a := newCoreApp(nil, nil)
			updates := make(chan func(), 256)
			a.update = func(fn func()) { updates <- fn }
			retired := filepath.Join(root, "recycled.md")
			a.corePlatform = func(_ context.Context, request backend.KernelPlatformRequest) (any, error) {
				if request.Operation != "move-to-trash" {
					t.Errorf("unexpected platform operation %s", request.Operation)
				}
				var path string
				if err := json.Unmarshal(request.Args[0], &path); err != nil {
					return nil, err
				}
				return nil, os.Rename(path, retired)
			}
			core, err := backend.NewNativeKernel(ctx, git, profile, func(event backend.KernelEvent) { updates <- func() { a.receiveCoreEvent(event) } })
			if err != nil {
				t.Fatal(err)
			}
			a.core = core
			defer func() {
				cancel()
				a.close()
				if err := core.Close(); err != nil {
					t.Error(err)
				}
			}()
			added, err := core.AddRepositories(ctx, []string{repo})
			if err != nil {
				t.Fatal(err)
			}
			ref := added.AddedRepositories[0]
			result, err := core.SelectRepository(ctx, ref)
			if err != nil {
				t.Fatal(err)
			}
			a.applyCoreSnapshot(result.Snapshot)
			a.ready, a.commitCapable = true, true
			tt := ui.NewTester(a.view, 1180, 760)
			tt.SetLocale("zh-Hans")
			await := func(done func() bool) {
				t.Helper()
				deadline := time.Now().Add(8 * time.Second)
				for {
					for len(updates) > 0 {
						(<-updates)()
					}
					tt.Frame()
					tt.Advance(time.Second / 60)
					if done() {
						return
					}
					if time.Now().After(deadline) {
						t.Fatalf("view did not settle: workspace=%v popup=%+v texts=%v", a.workspaceErr, a.coreSnapshot.CurrentPopup, tt.Texts())
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			await(func() bool { return tt.HasText("Skill 技能合集.md") })
			if scenario == "history" {
				if err := tt.Click(locales.In("zh-Hans").History()); err != nil {
					t.Fatal(err)
				}
				await(func() bool { return tt.HasText(locales.In("zh-Hans").FileCount(1)) })
				if err := tt.Click(locales.In("zh-Hans").FileCount(1)); err != nil {
					t.Fatal(err, tt.Texts())
				}
				await(func() bool { return a.workspacePending == 0 })
				if tt.HasText(workspaceLabel("zh-Hans", "workspace.nonContiguous")) || !tt.HasText("Skill 技能合集.md") {
					t.Fatalf("single commit's files hidden: %v", tt.Texts())
				}
				saveDesignEvidence(t, tt, "history-files-from-go.png")
				return
			}
			if err := tt.RightClick("Skill 技能合集.md"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(workspaceLabel("zh-Hans", "workspace.discard")); err != nil {
				t.Fatal(err)
			}
			await(func() bool {
				popup := a.coreSnapshot.CurrentPopup
				if popup == nil {
					return false
				}
				form := a.coreDialogForms[*popup.ID]
				return form != nil && form.presentedLayout.measured
			})
			// Complete the dialog's opening transition before targeting its button.
			tt.Advance(time.Second)
			saveDesignEvidence(t, tt, scenario+"-confirmation.png")
			if err := tt.Click(nativeDialogLabel("zh-Hans", "settings.save")); err != nil {
				t.Fatal(err, tt.Texts())
			}
			await(func() bool { return a.coreSnapshot.CurrentPopup == nil })
			got, err := os.ReadFile(file)
			want := "before\n"
			if scenario == "discard-crlf" {
				want = "before\r\n"
			}
			if err != nil || string(got) != want {
				t.Fatalf("discard did not restore file: %q %v", got, err)
			}
			got, err = os.ReadFile(retired)
			if err != nil || string(got) != "after\n" {
				t.Fatalf("discard did not retain original: %q %v", got, err)
			}
			if run("--no-optional-locks", "status", "--porcelain", "--", path) != "" || len(a.coreSnapshot.Selected.Files) != 1 || a.coreSnapshot.Selected.Files[0].Path != "keep.txt" {
				t.Fatal("discard left stale changes")
			}
			if run("show", ":keep.txt") != "keep staged" {
				t.Fatal("discard altered unrelated staged content")
			}
			saveDesignEvidence(t, tt, scenario+"-restored.png")
		})
	}
}
