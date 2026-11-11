package main

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// historyRow is one list row: a day heading (commit < 0) or a commit.
type historyRow struct {
	commit int
	day    string
	date   time.Time
}

// Commits arrive newest first. A heading starts each local calendar day, so
// the eye finds "when" before reading any message. A date the service could
// not express as RFC 3339 adds no heading and stays in the current group.
func historyRows(commits []backend.Commit) []historyRow {
	rows := make([]historyRow, 0, len(commits)+8)
	day := ""
	for i, commit := range commits {
		if parsed, err := time.Parse(time.RFC3339, commit.Date); err == nil {
			local := parsed.Local()
			if key := local.Format("2006-01-02"); key != day {
				day = key
				rows = append(rows, historyRow{commit: -1, day: key, date: local})
			}
		}
		rows = append(rows, historyRow{commit: i})
	}
	return rows
}

// Today and yesterday use the locale's own words; older days show the weekday
// and date, with the year only when it differs from the current one.
func historyDayLabel(c *ui.Context, date time.Time) string {
	now := c.Now().Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	then := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	if days := int(today.Sub(then).Hours() / 24); days == 0 || days == 1 {
		label := i18n.FormatRelativeTime(c.Locale(), float64(-days), i18n.RelativeDay, i18n.RelativeTimeOptions{Auto: true})
		// CLDR writes these words in sentence-internal case; a heading starts one.
		first, size := utf8.DecodeRuneInString(label)
		return string(unicode.ToUpper(first)) + label[size:]
	}
	skeleton := "yMMMEd"
	if date.Year() == now.Year() {
		skeleton = "MMMEd"
	}
	return i18n.FormatDate(c.Region().FormatLocale(c.Locale()), date, i18n.DateOptions{Skeleton: skeleton})
}

func fileStatusColor(t *ui.Theme, status string) ui.Color {
	switch strings.ToLower(status) {
	case "added", "new", "untracked", "a", "?", "??":
		return t.Success
	case "deleted", "conflicted", "d", "u":
		return t.Danger
	}
	return t.TextMuted
}

