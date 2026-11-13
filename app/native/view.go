package main

import (
	"path"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// view is the production workspace, shared by real windows and ui.NewTester.
// A file review replaces the overview; it never adds another permanent pane.
func (a *app) view(c *ui.Context) {
	a.beginCorePresentation()
	a.applyCoreZoom(c)
	a.applyCoreAppearance(c)
	colors := setWorkspaceTheme(c)
	tx := locales.In(c.Locale())
	if a.core != nil && a.coreSnapshot != nil && a.coreSnapshot.Welcome && a.coreSnapshot.Onboarding != nil {
		renderKernelOnboarding(c, tx, a.coreSnapshot.Onboarding, a.coreSnapshot.SignIn, &a.coreOnboardingForm, a.onboardingActions())
		a.coreAuthenticationView(c, tx)
		if a.syncMenu != nil {
			a.syncMenu()
		}
		return
	}
	width, height := c.Size()
	verticalPadding := float32(20)
	if height < 700 {
		verticalPadding = 12
	}
	padding := float32(32)
	if width < 1100 {
		padding = 24
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).FontWeight(400).Children(func() {
		a.projectRail(c, tx, colors)
		ui.Column(c.Key("workspace-canvas")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			a.workspaceTitleBar(c, tx, padding)
			if a.repo != nil {
				a.workspaceTaskBar(c, tx, colors, padding)
			}
			ui.Column(c.Key("workspace-content")).Grow(1).MinHeight(0).FillWidth().MaxWidth(1440).
				AlignSelf(ui.Center).Padding(verticalPadding, padding, 16, padding).Children(func() {
				a.workspaceNotices(c, tx)
				ui.Column(c.Key("workspace-page")).Grow(1).MinHeight(0).FillWidth().Children(func() {
					switch {
					case a.repo == nil:
						a.noProject(c, tx)
					case a.workspaceStashView(c):
					case a.reviewingFile:
						a.diffPane(c, tx, min(1440, width-a.nativeRailWidth(c))-2*padding)
					case a.tab == historyTab:
						a.historyPage(c, tx)
					default:
						a.changesOverview(c, tx, colors)
					}
				})
				// The single composer stays at the bottom of the same canvas in
				// overview and review, with stable input identities and draft.
				if a.repo != nil && a.tab == changesTab && a.commitCapable {
					a.commitForm(c, tx)
				}
			})
		})
	})
	a.openDialog(c, tx)
	a.settingsDialog(c, tx)
	a.generationConsentDialog(c, tx)
	a.pollCopilotLogin(c)
	a.coreAuthenticationView(c, tx)
	a.finishCorePresentation(c)
	if a.syncMenu != nil {
		a.syncMenu()
	}
}

func (a *app) projectRail(c *ui.Context, tx locales.Messages, colors workspaceColors) {
	bar := c.TitleBar()
	ui.Column(c.Key("project-rail")).Width(a.nativeRailWidth(c)).Shrink(0).FillHeight().Background(colors.rail).Children(func() {
		ui.Row(c.Key("brand-titlebar")).Height(max(48, bar.Height)).Padding(0, 16, 0, 20+bar.Left).
			Gap(10).DragWindow().Children(func() {
			if a.logo != nil {
				ui.Image(c, a.logo).Size(24, 24).Fit(ui.Contain).Label(tx.AppName()).Shrink(0)
			}
			ui.Text(c, tx.AppName()).FontSize(c.Theme().Rem(17.0 / 15)).FontWeight(500).NoWrap()
		})
		ui.Column(c).Grow(1).MinHeight(0).Padding(12, 12, 0, 12).Gap(4).Children(func() {
			quietButton(c.Key("open-repository"), tx.OpenRepository(), "plus").Height(36).FillWidth().Justify(ui.Start).
				Disabled(!a.ready || a.savingVersion).OnClick(a.showOpen)
			a.coreProjectList(c, tx, colors)
			if a.repo != nil {
				// Native launches already show the authoritative project list.
				// A second current-project heading would duplicate context and
				// push the task navigation away from the chosen project.
				if a.core == nil {
					ui.Row(c.Key("current-project")).Height(44).Padding(0, 10).Margin(20, 0, 8, 0).Gap(10).
						Tooltip(a.repo.Path).Children(func() {
						ui.Icon(c, icons["folder-open"]).FontSize(18).Shrink(0).TextColor(c.Theme().TextMuted)
						ui.Text(c, a.repo.Name).FontWeight(500).SingleLine().Grow(1).MinWidth(0)
					})
				}
			}
		})
		ui.Column(c).Padding(16).Gap(12).Children(func() {
			if a.core == nil && !a.commitCapable {
				ui.Text(c, tx.ReadOnly()).FontSize(c.Theme().Rem(14.0/15)).TextColor(c.Theme().TextMuted).Padding(0, 10)
			}
			quietButton(c.Key("settings"), tx.Settings(), "settings").Height(40).FillWidth().Justify(ui.Start).
				OnClick(func() { a.showSettings(settingsInterface) })
		})
	})
}

