package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/locales"
)

func TestLegacyRuntimeCleanupWarningPreservesProjectOpenResult(t *testing.T) {
	h := newViewHarness(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "desktop-kernel"), 0o700); err != nil {
		t.Fatal(err)
	}
	openErr := errors.New("missing repository")
	h.a.openErr = openErr
	h.a.recycleLegacyRuntime(root, t.TempDir())
	h.apply(t)
	if h.a.legacyCleanupErr == nil || h.a.openErr != openErr {
		t.Fatalf("cleanup replaced project state: cleanup=%v open=%v", h.a.legacyCleanupErr, h.a.openErr)
	}
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		h.tt.SetLocale(locale)
		h.tt.Frame()
		tx := locales.In(locale)
		if !h.tt.HasText(tx.FailedLegacyCleanup(h.a.legacyCleanupErr.Error())) {
			t.Fatalf("%s: cleanup warning missing", locale)
		}
		if _, ok := h.tt.Find(tx.FailedOpen(openErr.Error())); !ok {
			t.Fatalf("%s: project error missing", locale)
		}
	}
}

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
