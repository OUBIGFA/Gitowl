package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKernelMigrationRejectsIncompleteRecordAndKeepsLegacyData(t *testing.T) {
	for _, record := range []string{"", "{}", `{"format":1}`, `{"format":2,"newProfile":true}`, `{"format":1,"databases":[],"settings":-1}`} {
		t.Run(record, func(t *testing.T) {
			profile := t.TempDir()
			destination := filepath.Join(profile, "backend-data")
			if err := os.Mkdir(destination, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(destination, "migration.json")
			if err := os.WriteFile(path, []byte(record), 0600); err != nil {
				t.Fatal(err)
			}
			if err := prepareKernelData(KernelConfig{UserData: profile}); err == nil {
				t.Fatal("accepted incomplete migration")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != record {
				t.Fatal("invalid receipt was overwritten")
			}
		})
	}
	profile := t.TempDir()
	legacy := filepath.Join(profile, "desktop-kernel", "IndexedDB")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := prepareKernelData(KernelConfig{UserData: profile, Entry: filepath.Join(profile, "backend", "main.js")}); err == nil {
		t.Fatal("silently created empty profile without old reader")
	}
	if _, err := os.Stat(filepath.Join(profile, "backend-data")); !os.IsNotExist(err) {
		t.Fatal("changed destination despite missing reader")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("lost original database")
	}
}

func TestKernelMigrationAcceptsCompletedAndNewProfiles(t *testing.T) {
	for _, record := range []string{`{"format":1,"newProfile":true}`, `{"format":1,"databases":["repositories"],"settings":4}`} {
		profile := t.TempDir()
		destination := filepath.Join(profile, "backend-data")
		if err := os.Mkdir(destination, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, "migration.json"), []byte(record), 0600); err != nil {
			t.Fatal(err)
		}
		if err := prepareKernelData(KernelConfig{UserData: profile}); err != nil {
			t.Fatal(err)
		}
	}
	profile := t.TempDir()
	if err := prepareKernelData(KernelConfig{UserData: profile}); err != nil {
		t.Fatal(err)
	}
	if err := verifyMigrationReceipt(filepath.Join(profile, "backend-data", "migration.json")); err != nil {
		t.Fatal(err)
	}
}
