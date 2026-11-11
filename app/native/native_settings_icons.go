package main

// Settings categories each get one Lucide line icon, so the navigation reads
// at a glance as in the system settings apps. Other dialogs with navigation
// (repository settings) keep text alone: their pages are not a fixed set.
var settingsNavigationIcons = map[string]string{
	"accounts": "user-round", "integrations": "plug", "copilot": "bot", "git": "git-branch",
	"appearance": "palette", "notifications": "bell", "prompts": "shield-check",
	"advanced": "sliders-horizontal", "accessibility": "accessibility", "providers": "server",
}

func settingsNavigationIcon(popupType, id string) string {
	if popupType != "Preferences" {
		return ""
	}
	return settingsNavigationIcons[id]
}

// The icon and its gap, as quietButton lays them out, for the panel measure.
func settingsNavigationIconWidth(popupType, id string) float32 {
	if settingsNavigationIcon(popupType, id) == "" {
		return 0
	}
	return 18 + 8
}
