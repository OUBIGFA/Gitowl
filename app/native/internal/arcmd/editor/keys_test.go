package editor

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestComboLabel(t *testing.T) {
	c := Combo{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyK}
	if got := c.label(true); got != "⌃⇧K" {
		t.Errorf("darwin label = %q, want %q", got, "⌃⇧K")
	}
	if got := c.label(false); got != "Ctrl+Shift+K" {
		t.Errorf("non-darwin label = %q, want %q", got, "Ctrl+Shift+K")
	}
	up := Combo{Mods: ui.Ctrl | ui.Alt, Key: ui.KeyUp}
	if got := up.label(false); got != "Ctrl+Alt+Up" {
		t.Errorf("non-darwin arrow label = %q, want %q", got, "Ctrl+Alt+Up")
	}
	cmd := Combo{Mods: ui.Cmd | ui.Shift, Key: ui.KeyK}
	want := "Ctrl+Shift+K"
	if runtime.GOOS == "darwin" {
		want = "⇧⌘K"
	}
	if got := cmd.Label(); got != want {
		t.Errorf("Combo.Label() = %q, want %q", got, want)
	}
	if got := cmd.String(); got != "Cmd+Shift+K" {
		t.Errorf("Combo.String() = %q, want %q", got, "Cmd+Shift+K")
	}
}