// Task navigation belongs to the selected project. Keeping it out of the
// repository collection gives each selection one unambiguous meaning.
func (a *app) workspaceTaskBar(c *ui.Context, tx locales.Messages, colors workspaceColors, padding float32) {
	ui.Column(c.Key("workspace-taskbar")).FillWidth().Shrink(0).Padding(8, padding, 8, padding).
		BorderWidth(0, 0, 1, 0).BorderColor(colors.hairline).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(4).Children(func() {
			for _, nav := range []struct {
				label string
				tab   int
			}{{tx.Changes(), changesTab}, {tx.History(), historyTab}} {
				button := ui.ButtonBase(c.Key(nav.tab)).Height(36).Padding(8, 12).Gap(8).Label(nav.label)
				quietSurface(c, button)
				if a.tab == nav.tab {
					button.Background(colors.selected).FontWeight(500).Value(tx.CurrentPage())
				}
				button.Children(func() {
					ui.Text(c, nav.label).NoWrap()
					if nav.tab == changesTab && a.status != nil {
						ui.Text(c, i18n.FormatNumber(c.Locale(), len(a.status.Files), i18n.NumberOptions{})).FontSize(c.Theme().Rem(14.0 / 15)).FontFeatures("tnum").TextColor(c.Theme().TextMuted)
					}
				}).OnClick(func() { a.navigateWorkspace(nav.tab) })
			}
			ui.Spacer(c)
			if a.core != nil {
				a.coreRemoteControls(c, tx)
			}
		})
	})
}

func (a *app) workspaceTitleBar(c *ui.Context, tx locales.Messages, padding float32) {
	bar := c.TitleBar()
	// MyGo/system owns the caption buttons. The entire otherwise quiet top
	// band drags the window, and real controls stay clear of its reserved room.
	ui.Row(c.Key("workspace-titlebar")).Height(max(48, bar.Height)).Shrink(0).
		Padding(0, bar.Right+padding, 0, padding).Gap(12).DragWindow().Children(func() {
		if a.repo != nil {
			ui.Text(c, a.repo.Name).SingleLine().MinWidth(0).MaxWidth(260).Shrink(1).FontWeight(500).Tooltip(a.repo.Path)
			if a.core != nil {
				a.workspaceTitleControls(c)
			}
			ui.Spacer(c)
			if a.status != nil && a.core == nil {
				branch := tx.NoBranch()
				if a.status.Branch != nil {
					branch = *a.status.Branch
				}
				anchor := ui.ButtonBase(c.Key("branch-details")).Height(32).MaxWidth(200).Shrink(1).
					Padding(6, 8).Gap(8).Label(tx.BranchDetails()).Tooltip(branch)
				quietSurface(c, anchor).Children(func() {
					ui.Icon(c, icons["git-branch"]).FontSize(18).Shrink(0)
					ui.Text(c, branch).SingleLine().MinWidth(0).Grow(1)
				}).OnClick(func() { a.branchDetailsOpen = !a.branchDetailsOpen })
				ui.Popover(c.Key("branch-popover"), anchor, &a.branchDetailsOpen, func() {
					label := tx.NoBranch()
					if a.status.Branch != nil {
						label = tx.BranchLabel(*a.status.Branch)
					}
					ui.Text(c, label).FontWeight(500)
					if a.status.Upstream != nil {
						ui.Text(c, tx.UpstreamLabel(*a.status.Upstream, a.status.Ahead, a.status.Behind)).TextColor(c.Theme().TextMuted)
					}
					if a.status.Operation != nil && *a.status.Operation != "" {
						ui.Text(c, operationLabel(tx, *a.status.Operation)).TextColor(c.Theme().Warning)
					}
					if a.status.Conflicted {
						ui.Text(c, tx.Conflicted()).TextColor(c.Theme().Danger)
					}
				}).Width(320).Padding(16).Gap(12)
			}
			if a.core == nil {
				iconButton(c.Key("refresh"), tx.Refresh(), "refresh-cw").Disabled(!a.ready || a.composerLocked() || a.opening || a.loadingStatus).OnClick(a.refresh)
			}
		} else {
			ui.Spacer(c)
		}
	})
}

