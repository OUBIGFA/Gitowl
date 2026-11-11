package main

import "github.com/egoist/mygo/ui"

// Follow original Preferences' immediate appearance preview and cancel rollback.
// System appearance and accessibility preferences remain owned by MyGo/OS; only
// an explicit original light/dark preference changes the frame's base theme.
func (a *app) applyCoreAppearance(c *ui.Context) {
	if a.coreSnapshot == nil || a.coreSnapshot.Appearance == nil || c.Preferences().HighContrast {
		return
	}
	switch a.coreSnapshot.Appearance.Theme {
	case "light":
		c.SetTheme(ui.LightTheme())
	case "dark":
		c.SetTheme(ui.DarkTheme())
	}
}