// Each record leads with its author's avatar and summary; who and when sit
// directly below, with the change size, file scope and SHA on the same line.
// The description is secondary and clamped until its record is selected.
// File names expand only from the file count; selecting a row is reserved for
// history operations. MyGo owns the same Ctrl/Shift/arrow selection and
// original context menus.
func renderWorkspaceHistoryList(c *ui.Context, state *nativeWorkspaceState, listState *ui.ListState, workspace *backend.KernelWorkspace, commits []backend.Commit, shas []string, showEmpty bool, send func(backend.KernelWorkspaceAction), selectedContent func(*ui.Context)) {
	identity := workspace.Repository.Path + strings.Join(shas, "/")
	if state.workspaceHistoryIdentity != identity {
		state.workspaceHistorySelection.Clear()
		for _, sha := range shas {
			state.workspaceHistorySelection.Add(sha)
		}
		state.workspaceHistoryIdentity = identity
		state.workspaceHistoryCombinedFilesOpen = false
		if len(shas) != 1 || shas[0] != state.workspaceHistoryFilesSHA {
			state.workspaceHistoryFilesSHA = ""
		}
	}
	rows := historyRows(commits)
	// The statistics loader maps the list's visible rows back to commits.
	state.workspaceHistoryRows = state.workspaceHistoryRows[:0]
	for _, row := range rows {
		state.workspaceHistoryRows = append(state.workspaceHistoryRows, row.commit)
	}
	listState.Key = func(i int) any {
		if rows[i].commit < 0 {
			return "day:" + rows[i].day
		}
		return commits[rows[i].commit].SHA
	}
	listState.Label = func(i int) string {
		if rows[i].commit < 0 {
			return rows[i].day
		}
		return commits[rows[i].commit].Summary
	}
	listState.Header = func(i int) bool { return rows[i].commit < 0 }
	listState.Selection = &state.workspaceHistorySelection
	listState.Selected = &state.workspaceHistoryCursor
	reordering := len(workspace.ReorderSelection) > 0
	if reordering {
		ui.Text(c, workspaceLabel(c.Locale(), "workspace.reorderHint")).Role(ui.RoleStatus).Margin(0, 0, 8, 0)
		quietButton(c.Key("cancel-workspace-reorder"), workspaceLabel(c.Locale(), "workspace.cancelReorder"), "x").OnClick(func() { send(backend.KernelWorkspaceAction{Type: "history.reorder.cancel"}) })
		if c.Shortcut(0, ui.KeyEscape) {
			send(backend.KernelWorkspaceAction{Type: "history.reorder.cancel"})
		}
		quietButton(c.Key("insert-workspace-first"), workspaceLabel(c.Locale(), "workspace.insertHere"), "arrow-up-right").OnClick(func() {
			send(backend.KernelWorkspaceAction{Type: "history.reorder", SHAs: slices.Clone(workspace.ReorderSelection)})
		})
	}
	locale := c.Locale()
	tx := locales.In(locale)
	// A faint neutral surface identifies selection without competing with the
	// message. Preserve the platform selection palette in high-contrast mode.
	baseTheme := c.Theme()
	listTheme := *baseTheme
	if !c.Preferences().HighContrast {
		selected := baseTheme.Background.Mix(baseTheme.Text, 0.045)
		listTheme.Accent, listTheme.InactiveAccent, listTheme.AccentText = selected, selected, baseTheme.Text
		listTheme.SurfaceHover = baseTheme.Background.Mix(baseTheme.Text, 0.02)
	}
	c.SetTheme(&listTheme)
	defer c.SetTheme(baseTheme)
	small := c.Theme().Rem(14.0 / 15)
	list := ui.List(c.Key("workspace-history-list"), listState, len(rows), func(i int) {
		if rows[i].commit < 0 {
			// The pinned heading needs the canvas color, or records show through.
			ui.Row(c.Key("day:"+rows[i].day)).FillWidth().Padding(12, 16, 6, 16).Gap(12).AlignItems(ui.Center).Background(baseTheme.Background).Children(func() {
				ui.Text(c, historyDayLabel(c, rows[i].date)).FontSize(small).FontWeight(500).TextColor(baseTheme.TextMuted).NoWrap().Role(ui.RoleHeading).Level(2)
				ui.Box(c).Grow(1).Height(1).Background(baseTheme.Background.Mix(baseTheme.Text, 0.1))
			})
			return
		}
		commit := commits[rows[i].commit]
		chosen := slices.Contains(shas, commit.SHA)
		row := ui.Row(c.Key(commit.SHA)).FillWidth().Padding(12, 16).Gap(12).AlignItems(ui.Start).Label(commit.Summary)
		row.Children(func() {
			// MyGo picks a vivid hue per name. Keep one hue per author, at the
			// low chroma of this workspace, so avatars locate without shouting.
			hue := uint32(2166136261)
			for index := 0; index < len(commit.AuthorName); index++ {
				hue = (hue ^ uint32(commit.AuthorName[index])) * 16777619
			}
			ui.Avatar(c, commit.AuthorName, nil).Margin(2, 0, 0, 0).Background(ui.Oklch(0.58, 0.07, float32(hue%360)))
			ui.Column(c.Key("record")).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Row(c.Key("title")).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Text(c, commit.Summary).Grow(1).MinWidth(0).FontSize(c.Theme().Rem(16.0 / 15)).FontWeight(500)
					if len(commit.Tags) > 0 {
						// Tags are release landmarks: a quiet pill beside the
						// summary, not another line of prose.
						ui.Row(c.Key("tags")).Shrink(0).Gap(6).Margin(2, 0, 0, 0).Label(tx.CommitTags(strings.Join(commit.Tags, ", "))).Children(func() {
							for index, tag := range commit.Tags {
								if index == 2 && len(commit.Tags) > 3 {
									ui.Badge(c, fmt.Sprintf("+%d", len(commit.Tags)-2)).Tooltip(strings.Join(commit.Tags[2:], ", "))
									break
								}
								ui.Badge(c, tag).MaxWidth(160)
							}
						})
					}
				})
				ui.Row(c.Key("metadata")).FillWidth().MinWidth(0).MinHeight(28).Gap(8).AlignItems(ui.Center).Children(func() {
					date, absolute := commitDate(c, commit), ""
					if label, ok := commit.DateLabels[locale]; ok {
						absolute = label.Absolute
					}
					ui.Text(c, commit.AuthorName).SingleLine().Shrink(1).MinWidth(0).FontSize(small).Tooltip(commit.AuthorName)
					ui.Text(c, date).NoWrap().Shrink(0).FontSize(small).TextColor(baseTheme.TextMuted).Tooltip(absolute)
					ui.Spacer(c)
					if summary, ok := state.workspaceHistorySummaries[commit.SHA]; ok {
						if summary.LinesAdded+summary.LinesDeleted > 0 {
							stats := tx.CommitStats(summary.LinesAdded, summary.LinesDeleted)
							ui.Row(c.Key("history-line-stats")).Shrink(0).Gap(6).Padding(0, 4).FontSize(small).FontFeatures("tnum").Label(stats).Tooltip(stats).Children(func() {
								if summary.LinesAdded > 0 {
									ui.Text(c, "+"+i18n.FormatNumber(locale, summary.LinesAdded, i18n.NumberOptions{})).TextColor(baseTheme.Success).NoWrap()
								}
								if summary.LinesDeleted > 0 {
									ui.Text(c, "−"+i18n.FormatNumber(locale, summary.LinesDeleted, i18n.NumberOptions{})).TextColor(baseTheme.Danger).NoWrap()
								}
							})
						}
						expanded := state.workspaceHistoryFilesSHA == commit.SHA
						icon := "chevron-right"
						if expanded {
							icon = "chevron-down"
						}
						button := ui.ButtonBase(c.Key("history-file-count")).MinHeight(28).Padding(4, 6).Gap(4).Shrink(0).Label(tx.FileCount(summary.FileCount)).Expanded(expanded)
						quietSurface(c, button).Children(func() {
							ui.Text(c, tx.FileCount(summary.FileCount)).FontSize(small).TextColor(baseTheme.TextMuted)
							ui.Icon(c, icons[icon]).FontSize(14).TextColor(baseTheme.TextMuted)
						}).OnClick(func() {
							if expanded {
								state.workspaceHistoryFilesSHA = ""
								return
							}
							state.workspaceHistoryFilesSHA = commit.SHA
							if len(shas) != 1 || shas[0] != commit.SHA {
								send(backend.KernelWorkspaceAction{Type: "history.select", SHAs: []string{commit.SHA}})
							}
						})
					} else {
						ui.Text(c, tx.LoadingChangeset()).FontSize(small).TextColor(baseTheme.TextMuted).Role(ui.RoleStatus)
					}
					if menu := workspaceMenu(workspace, "commit", commit.SHA); menu != nil {
						for index, entry := range menu.Items {
							if entry.LabelKey == "workspace.copySHA" {
								button := ui.ButtonBase(c.Key("history-copy-sha")).MinHeight(28).Padding(4, 6).Gap(6).Shrink(0).
									Label(workspaceLabel(locale, entry.LabelKey)).Tooltip(commit.SHA).Description(commit.SHA).Disabled(!entry.Enabled)
								quietSurface(c, button).Children(func() {
									ui.Text(c, commit.ShortSHA).FontSize(small).Font(workspaceCodeFont()).TextColor(baseTheme.TextMuted).NoWrap()
									ui.Icon(c, icons["copy"]).FontSize(14).TextColor(baseTheme.TextMuted)
								}).OnClick(func() {
									send(backend.KernelWorkspaceAction{Type: "menu", Target: menu.Target, Lease: menu.Lease, MenuPath: []int{index}})
								})
								break
							}
						}
					}
				})
				if body := strings.TrimSpace(commit.Body); body != "" {
					if !chosen {
						// Three lines identify a description; selecting the
						// record shows all of it, in place. Blank lines would
						// spend the preview on nothing.
						body = strings.Join(strings.FieldsFunc(body, func(r rune) bool { return r == '\n' || r == '\r' }), "\n")
					}
					text := ui.Text(c.Key("body"), body).FillWidth().MaxWidth(c.Theme().Rem(48)).FontSize(small).TextColor(baseTheme.TextMuted).Selectable()
					if !chosen {
						text.MaxLines(3)
					}
				}
				if state.workspaceHistoryFilesSHA == commit.SHA && len(shas) == 1 && shas[0] == commit.SHA && selectedContent != nil {
					selectedContent(c)
				}
				if reordering {
					quietButton(c.Key("insert-after"), workspaceLabel(locale, "workspace.insertHere"), "arrow-up-right").Disabled(slices.Contains(workspace.ReorderSelection, commit.SHA)).OnClick(func() {
						sha := commit.SHA
						send(backend.KernelWorkspaceAction{Type: "history.reorder", SHAs: slices.Clone(workspace.ReorderSelection), BeforeSHA: &sha})
					})
				}
			})
		})
		row.PopoverMenu(func(m *ui.Menu) { renderWorkspaceMenu(m, locale, workspaceMenu(workspace, "commit", commit.SHA), send) })
	}).Grow(1).MinHeight(0).FillWidth().Gap(2).Label(workspaceLabel(locale, "workspace.history")).Children(func() {
		if len(commits) == 0 && showEmpty {
			emptyStateIcon(c, "history", locales.In(locale).NoHistory())
		}
	})
	// Arrow keys focus the list itself. Expose the same current-row menu there
	// so keyboard users reach the original copy and history operations too.
	list.PopoverMenu(func(m *ui.Menu) {
		if i := state.workspaceHistoryCursor; i >= 0 && i < len(rows) && rows[i].commit >= 0 {
			renderWorkspaceMenu(m, locale, workspaceMenu(workspace, "commit", commits[rows[i].commit].SHA), send)
		}
	})
	changed := list.Changed()
	if changed {
		var selected []string
		for _, commit := range commits {
			if state.workspaceHistorySelection.Has(commit.SHA) {
				selected = append(selected, commit.SHA)
			}
		}
		if len(selected) != 1 || selected[0] != state.workspaceHistoryFilesSHA {
			state.workspaceHistoryFilesSHA = ""
		}
		send(backend.KernelWorkspaceAction{Type: "history.select", SHAs: selected})
	}
}