func (a *app) workspaceNotices(c *ui.Context, tx locales.Messages) {
	if a.nativeWorkspace() != nil {
		a.workspaceOverlayView(c)
		a.workspaceConflictControls(c)
	}
	a.quitPendingView(c, tx)
	if a.serviceErr != nil {
		showError(c, tx.FailedService(a.serviceErr.Error()))
	} else if a.coreSnapshot != nil && a.coreSnapshot.Welcome {
		ui.Text(c, tx.WelcomePending()).TextColor(c.Theme().Warning).Margin(0, 0, 16, 0).Role(ui.RoleStatus)
	} else if !a.ready {
		label := tx.Connecting()
		if a.core != nil {
			label = tx.KernelConnecting()
		}
		ui.Text(c, label).TextColor(c.Theme().TextMuted).Margin(0, 0, 16, 0).Role(ui.RoleStatus)
	}
	if a.logoErr != nil {
		showError(c, tx.FailedLogo(a.logoErr.Error()))
	}
	if a.openErr != nil && !a.openModal {
		showError(c, tx.FailedOpen(a.openErr.Error()))
	}
}

func showError(c *ui.Context, message string) {
	// Native read-only text areas keep long diagnostics available without
	// pushing the task's controls beyond the minimum window height.
	ui.TextArea(c, &message).ReadOnly(true).FillWidth().Lines(1, 3).BorderWidth(0).
		Background(c.Theme().Background).TextColor(c.Theme().Danger).Margin(0, 0, 12, 0).Label(message)
}

func emptyState(c *ui.Context, message string) { emptyStateIcon(c, "", message) }

// A resting state of a whole page gets one quiet icon, so an empty canvas
// reads as an answer rather than as missing content.
func emptyStateIcon(c *ui.Context, icon, message string) {
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().Center().Padding(24).Gap(12).Children(func() {
		if icon != "" {
			ui.Icon(c, icons[icon]).FontSize(28).TextColor(c.Theme().TextMuted.Alpha(0.7))
		}
		ui.Text(c, message).TextColor(c.Theme().TextMuted)
	})
}

func (a *app) noProject(c *ui.Context, tx locales.Messages) {
	if a.coreSnapshot != nil && a.coreSnapshot.CloningRepository != nil {
		clone := a.coreSnapshot.CloningRepository
		taskTitle(c, tx.CloningProject(clone.Repository.Name), "")
		ui.Column(c).Grow(1).FillWidth().Justify(ui.Center).Gap(16).Padding(24).Children(func() {
			progress := clone.Progress.Value
			if progress == 0 {
				progress = -1
			}
			ui.Progress(c, progress).FillWidth().Label(tx.CloningProject(clone.Repository.Name))
			if clone.Progress.Description != nil {
				ui.Text(c, *clone.Progress.Description).FillWidth().TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
			}
		})
		return
	}
	if a.missingRepositoryView(c) {
		return
	}
	taskTitle(c, tx.Projects(), "")
	ui.Column(c).Grow(1).Center().Gap(16).Padding(24).Children(func() {
		message := tx.NoRepository()
		if a.opening {
			message = tx.LoadingRepository()
		}
		ui.Text(c, message).TextColor(c.Theme().TextMuted)
		quietButton(c.Key("empty-open"), tx.ChooseProject(), "folder-open").Disabled(!a.ready || a.opening).
			OnClick(a.showOpen)
	})
}

