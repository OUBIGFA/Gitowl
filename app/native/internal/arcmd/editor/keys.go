package editor

import (
	"runtime"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// Combo is a keyboard shortcut: a key with exactly some modifiers. Cmd is
// ui.Cmd, Command on macOS and Control elsewhere, so a combo written once
// works on every platform.
type Combo struct {
	Mods ui.Modifiers
	Key  ui.Key
}

// DefaultBindings are the editor's commands that can be rebound, and their
// keys.
func DefaultBindings() map[string]Combo {
	return map[string]Combo{
		"bold":      {ui.Cmd, ui.KeyB},
		"italic":    {ui.Cmd, ui.KeyI},
		"underline": {ui.Cmd, ui.KeyU},
		"strike":    {ui.Cmd | ui.Shift, ui.KeyX},
		"code":      {ui.Cmd, ui.KeyE},
		"find":      {ui.Cmd, ui.KeyF},
		"replace":   {ui.Cmd | ui.Shift, ui.KeyH},
		"newline":   {ui.Cmd, ui.KeyEnter},
	}
}

// binding is the combo of the editor's command, rebound or not.
func (e *Editor) binding(command string) Combo {
	if c, ok := e.bindings[command]; ok {
		return c
	}
	return DefaultBindings()[command]
}

// boundCommand is the editor's command bound to the key event, if any.
func (e *Editor) boundCommand(mods ui.Modifiers, key ui.Key) string {
	for command := range DefaultBindings() {
		if c := e.binding(command); c.Key == key && c.Mods == mods {
			return command
		}
	}
	return ""
}

var keyNames = map[ui.Key]string{
	ui.KeyEnter: "Enter", ui.KeyEscape: "Escape", ui.KeyTab: "Tab", ui.KeySpace: "Space", ui.KeyBackspace: "Backspace",
	ui.KeyDelete: "Delete", ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
	ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down",
	ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/", ui.KeySemicolon: ";",
	ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
}

var keySymbols = map[ui.Key]string{
	ui.KeyEnter: "↩", ui.KeyEscape: "⎋", ui.KeyTab: "⇥", ui.KeySpace: "空格", ui.KeyBackspace: "⌫", ui.KeyDelete: "⌦",
	ui.KeyLeft: "←", ui.KeyRight: "→", ui.KeyUp: "↑", ui.KeyDown: "↓", ui.KeyPageUp: "⇞", ui.KeyPageDown: "⇟",
	ui.KeyHome: "↖", ui.KeyEnd: "↘",
}

func init() {
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		keyNames[k] = string(rune('A' + int(k-ui.KeyA)))
	}
	for k := ui.Key0; k <= ui.Key9; k++ {
		keyNames[k] = string(rune('0' + int(k-ui.Key0)))
	}
	for i, k := range []ui.Key{ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6, ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12} {
		keyNames[k] = "F" + string(rune('1'+i))
		if i >= 9 {
			keyNames[k] = "F1" + string(rune('0'+i-9))
		}
	}
}

// Valid reports whether the combo can be a shortcut: a known key with Cmd,
// Control or Option, or a function key. Plain letters type.
func (c Combo) Valid() bool {
	if _, ok := keyNames[c.Key]; !ok {
		return false
	}
	if c.Key >= ui.KeyF1 && c.Key <= ui.KeyF12 {
		return true
	}
	return c.Mods&(ui.Cmd|ui.Ctrl|ui.Alt|ui.Super) != 0
}

// String writes the combo as it is stored: "Cmd+Shift+K".
func (c Combo) String() string {
	var parts []string
	mods := c.Mods
	if mods&ui.Cmd != 0 {
		parts = append(parts, "Cmd")
		mods &^= ui.Cmd
	}
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}, {ui.Super, "Super"}} {
		if mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, keyNames[c.Key]), "+")
}

// Label writes the combo as menus show it: "⇧⌘K" on macOS, "Ctrl+Shift+K"
// elsewhere.
func (c Combo) Label() string {
	return c.label(runtime.GOOS == "darwin")
}

func (c Combo) label(darwin bool) string {
	key := keyNames[c.Key]
	if !darwin {
		var parts []string
		for _, m := range []struct {
			mod  ui.Modifiers
			name string
		}{{ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}, {ui.Super, "Super"}} {
			if c.Mods&m.mod != 0 {
				parts = append(parts, m.name)
			}
		}
		return strings.Join(append(parts, key), "+")
	}
	if s, ok := keySymbols[c.Key]; ok {
		s = i18n.T(s)
		key = s
	}
	var b strings.Builder
	for _, m := range []struct {
		mod  ui.Modifiers
		sign string
	}{{ui.Ctrl, "⌃"}, {ui.Alt, "⌥"}, {ui.Shift, "⇧"}, {ui.Super, "⌘"}} {
		if c.Mods&m.mod != 0 {
			b.WriteString(m.sign)
		}
	}
	return b.String() + key
}

// ParseCombo reads a combo written by String.
func ParseCombo(s string) (Combo, bool) {
	parts := strings.Split(strings.TrimSpace(s), "+")
	if len(parts) == 0 {
		return Combo{}, false
	}
	name := parts[len(parts)-1]
	if name == "" && len(parts) >= 2 && parts[len(parts)-2] == "" {
		// "Cmd++" is the plus key, which no keyboard combo here names.
		return Combo{}, false
	}
	var c Combo
	found := false
	for k, n := range keyNames {
		if strings.EqualFold(n, name) {
			c.Key, found = k, true
			break
		}
	}
	if !found {
		return Combo{}, false
	}
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "cmd", "cmdorctrl":
			c.Mods |= ui.Cmd
		case "ctrl", "control":
			c.Mods |= ui.Ctrl
		case "alt", "option":
			c.Mods |= ui.Alt
		case "shift":
			c.Mods |= ui.Shift
		case "super", "meta":
			c.Mods |= ui.Super
		default:
			return Combo{}, false
		}
	}
	return c, true
}