// A continuous record keeps each commit's message, metadata and file scope
// together. There is no duplicate title or separately sized detail pane.
func (a *app) workspaceHistoryPage(c *ui.Context) {
	tx := locales.In(c.Locale())
	a.workspaceNavigationView(c)
	if a.loadingHistory {
		ui.Text(c, tx.LoadingHistory()).Role(ui.RoleStatus)
	}
	if a.historyErr != nil {
		showError(c, tx.FailedHistory(a.historyErr.Error()))
	}
	if len(a.coreSnapshot.Selected.HistorySelection) > 1 {
		ui.Column(c.Key("combined-history")).FillWidth().Shrink(0).Padding(0, 0, 16, 0).Children(func() { a.workspaceHistoryReview(c) })
	}
	a.workspaceHistoryList(c)
}

// A multi-selection remains the original combined changeset, never mislabeled
// as one arbitrary commit. It is visible without leaving the history list.
func (a *app) workspaceHistoryReview(c *ui.Context) bool {
	w := a.nativeWorkspace()
	if w == nil || a.coreSnapshot.Selected == nil || a.tab != historyTab {
		return false
	}
	selected := a.coreSnapshot.Selected
	if len(selected.HistorySelection) == 0 {
		return false
	}
	tx := locales.In(c.Locale())
	if len(selected.HistorySelection) > 1 {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, workspaceLabel(c.Locale(), "workspace.historySelection", fmt.Sprint(len(selected.HistorySelection)))).Grow(1).FontWeight(500)
			if w.HistorySelectionContiguous && !a.loadingCommit && a.commitErr == nil {
				quietButton(c.Key("combined-file-count"), tx.FileCount(len(selected.HistoryFiles)), "chevron-down").Expanded(a.workspaceHistoryCombinedFilesOpen).OnClick(func() { a.workspaceHistoryCombinedFilesOpen = !a.workspaceHistoryCombinedFilesOpen })
			}
		})
	}
	if !w.HistorySelectionContiguous {
		emptyState(c, workspaceLabel(c.Locale(), "workspace.nonContiguous"))
		return true
	}
	if a.loadingCommit {
		ui.Text(c, tx.LoadingChangeset()).Role(ui.RoleStatus)
		return true
	}
	if a.commitErr != nil {
		showError(c, tx.FailedChangeset(a.commitErr.Error()))
		return true
	}
	if len(selected.HistorySelection) > 1 && !a.workspaceHistoryCombinedFilesOpen {
		return true
	}
	if len(selected.HistorySelection) > 1 {
		// One record already shows its own change size beside the file count.
		ui.Text(c, tx.CommitStats(selected.HistoryLinesAdded, selected.HistoryLinesDeleted)).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	}
	if len(selected.HistoryFiles) == 0 {
		emptyState(c, tx.NoCommitFiles())
		return true
	}
	files := selected.HistoryFiles
	a.commitFilesList.list().Key = func(i int) any { return files[i].ID }
	ui.List(c.Key("workspace-history-files"), a.commitFilesList.list(), len(files), func(i int) {
		file := files[i]
		button := ui.ButtonBase(c.Key(file.ID)).MinHeight(40).FillWidth().Padding(6, 8).Gap(12).Label(tx.OpenFile(file.Path)).Tooltip(file.Path).Disabled(!a.ready)
		quietSurface(c, button).Children(func() {
			ui.Icon(c, icons[fileIcon(file.Path, file.IsSubmodule)]).FontSize(18).Shrink(0).TextColor(c.Theme().TextMuted)
			// The name leads; its folder is context, as in the changes list.
			ui.Text(c, path.Base(file.Path)).SingleLine().Shrink(1).MinWidth(0)
			if directory := path.Dir(file.Path); directory != "." {
				ui.Text(c, directory).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).MinWidth(0)
			} else {
				ui.Spacer(c)
			}
			ui.Text(c, fileStatus(tx, file.Status)).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(fileStatusColor(c.Theme(), file.Status)).NoWrap().Shrink(0)
			ui.Icon(c, icons["chevron-right"]).FontSize(18).Shrink(0).TextColor(c.Theme().TextMuted)
		}).OnClick(func() { a.reviewWorkspacePath(file.Path) })
		a.fileContextMenu(c, button, file.ID)
	}).Height(min(c.Theme().Rem(12), float32(len(files))*40)).FillWidth().Margin(4, 0, 0, 0).Label(tx.ChangedFiles())
	return true
}