func (a *app) changesOverview(c *ui.Context, tx locales.Messages, colors workspaceColors) {
	// The selected tab already names this page and shows its file count.
	a.workspaceNavigationView(c)
	if a.statusErr != nil {
		showError(c, tx.FailedStatus(a.statusErr.Error()))
	}
	if a.loadingStatus {
		ui.Text(c, tx.LoadingStatus()).TextColor(c.Theme().TextMuted).Margin(0, 0, 12, 0).Role(ui.RoleStatus)
	}
	if a.status == nil {
		ui.Spacer(c)
		return
	}
	if len(a.status.Files) == 0 {
		if !a.loadingStatus && a.statusErr == nil {
			emptyStateIcon(c, "circle-check", tx.NoChanges())
		} else {
			ui.Spacer(c)
		}
		return
	}
	ui.Row(c).Height(36).Gap(16).Margin(0, 0, 4, 0).Children(func() {
		if a.commitCapable {
			all := a.allFileCheckState()
			// The native checkbox acts on the complete real collection, not
			// just the virtualized rows MyGo has built in this viewport.
			ui.CheckboxState(c.Key("include-all"), &all, tx.IncludeAll()).Disabled(a.composerLocked() || !a.ready).
				OnChange(func() { a.changeAllIncluded(all == ui.CheckOn) })
		}
		ui.Spacer(c)
		// With saving available, the composer beside Save states the scope
		// and each row's chevron says how to open it.
		if !a.commitCapable {
			ui.Text(c, tx.ReviewFilesHint()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).NoWrap()
		}
	})
	a.fileList(c, tx, a.status.Files, a.changesList.list(), colors)
}

func (a *app) fileList(c *ui.Context, tx locales.Messages, files []backend.File, state *ui.ListState, colors workspaceColors) {
	if a.workspaceChangesList(c) {
		return
	}
	state.Key = func(i int) any { return files[i].Path }
	// Rows do not select themselves: including a whole file and opening it
	// are separate native controls, never a blue List selection side effect.
	state.Selected, state.Label = nil, nil
	ui.List(c.Key("files"), state, len(files), func(i int) {
		file := files[i]
		row := ui.Row(c).Height(48).Padding(0, 4).Gap(4).Radius(8)
		if a.tab == changesTab && a.included[file.Path] {
			row.Background(colors.selected.Alpha(0.55))
		}
		row.Children(func() {
			if a.tab == changesTab && a.commitCapable {
				included := a.fileCheckState(file.Path)
				ui.CheckboxState(c.Key("include-file"), &included, "").Size(32, 32).Padding(8).
					Label(tx.IncludeFile(file.Path)).Tooltip(tx.IncludeFile(file.Path)).Disabled(a.composerLocked() || !a.ready).
					OnChange(func() { a.changeIncluded(file.Path, included == ui.CheckOn) })
			}
			button := ui.ButtonBase(c.Key("open-file")).Height(48).Grow(1).Shrink(1).MinWidth(0).
				Padding(8, 10).Gap(12).Label(tx.OpenFile(file.Path)).Tooltip(file.Path).Disabled(!a.ready)
			quietSurface(c, button).Children(func() {
				ui.Icon(c, icons[fileIcon(file.Path, file.IsSubmodule)]).FontSize(18).Shrink(0).TextColor(c.Theme().TextMuted)
				ui.Text(c, file.Path).SingleLine().Grow(1).MinWidth(0)
				fileStatusPill(c, tx, file.Status, file.IsSubmodule)
				ui.Icon(c, icons["chevron-right"]).FontSize(18).Shrink(0).TextColor(c.Theme().TextMuted)
			}).OnClick(func() { a.reviewFile(i) })
		})
	}).Grow(1).MinHeight(0).FillWidth().Gap(4).Padding(8, 0, 0, 0).
		BorderWidth(1, 0, 0, 0).BorderColor(colors.hairline).Label(tx.ChangedFiles())
}

