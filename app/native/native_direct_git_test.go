package main

import (
	"context"
	"fmt"
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeViewUsesDirectGoGitOwner(t *testing.T) {
	ctx := context.Background()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "selected.txt"), []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := newCoreApp(nil, nil)
	updates := make(chan func(), 64)
	a.update = func(fn func()) { updates <- fn }
	store, err := backend.OpenNativeStore(ctx, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "has-shown-welcome-flow", "1"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	core, err := backend.NewNativeKernel(ctx, git, filepath.Join(root, "profile"), func(event backend.KernelEvent) { updates <- func() { a.receiveCoreEvent(event) } })
	if err != nil {
		t.Fatal(err)
	}
	a.core = core
	defer func() {
		a.close()
		if err := core.Close(); err != nil {
			t.Error(err)
		}
	}()
	added, err := core.AddRepositories(ctx, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	result, err := core.SelectRepository(ctx, added.AddedRepositories[0])
	if err != nil {
		t.Fatal(err)
	}
	a.applyCoreSnapshot(result.Snapshot)
	a.ready = true
	a.commitCapable = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.SetLocale("zh-Hans")
	tt.Advance(time.Second)
	if !tt.HasText("selected.txt") {
		t.Fatalf("native view did not render direct Git result: %v", tt.Texts())
	}
	result, err = core.SetMessage(ctx, added.AddedRepositories[0], backend.KernelMessage{Summary: "Create selected file"})
	if err != nil {
		t.Fatal(err)
	}
	a.applyCoreSnapshot(result.Snapshot)
	tt.Frame()
	visibleControl(t, tt, "保存版本", 1180, 760)
	saveDesignEvidence(t, tt, "direct-go-git-zh-Hans.png")
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			for _, size := range [][2]int{{900, 620}, {1180, 760}} {
				view := ui.NewTester(a.view, size[0], size[1])
				view.SetLocale(locale)
				view.SetDark(dark)
				view.Advance(time.Second)
				if !view.HasText("selected.txt") {
					t.Fatalf("repository disappeared at %s/%v/%v", locale, dark, size)
				}
				for _, page := range []string{"appearance", "git", "accounts", "copilot"} {
					for len(updates) > 0 {
						(<-updates)()
					}
					opened, err := core.ExecuteMenu(ctx, "preferences")
					if err != nil {
						t.Fatal(err)
					}
					result, err := core.ReplyPopup(ctx, backend.KernelPopupReply{PopupID: *opened.Snapshot.CurrentPopup.ID, Action: "navigate", Values: map[string]any{"tab": page}})
					if err != nil {
						t.Fatal(err)
					}
					a.applyCoreSnapshot(result.Snapshot)
					view.Frame()
					view.Advance(time.Second)
					visibleControl(t, view, nativeDialogLabel(locale, "settings.save"), size[0], size[1])
					visibleControl(t, view, nativeDialogLabel(locale, "settings.cancel"), size[0], size[1])
					if size[0] == 900 {
						saveDesignEvidence(t, view, fmt.Sprintf("native-settings-%s-%s-%v.png", page, locale, dark))
					}
					closed, err := core.ClosePopup(ctx, *result.Snapshot.CurrentPopup.ID)
					if err != nil {
						t.Fatal(err)
					}
					a.applyCoreSnapshot(closed.Snapshot)
					view.Frame()
				}
			}
		}
	}
}
