package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyRuntimeCleanupOnlySelectsKnownInstallFiles(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	if path, err := legacyRuntimeDirectory(root, data); err != nil || path != "" {
		t.Fatalf("empty installation: %q %v", path, err)
	}
	old := filepath.Join(root, "desktop-kernel")
	if err := os.MkdirAll(filepath.Join(old, "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyRuntimeDirectory(root, data); err == nil {
		t.Fatal("unknown directory was eligible for recycling")
	}
	for _, file := range []string{"electron.exe", filepath.Join("app", "main.js")} {
		if err := os.WriteFile(filepath.Join(old, file), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if path, err := legacyRuntimeDirectory(root, data); err != nil || path != old {
		t.Fatalf("known installation: %q %v", path, err)
	}
	if _, err := legacyRuntimeDirectory(root, old); err == nil {
		t.Fatal("user data was eligible for recycling")
	}
	for _, data := range []string{root, filepath.Join(old, "app")} {
		if _, err := legacyRuntimeDirectory(root, data); err == nil {
			t.Fatal("overlapping user data was eligible for recycling")
		}
	}
}
