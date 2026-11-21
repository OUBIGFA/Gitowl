package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func verifyMigrationReceipt(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var receipt struct {
		Format     int       `json:"format"`
		NewProfile bool      `json:"newProfile"`
		Databases  *[]string `json:"databases"`
		Settings   *int      `json:"settings"`
	}
	if json.Unmarshal(data, &receipt) != nil || receipt.Format != 1 ||
		(!receipt.NewProfile && (receipt.Databases == nil || receipt.Settings == nil || *receipt.Settings < 0)) {
		return errors.New("invalid migration completion record; existing data was preserved")
	}
	return nil
}

// Migration uses the installed legacy engine only as a one-time reader. The
// new package contains its export script, never an Electron distribution.
// Publish a whole imported directory after success; leave all old data intact.
func prepareKernelData(config KernelConfig) error {
	destination := filepath.Join(config.UserData, "backend-data")
	if err := verifyMigrationReceipt(filepath.Join(destination, "migration.json")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	legacy := filepath.Join(config.UserData, "desktop-kernel")
	hasLegacy := false
	for _, name := range []string{"IndexedDB", "Local Storage"} {
		if info, err := os.Stat(filepath.Join(legacy, name)); err == nil {
			if !info.IsDir() {
				return fmt.Errorf("legacy storage is not a directory: %s", name)
			}
			hasLegacy = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect legacy storage: %w", err)
		}
	}
	if !hasLegacy {
		if err := os.MkdirAll(destination, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destination, "migration.json"), []byte("{\"format\":1,\"newProfile\":true}\n"), 0o600)
	}
	if _, err := os.Stat(destination); err == nil {
		return errors.New("an unverified new profile already exists; original data was preserved")
	} else if !os.IsNotExist(err) {
		return err
	}
	backend := filepath.Dir(config.Entry)
	exporter := filepath.Join(backend, "legacy-export.cjs")
	oldEngine := filepath.Join(backend, "..", "desktop-kernel", "electron.exe")
	if _, err := os.Stat(oldEngine); err != nil {
		return errors.New("legacy data requires its previous installed runtime for one-time migration; original data was preserved")
	}
	exported := filepath.Join(config.UserData, "legacy-export.bin")
	cmd := exec.Command(oldEngine, exporter)
	cmd.Env = append(kernelEnvironment(config.UserData), "GITOWL_LEGACY_EXPORT="+exported, "ELECTRON_NO_ATTACH_CONSOLE=1")
	hideConsole(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("export original data: %w", err)
	}
	staging, err := os.MkdirTemp(config.UserData, "migration-pending-")
	if err != nil {
		return err
	}
	cmd = exec.Command(config.Node, "--conditions=import", "--experimental-webstorage", "--localstorage-file="+filepath.Join(staging, "settings.sqlite"), filepath.Join(backend, "migration.js"), exported, staging)
	cmd.Env = kernelEnvironment(config.UserData)
	hideConsole(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("import original data; staging retained for recovery: %w", err)
	}
	if err := verifyMigrationReceipt(filepath.Join(staging, "migration.json")); err != nil {
		return fmt.Errorf("data import did not produce a valid completion record: %w", err)
	}
	return os.Rename(staging, destination)
}