func (a *app) historyPage(c *ui.Context, tx locales.Messages) {
	if a.nativeWorkspace() != nil {
		a.workspaceHistoryPage(c)
		return
	}
	if a.viewingCommit && a.commitIndex >= 0 && a.commitIndex < len(a.commits) {
		quietButton(c.Key("back-to-history"), tx.BackToHistory(), "arrow-left").AlignSelf(ui.Start).Margin(0, 0, 16, 0).
			OnClick(func() { a.viewingCommit = false })
		commit := a.commits[a.commitIndex]
		taskTitle(c, commit.Summary, "")
		ui.Text(c, commitDetails(c, tx, commit)).TextColor(c.Theme().TextMuted).Margin(0, 0, 16, 0)
		if commit.Body != "" {
			ui.TextArea(c.Key(commit.SHA), &commit.Body).ReadOnly(true).FillWidth().Lines(1, 3).
				Background(c.Theme().Background).BorderWidth(0).Label(tx.VersionDescription()).Margin(0, 0, 12, 0)
		}
		if len(commit.Tags) > 0 {
			ui.Text(c, tx.CommitTags(strings.Join(commit.Tags, ", "))).TextColor(c.Theme().TextMuted).Margin(0, 0, 12, 0)
		}
		if a.loadingCommit {
			ui.Text(c, tx.LoadingChangeset()).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
		}
		if a.commitErr != nil {
			showError(c, tx.FailedChangeset(a.commitErr.Error()))
		}
		if a.changeset == nil {
			ui.Spacer(c)
			return
		}
		ui.Row(c).Gap(12).Margin(12, 0, 12, 0).Children(func() {
			ui.Text(c, tx.FileCount(len(a.changeset.Files))).FontWeight(500).Grow(1)
			ui.Text(c, tx.CommitStats(a.changeset.LinesAdded, a.changeset.LinesDeleted)).TextColor(c.Theme().TextMuted)
		})
		if len(a.changeset.Files) == 0 {
			emptyState(c, tx.NoCommitFiles())
		} else {
			a.fileList(c, tx, a.changeset.Files, a.commitFilesList.list(), workspaceColors{hairline: c.Theme().Border})
		}
		return
	}
	a.workspaceNavigationView(c)
	if a.loadingHistory {
		ui.Text(c, tx.LoadingHistory()).TextColor(c.Theme().TextMuted).Margin(0, 0, 12, 0).Role(ui.RoleStatus)
	}
	if a.historyErr != nil {
		showError(c, tx.FailedHistory(a.historyErr.Error()))
	}
	if a.nativeWorkspace() != nil {
		a.workspaceHistoryList(c)
		return
	}
	commits := a.commits
	a.commitsList.list().Key = func(i int) any { return commits[i].SHA }
	a.commitsList.list().Selected, a.commitsList.list().Label = nil, nil
	ui.List(c.Key("commits"), a.commitsList.list(), len(commits), func(i int) {
		button := ui.ButtonBase(c).Height(68).FillWidth().Padding(10, 12).Gap(16).Label(commits[i].Summary).Disabled(!a.ready)
		quietSurface(c, button).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
				ui.Text(c, commits[i].Summary).SingleLine().FontWeight(500)
				ui.Text(c, commitDetails(c, tx, commits[i])).SingleLine().FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
			})
			ui.Icon(c, icons["chevron-right"]).FontSize(18).TextColor(c.Theme().TextMuted)
		}).OnClick(func() { a.openVersion(i) })
	}).Grow(1).MinHeight(0).FillWidth().Gap(4).Label(tx.SavedVersions())
	if a.historyLoaded && len(commits) == 0 && a.historyErr == nil {
		emptyState(c, tx.NoHistory())
	}
	if a.historyMore {
		quietButton(c.Key("load-history"), tx.LoadMore(), "chevron-down").AlignSelf(ui.Center).Margin(16, 0, 0, 0).
			Disabled(a.loadingHistory || !a.ready).OnClick(func() { a.loadHistory(true) })
	}
}

