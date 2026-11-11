package main

import (
	"crypto/sha256"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo"
)

// One native menu made from the original menu's whitelist DTO. Rebuild only
// when its data or locale changes, not on every Git progress snapshot.
type nativeMenuHost struct {
	state   backend.KernelMenuDTO
	locale  string
	ready   bool
	menu    *mygo.Menu
	execute func(string)
	zoom    func(string)
}

func (host *nativeMenuHost) update(state backend.KernelMenuDTO, locale string, ready bool) {
	if host.menu != nil && host.locale == locale && host.ready == ready && reflect.DeepEqual(host.state, state) {
		return
	}
	host.state, host.locale, host.ready = state, locale, ready
	host.menu = mygo.NewMenu(nativeMenuItems(state.Items, locale, ready, host.execute, host.zoom))
	mygo.App.SetMenu(host.menu)
}

func nativeMenuLabel(item backend.KernelMenuItem, locale string) string {
	id := sha256.Sum256([]byte(item.LabelKey))
	label := locales.Catalog.Format(locale, fmt.Sprintf("dialogLabel_%x", id[:8]), nil)
	// Technical names remain data. An ampersand in a branch/editor name must
	// not create an accidental Win32 mnemonic.
	label = strings.ReplaceAll(label, "{value}", item.LabelData)
	label = strings.ReplaceAll(label, "&", "&&")
	if item.AccessKey != "" {
		lower := strings.ToLower(label)
		if index := strings.Index(lower, strings.ToLower(item.AccessKey)); index >= 0 {
			label = label[:index] + "&" + label[index:]
		} else {
			label += " (&" + strings.ToUpper(item.AccessKey) + ")"
		}
	}
	return label
}

// Unlike a webview, MyGo native UI deliberately ignores page zoom roles
// (v0.8.1 menu.go). Those three actions use the original Desktop zoom steps
// through the host's Context.SetZoom-backed presentation instead. Select All
// also routes here: Windows' native editing-role enablement does not offer its
// fallback shortcut to lists. The host handles the list or delegates to the
// same native editing role via Window.SelectAll, never the hidden Electron UI.
func nativeMenuItems(items []backend.KernelMenuItem, locale string, ready bool, execute func(string), zoom func(string)) []*mygo.MenuItem {
	out := make([]*mygo.MenuItem, 0, len(items))
	for _, dto := range items {
		item := &mygo.MenuItem{ID: dto.ID, Type: mygo.MenuItemType(dto.Type), Disabled: !dto.Enabled || !ready, Hidden: !dto.Visible, Checked: dto.Checked, Accelerator: dto.Accelerator}
		if dto.Type != "separator" {
			item.Label = nativeMenuLabel(dto, locale)
		}
		if dto.Type == "submenu" {
			item.Submenu = nativeMenuItems(dto.Items, locale, ready && dto.Enabled && dto.Visible, execute, zoom)
		} else if dto.Type != "separator" {
			switch dto.Role {
			case "resetZoom", "zoomIn", "zoomOut", "selectAll":
				item.Click = func(*mygo.MenuItem, *mygo.Window) {
					if zoom != nil {
						zoom(dto.Role)
					}
				}
			case "undo", "redo", "cut", "copy", "paste", "togglefullscreen", "minimize", "zoom", "close", "quit", "hide", "hideOthers", "unhide", "front", "toggleDevTools":
				// A native role must never also execute the hidden Electron role.
				item.Role = mygo.MenuRole(dto.Role)
			default:
				item.Click = func(*mygo.MenuItem, *mygo.Window) {
					if execute != nil {
						execute(dto.ID)
					}
				}
			}
		} else {
			item.Disabled = true
		}
		if dto.Type == "submenu" && (dto.Role == "services" || dto.Role == "window" || dto.Role == "help") {
			item.Role = mygo.MenuRole(dto.Role)
		}
		out = append(out, item)
	}
	return out
}

// Toolbar actions inherit the original menu's complete availability policy,
// including disabled/invisible ancestors. The DTO is only presentation state;
// commands are still admitted and executed by the original kernel.
func nativeMenuAction(items []backend.KernelMenuItem, id string) (backend.KernelMenuItem, bool) {
	for _, item := range items {
		if item.ID == id {
			item.Enabled = item.Enabled && item.Visible
			return item, true
		}
		if found, ok := nativeMenuAction(item.Items, id); ok {
			found.Enabled = found.Enabled && item.Enabled && item.Visible
			return found, true
		}
	}
	return backend.KernelMenuItem{}, false
}

func nextNativeMenuZoom(current float64, action string) float64 {
	if action == "resetZoom" {
		return 1
	}
	steps := []float64{0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2}
	closest := 0
	for i := range steps {
		if math.Abs(steps[i]-current) < math.Abs(steps[closest]-current) {
			closest = i
		}
	}
	if action == "zoomIn" {
		return steps[min(closest+1, len(steps)-1)]
	}
	if action == "zoomOut" {
		return steps[max(closest-1, 0)]
	}
	return current
}
