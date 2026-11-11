package main

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Keep the established minimum-window contract. The redesign changes hierarchy
// and density without stealing the central review canvas from small windows.
const projectRailWidth float32 = 232

// Workspace surfaces are warm graphite, not the desktop accent. High-contrast
// mode keeps MyGo's system-derived colors; language and text-size preferences
// still apply in both appearances.
type workspaceColors struct {
	rail, selected, hairline ui.Color
}

func workspaceFont(locale string) string {
	if runtime.GOOS != "windows" {
		return "system-ui"
	}
	if strings.HasPrefix(locale, "zh") {
		return "Microsoft YaHei UI"
	}
	// Repository names and drafts may contain Chinese in an English interface.
	return "Segoe UI, Microsoft YaHei UI"
}

func setWorkspaceTheme(c *ui.Context) workspaceColors {
	t := *c.Theme()
	t.Font, t.FontSize = workspaceFont(c.Locale()), 15*max(1, c.Preferences().TextScale)
	t.Radius, t.Spacing = 8, 4
	colors := workspaceColors{rail: t.Surface, selected: t.SurfacePressed, hairline: t.Border}
	if !c.Preferences().HighContrast {
		if t.Dark {
			t.Background, t.Surface = ui.Hex("#242422"), ui.Hex("#2c2c29")
			t.SurfaceHover, t.SurfacePressed = ui.Hex("#343430"), ui.Hex("#3b3b36")
			t.Border, t.Text, t.TextMuted = ui.Hex("#52524b"), ui.Hex("#e8e7e2"), ui.Hex("#b0afa7")
			t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#dedbd2"), ui.Hex("#efede7"), ui.Hex("#c6c2b7"), ui.Hex("#292925")
			t.Selection, t.Focus = ui.RGBA(208, 195, 217, 0.22), ui.Hex("#b7a5c1")
			t.Danger, t.Warning, t.Success = ui.Hex("#ee938f"), ui.Hex("#d8b878"), ui.Hex("#8cbe9c")
			t.Scrollbar = ui.Hex("#8e857a").Alpha(0.7)
			colors = workspaceColors{rail: ui.Hex("#1e1e1c"), selected: ui.Hex("#363632"), hairline: ui.Hex("#3b3b36")}
		} else {
			t.Background, t.Surface = ui.Hex("#fafaf7"), ui.Hex("#ffffff")
			t.SurfaceHover, t.SurfacePressed = ui.Hex("#eeeee8"), ui.Hex("#e4e4dc")
			t.Border, t.Text, t.TextMuted = ui.Hex("#c9c9bf"), ui.Hex("#292925"), ui.Hex("#66665e")
			t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#42394f"), ui.Hex("#554b62"), ui.Hex("#302938"), ui.Hex("#ffffff")
			t.Selection, t.Focus = ui.RGBA(71, 59, 78, 0.16), ui.Hex("#80688d")
			t.Danger, t.Warning, t.Success = ui.Hex("#a83e38"), ui.Hex("#85601e"), ui.Hex("#39714c")
			t.Scrollbar = ui.Hex("#9b9286").Alpha(0.75)
			colors = workspaceColors{rail: ui.Hex("#efefe9"), selected: ui.Hex("#e4e4dc"), hairline: ui.Hex("#ddddd4")}
		}
	}
	c.SetTheme(&t)
	return colors
}

func workspaceCodeFont() string {
	if runtime.GOOS == "windows" {
		// Code stays monospaced; Chinese prose in real Markdown diffs must
		// not fall through DirectWrite to the old serif fallback.
		return "Consolas, Microsoft YaHei UI"
	}
	return "monospace"
}

func workspaceDiffTheme(t *ui.Theme) *editor.Theme {
	d := editor.DefaultTheme(t)
	d.LineNumber = t.TextMuted
	d.Added, d.Deleted, d.Modified = t.Success, t.Danger, t.TextMuted
	return d
}