func (a *app) diffPane(c *ui.Context, tx locales.Messages, width float32) {
	file, _, selected := a.currentFile()
	_, height := c.Size()
	compact := height < 700
	label := tx.DiffTitle()
	if selected {
		label = file.Path
	}
	back := tx.BackToOverview()
	if a.tab == historyTab {
		back = tx.BackToVersion()
	}
	ui.Row(c).Gap(12).Margin(0, 0, 8, 0).Children(func() {
		iconButton(c.Key("back-to-overview"), back, "arrow-left").OnClick(a.backFromReview)
		// The file name leads; its folder is quiet context and its change size
		// follows, as in the file list the review was opened from.
		ui.Row(c.Key("review-title")).Grow(1).MinWidth(0).Gap(10).AlignItems(ui.Center).Children(func() {
			name, directory := label, ""
			if selected {
				name = path.Base(file.Path)
				if d := path.Dir(file.Path); d != "." {
					directory = d
				}
			}
			ui.Text(c, name).FontSize(c.Theme().Rem(18.0 / 15)).FontWeight(500).SingleLine().Shrink(1).MinWidth(0).Tooltip(label).Role(ui.RoleHeading).Level(1).Label(label)
			if directory != "" {
				ui.Text(c, directory).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).SingleLine().Shrink(2).MinWidth(0).Tooltip(label)
			}
			if added, deleted := patchStats(a.preview); added+deleted > 0 && !a.loadingDiff {
				changeStats(c, tx, added, deleted)
			}
			ui.Spacer(c)
		})
		if a.tab == changesTab && a.commitCapable && selected {
			included := a.fileCheckState(file.Path)
			ui.CheckboxState(c.Key("include-reviewed-file"), &included, tx.IncludeForSave()).Label(tx.IncludeFile(file.Path)).
				Disabled(a.composerLocked() || !a.ready).OnChange(func() { a.changeIncluded(file.Path, included == ui.CheckOn) })
		}
		if compact {
			a.nativeDiffControls(c, true)
		}
		if a.preview != nil && len(a.preview.patches) > 0 {
			anchor := iconButton(c.Key("review-options"), tx.ReviewOptions(), "ellipsis").
				OnClick(func() { a.reviewOptionsOpen = !a.reviewOptionsOpen })
			ui.Popover(c.Key("review-options-popover"), anchor, &a.reviewOptionsOpen, func() {
				ui.Text(c, tx.ReviewOptions()).FontWeight(500)
				if width >= 760 {
					layout := a.diffLayout
					if w := a.nativeWorkspace(); w != nil {
						layout = int(editor.DiffUnified)
						if w.SideBySide {
							layout = int(editor.DiffSplit)
						}
					}
					ui.Segmented(c.Key("diff-layout"), &layout, tx.SideBySide(), tx.Unified()).Label(tx.DiffLayout()).OnChange(func() {
						if a.nativeWorkspace() != nil {
							a.workspaceAction(backend.KernelWorkspaceAction{Type: "diff.option", Option: "sideBySide", Value: layout == int(editor.DiffSplit)})
						} else {
							a.diffLayout = layout
						}
					})
				} else {
					ui.Text(c, tx.NarrowDiffLayout()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
				}
				ui.Checkbox(c.Key("wrap-lines"), &a.wrap, tx.WrapLines())
			}).Width(280).Padding(16).Gap(16)
		}
	})
	if selected && file.OldPath != "" {
		ui.Text(c, tx.PreviousPath(file.OldPath)).TextColor(c.Theme().TextMuted).Margin(0, 0, 12, 0)
	}
	if a.diffErr != nil {
		showError(c, tx.FailedDiff(a.diffErr.Error()))
		ui.Spacer(c)
	} else if a.loadingDiff {
		emptyState(c, tx.LoadingDiff())
	} else if a.preview != nil {
		// Count only the actual central canvas, excluding the project rail
		// and page padding, when choosing the responsive native diff layout.
		a.previewView(c, tx, width)
	} else {
		emptyState(c, tx.SelectFile())
	}
}