// Fetch only the virtual list's visible commits. Exact statistics come from
// Desktop's original Git parser, without selecting each commit to obtain them.
func (a *app) loadVisibleHistorySummaries() {
	ref, ok := a.coreReference()
	if !ok || a.workspaceHistorySummaryLoading || a.workspaceHistorySummaryError != nil {
		return
	}
	first, last := a.commitsList.list().Visible()
	var shas []string
	// Visible rows include day headings; only commit rows have statistics.
	for row := max(0, first); row <= last && row < len(a.workspaceHistoryRows) && len(shas) < 32; row++ {
		i := a.workspaceHistoryRows[row]
		if i < 0 || i >= len(a.commits) {
			continue
		}
		if _, exists := a.workspaceHistorySummaries[a.commits[i].SHA]; !exists {
			shas = append(shas, a.commits[i].SHA)
		}
	}
	if len(shas) == 0 {
		return
	}
	a.workspaceHistorySummaryLoading = true
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.HistorySummaries(ctx, ref, shas) },
		complete: func(result backend.KernelResult, err error) {
			a.workspaceHistorySummaryLoading = false
			current, valid := a.coreReference()
			if !valid || current != ref {
				return
			}
			a.workspaceHistorySummaryError = err
			if err != nil {
				return
			}
			if a.workspaceHistorySummaries == nil {
				a.workspaceHistorySummaries = make(map[string]backend.KernelHistorySummary)
			}
			for _, summary := range result.HistorySummaries {
				a.workspaceHistorySummaries[summary.SHA] = summary
			}
		},
	})
}
