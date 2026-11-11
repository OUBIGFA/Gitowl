package main

import (
	"path"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// All variants retain MyGo's native focus, keyboard and accessibility behavior.
// Only their surface, spacing and Lucide content differ from the default button.
func quietSurface(c *ui.Context, button ui.Element) ui.Element {
	button.Radius(8).TextColor(c.Theme().Text)
	if !button.IsDisabled() && button.Pressed() {
		button.Background(c.Theme().SurfacePressed)
	} else if !button.IsDisabled() && button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	return button
}

func quietButton(c *ui.Context, label, icon string) ui.Element {
	button := ui.ButtonBase(c).MinHeight(36).Padding(6, 10).Gap(8).Label(label)
	quietSurface(c, button).Children(func() {
		if icon != "" {
			ui.Icon(c, icons[icon]).FontSize(18).Shrink(0)
		}
		ui.Text(c, label).NoWrap()
	})
	return button
}

func iconButton(c *ui.Context, label, icon string) ui.Element {
	button := ui.ButtonBase(c).Size(36, 36).Padding(9).Label(label).Tooltip(label)
	quietSurface(c, button).Children(func() {
		ui.Icon(c, icons[icon]).FontSize(18)
	})
	return button
}

func taskTitle(c *ui.Context, title, detail string) {
	ui.Row(c).Gap(16).AlignItems(ui.Baseline).Margin(0, 0, 16, 0).Children(func() {
		ui.Text(c, title).FontSize(c.Theme().FontSize * 22 / 15).FontWeight(500).
			Role(ui.RoleHeading).Level(1).Grow(1).MinWidth(0)
		if detail != "" {
			ui.Text(c, detail).TextColor(c.Theme().TextMuted).NoWrap().Shrink(0).FontFeatures("tnum")
		}
	})
}

// Only the patch header's presentation changes. Native expansion controls for
// collapsed unchanged lines, original hunk ranges and the diff model stay intact.
func workspaceDiffSeparator(c *ui.Context, sep ui.Element, section *editor.DiffSeparator) {
	if section.Lines != 0 {
		editor.DefaultSeparator(c, sep, section)
		return
	}
	tx := locales.In(c.Locale())
	sep.Padding(4).Description(section.Header)
	sep.Children(func() {
		// Keep function context visible; range-only hunks retain their exact
		// old/new line ranges instead of repeating a generic section label.
		context := section.Header
		if _, suffix, ok := strings.Cut(strings.TrimPrefix(section.Header, "@@"), "@@"); ok && strings.TrimSpace(suffix) != "" {
			context = strings.TrimSpace(suffix)
		}
		ui.Row(c).Grow(1).AlignSelf(ui.Stretch).Padding(0, 8).Radius(6).Background(c.Theme().SurfaceHover).
			Label(tx.DiffSection()).Tooltip(tx.RawDiffRange(section.Header)).Children(func() {
			ui.Text(c, context).SingleLine().Grow(1).MinWidth(0).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
		})
	})
}

func fileIcon(filePath string, submodule bool) string {
	if submodule {
		return "folder"
	}
	switch strings.ToLower(path.Ext(filePath)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico":
		return "image"
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".css", ".html", ".py", ".rs", ".yaml", ".yml":
		return "file-code"
	case ".md", ".txt", ".csv", ".rst":
		return "file-text"
	default:
		return "file"
	}
}

// Navigation changes only the canvas. The selected whole files, draft and list
// scroll positions belong to the project, not to the currently visible page.
func (a *app) navigateWorkspace(tab int) {
	a.reviewingFile, a.viewingCommit, a.reviewOptionsOpen = false, false, false
	a.setTab(tab)
}

func (a *app) reviewFile(index int) {
	a.reviewingFile, a.reviewOptionsOpen = true, false
	current := a.changeIndex
	if a.tab == historyTab {
		current = a.commitFileIndex
	}
	if index != current || a.preview == nil && !a.loadingDiff && a.diffErr == nil {
		a.selectFile(index)
	}
}

func (a *app) backFromReview() {
	// Keep the native DiffState too: a return should not reload or erase review
	// position, and a delayed reply must never send the user back into a file.
	a.reviewingFile, a.reviewOptionsOpen = false, false
}

func (a *app) openVersion(index int) {
	a.viewingCommit, a.reviewingFile, a.reviewOptionsOpen = true, false, false
	a.selectCommit(index)
}