func (a *app) previewView(c *ui.Context, tx locales.Messages, width float32) {
	p := a.preview
	switch p.kind {
	case "text", "large-text":
		if len(p.patches) == 0 {
			message := tx.DiffEmpty()
			if p.kind == "large-text" {
				message = tx.DiffLarge()
			}
			emptyState(c, message)
			return
		}
		if _, height := c.Size(); height >= 700 {
			a.nativeDiffControls(c, false)
		}
		layout := editor.DiffLayout(a.diffLayout)
		if w := a.nativeWorkspace(); w != nil {
			layout = editor.DiffUnified
			if w.SideBySide {
				layout = editor.DiffSplit
			}
		}
		if width < 760 {
			layout = editor.DiffUnified
		}
		// Split columns need their before/after names; the unified layout's
		// red and green lines explain themselves.
		if layout == editor.DiffSplit {
			ui.Row(c.Key("diff-context")).Shrink(0).FillWidth().MinHeight(24).Margin(0, 0, 8, 0).Children(func() {
				if layout == editor.DiffSplit {
					for _, label := range []string{tx.ImageBefore(), tx.ImageAfter()} {
						ui.Text(c, label).Grow(1).Basis(0).MinWidth(0).Padding(0, 12).
							FontSize(c.Theme().FontSize * 14 / 15).FontWeight(500).TextColor(c.Theme().TextMuted)
					}
				} else {
					ui.Text(c, tx.UnifiedDiffHint()).Padding(0, 12).FontSize(c.Theme().FontSize * 14 / 15).TextColor(c.Theme().TextMuted)
				}
			})
		}
		for i, patch := range p.patches {
			a.configureNativeDiffSelection(c, i, patch.state)
			patch.state.Layout, patch.state.Wrap = layout, a.wrap
			if a.coreSnapshot != nil && a.coreSnapshot.Appearance != nil {
				patch.state.TabSize = a.coreSnapshot.Appearance.TabSize
			}
			patch.state.Font = ui.Font{Family: workspaceCodeFont(), Size: c.Theme().FontSize}
			patch.state.Theme = workspaceDiffTheme(c.Theme())
			patch.state.RenderSeparator = workspaceDiffSeparator
			patch.state.FitContent = len(p.patches) > 1
		}
		if len(p.patches) == 1 {
			patch := p.patches[0]
			if len(patch.file.Hunks) == 0 {
				if patch.file.OldMode != "" || patch.file.NewMode != "" {
					ui.Text(c, tx.ModeChange(patch.file.OldMode, patch.file.NewMode)).Padding(12)
				}
				emptyState(c, tx.DiffEmpty())
			} else {
				diffElement := editor.DiffView(c.Key("patch"), patch.state).Grow(1).MinHeight(0).FillWidth().Label(tx.DiffTitle())
				if layout == editor.DiffUnified {
					// Assistive technology still hears how the single column reads.
					diffElement.Description(tx.UnifiedDiffHint())
				}
				a.workspaceDiffContextMenu(c, diffElement)
			}
		} else {
			ui.List(c, a.patchList.list(), len(p.patches), func(i int) {
				ui.Text(c, p.patches[i].file.NewName).Padding(10, 12).SingleLine()
				editor.DiffView(c.Key(i), p.patches[i].state).FillWidth()
			}).Grow(1).MinHeight(0).FillWidth().Label(tx.DiffTitle())
		}
	case "image":
		if a.core != nil {
			var onMode func(nativeImageDiffType)
			if a.ready {
				onMode = func(mode nativeImageDiffType) {
					core, ctx := a.core, a.ctx
					a.runCoreTask(func() (backend.KernelResult, error) { return core.ChangeImageDiffType(ctx, int(mode)) }, func(_ backend.KernelResult, err error) { a.openErr = err })
				}
			}
			a.nativeImageDiffView(c, &a.nativeImageDiffState, onMode)
		} else {
			ui.Row(c).Grow(1).MinHeight(0).FillWidth().AlignItems(ui.Stretch).Children(func() {
				imageSide(c, tx, tx.ImageBefore(), p.previous, p.before)
				imageSide(c, tx, tx.ImageAfter(), p.current, p.after)
			})
		}
	case "submodule":
		a.nativeSubmoduleView(c, tx, p.submodule, true)
	case "binary":
		emptyState(c, tx.DiffBinary())
	case "unrenderable":
		emptyState(c, tx.DiffUnrenderable())
	}
}

