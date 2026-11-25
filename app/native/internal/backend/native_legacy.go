package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo/plugins/sqlite"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Chromium's existing installed engine is a one-time old-data reader only. It
// is never shipped by or used as the business host of the native installation.
func prepareNativeLegacy(ctx context.Context, resources, userData string) error {
	if _, err := os.Stat(filepath.Join(userData, "gitowl-native.sqlite")); err == nil {
		db, err := sqlite.Open(ctx, filepath.Join(userData, "gitowl-native.sqlite"), sqlite.OpenOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		rows, queryErr := db.Query(ctx, "SELECT value FROM settings WHERE key='native-migration-v1'")
		db.Close()
		if queryErr != nil {
			return queryErr
		}
		// A failed import already creates the database. Only the transaction's
		// completion marker permits skipping the one-time legacy reader.
		if len(rows.Values) == 1 && rows.Values[0][0] == "complete" {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := verifyMigrationReceipt(filepath.Join(userData, "backend-data", "migration.json")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	legacy := false
	for _, name := range []string{"IndexedDB", "Local Storage"} {
		if _, err := os.Stat(filepath.Join(userData, "desktop-kernel", name)); err == nil {
			legacy = true
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if !legacy {
		return nil
	}
	// A Node-era SQLite file can exist after a failed migration. Its existence
	// alone is not evidence that the older Chromium data was imported.
	destination := filepath.Join(userData, "legacy-export.bin")
	if _, err := os.Stat(destination); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	engine := filepath.Join(resources, "..", "desktop-kernel", "electron.exe")
	if _, err := os.Stat(engine); err != nil {
		return errors.New("the installed legacy data reader is unavailable; original data was preserved")
	}
	exporter := filepath.Join(resources, "migration", "legacy-export.cjs")
	cmd := exec.CommandContext(ctx, engine, exporter)
	hideConsole(cmd)
	cmd.Env = append(os.Environ(), "GITOWL_USER_DATA="+userData, "GITOWL_LEGACY_EXPORT="+destination, "ELECTRON_NO_ATTACH_CONSOLE=1")
	if err := cmd.Run(); err != nil {
		return errors.New("legacy data export failed; original data was preserved")
	}
	return nil
}

func nativeLegacyStatements(path string) ([]sqlite.Statement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("legacy data requires a valid migration export; original data was preserved")
	}
	var exported struct {
		Format    int `json:"format"`
		Databases []struct {
			Name   string `json:"name"`
			Stores []struct {
				Name   string            `json:"name"`
				Keys   []json.RawMessage `json:"keys"`
				Values []json.RawMessage `json:"values"`
			} `json:"stores"`
		} `json:"databases"`
		Settings map[string]string `json:"settings"`
	}
	if json.Unmarshal(data, &exported) != nil || exported.Format != 1 || exported.Settings == nil {
		return nil, errors.New("legacy migration export is invalid")
	}
	statements := []sqlite.Statement{{SQL: "INSERT OR IGNORE INTO settings(key,value) VALUES ('legacy-structured-export',?)", Args: []any{string(data)}}}
	for key, value := range exported.Settings {
		statements = append(statements, sqlite.Statement{SQL: "INSERT OR IGNORE INTO settings(key,value) VALUES (?,?)", Args: []any{key, value}})
	}
	for _, database := range exported.Databases {
		for _, store := range database.Stores {
			if len(store.Keys) != len(store.Values) {
				return nil, errors.New("legacy export key/value counts differ")
			}
			for i, value := range store.Values {
				name := "S_" + store.Name
				for _, upper := range []string{"H", "R", "B"} {
					name = strings.ReplaceAll(name, upper, "^"+upper)
				}
				statements = append(statements, sqlite.Statement{SQL: "INSERT OR IGNORE INTO legacy_records VALUES (?,?,?,?)", Args: []any{database.Name, name, string(store.Keys[i]), strings.ReplaceAll(string(value), "^", "^^")}})
				if store.Name == "repositories" {
					var r KernelRepository
					if err := json.Unmarshal(value, &r); err != nil || r.Path == "" || r.ID <= 0 {
						return nil, fmt.Errorf("invalid exported repository in %s", database.Name)
					}
					r.Name = filepath.Base(r.Path)
					r.Kind = "repository"
					encoded, err := json.Marshal(r)
					if err != nil {
						return nil, err
					}
					statements = append(statements, sqlite.Statement{SQL: "INSERT OR IGNORE INTO repositories(id,path,data) VALUES (?,?,?)", Args: []any{r.ID, r.Path, string(encoded)}})
				}
			}
		}
	}
	return statements, nil
}
