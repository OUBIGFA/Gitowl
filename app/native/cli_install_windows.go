//go:build windows

package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

// The fixed bin path survives upgrades. The native executable owns CLI routing;
// installers only register that path and never invoke a JavaScript runtime.
func installNativeCLI(remove bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	install := filepath.Dir(exe)
	bin := filepath.Join(install, "bin")
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	value, kind, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	parts := []string{}
	found := false
	for _, part := range strings.Split(value, ";") {
		if strings.EqualFold(strings.Trim(strings.TrimSpace(part), "\""), bin) {
			found = true
			if remove {
				continue
			}
		}
		parts = append(parts, part)
	}
	if !remove {
		if err := os.MkdirAll(bin, 0755); err != nil {
			return err
		}
		batch := "@echo off\r\n\"%~dp0..\\Gitowl.exe\" %*\r\n"
		if err := os.WriteFile(filepath.Join(bin, "gitowl.bat"), []byte(batch), 0644); err != nil {
			return err
		}
		shell := "#!/bin/sh\nDIR=\"$(CDPATH= cd -- \"$(dirname -- \"$0\")\" && pwd)\"\nexec \"$DIR/../Gitowl.exe\" \"$@\"\n"
		if err := os.WriteFile(filepath.Join(bin, "gitowl"), []byte(shell), 0755); err != nil {
			return err
		}
		if !found {
			parts = append(parts, bin)
		}
	}
	if kind == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", strings.Join(parts, ";"))
	} else {
		err = key.SetStringValue("Path", strings.Join(parts, ";"))
	}
	if err != nil {
		return err
	}
	// Notify Explorer so terminals opened after installation inherit the CLI
	// path. Bound the broadcast; an unrelated hung window must not stall setup.
	message, _ := windows.UTF16PtrFromString("Environment")
	var result uintptr
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(0xffff, 0x001a, 0, uintptr(unsafe.Pointer(message)), 2, 2000, uintptr(unsafe.Pointer(&result)))
	return nil
}