func imageSide(c *ui.Context, tx locales.Messages, label string, image ui.ImageSource, blob *backend.Blob) {
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).Padding(12).Gap(12).Children(func() {
		ui.Text(c, label).FontWeight(500)
		if image == nil {
			emptyState(c, tx.ImageMissing())
		} else {
			ui.Image(c, image).Grow(1).FillWidth().MinHeight(0).Fit(ui.Contain).Label(label)
			ui.Text(c, tx.ImageInfo(blob.MediaType, blob.Bytes)).SingleLine().FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *app) openDialog(c *ui.Context, tx locales.Messages) {
	if ui.Modal(c.Key("open-dialog"), &a.openModal, func() {
		ui.Text(c, tx.OpenRepository()).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500)
		if a.core != nil && !a.opening {
			// One clear entry point, followed by the original three workflows.
			// Repository creation/clone/add and their recovery stay in the kernel.
			for _, option := range []struct{ action, title, detail, icon string }{
				{"clone-repository", "CloneRepository", "openCloneDetail", "download"},
				{"new-repository", "CreateRepository", "openCreateDetail", "plus"},
				{"add-local-repository", "AddRepository", "openAddDetail", "folder-open"},
			} {
				label := nativeDialogLabel(c.Locale(), "repositoryDialog."+option.title)
				item, available := nativeMenuAction(a.coreSnapshot.Menu.Items, option.action)
				button := ui.ButtonBase(c.Key(option.action)).FillWidth().Padding(14, 12).Gap(12).Border(1, c.Theme().Border).Background(c.Theme().Surface).Label(label).Disabled(!a.ready || !available || !item.Enabled)
				quietSurface(c, button).Children(func() {
					ui.Icon(c, icons[option.icon]).FontSize(20).Shrink(0)
					ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
						ui.Text(c, label).FontWeight(500)
						ui.Text(c, nativeDialogLabel(c.Locale(), "repositoryDialog."+option.detail)).TextColor(c.Theme().TextMuted).FontSize(c.Theme().Rem(14.0 / 15))
					})
					ui.Icon(c, icons["chevron-right"]).FontSize(16).Shrink(0)
				}).OnClick(func() {
					a.cancelOpen()
					core, ctx := a.core, a.ctx
					a.runCoreTask(func() (backend.KernelResult, error) { return core.ExecuteMenu(ctx, option.action) }, func(_ backend.KernelResult, err error) { a.openErr = err })
				})
			}
			quietButton(c.Key("cancel-open-project"), tx.Cancel(), "").AlignSelf(ui.End).OnClick(a.cancelOpen)
			return
		}
		ui.Text(c, tx.RepositoryPath())
		ui.Row(c).Gap(8).Children(func() {
			if ui.TextInput(c.Key("repository-path"), &a.pathDraft).Grow(1).MinWidth(0).Height(40).
				Placeholder(tx.RepositoryPathPlaceholder()).Label(tx.RepositoryPath()).AutoFocus().Submitted() {
				a.openRepository(a.pathDraft)
			}
			quietButton(c, tx.Browse(), "folder-open").Height(40).Disabled(a.browsing || a.chooseDirectory == nil).
				OnClick(func() { a.browse(tx.OpenRepository()) })
		})
		if a.browseErr != nil {
			showError(c, tx.FailedBrowse(a.browseErr.Error()))
		}
		if a.openErr != nil {
			showError(c, tx.FailedOpen(a.openErr.Error()))
		}
		if a.opening {
			ui.Text(c, tx.LoadingRepository()).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			quietButton(c, tx.Cancel(), "").OnClick(a.cancelOpen)
			ui.PrimaryButton(c, tx.Open()).Height(40).Padding(8, 20).FontWeight(500).
				Disabled(!a.ready || strings.TrimSpace(a.pathDraft) == "").OnClick(func() { a.openRepository(a.pathDraft) })
		})
	}).Width(560).MaxWidthPercent(90).Padding(24).Gap(16).Label(tx.OpenRepository()).Dismissed() {
		a.cancelOpen()
	}
}

func commitDetails(c *ui.Context, tx locales.Messages, commit backend.Commit) string {
	return tx.CommitDetails(commit.AuthorName, commit.ShortSHA, commitDate(c, commit))
}

func commitDate(c *ui.Context, commit backend.Commit) string {
	date := commit.Date
	if label, ok := commit.DateLabels[c.Locale()]; ok {
		date = label.Text
	} else if parsed, err := time.Parse(time.RFC3339, date); err == nil {
		date = i18n.FormatDate(c.Region().FormatLocale(c.Locale()), parsed.Local(), i18n.DateOptions{Date: i18n.ShortStyle, Time: i18n.ShortStyle})
	}
	return date
}

func fileStatus(tx locales.Messages, status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "new", "added", "a":
		return tx.FileAdded()
	case "modified", "m":
		return tx.FileModified()
	case "deleted", "d":
		return tx.FileDeleted()
	case "renamed", "r":
		return tx.FileRenamed()
	case "copied", "c":
		return tx.FileCopied()
	case "conflicted", "u":
		return tx.FileConflicted()
	case "untracked", "?", "??":
		return tx.FileUntracked()
	default:
		return tx.FileChanged()
	}
}

func operationLabel(tx locales.Messages, operation string) string {
	switch strings.ToLower(operation) {
	case "merge":
		return tx.OperationMerge()
	case "rebase":
		return tx.OperationRebase()
	case "cherry-pick", "cherrypick":
		return tx.OperationCherryPick()
	case "revert":
		return tx.OperationRevert()
	case "bisect":
		return tx.OperationBisect()
	default:
		return tx.OperationOther(operation)
	}
}
