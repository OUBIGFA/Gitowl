package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLegacyRuntimeCleanupThroughWindowsJunction(t *testing.T) {
	work := t.TempDir()
	programs := filepath.Join(work, "physical programs")
	root := filepath.Join(programs, "Gitowl")
	old := filepath.Join(root, "desktop-kernel")
	data := t.TempDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	junction := func(name, target string) string {
		t.Helper()
		path := filepath.Join(work, name)
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			"$ErrorActionPreference='Stop'; New-Item -ItemType Junction -Path $env:GITOWL_TEST_LINK -Target $env:GITOWL_TEST_TARGET | Out-Null")
		cmd.Env = append(os.Environ(), "GITOWL_TEST_LINK="+path, "GITOWL_TEST_TARGET="+target)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create junction: %v: %s", err, out)
		}
		return path
	}
	install := filepath.Join(junction("programs", programs), "Gitowl")
	if path, err := legacyRuntimeDirectory(install, data); err != nil || path != "" {
		t.Errorf("no previous installation: got %q, %v", path, err)
	}
	if err := os.MkdirAll(filepath.Join(old, "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"electron.exe", filepath.Join("app", "main.js")} {
		if err := os.WriteFile(filepath.Join(old, file), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if path, err := legacyRuntimeDirectory(install, data); err != nil || path != old {
		t.Errorf("known installation: got %q, %v; want %q", path, err, old)
	}
	if path, err := legacyRuntimeDirectory(install, junction("profile", filepath.Join(old, "app"))); err == nil || path != "" {
		t.Errorf("profile inside old runtime was eligible: %q, %v", path, err)
	}
	// A junction above the install is supported, but the cleanup target itself
	// must never be a junction, even if the target looks like a shipped runtime.
	junction("desktop-kernel", old)
	if path, err := legacyRuntimeDirectory(work, data); err == nil || path != "" {
		t.Errorf("redirected runtime was eligible: %q, %v", path, err)
	}
}
