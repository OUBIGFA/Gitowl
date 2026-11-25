package backend

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo/plugins/sqlite"
)

// NativeStore uses MyGo's maintained SQLite module, including its actual
// transactions. Migration reads old databases without changing their bytes.
type NativeStore struct{ db *sqlite.DB }

func OpenNativeStore(ctx context.Context, directory string) (*NativeStore, error) {
	if !filepath.IsAbs(directory) {
		return nil, errors.New("native data path must be absolute")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	db, err := sqlite.Open(ctx, filepath.Join(directory, "gitowl-native.sqlite"), sqlite.OpenOptions{})
	if err != nil {
		return nil, err
	}
	s := &NativeStore{db}
	for _, sql := range []string{
		"CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS repositories (id INTEGER PRIMARY KEY, path TEXT NOT NULL UNIQUE, data TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS legacy_records (database_name TEXT NOT NULL, table_name TEXT NOT NULL, record_key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(database_name,table_name,record_key))",
	} {
		if _, err := db.Execute(ctx, sql); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := s.importLegacy(ctx, directory); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrateAccountLinks(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *NativeStore) Close() error { return s.db.Close() }
func (s *NativeStore) Get(ctx context.Context, key string) (string, error) {
	rows, err := s.db.Query(ctx, "SELECT value FROM settings WHERE key=?", key)
	if err != nil {
		return "", err
	}
	if len(rows.Values) == 0 {
		return "", nil
	}
	return rows.Values[0][0].(string), nil
}
func (s *NativeStore) Set(ctx context.Context, key, value string) error {
	_, err := s.db.Execute(ctx, "INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}
func (s *NativeStore) Repositories(ctx context.Context) ([]KernelRepository, error) {
	rows, err := s.db.Query(ctx, "SELECT data FROM repositories ORDER BY id")
	if err != nil {
		return nil, err
	}
	result := []KernelRepository{}
	for _, row := range rows.Values {
		var r KernelRepository
		if err := json.Unmarshal([]byte(row[0].(string)), &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}
func (s *NativeStore) SaveRepository(ctx context.Context, r KernelRepository) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.Execute(ctx, "INSERT INTO repositories(id,path,data) VALUES (?,?,?) ON CONFLICT(id) DO UPDATE SET path=excluded.path,data=excluded.data", r.ID, r.Path, string(data))
	return err
}
func (s *NativeStore) RemoveRepository(ctx context.Context, id int) error {
	_, err := s.db.Execute(ctx, "DELETE FROM repositories WHERE id=?", id)
	return err
}

func legacyText(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return "", errors.New("legacy text has an unsupported SQLite type")
	}
}
func legacyUTF16(value any) (string, error) {
	data, ok := value.([]byte)
	if !ok || len(data)%2 != 0 {
		return "", errors.New("invalid Node WebStorage UTF-16 value")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	decoded := utf16.Decode(units)
	encoded := utf16.Encode(decoded)
	if len(encoded) != len(units) {
		return "", errors.New("unsupported unpaired UTF-16 data; original settings were preserved")
	}
	for i := range encoded {
		if encoded[i] != units[i] {
			return "", errors.New("unsupported unpaired UTF-16 data; original settings were preserved")
		}
	}
	return string(decoded), nil
}

// IndexedDBShim escapes literal carets and NUL before SQLite storage. Decode
// that documented storage representation before interpreting record JSON.
func unescapeLegacySQLite(value string) string {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '^' && i+1 < len(value) {
			switch value[i+1] {
			case '^':
				result.WriteByte('^')
				i++
				continue
			case '0':
				result.WriteByte(0)
				i++
				continue
			}
		}
		result.WriteByte(value[i])
	}
	return result.String()
}

func (s *NativeStore) importLegacy(ctx context.Context, directory string) error {
	done, err := s.Get(ctx, "native-migration-v1")
	if err != nil || done == "complete" {
		return err
	}
	var statements []sqlite.Statement
	chromium := false
	for _, name := range []string{"IndexedDB", "Local Storage"} {
		if _, err := os.Stat(filepath.Join(directory, "desktop-kernel", name)); err == nil {
			chromium = true
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	nodeAllowed := true
	if chromium {
		if err := verifyMigrationReceipt(filepath.Join(directory, "backend-data", "migration.json")); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			nodeAllowed = false
		}
	}
	settings := filepath.Join(directory, "backend-data", "settings.sqlite")
	if nodeAllowed {
		if _, err := os.Stat(settings); err == nil {
			db, err := sqlite.Open(ctx, settings, sqlite.OpenOptions{ReadOnly: true})
			if err != nil {
				return err
			}
			rows, readErr := db.Query(ctx, "SELECT key,value FROM nodejs_webstorage")
			db.Close()
			if readErr != nil {
				return readErr
			}
			for _, row := range rows.Values {
				key, err := legacyUTF16(row[0])
				if err != nil {
					return err
				}
				value, err := legacyUTF16(row[1])
				if err != nil {
					return err
				}
				statements = append(statements, sqlite.Statement{SQL: "INSERT OR IGNORE INTO settings(key,value) VALUES (?,?)", Args: []any{key, value}})
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	databases := filepath.Join(directory, "backend-data", "databases")
	files, err := filepath.Glob(filepath.Join(databases, "*.sqlite"))
	if err != nil {
		return err
	}
	if !nodeAllowed {
		files = nil
	}
	for _, file := range files {
		if strings.HasPrefix(filepath.Base(file), "__sysdb__") {
			continue
		}
		db, err := sqlite.Open(ctx, file, sqlite.OpenOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		func() {
			defer db.Close()
			tables, readErr := db.Query(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'S_%'")
			if readErr != nil {
				err = readErr
				return
			}
			for _, table := range tables.Values {
				name := table[0].(string)
				rows, readErr := db.Query(ctx, "SELECT key,value FROM \""+strings.ReplaceAll(name, "\"", "\"\"")+"\"")
				if readErr != nil {
					err = readErr
					return
				}
				for _, row := range rows.Values {
					key, e := legacyText(row[0])
					if e != nil {
						err = e
						return
					}
					value, e := legacyText(row[1])
					if e != nil {
						err = e
						return
					}
					// Retain every original record verbatim, including data which the
					// native UI does not currently display. Never silently discard it.
					statements = append(statements, sqlite.Statement{SQL: "INSERT OR IGNORE INTO legacy_records VALUES (?,?,?,?)", Args: []any{filepath.Base(file), name, key, value}})
					if name == "S_repositories" {
						var old struct {
							ID      int
							Path    string
							Alias   *string
							Missing bool
							Login   *string
						}
						if e := json.Unmarshal([]byte(unescapeLegacySQLite(value)), &old); e != nil || old.ID <= 0 || old.Path == "" {
							err = fmt.Errorf("invalid legacy repository record in %s", filepath.Base(file))
							return
						}
						r := KernelRepository{KernelRepositoryReference: KernelRepositoryReference{old.ID, old.Path}, Name: filepath.Base(old.Path), Alias: old.Alias, Missing: old.Missing, Login: old.Login, Kind: "repository"}
						data, _ := json.Marshal(r)
						statements = append(statements, sqlite.Statement{SQL: "INSERT OR IGNORE INTO repositories(id,path,data) VALUES (?,?,?)", Args: []any{r.ID, r.Path, string(data)}})
					}
				}
			}
		}()
		if err != nil {
			return err
		}
	}
	// Unmigrated Chromium storage cannot be mistaken for an empty installation.
	if len(files) == 0 {
		for _, name := range []string{"IndexedDB", "Local Storage"} {
			if _, err := os.Stat(filepath.Join(directory, "desktop-kernel", name)); err == nil {
				exported, err := nativeLegacyStatements(filepath.Join(directory, "legacy-export.bin"))
				if err != nil {
					return err
				}
				statements = append(statements, exported...)
				break
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	statements = append(statements, sqlite.Statement{SQL: "INSERT INTO settings(key,value) VALUES ('native-migration-v1','complete')"})
	_, err = s.db.Transaction(ctx, statements)
	return err
}
