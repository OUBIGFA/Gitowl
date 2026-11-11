package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestWindowClosePreferencePersistsAndRejectsInvalidData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-preferences.json")
	prefs, err := readWindowPreferences(path)
	if err != nil || prefs.CloseBehavior != closeWindowExit {
		t.Fatalf("first launch must exit: %+v, %v", prefs, err)
	}
	if err = writeWindowPreferences(path, nativeWindowPreferences{CloseBehavior: closeWindowToTray}); err != nil {
		t.Fatal(err)
	}
	prefs, err = readWindowPreferences(path)
	if err != nil || prefs.CloseBehavior != closeWindowToTray {
		t.Fatalf("close preference did not survive reload: %+v, %v", prefs, err)
	}
	if err = os.WriteFile(path, []byte(`{"closeBehavior":"unknown"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = readWindowPreferences(path); err == nil {
		t.Fatal("invalid preference was silently accepted")
	}
}

func TestTrayCloseKeepsWorkAliveAndExplicitQuitStillUsesWriteGuard(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.windowPreferences.CloseBehavior = closeWindowToTray
	a.savingVersion = true
	hidden := 0
	closing := &mygo.CloseEvent{}
	a.handleWindowClose(closing, func() { hidden++ })
	if !closing.DefaultPrevented() || hidden != 1 || a.closed {
		t.Fatal("tray close must hide the window and keep the active operation alive")
	}
	quitting := &mygo.QuitEvent{}
	a.handleBeforeQuit(quitting)
	if !quitting.DefaultPrevented() || a.quitting {
		t.Fatal("explicit quit abandoned an active write")
	}
	a.savingVersion = false
	a.handleBeforeQuit(&mygo.QuitEvent{})
	closing = &mygo.CloseEvent{}
	a.handleWindowClose(closing, func() { hidden++ })
	if closing.DefaultPrevented() || hidden != 1 || !a.quitting {
		t.Fatal("explicit quit was converted back into hiding")
	}
}

func TestTrayPreferenceFailureLeavesWindowReachable(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.windowPreferencesPath = filepath.Join(t.TempDir(), "window-preferences.json")
	a.prepareTray = func(bool) error { return errors.New("tray unavailable") }
	a.changeWindowCloseBehavior(closeWindowToTray)
	if a.windowPreferenceErr == nil || a.windowPreferences.CloseBehavior == closeWindowToTray {
		t.Fatal("failed tray creation enabled hiding")
	}
	closing := &mygo.CloseEvent{}
	a.handleWindowClose(closing, func() { t.Fatal("window hidden without a tray") })
	if closing.DefaultPrevented() {
		t.Fatal("default exit behavior was lost")
	}
}

func TestFailedPreferenceWriteKeepsExistingTray(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.windowPreferences.CloseBehavior = closeWindowToTray
	// A directory at the destination makes the atomic replacement fail.
	a.windowPreferencesPath = filepath.Join(t.TempDir(), "blocked-destination")
	if err := os.Mkdir(a.windowPreferencesPath, 0o700); err != nil {
		t.Fatal(err)
	}
	a.prepareTray = func(enabled bool) error {
		if !enabled {
			t.Fatal("existing tray removed before preference was saved")
		}
		return nil
	}
	a.changeWindowCloseBehavior(closeWindowExit)
	if a.windowPreferenceErr == nil || a.windowPreferences.CloseBehavior != closeWindowToTray {
		t.Fatal("failed write did not preserve the current preference")
	}
}
