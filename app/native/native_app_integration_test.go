package main

import (
	"context"
	"fmt"
	"image/png"
	"math"
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

// This is the production MyGo view and real original Electron kernel. It never
// invokes the reduced research service or substitutes a commit confirmation.
func TestNativeViewUsesOriginalDesktopKernel(t *testing.T) {
	for _, scenario := range []string{"save-en", "shortcut-zh", "hook-ignore", "hook-abort"} {
		t.Run(scenario, func(t *testing.T) { exerciseOriginalKernelView(t, scenario) })
	}
}

func TestNativePreferencesFitAllOriginalPages(t *testing.T) {
	exerciseOriginalKernelView(t, "preferences-fit")
}

func exerciseOriginalKernelView(t *testing.T, scenario string) {
	t.Helper()
	entry, electron := os.Getenv("GITOWL_KERNEL_ENTRY"), os.Getenv("GITOWL_KERNEL_ELECTRON")
	if entry == "" || electron == "" {
		t.Skip("run npm run test:kernel with the compiled original Desktop kernel")
	}
	scratch, err := filepath.Abs("../../_temp/native-kernel-ui")
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
	git := filepath.Join(filepath.Dir(entry), "git", "cmd", "git.exe")
	global := filepath.Join(work, "global-config")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("TEST_ENV", "1")
	repo := filepath.Join(work, "project")
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("init", "--initial-branch=main", repo)
	runGit("-C", repo, "config", "user.name", "Gitowl Native Kernel Tests")
	runGit("-C", repo, "config", "user.email", "native-kernel@example.invalid")
	runGit("-C", repo, "config", "commit.gpgsign", "false")
	for _, name := range []string{"selected.txt", "unselected.txt"} {
		if err = os.WriteFile(filepath.Join(repo, name), []byte("before\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("-C", repo, "add", ".")
	runGit("-C", repo, "commit", "-m", "initial")
	for _, name := range []string{"selected.txt", "unselected.txt"} {
		if err = os.WriteFile(filepath.Join(repo, name), []byte("after\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if strings.HasPrefix(scenario, "hook-") {
		if err = os.WriteFile(filepath.Join(repo, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\nprintf 'native-original-hook-output\\n'\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	a := newCoreApp(nil, nil)
	updates := make(chan func(), 1000)
	a.update = func(fn func()) { updates <- fn }
	core, err := backend.StartKernel(backend.KernelConfig{Electron: electron, Entry: entry, UserData: filepath.Join(work, "data")}, func(event backend.KernelEvent) {
		updates <- func() { a.receiveCoreEvent(event) }
	})
	if err != nil {
		t.Fatal(err)
	}
	a.core = core
	// A failed kernel action surfaces only as a generic RPC error in the view;
	// report the original popup so intermittent failures carry their cause.
	t.Cleanup(func() {
		if t.Failed() && a.coreSnapshot != nil && a.coreSnapshot.CurrentPopup != nil {
			p := a.coreSnapshot.CurrentPopup
			t.Logf("original popup at failure: type=%s title=%q content=%+v causes=%+v", p.Type, p.Title, p.Content, p.Causes)
		}
	})
	defer func() {
		// A failed test may leave its own rejecting hook awaiting a UI choice.
		// Settle that original callback explicitly before closing the fixture,
		// not by killing a kernel with an unresolved commit.
		if a.coreSnapshot != nil && a.coreSnapshot.CurrentPopup != nil && a.coreSnapshot.CurrentPopup.Type == "HookFailed" && a.coreSnapshot.CurrentPopup.ID != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = core.ClosePopup(ctx, *a.coreSnapshot.CurrentPopup.ID)
			cancel()
			deadline := time.Now().Add(10 * time.Second)
			for core.InFlight() != 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
		}
		if a.coreSnapshot != nil && a.coreSnapshot.Selected != nil && (a.coreGenerationPending || a.coreSnapshot.Selected.GenerationInProgress) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = core.CancelGeneration(ctx, a.coreSnapshot.Selected.Repository.KernelRepositoryReference)
			cancel()
			deadline := time.Now().Add(10 * time.Second)
			for core.InFlight() != 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
		}
		a.close()
		if err := core.Close(); err != nil {
			t.Error(err)
		}
	}()
	tt := ui.NewTester(func(c *ui.Context) {
		seen := make(map[string]bool, len(a.commits))
		for _, commit := range a.commits {
			if seen[commit.SHA] {
				t.Fatalf("history repeated a commit: commits=%+v pending=%d loading=%v", a.commits, a.corePending, a.loadingHistory)
			}
			seen[commit.SHA] = true
		}
		a.view(c)
	}, 1180, 760)
	settingsSize := [2]int{1180, 760}
	requestedSize := [2]int{}
	// Match the real host for every scenario that opens settings: the dialog
	// requests enough work-area space before exposing its complete controls.
	a.settingsWorkArea = func() (float32, float32) { return 1920, 1040 }
	a.expandSettings = func(w, h float32) { requestedSize = [2]int{int(math.Ceil(float64(w))), int(math.Ceil(float64(h)))} }
	settleSettings := func() {
		for i := 0; requestedSize != [2]int{} && i < 12; i++ {
			settingsSize, requestedSize = requestedSize, [2]int{}
			if settingsSize[0] > 1920 || settingsSize[1] > 1040 {
				t.Fatal("settings exceeds display work area")
			}
			tt.SetSize(settingsSize[0], settingsSize[1])
		}
		if requestedSize != [2]int{} {
			t.Fatal("settings resizing did not settle")
		}
	}
	locale := "en"
	if scenario == "shortcut-zh" {
		locale = "zh-Hans"
	}
	tt.SetLocale(locale)
	tx := locales.In(locale)
	await := func(done func() bool) {
		t.Helper()
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		// Kernel state and native layout settle independently. Keep the real
		// view's frame/timer loop running while waiting for visible controls.
		frames := time.NewTicker(time.Second / 60)
		defer frames.Stop()
		for !done() {
			select {
			case update := <-updates:
				update()
				tt.Frame()
				settleSettings()
			case <-frames.C:
				tt.Advance(time.Second / 60)
				settleSettings()
			case <-core.Done():
				t.Fatalf("original kernel disconnected: %v", core.Err())
			case <-deadline.C:
				t.Fatalf("original UI action did not settle: scenario=%s pending=%d snapshot=%+v errors=%v/%v/%v", scenario, a.corePending, a.coreSnapshot, a.serviceErr, a.openErr, a.diffErr)
			}
		}
		tt.Frame()
		tt.Advance(time.Second)
		settleSettings()
	}
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatalf("click %q: %v; texts=%q", label, err, tt.Texts())
		}
		tt.Advance(time.Second)
		settleSettings()
	}
	assertChanges := func(stage string) {
		t.Helper()
		if a.coreSnapshot == nil || a.coreSnapshot.Selected == nil || a.coreSnapshot.Selected.Section != "changes" || a.tab != changesTab {
			t.Fatalf("%s changed section without navigation: tab=%d snapshot=%+v texts=%q", stage, a.tab, a.coreSnapshot, tt.Texts())
		}
	}
	a.connect(repo)
	await(func() bool { return a.corePending == 0 && a.coreSnapshot != nil })
	if a.coreSnapshot.Welcome {
		// A production runtime has no test seed. Follow its actual Welcome
		// controls before the deferred initial project can become usable.
		click(tx.OnboardingSkip())
		await(func() bool {
			return a.corePending == 0 && a.coreSnapshot.Onboarding != nil && a.coreSnapshot.Onboarding.Step == "ConfigureGit" && !a.coreSnapshot.Onboarding.Loading
		})
		click(tx.OnboardingFinish())
	}
	await(func() bool {
		return a.ready && a.repo != nil && a.status != nil && len(a.status.Files) == 2 && a.corePending == 0
	})
	if a.backend != nil || a.generation != nil || a.coreSnapshot.Core != "desktop/desktop" || a.coreSnapshot.PresentationComplete {
		t.Fatal("native view used a reduced coordinator or claimed complete parity")
	}
	// This fixture has no remote: the retained upstream action is Publish.
	for _, action := range []string{tx.FetchUpdates(), tx.PullUpdates(), nativeDialogLabel(locale, "repositoryDialog.PublishRepository"), tx.GenerateMessage(), tx.SaveVersion()} {
		if !tt.HasText(action) {
			t.Fatalf("high-frequency entry missing: %q", action)
		}
	}
	if scenario == "preferences-fit" {
		a.showSettings(settingsInterface)
		await(func() bool {
			popup := a.coreSnapshot.CurrentPopup
			return a.corePending == 0 && popup != nil && len(popup.LayoutPages) == 9 && len(popup.Fields) > 0 && !popup.Fields[0].Disabled && popup.Fields[0].Value == "appearance" && !a.nativeDialogFormFor(popup).validating()
		})
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			tt.SetLocale(locale)
			for _, dark := range []bool{false, true} {
				click(nativeDialogChoiceLabel(locale, "settings.option.appearance"))
				await(func() bool {
					return a.corePending == 0 && a.coreSnapshot.CurrentPopup.Fields[0].Value == "appearance" && !a.nativeDialogFormFor(a.coreSnapshot.CurrentPopup).validating()
				})
				theme := "light"
				if dark {
					theme = "dark"
				}
				selector, ok := tt.AccessByRole(ui.RolePopUpButton, nativeDialogLabel(locale, "settings.theme"))
				if !ok {
					t.Fatal("theme selector missing")
				}
				tt.ClickAt(selector.Bounds.X+selector.Bounds.W/2, selector.Bounds.Y+selector.Bounds.H/2)
				click(nativeDialogChoiceLabel(locale, "settings.option."+theme))
				await(func() bool {
					return a.corePending == 0 && a.coreSnapshot.Appearance.Theme == theme && !a.nativeDialogFormFor(a.coreSnapshot.CurrentPopup).validating()
				})
				for _, scale := range []float32{1, 1.25, 1.5} {
					tt.SetPreferences(ui.Preferences{TextScale: scale})
					for _, size := range [][2]int{{900, 620}, {1440, 900}} {
						settingsSize = size
						tt.SetSize(size[0], size[1])
						settleSettings()
						var panelBounds ui.Rect
						for _, page := range []string{"accounts", "integrations", "copilot", "git", "appearance", "notifications", "prompts", "advanced", "accessibility"} {
							t.Logf("preferences %s/%v/%v/scale%g/%s", locale, dark, size, scale, page)
							click(nativeDialogChoiceLabel(locale, "settings.option."+page))
							await(func() bool {
								popup := a.coreSnapshot.CurrentPopup
								if popup != nil && popup.Type == "Error" {
									t.Fatalf("settings page %s: original error popup %+v", page, popup)
								}
								return a.corePending == 0 && popup != nil && popup.Type == "Preferences" && len(popup.Fields) > 0 && popup.Fields[0].Value == page && !a.nativeDialogFormFor(popup).validating()
							})
							if _, found := tt.AccessByRole(ui.RoleScroll, ""); found {
								t.Fatalf("%s/%s/%v settings has a scrollbar", locale, page, size)
							}
							for _, field := range a.coreSnapshot.CurrentPopup.Fields {
								if field.ID != "tab" {
									role := ui.RoleTextField
									if field.Kind == "select" {
										role = ui.RolePopUpButton
									}
									if field.Kind == "checkbox" {
										role = ui.RoleCheckBox
									}
									control, ok := tt.AccessByRole(role, nativeDialogLabel(locale, field.LabelKey))
									if !ok || control.Bounds.W <= 0 || control.Bounds.H <= 0 || control.Bounds.X < 0 || control.Bounds.Y < 0 || control.Bounds.X+control.Bounds.W > float32(settingsSize[0]) || control.Bounds.Y+control.Bounds.H > float32(settingsSize[1]) {
										t.Fatalf("%s/%s actual control not visible: %+v", locale, page, control)
									}
								}
							}
							visibleControl(t, tt, nativeDialogLabel(locale, "settings.save"), settingsSize[0], settingsSize[1])
							panel, ok := tt.AccessByRole(ui.RoleDialog, nativeDialogLabel(locale, "settings.title.Preferences"))
							if !ok {
								t.Fatal("settings dialog missing")
							}
							if panelBounds.W != 0 && panel.Bounds != panelBounds {
								t.Fatalf("settings resized between pages: %s %+v != %+v", page, panel.Bounds, panelBounds)
							}
							panelBounds = panel.Bounds
						}
						click(locales.In(locale).CustomProviders())
						click(locales.In(locale).AddProvider())
						if evidence := os.Getenv("GITOWL_NATIVE_LAYOUT_EVIDENCE"); evidence != "" {
							file, err := os.Create(filepath.Join(evidence, fmt.Sprintf("providers-%s-%v-%d-%g.png", locale, dark, size[0], scale)))
							if err != nil {
								t.Fatal(err)
							}
							if err := png.Encode(file, tt.Image()); err != nil {
								t.Fatal(err)
							}
							if err := file.Close(); err != nil {
								t.Fatal(err)
							}
						}
						if _, found := tt.AccessByRole(ui.RoleScroll, ""); found {
							t.Fatal("provider parameters still scroll")
						}
						for _, label := range []string{locales.In(locale).ProviderName(), locales.In(locale).ProviderFormat(), locales.In(locale).ProviderBaseURL(), locales.In(locale).ProviderAuthKind(), locales.In(locale).ProviderTimeout(), locales.In(locale).ProviderSecret(), locales.In(locale).SaveProvider()} {
							visibleControl(t, tt, label, settingsSize[0], settingsSize[1])
						}
						click(locales.In(locale).BackToProviders())
					}
				}
			}
		}
		click(locales.In("zh-Hant").Close())
		await(func() bool { return a.corePending == 0 && a.coreSnapshot.CurrentPopup == nil })
		return
	}
	click(tx.IncludeAll())
	await(func() bool { return a.corePending == 0 && len(a.included) == 0 })
	assertChanges("include-all checkbox")
	click(tx.IncludeFile("selected.txt"))
	await(func() bool { return a.corePending == 0 && a.included["selected.txt"] && !a.included["unselected.txt"] })
	assertChanges("single-file checkbox")
	click(tx.CommitSummary())
	tt.Type("Original kernel native commit")
	await(func() bool {
		return a.corePending == 0 && a.coreSnapshot.Selected.Message.Summary == "Original kernel native commit"
	})
	if scenario == "media" {
		exerciseOriginalKernelMedia(t, originalGenerationView{app: a, tester: tt, await: await, click: click, git: runGit, repo: repo})
		return
	}
	assertChanges("commit-summary typing")
	if scenario == "save-en" {
		// A context menu targets its own row, including keyboard invocation,
		// without changing the selected project's draft or refreshing its state.
		targetPath := filepath.Join(work, "menu-target")
		runGit("init", "--initial-branch=main", targetPath)
		a.runCoreTask(func() (backend.KernelResult, error) { return core.AddRepositories(a.ctx, []string{targetPath}) }, nil)
		await(func() bool { return a.corePending == 0 && len(a.coreSnapshot.RepositoryMenus) == 2 })
		// List items and their button share a label. Target the actionable
		// button rather than letting the text lookup pick its list wrapper.
		menuTarget, ok := tt.AccessByRole(ui.RoleButton, "menu-target")
		if !ok {
			t.Fatal("project menu target is missing")
		}
		tt.RightClickAt(menuTarget.Bounds.X+menuTarget.Bounds.W/2, menuTarget.Bounds.Y+menuTarget.Bounds.H/2)
		if a.repo.Path != repo {
			t.Fatal("opening project menu changed selection")
		}
		if err := tt.ChooseMenuItem(workspaceLabel(locale, "workspace.createAlias")); err != nil {
			t.Fatal(err)
		}
		await(func() bool {
			return a.corePending == 0 && a.coreSnapshot.CurrentPopup != nil && a.coreSnapshot.CurrentPopup.Type == "ChangeRepositoryAlias"
		})
		if a.repo.Path != repo || a.commitSummary != "Original kernel native commit" {
			t.Fatal("project menu lost current project or draft")
		}
		click(nativeDialogLabel(locale, "repositoryDialog.cancel"))
		await(func() bool { return a.corePending == 0 && a.coreSnapshot.CurrentPopup == nil })
		click(workspaceLabel(locale, "workspace.searchProjects"))
		tt.Key(0, ui.KeyDown)
		tt.Key(0, ui.KeyDown)
		tt.Key(ui.Shift, ui.KeyF10)
		if !tt.HasText(workspaceLabel(locale, "workspace.copyRepoPath")) {
			t.Fatal("keyboard project context menu missing")
		}
		tt.Key(0, ui.KeyEscape)
		// Exercise original menu admission, native typing and the real Dispatcher
		// filter. Empty results must not discard the current project or its draft.
		a.runCoreTask(func() (backend.KernelResult, error) { return core.ExecuteMenu(a.ctx, "show-repository-list") }, nil)
		searchLabel := workspaceLabel(locale, "workspace.searchProjects")
		await(func() bool { return a.corePending == 0 && tt.Focused(searchLabel) })
		tt.Type("no-such-project")
		await(func() bool {
			return a.corePending == 0 && a.coreSnapshot.RepositoryFilter != nil && a.coreSnapshot.RepositoryFilter.Text == "no-such-project"
		})
		if !tt.HasText(workspaceLabel(locale, "workspace.noProjectsFound")) || a.repo.Path != repo || a.commitSummary != "Original kernel native commit" {
			t.Fatal("project filtering lost its empty state, current selection or draft")
		}
		tt.Key(0, ui.KeyEscape)
		await(func() bool { return a.corePending == 0 && a.coreSnapshot.RepositoryFilter.Text == "" })
		tt.Type("prj")
		await(func() bool { return a.corePending == 0 && a.coreSnapshot.RepositoryFilter.Text == "prj" })
		if len(a.coreSnapshot.RepositoryFilter.Paths) != 1 || a.coreSnapshot.RepositoryFilter.Paths[0] != repo {
			t.Fatalf("original fuzzy result missing: %+v", a.coreSnapshot.RepositoryFilter)
		}
		tt.Key(0, ui.KeyDown)
		tt.Key(0, ui.KeyEnter)
		await(func() bool { return a.corePending == 0 && a.repo.Path == repo })
		click(searchLabel)
		tt.Key(0, ui.KeyEscape)
		await(func() bool { return a.corePending == 0 && a.coreSnapshot.RepositoryFilter.Text == "" })
		for _, history := range []bool{false, true} {
			if history {
				a.navigateWorkspace(historyTab)
				await(func() bool { return a.corePending == 0 && a.tab == historyTab })
			}
			a.runCoreTask(func() (backend.KernelResult, error) { return core.ExecuteMenu(a.ctx, "go-to-commit-message") }, nil)
			await(func() bool {
				return a.corePending == 0 && a.tab == changesTab && !a.coreSnapshot.FocusCommitMessage && tt.Focused(tx.CommitSummary())
			})
			tt.Key(0, ui.KeyEnd)
			tt.Type("!")
			await(func() bool {
				return a.coreDraftPending == 0 && a.coreSnapshot.Selected.Message.Summary == "Original kernel native commit!"
			})
			tt.Key(0, ui.KeyBackspace)
			await(func() bool {
				return a.coreDraftPending == 0 && a.coreSnapshot.Selected.Message.Summary == "Original kernel native commit"
			})
		}
	}
	click(tx.OpenFile("selected.txt"))
	await(func() bool { return a.corePending == 0 && a.preview != nil && !a.loadingDiff })
	if !a.reviewingFile {
		t.Fatal("file review did not use the production canvas")
	}
	if scenario == "save-en" {
		tt.SetSize(900, 620)
		if r, ok := tt.Find(tx.DiffTitle()); !ok || r.H < 200 {
			t.Fatalf("minimum-window review has no usable code viewport: %+v", r)
		}
		click(tx.CommitSummary())
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(strings.Repeat("Long review summary ", 8))
		await(func() bool {
			return a.coreDraftPending == 0 && a.nativeCommitView() != nil && a.nativeCommitView().SummaryLengthWarning
		})
		// Description is expanded by default. Fill it directly, then exercise
		// the explicit collapse without changing the stored original draft.
		click(tx.VersionDescription())
		tt.Type("逐项审阅已选改动。\n生成只填入草稿，不自动提交或推送。\n原作者、钩子与恢复仍由原内核负责。")
		await(func() bool { return a.coreDraftPending == 0 })
		if r, ok := tt.Find(tx.DiffTitle()); !ok || r.H < 160 {
			t.Fatalf("long summary/description hid minimum-window review: %+v", r)
		}
		click(tx.CommitDescription())
		click(tx.CommitSummary())
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type("Original kernel native commit")
		await(func() bool { return a.coreDraftPending == 0 })
		tt.SetSize(1180, 760)
		// Exercise the actual text-editor menu, not the outer DiffView edge.
		// The inner MyGo menu must retain editing commands and append the
		// original workspace commands without replaying a build callback.
		openDiffMenu := func() {
			t.Helper()
			rect, ok := tt.Find(tx.DiffTitle())
			if !ok {
				t.Fatal("real diff view is missing")
			}
			tt.RightClickAt(rect.X+min(140, rect.W/4), rect.Y+min(48, rect.H/2))
			for _, label := range []string{"Copy", "Select All", nativeDialogLabel(locale, "workspace.expandWhole"), nativeDialogLabel(locale, "workspace.discardLines"), nativeDialogLabel(locale, "workspace.checkMarks")} {
				found := false
				for _, item := range tt.Menu() {
					if item == label {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("inner editor menu is missing %q: %v", label, tt.Menu())
				}
			}
		}
		checkMarks := a.coreSnapshot.Workspace.CheckMarks
		openDiffMenu()
		if err := tt.ChooseMenuItem(nativeDialogLabel(locale, "workspace.checkMarks")); err != nil {
			t.Fatal(err)
		}
		await(func() bool { return a.corePending == 0 && a.coreSnapshot.Workspace.CheckMarks != checkMarks })
		assertChanges("diff menu checkbox")
		openDiffMenu()
		if err := tt.ChooseMenuItem(nativeDialogLabel(locale, "workspace.checkMarks")); err != nil {
			t.Fatal(err)
		}
		await(func() bool { return a.corePending == 0 && a.coreSnapshot.Workspace.CheckMarks == checkMarks })
	}
	click(tx.CollapseDiff())
	if a.commitSummary != "Original kernel native commit" || !a.included["selected.txt"] {
		t.Fatal("review/back lost original draft or inclusion")
	}
	if scenario == "generation" {
		exerciseOriginalKernelGeneration(t, originalGenerationView{app: a, tester: tt, await: await, click: click, git: runGit, repo: repo})
		return
	}
	if scenario == "shortcut-zh" {
		click(tx.CommitSummary())
		tt.Key(ui.Ctrl, ui.KeyEnter)
		tt.Key(ui.Ctrl, ui.KeyEnter)
	} else {
		click(tx.SaveVersion())
	}
	if strings.HasPrefix(scenario, "hook-") {
		await(func() bool {
			return a.coreSnapshot.CurrentPopup != nil && (a.coreSnapshot.CurrentPopup.Type == "HookFailed" || a.coreSnapshot.CurrentPopup.Type == "Error")
		})
		if a.coreSnapshot.CurrentPopup.Type == "Error" {
			// Upstream production defaults leave the hooks environment disabled.
			// A normal Git hook rejection is then an Error, not an ignorable
			// proxy-hook dialog. Verify that path instead of forcing beta flags.
			await(func() bool {
				return a.corePending == 0 && !a.savingVersion && !a.coreSnapshot.Selected.CommitInProgress
			})
			if !strings.Contains(a.coreSnapshot.CurrentPopup.Content.Message, "native-original-hook-output") ||
				runGit("-C", repo, "rev-list", "--count", "HEAD") != "1" ||
				a.commitSummary != "Original kernel native commit" || !a.included["selected.txt"] {
				t.Fatalf("original production hook failure lost its safe outcome: %+v", a.coreSnapshot.CurrentPopup)
			}
			if tt.HasText(tx.PopupIgnoreHook()) {
				t.Fatal("the native frontend invented an ignore choice absent from the original production flow")
			}
			return
		}
		if !a.savingVersion || core.InFlight() == 0 {
			t.Fatal("original hook continuation was not still awaiting a decision")
		}
		diagnostic, visible := tt.Access(tx.PopupTerminalOutput())
		if !visible || !strings.Contains(diagnostic.Value, "native-original-hook-output") {
			t.Fatalf("real hook diagnostics did not reach the MyGo accessible view: %+v; texts=%q", diagnostic, tt.Texts())
		}
		if scenario == "hook-ignore" {
			click(tx.PopupIgnoreHook())
		} else {
			click(tx.PopupAbort())
		}
	}
	await(func() bool {
		return a.corePending == 0 && !a.savingVersion && !a.coreSnapshot.Selected.CommitInProgress
	})
	if scenario == "hook-abort" {
		if runGit("-C", repo, "rev-list", "--count", "HEAD") != "1" || a.commitSummary != "Original kernel native commit" || !a.included["selected.txt"] {
			t.Fatal("aborting the original hook committed or lost the draft")
		}
		return
	}
	if runGit("-C", repo, "rev-list", "--count", "HEAD") != "2" || runGit("-C", repo, "show", "--format=", "--name-only", "HEAD") != "selected.txt" {
		t.Fatal("native action did not make exactly one original selected commit")
	}
	if runGit("-C", repo, "diff", "--name-only") != "unselected.txt" {
		t.Fatal("the original commit staged an unselected file")
	}
	if a.saveErr != nil || a.commitSummary != "" || a.lastSavedSHA != runGit("-C", repo, "rev-parse", "HEAD") {
		t.Fatalf("UI confirmation did not come from original state: %v %q %q", a.saveErr, a.commitSummary, a.lastSavedSHA)
	}
	click(tx.History())
	await(func() bool { return a.corePending == 0 && a.tab == historyTab && len(a.commits) > 0 })
	if len(a.commits) != 2 || a.commits[0].SHA != runGit("-C", repo, "rev-parse", "HEAD") {
		t.Fatalf("entering history did not refresh the original comparison after saving: %+v", a.commits)
	}
	clickHistoryRecordTitle(t, tt, a.commits[0].Summary, 0)
	tt.Advance(time.Second)
	await(func() bool {
		_, countVisible := tt.AccessByRole(ui.RoleButton, tx.FileCount(1))
		return countVisible && a.corePending == 0 && a.commitIndex >= 0 && a.changeset != nil && len(a.changeset.Files) > 0
	})
	if a.viewingCommit || tt.HasText(tx.OpenFile("selected.txt")) {
		t.Fatal("selecting history must retain the record list with file names collapsed")
	}
	// The count expands this record in place; no separate details page.
	count, ok := tt.AccessByRole(ui.RoleButton, tx.FileCount(1))
	if !ok || count.Expanded {
		t.Fatalf("collapsed file count missing: %+v", count)
	}
	tt.ClickAt(count.Bounds.X+count.Bounds.W/2, count.Bounds.Y+count.Bounds.H/2)
	tt.Frame()
	await(func() bool {
		_, visible := tt.AccessByRole(ui.RoleButton, tx.OpenFile("selected.txt"))
		return a.corePending == 0 && visible
	})
	if a.viewingCommit {
		t.Fatal("file count left the history list instead of expanding its record")
	}
	if scenario == "save-en" {
		if err := tt.RightClick(tx.OpenFile(a.changeset.Files[0].Path)); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, label := range tt.Menu() {
			if label == nativeDialogLabel(locale, "workspace.copyPath") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("central history file review lost the original context menu: %v", tt.Menu())
		}
		tt.CloseMenu()
	}
	click(tx.OpenFile(a.changeset.Files[0].Path))
	await(func() bool { return a.corePending == 0 && a.preview != nil && !a.loadingDiff })
	if !a.reviewingFile || a.tab != historyTab {
		t.Fatal("original history/file/diff selection was not presented")
	}
}
