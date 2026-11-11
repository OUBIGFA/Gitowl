package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// countText is a quiet tabular number beside a label: a count to scan, not
// another word to read. Zero is not shown by callers.
func countText(c *ui.Context, value int, color ui.Color) ui.Element {
	return ui.Text(c, i18n.FormatNumber(c.Locale(), value, i18n.NumberOptions{})).
		FontSize(c.Theme().Rem(13.0 / 15)).FontFeatures("tnum").TextColor(color).NoWrap().Shrink(0)
}

// tintedSurface marks the one sync action that has work to do, as GitHub
// Desktop's single fetch/pull/push button does, without a second dark primary
// competing with Save. The neutral quiet surface handles every other state.
func tintedSurface(c *ui.Context, button ui.Element) ui.Element {
	t := c.Theme()
	quietSurface(c, button)
	if button.IsDisabled() || c.Preferences().HighContrast {
		return button
	}
	switch {
	case button.Pressed():
		button.Background(t.Accent.Alpha(0.2))
	case button.Hovered():
		button.Background(t.Accent.Alpha(0.15))
	default:
		button.Background(t.Accent.Alpha(0.1))
	}
	return button.TextColor(t.Accent)
}

// The original Repository menu's fetch, pull and push/publish, permanently on
// the task bar. Icons identify direction; counts come from the original
// branch's ahead/behind, so the action with work to do stands out.
func (a *app) coreRemoteControls(c *ui.Context, tx locales.Messages) {
	selected := a.coreSnapshot.Selected
	if a.coreSnapshot == nil || selected == nil {
		return
	}
	reason := ""
	switch {
	case !a.ready:
		reason = tx.ActionNeedsReady()
	case a.savingVersion:
		reason = tx.ActionSaveBusy()
	case selected.RemoteInProgress || a.coreRemotePending:
		reason = tx.RemoteBusy()
	}
	for _, action := range []struct {
		name, key, label, icon string
		count                  int
	}{
		{"fetch", "fetch-updates", tx.FetchUpdates(), "refresh-cw", 0},
		{"pull", "pull-updates", tx.PullUpdates(), "arrow-down", selected.Behind},
		{"push", "push-changes", tx.PushChanges(), "arrow-up", selected.Ahead},
	} {
		item, available := nativeMenuAction(a.coreSnapshot.Menu.Items, action.name)
		label, unavailable, icon := action.label, reason, action.icon
		if action.name == "push" && !selected.HasRemote {
			label, icon = nativeDialogLabel(c.Locale(), "repositoryDialog.PublishRepository"), "cloud-upload"
		} else if action.name == "push" && selected.ForcePushRecommended && available {
			item.AccessKey = ""
			label = nativeMenuLabel(item, c.Locale())
		}
		if unavailable == "" && (!available || !item.Enabled) {
			if !selected.HasRemote {
				unavailable = tx.RemoteUnconfigured()
			} else {
				unavailable = tx.ActionNotAvailable()
			}
		}
		tooltip := label
		if unavailable != "" {
			tooltip += " · " + unavailable
		}
		if action.name == "fetch" {
			// Checking the remote is the secondary sync action. Pull and
			// push keep their words at every width.
			iconButton(c.Key(action.key), label, icon).Size(32, 32).Padding(7).Tooltip(tooltip).Disabled(unavailable != "").
				OnClick(func() { a.coreRemoteAction(action.name) })
			continue
		}
		pending := action.count > 0 && unavailable == ""
		button := ui.ButtonBase(c.Key(action.key)).Height(32).Padding(4, 10).Gap(6).Label(label).Tooltip(tooltip).Disabled(unavailable != "")
		if action.count > 0 {
			button.Value(i18n.FormatNumber(c.Locale(), action.count, i18n.NumberOptions{}))
		}
		if pending {
			tintedSurface(c, button)
		} else {
			quietSurface(c, button)
		}
		button.Children(func() {
			ui.Icon(c, icons[icon]).FontSize(16).Shrink(0)
			ui.Text(c, label).NoWrap()
			if action.count > 0 {
				color := c.Theme().TextMuted
				if pending {
					color = c.Theme().Accent
				}
				countText(c, action.count, color).FontWeight(500)
			}
		}).OnClick(func() { a.coreRemoteAction(action.name) })
	}
}

// One row of the project rail: name first, then what the original repository
// list knows about it, uncommitted files and the branch's ahead/behind.
func projectRowContent(c *ui.Context, repository backend.KernelRepository, name string, current bool) {
	muted := c.Theme().TextMuted
	icon := ui.Icon(c, icons["folder"]).FontSize(16).Shrink(0).TextColor(muted)
	text := ui.Text(c, name).SingleLine().Grow(1).MinWidth(0)
	if current {
		icon.TextColor(c.Theme().Text)
		text.FontWeight(500)
	}
	local := repository.Local
	if local == nil {
		return
	}
	if local.ChangedFiles > 0 {
		countText(c, local.ChangedFiles, muted)
	}
	for _, side := range []struct {
		value *int
		icon  string
	}{{local.Ahead, "arrow-up"}, {local.Behind, "arrow-down"}} {
		if side.value != nil && *side.value > 0 {
			ui.Row(c).Shrink(0).Gap(1).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons[side.icon]).FontSize(12).TextColor(muted)
				countText(c, *side.value, muted)
			})
		}
	}
}

// The accessible summary of a project row's counts, read after its name.
func projectRowDescription(tx locales.Messages, repository backend.KernelRepository) string {
	local := repository.Local
	if local == nil {
		return ""
	}
	description := ""
	if local.ChangedFiles > 0 {
		description = tx.FileCount(local.ChangedFiles)
	}
	if local.Ahead != nil && local.Behind != nil && (*local.Ahead > 0 || *local.Behind > 0) {
		if description != "" {
			description += " · "
		}
		description += tx.AheadBehind(*local.Ahead, *local.Behind)
	}
	return description
}
