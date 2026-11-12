package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	closeWindowExit   = "exit"
	closeWindowToTray = "tray"
)

// This preference belongs to the visible native host, not the Git kernel.
type nativeWindowPreferences struct {
	CloseBehavior string `json:"closeBehavior"`
}

func readWindowPreferences(path string) (nativeWindowPreferences, error) {
	prefs := nativeWindowPreferences{CloseBehavior: closeWindowToTray}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return prefs, nil
	}
	if err != nil {
		return prefs, err
	}
	if err = json.Unmarshal(data, &prefs); err != nil {
		return prefs, err
	}
	if prefs.CloseBehavior != closeWindowExit && prefs.CloseBehavior != closeWindowToTray {
		return nativeWindowPreferences{CloseBehavior: closeWindowToTray}, fmt.Errorf("invalid window close behavior %q", prefs.CloseBehavior)
	}
	return prefs, nil
}

func writeWindowPreferences(path string, prefs nativeWindowPreferences) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	// Replace only after the complete new file has reached disk. The stable
	// profile path preserves this choice across restarts and test upgrades.
	temp := path + ".tmp"
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(data, '\n'))
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func (a *app) changeWindowCloseBehavior(value string) {
	if value != closeWindowExit && value != closeWindowToTray {
		return
	}
	previous := a.windowPreferences
	next := nativeWindowPreferences{CloseBehavior: value}
	if value == closeWindowToTray && a.prepareTray != nil {
		if err := a.prepareTray(true); err != nil {
			a.windowPreferenceErr = err
			return
		}
	}
	if err := writeWindowPreferences(a.windowPreferencesPath, next); err != nil {
		if a.prepareTray != nil && previous.CloseBehavior != closeWindowToTray {
			err = errors.Join(err, a.prepareTray(false))
		}
		a.windowPreferenceErr = err
		return
	}
	a.windowPreferences, a.windowPreferenceErr = next, nil
	// Keep the existing tray until an exit preference is safely persisted.
	// A failed file write must not remove the user's way back to the window.
	if value == closeWindowExit && a.prepareTray != nil {
		a.windowPreferenceErr = a.prepareTray(false)
	}
}

func (a *app) handleWindowClose(event *mygo.CloseEvent, hide func()) {
	if !a.quitting && a.windowPreferences.CloseBehavior == closeWindowToTray {
		// Hiding does not cancel work or close the kernel. Explicit Quit below
		// still waits for the existing write guard before closing this window.
		event.PreventDefault()
		hide()
		return
	}
	a.deferQuit(&event.Preventable)
}

func (a *app) handleBeforeQuit(event *mygo.QuitEvent) {
	a.deferQuit(&event.Preventable)
	a.quitting = !event.DefaultPrevented()
}

// quitBlocked reports work whose result a quit could abandon: a version save,
// queued presentation work, or an admitted kernel call still awaiting reply.
func (a *app) quitBlocked() bool {
	return a.savingVersion || a.core != nil && (a.corePending != 0 || a.core.CriticalInFlight() != 0)
}

// deferQuit replaces a refused quit with a pending one. Under load there is
// almost always a refresh in flight, so refusing made quitting unreliable;
// forcing it could abandon a Git write. Instead new work stops being admitted,
// cancelable generation stops, and the app quits as soon as admitted work
// settles. The window stays reachable for any prompt that work still needs.
func (a *app) deferQuit(event *mygo.Preventable) {
	if !a.quitBlocked() {
		a.quitDeferred = false
		return
	}
	event.PreventDefault()
	if a.quitDeferred {
		return
	}
	// Cancellation is itself a kernel call; send it before admission stops.
	if a.generating {
		a.stopGeneration()
	}
	a.quitDeferred = true
	if a.showWindow != nil {
		a.showWindow()
	}
	update, ctx := a.update, a.ctx
	if update == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		done := make(chan struct{})
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				update(func() {
					select {
					case <-done:
						return
					default:
					}
					if a.closed || !a.quitDeferred {
						close(done)
						return
					}
					if !a.quitBlocked() {
						close(done)
						a.quitDeferred = false
						if a.requestQuit != nil {
							a.requestQuit()
						}
					}
				})
			}
		}
	}()
}

func (a *app) quitPendingView(c *ui.Context, tx locales.Messages) {
	if !a.quitDeferred {
		return
	}
	count := a.corePending
	if a.core != nil {
		count = max(count, a.core.CriticalInFlight())
	}
	if a.savingVersion {
		count = max(count, 1)
	}
	ui.Text(c.Key("quit-pending"), tx.QuitWaitingForWork(count)).TextColor(c.Theme().Warning).Margin(0, 0, 16, 0).Role(ui.RoleStatus)
}

func (a *app) windowBehaviorView(c *ui.Context, tx locales.Messages) {
	ui.Field(c, tx.WindowCloseBehavior(), func() {
		value := a.windowPreferences.CloseBehavior
		if value == "" {
			value = closeWindowExit
		}
		workspaceSelect(c.Key("window-close-behavior"), &value, tx.WindowCloseBehavior(), "", []nativeChoice[string]{
			{closeWindowExit, tx.ExitApplication()}, {closeWindowToTray, tx.MinimizeToTray()},
		}, a.prepareTray == nil).FillWidth().OnChange(func() { a.changeWindowCloseBehavior(value) })
	}).FillWidth().Shrink(0)
	if a.windowPreferenceErr != nil {
		behavior := tx.ExitApplication()
		if a.windowPreferences.CloseBehavior == closeWindowToTray {
			behavior = tx.MinimizeToTray()
		}
		ui.Text(c, tx.WindowPreferenceFailed(behavior)).TextColor(c.Theme().Danger)
		ui.Text(c, a.windowPreferenceErr.Error()).TextColor(c.Theme().TextMuted)
	}
}
