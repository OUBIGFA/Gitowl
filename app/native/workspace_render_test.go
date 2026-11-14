package main

import (
	"encoding/json"
	"image/png"
	"io/fs"
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

type realWorkspaceRender struct {
	a       *app
	tt      *ui.Tester
	updates chan func()
	out     string
	source  string
}

func renderFixture(t *testing.T) (repo, runtimeDir, out string) {
	t.Helper()
	repo = os.Getenv("GITOWL_NATIVE_SCREENSHOT_REPO")
	if repo == "" {
		t.Skip("set GITOWL_NATIVE_SCREENSHOT_REPO to a real review fixture")
	}
	runtimeDir = os.Getenv("GITOWL_NATIVE_RUNTIME")
	if runtimeDir == "" {
		t.Fatal("a production-view render requires GITOWL_NATIVE_RUNTIME")
	}
	var err error
	out, err = filepath.Abs("../../_temp/ui-redesign")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

func newRealWorkspaceRender(t *testing.T, repo, runtimeDir, out, source string) *realWorkspaceRender {
	t.Helper()
	preferences, err := os.MkdirTemp(out, "render-preferences-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(preferences) })
	service, err := backend.Start(backend.Config{
		Node: filepath.Join(runtimeDir, "node.exe"), Service: filepath.Join(runtimeDir, "service.js"), UserData: preferences,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	logo, err := loadLogo("resources")
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(service, logo)
	r := &realWorkspaceRender{a: a, updates: make(chan func(), 100), out: out, source: source}
	a.update = func(fn func()) { r.updates <- fn }
	r.tt = ui.NewTester(a.view, 1180, 760)
	r.tt.SetTitleBar(ui.TitleBar{Height: 48, Right: 138})
	r.tt.SetLocale("zh-Hans")
	t.Cleanup(a.close)
	a.connect(repo)
	r.await(t, func() bool { return a.status != nil && !a.loadingStatus })
	if !a.commitCapable || len(a.status.Files) == 0 {
		t.Fatal("render requires real changed files and the negotiated commit capability")
	}
	return r
}

func (r *realWorkspaceRender) await(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for !done() {
		select {
		case update := <-r.updates:
			update()
			r.tt.Frame()
			if r.a.serviceErr != nil || r.a.openErr != nil || r.a.statusErr != nil || r.a.historyErr != nil || r.a.commitErr != nil || r.a.diffErr != nil {
				t.Fatalf("real fixture: service=%v open=%v status=%v history=%v changeset=%v diff=%v", r.a.serviceErr, r.a.openErr, r.a.statusErr, r.a.historyErr, r.a.commitErr, r.a.diffErr)
			}
		case <-deadline.C:
			t.Fatal("real fixture operation did not settle")
		}
	}
}

func (r *realWorkspaceRender) click(t *testing.T, label string) {
	t.Helper()
	if err := r.tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func (r *realWorkspaceRender) summary(t *testing.T, tx locales.Messages, text string) {
	t.Helper()
	r.click(t, tx.CommitSummary())
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type(text)
}

func (r *realWorkspaceRender) snapshot(t *testing.T, name, locale, state string, width, height int, dark bool) {
	t.Helper()
	r.tt.SetSize(width, height)
	r.tt.SetDark(dark)
	r.tt.SetLocale(locale)
	// The real project label is non-interactive. Defocus editing without an
	// arbitrary click that could toggle a file or leave a caret in the diff.
	r.click(t, r.a.repo.Name)
	r.tt.Move(-10, -10)
	r.tt.Advance(300 * time.Millisecond)
	r.tt.Frame()
	file := filepath.Join(r.out, name+".png")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, r.tt.Image()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	// These ignored receipts describe the actual render and its real input;
	// they never claim a headless image contains system caption buttons.
	receipt := map[string]any{
		"renderer": "MyGo ui.NewTester, production app.view", "view": "app/native/view.go",
		"sourceFixture": r.source, "openedRepository": r.a.repo.Path, "repositoryName": r.a.repo.Name,
		"backend": "packaged Node/Git service, no backend mock", "locale": locale,
		"state": state, "widthDIP": width, "heightDIP": height, "dark": dark,
		"changedFiles": r.a.status.Files, "includedPaths": r.a.includedPaths(),
		"caption": "native titlebar space reserved; Tester does not draw system caption controls",
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(r.out, name+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("production-view snapshot: %s", file)
}

// Opt-in craft evidence: every page opens the supplied real Git project through
// the packaged service. This is not a second/demo UI, never rewrites the source
// fixture, and never submits a successful commit to make a screenshot look clean.
func TestNativeWorkspaceRenderRealProject(t *testing.T) {
	repo, runtimeDir, out := renderFixture(t)
	r := newRealWorkspaceRender(t, repo, runtimeDir, out, repo)
	tx := locales.In("zh-Hans")
	path := r.a.status.Files[0].Path
	r.click(t, tx.IncludeFile(path))
	r.summary(t, tx, "调整湖区两日行程")
	r.snapshot(t, "workspace-overview-zh", "zh-Hans", "overview", 1180, 760, false)
	r.snapshot(t, "workspace-overview-min-zh", "zh-Hans", "overview", 900, 620, false)
	r.snapshot(t, "workspace-overview-dark-zh", "zh-Hans", "overview", 1180, 760, true)
	r.tt.SetLocale("en")
	r.summary(t, locales.In("en"), "Refine the two-day itinerary")
	r.snapshot(t, "workspace-overview-en", "en", "overview", 1180, 760, false)
	r.snapshot(t, "workspace-overview-large-en", "en", "overview", 1920, 1080, false)
	r.tt.SetLocale("zh-Hans")
	r.summary(t, tx, "调整湖区两日行程")
	r.click(t, tx.OpenFile(path))
	r.await(t, func() bool { return r.a.preview != nil && !r.a.loadingDiff })
	r.snapshot(t, "workspace-review-zh", "zh-Hans", "focused review", 1180, 760, false)
	r.snapshot(t, "workspace-review-min-zh", "zh-Hans", "focused review", 900, 620, false)
	r.snapshot(t, "workspace-review-dark-zh", "zh-Hans", "focused review", 1180, 760, true)
	r.click(t, tx.CollapseDiff())
	if !r.a.included[path] || r.a.commitSummary != "调整湖区两日行程" {
		t.Fatal("real fixture overview/review/back lost its draft or selection")
	}
	r.tt.SetSize(1180, 760)
	r.tt.SetDark(false)
	r.click(t, tx.History())
	r.await(t, func() bool { return r.a.historyLoaded && !r.a.loadingHistory })
	r.snapshot(t, "workspace-history-zh", "zh-Hans", "history", 1180, 760, false)
	if len(r.a.commits) == 0 {
		t.Fatal("history render requires real commits")
	}
	r.click(t, r.a.commits[0].Summary)
	r.await(t, func() bool { return r.a.changeset != nil && !r.a.loadingCommit })
	r.snapshot(t, "workspace-version-zh", "zh-Hans", "version files", 1180, 760, false)
}

func TestNativeWorkspaceRenderRealHookFailure(t *testing.T) {
	source, runtimeDir, out := renderFixture(t)
	work, err := os.MkdirTemp(out, "render-hook-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	repo := filepath.Join(work, filepath.Base(source))
	git := filepath.Join(runtimeDir, "git", "cmd", "git.exe")
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+filepath.Join(work, "global-config"))
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	runGit("clone", "--quiet", "--no-hardlinks", source, repo)
	// A clone preserves the real history; copy the source's actual unsaved
	// files into it. Only this isolated copy gets a failing hook.
	if err = filepath.WalkDir(source, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(repo, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	runGit("-C", repo, "config", "user.name", "Gitowl UI Fixture")
	runGit("-C", repo, "config", "user.email", "fixture@example.invalid")
	runGit("-C", repo, "config", "commit.gpgsign", "false")
	runGit("-C", repo, "config", "core.hooksPath", ".git/hooks")
	hook := "#!/bin/sh\nprintf '本地检查未通过：请补充行程说明。\\n' >&2\n" +
		strings.Repeat("printf '检查详情：周末路线需要记录交通与步行安排。\\n' >&2\n", 180) + "exit 1\n"
	if err = os.WriteFile(filepath.Join(repo, ".git", "hooks", "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRealWorkspaceRender(t, repo, runtimeDir, out, source)
	tx := locales.In("zh-Hans")
	r.click(t, tx.IncludeFile(r.a.status.Files[0].Path))
	r.summary(t, tx, "调整湖区两日行程")
	r.click(t, tx.SaveVersion())
	r.await(t, func() bool { return !r.a.savingVersion && r.a.saveErr != nil })
	r.snapshot(t, "workspace-error-min-zh", "zh-Hans", "real rejecting Git hook, collapsed details", 900, 620, false)
	r.click(t, tx.ErrorDetails())
	r.tt.Advance(250 * time.Millisecond)
	r.snapshot(t, "workspace-error-details-min-zh", "zh-Hans", "real rejecting Git hook, expanded copyable details", 900, 620, false)
	if r.a.commitSummary != "调整湖区两日行程" || r.a.lastSavedSHA != "" {
		t.Fatal("a real rejecting hook lost the draft or invented a success")
	}
}
