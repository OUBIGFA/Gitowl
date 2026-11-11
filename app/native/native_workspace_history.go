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

// historyRow is one list row: a day heading (commit < 0) or a commit. first
// and last place a commit in its day, where the timeline starts and ends.
type historyRow struct {
	commit      int
	day         string
	date        time.Time
	first, last bool
}

// Commits arrive newest first. A heading starts each local calendar day, so
// the eye finds "when" before reading any message. A date the service could
// not express as RFC 3339 adds no heading and stays in the current group.
func historyRows(commits []backend.Commit) []historyRow {
	rows := make([]historyRow, 0, len(commits)+8)
	day := ""
	for i, commit := range commits {
		heading := i == 0
		if parsed, err := time.Parse(time.RFC3339, commit.Date); err == nil {
			local := parsed.Local()
			if key := local.Format("2006-01-02"); key != day {
				day, heading = key, true
				rows = append(rows, historyRow{commit: -1, day: key, date: local})
			}
		}
		if heading && len(rows) > 1 && rows[len(rows)-2].commit >= 0 {
			rows[len(rows)-2].last = true
		}
		rows = append(rows, historyRow{commit: i, first: heading})
	}
	if len(rows) > 0 {
		rows[len(rows)-1].last = true
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

// Under a day heading only the time of day is new information. The complete
// original date label stays in the tooltip.
func historyTimeLabel(c *ui.Context, commit backend.Commit) (text, full string) {
	full = commitDate(c, commit)
	if label, ok := commit.DateLabels[c.Locale()]; ok && label.Absolute != "" {
		full = label.Absolute
	}
	parsed, err := time.Parse(time.RFC3339, commit.Date)
	if err != nil {
		return full, full
	}
	return i18n.FormatDate(c.Region().FormatLocale(c.Locale()), parsed.Local(), i18n.DateOptions{Skeleton: "jm", HourCycle: c.Region().HourCycle}), full
}

func fileStatusColor(t *ui.Theme, status string) ui.Color {
	switch strings.ToLower(status) {
	case "added", "new", "untracked", "a", "?", "??":
		return t.Success
	case "deleted", "conflicted", "d", "u":
		return t.Danger
	case "modified", "m":
		return t.Warning
	}
	return t.TextMuted
}

// One author keeps one hue, at the low chroma of this workspace, so the
// avatars locate people without shouting. MyGo's own hue is too vivid here.
func authorAvatar(c *ui.Context, name string) ui.Element {
	hue := uint32(2166136261)
	for index := 0; index < len(name); index++ {
		hue = (hue ^ uint32(name[index])) * 16777619
	}
	return ui.Avatar(c, name, nil).Size(28, 28).Shrink(0).Background(ui.Oklch(0.6, 0.06, float32(hue%360)))
}

// A tag is a release landmark: a quiet pill beside the summary. A tag the
// next push publishes takes the accent and says so.
func historyTag(c *ui.Context, tx locales.Messages, tag string, unpushed bool, accent ui.Color) {
	t := c.Theme()
	color, background := t.TextMuted, t.Text.Alpha(0.07)
	tooltip := tag
	if unpushed {
		color, background, tooltip = accent, accent.Alpha(0.1), tx.TagNotPushed(tag)
	}
	ui.Row(c.Key("tag:"+tag)).Shrink(0).Gap(4).Padding(1, 8).Radius(999).Background(background).AlignItems(ui.Center).Tooltip(tooltip).Children(func() {
		ui.Icon(c, icons["tag"]).FontSize(12).TextColor(color)
		ui.Text(c, tag).FontSize(t.Rem(13.0 / 15)).TextColor(color).SingleLine().MaxWidth(140)
	})
}

// Each record sits on a timeline of its day: the author's avatar on a thin
// line, the summary beside it. Who and when follow on the second line with
// the change size, file scope and SHA at its end; a commit not yet pushed says
// so there. The description is one quiet line until its record is selected.
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
		selected := baseTheme.Background.Mix(baseTheme.Text, 0.05)
		listTheme.Accent, listTheme.InactiveAccent, listTheme.AccentText = selected, selected, baseTheme.Text
		listTheme.SurfaceHover = baseTheme.Background.Mix(baseTheme.Text, 0.025)
	}
	c.SetTheme(&listTheme)
	defer c.SetTheme(baseTheme)
	small := c.Theme().Rem(14.0 / 15)
	timeline := baseTheme.Background.Mix(baseTheme.Text, 0.14)
	list := ui.List(c.Key("workspace-history-list"), listState, len(rows), func(i int) {
		if rows[i].commit < 0 {
			// The pinned heading needs the canvas color, or records show through.
			ui.Row(c.Key("day:"+rows[i].day)).FillWidth().Padding(14, 16, 6, 12).Gap(12).AlignItems(ui.Center).Background(baseTheme.Background).Children(func() {
				ui.Text(c, historyDayLabel(c, rows[i].date)).FontSize(small).FontWeight(500).TextColor(baseTheme.Text).NoWrap().Role(ui.RoleHeading).Level(2)
				ui.Box(c).Grow(1).Height(1).Background(baseTheme.Background.Mix(baseTheme.Text, 0.1))
			})
			return
		}
		position := rows[i]
		commit := commits[position.commit]
		chosen := slices.Contains(shas, commit.SHA)
		local := slices.Contains(workspace.LocalCommitSHAs, commit.SHA)
		row := ui.Row(c.Key(commit.SHA)).FillWidth().Padding(0, 16, 0, 12).Gap(12).AlignItems(ui.Stretch).Label(commit.Summary)
		row.Children(func() {
			// The day's timeline: segments above and below the avatar join
			// the records of one day and stop at its first and last.
			ui.Column(c.Key("timeline")).Width(28).Shrink(0).AlignItems(ui.Center).Children(func() {
				segment := func(key string, visible bool) ui.Element {
					line := ui.Box(c.Key(key)).Width(2).Radius(1)
					if visible {
						line.Background(timeline)
					}
					return line
				}
				segment("above", !position.first).Height(12).Shrink(0)
				authorAvatar(c, commit.AuthorName)
				segment("below", !position.last).Grow(1).MinHeight(4)
			})
			ui.Column(c.Key("record")).Grow(1).MinWidth(0).Gap(2).Padding(10, 0, 12, 0).Children(func() {
				ui.Row(c.Key("title")).FillWidth().MinWidth(0).MinHeight(24).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, commit.Summary).SingleLine().Grow(1).MinWidth(0).FontSize(c.Theme().Rem(16.0 / 15)).FontWeight(500).Tooltip(commit.Summary)
					if len(commit.Tags) > 0 {
						ui.Row(c.Key("tags")).Shrink(0).Gap(6).Label(tx.CommitTags(strings.Join(commit.Tags, ", "))).Children(func() {
							for index, tag := range commit.Tags {
								if index == 2 && len(commit.Tags) > 3 {
									ui.Badge(c, fmt.Sprintf("+%d", len(commit.Tags)-2)).Tooltip(strings.Join(commit.Tags[2:], ", "))
									break
								}
								historyTag(c, tx, tag, slices.Contains(workspace.TagsToPush, tag), baseTheme.Accent)
							}
						})
					}
				})
				ui.Row(c.Key("metadata")).FillWidth().MinWidth(0).MinHeight(28).Gap(8).AlignItems(ui.Center).Children(func() {
					when, full := historyTimeLabel(c, commit)
					ui.Text(c, commit.AuthorName).SingleLine().Shrink(1).MinWidth(0).FontSize(small).Tooltip(commit.AuthorName + " <" + commit.AuthorEmail + ">")
					ui.Text(c, when).NoWrap().Shrink(0).FontSize(small).FontFeatures("tnum").TextColor(baseTheme.TextMuted).Tooltip(full)
					if local {
						ui.Row(c.Key("not-pushed")).Shrink(0).Gap(3).AlignItems(ui.Center).Label(tx.NotPushed()).Children(func() {
							ui.Icon(c, icons["arrow-up"]).FontSize(13).TextColor(baseTheme.Accent)
							ui.Text(c, tx.NotPushed()).FontSize(small).TextColor(baseTheme.Accent).NoWrap()
						})
					}
					ui.Spacer(c)
					if summary, ok := state.workspaceHistorySummaries[commit.SHA]; ok {
						expanded := state.workspaceHistoryFilesSHA == commit.SHA
						icon := "chevron-right"
						if expanded {
							icon = "chevron-down"
						}
						// One control carries the commit's scope: its files and
						// the size of its change, and opens the file list.
						button := ui.ButtonBase(c.Key("history-file-count")).MinHeight(28).Padding(4, 8).Gap(8).Shrink(0).Label(tx.FileCount(summary.FileCount)).Expanded(expanded)
						quietSurface(c, button).Children(func() {
							ui.Text(c, tx.FileCount(summary.FileCount)).FontSize(small).TextColor(baseTheme.TextMuted).NoWrap()
							if summary.LinesAdded+summary.LinesDeleted > 0 {
								changeStats(c, tx, summary.LinesAdded, summary.LinesDeleted)
							}
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
						// One line tells that a description exists and what it
						// starts with; selecting the record shows all of it.
						body = strings.Join(strings.FieldsFunc(body, func(r rune) bool { return r == '\n' || r == '\r' }), " ")
					}
					text := ui.Text(c.Key("body"), body).FillWidth().MaxWidth(c.Theme().Rem(52)).FontSize(small).TextColor(baseTheme.TextMuted).Selectable()
					if chosen {
						text.Margin(4, 0, 0, 0)
					} else {
						text.SingleLine()
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
	}).Grow(1).MinHeight(0).FillWidth().Label(workspaceLabel(locale, "workspace.history")).Children(func() {
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
